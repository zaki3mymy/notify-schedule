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
| tflint | Terraform の lint |

terraform（1.17 以上）は flake に入れていないので、別に入れる。unfree で cache.nixos.org にビルド済みのものがなく、flake.lock を更新するたびにソースからのビルド（数分）がかかるため。CI では `hashicorp/setup-terraform` で入れる。

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
| `make test` | テストを実行する（`-race` 付き）。カバレッジを `coverage.out` に書き、全体のカバレッジを出す |
| `make cover-html` | カバレッジを HTML にする（`coverage.html`。`make test` のあとに実行する） |
| `make fmt` | コードを整形する（ファイルを書き換える） |
| `make lint` | 静的解析をする（`go vet`、golangci-lint。整形の崩れと依存の向きも確認する） |
| `make image` | コンテナイメージをビルドする（`IMAGE` で名前、`CONTAINER` で podman・docker を変えられる） |
| `make tf-fmt` | Terraform を整形する（ファイルを書き換える） |
| `make tf-lint` | Terraform の整形の崩れと、TFLint の指摘を確認する |
| `make tf-validate` | Terraform を検証する（backend には接続しない） |
| `make tf-init ENV=dev TF_STATE_BUCKET=...` | Terraform を初期化する（Google Cloud へのログインが要る） |
| `make tf-plan ENV=dev GCP_PROJECT_ID=...` | Terraform の plan（先に `tf-init`） |
| `make tidy` | `go.mod` と `go.sum` を整理する |
| `make clean` | ビルドの成果物を消す |

`make` だけを実行すると、ターゲットの一覧が出る。

## CI

PR（`develop`・`main` 向き）と、`develop`・`main` への push で [.github/workflows/ci.yml](../.github/workflows/ci.yml) が動く。

- ローカルと同じツールを使うため、flake.nix の devShell の中で make のターゲットを実行する（`nix develop -c make lint` など）。`make lint` だけだと、ランナーにあるツールが使われてしまう。
- 実行するのは `make lint`・`make test`・`make build`、ワークフローの lint（actionlint）、`make tf-lint`・`make tf-validate`。別のジョブで、コンテナイメージのビルド（`make image`）も確かめる（push はしない）。
- カバレッジは、実行結果の画面（ジョブのサマリー）に全体と関数ごとの値を出し、HTML のレポート（`coverage.html`）と `coverage.out` を成果物（`coverage-report`、30日間）に残す。
- ファズテストは、`go test` ではシード（`f.Add` の入力）だけを実行する。入力を生成して探すときは、手元で `go test -fuzz=<名前> ./<パッケージ>` を実行する。
- キャッシュ
  - nix のストア（go・golangci-lint などのツール）：キーは flake.nix・flake.lock のハッシュ。ツールはビルドせず、cache.nixos.org からビルド済みのものを取得するが、その取得も省く。
  - Go のモジュール・ビルドのキャッシュと、golangci-lint の解析のキャッシュ：キーは go.mod・go.sum・flake.lock のハッシュ。
  - キャッシュを保存するのは、`develop`・`main` への push のときだけ。PR では読むだけにする。
    - GitHub Actions のキャッシュは、作ったブランチと、そのブランチに向いた PR でしか使えない。PR で作ったキャッシュはその PR でしか使えず、リポジトリのキャッシュの容量（10GB。超えると古いものから消える）を埋めるだけになるため。
    - develop のキャッシュは develop 向きの PR で、main のキャッシュは main 向きの PR（リリースの PR）で使われる。
- PR のタイトルは Conventional Commits の形（`feat: ...`、`fix(cli): ...` など）にする。[.github/workflows/label-pr-by-title.yml](../.github/workflows/label-pr-by-title.yml) がタイトルからラベルを付ける（形が違うときは失敗する）。

## Dependabot

[.github/dependabot.yml](../.github/dependabot.yml) で、次の更新 PR を週に1回作る。PR のタイトルは `chore(deps): ...` などの形にしている。

| 対象 | 内容 |
|---|---|
| `github-actions` | ワークフローのアクション（SHA で固定したものも、SHA とバージョンのコメントを更新する） |
| `terraform` | Terraform のプロバイダ（`tf/envs/dev`・`tf/envs/prod`） |
| `docker` | Containerfile のベースイメージ（ダイジェストも更新する） |
| `gomod` | Go のモジュール（マイナー・パッチは1つの PR にまとめる） |
| `nix` | flake.lock の入力（nixpkgs を進めると、開発のツールがまとめて新しくなる） |

## Terraform

アプリのリソース（Cloud Run Job など）は `tf/` で管理する。プロジェクト・API・Artifact Registry・state のバケットは account-baseline で管理している。

```
tf/modules/      # アプリのリソース（dev・prod で共通）
tf/envs/dev/     # dev（backend・provider）
tf/envs/prod/    # prod
```

- state は環境のプロジェクトのバケットに、`notify-schedule/` のキーで置く。
- リポジトリは public なので、秘密の値や個人の情報（カレンダー ID、メールアドレス、nandemo-oshirase のエンドポイントなど）は tfvars や `.tf` に書かない。プロジェクト ID も `make tf-plan` に渡す。
- PR では、CI が向き先の環境（develop 向きは dev、main 向きは prod）で plan し、結果を PR にコメントする（プロジェクト ID は伏せる）。
