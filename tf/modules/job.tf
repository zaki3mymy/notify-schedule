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

        # トレースを Grafana Cloud に OTLP で直接送る（Collector は挟まない）
        env {
          name  = "OTEL_SERVICE_NAME"
          value = "notify-schedule"
        }
        env {
          # 環境（dev・prod）。Grafana で環境ごとに絞り込むために付ける。
          # deployment.environment.name は新しいセマンティック規約の名前で、古い名前も一緒に付けておく
          name  = "OTEL_RESOURCE_ATTRIBUTES"
          value = "deployment.environment.name=${var.environment},deployment.environment=${var.environment}"
        }
        env {
          name  = "GRAFANA_CLOUD_OTLP_ENDPOINT"
          value = var.grafana_otlp_endpoint
        }
        env {
          # Authorization ヘッダーの値。Secret Manager から読むので、値は state にも Job の設定にも残らない
          name = "GRAFANA_CLOUD_OTLP_AUTH"
          value_source {
            secret_key_ref {
              secret  = var.grafana_otlp_auth_secret
              version = "latest"
            }
          }
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
  depends_on = [
    google_secret_manager_secret_iam_member.runtime_config,
    google_secret_manager_secret_iam_member.runtime_grafana_otlp_auth,
  ]
}
