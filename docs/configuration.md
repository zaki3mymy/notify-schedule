# 設定

外から注入される設定は、環境変数と設定ファイル（YAML）の2つ。アプリは起動時にまとめて読み込んで検証し、不備があれば、予定の取得元にも通知先にも触れずに、不備をすべて出して終了する。

## 環境変数

| 環境変数 | 必須 | 内容 |
|---|---|---|
| `NOTIFY_SCHEDULE_CONFIG` | はい | 設定ファイルのパス |

手元で動かすときは、[.env.example](../.env.example) を `.env` にコピーして使う（`.env` はコミットしない。direnv を使っていれば自動で読み込まれる）。デプロイ先では、Cloud Run Job の設定で渡す。

## 設定ファイル

例は [config.example.yaml](../config.example.yaml)。本物の設定ファイル（`config.yaml`）はコミットしない（`.gitignore` に入れてある）。デプロイ先では Secret Manager に置き、ファイルとしてマウントする。

| 項目 | 必須 | 内容 |
|---|---|---|
| `timezone` | いいえ | 対象日（「明日」など）を決めるタイムゾーン。IANA の名前（例: `Asia/Tokyo`）。省略すると `Asia/Tokyo` |
| `nandemo_oshirase.endpoint` | はい | 通知先の nandemo-oshirase の URL（`https`） |
| `nandemo_oshirase.api_key` | はい | nandemo-oshirase の API キー |
| `calendars` | はい（1件以上） | 予定を取得するカレンダー |
| `calendars[].id` | はい | Google カレンダーの ID（カレンダーの設定の「カレンダーの統合」にある）。重複はエラー |
| `calendars[].label` | はい | 通知のときに予定に添える名前（例: `家族`） |

- 知らないキーはエラーにする（書き間違いに気づけるように）。
- 前後の空白は取り除く。空白だけの値は、空として扱う。
- 不備のエラーには、どの項目が悪いかを書く。ただし、API キー・エンドポイント・カレンダーの ID の値は、エラーやログに出さない（カレンダーの ID は、重複のときも位置で示す）。
- タイムゾーンのデータベースはバイナリに埋め込んでいる（コンテナに `/usr/share/zoneinfo` がなくても動く）。

## デプロイ先の設定ファイル

デプロイ先では、設定ファイルを Secret Manager のシークレット `notify-schedule-config` に置く（器は Terraform で作り、値は Terraform の外で入れる）。値の更新は make で行う（gcloud でログインしておく）。

```shell
make secret-update ENV=dev GCP_PROJECT_ID=<プロジェクト ID> FILE=config.yaml
```

1. `--validate-config` で設定ファイルを検証する。不備があれば、ここで止まる
2. 新しいバージョンを追加する
3. Cloud Run Job があれば `--dry-run` で実行し、実際に動くか（カレンダーを読めるか、API キーが正しいか）を確かめる。失敗したら、追加したバージョンを消して止まる（古いバージョンが残る）
4. 新しいバージョン以外を消す

- Secret Manager の無料枠（請求先アカウント全体で、有効なバージョンが6つまで）に収めるため、有効なバージョンを常に1つにする。無効（disabled）にしただけのバージョンも数えられるので、消す（destroy）。
- `ENV=prod` のときは、実行の前に確認を求める。

カレンダーは、実行用のサービスアカウントに「予定の表示（すべての予定の詳細）」で共有する。メールアドレスは Terraform の output（`runtime_service_account_email`）にある。
