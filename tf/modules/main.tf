# notify-schedule のリソース（dev・prod で共通）。
# プロジェクト・API・Artifact Registry・state のバケットは、このリポジトリの外で用意している。

terraform {
  required_version = ">= 1.17.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 8.0"
    }
  }
}
