# 毎日決まった時刻に Cloud Run Job を実行する。対象日やカレンダーはアプリの中で決めるので、ここでは実行するだけ
resource "google_service_account" "scheduler" {
  project      = var.project_id
  account_id   = "notify-schedule-scheduler"
  display_name = "notify-schedule（Scheduler 用）"
}

# この Job の実行だけを許す（プロジェクトのロールは付けない）
resource "google_cloud_run_v2_job_iam_member" "scheduler_invoker" {
  project  = var.project_id
  location = google_cloud_run_v2_job.app.location
  name     = google_cloud_run_v2_job.app.name
  role     = "roles/run.invoker"
  member   = google_service_account.scheduler.member
}

resource "google_cloud_scheduler_job" "app" {
  project   = var.project_id
  region    = "asia-northeast1"
  name      = "notify-schedule"
  schedule  = var.schedule
  time_zone = "Asia/Tokyo"
  paused    = var.scheduler_paused

  # 実行の要求が失敗したときは、再送しない（アプリの失敗の再実行は、Job の max_retries に任せる）
  retry_config {
    retry_count = 0
  }

  http_target {
    http_method = "POST"
    uri         = "https://run.googleapis.com/v2/projects/${var.project_id}/locations/${google_cloud_run_v2_job.app.location}/jobs/${google_cloud_run_v2_job.app.name}:run"

    oauth_token {
      service_account_email = google_service_account.scheduler.email
      scope                 = "https://www.googleapis.com/auth/cloud-platform"
    }
  }

  depends_on = [google_cloud_run_v2_job_iam_member.scheduler_invoker]
}
