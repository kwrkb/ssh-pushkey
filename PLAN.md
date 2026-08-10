# ssh-pushkey 実装計画

## Context

Windows OpenSSH サーバー向けの `ssh-copy-id` 代替 CLI ツール。
Linux/macOS の `ssh-copy-id` が Windows OpenSSH 特有の仕様（BOM なし UTF-8、Administrators 問題、ACL 設定）に
対応していないため、これらを自動処理する Go ツールを実装する。目的・スコープの正典は [VISION.md](VISION.md)。

## ファイル構成

```
ssh-pushkey/
├── main.go              # CLI 引数パース、~/.ssh/config 解決、デフォルト鍵探索
├── ssh.go               # SSH 接続・known_hosts 検証・リモートコマンド実行
├── deploy.go            # 鍵配置ロジック（Admin 判定、ACL、重複チェック）
├── *_test.go            # ユニットテスト
├── integration_test.go  # 統合テスト（build tag: integration）
└── docs/                # 検証記録（match-address-verification.md）
```

## 現在地

**v1.7.2 リリース済み。未着手のフェーズなし。**

Step 1〜12（初期実装 / ACL 準拠 / ホスト鍵検証 / 全体レビュー対応 / 信頼性向上）および
Issue #4・#16・#17 は完了。`CHANGELOG.md` に各リリースの内容、`LESSONS.md` に判断の記録がある。
完了ステップの詳細な手順履歴は git 履歴に残っているため本ファイルからは削除した（2026-08-10）。

開発環境も整備済み: `Makefile` が正規の入口、統合テストは 2 アカウント（管理者 / 一般）で実行可能、
GitHub Actions の CI が PR / master push で `make check` 相当を実行する。

## 次のフェーズ候補（未着手）

### 配布チャネルの追加（Homebrew tap / Scoop bucket）

GoReleaser によるアーカイブ + checksums 配布は導入済み（v1.7.1〜）。その後段。

- [ ] 採用可否の判断。採用する場合は tap / bucket リポジトリ作成・トークン設定・README のダウンロード導線更新を併せて決める

## リポジトリ

- モジュールパス: `github.com/kwrkb/ssh-pushkey`
- デュアルリモート: `origin` = GitHub（プライマリ・canonical な配布元）、`gitlab` = GitLab（サブ）
