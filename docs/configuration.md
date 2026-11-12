# 設定

外から注入される設定は、環境変数と設定ファイル（YAML）の2つ。アプリは起動時にまとめて読み込んで検証し、不備があれば、予定の取得元にも通知先にも触れずに、不備をすべて出して終了する。

## 環境変数

| 環境変数 | 必須 | 内容 |
|---|---|---|
| `NOTIFY_SCHEDULE_CONFIG` | はい | 設定ファイルのパス |

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
