variable "project_id" {
  description = "デプロイ先のプロジェクト"
  type        = string
}

variable "image" {
  description = "Cloud Run Job で動かすイメージ（Artifact Registry のコミットの SHA のタグ）"
  type        = string
}
