# 開発の操作は、すべてこの Makefile のターゲットにまとめる。
# pre-commit と CI も、ここのターゲットを呼ぶ（ローカルと CI で同じコマンドを使うため）。
#
# go・golangci-lint などのツールは flake.nix で管理する（nix develop か direnv で PATH に入る）。

.DEFAULT_GOAL := help

APP      := ./bin/notify-schedule
COVERAGE := coverage.out

# コンテナのツール（podman があれば podman、なければ docker）と、ビルドするイメージの名前
CONTAINER ?= $(shell command -v podman >/dev/null 2>&1 && echo podman || echo docker)
IMAGE     ?= notify-schedule:local

.PHONY: help
help: ## ターゲットの一覧を出す
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## アプリをビルドする（bin/notify-schedule）
	go build -trimpath -o $(APP) ./cmd/notify-schedule

.PHONY: test
test: ## テストを実行し、カバレッジを coverage.out に書く（最後に全体のカバレッジを出す）
	go test -race -coverprofile=$(COVERAGE) ./...
	@go tool cover -func=$(COVERAGE) | tail -n 1

.PHONY: cover-html
cover-html: ## カバレッジを HTML にする（coverage.html。make test のあとに実行する）
	go tool cover -html=$(COVERAGE) -o coverage.html

.PHONY: fmt
fmt: ## コードを整形する（ファイルを書き換える）
	golangci-lint fmt

.PHONY: lint
lint: ## 静的解析をする（go vet・整形の崩れ・依存の向きも確認する）
	go vet ./...
	golangci-lint run

.PHONY: image
image: ## コンテナイメージをビルドする（IMAGE で名前を変えられる）
	$(CONTAINER) build -f Containerfile -t $(IMAGE) .

.PHONY: image-push
image-push: ## コンテナイメージを push する（IMAGE に push 先を含めた名前を指定する。先に make image）
	$(CONTAINER) push $(IMAGE)

TF_ENVS := dev prod

.PHONY: tf-fmt
tf-fmt: ## Terraform を整形する（ファイルを書き換える）
	terraform fmt -recursive tf

.PHONY: tf-lint
tf-lint: ## Terraform の整形の崩れと、TFLint の指摘を確認する
	terraform fmt -check -recursive tf
	tflint --chdir=tf --recursive --config $(CURDIR)/tf/.tflint.hcl

.PHONY: tf-validate
tf-validate: ## Terraform を検証する（backend には接続しない）
	@for env in $(TF_ENVS); do \
		terraform -chdir=tf/envs/$$env init -backend=false -input=false >/dev/null && \
		terraform -chdir=tf/envs/$$env validate || exit 1; \
	done

# 以下は Google Cloud にログインしてから使う（ENV=dev か prod）。
# Cloud Run Job のイメージは、Artifact Registry の IMAGE_TAG（既定は HEAD のコミットの SHA）のタグを使う
IMAGE_TAG      ?= $(shell git rev-parse HEAD)
REGISTRY_REPO   = asia-northeast1-docker.pkg.dev/$(GCP_PROJECT_ID)/apps/notify-schedule
REGISTRY_IMAGE  = $(REGISTRY_REPO):$(IMAGE_TAG)
TF_VARS         = -var="project_id=$(GCP_PROJECT_ID)" -var="image=$(REGISTRY_IMAGE)"

.PHONY: tf-init
tf-init: ## Terraform を初期化する（ENV・TF_STATE_BUCKET が要る）
	$(call require,ENV TF_STATE_BUCKET)
	terraform -chdir=tf/envs/$(ENV) init -input=false -backend-config="bucket=$(TF_STATE_BUCKET)"

.PHONY: tf-plan
tf-plan: ## Terraform の plan（ENV・GCP_PROJECT_ID が要る。先に tf-init）
	$(call require,ENV GCP_PROJECT_ID)
	terraform -chdir=tf/envs/$(ENV) plan -input=false -no-color $(TF_VARS)

.PHONY: tf-apply
tf-apply: ## Terraform の apply（ENV・GCP_PROJECT_ID が要る。先に tf-init。TF_APPLY_ARGS でオプションを足せる）
	$(call require,ENV GCP_PROJECT_ID)
	terraform -chdir=tf/envs/$(ENV) apply -input=false $(TF_APPLY_ARGS) $(TF_VARS)

.PHONY: smoke-test
smoke-test: ## デプロイした Cloud Run Job を --dry-run で実行し、成功するかを確かめる（GCP_PROJECT_ID が要る）
	$(call require,GCP_PROJECT_ID)
	gcloud run jobs execute notify-schedule --project=$(GCP_PROJECT_ID) --region=asia-northeast1 --args=--dry-run --wait

.PHONY: image-tag-version
image-tag-version: ## Artifact Registry の IMAGE_TAG のイメージに、リリースのタグ（VERSION）を足す（GCP_PROJECT_ID・VERSION が要る）
	$(call require,GCP_PROJECT_ID VERSION)
	gcloud artifacts docker tags add $(REGISTRY_IMAGE) $(REGISTRY_REPO):$(VERSION)

.PHONY: secret-update
secret-update: ## 設定ファイルを Secret Manager に入れ、古いバージョンを消す（ENV・GCP_PROJECT_ID・FILE が要る）
	$(call require,ENV GCP_PROJECT_ID FILE)
	ENV=$(ENV) GCP_PROJECT_ID=$(GCP_PROJECT_ID) FILE=$(FILE) ./scripts/secret-update.sh

# require は、変数が空ならエラーにする
require = $(foreach v,$(1),$(if $($(v)),,$(error $(v) を指定してください)))

.PHONY: tidy
tidy: ## go.mod と go.sum を整理する
	go mod tidy

.PHONY: clean
clean: ## ビルドの成果物を消す
	rm -rf ./bin
