# 値は tfvars に書かず、make tf-plan などで渡す（CI では GitHub の Environment の Secrets から渡す）
variable "project_id" {
  description = "デプロイ先のプロジェクト"
  type        = string
}
