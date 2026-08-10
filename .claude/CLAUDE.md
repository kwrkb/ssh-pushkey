# ssh-pushkey Project Settings

## プロジェクト概要

Windows OpenSSH サーバーへ SSH 公開鍵を配置する CLI ツール（`ssh-copy-id` の Windows 対応版）。

## ファイル構成

- `main.go` — CLI エントリポイント（引数パース、`~/.ssh/config` 解決、パスワード入力）
- `ssh.go` — SSH 接続・known_hosts 検証・リモートコマンド実行（EncodedCommand 方式）
- `deploy.go` — 鍵配置ロジック（Admin 判定、スクリプト生成、ACL 設定）
- `*_test.go` — ユニットテスト（`main_test.go` / `ssh_test.go` / `deploy_test.go` / `hostkey_verification_test.go`）
- `integration_test.go` — 統合テスト（`//go:build integration`、実機 Windows SSH 必要）
- `docs/` — 検証記録（`match-address-verification.md`）

## 開発コマンド

正規の入口は **`Makefile`**（env ファイルを自動ロードする）。生 `go` コマンドは下記に等価。

```bash
make build         # go build -ldflags "-X main.version=dev" ./...
make test          # go test ./...
make vet           # go vet ./...
make check         # build + vet + test + `go build -tags=integration ./...`（PR 前必須）
make itest-admin   # 統合テスト（管理者アカウント / .env.integration）
make itest-user    # 統合テスト（一般アカウント / .env.integration.user）
make itest-all     # admin → user の順で両方
# 特定テストのみ: make itest-admin RUN=TestIntegration_AdminDetection
```

## Git Workflow

- デュアルリモート: `origin`=GitHub（プライマリ）、`gitlab`=GitLab（サブ）
- push は `git push origin && git push gitlab` の順
- MR/PR も両方に作成: `gh pr create` + `glab mr create`
- マージ戦略は揃える（両方 merge commit 推奨）
- 両 remote で独立にマージするとマージコミット SHA が割れる。次回 master push が非 fast-forward で reject されたら `git merge gitlab/master` で reconcile してから両方に push する
- リリースは両方とも CI/CD で自動化済み:
  - **GitLab**: `.gitlab-ci.yml` の `release` ジョブが `v*` タグ push で CHANGELOG.md からノート抽出
  - **GitHub**: `.github/workflows/release.yml` が `v*` タグ push で CHANGELOG.md からノート抽出 → クロスプラットフォームバイナリ付きリリース作成
- リリース手順: `CHANGELOG.md` 更新 → タグ作成 → 両リモートに push → 両 CI が自動でリリース作成
- CI: `.github/workflows/ci.yml` が PR / master push で build + vet + test + integration ビルドを実行（Go は `go.mod` 準拠）。`make check` とほぼ同内容なのでローカルで通れば CI も通る

## テスト方針

- ユニットテスト: `strings.Contains` でスクリプト内容を検証（CLIXML 混入対策）
- 統合テスト: build tag `integration` で分離。認証情報は 2 つの env ファイルから読む（いずれも gitignore 済み、Makefile が自動ロードするので手動 `source` 不要）:
  - `.env.integration` — 管理者アカウント（例: `pushadmin`）
  - `.env.integration.user` — 一般アカウント（例: `pushuser`）
  - 各ファイルは `export SSH_TEST_HOST=... / SSH_TEST_USER=... / SSH_TEST_PASSWORD=...` を定義
  - 値に 1Password の `op://<vault>/<item>/<field>` 参照を書ける。その場合 Makefile は `source` ではなく `op run --env-file` に委譲して解決する（`op` CLI とサインインが必要）
- PR 前は `make check` を必ず実行（build + vet + test + integration ビルド検証で build tag 付きファイルのコンパイル崩れを検知）

## Windows OpenSSH 固有ルール

- SSH 経由の PowerShell 実行は `-EncodedCommand`（UTF-16LE Base64）を使う
- ACL は `.ssh` ディレクトリと鍵ファイルの両方に設定する
- ACE は**必ず SID で指定**（言語非依存のため名前指定にしない）: `*S-1-5-18`（SYSTEM）/ `*S-1-5-32-544`（Administrators）/ `*${userSid}`（実行ユーザー、実行時に取得）の 3 つ
- `authorized_keys` は BOM なし UTF-8 で書き込む
- 配置先は2択で分岐（`buildDeployScript`）: Admin 判定が真なら `C:\ProgramData\ssh\administrators_authorized_keys`、それ以外は `%USERPROFILE%\.ssh\authorized_keys`
- Admin 判定（`resolveKeyFileTarget`）は Administrators グループ所属だけでは決めない。`sshd -T -C <spec>` の `AuthorizedKeysFile` 出力（フォールバックで `sshd_config` の `Match Group administrators` ブロック）を見て、実際に administrators_authorized_keys が有効かどうかで判定する
- `sshd -T` には `SSH_CONNECTION` から組み立てた `-C` スペックを渡し、`Match Address` まで評価させる（`parseSSHConnectionSpec`。取得失敗時は loopback 相当にフォールバック）
