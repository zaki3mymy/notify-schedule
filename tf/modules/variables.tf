variable "project_id" {
  description = "デプロイ先のプロジェクト"
  type        = string
}

variable "image" {
  description = "Cloud Run Job で動かすイメージ（Artifact Registry のコミットの SHA のタグ）"
  type        = string
}

variable "schedule" {
  description = "Job を実行する時刻（cron の形式。Asia/Tokyo）"
  type        = string
}

variable "scheduler_paused" {
  description = "Scheduler を一時停止にするか（dev は手で実行するので true）"
  type        = bool
}
