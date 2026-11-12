variable "project_id" {
  description = "デプロイ先のプロジェクト（zaki3mymy-dev・zaki3mymy-prod）"
  type        = string
}

variable "env" {
  description = "環境（dev・prod）"
  type        = string

  validation {
    condition     = contains(["dev", "prod"], var.env)
    error_message = "env は dev か prod にしてください。"
  }
}
