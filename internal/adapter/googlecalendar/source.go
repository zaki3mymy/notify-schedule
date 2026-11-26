// Package googlecalendar は、Google Calendar API で予定を取得する ScheduleSource（usecase.ScheduleSource の実装）。
//
// 認証は ADC（Application Default Credentials）で行う。Cloud Run では実行用のサービスアカウント、
// 手元では gcloud auth application-default login の認証情報が使われる。キーファイルは使わない。
// 予定を読むカレンダーは、そのサービスアカウント（または手元のアカウント）に閲覧を共有しておく。
//
// Calendar API の型は、このパッケージの外に出さない（ドメインの型に変換して返す）。
package googlecalendar

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"github.com/zaki3mymy/notify-schedule/internal/domain"
	"github.com/zaki3mymy/notify-schedule/internal/usecase"
)

// pageSize は、1回のリクエストで取得する予定の数（Calendar API の上限は 2500）。
const pageSize = 250

// maxConcurrentFetches は、同時に予定を取得するカレンダーの数の上限。
// Calendar API に同時実行数の制限はない（制限は1分あたりのリクエスト数）が、
// カレンダーが増えても一度に投げすぎないようにする。
const maxConcurrentFetches = 5

// tracerName は、このパッケージが作る span の計装の名前。
const tracerName = "github.com/zaki3mymy/notify-schedule/internal/adapter/googlecalendar"

// Source は、Google Calendar API で予定を取得する。
type Source struct {
	svc *calendar.Service
}

// New は、Source を作る。スコープは calendar.readonly。
// opts は、テストでエンドポイントや HTTP クライアントを差し替えるのに使う（ふだんは何も渡さない）。
func New(ctx context.Context, opts ...option.ClientOption) (*Source, error) {
	opts = append([]option.ClientOption{option.WithScopes(calendar.CalendarReadonlyScope)}, opts...)
	svc, err := calendar.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("カレンダーの API のクライアントを作れませんでした: %w", err)
	}
	return &Source{svc: svc}, nil
}

// Fetch は、cals のすべてのカレンダーから d の予定を取得し、1つの DailySchedule にまとめる。
//
// d の範囲（d.Start〜d.End）に少しでもかかる予定を取得する（前日から続く予定や、翌日まで続く予定も含む）。
// キャンセルされた予定は除く。取得に失敗したカレンダーがあっても、ほかのカレンダーは取得し、
// 失敗したカレンダーを FetchResult.Failures で返す（error は返さない）。
// 失敗したカレンダーがあれば、fetch_schedule の span を Error にする。
//
// カレンダーは並行して取得する（同時に maxConcurrentFetches まで）。予定と Failures は、
// 取得が終わった順ではなく cals の順に並べる。
func (s *Source) Fetch(ctx context.Context, cals []domain.Calendar, d domain.Date) (usecase.FetchResult, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "fetch_schedule",
		trace.WithAttributes(attribute.Int("schedule.calendars", len(cals))))
	defer span.End()

	type fetched struct {
		events []domain.Event
		err    error
	}
	results := make([]fetched, len(cals))
	sem := make(chan struct{}, maxConcurrentFetches)
	var wg sync.WaitGroup
	for i, cal := range cals {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			events, err := s.fetchCalendar(ctx, cal, d)
			results[i] = fetched{events: events, err: err}
		})
	}
	wg.Wait()

	result := usecase.FetchResult{Schedule: domain.DailySchedule{Date: d}}
	for i, r := range results {
		if r.err != nil {
			result.Failures = append(result.Failures, usecase.CalendarFailure{Calendar: cals[i], Err: r.err})
			continue
		}
		result.Schedule.Events = append(result.Schedule.Events, r.events...)
	}
	span.SetAttributes(
		attribute.Int("schedule.events", len(result.Schedule.Events)),
		attribute.Int("schedule.failed_calendars", len(result.Failures)),
	)
	if len(result.Failures) > 0 {
		span.SetStatus(codes.Error, fmt.Sprintf("%d 件のカレンダーの予定を取得できませんでした", len(result.Failures)))
	}
	return result, nil
}

// fetchCalendar は、1つのカレンダーから d の予定を取得する。
//
// カレンダーごとに span を作る。カレンダーの ID はメールアドレスのことがあるので、span には
// ラベルだけを付ける。失敗したときも、エラーの本文（URL に ID を含むことがある）は記録せず、
// エラーの型だけを付ける。
func (s *Source) fetchCalendar(ctx context.Context, cal domain.Calendar, d domain.Date) (events []domain.Event, err error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "fetch_calendar",
		trace.WithAttributes(attribute.String("calendar.label", cal.Label)))
	defer func() {
		if err != nil {
			span.SetAttributes(attribute.String("error.type", fmt.Sprintf("%T", err)))
			span.SetStatus(codes.Error, "予定を取得できませんでした")
		} else {
			span.SetAttributes(attribute.Int("calendar.events", len(events)))
		}
		span.End()
	}()

	loc := d.Location()
	call := s.svc.Events.List(cal.ID).
		// 繰り返しの予定を、1回ずつの予定に展開する（orderBy=startTime を使うのに必要）
		SingleEvents(true).
		OrderBy("startTime").
		TimeMin(d.Start().Format(time.RFC3339)).
		TimeMax(d.End().Format(time.RFC3339)).
		TimeZone(loc.String()).
		MaxResults(pageSize)

	err = call.Pages(ctx, func(page *calendar.Events) error {
		for _, item := range page.Items {
			if item.Status == "cancelled" {
				continue
			}
			e, err := toEvent(item, cal.Label, loc)
			if err != nil {
				return err
			}
			events = append(events, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

// toEvent は、Calendar API の予定をドメインの予定に変換する。
//
// 終日予定（start.date）は、その日付の loc での 00:00 にする（end.date は最終日の翌日）。
// 時刻指定の予定（start.dateTime）は、RFC3339 の時刻をそのまま使う。
func toEvent(item *calendar.Event, label string, loc *time.Location) (domain.Event, error) {
	if item.Start == nil || item.End == nil {
		return domain.Event{}, fmt.Errorf("開始または終了がない予定があります（ID: %s）", item.Id)
	}
	e := domain.Event{Title: item.Summary, CalendarLabel: label}

	if item.Start.Date != "" {
		start, err := time.ParseInLocation(time.DateOnly, item.Start.Date, loc)
		if err != nil {
			return domain.Event{}, fmt.Errorf("終日予定の開始日を読めませんでした（ID: %s）: %w", item.Id, err)
		}
		end, err := time.ParseInLocation(time.DateOnly, item.End.Date, loc)
		if err != nil {
			return domain.Event{}, fmt.Errorf("終日予定の終了日を読めませんでした（ID: %s）: %w", item.Id, err)
		}
		e.Start, e.End, e.AllDay = start, end, true
		return e, nil
	}

	start, err := time.Parse(time.RFC3339, item.Start.DateTime)
	if err != nil {
		return domain.Event{}, fmt.Errorf("予定の開始時刻を読めませんでした（ID: %s）: %w", item.Id, err)
	}
	end, err := time.Parse(time.RFC3339, item.End.DateTime)
	if err != nil {
		return domain.Event{}, fmt.Errorf("予定の終了時刻を読めませんでした（ID: %s）: %w", item.Id, err)
	}
	e.Start, e.End = start, end
	return e, nil
}
