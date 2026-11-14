# 運用

デプロイ先（dev・prod）での操作をまとめる。どれも gcloud でログインしてから行う。以下の `<プロジェクト ID>` は、操作する環境のプロジェクト。

## 環境

| | dev | prod |
|---|---|---|
| デプロイ | `develop` へのマージで CD が行う | `main` へのマージ（リリース）で CD が行う |
| 定時の実行 | なし（手で実行する） | Cloud Scheduler が毎日 21:00（Asia/Tokyo） |
| 通知先 | dev の nandemo-oshirase | prod の nandemo-oshirase |

リリースの流れは [development.md](./development.md#リリース) を参照。

## 設定の更新

カレンダーの追加や API キーの変更は、設定ファイルを直して `make secret-update` で入れる（詳しくは [configuration.md](./configuration.md#デプロイ先の設定ファイル)）。新しい設定で `--dry-run` が通らなければ、古い設定のまま残る。

```shell
make secret-update ENV=prod GCP_PROJECT_ID=<プロジェクト ID> FILE=config.yaml
```

- 古いバージョンは消すので、Secret Manager から前の設定には戻せない。設定ファイルは手元に残しておく（リポジトリには入れない）。
- カレンダーを足したときは、実行用のサービスアカウントにそのカレンダーを共有する。

## 手動実行

```shell
# LINE には送らずに確かめる
gcloud run jobs execute notify-schedule --project=<プロジェクト ID> --region=asia-northeast1 --wait --args=--dry-run

# 通知を送り直す（日付を指定する）
gcloud run jobs execute notify-schedule --project=<プロジェクト ID> --region=asia-northeast1 --wait --args=--date=2026-12-24
```

フラグは [usage.md](./usage.md#フラグ) を参照。

## 実行の結果を見る

```shell
# 実行の一覧（成功・失敗）
gcloud run jobs executions list --job=notify-schedule --project=<プロジェクト ID> --region=asia-northeast1

# アプリのログ（直近1日）
gcloud logging read 'resource.type="cloud_run_job" AND resource.labels.job_name="notify-schedule"' \
  --project=<プロジェクト ID> --freshness=1d --format='value(timestamp, severity, jsonPayload.message)'
```

ログの形は [usage.md](./usage.md#ログ) を参照。終了コードが `1`（取得や通知の失敗）のときは、Job が1回だけ再実行する。

### トレース

実行ごとのトレースは Grafana Cloud で見る。Grafana の Explore で、データソース Tempo を選び、`service.name` が `notify-schedule` のものを探す。環境は `deployment.environment.name`（`dev`・`prod`）で絞り込める。

1回の実行は、次の span になる。失敗したところの span は、status が Error になる。

```
notify-schedule        1回の実行（schedule.date・schedule.calendars・schedule.events・notify.messages・notify.dry_run）
├─ fetch_schedule      予定の取得（schedule.calendars・schedule.events）
│  └─ fetch_calendar   カレンダーごとの取得（calendar.label・calendar.events）
│     └─ HTTP GET      Google Calendar API の呼び出し
└─ notify              通知（notify.messages・notify.dry_run）。予定がなく通知しないときは出ない
   └─ POST /notify     nandemo-oshirase の呼び出し（この下に nandemo-oshirase の span がつく）
```

カレンダーの ID は span に出さない（ラベルで見分ける）。HTTP の span の URL（`url.full`）やエラーの本文に入るカレンダーの ID は `{calendarId}`、nandemo-oshirase のエンドポイントのホストは `{nandemo-oshirase}` に置き換えて送っている。

トレースの送り方は [architecture.md](./architecture.md#トレースの送り方) を参照。

## 切り戻し

### アプリを前のリリースに戻す

ふつうは、問題の PR を develop で revert し、リリースする（いつもの流れで直す）。

急いで戻すときは、Job のイメージを前のリリースのものに差し替える。prod のイメージには、コミットの SHA のタグに加えて、リリースのタグ（`v1.2.3` など）も付いている。

```shell
gcloud run jobs update notify-schedule --project=<プロジェクト ID> --region=asia-northeast1 \
  --image=asia-northeast1-docker.pkg.dev/<プロジェクト ID>/apps/notify-schedule:v1.2.3   # 戻したいリリース
```

- これは Terraform の外での変更なので、次の CD の apply で `main` のイメージに戻る。差し替えたあとは、revert のリリースまで `main` にマージしない。
- Artifact Registry に残るイメージは、新しいものから10個まで。それより前のリリースには、この方法では戻せない。

### 通知を止める

prod の定時の実行を止めるときは、Scheduler を一時停止する。

```shell
gcloud scheduler jobs pause notify-schedule --project=<プロジェクト ID> --location=asia-northeast1
gcloud scheduler jobs resume notify-schedule --project=<プロジェクト ID> --location=asia-northeast1   # 再開
```

これも Terraform の外での変更なので、止めている間に CD が動いたら、Scheduler の状態を確かめる。
