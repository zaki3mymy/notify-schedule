# 開発

## 開発環境

開発に使うツールは [flake.nix](../flake.nix) で管理する。

| ツール | 用途 |
|---|---|
| go | ビルドとテスト |
| golangci-lint | 静的解析と整形 |
| gnumake | 開発の操作（make のターゲット） |
| pre-commit | コミット前のチェック |
| actionlint | GitHub Actions のワークフローの lint |

[direnv](https://direnv.net/) を使うと、このディレクトリに入ったときに自動でツールが PATH に入る（[.envrc](../.envrc)）。direnv を使わないときは `nix develop` を実行する。

```shell
direnv allow   # または nix develop
pre-commit install
make build
```

- devShell では `GOTOOLCHAIN=local` にしている。go.mod の `go` より新しいツールチェーンを勝手に取得せず、nixpkgs の go を使うため。そのため、go.mod の `go` は、nixpkgs の go のバージョン以下にする。
- golangci-lint は、自分をビルドした Go より新しい Go のコードを解析できない。nixpkgs の golangci-lint は同じ nixpkgs の go でビルドされているので、この組み合わせなら問題が起きない。
- ツールのバージョンは、flake.lock の nixpkgs のコミット（`rev`）で決まる。nixpkgs のコミットを進めると、すべてのツールがまとめて新しくなる。
  - 更新は Dependabot が週に1回 PR を作る。手で上げるときは `nix flake update` を実行する。
  - 実際のバージョンは `nix develop -c go version` や `nix eval --inputs-from . --raw nixpkgs#go.version` で確かめられる。

## make のターゲット

開発の操作は、すべて [Makefile](../Makefile) のターゲットにまとめている。pre-commit と CI も、これらのターゲットを呼ぶ（ローカルと CI で同じコマンドを使うため）。

| ターゲット | 内容 |
|---|---|
| `make build` | アプリをビルドする（`bin/notify-schedule`） |
| `make test` | テストを実行する（`-race` 付き） |
| `make fmt` | コードを整形する（ファイルを書き換える） |
| `make lint` | 静的解析をする（`go vet`、golangci-lint。整形の崩れと依存の向きも確認する） |
| `make tidy` | `go.mod` と `go.sum` を整理する |
| `make clean` | ビルドの成果物を消す |

`make` だけを実行すると、ターゲットの一覧が出る。

## CI

PR（`develop`・`main` 向き）で [.github/workflows/ci.yml](../.github/workflows/ci.yml) が動く。

- ローカルと同じツールを使うため、flake.nix の devShell の中で make のターゲットを実行する（`nix develop -c make lint` など）。`make lint` だけだと、ランナーにあるツールが使われてしまう。
- 実行するのは `make lint`・`make test`・`make build` と、ワークフローの lint（actionlint）。
- キャッシュ
  - nix のストア（go・golangci-lint などのツール）：キーは flake.nix・flake.lock のハッシュ。ツールはビルドせず、cache.nixos.org からビルド済みのものを取得するが、その取得も省く。
  - Go のモジュール・ビルドのキャッシュと、golangci-lint の解析のキャッシュ：キーは go.mod・go.sum・flake.lock のハッシュ。
- PR のタイトルは Conventional Commits の形（`feat: ...`、`fix(cli): ...` など）にする。[.github/workflows/label-pr-by-title.yml](../.github/workflows/label-pr-by-title.yml) がタイトルからラベルを付ける（形が違うときは失敗する）。

## Dependabot

[.github/dependabot.yml](../.github/dependabot.yml) で、次の更新 PR を週に1回作る。PR のタイトルは `chore(deps): ...` などの形にしている。

| 対象 | 内容 |
|---|---|
| `github-actions` | ワークフローのアクション（SHA で固定したものも、SHA とバージョンのコメントを更新する） |
| `gomod` | Go のモジュール（マイナー・パッチは1つの PR にまとめる） |
| `nix` | flake.lock の入力（nixpkgs を進めると、開発のツールがまとめて新しくなる） |
