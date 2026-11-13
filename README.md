# notify-schedule

毎日決まった時刻に Google カレンダーから指定日（既定は明日）の予定を取得し、[nandemo-oshirase](https://github.com/zaki3mymy/nandemo-oshirase) で LINE に通知する。

- 複数のカレンダーの予定を1通にまとめて送る。予定がない日は何も送らない。
- Google Cloud の Cloud Run Jobs で動かし、Cloud Scheduler で定時に実行する。
- 環境は dev と prod があり、それぞれの環境の nandemo-oshirase を呼ぶ。

## 通知の例

```
11/12(木) の予定

[終日] 誕生日（家族）
〜06:00 夜行バス（旅行）
09:00-10:00 打ち合わせ（仕事）
22:00〜 夜勤（仕事）
```

- 終日予定（と、前日から翌日まで続く予定）を先に、時刻指定の予定は開始時刻の順に並べる。
- 前日から続く予定は「〜06:00」、翌日まで続く予定は「22:00〜」のように書く。
- 予定の後ろの（）は、カレンダーのラベル。
- 1通が LINE の上限（5000文字）を超えるときは、予定の区切りで複数のメッセージに分ける（2通目からは見出しに「（続き）」を付ける）。

## ドキュメント

| ドキュメント | 内容 |
|---|---|
| [docs/usage.md](./docs/usage.md) | 使い方（フラグ・終了コード・ログ・手元での実行） |
| [docs/configuration.md](./docs/configuration.md) | 設定（環境変数と設定ファイル） |
| [docs/operations.md](./docs/operations.md) | 運用（設定の更新、手動実行、実行の結果、切り戻し） |
| [docs/architecture.md](./docs/architecture.md) | ディレクトリ構成と依存の向き |
| [docs/development.md](./docs/development.md) | 開発環境と、開発で使うコマンド |
