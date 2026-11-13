terraform {
  required_version = ">= 1.17.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 8.0"
    }
  }

  # bucket は init のときに -backend-config で渡す（make tf-init）
  backend "gcs" {
    prefix = "notify-schedule"
  }
}

provider "google" {
  project = var.project_id
  region  = "asia-northeast1"

  default_labels = {
    app = "notify-schedule"
  }
}

module "app" {
  source = "../../modules"

  project_id = var.project_id
  image      = var.image

  # dev は毎日は動かさないので、Cloud Scheduler を作らない（使うときは手で実行する）
}
