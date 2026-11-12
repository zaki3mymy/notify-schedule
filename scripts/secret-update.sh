#!/usr/bin/env bash
# 設定ファイルを Secret Manager に入れ、古いバージョンを消す（make secret-update から呼ぶ）。
# 無料枠（有効なバージョンが6つまで）に収めるため、有効なバージョンを常に1つにする。
set -euo pipefail

: "${ENV:?ENV（dev か prod）を指定してください}"
: "${GCP_PROJECT_ID:?GCP_PROJECT_ID を指定してください}"
: "${FILE:?FILE（設定ファイルのパス）を指定してください}"

SECRET=notify-schedule-config
JOB=notify-schedule
REGION=asia-northeast1

if [[ ! -f "$FILE" ]]; then
  echo "設定ファイルがありません: $FILE" >&2
  exit 1
fi

if [[ "$ENV" == prod ]]; then
  read -r -p "prod の設定を更新します。よろしいですか？ [y/N] " answer
  [[ "$answer" == y ]] || { echo "中止しました"; exit 1; }
fi

echo "1/4 設定ファイルを検証します"
if ! output=$(NOTIFY_SCHEDULE_CONFIG="$FILE" go run ./cmd/notify-schedule --validate-config 2>&1); then
  echo "$output" >&2
  echo "設定ファイルに不備があるので、Secret Manager には入れません" >&2
  exit 1
fi

echo "2/4 新しいバージョンを追加します"
new=$(gcloud secrets versions add "$SECRET" --project="$GCP_PROJECT_ID" --data-file="$FILE" --format='value(name)')
new=${new##*/}
echo "  バージョン $new を追加しました"

echo "3/4 Cloud Run Job を --dry-run で実行して、実際に動くかを確かめます"
if gcloud run jobs describe "$JOB" --project="$GCP_PROJECT_ID" --region="$REGION" >/dev/null 2>&1; then
  if ! gcloud run jobs execute "$JOB" --project="$GCP_PROJECT_ID" --region="$REGION" --args=--dry-run --wait; then
    echo "--dry-run が失敗したので、追加したバージョン $new を消します（古いバージョンが残ります）" >&2
    gcloud secrets versions destroy "$new" --secret="$SECRET" --project="$GCP_PROJECT_ID" --quiet
    exit 1
  fi
else
  echo "  Cloud Run Job がまだないので、飛ばします"
fi

echo "4/4 古いバージョンを消します"
for version in $(gcloud secrets versions list "$SECRET" --project="$GCP_PROJECT_ID" --filter='state!=DESTROYED' --format='value(name)'); do
  version=${version##*/}
  [[ "$version" == "$new" ]] && continue
  gcloud secrets versions destroy "$version" --secret="$SECRET" --project="$GCP_PROJECT_ID" --quiet
  echo "  バージョン $version を消しました"
done

echo "完了しました（有効なバージョンは $new だけです）"
