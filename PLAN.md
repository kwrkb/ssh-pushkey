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

### 10-5: ホストキー検証パスの自動テスト整備（Issue #16・完了）

過去の MR !2/!3/!4 がホストキー検証まわりの動作確認チェックボックスを未チェックのままマージされ、
`integration_test.go` にも検証が無かった。Issue は実 Windows ホスト向け integration-tag テスト＋手動手順を
想定していたが、**ホストキー検証は完全にクライアント側の Go SSH 処理で OS 非依存**のため、127.0.0.1 上に
立てた標準ライブラリ製 SSH サーバに対し実コードパス（`dialSSH` → `createHostKeyCallback` → `knownhosts`）を
そのまま exercise できる。これにより全項目を **tag なし・CI 実行可能**（`go test ./...`）なテストに昇格させた。

- [x] `hostkey_verification_test.go` 追加 — loopback の in-process SSH サーバを起動し検証
- [x] **TOFU 承認**: 未知ホスト → `yes` → known_hosts 追記
- [x] **誤検知なし**: 追記後の再接続でプロンプトが出ないことを `failIfPrompted` で保証
- [x] **TOFU 拒否**: `no` → 接続失敗 + 鍵が書き込まれない
- [x] **`--insecure`**: 検証スキップ + known_hosts を一切作成・変更しない
- [x] **ホストキー不一致・拒否**: 登録済みと異なる鍵 + `no` → 接続拒否 + 旧鍵のまま
- [x] **ホストキー変更・更新**: 不一致 + `yes` → known_hosts が新鍵へ更新（旧鍵除去）→ 再接続無プロンプト
- [x] **HashKnownHosts**: ハッシュ化エントリでの無プロンプト照合 / 既存ハッシュありファイルへの TOFU 追記もハッシュ化
- [x] テスト用に `readLineFromTerminal` を `var` 化しプロンプト応答を注入（本体変更は1行）

> 残る未自動化: `readLineFromTerminal` 内の実 `/dev/tty` オープンのみ（プロンプト応答を注入する都合上
> 上書きするため）。TTY 自体の挙動は手動確認に委ねる。
> 不一致テストの鍵は**同一種別（ed25519）の別鍵**を使う — 別種別だと `HostKeyAlgorithms` 制限の再試行
> 経路に逸れ、`KeyError.Want` の不一致経路を assert できない。

**Issue**: https://github.com/kwrkb/ssh-pushkey/issues/16

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

### 11-1: SSH再試行条件の絞り込み（完了）

**問題**: `HostKeyAlgorithms` を使った初回接続が失敗したとき、原因を問わず制限解除して再試行してしまう

**方針**: `shouldRetryWithoutHostKeyAlgorithms(err)` ヘルパーを導入し、再試行判定を明確化する

- [x] `shouldRetryWithoutHostKeyAlgorithms(err error) bool` ヘルパー関数を作成
  - `"no common algorithm"` かつ `"host key"` を含む → true（鍵アルゴリズム交渉失敗）
  - `"unable to authenticate"` を含む → false（認証失敗）
  - その他 → false（保守的判定）
- [x] `dialSSH` のリトライ条件をヘルパーに置き換える
- [x] ヘルパー関数のユニットテスト追加（`ssh_test.go`）

**変更対象ファイル**: `ssh.go`, `ssh_test.go`

### 11-2: 公開鍵入力の厳格化（完了）

**問題**: `.pub` ファイルの内容検証が緩く、複数行・連結済み入力をそのまま配備できてしまう

**方針**: 非空行が1行であることを確認し、`ssh.ParseAuthorizedKey` で検証。複数行は黙殺せず reject

- [x] `readPubKey` で空行除去後、非空行が1行であることを確認（2行以上はエラー）
- [x] `ssh.ParseAuthorizedKey` でフォーマット検証
- [x] `resolveKey()` 経由の鍵も同じ検証パスを通す（将来の ssh-agent 入力も統一）
- [x] `readPubKey` の正常系・異常系テストを追加する

**変更対象ファイル**: `main.go`, `main_test.go`

### 11-3: 管理者鍵ファイル判定の一般化（完了）

**問題**: `administrators_authorized_keys` 判定が `Match Group administrators` ブロック内の `AuthorizedKeysFile` だけに依存している

**方針**: `sshd -T` 優先 → 現行パースにフォールバック → 確信なければ user 側配備 + 警告

- [x] `useAdminKeyFile` を bool → 構造体（配備先パス + 判定理由）に設計変更
  > `useAdminKeyFile(client) bool` → `resolveKeyFileTarget(client) (keyFileTarget, error)` に置換。`keyFileTarget{isAdmin bool, reason string}` で配置先と判定理由を保持。
- [x] `sshd -T` をリモート実行し `authorizedkeysfile` の実効値を取得する処理を追加
  > `sshd -T -C "user=$u,host=localhost,addr=127.0.0.1"` で Match 評価済み実効値を取得。`effectiveAdminKeysFromSshdT(output)` pure 関数で解析（大小無視・CRLF 対応）。
- [x] `sshd -T` 失敗時は現行の `sshd_config` パースにフォールバック
  > `SSHD_T_OK` マーカー未検出・`runRemotePowerShell` エラー時に既存の `Get-Content` + ステートマシンパースへ移行。
- [x] 確信できない場合は user 側に配備 + 明示警告（「成功に見えてログインできない」を回避）
- [x] Linux 接続時は早期エラー（Windows 専用であることを明示）— Gemini 指摘対応
  > `looksLikeNonWindows(output)` pure 関数で "command not found" / "not recognized as" 等のシグナルを検出し error を返す。
- [x] 構成差分をカバーするテストを追加する
  > `TestEffectiveAdminKeysFromSshdT`（admin/user/マーカーなし/CRLF/大文字小文字）、`TestLooksLikeNonWindows` を `deploy_test.go` に追加。

**変更対象ファイル**: `deploy.go`, `deploy_test.go`, `integration_test.go`

## 将来の改善候補

### 配布チャネル整備（GoReleaser 採用）（マージ済み — PR #11 / MR !14、次タグから有効）

配布を見据え、GitHub リリースを GoReleaser 化（手動 `gh release create` から移行）。

- [x] **方針決定**: GitHub canonical（GitHub のみ GoReleaser）。GitLab は `.gitlab-ci.yml` 据え置きで生バイナリのまま（非対称を許容）。
- [x] スコープ: **アーカイブ + checksums のみ**。`.goreleaser.yaml`（builds 5 ターゲット／windows-arm64 除外、tar.gz・windows=zip、`checksums.txt`、`changelog: disable`、`release.github`）+ `release.yml` を goreleaser-action 化。
- [x] 挙動保持: リリースノートは `CHANGELOG.md` から `--release-notes`、バージョンは `-X main.version={{ .Tag }}`。
- [x] ローカル検証（マージゲート）: goreleaser v2.16.0 で `check` ✅ / `release --snapshot --clean` → 5 アーカイブ + checksums ✅ / `--version` で ldflags 注入確認 ✅。
- [x] master へマージ（2026-06-20）。次タグ（v1.8.0 等）を打った時点で自動的に GoReleaser 経由のリリースに切り替わる（次回リリースで自然消化）。
- [ ] **後段（今回スコープ外）**: Homebrew tap / Scoop bucket。採用時は tap/bucket リポジトリ作成・トークン設定・README DL 導線更新を併せて決める。

### デフォルト公開鍵の探索強化（ssh-copy-id 互換）（完了）

`-i` 未指定時に ssh-agent → `~/.ssh/id_*.pub` (mtime最新) の順で自動探索するように変更。

- [x] `ssh-add -L` 実行 → 出力があればその鍵を使用
- [x] エージェント未使用/鍵なしの場合、`~/.ssh/id_*.pub` を glob → mtime 最新を選択
- [x] どちらも見つからない場合はエラー
- [x] `main.go` の `defaultPubKeyPath()` を `resolveDefaultKey()` に置き換え
- [x] `main_test.go` に `parseSshAddOutput` / `findNewestPubKeyIn` のユニットテスト追加

### Step 12: 信頼性向上（dry-run / timeout / icacls エラー伝搬）（完了）

PLAN.md「将来の改善候補」の3項目をまとめて1リリースとして実装。新依存なし。
**§B（`~/.ssh/config` 解決）と合わせて v1.6.0 としてリリース済み（2026-06-20、GitHub/GitLab 両方）。** PR #6 / MR !9。

#### Dry-run モード（`-n` フラグ）（完了）

`ssh-copy-id -n` 互換。鍵を実際に配置せず、何が行われるかをプレビュー表示する。

- [x] `-n` / `--dry-run` フラグ追加（`main.go`、`-h` と同じ並行登録パターン）
- [x] 鍵読み込み・SSH接続・Admin判定まで実行し、配置先パスを表示
  > dry-run でも配置先・重複判定はリモートで行うため SSH 接続・パスワード入力は発生する（usage/README/CHANGELOG に明記）。
- [x] 実際のファイル書き込み・ACL設定・ディレクトリ作成はスキップ
  > 別スクリプトを作らず `buildDeployScript` に `$dryRun` を注入し、書き込み前に `exit 0` ガード。パス決定・重複ロジックを共有しドリフトを防ぐ。
- [x] 重複チェック結果も表示（`DRY_RUN_DUP:True/False` → 「既に配置済み」or「新規追加予定」）

#### エラーメッセージの局所化（icacls 出力の伝搬）（完了）

PowerShell 側でエラー発生時に `ACL_SET_FAILED_DIR` などのマーカーを返しているが、`icacls` が出力した実際のエラーメッセージが Go 側に伝搬されていない。

- [x] PowerShell スクリプト内で `icacls` の出力を `2>&1` で捕捉しマーカーに `|<実エラー>` で付加
- [x] Go 側で `extractAclErrorDetail` が詳細メッセージを抽出しエラーに含める
- [x] `deploy_test.go` にエラーメッセージ伝搬のテスト追加
- [x] PS 5.1 の NativeCommandError 回避: icacls 区間を `ErrorActionPreference='Continue'` に切替
  > Stop のまま native コマンドの stderr を `2>&1` するとマーカー出力前に終端エラーで throw する罠を回避。実機（kiwar@Windows）で実エラー `The system cannot find the path specified.` の伝搬を確認。

#### 接続タイムアウト（完了）

- [x] `ssh.ClientConfig.Timeout = 30s`（`ssh.Dial` → `net.DialTimeout`、TCP dial 段階のハングを防止）
  > ハンドシェイク/認証段階は対象外だが、到達不能ホストの長時間待ち防止という主目的には十分。

### 重複判定の鍵 blob 単位比較化（Codex 指摘）（完了）

現在の `Select-String -SimpleMatch -Pattern $pubKey` は行全体一致のため、同じ鍵本体でもコメント違いだと重複検知できない。

- [x] 鍵 blob（type + base64 部分）のみで比較するように変更
  > Go 側で `pubKeyBlob()`（`ssh.ParseAuthorizedKey` → `ssh.MarshalAuthorizedKey`）が blob を抽出し `buildDeployScript` に渡す。PS 側は `Select-String` を廃止し、行ごとに `\s+` で分割したトークンを2つずつスライドさせ `(type, base64)` ペアを `-ceq`（base64 は大小区別が必要）で blob 比較。空行・`#` 行は skip。
  > options 前置行（`command="..." ssh-rsa AAAA...`）でも、スライド窓により `(type, base64)` ペアが正しく検知される（Codex P2 で旧 `Select-String` からのデグレ指摘 → 修正済み）。
- [x] テスト追加（`TestPubKeyBlob` / `_Invalid`、`deploy_test.go` を `-ceq $keyBlob` 検証に更新、`TestIntegration_DuplicateByBlob` に options 前置行ケースを追加）

### known_hosts 書き込み競合の対策（Gemini・Codex 共通指摘）（完了）

`known_hosts` への追記・書き換えにロック制御がなく、複数プロセス同時実行で破損し得る。

- [x] ファイルロック（`flock` / `LockFileEx` 相当）の導入検討 → **不採用**
  > 破損を起こすのは truncate+write の `replaceHostKeyInKnownHosts` のみ。TOFU 追記は既に `O_APPEND` で原子的。flock は新依存／build-tag のプラットフォーム別コードを招き、非ロックの書き手（実 `ssh`）も防げず過剰。
- [x] atomic temp+rename を採用（依存ゼロ・OpenSSH 自身の known_hosts 更新と同方式）
  > `atomicWriteFile()`（同一ディレクトリ temp → `os.Rename`）で `replaceHostKeyInKnownHosts` の書き戻しを置換。読み手は常に旧 or 新の完全なファイルのみを見る。残る race は同時 yes 時の lost-update（良性・次回 TOFU で自己修復）として受容。
  > テスト: `TestAtomicWriteFile_CreatesAndOverwrites`。

### [標準準拠・セキュリティ監査 2026-05-29] 要検討事項

本家 `ssh-copy-id`・標準との網羅性監査（機能面・セキュリティ面）で、結論を保留した「要検討」事項が2件。
詳細レポート: `~/.claude/plans/tingly-sauteeing-quail.md`。軽微な改善候補（鍵 blob 比較は上記既出、`.pub` 自動補完、cert 除外、末尾改行保証、keyboard-interactive フォールバック）はレポート §3 を参照。

#### A. ホスト鍵変更時の挙動を OpenSSH 準拠にするか（セキュリティ・主要争点）

**現状**: known_hosts と不一致を検知すると MITM 警告後に `yes/no` で known_hosts を上書き更新できる（`ssh.go:118-145`）。本家 OpenSSH は**ハード拒否**し手動修正を要求する。password 送信予定の接続上で対話的上書きを許す点が OpenSSH より緩い。

**判断材料（両論、結論保留）**:
- A案 現状維持（更新を提案）: 正当な鍵ローテに即対応・ワンコマンド志向に合致／緩い
- B案 OpenSSH 準拠（拒否）: MITM 耐性最大／鍵ローテのたび手動削除
- 折衷案: 既定は拒否 + `--update-known-hosts` 等の明示フラグでのみ更新許可

- [x] 方針決定（A / B / 折衷）→ **A案 現状維持を採用**（2026-06-20、ユーザー判断）
  > 理由: ワンコマンド志向・便利さを優先。正当な鍵ローテに対話的に即対応できる現挙動を維持する。
- [x] 決定に応じて `ssh.go` 変更 → 変更不要（現状維持のため）

#### B. `~/.ssh/config` 解決（完了 — v1.6.0 / PR #7 / MR !10）

**当初の課題**: `dialSSH` が `ClientConfig` を素から構築するため、`ssh-pushkey myalias` の Host alias / 個別 User・Port が効かなかった。クライアントは Linux/Mac/WSL 上で動くため実害のある機能ギャップだった。

**方針決定**: 外部依存 `kevinburke/ssh_config` を導入し、Host alias → `HostName` / `User` / `Port` を解決する（ユーザー判断）。

- [x] `kevinburke/ssh_config` 導入。`~/.ssh/config` を自前 `Decode` → `cfg.Get` で解決（未設定キーは `""` 返却のクリーンなセマンティクス）
- [x] `resolveConnection(get, rawTarget, cliPort, portExplicit)` 新設（`parseTarget` を置換）。`get` を DI してテスト可能化
- [x] 優先順位 CLI（`user@` / `-p` を `flag.Visit` で検出）> ssh_config > 既定 22
- [x] known_hosts は解決後の `HostName` でキーイング（`dialSSH` に解決後 host を渡す既存経路のまま）
- [x] 非対応を明記: `ProxyJump` / `IdentityFile`（`-i` は配置鍵で別物）/ `HostKeyAlias` / `Match`。config 不在・parse エラーは graceful degrade（後者は stderr 警告）
- [x] テスト追加（後方互換・`flag.Visit` 両方向・alias 解決・不正 Port フォールバック）、実バイナリで alias→`admin@10.0.0.5:2222` と `-p` 上書きを確認

## 開発環境: 統合テスト実行の簡略化（2026-06-21）

### Context

統合テスト（`go test -tags=integration`）の実行が手間（毎回 `read -rs SSH_TEST_PASSWORD` で
パスワード手入力、`.env.integration` の `SSH_TEST_HOST` が古い `192.168.1.10` のまま等）。
**専用テストアカウント + 認証情報を gitignore 済みファイルに保存 + Makefile 一発化**で簡略化する
方針をユーザーが選択。さらに管理者・一般の**両アカウント**を作って admin/user 両配置パスを網羅する。

### 完了済み（ブランチ `chore/add-makefile`、master 起点 / #4 とは分離）

- [x] `Makefile` 新規作成。`build` / `vet` / `test` / `itest`(=itest-admin) / `itest-user` /
  `itest-all` / `check`(PR 前チェック一式) / `help`。
  > `itest-*` は env ファイルを自分で `source` するため手動 source 不要。`RUN=<TestName>` で絞り込み可。
  > 認証情報未設定・ファイル欠如時はガードで明示エラー。
- [x] `.gitignore` を `.env.integration` → `.env.integration*` に変更（admin/user 両ファイルを無視）。
- [ ] 両リモートへ push + Draft PR/MR 作成（このセッションで実施予定）。
- [ ] レビュー後マージ（デュアルリモート: ローカル `--no-ff` マージ → origin/gitlab 両 push）。

### 再開時の残タスク（ユーザー手元の Windows 操作 → 検証）

1. **Windows 側で専用テストアカウント2つを作成**（管理者 PowerShell）:
   ```powershell
   # 管理者アカウント
   $pw1 = Read-Host -AsSecureString "pushadmin のパスワード"
   New-LocalUser -Name "pushadmin" -Password $pw1 `
     -FullName "ssh-pushkey integration (admin)" -Description "loopback-only test account"
   Add-LocalGroupMember -Group "Administrators" -Member "pushadmin"

   # 一般アカウント（Administrators には入れない）
   $pw2 = Read-Host -AsSecureString "pushuser のパスワード"
   New-LocalUser -Name "pushuser" -Password $pw2 `
     -FullName "ssh-pushkey integration (user)" -Description "loopback-only test account"
   ```
2. **認証情報ファイルを作成**（いずれも gitignore 済み・平文保存だが loopback 専用の使い捨てアカウント）:
   - `.env.integration`（管理者）:
     ```bash
     export SSH_TEST_HOST=127.0.0.1
     export SSH_TEST_USER=pushadmin
     export SSH_TEST_PASSWORD='手順1のパスワード'
     ```
   - `.env.integration.user`（一般）: 上記の USER/PASSWORD を `pushuser` 用に。
3. **検証**: `make itest`（管理者）/ `make itest-user`（一般）/ `make itest-all`（両方）が全 PASS すること。
   > 一般アカウントでは `administrators_authorized_keys` ではなく `~\.ssh\authorized_keys` 経路を通る。
4. **chore/add-makefile をマージ**（レビュー後）。

### 関連: Issue #4（sshd -T -C を SSH_CONNECTION から導出）

- 実装・実機検証完了。PR #13 / MR #16（Draft）。実機（127.0.0.1）で SSH_CONNECTION 伝播・CLIXML 耐性・
  `laddr`/`lport` 込みスペックの sshd -T 受理を確認済み。Match ルール下の誤判定解消そのものは
  loopback では検証不能（correct-by-construction）。マージはユーザー承認後。
  > **`addr=` の未検証ギャップは Issue #17 で独立追跡** → **完了（2026-06-21）**。
  > 非 loopback クライアント（Mac `192.0.2.10` → Windows `192.0.2.20`）から `Match Address`
  > 依存 sshd_config に対し旧バイナリで誤判定（admin パス選択）・新バイナリで正解（user パス選択）を
  > 実機で観測。手順書は [`docs/match-address-verification.md`](docs/match-address-verification.md) に
  > 再現可能化済み。

### 10-6: Issue #17 e2e 検証手順の記録（完了）

非 loopback クライアントでの Match Address 誤判定解消を実機で確認し、手順を再現可能な形で記録した。

- [x] 検証台 sshd_config（`Match Address <client-ip>` → `Match User pushadmin` の順）
  > Windows の `sshd -T -C user=...` ではグループ解決が効かず `Match Group administrators` が
  > 発火しないため、admin baseline は `Match User pushadmin` に置換。Match 評価は **first-wins** のため、
  > 実IP用の `Match Address` を必ず先頭に置く。
- [x] 事前確認: `sshd.exe -T -C "addr=127.0.0.1"` と `addr=192.0.2.10` で `AuthorizedKeysFile` が
  割れること（旧コードなら admin / 新コードなら user）を `sshd -T` 直接実行で確認。
- [x] 修正前バイナリで admin パス選択を再現（バグ再現）、修正後バイナリで user パス選択を確認。
- [x] 手順を [`docs/match-address-verification.md`](docs/match-address-verification.md) に保存。

**ハマりどころ**: 配置先に既存鍵が残ると deploy 自体がスキップされ、修正の効果が観測できない。
検証前に配置先（user 側 / admin 側両方）の鍵を消したクリーンな状態から行うこと。

**Issue**: https://github.com/kwrkb/ssh-pushkey/issues/17

## リポジトリ

- [x] `git init` + リモート設定: `gitlab.com/kwrkb/ssh-pushkey`
  > モジュールパスを `github.com/user` → `gitlab.com/kwrkb` に変更
