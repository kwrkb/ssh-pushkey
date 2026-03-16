# ssh-pushkey Project Settings

## プロジェクト概要

Windows OpenSSH サーバーへ SSH 公開鍵を配置する CLI ツール（`ssh-copy-id` の Windows 対応版）。

## ファイル構成

- `main.go` — CLI エントリポイント（引数パース、パスワード入力）
- `ssh.go` — SSH 接続・リモートコマンド実行（EncodedCommand 方式）
- `deploy.go` — 鍵配置ロジック（Admin 判定、スクリプト生成、ACL 設定）
- `deploy_test.go` — ユニットテスト
- `integration_test.go` — 統合テスト（`//go:build integration`、実機 Windows SSH 必要）

## 開発コマンド

```bash
# ビルド
go build -ldflags "-X main.version=dev"

# ユニットテスト
go test ./...

# 静的解析
go vet ./...

# 統合テスト（Windows SSH ホスト必要）
go test -tags=integration -v ./...
```

## Git Workflow

- デュアルリモート: `origin`=GitLab, `github`=GitHub
- push は両方に行う: `git push origin && git push github`
- MR/PR も両方に作成: `glab mr create` + `gh pr create`
- マージ戦略は揃える（両方 merge commit 推奨）
- リリースは両方とも CI/CD で自動化済み:
  - **GitLab**: `.gitlab-ci.yml` の `release` ジョブが `v*` タグ push で CHANGELOG.md からノート抽出
  - **GitHub**: `.github/workflows/release.yml` が `v*` タグ push で CHANGELOG.md からノート抽出 → クロスプラットフォームバイナリ付きリリース作成
- リリース手順: `CHANGELOG.md` 更新 → タグ作成 → 両リモートに push → 両 CI が自動でリリース作成

## テスト方針

- ユニットテスト: `strings.Contains` でスクリプト内容を検証（CLIXML 混入対策）
- 統合テスト: build tag `integration` で分離。環境変数 `SSH_TEST_HOST`, `SSH_TEST_USER`, `SSH_TEST_PASSWORD` が必要
- PR 前に `go test ./...` と `go vet ./...` を必ず実行

## Windows OpenSSH 固有ルール

- SSH 経由の PowerShell 実行は `-EncodedCommand`（UTF-16LE Base64）を使う
- ACL は `.ssh` ディレクトリと鍵ファイルの両方に設定する
- ACE は常に `SYSTEM:(F)` / `Administrators:(F)` / `${env:USERNAME}:(F)` の 3 つ
- `authorized_keys` は BOM なし UTF-8 で書き込む
