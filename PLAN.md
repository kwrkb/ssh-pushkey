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

**v1.7.3 リリース済み（2026-08-11）。未着手のフェーズなし。**

Step 1〜12（初期実装 / ACL 準拠 / ホスト鍵検証 / 全体レビュー対応 / 信頼性向上）および
Issue #4・#16・#17 は完了。`CHANGELOG.md` に各リリースの内容、`LESSONS.md` に判断の記録がある。
完了ステップの詳細な手順履歴は git 履歴に残っているため本ファイルからは削除した（2026-08-10）。

開発環境も整備済み: `Makefile` が正規の入口、統合テストは 2 アカウント（管理者 / 一般）で実行可能、
GitHub Actions の CI が PR / master push で `make check` 相当を実行する。

## 次のフェーズ候補

### 配布チャネルの追加 — WinGet を採用、Homebrew / Scoop は不採用（2026-08-11）

GoReleaser によるアーカイブ + checksums 配布は導入済み（v1.7.1〜）で、導線は
GitHub Releases（正本）+ `go install` に整理済み。その後段として **WinGet** を追加する。

クライアント環境で切ると、`ssh-copy-id` が**存在しない**のは Windows だけ（WSL/Linux/macOS には
あり、Windows 非対応なだけ）。代替不在の層に届けるのが目的なので Windows 向けチャネルを選ぶ。
個人 bucket / tap は `scoop bucket add` / `brew tap` を先に踏ませる必要があり README を読んだ人に
しか届かないため、素の `winget search` に載る WinGet を採る。判断の詳細は `LESSONS.md`。

- [x] `microsoft/winget-pkgs` の fork 作成と PAT の secret 登録（手作業）
- [x] `.goreleaser.yaml` に `winget:` を追加し、`release.yml` にトークンを配線
- [x] README / README_ja のインストール導線に WinGet を追記
- [ ] 次の `v*` タグで実際に PR が作られ、`microsoft/winget-pkgs` にマージされるまで確認する
      （`goreleaser release --snapshot` で manifest 生成までは検証済み。upstream の検証パイプラインは実タグでしか通らない）

不採用: Homebrew（`brews:` は GoReleaser v2.16 で hard deprecation ＝ `goreleaser check` が失敗し
formula は選べない。`homebrew_casks:` は確実に効くのが macOS のみで、そこは既に `go install` /
tarball がある層）、Scoop（発見性が無く、コストは WinGet と同額）。

## リポジトリ

- モジュールパス: `github.com/kwrkb/ssh-pushkey`
- デュアルリモート: `origin` = GitHub（プライマリ・canonical な配布元）、`gitlab` = GitLab（サブ）
