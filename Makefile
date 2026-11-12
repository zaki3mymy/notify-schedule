# 開発の操作は、すべてこの Makefile のターゲットにまとめる。
# pre-commit と CI も、ここのターゲットを呼ぶ（ローカルと CI で同じコマンドを使うため）。
#
# go・golangci-lint などのツールは flake.nix で管理する（nix develop か direnv で PATH に入る）。

.DEFAULT_GOAL := help

APP := ./bin/notify-schedule

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
fmt: ## コードを整形する（ファイルを書き換える）
	golangci-lint fmt

.PHONY: lint
lint: ## 静的解析をする（go vet・整形の崩れ・依存の向きも確認する）
	go vet ./...
	golangci-lint run

.PHONY: tidy
tidy: ## go.mod と go.sum を整理する
	go mod tidy

.PHONY: clean
clean: ## ビルドの成果物を消す
	rm -rf ./bin
