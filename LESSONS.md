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

### GitLabリポジトリのPRは glab を使う
- `gh pr create` はGitHub APIを使うため、GitLabリポジトリでは `Head sha can't be blank` エラーで失敗する。GitHubミラーがあっても実体がGitLabなら `glab` を使う必要がある
- **ルール**: リモートURLが `gitlab.com` の場合は `glab mr create` を使う。`gh pr create` は使わない

### GitLabリリースは自動作成されない
- GitHub Actions はタグ push で自動リリース（バイナリ付き）を作成するが、GitLab 側にはリリースが自動作成されない。GitLab はミラー元なのにリリースが欠落する状態になる
- **ルール**: タグ push 後、`glab release create vX.Y.Z` で GitLab リリースも手動作成する。GitHub Actions の完了を待つ必要はない（並行して作成可能）

## OSSメッセージの国際化 (2026-03-10)

### ユーザー向けメッセージは英語にする
- OSSとして公開するツールのユーザー向けメッセージ（フラグ説明、ステータス、エラー）が日本語だと、日本語話者以外が使えない
- **ルール**: ユーザーに表示されるメッセージは英語で統一する。コード内コメントは日本語でもOK（開発者向け）。日本語ユーザー向けドキュメントは `README_ja.md` 等で別途提供する

### メッセージ変更時はテストへの影響を事前に確認する
- メッセージを変更する際、テストがメッセージ文字列をアサートしていると全て壊れる。ssh-pushkeyでは `deploy_test.go` がPowerShellスクリプト内容の `strings.Contains` チェックのみだったため影響なしだった
- **ルール**: メッセージ変更前に `grep` でテストファイル内の該当文字列を検索し、影響範囲を把握する

## プロジェクト成熟度改善 (2026-03-10)

### CHANGELOG.mdでリリースノートを一元管理する
- リリースノートが GitLab（手動）と GitHub（`--generate-notes`）で内容がバラバラだった。CHANGELOG.md を single source of truth とし、CI が `sed` で該当バージョンのセクションを抽出してリリースノートに使う
- **ルール**: 新バージョンの変更は必ず CHANGELOG.md に記載してからタグを作成する。CI はそこから自動抽出する

### CI/CDのリリース自動化でsedパターンを共通化する
- GitLab CI と GitHub Actions の両方で CHANGELOG.md からリリースノートを抽出する。同じ `sed` パターンを使うことで、どちらかだけノートが空になる事故を防ぐ
- **ルール**: `sed -n "/^## \[VERSION\]/,/^## \[/{...}" CHANGELOG.md` パターンで抽出。フォールバックとして空の場合は `Release vX.Y.Z` を使う

### READMEのバージョンはハードコードしない
- ビルドコマンドにバージョンをハードコードすると、リリースのたびに README を更新する必要がある
- **ルール**: `$(git describe --tags --abbrev=0)` で最新タグから動的取得する

## CI/CD レビュー改善 (2026-03-10)

### CIのDockerイメージバージョンはgo.modと一致させる
- `.gitlab-ci.yml` で `golang:1.24` を使っていたが、`go.mod` は `go 1.26.1` だった。GitHub Actions は `go-version-file: go.mod` で自動追従するが、GitLab CI は手動管理のため乖離が起きやすい
- **ルール**: GitLab CI の golang イメージバージョンは `go.mod` の Go バージョンと一致させる。Go をアップデートしたら CI も同時に更新する

### golang公式Dockerイメージにはcurlが同梱済み
- GitLab CI の release ジョブで `apt-get update && apt-get install -y curl` を実行していたが、`golang:1.26` イメージには curl がプリインストールされている
- **ルール**: golang 公式イメージで追加パッケージをインストールする前に、既に含まれていないか確認する。不要な `apt-get` はジョブを数秒遅延させる

### シェルスクリプト内の変数は常にクォートする
- GitHub Actions で `gh release create ${GITHUB_REF_NAME}` と未クォートで使用していた。タグ名にスペースが入ることは稀だが、シェルのベストプラクティスに反する
- **ルール**: CI/CD のシェルスクリプト内で変数展開する際は `"${VAR}"` とダブルクォートで囲む。特に外部入力由来の値（タグ名、ブランチ名）は必須

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

### マルチAIレビューで異なる観点が得られる
- Claude（simplify）は構造・重複・効率を重視、Codex はユーザーシナリオの抜け（TOFU パスの未対応）を発見、Gemini はコードパス間の一貫性不整合を発見。単一AIでは見落とすバグが複数AIの組み合わせで検出できた
- **ルール**: セキュリティやデータ整合性に関わる変更は、複数AIでレビューする。各AIに異なる観点（構造、シナリオ、一貫性）を指示すると効果的

## デフォルト公開鍵の探索強化 (2026-03-10)

### 関数を削除する前に全 build tag のビルドを確認する
- `defaultPubKeyPath()` を削除したが、`integration_test.go`（`//go:build integration`）がまだ参照しており、`go test -tags=integration ./...` がコンパイルエラーになった。通常の `go test ./...` では build tag 付きファイルがビルドされないため見逃した。Codex レビューで発見
- **ルール**: 関数を削除・リネームする際は `go build -tags=integration ./...` など全 build tag でビルドを確認する。`go test ./...` だけでは不十分

### ssh-add -L の出力パースでは鍵タイプのプレフィックスで判定する
- `ssh-add -L` は鍵がない場合 `The agent has no identities.` を返す。`len(fields) >= 2` だけでは誤って有効な鍵と判定してしまう。また FIDO/U2F キーは `sk-ssh-ed25519`, `sk-ecdsa-sha2-nistp256` で始まるため `ssh-` と `ecdsa-` だけでは不十分
- **ルール**: ssh 公開鍵の判定は鍵タイプ文字列のプレフィックス（`ssh-`, `ecdsa-`, `sk-ssh-`, `sk-ecdsa-`）で行う。フィールド数だけで判定しない

### Gemini CLI のレビューでは plan モードを使わない
- Gemini CLI の `--approval-mode plan` はシェルコマンド実行が制限されるため、`git diff` や `go build` が実行できずレビューが機能しなかった。またプロンプトにビルド・テスト実行を含めないと、コンパイルエラーのような基本的な問題を見逃す
- **ルール**: Gemini CLI でコードレビューする際は必ず `-y` を使い、プロンプトに「差分取得 → ビルド → テスト → レビュー」の手順を含める。`--approval-mode plan` はレビューには使わない
