# 開発

## 開発環境

開発に使うツールは [flake.nix](../flake.nix) で管理する。

| ツール | 用途 |
|---|---|
| go | ビルドとテスト |
| golangci-lint | 静的解析と整形 |
| gnumake | 開発の操作（make のターゲット） |
| pre-commit | コミット前のチェック |

[direnv](https://direnv.net/) を使うと、このディレクトリに入ったときに自動でツールが PATH に入る（[.envrc](../.envrc)）。direnv を使わないときは `nix develop` を実行する。

```shell
direnv allow   # または nix develop
pre-commit install
make build
```

- devShell では `GOTOOLCHAIN=local` にしている。go.mod の `go` より新しいツールチェーンを勝手に取得せず、nixpkgs の go を使うため。そのため、go.mod の `go` は、nixpkgs の go のバージョン以下にする。
- golangci-lint は、自分をビルドした Go より新しい Go のコードを解析できない。nixpkgs の golangci-lint は同じ nixpkgs の go でビルドされているので、この組み合わせなら問題が起きない。
- ツールのバージョンを上げるときは、`nix flake update` で flake.lock を更新する。

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
