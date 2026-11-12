# notify-schedule

毎日決まった時刻に Google カレンダーから指定日（既定は明日）の予定を取得し、[nandemo-oshirase](https://github.com/zaki3mymy/nandemo-oshirase) で LINE に通知する。

- 複数のカレンダーの予定を1通にまとめて送る。予定がない日は何も送らない。
- Google Cloud の Cloud Run Jobs で動かし、Cloud Scheduler で定時に実行する。
- 環境は dev と prod があり、それぞれの環境の nandemo-oshirase を呼ぶ。

## ディレクトリ構成と依存の向き

スケジュールをメッセージに変換するところ（ドメイン）を中心にし、外側（Google カレンダー、nandemo-oshirase）がドメインに依存する形にする。

```
cmd/notify-schedule/               # 組み立て（設定の読み込み・検証、アダプタの生成、ユースケースの実行）
internal/domain/                   # ドメイン：スケジュール・メッセージの型と、その変換のルール
internal/usecase/                  # ユースケースと、外側に求めるもの（ポート＝interface）
internal/adapter/googlecalendar/   # ポートの実装：Google カレンダーからの予定の取得
internal/adapter/nandemooshirase/  # ポートの実装：nandemo-oshirase への通知
internal/config/                   # 環境変数と設定ファイルの読み込み・検証
```

```
cmd ─┬─▶ adapter ─┐
     └─▶ config  ─┴─▶ usecase ─▶ domain
```

- `domain` は何にも依存しない（標準ライブラリだけ）。
- `usecase` は `domain` だけに依存する。
- `adapter` は `config` を直接読まない。値は `cmd` からコンストラクタで受け取る。
- この向きは、golangci-lint の depguard で確認している（`.golangci.yml`）。

## 開発

### 必要なもの

- Go（バージョンは `go.mod` の `go`。手元の Go が古くても、`GOTOOLCHAIN=auto`（既定）なら自動で取得される）
- make
- [pre-commit](https://pre-commit.com/)

golangci-lint は、`make lint` などを初めて実行したときに、バージョンを固定して `bin/` に入れる（Go のモジュールの依存には入れない）。

### 始め方

```shell
pre-commit install
make build
```

### make のターゲット

| ターゲット | 内容 |
|---|---|
| `make build` | アプリをビルドする（`bin/notify-schedule`） |
| `make test` | テストを実行する（`-race` 付き） |
| `make fmt` | コードを整形する（ファイルを書き換える） |
| `make lint` | 静的解析をする（`go vet`、golangci-lint。整形の崩れと依存の向きも確認する） |
| `make tidy` | `go.mod` と `go.sum` を整理する |
| `make clean` | ビルドの成果物と、`bin/` に入れたツールを消す |

`make` だけを実行すると、ターゲットの一覧が出る。

pre-commit と CI も、これらのターゲットを呼ぶ（ローカルと CI で同じコマンドを使うため）。

### golangci-lint のバージョンを上げる

`Makefile` の `GOLANGCI_LINT_VERSION` を変える。golangci-lint は、自分をビルドした Go より新しい Go のコードを解析できないため、このリポジトリの Go でビルドしている。
