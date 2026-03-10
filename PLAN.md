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

## 過去の実装ステップ（完了済み）

- Step 1-7: 初期実装 〜 v1.0.0 リリース（完了）
- Step 8: Windows OpenSSH ACL準拠 v1.1.0（完了）

## Step 9: セキュリティ修正（v1.2.0）

### 現状の把握

セキュリティレビューで3件の指摘を受けた:
1. **High**: ホスト鍵検証が無効（MITM脆弱性）
2. **Medium**: Admin鍵ファイル判定が不正確
3. **Medium**: ACLのプリンシパル指定が名前ベースで脆弱

### 9-1: ホスト鍵検証の実装（High）

**問題**: `ssh.InsecureIgnoreHostKey()` でホスト鍵検証を完全に無効化しており、MITM攻撃でパスワードが漏洩する

**方針**: `known_hosts` ファイルによる検証をデフォルトにし、`--insecure` フラグで明示的にオプトイン

- [x] `golang.org/x/crypto/ssh/knownhosts` パッケージを使用
- [x] デフォルト: `~/.ssh/known_hosts` からホスト鍵を検証
- [x] 未知のホスト: フィンガープリントを表示し、ユーザーに確認プロンプト（Trust on First Use）
  - 承認時は `~/.ssh/known_hosts` に自動追記
- [x] `--insecure` フラグ追加: 検証スキップ（現在の挙動）+ 警告メッセージ表示
- [x] README に Security セクション追加

**変更ファイル**: `ssh.go`, `main.go`, `README.md`

### 9-2: Admin鍵ファイル判定の改善（Medium）

**問題**: `Match Group administrators` の存在だけで判定し、実際の `AuthorizedKeysFile` 値を検証していない

**方針**: Match ブロック内の `AuthorizedKeysFile` ディレクティブを実際にパースする

- [x] sshd_config から `Match Group administrators` ブロックを抽出
- [x] ブロック内の `AuthorizedKeysFile` 値が `administrators_authorized_keys` を含むか検証
- [x] Match ブロックはあるが AuthorizedKeysFile が異なる場合は user ディレクトリにフォールバック + 警告

**変更ファイル**: `deploy.go`, `deploy_test.go`

### 9-3: ACL プリンシパルの SID ベース化（Medium）

**問題**: `Administrators` / `SYSTEM` が英語名ハードコード。非英語 Windows やドメイン環境で失敗する

**方針**: Well-known SID を使用して言語非依存にする

- [x] `SYSTEM` → `*S-1-5-18`（Well-known SID）
- [x] `Administrators` → `*S-1-5-32-544`（Well-known SID）
- [x] ユーザー: `${env:USERNAME}` → PowerShell で現在のユーザーの SID を取得して使用
- [x] テスト更新

**変更ファイル**: `deploy.go`, `deploy_test.go`

## リポジトリ

- [x] `git init` + リモート設定: `gitlab.com/kwrkb/ssh-pushkey`
  > モジュールパスを `github.com/yugosasaki` → `gitlab.com/kwrkb` に変更
