// Package usecase は、ユースケース（処理の流れ）と、外側に求めるもの（ポート）を定める。
//
// ポートは interface で、外側（adapter）が実装する。このパッケージは domain だけに依存し、
// 予定の取得元や通知先、HTTP、環境変数のことを知らない。依存の向きは docs/architecture.md を参照。
package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/zaki3mymy/notify-schedule/internal/domain"
)

// ScheduleSource は、予定の取得元。指定したカレンダーから、指定した日の予定を取得する。
//
// 取得できなかったカレンダーがあっても、ほかのカレンダーは取得し、取得できなかったカレンダーを
// FetchResult.Failures で返す（error は返さない）。error は、どのカレンダーも試せなかったときに返す。
type ScheduleSource interface {
	Fetch(ctx context.Context, cals []domain.Calendar, d domain.Date) (FetchResult, error)
}

// FetchResult は、ScheduleSource.Fetch の結果。
type FetchResult struct {
	// Schedule は、取得できたカレンダーの予定（Schedule.FailedCalendars は見ない。Run が Failures から入れる）。
	Schedule domain.DailySchedule
	// Failures は、予定を取得できなかったカレンダー。
	Failures []CalendarFailure
}

// CalendarFailure は、予定を取得できなかったカレンダーと、その理由。
type CalendarFailure struct {
	Calendar domain.Calendar
	Err      error
}

// Error は、カレンダーのラベルと理由を返す（カレンダーの ID は含めない。メールアドレスのことがあるため）。
func (f CalendarFailure) Error() string {
	return fmt.Sprintf("カレンダー「%s」の予定を取得できませんでした: %v", f.Calendar.Label, f.Err)
}

// Unwrap は、理由のエラーを返す。
func (f CalendarFailure) Unwrap() error { return f.Err }

// Notifier は、通知先。メッセージをまとめて送る。
type Notifier interface {
	Notify(ctx context.Context, msgs []domain.Message) error
}

// NotifySchedule は、ある日の予定を取得して、通知するユースケース。
type NotifySchedule struct {
	source   ScheduleSource
	notifier Notifier
}

// NewNotifySchedule は、予定の取得元と通知先から NotifySchedule を作る。
func NewNotifySchedule(source ScheduleSource, notifier Notifier) *NotifySchedule {
	return &NotifySchedule{source: source, notifier: notifier}
}

// Result は、Run の結果（呼び出し側がログに出すため）。
type Result struct {
	// Events は、取得した予定の数。
	Events int
	// Messages は、送ったメッセージの数。予定がなく送らなかったときは 0。
	Messages int
	// Failures は、予定を取得できなかったカレンダー（一部だけ取得できなかったとき）。
	// すべて取得できなかったときは、Run がエラーを返す（Failures は空）。
	Failures []CalendarFailure
}

// Notified は、通知を送ったかを返す。
func (r Result) Notified() bool { return r.Messages > 0 }

// Partial は、一部のカレンダーの予定を取得できなかったかを返す。
func (r Result) Partial() bool { return len(r.Failures) > 0 }

// FailedLabels は、予定を取得できなかったカレンダーのラベルを返す。
func (r Result) FailedLabels() []string {
	labels := make([]string, 0, len(r.Failures))
	for _, f := range r.Failures {
		labels = append(labels, f.Calendar.Label)
	}
	return labels
}

// Run は、cals から d の予定を取得し、メッセージに変換して通知する。
//
// 予定が1件もないときは、通知せずに正常に終わる。
// 一部のカレンダーの予定を取得できなかったときは、取得できた分を、取得できなかったカレンダーの知らせと一緒に
// 通知し、エラーは返さない（Result.Failures で返す）。エラーにすると、再実行で二重に送られるため。
// ほかのカレンダーに予定がなくても、知らせだけを通知する。
// すべてのカレンダーの予定を取得できなかったときは、通知せずにエラーを返す（再実行で取得できることを期待する）。
// 通知は最後に1回だけ行う。通知より前に失敗したときは、通知しない（再実行しても二重に送られないようにするため）。
func (u *NotifySchedule) Run(ctx context.Context, cals []domain.Calendar, d domain.Date) (Result, error) {
	if len(cals) == 0 {
		return Result{}, errors.New("予定を取得するカレンダーが指定されていません")
	}

	fetched, err := u.source.Fetch(ctx, cals, d)
	if err != nil {
		return Result{}, fmt.Errorf("%s の予定の取得に失敗しました: %w", d, err)
	}
	if len(fetched.Failures) >= len(cals) {
		errs := make([]error, 0, len(fetched.Failures))
		for _, f := range fetched.Failures {
			errs = append(errs, f)
		}
		return Result{}, fmt.Errorf("%s の予定の取得に失敗しました（すべてのカレンダー）: %w", d, errors.Join(errs...))
	}

	schedule := fetched.Schedule
	schedule.Date = d
	schedule.FailedCalendars = nil
	for _, f := range fetched.Failures {
		schedule.FailedCalendars = append(schedule.FailedCalendars, f.Calendar)
	}
	result := Result{Events: len(schedule.Events), Failures: fetched.Failures}

	messages := domain.ComposeMessages(schedule)
	if len(messages) == 0 {
		return result, nil
	}

	// 取得に時間がかかってキャンセルやタイムアウトになったときは、通知を始めない
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("通知の前に中断されました: %w", err)
	}
	if err := u.notifier.Notify(ctx, messages); err != nil {
		return result, fmt.Errorf("%s の予定の通知に失敗しました: %w", d, err)
	}
	result.Messages = len(messages)
	return result, nil
}
