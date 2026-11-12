# 実行用のサービスアカウント。Google カレンダーは、このアカウントのメールアドレスに閲覧を共有して読む
resource "google_service_account" "runtime" {
  project      = var.project_id
  account_id   = "notify-schedule"
  display_name = "notify-schedule（実行用）"
}

# 設定ファイル（カレンダー ID、nandemo-oshirase のエンドポイントと API キー）。
# 値は make secret-update で入れる（state や公開リポジトリに値を残さないため、Terraform では器だけを作る）
resource "google_secret_manager_secret" "config" {
  project   = var.project_id
  secret_id = "notify-schedule-config"

  # ロケーションごとに課金されるので、1か所にする
  replication {
    user_managed {
      replicas {
        location = "asia-northeast1"
      }
    }
  }
}

resource "google_secret_manager_secret_iam_member" "runtime_config" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.config.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.runtime.member
}
