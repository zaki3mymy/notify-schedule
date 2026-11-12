# 開発の操作は、すべてこの Makefile のターゲットにまとめる。
# pre-commit と CI も、ここのターゲットを呼ぶ（ローカルと CI で同じコマンドを使うため）。

.DEFAULT_GOAL := help

# golangci-lint は、go.mod の依存に入れず、バージョンを固定して bin/ に入れる
GOLANGCI_LINT_VERSION := v2.15.0

BIN_DIR       := $(CURDIR)/bin
GOLANGCI_LINT := $(BIN_DIR)/golangci-lint-$(GOLANGCI_LINT_VERSION)
APP           := $(BIN_DIR)/notify-schedule

.PHONY: help
help: ## ターゲットの一覧を出す
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## アプリをビルドする（bin/notify-schedule）
	go build -trimpath -o $(APP) ./cmd/notify-schedule

.PHONY: test
test: ## テストを実行する
	go test -race ./...

.PHONY: fmt
fmt: $(GOLANGCI_LINT) ## コードを整形する（ファイルを書き換える）
	$(GOLANGCI_LINT) fmt

.PHONY: lint
lint: $(GOLANGCI_LINT) ## 静的解析をする（go vet・整形の崩れ・依存の向きも確認する）
	go vet ./...
	$(GOLANGCI_LINT) run

.PHONY: tidy
tidy: ## go.mod と go.sum を整理する
	go mod tidy

.PHONY: clean
clean: ## ビルドの成果物と、bin/ に入れたツールを消す
	rm -rf ./bin

# go install <パッケージ>@<バージョン> は、このリポジトリの go.mod を見ずにツールチェーンを選ぶ。
# golangci-lint は、自分をビルドした Go より新しい Go のコードを解析できないので、
# このリポジトリの Go（go.mod の go）でビルドする
$(GOLANGCI_LINT):
	GOTOOLCHAIN=$$(go env GOVERSION) GOBIN=$(BIN_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv $(BIN_DIR)/golangci-lint $@
