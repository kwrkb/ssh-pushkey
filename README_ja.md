# ssh-pushkey

[English](README.md)

Windows OpenSSH サーバー向けの `ssh-copy-id` 代替ツール。

パスワード認証で接続し、公開鍵を自動配置する。Windows 特有の罠（BOM、Admin 分岐、ACL）をすべて処理する。`.ssh` ディレクトリと鍵ファイルの両方に適切な ACL を設定する。

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

### オプション

| フラグ | デフォルト | 説明 |
|--------|-----------|------|
| `-i` | `~/.ssh/id_ed25519.pub` | 公開鍵ファイルのパス |
| `-p` | `22` | SSH ポート番号 |
| `--version` | - | バージョン表示 |

### 例

```bash
# デフォルトの鍵を使用
ssh-pushkey admin@192.168.1.10

# 鍵とポートを指定
ssh-pushkey -i ~/.ssh/id_rsa.pub -p 2222 user@server
```

## 何をしてくれるのか

1. パスワード認証で SSH 接続
2. Administrators グループかどうかを判定
3. `sshd_config` で `administrators_authorized_keys` が有効か確認
4. 適切なファイルに公開鍵を BOM なし UTF-8 で書き込み
5. `.ssh` ディレクトリと鍵ファイルの両方に `icacls` で ACL を設定（`SYSTEM:(F)`、`Administrators:(F)`、`User:(F)`）

| ユーザー種別 | 配置先 |
|-------------|--------|
| Admin（`administrators_authorized_keys` 有効） | `C:\ProgramData\ssh\administrators_authorized_keys` |
| Admin（無効）/ 一般ユーザー | `~\.ssh\authorized_keys` |

## ビルド

```bash
go build -ldflags "-X main.version=1.1.0" -o ssh-pushkey
```

## テスト

```bash
# ユニットテスト
go test ./...

# インテグレーションテスト（実機接続）
export SSH_TEST_HOST=192.168.1.10
export SSH_TEST_USER=user
read -rs SSH_TEST_PASSWORD && export SSH_TEST_PASSWORD
go test -tags=integration -v ./...
```

## License

MIT
