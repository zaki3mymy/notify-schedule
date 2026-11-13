# アプリを動かす Cloud Run Job。Cloud Scheduler から1日1回実行する
resource "google_cloud_run_v2_job" "app" {
  project  = var.project_id
  name     = "notify-schedule"
  location = "asia-northeast1"

  template {
    task_count = 1

    template {
      service_account = google_service_account.runtime.email
      # 通知はタスクの最後に1回だけ送るので、再実行で二重に通知されるのは、通知のあとに落ちたときだけ
      max_retries = 1
      timeout     = "300s"

      containers {
        image = var.image

        env {
          name  = "NOTIFY_SCHEDULE_CONFIG"
          value = "/secrets/config.yaml"
        }

        resources {
          limits = {
            cpu    = "1"
            memory = "512Mi"
          }
        }

        volume_mounts {
          name       = "config"
          mount_path = "/secrets"
        }
      }

      volumes {
        name = "config"
        secret {
          secret = google_secret_manager_secret.config.secret_id
          items {
            version = "latest"
            path    = "config.yaml"
          }
        }
      }
    }
  }

  # シークレットを読む権限ができてから Job を作る
  depends_on = [google_secret_manager_secret_iam_member.runtime_config]
}
