{
  description = "notify-schedule の開発環境";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            # nixpkgs の golangci-lint は、同じ nixpkgs の go でビルドされている
            # （golangci-lint は、自分をビルドした Go より新しい Go のコードを解析できない）
            golangci-lint
            gnumake
            pre-commit
            # GitHub Actions のワークフローの lint（pre-commit で使う）
            actionlint
          ];

          # go.mod の go より新しいツールチェーンを勝手に取得せず、nixpkgs の go を使う
          GOTOOLCHAIN = "local";
        };
      }
    );
}
