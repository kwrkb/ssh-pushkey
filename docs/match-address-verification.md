# issue-4 実機検証手順書: sshd Match Address 導出修正

> **対象 issue:** [#4](https://github.com/kwrkb/ssh-pushkey/issues/4)（修正本体） / [#17](https://github.com/kwrkb/ssh-pushkey/issues/17)（非 loopback での e2e 検証）
> **検証目的:** Windows OpenSSH 環境で、`ssh-pushkey` が `sshd -T` に渡す client IP を loopback 固定ではなく **実接続元 IP** から導出するようになったことを、実機で確認する。
> **検証日:** 2026-06-21
> **結果:** Pass（修正前は誤判定、修正後は正しく user ディレクトリへ deploy）

---

## 1. 背景: 何を検証するのか

`ssh-pushkey` は公開鍵の配置先を決めるため、接続先で `sshd -T` を実行して有効な `AuthorizedKeysFile` を取得する。

```
sshd -T -C addr=<IP> -C user=<USER>
  → admin 系パス  → administrators_authorized_keys
  → user 系パス   → ~/.ssh/authorized_keys
```

### バグの核心

修正前のコードは `-C addr=` に **`127.0.0.1`（loopback 固定）** を渡していた。
そのため `Match Address` で接続元 IP ごとに配置先を切り替える構成下で、
実際の接続元 IP では選ばれるはずの user パスではなく、admin パスを誤って選んでいた。

### 修正内容

`SSH_CONNECTION` 環境変数から **実際の接続元 IP** を取得し、それを `sshd -T -C addr=` に渡すように変更。

---

## 2. 検証環境

| 役割 | マシン | IP | 備考 |
|------|--------|-----|------|
| 接続元（クライアント） | Mac | `192.0.2.10` | `ssh-pushkey` 実行側 |
| 接続先（サーバ） | WIN-SERVER | `192.0.2.20` | Windows 11 + OpenSSH |

検証用ローカルアカウント（Windows 側に作成）:

| アカウント | グループ | 判定上の役割 |
|------------|----------|--------------|
| `pushadmin` | Administrators | admin 判定の対象 |
| `pushuser` | Users | 対照群（addr に依存せず常に user パス） |

> アカウントは PowerShell（管理者）で作成。
> ```powershell
> $pass = ConvertTo-SecureString "<password>" -AsPlainText -Force
> New-LocalUser "pushadmin" -Password $pass -PasswordNeverExpires
> New-LocalUser "pushuser"  -Password $pass -PasswordNeverExpires
> Add-LocalGroupMember -Group "Administrators" -Member "pushadmin"
> ```

---

## 3. 検証台（harness）の構築

### 3.1 sshd_config の編集

`C:\ProgramData\ssh\sshd_config` の末尾を以下の構成にする。

```sshd-config
AuthorizedKeysFile .ssh/authorized_keys

Match Address 192.0.2.10
    AuthorizedKeysFile .ssh/authorized_keys

Match User pushadmin
    AuthorizedKeysFile __PROGRAMDATA__/ssh/administrators_authorized_keys
```

**ブロックの順序が決定的に重要**（理由は §3.2）。

#### 注意点（ハマりどころ）

- **`Match Group administrators` は使わない。** Windows のオフライン `sshd -T -C user=...` 評価では
  グループ解決ができず（`ga_init, unable to resolve user` 警告が出る）、Match Group が発火しない。
  代わりに **`Match User pushadmin`**（文字列マッチでグループ解決不要）を使う。
- `User`（アカウント名）と `Group`（グループ名）は別物。`Match Group pushadmin` と書くと
  「pushadmin というグループ」を探して当たらないので注意。

### 3.2 Match の評価順序（first-wins）

検証で実測した結果、Windows OpenSSH の `AuthorizedKeysFile` は
**最初にマッチした Match ブロックが勝つ（first-wins）** 挙動だった。

したがって:

- `Match Address`（実IP用）を**先**に置く
- `Match User pushadmin`（admin baseline）を**後**に置く

この順序により:

| 接続元 addr | 先にマッチするブロック | 配置先 |
|-------------|------------------------|--------|
| `192.0.2.10`（実IP） | `Match Address` | `.ssh/authorized_keys` |
| `127.0.0.1`（loopback） | `Match Address` 不一致 → `Match User` | `administrators_authorized_keys` |

> 当初 `Match User` を先に置いたところ、実IPでも admin パスが返って差が出なかった。
> first-wins のため、**実IP用の `Match Address` を必ず先頭に置くこと。**

### 3.3 sshd 再起動

```powershell
Restart-Service sshd
```

---

## 4. 事前確認: harness が「割れる」ことの検証

クライアントを動かす前に、`sshd -T` を直接叩いて
loopback と実IPで配置先が割れることを確認する。

```powershell
$sshd = 'C:\Program Files\OpenSSH\sshd.exe'

# 旧コード相当（loopback）
& $sshd -T -C "user=pushadmin,host=localhost,addr=127.0.0.1" `
  -f C:\ProgramData\ssh\sshd_config | Select-String AuthorizedKeysFile

# 新コード相当（実IP）
& $sshd -T -C "user=pushadmin,host=client,addr=192.0.2.10" `
  -f C:\ProgramData\ssh\sshd_config | Select-String AuthorizedKeysFile
```

### 期待結果（harness 成立条件）

| addr | 期待される出力 |
|------|----------------|
| `127.0.0.1` | `administrators_authorized_keys` |
| `192.0.2.10` | `.ssh/authorized_keys` |

> ここで割れれば、「旧コードなら誤判定し、新コードなら正解する」検証台が完成している。
> `ga_init, unable to resolve user pushadmin` の警告は出るが、`Match User` は文字列マッチで機能するため無視してよい。
> `sshd -T` の実行には管理者権限が必要。

---

## 5. 実機検証

### 5.1 修正前（バグ再現）

```bash
# Mac 側
ssh-pushkey pushadmin@192.0.2.20
```

修正前バイナリの出力（バグ再現）:

```
=> User is in Administrators group
=> administrators_authorized_keys is enabled (sshd -T)
=> Target: sshd -T: administrators_authorized_keys
=> Deploying public key...
=> Key deployment completed!
```

Mac の実IP（`192.0.2.10`）は `Match Address` にマッチするので
本来 `.ssh/authorized_keys` が選ばれるべきだが、loopback を渡しているため
`administrators_authorized_keys` を誤選択 → **バグが再現。**

### 5.2 修正後

```bash
# Mac 側（master にマージ済みなら master をビルド）
go build -o ssh-pushkey-dev
./ssh-pushkey-dev pushadmin@192.0.2.20
```

期待される出力:

```
=> User is in Administrators group
=> sshd -T: AuthorizedKeysFile does NOT point to administrators_authorized_keys
=> Deploying to user directory (~/.ssh/authorized_keys)
=> Key deployment completed!
```

### 5.3 結果確認（Windows 側）

```powershell
# 修正後はここに鍵が入る（期待どおり）
Get-Content C:\Users\pushadmin\.ssh\authorized_keys

# ここに増えていなければ誤判定なし
Get-Content C:\ProgramData\ssh\administrators_authorized_keys
```

`C:\Users\pushadmin\.ssh\authorized_keys` に鍵が配置され、
`administrators_authorized_keys` に混入していなければ **検証成功**。

---

## 6. ハマりどころ（教訓）

| 現象 | 原因 | 対処 |
|------|------|------|
| 何度実行しても旧挙動が出る | **配置先に既存鍵が残っており、ツールが deploy をスキップ** | 検証前に配置先の鍵を消してクリーンな状態にする |
| `sshd -T` で両方同じパスが返る | `Match Group` がオフライン評価で発火しない / Match の順序ミス | `Match User` を使う / `Match Address` を先頭に置く |
| 実IPでも admin パスが返る | Match の評価が first-wins のため、Match User が先で勝っていた | `Match Address` を先、`Match User` を後に |
| 古いバイナリを実行していた | `go/bin` の旧 `go install` 済みバイナリを PATH 経由で叩いていた | `go build -o <別名>` で明示的に作り直して実行 |

> **最大のハマりどころ:** 既存鍵の重複が deploy スキップを招き、修正の効果が見えなくなる。
> 検証は必ず配置先の鍵を消したクリーンな状態から行うこと。

---

## 7. 後始末

```powershell
# sshd_config を検証前の状態に復元
Copy-Item C:\ProgramData\ssh\sshd_config.bak C:\ProgramData\ssh\sshd_config -Force
Restart-Service sshd

# 検証で配置した鍵を削除
Remove-Item C:\Users\pushadmin\.ssh\authorized_keys -ErrorAction SilentlyContinue
```

- `pushadmin` / `pushuser` アカウントは権限テスト等で再利用可能なため残してよい。
  残す場合は検証用パスワードをランダム化推奨:
  ```powershell
  Set-LocalUser pushadmin -Password (Read-Host "新パスワード" -AsSecureString)
  ```
- `administrators_authorized_keys` は config 復元後は参照されなくなるため、残しても無害。

---

## 8. 結論

- 修正前: loopback 固定により実IP環境で admin パスを誤選択（バグ再現を確認）。
- 修正後: `SSH_CONNECTION` 由来の実IPで `sshd -T` を評価し、`Match Address` にマッチして
  user ディレクトリへ正しく deploy。
- **issue-4 の修正は実機で正しく機能することを確認。issue-17 のクローズ要件を満たす。**
