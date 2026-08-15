# LESSONS

## 定期メンテナンス: stdlib 脆弱性は toolchain 行で追従する (2026-08-15)

### `go` ディレクティブは据え置き、`toolchain` 行だけを上げる
- `govulncheck` が stdlib 1.26.5 の脆弱性を複数報告した（いずれもこのツールからは到達不能）。修正は
  Go 1.26.6 で、`go.mod` の何を上げるかが問題になった
- **却下した案**: (A) `go 1.26.6` に上げる — 過去に Termux（toolchain の自動ダウンロード不可）で
  `go install` が失敗し 1.26.0 まで下げた経緯がある（CHANGELOG 1.x）。(B) `go.mod` を触らず CI の
  `setup-go` に版を直書き — ローカルと CI で版がずれ、`go.mod` を見ても実際のビルド版が分からない
- **決め手**: `go get toolchain@go1.26.6` で `toolchain` 行を足した。`toolchain` 行は main module
  としてビルドするとき（ローカル / CI）だけ効き、`go install pkg@version` の利用者には適用されない
  ので Termux 互換を壊さない。ローカルは `GOTOOLCHAIN=auto` が 1.26.6 を自動取得し、`sudo` で
  `/usr/local/go` を更新しなくても `make check` と `govulncheck` が新版で通った（`go version` が
  モジュール内で go1.26.6 を返すことで確認）
- **覆す条件**: `toolchain` 行の版が Termux 側にも強制される挙動が確認された場合、または `go` 行の
  最小版を上げる別の理由（言語機能）が出た場合は `go` 行を上げて CHANGELOG に互換性影響を明記する

## Scoop 用 PAT を 1Password 管理へ移行 (2026-08-15)

### CI シークレットの「移行先」は vault 単位の到達範囲で選ぶ
- 1Password サービスアカウントのアクセス権は **vault 単位**で付き、個々の item に絞れない。手元の
  `Dev` vault を GitHub Actions 用 SA にそのまま与えると、Actions 環境が漏れた際の到達範囲が Scoop
  PAT だけでなく `Dev` の全 item に広がる。実際、無関係な複数プロジェクトの本番シークレットが同居して
  いた
- **却下した案**: (A) `actions/create-github-app-token` — installation token は 1 時間で失効し
  `scoop-bucket` にスコープできるため長期 PAT を完全に廃止できるが、SA と `gh` vault は作成済みで
  PAT 格納済みだった。(B) 既存 `Dev` vault を GitHub 用 SA に付与 — 追加作業ゼロだが上記の到達範囲
  問題がある。(C) `secrets.SCOOP_GITHUB_TOKEN` の行を残したまま 1Password 読み出しを併存 — step
  レベルの `env:` が job env より優先されるため、GitHub 側の secret を削除した時点で空文字が渡り、
  goreleaser の scoop push が認証エラーで落ちる
- **決め手**: `gh` 専用 vault を切って Scoop PAT だけを入れ、GitHub 用 SA には read-only でその vault
  のみ付与した。これで移行後に GitHub 側に残る長期シークレットは `OP_SERVICE_ACCOUNT_TOKEN` 1 つに
  なる。**シークレットの数は減っていない**（1 個が 1 個に置き換わっただけ）。得られるのはローテーション
  の一元化と 1Password 側の監査ログで、そこを理由に採る限り妥当
- **覆す条件**: SA トークンの失効でリリースが黙って壊れた場合、または GitHub 側に長期シークレットを
  一切置かない要件が出た場合は GitHub App 方式（案 A）へ移す

### `skip_upload: auto` のせいでプレリリースタグは検証にならない
- 配布チャネルのトークンを差し替えたら普通はプレリリースタグで試したくなるが、`.goreleaser.yaml` の
  `scoops.skip_upload: auto` はプレリリース時に bucket アップロードごとスキップする。`v*-rc1` を打っても
  **ジョブは緑になるがトークンを一切試していない**。release.yml のコメントが警告している「リリースは
  公開済みだが bucket 未更新」という半端な状態を、本番タグで初めて踏むことになる
- **決め手**: ブランチ限定（`on: push: branches: [...]`）の一時ジョブ `op-check.yml` を置き、1Password
  から PAT を読んで `gh api repos/kwrkb/scoop-bucket -q .permissions.push` を確認する方式で検証した。
  run 31868231450 で `push permission: true` を得てからファイルを削除している。本番リリースを 1 回も
  消費せずに「op 参照が解決するか」「SA が vault に届くか」「PAT に push 権限があるか」を分離して潰せる
- **ルール**: fine-grained PAT が対象リポジトリをリソースに含まない場合、GitHub API は 403 ではなく
  **404** を返す。権限確認では API 呼び出しの成否を見ず、`permissions.push` の値を明示的に判定する
- 併せて `gh api repos/kwrkb/scoop-bucket/branches/main -q .protected` が `false` であることも確認した。
  collaborator 権限があっても branch protection があれば goreleaser の直接 push は弾かれるため、
  push 権限の確認だけでは経路が通る保証にならない
- **覆す条件**: `skip_upload` を外してプレリリースでも bucket を更新する運用に変えたら、プレリリース
  タグ自体が経路の検証になるため一時ジョブは不要になる

## WinGet から Scoop へ切替: Defender 誤検知 (2026-08-12)

**「WinGet 採用」の結論を差し替える。** 覆す条件として書いた「WinGet の PR が未署名バイナリ等で
継続的に落ちるなら Scoop へ退避」が、1回目のリリースで現実になった。

### 「ツールの正当な機能」がそのままマルウェア検知のヒューリスティックに一致する
- v1.8.0 の manifest PR（`microsoft/winget-pkgs#415368`）は 01〜07 を通過したあと `08. Installation Validation` で `Validation-Defender-Error` + `Needs-Author-Feedback` が付いて停止した。同じ `07. Installers Scan` は**直前の実行では SUCCESS** だったので、定義更新で新たにヒットした典型的なヒューリスティック誤検知
- 見落としていたのは、誤検知の原因が「未署名 Go バイナリ」だけではないこと。本ツールは **SSH で接続し `authorized_keys` に鍵を追記し `icacls` で ACL を書き換える**。これはバックドア設置の手順そのもので、スキャナから見て正規の用途と区別する材料が無い。つまり**将来のリリースでも再発する前提で設計すべき**性質だった
- **決め手**: 単発の事故なら誤検知申請（WDSI）で通せばよいが、再発が見込まれるなら「毎リリースが外部審査の通過に依存する」構造そのものが問題になる。配布チャネル選定時にはコストを「リポジトリ数とトークン」でしか見積もっておらず、**審査を通る難易度**を勘定に入れていなかった
- **ルール**: 配布チャネルのコストを見積もるときは、リポジトリ／トークン／CI 設定に加えて「**そのチャネルが第三者の審査を伴うか**」と「**自分のツールがその審査を安定して通る性質か**」を評価する。セキュリティツールや、権限・認証情報・システム設定を書き換えるツールは、審査付きチャネルと相性が悪い

### 発見性より「リリースが自分で完結すること」を優先した
- **却下した案**: (A) WDSI に誤検知申請を出して WinGet を維持 — 今回は通る見込みだが、再発するたびに申請と再検証依頼が要り、リリースのリードタイムが他者依存になる。(B) Authenticode 証明書でコード署名 — 検知率は下がるが OV/EV で年間数百ドル、EV はハードウェアトークンも必要で、単一バイナリの小規模ツールに対する恒久コストとして見合わない。VISION の「シンプル」とも衝突する。(C) WinGet と Scoop の併用 — 誤検知が解消するまで毎リリース winget パイプが失敗し、ワークフローが赤くなり続ける
- **決め手**: Scoop の個人 bucket は発見性がゼロ（`scoop bucket add` を先に踏ませる必要がある）で、そこは WinGet に劣る。それでも、**自分の bucket に push するだけで完結し、他者の審査でリリースが止まらない**ことを優先した。GitHub Releases と `go install` という既存導線は無傷で残っており、Windows 層に届く手段がゼロになるわけではない
- **覆す条件**: Defender の誤検知が解消し、かつ複数リリースにわたって再発しないと確認できたら WinGet を再検討する。あるいはコード署名を導入する動機（SmartScreen 警告への苦情など）が別途生じた場合

## WinGet 配布の実装 (2026-08-11)

### cross-fork の PR を作るトークンは classic PAT でなければならない
- WinGet は fork（`kwrkb/winget-pkgs`）へ push して upstream（`microsoft/winget-pkgs`）へ PR を出す構成になる。最小権限のつもりで fine-grained PAT を選ぶと**動かない**: PR 作成にはトークンの resource owner が **base（ターゲット）側**である必要があり、自分が所有もメンバーでもない `microsoft` を resource owner にした PAT は作成できない。実行すると `Resource not accessible by personal access token`（403）になる
- **却下した案**: (A) fine-grained PAT — 上記の理由で cross-fork PR が作れない。GitHub 公式の解決策は提供されていない。(B) workflow 既定の `secrets.GITHUB_TOKEN` — 当該リポジトリにしか権限が無く、fork への push すら届かない
- **決め手**: classic PAT の `public_repo` スコープを採用。必要な動作は「public な fork への write」と「public な base への PR 作成」の2つだけで、`repo`（フル）だと private リポジトリ全体まで渡すことになり不要に広い
- **覆す条件**: GitHub が fine-grained PAT で cross-fork PR をサポートしたら移行する（roadmap #600 で言及あり、時期未定）

### winget の manifest は `--snapshot` で生成まで検証できる
- `.goreleaser.yaml` の不備はタグ push でしか発火せず、失敗すると「実タグ上に半端なリリース」が残る。winget パイプも同じ危険があるが、`goreleaser release --snapshot --clean` は publish 段階ごとスキップするため、**アップロードせず manifest 生成だけ**を実行して `dist/winget/manifests/.../*.yaml` を目視できる。`WINGET_GITHUB_TOKEN` を渡さなくても exit 0 になることで、publish が試みられていないと確認できる（`skip_upload: auto` は無関係。あれはプレリリース時のアップロードを抑止する設定で、snapshot の挙動には効かない）
- 実際に検証すべきは `InstallerType: zip` + `NestedInstallerFiles.RelativeFilePath` が**アーカイブ内の実パスと一致しているか**。`unzip -l` で zip 直下に `ssh-pushkey.exe` があることを確認して突き合わせた。ここがずれると winget 側の検証で落ちる
- **ルール**: winget / scoop / homebrew のパイプを足したら、タグを打つ前に `--snapshot` で manifest を生成し、アーカイブの実レイアウトと突き合わせる。スキーマ検証（`goreleaser check`）だけでは、アーカイブ内のパス不一致は検出できない

## 配布チャネル再判断: WinGet 採用 / Homebrew・Scoop 不採用 (2026-08-11)

**同日の「改善候補の取捨選択」エントリで tap / bucket を見送りとした結論を、実測に基づき差し替える。**
見送り時の未検証項目を潰したところ、前提が2つ崩れた。

### `goreleaser check` で潰したら Homebrew は選択肢から消えた
- 見送り時は「Homebrew は別リポジトリ + PAT が必要だから高い」というコスト論で判断していたが、実際に検証すると**そもそも formula を選べない**。`brews:` は GoReleaser v2.16 で hard deprecation となり、ローカル v2.17.1 で `goreleaser check` が失敗する: `DEPRECATED: brews should not be used anymore` → `check failed  error=1 out of 1 configuration file(s) have issues`
- 残る `homebrew_casks:` は check を通るが、Cask は長らく macOS 専用（Homebrew maintainer, 2022: "Casks are only for macOS."）。Homebrew 6.0.0（2026-06-11）で Linux cask 要件の明示化・Linux checksum variations・AppImage 対応が入っており単一バイナリなら通る可能性はあるが、ローカルに brew が無く**実地検証できていない**
- **却下した案**: `homebrew_casks:` を入れる — 確実に効くのは macOS のみで、そこは既に `go install` / tarball がある層。代替不在の層ではない
- **ルール**: 「コストが高いから見送り」と書きそうになったら、先に `goreleaser check` を回す。コストを論じる前に**そもそも選べるか**が決まっていることがある

### ターゲット OS とクライアント OS を混同して配布チャネルを選びかけた
- 「Windows OpenSSH がターゲットのツールだから Windows のパッケージマネージャ（Scoop）」と考えたが、これは**ツールが何を操作するか**と**誰がどこで実行するか**の混同。クライアント環境で切り直すと像が変わる: WSL/Linux・macOS には `ssh-copy-id` が存在する（Windows 非対応なだけ）のに対し、**Windows OpenSSH には `ssh-copy-id` がそもそも実装されていない**（Win32-OpenSSH の要望 issue #341 / #1467 / #2301 が長年オープン、野良 PowerShell スクリプトが流通している）
- 同時に、最大セグメントであろう WSL/Linux は**どのチャネルを足しても既存導線（`go install` / tarball）のまま**で改善しないことも判明した
- **決め手**: 代替が完全に存在しないのは Windows→Windows のみ。届ける価値が最も高いのはそこなので Windows 向けチャネルを選ぶ、という結論自体は維持しつつ、選ぶ理由を「ターゲットが Windows だから」から「その層にだけ代替が無いから」に置き換えた
- **ルール**: 配布チャネルは「ツールが何を対象にするか」ではなく「**実行する人がどの OS にいて、そこに代替が有るか**」で選ぶ

### 個人 bucket / tap は発見性がゼロなので、WinGet を採った
- **却下した案**: (A) Scoop 個人 bucket — `scoop bucket add` を先に踏ませる必要があり、README を読んだ人にしか届かない。コストは WinGet と同額（新規リポジトリ + PAT）なのに発見性だけが無い。(B) Scoop を同一リポジトリで運用して PAT を回避 — GoReleaser 公式が `directory:` について「サブディレクトリだと `scoop bucket list` が 0 manifests になるので空推奨」と明記しており、manifest が**リポジトリルート**に出る。`scoop bucket add` でソースツリー全体が clone される形になり、PAT 1本を避ける対価としては割に合わない
- **決め手**: 別リポジトリへの publish は GoReleaser 公式が「default action token は使えない、contents write 権限を持つ別トークンが必要」と明記しており、Scoop / Homebrew / WinGet のいずれも PAT 必須でコストが同額。同額なら**素の `winget search` に載り Windows 11 に標準搭載されている** WinGet が優位。`winget:` セクションも `goreleaser check` パスを確認済み
- **覆す条件**: (1) WinGet の PR が未署名バイナリ等で継続的に落ちるなら、外部依存ゼロで自己完結する Scoop bucket に退避する。(2) macOS 向けの要望が実際に来たら `homebrew_casks:` を追加する（Linux で cask が動くかは要実地検証）

## 改善候補の取捨選択: 配布導線 / tap・bucket / --verbose (2026-08-11)

### Homebrew tap / Scoop bucket は「別リポジトリが構造上必須か」で切り分ける
- 配布チャネル追加の可否を運用コストの多寡で論じようとしたが、この案件はそれ以前の構造的制約で決まった。Homebrew の tap は `brew tap kwrkb/<name>` が `github.com/kwrkb/homebrew-<name>` に解決される命名規約のため、**第2のリポジトリと、そこへの write 権限を持つ PAT の secret 登録が必須**になる。一方 Scoop の bucket は manifest の置き場所に規約上の制約がなく、同一リポジトリの `bucket/` ディレクトリで成立し得る
- **却下した案**: (A) Homebrew tap を今回入れる — 「いきなり別リポジトリ作成や外部公開はしない」という今回の明示制約に正面から抵触する。(B) Scoop bucket だけ今回入れる — 同一リポジトリ経路なら安いが、GoReleaser が manifest を commit するのに workflow 既存の `GITHUB_TOKEN`（contents:write）で足りるか PAT が要るかが未検証で、安いかどうかが確定していない。(C) 運用コストを見積もって総合判断 — 制約と未検証項目で既に決まるため、見積もりは判断に寄与しない
- **決め手**: `LESSONS.md` の「配布チャネルは段階導入し、土台と tap/bucket を分ける」（2026-06-20）に既に段階分離のルールが記録済みで、今回やるべきは「今がその段階か」の判定だけだった。外部からの要望は観測されておらず、`GITHUB_TOKEN` で足りるかも未検証 → 両方とも見送り、`PLAN.md` に覆す条件として記録
- **覆す条件**: (1) 外部から tap / bucket の要望が実際に来る、または (2) GoReleaser が同一リポジトリへ Scoop manifest を commit するのに `GITHUB_TOKEN` で足りる（PAT 不要）と検証できた場合、Scoop から再検討する

### 診断フラグを足す前に「既に出ている出力」と実際の欠落を突き合わせる
- `--verbose` で出したい候補として resolved host/port/user・public key source・administrator 判定・effective AuthorizedKeysFile・host key verification path・ACL 適用対象・deploy result が挙がったが、コードを当たると大半は既に通常実行で stdout に出ていた（`main.go` の `=> Public key loaded:` / `=> Connecting to %s@%s:%d`、`deploy.go` の `=> Checking Administrators group...` / `=> administrators_authorized_keys is enabled (sshd -T)` / `=> Target: %s`）。「静かな UX に診断を足す」という前提自体が実態と合っていなかった
- **却下した案**: (A) `--verbose` を実装する — 新規フラグ1本と出力の二重系統を持ち込む対価が、既出力との重複でほぼ相殺される。(B) known_hosts パスや `sshd -T` の生の `AuthorizedKeysFile` 値も出す — 「非スコープ」と明示されている `doctor` 的診断機能への入口になる
- **決め手**: 突き合わせで残った実際の欠落は1点だけだった。**dry-run は `DRY_RUN_TARGET:` で配置先パスを出すのに、通常実行の `KEY_DEPLOYED` はパスを持たない**という非対称。非 Admin 時の配置先は `$env:USERPROFILE` 展開後にしか確定せず Go 側で組み立てられないため、既存の `DRY_RUN_TARGET:` と同じ方式でマーカーに `$keyFile` を載せる（`KEY_DEPLOYED:<path>` / `KEY_ALREADY_EXISTS:<path>`）形にした。フラグ追加ゼロで欠落が埋まる
- **覆す条件**: 「どこで失敗したか分からない」という報告が実際に来て、かつ既存の stdout と `--dry-run` の組み合わせでは切り分けられないケースが具体的に示された場合

### 「既存機能で足りる」を不採用の論拠にするなら、その機能が発見可能かを先に確認する
- `--verbose` 不採用の論拠を「`--dry-run` で足りる」と書こうとした時点で、その `--dry-run` が README の Options 表に載っていないことに気づいた。`CHANGELOG.md` を確認すると v1.6.0（2026-06-20）で追加済み、README の最終更新も同日なのに表だけ更新漏れしていた（`--help` も同様）
- **ルール**: 新機能を「既存機能で代替できる」と言って却下するときは、その既存機能がドキュメント上で発見可能かを必ず確認する。発見できない機能は存在しないのと同じで、却下の論拠が成立しない。ドキュメント整備が却下の前提条件になる

## env ファイルの 1Password 参照は source では解決できない (2026-08-10)

### `op://` 参照を含む env ファイルは `.`（source）ではなく `op run --env-file` に渡す
- `Makefile` の itest ターゲットは `set -a; . ./.env.integration` で認証情報を読んでいたが、値が `op://<vault>/<item>/<field>` 参照になった時点で `sh` がパースに失敗し `export: は: bad variable name` で起動しなくなった。シェルにとって `op://` 参照はただの文字列で、解決する主体がいない
- **却下した案**: (A) env ファイルを平文パスワードに戻す — 平文の常設を避けたいという元の動機に反する。(B) itest を常に `op run` 経由にする — op 未導入の環境（CI コンテナなど）で無条件に落ちる
- **決め手**: env ファイルに `op://` が含まれるかを `grep` で判定し、含む時だけ `op run --env-file` に委譲、含まなければ従来の source にフォールバックする分岐を採用。`make itest-all` で管理者・一般とも全 6 テスト PASS を実機確認した
- **覆す条件**: 認証情報の供給元が 1Password 以外（direnv、OS キーチェーン等）に変わった場合、または CI で統合テストを回すようになり op 前提を置けなくなった場合

### Makefile のレシピ内 `echo "..."` にバッククォートを書くとコマンド置換される
- エラーメッセージに `` `op signin` `` と書いたところ、`sh` が二重引用符内のバッククォートをコマンド置換として解釈し、意図せず `op signin` が実行される状態になっていた
- **ルール**: Makefile のレシピ内でコード片を示すときはシングルクォートの `echo` を使うか、バッククォートを書かない

## 非 loopback での Match Address 検証 (Issue #17) (2026-06-21)

### 既存鍵が残った状態ではバグも修正も観測できない（最大のハマりどころ）
- 修正バイナリと旧バイナリの差を観測しようとして、何度実行しても挙動が変わらない症状にはまった。原因は配置先（user 側 / admin 側両方）に過去の検証で残った鍵があり、`buildDeployScript` の重複判定（blob 単位）が deploy 自体をスキップしていたこと。バグ再現も修正検証も「実際に書き込みが起きる」前提なので、書き込み前にショートサーキットされるとどちらの分岐に行ったかさえ分からなくなる
- **ルール**: 配置ロジックを差し替えた前後で挙動差を比較する実機検証では、必ず**配置先候補すべて**（admin 側 `administrators_authorized_keys` と user 側 `~/.ssh/authorized_keys` の両方）の既存鍵を削除してクリーンな状態から始める。重複検知が成功扱いで早期終了するため、ログ上は正常完了に見えて「何が起きたか」が一切残らない

### Windows OpenSSH の `sshd -T -C user=...` ではグループ解決が効かない
- 検証台で `Match Group administrators` を使ったら、`sshd -T -C user=pushadmin,addr=...` 経由のオフライン評価で `ga_init, unable to resolve user pushadmin` 警告が出てブロックが**発火しなかった**。`sshd_config` 上は通る構成だが、オフラインの実効値プローブではグループ解決ができないため、harness としては機能しない
- **ルール**: `sshd -T -C` ベースの検証 harness を作るときは `Match Group` を避け、**`Match User <username>`**（文字列マッチでグループ解決不要）で組む。`Match Group administrators` を前提にしたテストは「実機 SSH 接続では効くが `sshd -T` では発火しない」という非対称を生む

### Windows OpenSSH の Match 評価は first-wins、Match Address を先に置く
- `Match Address 192.0.2.10` と `Match User pushadmin` を後者→前者の順で書いたところ、実IP（`192.0.2.10`）からの `sshd -T` でも admin パスが返ってきて Address ブロックが効いていなかった。順序を入れ替えて Address を先頭に置いたら期待どおり割れた。Windows OpenSSH の `AuthorizedKeysFile` は実測で **first-wins**（最初にマッチした Match ブロックが勝つ）挙動
- **ルール**: addr 依存の差を観測したい検証台では `Match Address` を必ず**先頭**に置き、baseline（`Match User ...`）はその後に置く。loopback で baseline を踏ませ、実IPでだけ Address ブロックに入れる順序が「差が出る harness」の鍵

### `go install` 済みの旧バイナリが PATH 経由で実行されて自爆する
- 修正後の master をビルドしたつもりが、Mac の `$GOPATH/bin/ssh-pushkey`（旧 `go install` の遺物）が PATH で勝っていて、いつまでも旧挙動が出続けた
- **ルール**: バイナリ差し替え検証では `go install` ではなく `go build -o <別名>` で明示的に成果物を作り、`./別名` 相対パスで実行する。`which ssh-pushkey` で何が走っているかを毎回確認する習慣をつける

## ホストキー検証パスのテスト整備 (Issue #16) (2026-06-21)

### OS 非依存のクライアント側処理は「実機統合テスト」ではなくローカルサーバで CI 化する
- ホストキー検証（TOFU / 不一致拒否 / 鍵更新 / HashKnownHosts）は `dialSSH → createHostKeyCallback → knownhosts` の**純粋なクライアント側 Go SSH 処理**で、リモートが Windows か否かに一切依存しない。Issue は実 Windows ホスト向け integration-tag テスト＋手動手順を想定していたが、その切り分け自体が不要だった
- **ルール**: 「実機が要る」と書かれた検証要求を鵜呑みにせず、まず *どの層の挙動か* を切り分ける。検証対象がクライアント側で完結するなら、`127.0.0.1:0` に標準ライブラリ製の `ssh.ServerConfig`（password 認証のみ・チャネルは Reject で十分。`ssh.Dial` は認証完了で返りチャネルを開かない）を立て、実コードパスをそのまま exercise する tag なしテスト（`go test ./...` で走る）に昇格させる

### 端末入力は `var` 関数にして応答を注入可能にする
- `readLineFromTerminal` は `/dev/tty` を直接読むためテストから制御できなかった。`func` 宣言を `var readLineFromTerminal = func() ...` に変えるだけで、テストが固定応答（yes/no）や「呼ばれたら失敗」スパイを注入できる。本体変更は1行
- **ルール**: 対話プロンプトを伴う関数はパッケージ変数 `var fn = func(){...}` にしておく。テストでは `prev := fn; t.Cleanup(func(){ fn = prev })` で必ず復元する。`t.Setenv("HOME", tmp)` で known_hosts の探索先を temp に振るのと併せ、これらのテストは**非並列前提**（`t.Parallel()` 禁止）

### known_hosts 不一致テストは同一鍵種別の別鍵を使う
- 不一致（REMOTE HOST IDENTIFICATION HAS CHANGED）経路を検証する際、登録済みの「誤った鍵」をサーバ鍵と**別種別**（例: ecdsa vs ed25519）にすると、`HostKeyAlgorithms` 制限でハンドシェイクが失敗し `shouldRetryWithoutHostKeyAlgorithms` の再試行経路に逸れて、本来 assert したい `KeyError.Want` の不一致経路に到達しない
- **ルール**: 鍵不一致／更新の経路をテストするときは、シードする旧鍵とサーバ鍵を**同一種別（ed25519）の別鍵**にする。`crypto/ed25519.GenerateKey` を2回呼べばよい

## GoReleaser: changelog.disable は --release-notes も無効化する (2026-06-21)

### `changelog.disable: true` と `--release-notes` は併用不可

- GoReleaser の `--release-notes=FILE` は **changelog パイプ内**で読み込まれる。`.goreleaser.yaml` で `changelog: { disable: true }` にするとパイプごとスキップ（ログ `skipped generating changelog`）され、`--release-notes` が**サイレントに無視**されてリリース本文が空（GitHub API 上は `"\n"`、length 1）になる
- 症状の切り分け: 全ステップ `success`・全アセット upload 成功なのに本文だけ空。フォールバック文字列（`Release vX`）ですらないなら、抽出ファイルは非空なのに goreleaser が読んでいない＝この罠。**Node 20 deprecation 警告は無関係**（JS ラッパーの実行時警告で本文だけを選択的に空にはできない。証拠で否定すること）
- 根本原因の確定: ローカルと CI で goreleaser バージョンを揃え（`goreleaser --version`）、`--verbose` で `skipped generating changelog` を確認。本文はディスクに出ないので publish を伴わないと直接は見えない → パイプ挙動の差分で判定する
- **対処**: 配布物生成は goreleaser に任せ、本文だけ publish 後に別ステップ `gh release edit "$GITHUB_REF_NAME" --notes-file /tmp/release-notes.md` で明示設定。`changelog.disable: true` は維持（git ログ由来ノートは不要）。goreleaser 引数から `--release-notes` は外す。代替案「disable を外す」は goreleaser の precedence 挙動に依存し実リリースまで目視検証できないため不採用
- **GitLab は無影響**: `release-cli create --description "$(cat notes)"` で本文を直接渡すため同じ CHANGELOG 抽出でも空にならない。GitHub 固有の罠
- **リリース後検証**: タグ push 後に `gh api repos/<o>/<r>/releases/tags/<tag> --jq '(.body|length)'` で本文長を必ず確認する（アセットの有無だけ見て満足しない）

## 既存ファイルへの追記は末尾改行を保証する (2026-06-20)

### 追記する前に既存ファイルの末尾改行を確認する

- 既存ファイルへ単純に1行追記すると、そのファイルが**末尾改行なし**で終わっている場合、新しい行が直前の行に連結する。`authorized_keys` ではこれで新旧の鍵が両方とも認証不能になり、`known_hosts` ではエントリが破損する
- 本ツール自身が書いたファイルは常に `\n` 終端なので安全だが、`administrators_authorized_keys` は MS 公式手順・他ツール・手動編集で作られることが多く、末尾改行なしが現実的に起こる。本家 `ssh-copy-id` はこのケースを明示的にガードしている
- **ルール**: 既存ファイルへ追記する前にバイト単位で末尾を検査し、最後のバイトが LF(10) でなければ改行を1つ補ってから追記する。対象は `authorized_keys` 追記（`deploy.go` の PowerShell `$existing[$existing.Length - 1] -ne 10`）と `known_hosts` の TOFU 追記（`ssh.go` の `appendKnownHostsLine`）の両方
- **注意**: 判定するのは LF(10) のみ。**CR(13) を「改行終端」に含めてはいけない**。CRLF でも末尾バイトは LF なので 10 判定で足り、`\r` のみで終わるファイルは「不完全な終端」として `\n` を補い CRLF へ正規化すべき。13 を改行扱いすると `keyA\rNEW` が sshd から1行に見えて連結バグが再発する
- ファイル全体を読み直して書き戻す経路（`replaceHostKeyInKnownHosts` → `atomicWriteFile`）は末尾改行を再構成済みなので対象外。漏れやすいのは **O_APPEND / AppendAllText の単純追記**の方
- **テスト**: 純粋な Go ヘルパー（`appendKnownHostsLine`）はユニットで末尾改行3パターンを検証。PowerShell 側は生成スクリプトの順序検証にとどまるため、末尾改行なしファイルへ追記して行数が増える（連結しない）ことを integration テスト（`TestIntegration_AppendToFileWithoutTrailingNewline`）で実機検証する

## 信頼性向上: dry-run / icacls エラー伝搬 (2026-06-20)

### native コマンドの `2>&1` は `$ErrorActionPreference='Stop'` と相性が悪い（PS 5.1）
- icacls 等の native コマンドの stderr を `$out = & icacls ... 2>&1` で捕捉する際、`ErrorActionPreference='Stop'` のままだと、コマンドが stderr に書いた瞬間に PowerShell が `NativeCommandError` を**終端エラーとして throw** し、後続の `if ($LASTEXITCODE -ne 0) { Write-Output 'MARKER' }` に到達しない。結果、失敗時にマーカーが出力されず Go 側で原因不明エラーになる
- **ルール**: `$LASTEXITCODE` を明示チェックしている native コマンド区間では `Stop` は不要。`2>&1` で出力捕捉する直前に `$ErrorActionPreference = 'Continue'` に落とす。.NET メソッド（`AppendAllText` 等）の例外は ErrorActionPreference に依存せず throw するので、そちらの保護は別途維持される
- マーカーには実エラーを `"ACL_SET_FAILED_DIR|$out"` の形で付加し、Go 側は `|` 以降を抽出してエラーメッセージに含める（トラブルシュート性向上）

### dry-run は別スクリプトを作らず本番スクリプトにフラグ注入する
- dry-run 用に `buildDryRunScript` を別途作ると、配置先パス決定・重複チェックのロジックが本番と二重化し、片方だけ修正してドリフトする危険がある
- **ルール**: `buildDeployScript(pubKey, isAdmin, dryRun)` のように本番スクリプトに `$dryRun` を注入し、書き込み・ACL・ディレクトリ作成の**前**に `if ($dryRun) { ...; exit 0 }` ガードを置く。共通部分（パス決定・重複判定）は1経路に統一。静的テストは「副作用文が exit 0 ガードより後にある（実行時到達不能）」ことを順序で検証する

### dry-run でも SSH 接続・パスワード入力は発生する
- 配置先（admin/user 分岐）と重複判定はリモートの状態に依存するため、正確なプレビューには SSH 接続が必須。完全ローカルな dry-run は不正確になる
- **ルール**: dry-run の仕様として「接続・パスワード入力は行うが書き込みはしない」ことを usage / README / CHANGELOG に明記する

## ~/.ssh/config 解決 (2026-06-20)

### kevinburke/ssh_config は package-level Get と *Config.Get でセマンティクスが違う
- package-level `ssh_config.Get(alias, key)` は OpenSSH 既定値を補完する（未設定でも `Port`→"22" 等を返す）。一方 `Decode` で得た `*Config.Get` は**未設定キーは ""** を返す（実測で確認）。優先順位ロジックで「config に値があるか」を空文字判定する場合、既定補完されると判定が壊れる
- **ルール**: 「設定されているか」を空文字で判定したいときは、`~/.ssh/config` を自前で `os.Open`→`ssh_config.Decode`→`cfg.Get` する。package-level `Get`/`GetStrict`（DefaultUserSettings 経由・既定補完あり・/etc/ssh も読む）に依存しない。これでテストの fake getter と本番のセマンティクスも一致する

### ssh_config は alias だけでなく任意の一致 Host パターンに適用される
- `Host *` のようなワイルドカードブロックがあると、`ssh-pushkey admin@1.2.3.4` のような素のホスト指定でも `Port`/`HostName`/`User` が解決される。「エイリアスを使ったときだけ」という思い込みは誤り
- **ルール**: config 解決を入れたら CLI 明示（`user@` / `-p`）を常に優先させ（`flag.Visit` で `-p` 明示を検出）、ワイルドカード一致でも CLI が勝つことをテストで固定する。CHANGELOG/README にも「任意の一致 Host に適用」と明記する

### `-i`（配置する公開鍵）は ssh_config の IdentityFile と無関係
- 本ツールはパスワード認証で、`-i` は**リモートへ配置する公開鍵**。ssh_config の `IdentityFile`（認証鍵）とは意味が違う。安易に紐付けると将来「親切な修正」で壊される
- **ルール**: `IdentityFile` は意図的に非対応とし、その旨をコード comment・README 両方に残す（片方だけだと見落とされる）

## レビュー対応・デュアルリモートマージ (2026-06-20)

### AI/bot の「コンパイルエラー」「API挙動」指摘は pin したバージョンの実ソースで検証する
- Gemini が PR#6 で「`ssh.ClientConfig.Timeout` はハンドシェイクにも適用される（コメントが不正確）」、PR#7 で「`(*Config).Get` の戻り値は string のみでコンパイルエラーになる（critical）」と指摘したが、いずれも本リポジトリが pin した `golang.org/x/crypto v0.53.0` / `kevinburke/ssh_config v1.6.0` の実装には当てはまらなかった（前者は `config.Timeout` の参照が `net.DialTimeout` のみ、後者は `Get` が `(string, error)` を返しビルドも通過済み）。bot は別バージョンの実装を前提にしていた
- **ルール**: bot の「コンパイルエラー」「この API はこう動く」系の指摘は、`grep -rn` で `$(go env GOMODCACHE)/<module>@<pinned-version>/` の実ソースを確認してから採否を決める。既にビルド/テストが通っている事実も有力な反証。検証結果（根拠付き）を PR にインライン返信して記録する

### nil ガードは呼び出し元が制御できる非公開関数には足さない
- Gemini が `resolveConnection` に `get == nil` ガードを提案したが、`get` は全呼び出し元（`loadUserSSHConfig` は常に非 nil getter を返す／テストも非 nil）で nil になり得なかった。内部関数の事前条件違反を空 getter にフォールバックして黙殺すると「config が無いように見える」挙動でバグを隠す
- **ルール**: 非公開ヘルパーの nil ガードは、呼び出し元が nil を渡し得る場合のみ足す。制御された内部呼び出しのみなら fail-fast(panic) の方がバグを早く露見させる。「防御的だから」と一律に足さない

### デュアルリモートはローカル `--no-ff` マージ → 両リモート push で SHA 割れを防ぐ
- GitHub(origin)/GitLab(gitlab) 両方に PR/MR がある状態で、各プラットフォーム UI から独立にマージするとマージコミット SHA が割れて master 履歴が分岐する
- **ルール**: デュアルリモートのマージは「ローカル master で `git merge --no-ff <branch>` → `git push origin master && git push gitlab master`」で同一履歴を両方に流す。ブランチ HEAD が master の祖先になるため PR/MR は merged として自動クローズされる。複数 PR を順に取り込む際は usageText/CHANGELOG 等で必ず衝突するので、解決後に diff を確認し、意図的に除いた行（例: 別ブランチの dry-run 行）を取りこぼさない

## blob 単位重複判定 / known_hosts アトミック書き込み (2026-06-20)

### authorized_keys の鍵照合はトークンのスライド窓で `(type, base64)` を探す
- 重複判定を blob 単位にする際、各行を `\s+` 分割して先頭2フィールド `$parts[0] + ' ' + $parts[1]` だけ比較すると、`command="..." ssh-ed25519 AAAA...` のような options 前置行で先頭が type にならず検知漏れする。旧 `Select-String` 全行一致からのデグレで、毎回重複追記される（Codex P2）
- **ルール**: authorized_keys の行から鍵本体を取り出すときは、行頭固定でなく**トークンを2つずつスライドさせて `(type, base64)` ペアを `-ceq`（base64 は大小区別必須）で照合**する。options やコメントが前後に付いても鍵ペアはトークン2連として残るため、これで options 行も正しく検知できる。`for` の内側 `break` は for だけ抜けるので、外側 `foreach` 用に `if ($exists) { break }` を別途置く
- `$i -lt $parts.Count - 1` は PowerShell では `$i -lt ($parts.Count - 1)`（加算が比較より優先）と解釈されるので境界は正しい

### 「テストが通る」が修正を証明しない場合がある — 判別力のある経路で検証する
- ユニットテストが生成スクリプトに `-ceq $keyBlob` 文字列が**含まれること**だけを検証していると、バグ版でも修正版でも同じ部分文字列が存在するため `go test ./...` は両方で green になる。options 行を実際に検知できるかは PowerShell 実行時にしか分からず、ユニットテストはこの修正に対し**ゼロ検証**だった（advisor 指摘）
- **ルール**: 修正の正否を本当に分けるのが実行時挙動（PowerShell ランタイム等）なら、証明はその経路（統合テスト）に置く。「ユニットが green」を修正済みの根拠として報告しない。判別力テストは「バグ版なら False / 修正版だけ True」になる入力を用意する（例: 平文鍵行を残すと壊れたスキャナでも True になるので、敢えて option 行のみにする）

### runRemotePowerShell の出力で**ファイル内容を Go 側に往復させない**
- 統合テストで authorized_keys を base64 退避→Go で保持→復元しようとしたら、`runRemotePowerShell` の出力に CLIXML 初期化ノイズ（`Preparing modules for first use.`）が混ざり、退避 base64 が汚染されて `FromBase64String` が落ち、復元失敗で**実ホストの authorized_keys が注入した option 行のまま残った**
- **ルール**: リモートのファイル内容を退避・復元する処理は、データを Go 側へ吸い上げず**remote PowerShell 内で完結**させる（`Copy-Item`/`Move-Item` で `.bak` 兄弟ファイルに退避→復元）。CLIXML 混入の影響を受けるのは「出力をデータとして取り込む」場面なので、検証は `strings.Contains` で済む一方、データ往復は別経路にする

### 実ホストを破壊する統合テストは復元を堅牢にし、自己修復不能性も想定する
- 上記の復元失敗で残った option 行は、修正後の重複判定が blob を検知して**スキップするため、ただ再デプロイしても平文鍵が復活しない**（先に削除が必要）。「とりあえず再実行」では直らない状態に陥った
- **ルール**: 実ホスト状態を書き換える統合テストは (1) `defer` で必ず復元し、(2) 復元失敗は `t.Fatalf` でなく `t.Errorf` にして他の cleanup を妨げず、(3) 復元手順自体が壊れにくい方式（remote 内完結）を採る。破壊系テストを足すときは「復元が失敗したらホストはどう壊れ、どう戻すか」まで設計に含める

### known_hosts の安全な書き換えは flock でなく temp+rename
- truncate+write は部分書き込みで known_hosts を破損し得るが、flock 導入は新依存／build-tag のプラットフォーム別コードを招き、非ロックの書き手（実 `ssh`）も防げず過剰。破損経路は truncate のみで、TOFU 追記は既に `O_APPEND` で原子的
- **ルール**: 単一ファイルの破損防止は、まず**同一ディレクトリの temp に書いて `os.Rename`**（OpenSSH 自身の known_hosts 更新と同方式）で足りないか検討する。読み手は常に旧 or 新の完全なファイルだけを見る。残る同時 yes の lost-update は次回 TOFU で自己修復する良性として受容してよい。temp は必ず宛先と同ディレクトリに作る（別 FS だと rename が非原子）

## GoReleaser 採用（配布基盤）(2026-06-20)

### GoReleaser 設定はタグを打つ前にローカルで検証する
- `.goreleaser.yaml` の不備はタグ（`v*`）push でしか発火せず、失敗すると「実タグ上に半端なリリース」が残る高コストな失敗モードになる。`formats:`（複数形）か `format:`（単数）かといったスキーマ差はバージョン依存で、記憶で断定すると壊れる（v2.16 では `formats:` が正、`goreleaser check` が判定してくれた）
- **ルール**: GoReleaser 導入・変更時はマージ前に必ずローカル検証する。(1) **CI と同じメジャー版を入れる**（`go install github.com/goreleaser/goreleaser/v2@latest`。無印パスは v1 を引く恐れ）、(2) `goreleaser check` で構文・スキーマ、(3) `goreleaser release --snapshot --clean` + `ls dist/` で**実際にアーカイブ・checksums が生成されるか**まで確認、(4) 生成バイナリの `--version` で ldflags 注入を確認。タグはこのゲートを通してから打つ

### デュアルリモートの GoReleaser は「canonical 一本化」で複雑さを避ける
- GoReleaser の1実行は1ホスト（`release.github` 等）にしか公開できない。GitHub/GitLab 両方にアーカイブを出すには host 判定・トークン・設定分岐が要り複雑化する
- **ルール**: 配布の正本を片方（GitHub）に寄せ、もう片方の CI は据え置く。生じる成果物の非対称（GitHub=アーカイブ / GitLab=生バイナリ）は許容しつつ、PR 説明に「意図的な非対称」と**明記**して後年の誤読を防ぐ。既存挙動の保持（リリースノートは `CHANGELOG.md` から `--release-notes`、バージョンは `-X main.version={{ .Tag }}`）も確認する

### 配布チャネルは段階導入し、土台と tap/bucket を分ける
- 「配布を考えて」と Homebrew/Scoop まで一気に入れると、tap/bucket リポジトリ作成・トークン・README 導線更新が absorb され、スコープが膨らむ
- **ルール**: まず archives + checksums の土台だけを入れ、Homebrew tap / Scoop bucket は別段階に切る。ユーザーが明示的に後回しにした配布チャネルを `.goreleaser.yaml` に先回りで足さない

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

### 単一値を取得する場合も CLIXML 混入を前提に「行スキャン」で抽出する (Issue #4, 2026-06-21)
- `Write-Output $env:SSH_CONNECTION` のように単一値を取得しても、出力には `#< CLIXML` ヘッダや末尾の `<Objs>` プログレス XML が混入する（実機 Windows OpenSSH で確認: `"#< CLIXML\r\n127.0.0.1 48542 127.0.0.1 22\r\n<Objs ...></Objs>"`）。これを `strings.TrimSpace` + `strings.Fields` で丸ごとトークン化するとフィールド数が4を超えて常にフォールバックに落ち、修正が silent no-op になっていた
- ユニットテスト（クリーンな値を渡す）では気付けず、実機プローブで初めて発覚した
- **ルール**: リモート PowerShell の出力から値を取り出すときは出力全体を1値として扱わない。`effectiveAdminKeysFromSshdT` と同様に行ごとに走査し、期待する形状（例: SSH_CONNECTION の「4フィールド = IP/port/IP/port」）に一致する行だけを抽出する。`strings.Contains` での有無判定だけでなくトークン抽出にも CLIXML 耐性を持たせる。ユニットテストには実機出力を写した CLIXML 混入ケースを必ず1つ含める

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

---

## 設計判断ログ（旧 implementation-notes.md より統合 / 2026-07-26）

実装中に下した判断と、その理由（採用しなかった代替案を含む）。以降の節は旧 `implementation-notes.md` の内容をそのまま移設したもの。

### Issue #4: sshd -T -C の client address を SSH_CONNECTION から導出 (2026-06-21)

#### パースを PowerShell ではなく Go 側に置いた
- 候補: (A) PowerShell ネイティブで `$env:SSH_CONNECTION` を分解してスペック生成（1ラウンドトリップ）、(B) SSH_CONNECTION を取得して Go でスペック生成（ラウンドトリップ追加）。
- (B) を採用。Issue が「パースロジックのユニットテスト」を明示要求しており、Match ルールは非 loopback ホストが必要で e2e 検証不能。Go の純粋関数なら CI で検証できる。ラウンドトリップ1回の追加コストは許容。

#### host も addr と同じ実 client IP に設定した（省略しない）
- Issue 制約「real addr と fake host=localhost を混ぜるな」「古い sshd は user/host/addr 必須」の両方を満たす。
- host を実 IP で供給することで sshd バージョン依存（host 必須要件）を回避。`Match Host <exact-ip>` 誤発火リスクは実質ゼロ（IP には通常 `Match Address` を使う）。
- `laddr`/`lport` も SSH_CONNECTION の server 側から導出し、Match LocalAddress/LocalPort にも対応。

#### 実機検証で CLIXML 汚染を発見し、行スキャン方式へ修正（当初実装のバグ）
- 当初は `strings.TrimSpace(connOut)` を `strings.Fields` で分解していたが、実機プローブで `Write-Output $env:SSH_CONNECTION` の出力に CLIXML（`#< CLIXML` ヘッダ + `<Objs>` XML）が混入することが判明。フィールド数が4超になり常にフォールバック＝ silent no-op だった。
- `parseSSHConnectionSpec`（単一値の検証・スペック生成）と `sshdMatchSpecSuffixFromOutput`（出力を行スキャンして一致行を抽出）に分割。`effectiveAdminKeysFromSshdT` と同じ行スキャン方針に揃えた。詳細は LESSONS.md。

#### 実機検証で確認できたこと / できなかったこと
- 確認済み（loopback 実機 127.0.0.1）: SSH_CONNECTION は EncodedCommand セッションで populate される / CLIXML 混入下でも実スペックを抽出できる / 実機 sshd が `laddr`/`lport` 込みスペックを exit 0 で受理する（フォールバックに落ちない）。
- 未確認（loopback では不可能）: `Match Address`/`Match Host` ルール下での誤判定の実際の解消。loopback クライアントでは新スペックの `addr=127.0.0.1` が旧固定値と機能的に同一のため、本来のバグシナリオは exercise されない。修正は correct-by-construction。非 loopback クライアントでの検証は今後の課題。
