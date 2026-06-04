# LESSONS

## PowerShellリモート実行の修正 (2026-03-09)

### Windows SSH経由のPowerShellコマンドは-EncodedCommandを使う
- Windows OpenSSHのデフォルトシェルがPowerShellの場合、`powershell -Command "$var = ..."` で送信すると外側のPowerShellが `$var` を変数展開してしまい、コマンドが壊れる。セミコロン区切りの複合文も正しくパースされない
- **ルール**: SSH経由でPowerShellスクリプトを実行する際は、スクリプトをUTF-16LEエンコード→Base64化し、`powershell -NoProfile -EncodedCommand <base64>` で実行する。`-Command` に直接文字列を渡さない

### シェルのネスト問題を意識する
- `runRemoteCommand` で送信するコマンドは、リモートのログインシェルによって一度解釈される。Windows OpenSSHではログインシェルがPowerShellの場合があり、`powershell -Command "..."` はPowerShell→PowerShellの二重解釈になる
- **ルール**: リモートコマンド実行時は「ログインシェルが何か」を意識し、変数展開・クォート・特殊文字の二重解釈が起きないエンコード手法を使う

### Select-Stringの-SimpleMatchと[regex]::Escape()を併用しない
- `Select-String -SimpleMatch -Pattern ([regex]::Escape($pubKey))` で重複チェックしたところ、`[regex]::Escape` が `-` を `\-` に変換し、`-SimpleMatch` がそれをリテラル `\-` として検索するため一致しなかった。結果、鍵が何度も重複登録された
- **ルール**: `-SimpleMatch` はリテラル検索なのでエスケープ不要。`$pubKey` をそのまま渡す。正規表現モードを使うなら `-SimpleMatch` を外して `[regex]::Escape()` を使う。両方同時に使わない

### PowerShellのCLIXML出力がstdoutに混入する
- `-EncodedCommand` 経由でもPowerShellのモジュール初期化時にCLIXMLプログレスメッセージ（`#< CLIXML`）がstdoutに出力される。出力の完全一致比較が失敗する原因になる
- **ルール**: リモートPowerShellの出力を検証する際は、完全一致ではなく `strings.Contains` で判定する。または `$ProgressPreference = 'SilentlyContinue'` をスクリプト先頭に追加してプログレス出力を抑制する

## Admin判定のCLIXML問題 (2026-03-09)

### CLIXML混入ルールはすべての出力判定に一貫して適用する
- テストの出力比較は `strings.Contains` に修正済みだったが、`useAdminKeyFile` 内の `IsInRole` / `sshd_config` 判定は `strings.TrimSpace(output) == "True"` のままだった。結果、Adminユーザーが一般ユーザーと誤判定された
- **ルール**: PowerShellの出力を判定するコードを書く・修正する際は、プロジェクト内の全箇所を検索し、同じパターンの出力比較が残っていないか確認する。1箇所直したら他も直す

## Windows OpenSSH ACL準拠 (2026-03-10)

### ACLはディレクトリとファイルの両方に設定する
- Windows OpenSSHは `.ssh` ディレクトリと `authorized_keys` ファイルの両方に正しいACLを要求する。ファイルのみに `icacls /inheritance:r` を適用しても、親ディレクトリのACLが不正だと認証に失敗する
- **ルール**: Windows OpenSSHの鍵配置時は、親ディレクトリ（`$sshDir`）と鍵ファイル（`$keyFile`）の両方に `icacls /inheritance:r` + ACE付与を行う

### ACEはisAdminに関わらず統一する
- 以前はAdmin時に `SYSTEM:(F)` + `Administrators:(F)` のみ、一般ユーザー時に `SYSTEM:(F)` + `${env:USERNAME}:(F)` のみを付与していた。しかしAdminユーザーでもユーザー個別のACEが必要で、一般ユーザーでもAdministratorsグループのACEが必要
- **ルール**: ACEは常に `SYSTEM:(F)` / `Administrators:(F)` / `${env:USERNAME}:(F)` の3つを付与する。isAdminで分岐しない

### icacls等の外部コマンドは成否を検証する
- icaclsの実行結果を検証していなかったため、ACL設定が失敗してもサイレントに成功扱いになっていた
- **ルール**: PowerShellスクリプト内で外部コマンド（icacls等）を実行した後は `$LASTEXITCODE` をチェックし、失敗時はマーカー文字列を出力してGo側でハンドリングする

## Go SSH クライアントと known_hosts の互換性 (2026-03-10)

### Go の SSH クライアントは HostKeyAlgorithms を known_hosts に合わせる
- Go の `x/crypto/ssh` は known_hosts に登録済みのアルゴリズムを無視して任意のアルゴリズムでネゴシエーションする。known_hosts に `ssh-ed25519` しかないのに `ecdsa-sha2-nistp384` でネゴシエーションすると「ホスト鍵が変更された」と誤検知する
- OpenSSH は known_hosts のアルゴリズムだけをネゴシエーション対象にする
- **ルール**: Go で SSH 接続する際は known_hosts をパースして `config.HostKeyAlgorithms` を制限する。ただし鍵ローテーション時にハンドシェイクが失敗するため、制限を外してリトライするフォールバックも必要

### known_hosts の自前パースではハッシュ形式を考慮する
- `knownhosts.New()` のコールバックはハッシュ化エントリ（`|1|<salt>|<hash>`）を内部で処理するが、マッチングロジックは private で外部利用不可
- 自前で known_hosts をパースする場合、plain-text マッチング（`h == addr`）だけではハッシュ形式に対応できない
- **ルール**: known_hosts を自前パースする場合は `|1|` プレフィックスを検出し、HMAC-SHA1(salt, addr) で比較するロジックを実装する

### マルチエイリアス行の書き換えはフィールド再構成で行う
- `host,ip ssh-ed25519 AAAA...` のような行から特定ホストだけ除去する際、`strings.Replace` で行内テキスト置換すると Base64 部分に偶然一致するリスクがある
- **ルール**: known_hosts の行を書き換える際は `fields` に分割してから `fields[0]` を再構成し、`strings.Join(fields, " ")` で行を組み立てる

### ハッシュ形式の判定ロジックは全コードパスで統一する
- TOFU（初回接続）パスでは「ファイル内にハッシュエントリがあればハッシュ化」、鍵更新パスでは「置換対象がハッシュならハッシュ化」と判定基準がズレていた。Gemini レビューで発見
- **ルール**: known_hosts へのエントリ追加時、ハッシュ形式の判定基準は「ファイル内に任意のハッシュエントリが存在するか」で統一する。コードパスごとに異なる判定を入れない

## デフォルト公開鍵の探索強化 (2026-03-10)

### 関数を削除する前に全 build tag のビルドを確認する
- `defaultPubKeyPath()` を削除したが、`integration_test.go`（`//go:build integration`）がまだ参照しており、`go test -tags=integration ./...` がコンパイルエラーになった。通常の `go test ./...` では build tag 付きファイルがビルドされないため見逃した。Codex レビューで発見
- **ルール**: 関数を削除・リネームする際は `go build -tags=integration ./...` など全 build tag でビルドを確認する。`go test ./...` だけでは不十分

### ssh-add -L の出力パースでは鍵タイプのプレフィックスで判定する
- `ssh-add -L` は鍵がない場合 `The agent has no identities.` を返す。`len(fields) >= 2` だけでは誤って有効な鍵と判定してしまう。また FIDO/U2F キーは `sk-ssh-ed25519`, `sk-ecdsa-sha2-nistp256` で始まるため `ssh-` と `ecdsa-` だけでは不十分
- **ルール**: ssh 公開鍵の判定は鍵タイプ文字列のプレフィックス（`ssh-`, `ecdsa-`, `sk-ssh-`, `sk-ecdsa-`）で行う。フィールド数だけで判定しない

## 公開鍵入力の厳格化と SSH 再試行の絞り込み (2026-04-29)

### 公開鍵検証は ssh.ParseAuthorizedKey で行う
- 旧 `readPubKey` は `len(strings.Fields(key)) >= 2` のみで検証しており、複数行ファイルや base64 が壊れた `.pub` をそのまま `authorized_keys` に追記してしまう。鍵が壊れると SSH ログイン不能になる
- **ルール**: ローカルファイル・ssh-agent 出力など外部由来の公開鍵を扱う際は `golang.org/x/crypto/ssh.ParseAuthorizedKey` で必ず検証する。フィールド数チェックや自前 base64 検証で済ませない

### .pub ファイルは「非空行 1 行」を強制する
- `.pub` ファイルに複数鍵が並んでいたり、コピペ事故で別鍵が連結されていても、旧実装は無音で先頭部分だけ取り出して使ってしまう。意図しない鍵が配備される温床になる
- **ルール**: `.pub` ファイルから鍵を読み込む際は、空白除去後の非空行を数え、0 行 → 空エラー、2 行以上 → reject エラー、1 行 → `ssh.ParseAuthorizedKey` で検証、の 3 段階を必ず通す。複数鍵を黙殺せず明示拒否する

### 検証ヘルパーは入力経路をまたいで共有する
- ファイル読込（`readPubKey`）と ssh-agent 取得（`keyFromAgent`）で別々の検証パスがあると、片方だけ厳密化しても agent 経路で壊れた鍵が抜ける
- **ルール**: 同じ意味の検証を複数の入力経路で行うときは `validatePubKeyLine` のような共通ヘルパーに集約し、すべての経路から呼ぶ。経路ごとに検証を書き分けない

### TrimRight ではなく TrimSpace で正規化する
- `validatePubKeyLine` を最初 `strings.TrimRight(line, " \t\r\n")` で実装したところ、行頭スペース付き入力（`"  ssh-ed25519 ..."`）の先頭スペースが残り、`authorized_keys` に書き込まれて PowerShell の `Select-String -SimpleMatch` 重複チェックが破綻する潜在回帰になった。自己レビューで発見
- **ルール**: 公開鍵 1 行の正規化は `strings.TrimSpace` を使い、前後両方の空白を除去する。末尾だけ trim する関数を選ばない

### x/crypto/ssh のエラーは sentinel ではなく文字列で判定する
- `golang.org/x/crypto/ssh` は鍵アルゴリズム交渉失敗・認証失敗時に sentinel error を公開しておらず、`fmt.Errorf` 由来の文字列でしか区別できない。`errors.Is` / `errors.As` は使えない
- **ルール**: x/crypto/ssh のエラー分類が必要なときは `strings.ToLower(err.Error())` 後にキーワード（`"no common algorithm"`, `"unable to authenticate"` 等）を含むかで判定する。アップストリーム文言変更で破綻し得るためテストでカバーし、フォールバック挙動は保守的に「再試行しない」側に倒す

### SSH 再試行は失敗種別を絞り込む
- 旧 `dialSSH` は `HostKeyAlgorithms` 制限有時の任意の失敗で制限を外して再試行していた。認証失敗でもパスワードを再送するため、サーバー側ロックアウトと監査ログ汚染のリスクがあった
- **ルール**: SSH 再試行は「成功する見込みのある失敗」だけに絞る。`shouldRetryWithoutHostKeyAlgorithms(err)` のように判定をヘルパー関数に切り出してユニットテストで網羅する。`unable to authenticate` を含むエラーでは絶対に再試行しない

### Go ユニットテストで実鍵を使うときは固定値を const 化する
- `ssh.ParseAuthorizedKey` を通すには実 base64 で構成された正規の公開鍵が必要。テストごとに `ssh-keygen` 生成すると再現性がなく CI で揺れる
- **ルール**: フォーマット検証を伴う Go テストでは正規データを `const` で固定化し、複数テストで共有する。生成スクリプトに依存させない

## bot レビューコメント対応 (2026-05-30)

### bot 提案の「エッジケース改善」は実装前に検証可能性を確認する
- codex から sshd -T -C の `addr=127.0.0.1` 固定を実クライアントアドレスに変えるよう提案された。`Match Address` ルールが存在する環境では正しいが、(a) 実機テスト環境がloopback接続のため実シナリオを検証不可、(b) 実装に `SSH_CONNECTION` の populated 有無と `sshd -T -C` の host 省略可否の事前検証が必要、(c) `buildDeployScript` は2固定パスにしか書き込まないため改善インパクトが限定的、という3点が重なった
- **ルール**: bot 提案の改善を採用する前に「テスト環境でエンドツーエンド検証できるか」を確認する。できない場合は実装前提条件をまとめて follow-up Issue 化し、コード変更なしでスキップする

### `sshd -T -C` でアドレスを修正するとき addr と host は一貫させる
- `addr` だけ実クライアント値にして `host=localhost` を残すと、`Match Host localhost` ルールが意図せず発火し別の誤判定を引き起こす（正しい `addr` で接続中なのに `localhost` 向け設定が適用される）
- **ルール**: `sshd -T -C` のコンテキストを修正するときは `addr`/`host`/`laddr`/`lport` を同一ソース（例: `$env:SSH_CONNECTION`）から統一して派生させる。片方だけ変えない。env 未設定時は現状の `localhost`/`127.0.0.1` にフォールバック

## CLI help 改善 (2026-06-04)

### Go `flag` パッケージで `-h` ショートハンドを使うには明示的に定義する
- `flag.Bool("help", ...)` を定義すると `flag` は `-help` を処理するが `-h` は未定義扱いのまま残る。そのため `-h` を渡すと `flag: help requested` で stderr + exit 2 になる。Gemini レビューで発見
- **ルール**: `-h` も有効にする場合は `flag.BoolVar(showHelp, "h", false, "print help")` で同じポインタに紐付ける。別変数を用意して `||` で判定するより簡潔でバグが少ない

### `flag.PrintDefaults()` は `<value-name>` を出せないため help に使わない
- `flag.PrintDefaults()` は `-i string` / `-p int` のように Go の型名を出力するだけで、`-i <path>` のような意味のある value 名を表示できない。エージェント向け help として情報が不足する
- **ルール**: エージェントが読める help を作る場合は `flag.PrintDefaults()` を廃止して手書きの `const usageText` に一本化する。`flag.Usage` と明示 `--help` フラグの両方から同じ定数を参照することで二重管理を避ける

### PowerShell の新規セッションでは `$LASTEXITCODE` は `$null`（`0` ではない）
- `runRemotePowerShell` は毎回独立したセッションを開く。そこでは `$LASTEXITCODE` は `$null` から始まり、PowerShell では `$null -eq 0` が false になる。「前のコマンドが成功のまま残った `$LASTEXITCODE` が 0 だから誤動作」というシナリオは発火しない
- **ルール**: リモート PowerShell スクリプトの `$LASTEXITCODE` 依存ロジックを評価するときは、セッションが新規かどうかを確認する。新規セッションでは `$LASTEXITCODE` は `$null` なので「前コマンドの残り値」問題は起きない。`$ErrorActionPreference = 'Stop'` の追加は defense-in-depth であり、この前提を理解した上で採用する

## 依存脆弱性対応とリリース (2026-06-04)

### govulncheck は PR 前・リリース前に必ず実行する
- `golang.org/x/crypto@v0.48.0` に到達可能な脆弱性が 7 件含まれており、knownhosts の `@revoked` バイパス（GO-2026-5021）など本ツールの中核機能に直撃するものもあった。通常の `go test` / `go vet` では検出できない
- **ルール**: セキュリティ修正・依存更新・リリース前に `govulncheck ./...` を実行する。exit 0（到達可能な脆弱性 0 件）を確認してからタグを打つ

### x/crypto を上げると x/sys / x/term も連動して上がる
- `go get golang.org/x/crypto@latest` 単体では `go.sum` の他モジュールが古い状態になる場合がある。`go mod tidy` を続けて実行することで x/sys・x/term が正しいバージョンに揃う
- **ルール**: `go get <module>@latest && go mod tidy` をセットで実行する。`go get` だけで終わらない

### GitHub Actions のアクションバージョンは Node.js ランタイムに依存する
- `actions/checkout@v4` / `actions/setup-go@v5` は Node.js 20 ベースで動作し、2026-06-16 以降 Node.js 24 が強制されると動作しなくなる警告が出た。メジャーバージョン（v5→v6）で Node.js バージョンが切り替わる
- **ルール**: GitHub Actions ワークフローを触った際に deprecation 警告があれば、同じタイミングでアクションのメジャーバージョンも上げる。`gh api repos/actions/<name>/releases/latest --jq .tag_name` で最新バージョンを確認できる
