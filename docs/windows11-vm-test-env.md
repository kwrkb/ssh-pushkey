# Windows 11 仮想マシンで統合テスト環境を作る

## 目的

既存の検証環境（WSL → ホスト Windows の `127.0.0.1:22`）では、稼働中の `sshd_config` の
`Match Group administrators` ブロックがコメントアウトされているため、管理者用の配置先
（`C:\ProgramData\ssh\administrators_authorized_keys`）を通せない。
初期設定の OpenSSH サーバーが入った Windows 11 仮想マシンを Hyper-V に立て、その経路を実機で確かめる。

この環境では次のことも確かめられる。

- 接続元が loopback 以外のアドレスになるので、`Match Address` の評価が本番に近い。
- 英語版の OS を入れれば、ACE を SID で指定して言語に依存しない作りになっているか確かめられる。

## 前提

- ホストは Windows 11 Pro / Enterprise / Education（Home では Hyper-V を使えない）。
- ゲスト OS は **Windows 11 Pro** を使う。プロダクトキーを入れずにインストールでき、ライセンス認証しなくても期限なく動く。認証しない場合はデスクトップに透かしが出て、壁紙などの個人用設定を変えられないが、テストには支障ない。
  - 認証するなら、ホストとは別のライセンスが要る。ホストのライセンスは流用できない（OEM 版はその PC のハードウェアに紐づき、リテール版は 1 本で 1 台）。
  - 期限付きでよければ、Windows 11 Enterprise 評価版（無料、90 日間）でも同じ手順で作れる。
- 特に断りがなければ、コマンドは **ホスト側の管理者 PowerShell** で実行する。

## 1. Hyper-V を有効にする（初回のみ）

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V -All
```

実行後に再起動する。WSL2 と同時に使っても問題ない。

## 2. ISO を入手する

Microsoft の「Windows 11 のダウンロード」ページの「x64 デバイス用 Windows 11 ディスク イメージ (ISO) をダウンロードする」で、
「Windows 11 (multi-edition ISO for x64 devices)」を選ぶ。言語は English を推奨する。
この ISO には Home と Pro の両方が入っており、インストール時にエディションを選ぶ。

## 3. 仮想マシンを作る

`$iso` は自分の環境に合わせて書き換える。

```powershell
$vm  = "pushkey-win11"
$iso = "C:\Users\<you>\Downloads\<Win11_xxx_English_x64>.iso"

New-VM -Name $vm -Generation 2 -MemoryStartupBytes 4GB `
  -NewVHDPath "C:\Hyper-V\$vm.vhdx" -NewVHDSizeBytes 64GB -SwitchName "Default Switch"
Set-VMProcessor -VMName $vm -Count 2
Set-VMMemory -VMName $vm -DynamicMemoryEnabled $false   # インストーラの 4GB 要件チェック対策

# Windows 11 は TPM 2.0 必須
Set-VMKeyProtector -VMName $vm -NewLocalKeyProtector
Enable-VMTPM -VMName $vm

Add-VMDvdDrive -VMName $vm -Path $iso
Set-VMFirmware -VMName $vm -FirstBootDevice (Get-VMDvdDrive -VMName $vm)
Start-VM $vm
vmconnect localhost $vm
```

起動直後に「Press any key to boot from CD or DVD」と表示されたら、すぐにキーを押す。
間に合わなかった場合は、仮想マシンを再起動してやり直す。

## 4. OS をインストールする

- プロダクトキーの入力画面では「I don't have a product key」を選び、エディションは **Windows 11 Pro** を選ぶ。
  Home を選ぶと、後からローカルアカウントを作りにくい。
- インストールの種類は「Custom: Install Windows only」を選び、空のディスクを指定する。
- 初期設定（OOBE）では Microsoft アカウントではなく、ローカルアカウントを作る。Pro 版は既定で Microsoft アカウントを求めてくるので、次のどちらかで回避する（ビルドによって効く方法が違う）。
  - Microsoft アカウントのサインイン画面で `Shift+F10` を押してコマンドプロンプトを開き、`start ms-cxh:localonly` を実行する。ローカルアカウントの作成画面が開く（2026-11 に入手した ISO の Pro 版ではこの方法で作れた）。
  - 効かない場合は、ネットワークを切ってからネットワーク必須の確認を外す。ホスト側で `Disconnect-VMNetworkAdapter -VMName pushkey-win11` を実行し、
    仮想マシンのコマンドプロンプトで次を実行する。再起動後のネットワーク画面に「I don't have internet」が出るので、それを選ぶ。

    ```
    reg add HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\OOBE /v BypassNRO /t REG_DWORD /d 1 /f
    shutdown /r /t 0
    ```

    OOBE が終わったら、ホスト側で `Connect-VMNetworkAdapter -VMName pushkey-win11 -SwitchName "Default Switch"` を実行してネットワークを戻す。
- ここで作るアカウントは仮想マシンの管理用（例: `vmadmin`）とし、テスト用のアカウントとは分ける。

## 5. OpenSSH サーバーを入れる（仮想マシン内の管理者 PowerShell）

Windows 11 には OpenSSH サーバーが最初から入っていないので、インストールしてからサービスを起動する。

```powershell
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service sshd -StartupType Automatic
Start-Service sshd

# ファイアウォール規則（通常はインストール時に作られる。無ければ作る）
if (-not (Get-NetFirewallRule -Name "OpenSSH-Server-In-TCP" -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule -Name OpenSSH-Server-In-TCP -DisplayName "OpenSSH Server" `
    -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22 -Profile Any
}
# インストール時に作られる規則は Private プロファイルだけのことがある。必ず全プロファイルにする
Set-NetFirewallRule -Name OpenSSH-Server-In-TCP -Profile Any
```

Default Switch のネットワークは「識別されていないネットワーク」として Public 扱いになる。
規則が `Private` のままだと、sshd が動いていてもホストからの接続が弾かれる（2026-11-14 の構築で実際に起きた）。

`sshd_config` は sshd を初めて起動したときに作られる。管理者用ブロックが有効になっているか確認する。

```powershell
Select-String -Path C:\ProgramData\ssh\sshd_config -Pattern "Match Group administrators" -Context 0,1
```

行頭に `#` が付いていなければ、管理者用の配置先が有効。

## 6. テスト用アカウントを作る（仮想マシン内）

グループは SID で指定するので、OS の言語に関係なく動く。

```powershell
$pw = Read-Host -AsSecureString "Test account password"   # 実行するとパスワードを聞かれる（入力は表示されない）
New-LocalUser pushadmin -Password $pw -PasswordNeverExpires
New-LocalUser pushuser  -Password $pw -PasswordNeverExpires
Add-LocalGroupMember -SID S-1-5-32-544 -Member pushadmin   # Administrators
Add-LocalGroupMember -SID S-1-5-32-545 -Member pushuser    # Users
Add-LocalGroupMember -SID S-1-5-32-545 -Member pushadmin   # Users
```

コマンドの中の文字列はパスワードを聞くときの表示で、パスワードそのものではない。書き換えずに実行し、聞かれたらパスワードを入力する。
上の書き方では 2 つのアカウントに同じパスワードが付く。分けたい場合は、アカウントごとに `Read-Host` を実行する。

パスワードは 1Password に保存しておき、手順 9 で `op://` で参照する。

## 7. IP アドレスを確認し、チェックポイントを取る（ホスト側）

チェックポイントは、ファイアウォールの設定まで仮想マシン側の作業をすべて終えてから取る。
先に取ると、そこへ戻したときに設定が消える。

```powershell
(Get-VMNetworkAdapter -VMName pushkey-win11).IPAddresses
Checkpoint-VM -Name pushkey-win11 -SnapshotName "clean-ssh"
```

- Default Switch の IP アドレスは、ホストを再起動すると変わることがある。テストの前に毎回確認する。
- チェックポイントを取っておけば、テストで鍵ファイルや ACL が変わっても `Restore-VMCheckpoint -VMName pushkey-win11 -Name clean-ssh -Confirm:$false` ですぐ戻せる。

## 8. WSL から届くようにする（ホスト側のポート転送）

ミラードネットワークの WSL には Default Switch（`vEthernet (Default Switch)`）に当たるインターフェースが無い。
仮想マシンの IP アドレス宛ての通信は LAN の既定ゲートウェイへ送られてしまい、WSL からは直接届かない。
そこで、ホストの loopback から仮想マシンの 22 番へ転送する。

まずホスト側で、仮想マシンの 22 番に直接届くことを確かめる。

```powershell
Test-NetConnection <VMのIP> -Port 22
```

`TcpTestSucceeded` が `False` なら、仮想マシン側の sshd かファイアウォール（手順 5）を見直す。

届いたら、**ホストの**管理者 PowerShell で転送を作る。仮想マシン接続のウィンドウの中で実行しないこと
（プロンプトが仮想マシンの管理用アカウントになっていたら、それは仮想マシンの中）。

```powershell
netsh interface portproxy add v4tov4 listenaddress=127.0.0.1 listenport=22022 connectaddress=<VMのIP> connectport=22
netsh interface portproxy show v4tov4
```

- 待ち受けるポートは、WSL 側で使われていない番号にする。WSL で sshd を動かしていると 2222 番などがふさがっていて、
  WSL から `127.0.0.1` に接続すると転送より先に WSL 自身の sshd につながる。WSL で `ss -ltn` を実行して空きを確かめる。
- 仮想マシンの IP アドレスが変わったら、`netsh interface portproxy delete v4tov4 listenaddress=127.0.0.1 listenport=22022` で消して作り直す。
- 仮想マシンからは、接続元がホストの Default Switch 側のアドレスに見える。loopback 以外なので、`Match Address` の評価は本番に近い。

WSL から、つながった先が仮想マシンの Windows の sshd であることを確かめる。

```bash
nc -zv 127.0.0.1 22022
ssh -v -p 22022 -o BatchMode=yes pushuser@127.0.0.1 exit 2>&1 | grep "remote software version"
```

`OpenSSH_for_Windows` と出れば仮想マシンにつながっている。`Ubuntu` などと出たら、WSL 自身の sshd につながっている。

## 9. 統合テストを回す（WSL）

Makefile が読む env ファイル名（`.env.integration` / `.env.integration.user`）は固定なので、`op run` で直接実行する。
接続先は手順 8 の転送（`127.0.0.1` の 22022 番）にする。値は環境変数で渡す。`op run` は環境変数の中の `op://` 参照も解決する。

```bash
SSH_TEST_HOST=127.0.0.1 SSH_TEST_PORT=22022 SSH_TEST_USER=pushadmin \
  SSH_TEST_PASSWORD=op://<vault>/<item>/password SSH_TEST_EXPECT_KEYFILE=admin \
  op run -- go test -tags=integration -count=1 -v ./...

SSH_TEST_HOST=127.0.0.1 SSH_TEST_PORT=22022 SSH_TEST_USER=pushuser \
  SSH_TEST_PASSWORD=op://<vault>/<item>/password SSH_TEST_EXPECT_KEYFILE=user \
  op run -- go test -tags=integration -count=1 -v ./...
```

同じ値を `export` で並べた env ファイル（例: `.env.integration.vm`）を作り、`op run --env-file=<ファイル>` で渡してもよい。
`.env.integration*` はすべて gitignore されている。

- `SSH_TEST_EXPECT_KEYFILE=admin` は、手順 5 で管理者用ブロックが有効だと確認できた場合の値。
  テスト対象（`resolveKeyFileTarget`）の判定から決めず、仮想マシンの `sshd_config` から決める。
- `-count=1` を外すと、結果がキャッシュされて実機に接続しないまま PASS になる。
- 2 つのアカウントをシェルのループで回すときは、zsh では `set -- $var` で単語が分割されない点に注意する。
  分割されないと `SSH_TEST_USER` に「pushadmin admin」のような存在しないユーザー名が入り、認証で全件失敗する。

### `.ssh` が無い状態から回す意味

`TestIntegration_ExistingFilesStayReadable` は、`.ssh` が親から ACE を継承している場合だけ意味のある検証になる。
`pushuser` の `.ssh` が無い状態（チェックポイント `clean-ssh` に戻した直後など）から回すこと。
一度配置して継承が外れた `.ssh` では、修正前のコードでも通ってしまう。

修正前のコード（v1.9.2 まで）では、この状態から回すと `TestIntegration_KeyAuthAfterDeploy` が鍵ファイルの復元で失敗し、
後続のテストがロックファイルで止まった。配置処理が `.ssh` の継承を外すときに、中にあった退避用の目印とロックのファイルから
ACE がすべて消えたためで、製品側の不具合だった（既存の `known_hosts` なども読めなくなっていた。PR #40 で修正）。
古いコードで回して同じ状態になったら、`pushuser` で次を実行して片付ける。

```powershell
$d = Join-Path $env:USERPROFILE ".ssh"
$sid = ([Security.Principal.WindowsIdentity]::GetCurrent()).User.Value
Get-ChildItem -LiteralPath $d -Force | ForEach-Object { icacls $_.FullName /grant "*${sid}:(F)" | Out-Null; Remove-Item -LiteralPath $_.FullName -Force }
Remove-Item -LiteralPath $d -Force
```

ACE が空のファイルも持ち主は ACL を付け直せるので、付け直してから消す。`icacls /reset` は親に継承できる ACE が無いので効かない。

## 検証記録

| 日付 | アカウント | 期待値 | 結果 |
|---|---|---|---|
| 2026-11-14 | `pushadmin` | admin | 8 件すべて PASS。`administrators_authorized_keys` への配置と、その鍵での認証を実機で初めて確認 |
| 2026-11-14 | `pushuser` | user | 3 件 PASS、5 件 FAIL（修正前のコード。上記の ACL 不具合） |
| 2026-11-15 | `pushuser` | user | PR #40 のコードで、チェックポイントに戻した状態から 9 件すべて PASS |
| 2026-11-15 | `pushadmin` | admin | PR #40 のコードで 9 件すべて PASS。`C:\ProgramData\ssh` の中のファイルの ACL はテスト前と一致 |

環境: Windows 11 Pro（English）、OpenSSH_for_Windows_9.5、既定の `sshd_config`（管理者用ブロックは有効）。
