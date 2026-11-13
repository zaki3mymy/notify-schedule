# 使い方

`notify-schedule` は、実行したら終わる CLI。指定日（既定は明日）の予定を Google カレンダーから取得し、nandemo-oshirase で通知する。予定がない日は何も送らない。デプロイ先では Cloud Run Jobs で動かし、Cloud Scheduler で定時に実行する。

設定（環境変数と設定ファイル）は [configuration.md](./configuration.md) を参照。

## フラグ

| フラグ | 既定 | 内容 |
|---|---|---|
| `--date YYYY-MM-DD` | なし | 対象日。手で再送するときなどに使う。`--offset-days` とは同時に指定できない |
| `--offset-days N` | `1` | 設定のタイムゾーンでの「今日」から N 日後を対象日にする（`0` で今日） |
| `--calendar ID_OR_LABEL` | なし（すべて） | 予定を取得するカレンダーを、設定の ID かラベルで絞り込む。何回でも指定できる |
| `--dry-run` | なし | nandemo-oshirase に `dry_run` で送る（LINE には送らない。nandemo-oshirase はトークンとメッセージの形式を確かめる） |
| `--validate-config` | なし | フラグと設定の検証だけを行って終わる。外部の API には触れないので、認証情報がなくても動く |
| `-h` | | 使い方を出す |

## 起動の順番

1. フラグと、環境変数・設定ファイルを読み、検証する。フラグと設定を突き合わせる（`--calendar` が設定にあるか、対象日）
2. **1つでも不備があれば、Google カレンダーにも nandemo-oshirase にも触れずに、不備をすべてログに出して終わる**
3. `--validate-config` なら、ここで終わる
4. 予定を取得し、メッセージにまとめて、通知する（予定がなければ通知しない）

## 終了コード

| コード | 意味 |
|---|---|
| `0` | 成功（予定がなく通知しなかったときも含む）。`-h` と、不備のない `--validate-config` も `0` |
| `1` | 予定の取得や通知に失敗した（Cloud Run Jobs は再実行する） |
| `2` | フラグか設定に不備がある（外部の API には触れていない） |

通知は最後に1回だけ行う。通知より前に失敗したときは通知しないので、再実行しても二重には送られない。

## ログ

標準出力に、1行1件の JSON で出す。Cloud Logging が読めるよう、レベルは `severity`（`INFO`・`ERROR` など）、本文は `message` のキーにしている。

```json
{"time":"...","severity":"INFO","message":"通知しました","date":"2026-11-13","events":3,"messages":1,"dry_run":false}
{"time":"...","severity":"INFO","message":"予定がないので、通知しませんでした","date":"2026-11-13","events":0}
{"time":"...","severity":"ERROR","message":"フラグか設定に不備があります。予定の取得も通知もせずに終了します","problems":["calendars[0].label: 必須です"]}
```

API キーやカレンダーの ID は、ログに出さない。

## デプロイ先での実行

prod では、Cloud Scheduler が毎日 21:00（Asia/Tokyo）に Cloud Run Job `notify-schedule` を実行する（時刻は `tf/envs/prod/main.tf` の `schedule`）。dev には Scheduler を作らないので、使うときは手で実行する（Cloud Scheduler の無料枠は、一時停止のものも含めて請求先アカウントで3つまでのため）。

```shell
gcloud run jobs execute notify-schedule --project=<プロジェクト ID> --region=asia-northeast1 --wait

# フラグは --args にカンマ区切りで渡す
gcloud run jobs execute notify-schedule --project=<プロジェクト ID> --region=asia-northeast1 --wait \
  --args=--dry-run,--date=2026-12-24
```

- Scheduler は、実行の要求が失敗しても再送しない。アプリが失敗したとき（終了コード `1`）の再実行は、Job のリトライ（1回）に任せる。
- Scheduler 用のサービスアカウント（`notify-schedule-scheduler`）は、この Job の実行（`roles/run.invoker`）しかできない。

## 手元で動かす

開発環境（[development.md](./development.md)）に入ってから実行する。

```shell
cp config.example.yaml config.yaml   # 値を書き換える（config.yaml はコミットしない）
cp .env.example .env                 # 環境変数（.env はコミットしない）

# direnv を使っていれば、.env はこのディレクトリに入ったときに読み込まれる（.envrc の dotenv_if_exists）。
# direnv を使わないときは、自分で読み込む
set -a; source .env; set +a

# 設定の確認だけ（認証情報は要らない）
go run ./cmd/notify-schedule --validate-config
```

予定を取得するには、Google の認証情報（ADC）が要る。デプロイ先と同じく、カレンダーを共有した実行用のサービスアカウントの権限で動かすには、そのサービスアカウントになりすまして ADC を作る（自分のアカウントに、そのサービスアカウントの「サービス アカウント トークン作成者」のロールが要る）。

```shell
gcloud auth application-default login \
  --impersonate-service-account=<実行用のサービスアカウントのメールアドレス>

# LINE には送らずに、取得から通知の直前までを確かめる
go run ./cmd/notify-schedule --dry-run

# 日付やカレンダーを指定する
go run ./cmd/notify-schedule --dry-run --date 2026-12-24 --calendar 家族
```

自分のアカウントの権限で読む場合は、`gcloud auth application-default login --scopes=https://www.googleapis.com/auth/cloud-platform,https://www.googleapis.com/auth/calendar.readonly` で ADC を作る（gcloud の既定の OAuth クライアントでカレンダーのスコープを許可できないときは、自分の OAuth クライアントを `--client-id-file` で指定する）。

## コンテナで動かす

```shell
make image   # notify-schedule:local をビルドする（podman があれば podman、なければ docker）

docker run --rm --user "$(id -u)" \
  -v "$PWD/config.yaml:/config/config.yaml:ro" \
  -e NOTIFY_SCHEDULE_CONFIG=/config/config.yaml \
  -v "$HOME/.config/gcloud/application_default_credentials.json:/adc.json:ro" \
  -e GOOGLE_APPLICATION_CREDENTIALS=/adc.json \
  notify-schedule:local --dry-run
```

- イメージは distroless（`static-debian13:nonroot`）で、ユーザー 65532 で動く。マウントするファイルが自分だけ読める権限（`0600`）のときは、docker では `--user "$(id -u)"`、podman では `--userns=keep-id` を付けて、自分のユーザーで動かす。
- タイムゾーンのデータはバイナリに埋め込んでいるので、イメージにはなくてよい。
