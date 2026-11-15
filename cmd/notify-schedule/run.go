package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/zaki3mymy/notify-schedule/internal/adapter/googlecalendar"
	"github.com/zaki3mymy/notify-schedule/internal/adapter/nandemooshirase"
	"github.com/zaki3mymy/notify-schedule/internal/config"
	"github.com/zaki3mymy/notify-schedule/internal/domain"
	"github.com/zaki3mymy/notify-schedule/internal/usecase"
)

// 終了コード（Cloud Run Jobs のリトライと監視で使う）
const (
	// exitOK は、成功（予定が0件で通知しなかったとき、一部のカレンダーの予定を取得できずに、取得できた分を通知したときも含む）。
	exitOK = 0
	// exitFailure は、予定の取得（すべてのカレンダー）や通知に失敗したとき。Cloud Run Jobs は再実行する。
	// --fail-on-partial なら、一部のカレンダーの予定を取得できなかったときも exitFailure にする。
	exitFailure = 1
	// exitInvalid は、フラグや設定に不備があるとき（外部の API には触れていない）。
	exitInvalid = 2
)

// env は、run が外から受け取るもの（テストで差し替える）。
type env struct {
	lookupEnv config.LookupEnv
	now       func() time.Time
	stdout    io.Writer
	stderr    io.Writer
	// newSource・newNotifier は、検証が済んだあとにだけ呼ぶ（設定に不備があれば外部の API に触れない）
	newSource   func(ctx context.Context) (usecase.ScheduleSource, error)
	newNotifier func(cfg config.NandemoOshirase, dryRun bool) (usecase.Notifier, error)
	// hideInTraces は、設定のうちトレースに出したくない値（カレンダーの ID など）を伏せるようにする。
	// 設定を読めたら、外部の API を呼ぶ前に呼ぶ
	hideInTraces func(cfg config.Config)
}

// run は、フラグと設定を検証してから、ユースケースを実行する。戻り値は終了コード。
//
//  1. フラグを読み、検証する
//  2. 環境変数と設定ファイルを読み、検証する
//  3. フラグと設定を突き合わせる（対象日のタイムゾーン、--calendar の指定）
//  4. ここまでに1つでも不備があれば、外部の API に触れずに、不備をすべてログに出して終わる
//  5. --validate-config なら、ここで終わる
//  6. アダプタを作り、ユースケースを実行する
func run(ctx context.Context, args []string, e env) int {
	logger := newLogger(e.stdout)

	opts, flagErr := parseFlags(args, e.stderr)
	if errors.Is(flagErr, flag.ErrHelp) {
		return exitOK
	}

	var problems []error
	if flagErr != nil {
		problems = append(problems, flagErr)
	}

	cfg, cfgErr := config.Load(e.lookupEnv)
	if cfgErr != nil {
		problems = append(problems, cfgErr)
	}

	// フラグと設定の突き合わせ。1回の実行で不備がすべてわかるよう、できるものはすべて確かめる
	var (
		date      domain.Date
		calendars []domain.Calendar
	)
	if cfgErr == nil {
		// --calendar の絞り込みは、設定が読めていれば確かめられる（ほかのフラグに不備があっても）
		var err error
		if calendars, err = opts.selectCalendars(cfg.Calendars); err != nil {
			problems = append(problems, err)
		}
		// 対象日は、フラグに不備がないときだけ決める（--date と --offset-days のどちらを使うかが決まらないため）
		if flagErr == nil {
			if date, err = opts.targetDate(e.now(), cfg.Location); err != nil {
				problems = append(problems, err)
			}
		}
	}

	if len(problems) > 0 {
		logger.Error("フラグか設定に不備があります。予定の取得も通知もせずに終了します", "problems", problemList(problems))
		return exitInvalid
	}

	e.hideInTraces(cfg)

	if opts.validateConfig {
		logger.Info("フラグと設定に不備はありません", "date", date.String(), "calendars", len(calendars))
		return exitOK
	}

	// 予定の取得と通知（外部の API を呼ぶところ）を1つの span にする。
	// アダプタの HTTP の呼び出しの span は、ctx を通じてこの span の子になる
	ctx, span := otel.Tracer(tracerName).Start(ctx, "notify-schedule", trace.WithAttributes(
		attribute.String("schedule.date", date.String()),
		attribute.Int("schedule.calendars", len(calendars)),
		attribute.Bool("notify.dry_run", opts.dryRun),
	))
	defer span.End()

	source, err := e.newSource(ctx)
	if err != nil {
		logger.Error("予定の取得元を用意できませんでした", "error", err.Error())
		recordError(span, err)
		return exitFailure
	}
	notifier, err := e.newNotifier(cfg.NandemoOshirase, opts.dryRun)
	if err != nil {
		logger.Error("通知先を用意できませんでした", "error", err.Error())
		recordError(span, err)
		return exitFailure
	}

	logger.Info("予定を取得して通知します", "date", date.String(), "calendars", len(calendars), "dry_run", opts.dryRun)
	result, err := usecase.NewNotifySchedule(source, notifier).Run(ctx, calendars, date)
	span.SetAttributes(
		attribute.Int("schedule.events", result.Events),
		attribute.Int("schedule.failed_calendars", len(result.Failures)),
		attribute.Int("notify.messages", result.Messages),
	)
	// 一部のカレンダーの予定を取得できなかったときは、取得できた分は通知して、終了コードは 0 にする
	// （1 にすると Job が再実行し、二重に送られるため）。そのかわり、span を Error にして、トレースで見つけられるようにする
	if result.Partial() {
		labels := result.FailedLabels()
		logger.Warn("一部のカレンダーの予定を取得できませんでした。取得できた分を通知します",
			"date", date.String(), "failed_calendars", labels, "errors", failureList(result.Failures))
		span.SetAttributes(attribute.StringSlice("schedule.failed_calendar_labels", labels))
		span.SetStatus(codes.Error, fmt.Sprintf("%d 件のカレンダーの予定を取得できませんでした", len(labels)))
	}
	if err != nil {
		logger.Error("失敗しました", "date", date.String(), "events", result.Events, "error", err.Error())
		recordError(span, err)
		return exitFailure
	}

	// 設定の確認（make secret-update・CD のスモークテスト）で、読めないカレンダーに気づけるよう、
	// --fail-on-partial なら一部の取得の失敗も失敗にする（--dry-run と一緒に使い、再実行で二重に送られないようにする）
	if result.Partial() && opts.failOnPartial {
		logger.Error("--fail-on-partial なので、一部のカレンダーの予定を取得できなかったことを失敗にします",
			"date", date.String(), "failed_calendars", result.FailedLabels())
		return exitFailure
	}

	if !result.Notified() {
		logger.Info("予定がないので、通知しませんでした", "date", date.String(), "events", result.Events)
		return exitOK
	}
	logger.Info("通知しました", "date", date.String(), "events", result.Events, "messages", result.Messages,
		"failed_calendars", len(result.Failures), "dry_run", opts.dryRun)
	return exitOK
}

// tracerName は、run が作る span の計装の名前。
const tracerName = "github.com/zaki3mymy/notify-schedule"

// トレースで、伏せた値の代わりに入れる文字列。
const (
	redactedCalendarID = "{calendarId}"
	redactedNandemo    = "{nandemo-oshirase}"
	redactedSecret     = "{redacted}"
)

// hider は、トレースに出したくない値を伏せる（telemetry.Provider。テストで差し替える）。
type hider interface {
	Hide(placeholder string, values ...string)
}

// hideConfigInTraces は、設定のうち、トレースに出したくない値を伏せるようにする。
//
//   - カレンダーの ID（メールアドレスのことがある）。HTTP の span の URL やエラーの本文に入る
//   - nandemo-oshirase のエンドポイントのホスト（API Gateway の URL を公開したくないため）
//   - nandemo-oshirase の API キー（span には入らないはずだが、念のため）
func hideConfigInTraces(h hider, cfg config.Config) {
	for _, cal := range cfg.Calendars {
		h.Hide(redactedCalendarID, cal.ID)
	}
	if u, err := url.Parse(cfg.NandemoOshirase.Endpoint); err == nil {
		h.Hide(redactedNandemo, u.Host, u.Hostname())
	}
	h.Hide(redactedSecret, cfg.NandemoOshirase.APIKey.Value())
}

// recordError は、span を失敗にし、エラーを記録する。
func recordError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// problemList は、不備をログに出せる形（文字列の一覧）にする。errors.Join でまとめたものは、1つずつに分ける。
func problemList(errs []error) []string {
	var list []string
	var add func(err error)
	add = func(err error) {
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			for _, e := range joined.Unwrap() {
				add(e)
			}
			return
		}
		list = append(list, err.Error())
	}
	for _, err := range errs {
		add(err)
	}
	return list
}

// failureList は、取得できなかったカレンダーをログに出せる形（文字列の一覧）にする。
func failureList(failures []usecase.CalendarFailure) []string {
	list := make([]string, 0, len(failures))
	for _, f := range failures {
		list = append(list, f.Error())
	}
	return list
}

// newGoogleCalendarSource は、Google カレンダーの ScheduleSource を作る（認証は ADC）。
func newGoogleCalendarSource(ctx context.Context) (usecase.ScheduleSource, error) {
	return googlecalendar.New(ctx)
}

// newNandemoOshiraseNotifier は、nandemo-oshirase の Notifier を作る。
func newNandemoOshiraseNotifier(cfg config.NandemoOshirase, dryRun bool) (usecase.Notifier, error) {
	return nandemooshirase.New(nandemooshirase.Config{
		Endpoint: cfg.Endpoint,
		APIKey:   cfg.APIKey.Value(),
		DryRun:   dryRun,
	})
}

// newLogger は、Cloud Logging で読める JSON のログを w に出すロガーを作る。
// Cloud Logging は severity と message のキーを読むので、slog の level・msg を言い換える。
func newLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return a
			}
			switch a.Key {
			case slog.LevelKey:
				return slog.String("severity", severity(a.Value.Any().(slog.Level)))
			case slog.MessageKey:
				return slog.Attr{Key: "message", Value: a.Value}
			}
			return a
		},
	}))
}

// severity は、slog のレベルを Cloud Logging の severity にする。
func severity(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARNING"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}
