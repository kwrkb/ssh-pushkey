# Implementation Notes（意思決定ログ）

## Issue #4: sshd -T -C の client address を SSH_CONNECTION から導出 (2026-06-21)

### パースを PowerShell ではなく Go 側に置いた
- 候補: (A) PowerShell ネイティブで `$env:SSH_CONNECTION` を分解してスペック生成（1ラウンドトリップ）、(B) SSH_CONNECTION を取得して Go でスペック生成（ラウンドトリップ追加）。
- (B) を採用。Issue が「パースロジックのユニットテスト」を明示要求しており、Match ルールは非 loopback ホストが必要で e2e 検証不能。Go の純粋関数なら CI で検証できる。ラウンドトリップ1回の追加コストは許容。

### host も addr と同じ実 client IP に設定した（省略しない）
- Issue 制約「real addr と fake host=localhost を混ぜるな」「古い sshd は user/host/addr 必須」の両方を満たす。
- host を実 IP で供給することで sshd バージョン依存（host 必須要件）を回避。`Match Host <exact-ip>` 誤発火リスクは実質ゼロ（IP には通常 `Match Address` を使う）。
- `laddr`/`lport` も SSH_CONNECTION の server 側から導出し、Match LocalAddress/LocalPort にも対応。

### 実機検証で CLIXML 汚染を発見し、行スキャン方式へ修正（当初実装のバグ）
- 当初は `strings.TrimSpace(connOut)` を `strings.Fields` で分解していたが、実機プローブで `Write-Output $env:SSH_CONNECTION` の出力に CLIXML（`#< CLIXML` ヘッダ + `<Objs>` XML）が混入することが判明。フィールド数が4超になり常にフォールバック＝ silent no-op だった。
- `parseSSHConnectionSpec`（単一値の検証・スペック生成）と `sshdMatchSpecSuffixFromOutput`（出力を行スキャンして一致行を抽出）に分割。`effectiveAdminKeysFromSshdT` と同じ行スキャン方針に揃えた。詳細は LESSONS.md。

### 実機検証で確認できたこと / できなかったこと
- 確認済み（loopback 実機 127.0.0.1）: SSH_CONNECTION は EncodedCommand セッションで populate される / CLIXML 混入下でも実スペックを抽出できる / 実機 sshd が `laddr`/`lport` 込みスペックを exit 0 で受理する（フォールバックに落ちない）。
- 未確認（loopback では不可能）: `Match Address`/`Match Host` ルール下での誤判定の実際の解消。loopback クライアントでは新スペックの `addr=127.0.0.1` が旧固定値と機能的に同一のため、本来のバグシナリオは exercise されない。修正は correct-by-construction。非 loopback クライアントでの検証は今後の課題。
