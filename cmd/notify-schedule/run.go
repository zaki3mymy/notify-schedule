package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"time"

	"github.com/zaki3mymy/notify-schedule/internal/adapter/googlecalendar"
	"github.com/zaki3mymy/notify-schedule/internal/adapter/nandemooshirase"
	"github.com/zaki3mymy/notify-schedule/internal/config"
	"github.com/zaki3mymy/notify-schedule/internal/domain"
	"github.com/zaki3mymy/notify-schedule/internal/usecase"
)

// 終了コード（Cloud Run Jobs のリトライと監視で使う）
const (
	// exitOK は、成功（予定が0件で通知しなかったときも含む）。
	exitOK = 0
	// exitFailure は、予定の取得や通知に失敗したとき。Cloud Run Jobs は再実行する。
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

	if opts.validateConfig {
		logger.Info("フラグと設定に不備はありません", "date", date.String(), "calendars", len(calendars))
		return exitOK
	}

	source, err := e.newSource(ctx)
	if err != nil {
		logger.Error("予定の取得元を用意できませんでした", "error", err.Error())
		return exitFailure
	}
	notifier, err := e.newNotifier(cfg.NandemoOshirase, opts.dryRun)
	if err != nil {
		logger.Error("通知先を用意できませんでした", "error", err.Error())
		return exitFailure
	}

	logger.Info("予定を取得して通知します", "date", date.String(), "calendars", len(calendars), "dry_run", opts.dryRun)
	result, err := usecase.NewNotifySchedule(source, notifier).Run(ctx, calendars, date)
	if err != nil {
		logger.Error("失敗しました", "date", date.String(), "events", result.Events, "error", err.Error())
		return exitFailure
	}

	if !result.Notified() {
		logger.Info("予定がないので、通知しませんでした", "date", date.String(), "events", result.Events)
		return exitOK
	}
	logger.Info("通知しました", "date", date.String(), "events", result.Events, "messages", result.Messages, "dry_run", opts.dryRun)
	return exitOK
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
