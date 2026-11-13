# 値は tfvars に書かず、make tf-plan などで渡す（CI では GitHub の Environment の Secrets から渡す）
variable "project_id" {
  description = "デプロイ先のプロジェクト"
  type        = string
}

variable "image" {
  description = "Cloud Run Job で動かすイメージ（make tf-plan・tf-apply が、コミットの SHA から組み立てて渡す）"
  type        = string
}
