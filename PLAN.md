# ssh-pushkey 実装計画

## Context

Windows OpenSSHサーバー向けの `ssh-copy-id` 代替CLIツール。
Linux/macOSの `ssh-copy-id` がWindows OpenSSH特有の仕様（BOMなしUTF-8、Administrators問題、ACL設定）に対応していないため、これらを自動処理するGoツールを実装する。

## ファイル構成

```
ssh-pushkey/
├── main.go              # CLI引数パース、デフォルト鍵探索、エントリーポイント
├── main_test.go         # 鍵探索ロジックのユニットテスト
├── ssh.go               # SSH接続・セッション管理
├── deploy.go            # 鍵配置ロジック（Admin/一般分岐、ACL、重複チェック）
├── deploy_test.go       # PowerShellスクリプト生成のユニットテスト
├── integration_test.go  # 統合テスト（build tag: integration）
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

## Step 10: ホストキー変更のインタラクティブ対応

### 現状の把握

Termux から Windows ホストへ接続時、known_hosts に登録済みでも Go SSH クライアントが別の鍵アルゴリズムをネゴシエーションし「REMOTE HOST IDENTIFICATION HAS CHANGED」エラーで接続不可になる問題が発生。

### 10-1: インタラクティブ known_hosts 更新（完了）

- [x] ホストキー変更時に yes/no プロンプトで known_hosts 更新を確認
- [x] 古いエントリ除去 + 新しい鍵追記の `replaceHostKeyInKnownHosts` 実装
- [x] マルチエイリアス行で対象ホストだけ除去し他エイリアスを保持

### 10-2: HostKeyAlgorithms 制限（完了）

- [x] known_hosts から登録済みアルゴリズムを抽出し `config.HostKeyAlgorithms` に設定
- [x] アルゴリズム制限でハンドシェイク失敗時は制限を外してリトライ（鍵ローテーション対応）

### 10-3: レビュー指摘対応（完了）

- [x] `strings.Replace` によるマルチエイリアス行書き換えをフィールド再構成に変更（Base64衝突リスク排除）

### 10-4: ハッシュ化 known_hosts 対応（完了）

`HashKnownHosts yes` 環境で `|1|...` 形式のエントリに対して自前パース関数がマッチできない問題。

- [x] `hostKeyAlgorithmsFromKnownHosts` でハッシュ形式エントリに対応
- [x] `replaceHostKeyInKnownHosts` でハッシュ形式エントリの除去に対応
- [x] HMAC-SHA1 でホスト名をハッシュし比較するロジック追加
- [x] TOFU パスでもハッシュ形式を維持（Codex レビューで発見）
- [x] TOFU / replace 両パスでハッシュ判定ロジックを統一（Gemini レビューで発見）
- [x] `ssh_test.go` — `matchHashedHost` / `hostMatchesAddr` のユニットテスト追加

**MR**: https://gitlab.com/kwrkb/ssh-pushkey/-/merge_requests/3 (10-1 〜 10-3)
**MR**: https://gitlab.com/kwrkb/ssh-pushkey/-/merge_requests/4 (10-4)

## 将来の改善候補

### デフォルト公開鍵の探索強化（ssh-copy-id 互換）（完了）

`-i` 未指定時に ssh-agent → `~/.ssh/id_*.pub` (mtime最新) の順で自動探索するように変更。

- [x] `ssh-add -L` 実行 → 出力があればその鍵を使用
- [x] エージェント未使用/鍵なしの場合、`~/.ssh/id_*.pub` を glob → mtime 最新を選択
- [x] どちらも見つからない場合はエラー
- [x] `main.go` の `defaultPubKeyPath()` を `resolveDefaultKey()` に置き換え
- [x] `main_test.go` に `parseSshAddOutput` / `findNewestPubKeyIn` のユニットテスト追加

### Dry-run モード（`-n` フラグ）

`ssh-copy-id -n` 互換。鍵を実際に配置せず、何が行われるかをプレビュー表示する。

- [ ] `-n` / `--dry-run` フラグ追加
- [ ] 鍵読み込み・SSH接続・Admin判定まで実行し、配置先パスと鍵内容を表示
- [ ] `DeployKey` を呼ばずに終了（実際のファイル書き込み・ACL設定はスキップ）
- [ ] 重複チェック結果も表示（「既に配置済み」or「新規追加予定」）

### エラーメッセージの局所化（icacls 出力の伝搬）

PowerShell 側でエラー発生時に `ACL_SET_FAILED_DIR` などのマーカーを返しているが、`icacls` が出力した実際のエラーメッセージが Go 側に伝搬されていない。トラブルシューティング改善のため、実エラー出力を含める。

- [ ] PowerShell スクリプト内で `icacls` の stderr/stdout を捕捉しマーカーに付加
- [ ] Go 側でマーカー解析時に詳細メッセージを抽出しエラーに含める
- [ ] `deploy_test.go` にエラーメッセージ伝搬のテスト追加

## リポジトリ

- [x] `git init` + リモート設定: `gitlab.com/kwrkb/ssh-pushkey`
  > モジュールパスを `github.com/yugosasaki` → `gitlab.com/kwrkb` に変更
