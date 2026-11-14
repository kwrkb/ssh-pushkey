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
- ゲスト OS は **Windows 11 Enterprise 評価版**（無料、90 日間）を使う。ホストの Windows のライセンスは流用できない（OEM 版はその PC のハードウェアに紐づき、リテール版は 1 本で 1 台）。
- 特に断りがなければ、コマンドは **ホスト側の管理者 PowerShell** で実行する。

## 1. Hyper-V を有効にする（初回のみ）

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V -All
```

実行後に再起動する。WSL2 と同時に使っても問題ない。

## 2. ISO を入手する

Microsoft Evaluation Center から **Windows 11 Enterprise** の ISO（64-bit、English 推奨）をダウンロードする。

## 3. 仮想マシンを作る

`$iso` は自分の環境に合わせて書き換える。

```powershell
$vm  = "pushkey-win11"
$iso = "C:\Users\<you>\Downloads\<Windows11EnterpriseEval>.iso"

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

- インストールの種類は「Custom: Install Windows only」を選び、空のディスクを指定する。
- 初期設定（OOBE）では Microsoft アカウントではなく、ローカルアカウントを作る。
  - Enterprise 版はサインイン画面の「Sign-in options」→「Domain join instead」でローカルアカウントを作れる。
  - その選択肢がない場合は、`Shift+F10` で開いたコマンドプロンプトから `start ms-cxh:localonly` を試す（ビルドによって挙動が違う）。
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
```

`sshd_config` は sshd を初めて起動したときに作られる。管理者用ブロックが有効になっているか確認する。

```powershell
Select-String -Path C:\ProgramData\ssh\sshd_config -Pattern "Match Group administrators" -Context 0,1
```

行頭に `#` が付いていなければ、管理者用の配置先が有効。

## 6. テスト用アカウントを作る（仮想マシン内）

グループは SID で指定するので、OS の言語に関係なく動く。

```powershell
$pw = Read-Host -AsSecureString "password"
New-LocalUser pushadmin -Password $pw -PasswordNeverExpires
New-LocalUser pushuser  -Password $pw -PasswordNeverExpires
Add-LocalGroupMember -SID S-1-5-32-544 -Member pushadmin   # Administrators
Add-LocalGroupMember -SID S-1-5-32-545 -Member pushuser    # Users
Add-LocalGroupMember -SID S-1-5-32-545 -Member pushadmin   # Users
```

パスワードは 1Password に保存しておき、手順 9 の env ファイルから `op://` で参照する。

## 7. IP アドレスを確認し、チェックポイントを取る（ホスト側）

```powershell
(Get-VMNetworkAdapter -VMName pushkey-win11).IPAddresses
Checkpoint-VM -Name pushkey-win11 -SnapshotName "clean-ssh"
```

- Default Switch の IP アドレスは、ホストを再起動すると変わることがある。テストの前に毎回確認する。
- チェックポイントを取っておけば、テストで鍵ファイルや ACL が変わっても `Restore-VMCheckpoint -VMName pushkey-win11 -Name clean-ssh -Confirm:$false` ですぐ戻せる。

## 8. WSL から接続できるか確かめる

```bash
nc -zv <VMのIP> 22
ssh pushuser@<VMのIP>
```

WSL から届かない場合は、ホスト側で `Test-NetConnection <VMのIP> -Port 22` を実行し、どこで止まっているかを切り分ける。

- ホストからも届かない場合は、仮想マシン側の sshd かファイアウォールを疑う。
- ホストからは届くが WSL から届かない場合は、WSL のネットワークモード（ミラード）や Hyper-V ファイアウォールの設定を疑う。

## 9. 統合テストを回す（WSL）

Makefile が読む env ファイル名（`.env.integration` / `.env.integration.user`）は固定なので、
仮想マシン用に別名のファイルを作り、`op run` で直接実行する。`.env.integration*` はすべて gitignore されている。

`.env.integration.vm`（管理者用）:

```
export SSH_TEST_HOST=<VMのIP>
export SSH_TEST_USER=pushadmin
export SSH_TEST_PASSWORD=op://<vault>/<item>/password
export SSH_TEST_EXPECT_KEYFILE=admin
```

`.env.integration.vm.user`（一般用）: `SSH_TEST_USER=pushuser`、`SSH_TEST_EXPECT_KEYFILE=user` に変えて作る。

`SSH_TEST_EXPECT_KEYFILE=admin` は、手順 5 で管理者用ブロックが有効だと確認できた場合の値。
テスト対象（`resolveKeyFileTarget`）の判定から決めず、仮想マシンの `sshd_config` から決める。

```bash
op run --env-file=.env.integration.vm -- go test -tags=integration -count=1 -v ./...
op run --env-file=.env.integration.vm.user -- go test -tags=integration -count=1 -v ./...
```

`-count=1` を外すと、結果がキャッシュされて実機に接続しないまま PASS になる。
