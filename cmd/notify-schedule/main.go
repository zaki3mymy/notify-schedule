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

func main() {
	// Cloud Run は止めるときに SIGTERM を送る。受け取ったら、通知を始める前なら中断する
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], env{
		lookupEnv:   os.LookupEnv,
		now:         time.Now,
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		newSource:   newGoogleCalendarSource,
		newNotifier: newNandemoOshiraseNotifier,
	})
	stop()
	os.Exit(code)
}
