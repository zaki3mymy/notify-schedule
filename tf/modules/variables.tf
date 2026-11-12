variable "project_id" {
  description = "デプロイ先のプロジェクト"
  type        = string
}

variable "image" {
  description = "Cloud Run Job で動かすイメージ（Artifact Registry のコミットの SHA のタグ）"
  type        = string
}

variable "schedule" {
  description = "Job を実行する時刻（cron の形式。Asia/Tokyo）。null なら Cloud Scheduler を作らない（手で実行する）"
  type        = string
  default     = null
}
