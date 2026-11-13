output "runtime_service_account_email" {
  description = "実行用のサービスアカウントのメールアドレス（Google カレンダーの共有先）"
  value       = module.app.runtime_service_account_email
}
