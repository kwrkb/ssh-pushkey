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

## Step 11: 全体レビューの未解決事項

### 現状の把握

`go test ./...` と `go vet ./...` は通過しているが、全体レビューで以下の未解決リスクを確認した。

1. `dialSSH` が `HostKeyAlgorithms` 設定時の失敗を無条件で再試行し、認証失敗でも二重ログイン試行になる
2. `readPubKey` が複数行の `.pub` ファイルを1件の鍵として受け入れ、意図しない追加キーを配備し得る
3. 管理者向け鍵ファイル判定が `Match Group administrators` ブロック前提で、他の有効な OpenSSH 設定形状を取りこぼす

### マルチ AI レビュー結果（2026-03-13）

Gemini・Codex・Claude の3者で議論。

**実装順序**: 11-2 → 11-1 → 11-3（3者一致）

- **11-2 を最優先**: 複数行入力で `authorized_keys` 破損 → SSH ログイン不能リスク
- **11-1 は次点**: 認証失敗でも再試行 → ロックアウト・監査ログ汚染リスク
- **11-3 は後回し**: 現状で標準構成はカバーできている

**実装アプローチ（採用方針）**:
- 11-2: 非空行が1行であることを確認 + `ssh.ParseAuthorizedKey` で検証。複数行は黙殺せず reject（Codex 案採用）
- 11-1: `shouldRetryWithoutHostKeyAlgorithms(err)` ヘルパーに切り出し、`"no common algorithm"` かつ `"host key"` を含む場合のみ再試行。`"unable to authenticate"` では再試行しない（Codex 案採用）
- 11-3: `sshd -T` 優先 → 失敗時は現行パースにフォールバック → 確信なければ user 側配備 + 警告。前提として `useAdminKeyFile` を bool → 配備先パス + 判定理由に設計変更（Codex 案採用、Gemini 方向に同意）

**Step 11 以降の優先順位**: icacls エラー伝搬 → Dry-run モード（Codex 推奨、Claude 同意）
- 理由: ACL 失敗時の観測性がツールのコア価値に直結。Dry-run は便利機能だが後でよい

**対応すべき追加指摘**:
1. 接続先が Linux の場合のエラー改善（中優先度、11-3 と同時に対応）
2. `known_hosts` 書き込み競合のロック制御（中高優先度）

**見送る指摘**:
3. `SSH_ASKPASS` サポート（低優先度、TTY 前提で一貫しており現時点では不要）
4. PowerShell スクリプトの `text/template` 化（低優先度、現規模では過剰。分岐増加時に再評価）

### 11-1: SSH再試行条件の絞り込み

**問題**: `HostKeyAlgorithms` を使った初回接続が失敗したとき、原因を問わず制限解除して再試行してしまう

**方針**: `shouldRetryWithoutHostKeyAlgorithms(err)` ヘルパーを導入し、再試行判定を明確化する

- [ ] `shouldRetryWithoutHostKeyAlgorithms(err error) bool` ヘルパー関数を作成
  - `"no common algorithm"` かつ `"host key"` を含む → true（鍵アルゴリズム交渉失敗）
  - `"unable to authenticate"` を含む → false（認証失敗）
  - その他 → false（保守的判定）
- [ ] `dialSSH` のリトライ条件をヘルパーに置き換える
- [ ] ヘルパー関数のユニットテスト追加（`ssh_test.go`）

**変更対象ファイル**: `ssh.go`, `ssh_test.go`

### 11-2: 公開鍵入力の厳格化（最優先）

**問題**: `.pub` ファイルの内容検証が緩く、複数行・連結済み入力をそのまま配備できてしまう

**方針**: 非空行が1行であることを確認し、`ssh.ParseAuthorizedKey` で検証。複数行は黙殺せず reject

- [ ] `readPubKey` で空行除去後、非空行が1行であることを確認（2行以上はエラー）
- [ ] `ssh.ParseAuthorizedKey` でフォーマット検証
- [ ] `resolveKey()` 経由の鍵も同じ検証パスを通す（将来の ssh-agent 入力も統一）
- [ ] `readPubKey` の正常系・異常系テストを追加する

**変更対象ファイル**: `main.go`, `main_test.go`

### 11-3: 管理者鍵ファイル判定の一般化

**問題**: `administrators_authorized_keys` 判定が `Match Group administrators` ブロック内の `AuthorizedKeysFile` だけに依存している

**方針**: `sshd -T` 優先 → 現行パースにフォールバック → 確信なければ user 側配備 + 警告

- [ ] `useAdminKeyFile` を bool → 構造体（配備先パス + 判定理由）に設計変更
- [ ] `sshd -T` をリモート実行し `authorizedkeysfile` の実効値を取得する処理を追加
- [ ] `sshd -T` 失敗時は現行の `sshd_config` パースにフォールバック
- [ ] 確信できない場合は user 側に配備 + 明示警告（「成功に見えてログインできない」を回避）
- [ ] Linux 接続時は早期エラー（Windows 専用であることを明示）— Gemini 指摘対応
- [ ] 構成差分をカバーするテストを追加する

**変更対象ファイル**: `deploy.go`, `deploy_test.go`, `integration_test.go`

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

### エラーメッセージの局所化（icacls 出力の伝搬）（Step 11 後の最優先）

PowerShell 側でエラー発生時に `ACL_SET_FAILED_DIR` などのマーカーを返しているが、`icacls` が出力した実際のエラーメッセージが Go 側に伝搬されていない。トラブルシューティング改善のため、実エラー出力を含める。

- [ ] PowerShell スクリプト内で `icacls` の stderr/stdout を捕捉しマーカーに付加
- [ ] Go 側でマーカー解析時に詳細メッセージを抽出しエラーに含める
- [ ] `deploy_test.go` にエラーメッセージ伝搬のテスト追加

### 重複判定の鍵 blob 単位比較化（Codex 指摘）

現在の `Select-String -SimpleMatch -Pattern $pubKey` は行全体一致のため、同じ鍵本体でもコメント違いだと重複検知できない。

- [ ] 鍵 blob（type + base64 部分）のみで比較するように変更
- [ ] テスト追加

### 接続タイムアウトの追加（Codex 指摘）

`dialSSH` に接続タイムアウトがなく、ネットワーク不調時に長時間待ちが発生し得る。

- [ ] `ssh.ClientConfig.Timeout` または `net.DialTimeout` で適切なタイムアウトを設定
- [ ] デフォルト値の決定（30秒程度）

### known_hosts 書き込み競合の対策（Gemini・Codex 共通指摘）

`known_hosts` への追記・書き換えにロック制御がなく、複数プロセス同時実行で破損し得る。

- [ ] ファイルロック（`flock` / `LockFileEx` 相当）の導入検討
- [ ] CLI ツールとしての頻度を考慮した実装判断

## リポジトリ

- [x] `git init` + リモート設定: `gitlab.com/kwrkb/ssh-pushkey`
  > モジュールパスを `github.com/yugosasaki` → `gitlab.com/kwrkb` に変更
