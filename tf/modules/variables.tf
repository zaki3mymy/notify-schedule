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

variable "environment" {
  description = "環境の名前（dev・prod）。トレースのリソース属性 deployment.environment.name に付ける"
  type        = string

  validation {
    condition     = contains(["dev", "prod"], var.environment)
    error_message = "environment は dev か prod にしてください。"
  }
}

variable "grafana_otlp_endpoint" {
  description = "Grafana Cloud の OTLP の送信先（スタックの OTLP の URL に /otlp を付けたもの）"
  type        = string
  default     = "https://otlp-gateway-prod-ap-northeast-0.grafana.net/otlp"
}

variable "grafana_otlp_auth_secret" {
  description = "Grafana Cloud にトレースを送るための Authorization ヘッダーの値を置いた Secret Manager の secret（プロジェクトに用意されているもの）"
  type        = string
  default     = "grafana-otlp-auth"
}
