# ディレクトリ構成と依存の向き

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
- この向きは、golangci-lint の depguard で確認している（[.golangci.yml](../.golangci.yml)）。
