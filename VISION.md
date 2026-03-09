# VISION

## Why

Windows OpenSSHへのSSH鍵登録がストレスすぎる。

`ssh-copy-id` はLinux/macOS向けで、Windowsの以下の仕様に対応していない:

- BOMなしUTF-8でないと鍵が認識されない
- Administratorsグループのユーザーは `~/.ssh/authorized_keys` ではなく `C:\ProgramData\ssh\administrators_authorized_keys` に置く必要がある
- `icacls` でACLを正しく設定しないと鍵認証が無視される

これらを毎回手動でやるのは苦痛。ワンコマンドで終わらせたい。

## What

`ssh-pushkey user@host` — これだけでWindows OpenSSHサーバーに公開鍵を登録できるCLIツール。

## Principles

- **ワンコマンド**: パスワードを入力したら、あとは全自動
- **Windows特有の罠を全部処理**: BOM、Admin分岐、ACL — ユーザーが意識する必要なし
- **シンプル**: 必要最小限の機能だけ。設定ファイルなし、依存最小限
