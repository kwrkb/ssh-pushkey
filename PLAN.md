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

**v1.8.0 リリース済み（2026-08-11）。WinGet の upstream マージ確認のみ残（下記）。**

Step 1〜12（初期実装 / ACL 準拠 / ホスト鍵検証 / 全体レビュー対応 / 信頼性向上）および
Issue #4・#16・#17 は完了。`CHANGELOG.md` に各リリースの内容、`LESSONS.md` に判断の記録がある。
完了ステップの詳細な手順履歴は git 履歴に残っているため本ファイルからは削除した（2026-08-10）。

開発環境も整備済み: `Makefile` が正規の入口、統合テストは 2 アカウント（管理者 / 一般）で実行可能、
GitHub Actions の CI が PR / master push で `make check` 相当を実行する。

## 次のフェーズ候補

### 配布チャネルの追加 — Scoop を採用（2026-08-12。WinGet から切替）

GoReleaser によるアーカイブ + checksums 配布は導入済み（v1.7.1〜）で、導線は
GitHub Releases（正本）+ `go install` に整理済み。その後段として **Scoop** を追加する。

クライアント環境で切ると、`ssh-copy-id` が**存在しない**のは Windows だけ（WSL/Linux/macOS には
あり、Windows 非対応なだけ）。代替不在の層に届けるのが目的なので Windows 向けチャネルを選ぶ。

当初は発見性（素の `winget search` に載る）を理由に WinGet を採ったが、v1.8.0 の manifest PR が
`microsoft/winget-pkgs` の検証で Defender 誤検知に止められた。本ツールの挙動は
ヒューリスティックにはバックドア設置と区別がつかず再発が見込まれ、毎リリースが外部の審査に
依存する。発見性より**リリースが他者の審査で止まらないこと**を優先し Scoop に切り替えた。
判断の詳細は `LESSONS.md`。

- [x] `kwrkb/scoop-bucket`（public）作成と PAT の secret 登録
- [x] `.goreleaser.yaml` を `winget:` から `scoops:` へ差し替え、`release.yml` のトークンを配線
- [x] README / README_ja のインストール導線を Scoop に差し替え
- [ ] 次の `v*` タグで bucket に manifest が push されることを確認する
      （`goreleaser release --snapshot` で manifest 生成までは検証済み）
- [ ] 上記確認後、README / README_ja の「次のリリース以降で利用可能」注記を外す

不採用: WinGet（上記。誤検知が解消し、かつ再発しないと確認できたら再検討）、
Homebrew（`brews:` は GoReleaser v2.16 で hard deprecation ＝ `goreleaser check` が失敗し
formula は選べない。`homebrew_casks:` は確実に効くのが macOS のみで、そこは既に `go install` /
tarball がある層）。

## リポジトリ

- モジュールパス: `github.com/kwrkb/ssh-pushkey`
- デュアルリモート: `origin` = GitHub（プライマリ・canonical な配布元）、`gitlab` = GitLab（サブ）
