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
| `make image-push IMAGE=...` | コンテナイメージを push する（`IMAGE` は push 先を含めた名前。先に `make image`） |
| `make tf-fmt` | Terraform を整形する（ファイルを書き換える） |
| `make tf-lint` | Terraform の整形の崩れと、TFLint の指摘を確認する |
| `make tf-validate` | Terraform を検証する（backend には接続しない） |
| `make tf-init ENV=dev TF_STATE_BUCKET=...` | Terraform を初期化する（Google Cloud へのログインが要る） |
| `make tf-plan ENV=dev GCP_PROJECT_ID=...` | Terraform の plan（先に `tf-init`。Cloud Run Job のイメージは `IMAGE_TAG` のタグ。既定は HEAD のコミットの SHA） |
| `make tf-apply ENV=dev GCP_PROJECT_ID=...` | Terraform の apply（先に `tf-init`。`TF_APPLY_ARGS` でオプションを足せる） |
| `make smoke-test GCP_PROJECT_ID=...` | デプロイした Cloud Run Job を `--dry-run` で実行し、成功するかを確かめる |
| `make secret-update ENV=dev GCP_PROJECT_ID=... FILE=config.yaml` | 設定ファイルを Secret Manager に入れる（[configuration.md](./configuration.md)） |
| `make tidy` | `go.mod` と `go.sum` を整理する |
| `make clean` | ビルドの成果物を消す |

`make` だけを実行すると、ターゲットの一覧が出る。

## CI

PR（`develop`・`main` 向き）で [.github/workflows/ci.yml](../.github/workflows/ci.yml) が動く。`develop`・`main` への push では、CD（後述）から同じワークフローが呼ばれる。

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

## CD

`develop`・`main` への push で [.github/workflows/cd.yml](../.github/workflows/cd.yml) が動き、`develop` は dev、`main` は prod に出す。Actions の画面から手で動かすこともできる（`develop`・`main` のときだけ出す）。

1. CI（`ci.yml`）を呼び、通ったら次に進む。
2. コンテナイメージをビルドし、Artifact Registry に push する（`make image`・`make image-push`）。
   - タグはコミットの SHA。タグは上書きできないので、同じコミットのイメージがもうあるときは、ビルドと push を飛ばす。
3. `make tf-init`・`make tf-apply` で、Terraform を apply する（承認は待たない。差分は PR の plan のコメントで確かめておく）。Cloud Run Job のイメージは、2 のコミットの SHA のタグにする。
4. `make smoke-test` で、デプロイした Job を `--dry-run` で実行する（実際のカレンダーを読み、nandemo-oshirase には `dry_run` で送る）。失敗したら CD を失敗にする。

- GitHub の Environment（`develop`・`prod`）の secrets で、Workload Identity 連携で認証する。
- 同じブランチのデプロイは順に行う（途中で取り消さない）。

## リリース

`develop` で開発し、`main` へのマージで prod にリリースする。バージョンは SemVer のタグ（`vX.Y.Z`）。

1. Actions の画面で「Create release PR」（[.github/workflows/release-pr.yml](../.github/workflows/release-pr.yml)）を実行する。`develop` → `main` のリリース PR ができる（すでにあれば更新される）。
   - タイトルは `release: <次のバージョン> (develop → main)`、本文はリリースノート。
2. リリース PR を **「Create a merge commit」で** マージする（スカッシュすると、中身の PR がバージョンの計算に数えられなくなる）。
3. `main` の CD が prod にデプロイし、スモークテストが通ったら、次を行う。
   - 次のバージョンのタグを打つ。前のタグからの PR のラベルで決める（`breaking`: メジャー、`feat`: マイナー、それ以外: パッチ。ラベルは PR のタイトルから付く）。
   - GitHub リリースを作る。ノートは [.github/release.yml](../.github/release.yml) の見出しに振り分ける。
   - prod の Artifact Registry のイメージ（コミットの SHA のタグ）に、同じバージョンのタグも付ける。
   - `main` → `develop` の PR を作り、マージコミットでマージする（`develop` の履歴からもタグを辿れるようにするため）。自動でマージできないときは、PR が開いたまま残り、CD が失敗する。

- 手でタグを push したときも、[.github/workflows/release.yml](../.github/workflows/release.yml) が GitHub リリースを作る。
- ワークフローの中身は [zaki3mymy/actions](https://github.com/zaki3mymy/actions) の再利用ワークフロー（`next-tag.yml`・`create-release.yml`・`release-pr.yml`・`back-merge.yml`）。
- リリース PR と取り込みの PR は、GitHub App のトークンで作る（シークレットの `BACK_MERGE_APP_CLIENT_ID`・`BACK_MERGE_APP_PRIVATE_KEY`）。

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

アプリのリソース（Cloud Run Job など）は `tf/` で管理する。プロジェクト・API・Artifact Registry・state のバケットは、このリポジトリの外で用意している。

```
tf/modules/      # アプリのリソース（dev・prod で共通）
tf/envs/dev/     # dev（backend・provider）
tf/envs/prod/    # prod
```

- state は環境のプロジェクトのバケットに、`notify-schedule/` のキーで置く。
- リポジトリは public なので、秘密の値や個人の情報（カレンダー ID、メールアドレス、nandemo-oshirase のエンドポイントなど）は tfvars や `.tf` に書かない。プロジェクト ID も `make tf-plan` に渡す。
- Cloud Run Job のイメージ（変数 `image`）は、`make tf-plan`・`make tf-apply` が `IMAGE_TAG`（既定は HEAD のコミットの SHA）から組み立てて渡す。push していないコミットで apply すると、イメージがないので失敗する。
  - PR の plan では、PR の head のコミットのタグを渡すので、イメージの差分が毎回出る。
- PR では、CI が向き先の環境（develop 向きは dev、main 向きは prod）で plan し、結果を PR にコメントする（プロジェクト ID は伏せる）。
