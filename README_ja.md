# ssh-pushkey

[English](README.md)

Windows OpenSSH サーバー向けの `ssh-copy-id` 代替ツール。

パスワード認証で接続し、公開鍵を自動配置する。Windows 特有の罠（BOM、Admin 分岐、ACL）をすべて処理する。`.ssh` ディレクトリと鍵ファイルの両方に Well-known SID を使った言語非依存の ACL を設定する。

## デモ

![ssh-pushkey demo](demo/demo.gif)

## インストール

[GitLab Releases](https://gitlab.com/kwrkb/ssh-pushkey/-/releases) / [GitHub Releases](https://github.com/kwrkb/ssh-pushkey/releases) からバイナリをダウンロード、または:

```bash
go install gitlab.com/kwrkb/ssh-pushkey@latest
```

## 使い方

```bash
ssh-pushkey user@host
```

パスワードを入力すれば、あとは全自動。

### デフォルト鍵の自動探索

`-i` 未指定時、`ssh-copy-id` と同じロジックで公開鍵を自動探索する:

1. **ssh-agent** — `ssh-add -L` を実行し、鍵があれば最初の鍵を使用
2. **ファイルフォールバック** — `~/.ssh/id_*.pub` を glob し、更新日時が最新のものを選択
3. どちらにも鍵がなければエラー終了

対応鍵タイプ: `ed25519`, `rsa`, `ecdsa`, FIDO/U2F (`sk-ssh-ed25519`, `sk-ecdsa-sha2-nistp256`)

### オプション

| フラグ | デフォルト | 説明 |
|--------|-----------|------|
| `-i` | *（自動探索）* | 公開鍵ファイルのパス |
| `-p` | `22` | SSH ポート番号 |
| `--insecure` | `false` | ホスト鍵検証をスキップ（非推奨） |
| `--version` | - | バージョン表示 |

### 例

```bash
# 鍵を自動探索して配置
ssh-pushkey admin@192.168.1.10

# 鍵とポートを指定
ssh-pushkey -i ~/.ssh/id_rsa.pub -p 2222 user@server
```

## 何をしてくれるのか

1. パスワード認証で SSH 接続
2. Administrators グループかどうかを判定
3. `sshd_config` で `administrators_authorized_keys` が有効か確認
4. 適切なファイルに公開鍵を BOM なし UTF-8 で書き込み
5. `.ssh` ディレクトリと鍵ファイルの両方に `icacls` で Well-known SID ベースの ACL を設定（`SYSTEM`、`Administrators`、現在のユーザー）

| ユーザー種別 | 配置先 |
|-------------|--------|
| Admin（`administrators_authorized_keys` 有効） | `C:\ProgramData\ssh\administrators_authorized_keys` |
| Admin（無効）/ 一般ユーザー | `~\.ssh\authorized_keys` |

## ビルド

```bash
go build -ldflags "-X main.version=$(git describe --tags --abbrev=0)" -o ssh-pushkey
```

## セキュリティ

### ホスト鍵検証

デフォルトで `~/.ssh/known_hosts` を使ってリモートホストの鍵を検証する（OpenSSH と同じ挙動）。初回接続時はフィンガープリントを表示して確認を求め（Trust on First Use）、承認すると `known_hosts` に自動追記する。

ホスト鍵が前回と異なる場合はプロンプトで更新を確認する。正当な鍵ローテーションに対応しつつ、MITM 攻撃の可能性も警告する。

ハッシュ化 known_hosts エントリ（`HashKnownHosts yes`）にも完全対応 — マッチングも書き込みもハッシュ形式を維持する。

`--insecure` でホスト鍵検証をスキップできるが、パスワードが中間者攻撃で漏洩するリスクがあるため **非推奨**。

### ACL 設定

ACL エントリはローカライズされたグループ名ではなく Well-known SID（`S-1-5-18`: SYSTEM、`S-1-5-32-544`: Administrators）を使用する。非英語版 Windows やドメイン環境でも正しく動作する。

## テスト

### ユニットテスト

```bash
go test ./...
```

### インテグレーションテスト

インテグレーションテストは実際の Windows OpenSSH サーバーに接続する。`integration` ビルドタグで分離されており、環境変数が未設定の場合はスキップされる。

**セットアップ:**

1. env ファイルをコピーして編集:
   ```bash
   cp .env.integration.example .env.integration
   # .env.integration のホスト名・ユーザー名を編集
   ```

2. パスワードを設定（ファイルに保存しない）:
   ```bash
   read -rs SSH_TEST_PASSWORD && export SSH_TEST_PASSWORD
   ```

3. 実行:
   ```bash
   source .env.integration && go test -tags=integration -v ./...
   ```

**環境変数:**

| 変数 | 必須 | 説明 |
|------|------|------|
| `SSH_TEST_HOST` | Yes | Windows SSH サーバーの IP またはホスト名 |
| `SSH_TEST_USER` | Yes | SSH ユーザー名 |
| `SSH_TEST_PASSWORD` | Yes | SSH パスワード（`read -rs` で設定推奨） |
| `SSH_TEST_PORT` | No | SSH ポート（デフォルト: 22） |
| `SSH_TEST_PUBKEY` | No | 公開鍵パス（デフォルト: `~/.ssh/id_ed25519.pub`） |

## 変更履歴

[CHANGELOG.md](CHANGELOG.md) を参照。

## License

MIT
