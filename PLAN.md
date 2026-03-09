# ssh-pushkey 実装計画

## Context

Windows OpenSSHサーバー向けの `ssh-copy-id` 代替CLIツール。
Linux/macOSの `ssh-copy-id` がWindows OpenSSH特有の仕様（BOMなしUTF-8、Administrators問題、ACL設定）に対応していないため、これらを自動処理するGoツールを実装する。

## ファイル構成

```
ssh-pushkey/
├── main.go          # CLI引数パース、エントリーポイント
├── ssh.go           # SSH接続・セッション管理
├── deploy.go        # 鍵配置ロジック（Admin/一般分岐、ACL、重複チェック）
├── deploy_test.go   # PowerShellスクリプト生成のユニットテスト
├── go.mod
└── go.sum
```

## 実装ステップ

### Step 1: プロジェクト初期化（完了）

- [x] `go mod init gitlab.com/kwrkb/ssh-pushkey`
- [x] 依存追加: `golang.org/x/crypto/ssh`, `golang.org/x/term`

### Step 2: main.go — CLI引数パース（完了）

- [x] `flag` パッケージで `-i`, `-p`, `--version` を処理
- [x] 残り引数から `user@host` をパース
- [x] デフォルト値: 鍵=`~/.ssh/id_ed25519.pub`, ポート=22
- [x] 公開鍵ファイル読み込み・バリデーション

### Step 3: ssh.go — SSH接続（完了）

- [x] パスワードプロンプト（`golang.org/x/term` でエコーバック無効）
- [x] `ssh.Dial` でパスワード認証接続
- [x] `HostKeyCallback: ssh.InsecureIgnoreHostKey()` + TODOコメント
- [x] リモートコマンド実行ヘルパー関数（1セッション1コマンド制約対応）

### Step 4: deploy.go — 鍵配置ロジック（完了）

- [x] `buildDeployScript(pubKey string, isAdmin bool) string` — PowerShellスクリプト生成
  - [x] Administratorsグループ判定
  - [x] パス分岐（`administrators_authorized_keys` vs `~\.ssh\authorized_keys`）
  - [x] ディレクトリ存在確認・作成
  - [x] 重複チェック（Select-Stringで確認）
  - [x] `[System.IO.File]::AppendAllText()` でBOMなしUTF-8書き込み
  - [x] `icacls` でACL設定（継承無効、SYSTEM:F、対象ユーザー/Administrators:F）
- [x] `DeployKey(client *ssh.Client, pubKey string) error` — 実行関数

### Step 5: deploy_test.go — ユニットテスト（完了）

- [x] Admin/一般ユーザーでのパス・ACL差異テスト
- [x] BOMなしUTF-8書き込みコードの存在確認
- [x] 公開鍵のエスケープ処理テスト
- [x] 重複チェックロジックの存在確認
- [x] ErrorActionPreference設定の確認

## 検証結果

- [x] `go build` — 正常ビルド確認
- [x] `go test ./...` — 全4テスト PASS
- [x] `go vet ./...` — 静的解析 OK

## リポジトリ

- [x] `git init` + リモート設定: `gitlab.com/kwrkb/ssh-pushkey`
  > モジュールパスを `github.com/yugosasaki` → `gitlab.com/kwrkb` に変更
