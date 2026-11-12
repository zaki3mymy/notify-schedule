# notify-schedule

毎日決まった時刻に Google カレンダーから指定日（既定は明日）の予定を取得し、[nandemo-oshirase](https://github.com/zaki3mymy/nandemo-oshirase) で LINE に通知する。

- 複数のカレンダーの予定を1通にまとめて送る。予定がない日は何も送らない。
- Google Cloud の Cloud Run Jobs で動かし、Cloud Scheduler で定時に実行する。
- 環境は dev と prod があり、それぞれの環境の nandemo-oshirase を呼ぶ。

## ドキュメント

| ドキュメント | 内容 |
|---|---|
| [docs/architecture.md](./docs/architecture.md) | ディレクトリ構成と依存の向き |
| [docs/development.md](./docs/development.md) | 開発環境と、開発で使うコマンド |
