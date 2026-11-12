# --recursive で実行するため、make tf-lint ではこのファイルを絶対パスで渡す
plugin "terraform" {
  enabled = true
  preset  = "recommended"
}
