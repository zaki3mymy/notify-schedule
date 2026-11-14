// Command notify-schedule は、Google カレンダーから指定日（既定は明日）の予定を取得し、
// nandemo-oshirase で通知する。Cloud Run Jobs で動かす、実行したら終わる CLI。
//
// ここは依存を組み立てる場所（コンポジションルート）で、処理の流れはユースケース（internal/usecase）にある。
// 使い方は docs/usage.md を参照。
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zaki3mymy/notify-schedule/internal/config"
	"github.com/zaki3mymy/notify-schedule/internal/telemetry"
)

// serviceName は、トレースの service.name の既定値（環境変数 OTEL_SERVICE_NAME で上書きできる）。
const serviceName = "notify-schedule"

func main() {
	// Cloud Run は止めるときに SIGTERM を送る。受け取ったら、通知を始める前なら中断する
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// トレースの準備に失敗しても、通知は止めない（ログに出して、トレースなしで続ける）
	logger := newLogger(os.Stdout)
	tel, err := telemetry.Setup(ctx, serviceName, os.LookupEnv)
	if err != nil {
		logger.Warn("トレースを送る準備ができませんでした。トレースなしで続けます", "error", err.Error())
	}

	code := run(ctx, os.Args[1:], env{
		lookupEnv:   os.LookupEnv,
		now:         time.Now,
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		newSource:   newGoogleCalendarSource,
		newNotifier: newNandemoOshiraseNotifier,
		// トレースに出したくない値（カレンダーの ID など）は、設定を読んだあとに run が伏せる
		hideInTraces: func(cfg config.Config) { hideConfigInTraces(tel, cfg) },
	})
	stop()

	// os.Exit は defer を実行しないので、ここで span を送り切る
	if err := tel.Shutdown(); err != nil {
		logger.Warn("トレースを送り切れませんでした", "error", err.Error())
	}

	os.Exit(code)
}
