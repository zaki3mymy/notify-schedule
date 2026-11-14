# ディレクトリ構成と依存の向き

スケジュールをメッセージに変換するところ（ドメイン）を中心にし、外側（Google カレンダー、nandemo-oshirase）がドメインに依存する形にする。

```
cmd/notify-schedule/               # 組み立て（設定の読み込み・検証、アダプタの生成、ユースケースの実行）
internal/domain/                   # ドメイン：スケジュール・メッセージの型と、その変換のルール
internal/usecase/                  # ユースケースと、外側に求めるもの（ポート＝interface）
internal/adapter/googlecalendar/   # ポートの実装：Google カレンダーからの予定の取得
internal/adapter/nandemooshirase/  # ポートの実装：nandemo-oshirase への通知
internal/config/                   # 環境変数と設定ファイルの読み込み・検証
internal/telemetry/                # トレースの仕組み：Grafana Cloud への送信と、送る前に値を伏せる処理
```

```
cmd ─┬─▶ adapter ─┐
     ├─▶ config  ─┴─▶ usecase ─▶ domain
     └─▶ telemetry
```

- `domain` は何にも依存しない（標準ライブラリだけ）。
- `usecase` は `domain` だけに依存する。
- `adapter` は `config` を直接読まない。値は `cmd` からコンストラクタで受け取る。
- `telemetry` は、アプリの中身に依存しない。何を伏せるかは `cmd` が決めて渡す。
- この向きは、golangci-lint の depguard で確認している（[.golangci.yml](../.golangci.yml)）。

## トレースの送り方

トレースは OpenTelemetry で計装し、アプリ（[internal/telemetry](../internal/telemetry/telemetry.go)）から Grafana Cloud の OTLP のエンドポイントに直接送る。Collector は挟まない。

Collector をサイドカーとして挟まない理由：

- **Cloud Run Jobs では、送り切る前に止められるおそれがある**。Job はメインのコンテナが終わると終わるため、サイドカーの Collector が溜めたトレースを送り切る前に止められないかを気にする必要がある。アプリから直接送れば、終了の前に `TracerProvider.Shutdown` で送り切るまで待てる（最大10秒）。
- **Collector の機能が活きない**。1日1回・数秒で終わる Job なので、Collector の溜める・まとめる・再送する・加工するといった機能の効果が小さい。そのわりに、コンテナが増え、起動の順番（`depends_on`・起動の確認）やイメージの用意が要る。
- **アプリのコードはほぼ変わらない**。Go では、Collector を挟んでも、SDK の初期化や span はアプリに書く必要がある。違うのは送り先と認証ヘッダーだけなので、あとで Collector を挟みたくなっても、`telemetry.Setup` の数行を変えれば済む。

そのほか：

- 送信先と認証ヘッダーの値は、環境変数で受け取る（[configuration.md](./configuration.md#環境変数)）。どちらもないときは送らない。
- nandemo-oshirase には `traceparent` ヘッダーでトレースの ID を引き継ぐ（W3C Trace Context）。nandemo-oshirase 側がそれを引き継げば、nandemo-oshirase の span も同じトレースにつながる。
- Google カレンダーの API のクライアントは、内部で `otelhttp` を使っているので、TracerProvider を設定するだけで HTTP の呼び出しの span が出る。
- トレースに出したくない値（カレンダーの ID、nandemo-oshirase のエンドポイントのホスト、API キー）は、送る直前に、span の名前・属性・イベント・status の文字列からまとめて置き換える（[internal/telemetry/redact.go](../internal/telemetry/redact.go)）。HTTP の span は `url.full` にリクエストの URL をそのまま入れ、エラーの本文にも URL が入ることがあるので、入る場所を1つずつ追うのではなく、送る前に置き換える。
- 何を伏せるかは、設定を知っている `cmd` が決める（`run.go` の `hideConfigInTraces`）。伏せる値は設定を読んだあとにわかるので、`run` が設定を読んだところで `telemetry.Provider.Hide` に渡す。値が URL に入ると形が変わる（メールアドレスの `@` が `%40` になる）ことは `telemetry` が扱う。
