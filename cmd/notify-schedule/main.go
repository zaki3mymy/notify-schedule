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
)

// shutdownTimeout は、終了の前に、溜まっている span を送り切るのを待つ時間の上限。
const shutdownTimeout = 10 * time.Second

func main() {
	// Cloud Run は止めるときに SIGTERM を送る。受け取ったら、通知を始める前なら中断する
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// トレースの準備に失敗しても、通知は止めない（ログに出して、トレースなしで続ける）
	logger := newLogger(os.Stdout)
	// トレースに出したくない値（カレンダーの ID など）は、設定を読んだあとに run が足す
	redact := &redactor{}
	shutdownTracing, err := setupTracing(ctx, os.LookupEnv, redact)
	if err != nil {
		logger.Warn("トレースを送る準備ができませんでした。トレースなしで続けます", "error", err.Error())
	}

	code := run(ctx, os.Args[1:], env{
		lookupEnv:    os.LookupEnv,
		now:          time.Now,
		stdout:       os.Stdout,
		stderr:       os.Stderr,
		newSource:    newGoogleCalendarSource,
		newNotifier:  newNandemoOshiraseNotifier,
		hideInTraces: redact.addConfig,
	})
	stop()

	// os.Exit は defer を実行しないので、ここで span を送り切る。SIGTERM で ctx が終わっていても送れるよう、
	// 新しい context を使う
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	if err := shutdownTracing(sctx); err != nil {
		logger.Warn("トレースを送り切れませんでした", "error", err.Error())
	}
	cancel()

	os.Exit(code)
}
