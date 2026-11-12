// Package domain は、このアプリの中心となる型とルールを定める。
//
// ある1日の予定（DailySchedule）を、通知するメッセージ（Message）に変換する（ComposeMessages）。
// このパッケージは標準ライブラリ以外の何にも依存しない（Google カレンダーや nandemo-oshirase、
// HTTP、環境変数のことを知らない）。依存の向きは docs/architecture.md を参照。
package domain
