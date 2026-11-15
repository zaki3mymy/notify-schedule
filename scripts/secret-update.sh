#!/usr/bin/env bash
# 設定ファイルを Secret Manager に入れ、古いバージョンを消す（make secret-update から呼ぶ）。
# 無料枠（有効なバージョンが6つまで）に収めるため、有効なバージョンを常に1つにする。
#
# Job は設定を versions/latest で読む。latest は、状態にかかわらず最後に作ったバージョンを指す
# （最後のバージョンを消すと、前の有効なバージョンには戻らず、Job が設定を読めなくなる）。
# そのため、最後のバージョンを消すときは、先に有効なバージョンを新しく追加しておく。
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

# 確認に失敗したときに戻す先（いま有効なバージョンのうち、いちばん新しいもの）
previous=$(gcloud secrets versions list "$SECRET" --project="$GCP_PROJECT_ID" --filter='state=ENABLED' \
  --sort-by=~createTime --limit=1 --format='value(name)')
previous=${previous##*/}

echo "2/4 新しいバージョンを追加します"
new=$(gcloud secrets versions add "$SECRET" --project="$GCP_PROJECT_ID" --data-file="$FILE" --format='value(name)')
new=${new##*/}
echo "  バージョン $new を追加しました"

echo "3/4 Cloud Run Job を --dry-run で実行して、実際に動くかを確かめます（読めないカレンダーが1つでもあれば失敗にします）"
if describe=$(gcloud run jobs describe "$JOB" --project="$GCP_PROJECT_ID" --region="$REGION" 2>&1); then
  if ! gcloud run jobs execute "$JOB" --project="$GCP_PROJECT_ID" --region="$REGION" --args=--dry-run,--fail-on-partial --wait; then
    echo "--dry-run が失敗しました" >&2
    if [[ -z "$previous" ]]; then
      # 戻す先がない（初めて入れた）。消すと latest が消したバージョンを指すので、残して止まる
      echo "  前の設定がないので、追加したバージョン $new を残します。設定ファイルを直して、もう一度実行してください" >&2
      exit 1
    fi
    # latest が消したバージョンを指さないよう、前の設定を新しいバージョンとして入れ直してから消す
    restored=$(gcloud secrets versions access "$previous" --secret="$SECRET" --project="$GCP_PROJECT_ID" |
      gcloud secrets versions add "$SECRET" --project="$GCP_PROJECT_ID" --data-file=- --format='value(name)')
    restored=${restored##*/}
    echo "  前の設定に戻します。前の設定（バージョン $previous）を、バージョン $restored として入れ直しました" >&2
    for version in "$new" "$previous"; do
      gcloud secrets versions destroy "$version" --secret="$SECRET" --project="$GCP_PROJECT_ID" --quiet
      echo "  バージョン $version を消しました" >&2
    done
    exit 1
  fi
else
  echo "  対象の Cloud Run Job（$JOB）が見つからないので、--dry-run での確認を飛ばします" >&2
  echo "    ${describe//$'\n'/$'\n'    }" >&2
fi

echo "4/4 古いバージョンを消します"
for version in $(gcloud secrets versions list "$SECRET" --project="$GCP_PROJECT_ID" --filter='state!=DESTROYED' --format='value(name)'); do
  version=${version##*/}
  [[ "$version" == "$new" ]] && continue
  gcloud secrets versions destroy "$version" --secret="$SECRET" --project="$GCP_PROJECT_ID" --quiet
  echo "  バージョン $version を消しました"
done

echo "完了しました（有効なバージョンは $new だけです）"
