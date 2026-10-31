//go:build integration

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// 実行方法（パスワード手打ち）:
//   read -rs SSH_TEST_PASSWORD && export SSH_TEST_PASSWORD
//   SSH_TEST_HOST=192.168.1.10 SSH_TEST_USER=kiwar go test -tags=integration -v ./...

type testEnv struct {
	host     string
	port     int
	user     string
	password string
}

func loadTestEnv(t *testing.T) testEnv {
	t.Helper()

	host := os.Getenv("SSH_TEST_HOST")
	user := os.Getenv("SSH_TEST_USER")
	password := os.Getenv("SSH_TEST_PASSWORD")

	if host == "" || user == "" || password == "" {
		t.Skip("SSH_TEST_HOST, SSH_TEST_USER, SSH_TEST_PASSWORD are required")
	}

	port := 22
	if p := os.Getenv("SSH_TEST_PORT"); p != "" {
		var err error
		port, err = strconv.Atoi(p)
		if err != nil {
			t.Fatalf("invalid SSH_TEST_PORT: %s", p)
		}
	}

	return testEnv{host: host, port: port, user: user, password: password}
}

func TestIntegration_SSHConnect(t *testing.T) {
	env := loadTestEnv(t)

	client, err := dialSSH(env.user, env.host, env.port, env.password, true)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()
}

func TestIntegration_RemotePowerShell(t *testing.T) {
	env := loadTestEnv(t)

	client, err := dialSSH(env.user, env.host, env.port, env.password, true)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()

	// 変数展開が正しく動くことを確認（-EncodedCommandの検証）
	output, err := runRemotePowerShell(client, "$x = 'hello'; Write-Output $x")
	if err != nil {
		t.Fatalf("PowerShell execution failed: %v", err)
	}

	// PowerShellはモジュール初期化時にCLIXMLプログレスを出力することがある
	if !strings.Contains(output, "hello") {
		t.Errorf("output should contain %q, got %q", "hello", output)
	}
}

func TestIntegration_AdminDetection(t *testing.T) {
	env := loadTestEnv(t)

	client, err := dialSSH(env.user, env.host, env.port, env.password, true)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()

	// パニックせず結果を返すことを確認（isAdmin True/False どちらでもOK）
	target, err := resolveKeyFileTarget(client)
	if err != nil {
		t.Fatalf("resolveKeyFileTarget failed: %v", err)
	}
	t.Logf("target = %+v", target)
}

func TestIntegration_DeployKey(t *testing.T) {
	env := loadTestEnv(t)

	pubKey, _, err := resolveKey(os.Getenv("SSH_TEST_PUBKEY"))
	if err != nil {
		t.Fatalf("failed to read public key: %v", err)
	}

	client, err := dialSSH(env.user, env.host, env.port, env.password, true)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()

	if err := DeployKey(client, pubKey, false); err != nil {
		t.Fatalf("key deployment failed: %v", err)
	}

	// 2回目は追記せず（重複判定）、ACL の再設定だけで成功することを確認
	if err := DeployKey(client, pubKey, false); err != nil {
		t.Fatalf("second deployment failed: %v", err)
	}
}

// TestIntegration_DuplicateByBlob は blob 単位の重複判定（PowerShell 実行時挙動）を実機で検証する。
// ユニットテストは生成スクリプトの文字列一致しか見られないため、実際の
// Get-Content ループ / -split / -ceq 比較が意図通り動くかはここでしか確認できない。
func TestIntegration_DuplicateByBlob(t *testing.T) {
	env := loadTestEnv(t)

	pubKey, _, err := resolveKey(os.Getenv("SSH_TEST_PUBKEY"))
	if err != nil {
		t.Fatalf("failed to read public key: %v", err)
	}

	client, err := dialSSH(env.user, env.host, env.port, env.password, true)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()

	// 鍵を配置しておく（既にあれば idempotent にスキップされる）
	if err := DeployKey(client, pubKey, false); err != nil {
		t.Fatalf("key deployment failed: %v", err)
	}

	target, err := resolveKeyFileTarget(client)
	if err != nil {
		t.Fatalf("resolveKeyFileTarget failed: %v", err)
	}
	blob, err := pubKeyBlob(pubKey)
	if err != nil {
		t.Fatalf("pubKeyBlob failed: %v", err)
	}

	// 核心: 同一 blob・別コメントでも重複と判定されること（dry-run の DRY_RUN_DUP を確認）
	variant := blob + " a-different-comment@elsewhere"
	out, err := runRemotePowerShell(client, buildDeployScript(variant, blob, target.isAdmin, true))
	if err != nil {
		t.Fatalf("dry-run (variant) failed: %v", err)
	}
	if !strings.Contains(out, "DRY_RUN_DUP:True") {
		t.Errorf("same blob with a different comment must be detected as duplicate\noutput:\n%s", out)
	}

	// false-positive 防止: 別の鍵は重複と誤判定されないこと
	otherBlob := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINOTAREALKEYforDedupFalsePositiveCheck00"
	out, err = runRemotePowerShell(client, buildDeployScript(otherBlob+" x@y", otherBlob, target.isAdmin, true))
	if err != nil {
		t.Fatalf("dry-run (other) failed: %v", err)
	}
	if !strings.Contains(out, "DRY_RUN_DUP:False") {
		t.Errorf("a distinct key must not be flagged as duplicate\noutput:\n%s", out)
	}

	// options 前置行（command="..." <type> <base64> ...）でも鍵本体を検知できること。
	// buildDeployScript と同じ式で $keyFile を決め、remote 側でバックアップ → option 行のみで上書き →
	// dry-run スキャン → 必ず元へ復元する（残置すると実ホスト上の有効な authorization になるため）。
	// 平文の鍵行も残すと壊れたスキャナでも True になり判別力が失われるので、敢えて option 行だけにする。
	// バックアップ／復元は remote PowerShell 内で完結させ、ファイル内容を Go 側へ吸い上げない
	// （runRemotePowerShell の出力には CLIXML 初期化ノイズが混ざり base64 を汚染するため）。
	keyFileExpr := "$keyFile = Join-Path $env:USERPROFILE '.ssh\\authorized_keys'"
	if target.isAdmin {
		keyFileExpr = "$keyFile = 'C:\\ProgramData\\ssh\\administrators_authorized_keys'"
	}
	bakExpr := keyFileExpr + "; $bak = \"$keyFile.pushkey-test-bak\""

	// バックアップ（既存 bak は除去してから、元ファイルがあればコピー）。
	if _, err := runRemotePowerShell(client, bakExpr+
		"; if (Test-Path $bak) { Remove-Item -Force $bak }"+
		"; if (Test-Path $keyFile) { Copy-Item $keyFile $bak }"); err != nil {
		t.Fatalf("failed to back up authorized_keys: %v", err)
	}

	defer func() {
		// 復元: bak があれば戻す（move なので bak も消える）。無ければ元々ファイル無しなので option 行ファイルを削除。
		restore := bakExpr +
			"; if (Test-Path $bak) { Move-Item -Force $bak $keyFile } " +
			"else { if (Test-Path $keyFile) { Remove-Item -Force $keyFile } }"
		if _, rerr := runRemotePowerShell(client, restore); rerr != nil {
			t.Errorf("failed to restore authorized_keys (manual cleanup may be required): %v", rerr)
		}
	}()

	// command="..." 付きで test 鍵 blob を唯一の行として書き込む（UTF-8 BOM なし、末尾改行付き）。
	optionLine := `command="echo hi" ` + blob + " injected-with-options"
	escapedLine := strings.ReplaceAll(optionLine, "'", "''")
	if _, err := runRemotePowerShell(client, keyFileExpr+fmt.Sprintf(
		"; $enc = New-Object System.Text.UTF8Encoding($false); [IO.File]::WriteAllText($keyFile, '%s' + \"`n\", $enc)",
		escapedLine)); err != nil {
		t.Fatalf("failed to inject option-bearing line: %v", err)
	}

	out, err = runRemotePowerShell(client, buildDeployScript(pubKey, blob, target.isAdmin, true))
	if err != nil {
		t.Fatalf("dry-run (option-bearing) failed: %v", err)
	}
	if !strings.Contains(out, "DRY_RUN_DUP:True") {
		t.Errorf("a key present on an options-prefixed line must be detected as duplicate\noutput:\n%s", out)
	}
}

// TestIntegration_AppendToFileWithoutTrailingNewline は、末尾改行なしで終わる既存の
// authorized_keys へ鍵を追記しても新旧の鍵が1行に連結しない（末尾改行ガードが実機で効く）
// ことを検証する。ユニットテストは生成スクリプトの文字列順序しか見られず、PowerShell の
// ReadAllBytes 判定 / AppendAllText の実挙動はここでしか確認できない。
func TestIntegration_AppendToFileWithoutTrailingNewline(t *testing.T) {
	env := loadTestEnv(t)

	pubKey, _, err := resolveKey(os.Getenv("SSH_TEST_PUBKEY"))
	if err != nil {
		t.Fatalf("failed to read public key: %v", err)
	}
	blob, err := pubKeyBlob(pubKey)
	if err != nil {
		t.Fatalf("pubKeyBlob failed: %v", err)
	}

	client, err := dialSSH(env.user, env.host, env.port, env.password, true)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()

	target, err := resolveKeyFileTarget(client)
	if err != nil {
		t.Fatalf("resolveKeyFileTarget failed: %v", err)
	}

	// $keyFile の決定は buildDeployScript と同じ式に合わせる。
	keyFileExpr := "$keyFile = Join-Path $env:USERPROFILE '.ssh\\authorized_keys'"
	if target.isAdmin {
		keyFileExpr = "$keyFile = 'C:\\ProgramData\\ssh\\administrators_authorized_keys'"
	}
	bakExpr := keyFileExpr + "; $bak = \"$keyFile.pushkey-test-bak\""

	// バックアップ（既存 bak は除去 → 元ファイルがあればコピー）。.ssh ディレクトリが無いと
	// WriteAllText が失敗するため作成しておく。
	if _, err := runRemotePowerShell(client, bakExpr+
		"; $d = Split-Path -Parent $keyFile; if (-not (Test-Path $d)) { New-Item -ItemType Directory -Path $d -Force | Out-Null }"+
		"; if (Test-Path $bak) { Remove-Item -Force $bak }"+
		"; if (Test-Path $keyFile) { Copy-Item $keyFile $bak }"); err != nil {
		t.Fatalf("failed to back up authorized_keys: %v", err)
	}

	defer func() {
		// 復元: bak があれば戻す。無ければ元々ファイル無しなのでテスト生成ファイルを削除。
		restore := bakExpr +
			"; if (Test-Path $bak) { Move-Item -Force $bak $keyFile } " +
			"else { if (Test-Path $keyFile) { Remove-Item -Force $keyFile } }"
		if _, rerr := runRemotePowerShell(client, restore); rerr != nil {
			t.Errorf("failed to restore authorized_keys (manual cleanup may be required): %v", rerr)
		}
	}()

	// 配置する鍵とは別 blob の既存行を、末尾改行なし（核心条件）で書き込む。
	// 別 blob なので DeployKey は重複と判定せず必ず追記する。
	seedLine := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITRAILINGNEWLINESEEDKEY000000000000000 seed@host"
	escapedSeed := strings.ReplaceAll(seedLine, "'", "''")
	if _, err := runRemotePowerShell(client, keyFileExpr+fmt.Sprintf(
		"; $enc = New-Object System.Text.UTF8Encoding($false); [IO.File]::WriteAllText($keyFile, '%s', $enc)",
		escapedSeed)); err != nil {
		t.Fatalf("failed to seed file without trailing newline: %v", err)
	}

	// 実書き込み（dry-run でない）で鍵を追記する。
	if err := DeployKey(client, pubKey, false); err != nil {
		t.Fatalf("key deployment failed: %v", err)
	}

	// 検証は remote 側で完結させ、行数と blob 存在のみをマーカー出力する
	// （ファイル内容を Go 側へ吸い上げると CLIXML 初期化ノイズが混ざるため）。
	escapedBlob := strings.ReplaceAll(blob, "'", "''")
	check := keyFileExpr + fmt.Sprintf(
		"; $lines = @(Get-Content $keyFile | Where-Object { $_.Trim() -ne '' })"+
			"; Write-Output \"LINECOUNT:$($lines.Count)\""+
			"; $hasNew = ($lines | Where-Object { $_ -like '*%s*' }).Count"+
			"; Write-Output \"HASNEW:$hasNew\"",
		escapedBlob)
	out, err := runRemotePowerShell(client, check)
	if err != nil {
		t.Fatalf("failed to inspect appended file: %v", err)
	}

	// 末尾改行なしファイルでも seed 行と新鍵が別行になり、合計2行であること。
	// 連結バグが再発すると1行になり LINECOUNT:1 となる。
	if !strings.Contains(out, "LINECOUNT:2") {
		t.Errorf("appending to a file without a trailing newline must keep keys on separate lines (want 2 lines)\noutput:\n%s", out)
	}
	if !strings.Contains(out, "HASNEW:1") {
		t.Errorf("the newly deployed key must be present on its own line\noutput:\n%s", out)
	}
}

// TestIntegration_ReapplyACLOnExistingKey は、鍵が既に配置済みでも DeployKey が ACL を
// 設定し直すことを検証する。前回の実行で追記後に icacls が失敗した状態を、継承の再有効化で
// 再現する（継承 ACE が残り、保護フラグが外れた状態）。修正前は重複判定で即終了していたため、
// 再実行しても ACL は崩れたままだった。
func TestIntegration_ReapplyACLOnExistingKey(t *testing.T) {
	env := loadTestEnv(t)

	pubKey, _, err := resolveKey(os.Getenv("SSH_TEST_PUBKEY"))
	if err != nil {
		t.Fatalf("failed to read public key: %v", err)
	}

	client, err := dialSSH(env.user, env.host, env.port, env.password, true)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()

	// 鍵を配置済みの状態にする（既にあれば重複扱い）
	if err := DeployKey(client, pubKey, false); err != nil {
		t.Fatalf("key deployment failed: %v", err)
	}

	target, err := resolveKeyFileTarget(client)
	if err != nil {
		t.Fatalf("resolveKeyFileTarget failed: %v", err)
	}

	// $keyFile / $sshDir の決定は buildDeployScript と同じ式に合わせる。
	pathExpr := "$keyFile = Join-Path $env:USERPROFILE '.ssh\\authorized_keys'; $sshDir = Join-Path $env:USERPROFILE '.ssh'"
	if target.isAdmin {
		pathExpr = "$keyFile = 'C:\\ProgramData\\ssh\\administrators_authorized_keys'; $sshDir = 'C:\\ProgramData\\ssh'"
	}

	// 後始末: DeployKey が修復できなかった場合でも、ディレクトリと鍵ファイルの継承を外して
	// 3 SID に戻す（崩れたまま残すと sshd が鍵ファイルを拒否し得る）。
	defer func() {
		restore := pathExpr + "; $sid = ([System.Security.Principal.WindowsIdentity]::GetCurrent()).User.Value" +
			"; foreach ($p in @($sshDir, $keyFile)) { icacls $p /inheritance:r /grant '*S-1-5-18:(F)' /grant '*S-1-5-32-544:(F)' /grant \"*${sid}:(F)\" | Out-Null }"
		if _, rerr := runRemotePowerShell(client, restore); rerr != nil {
			t.Errorf("failed to restore ACL (manual cleanup may be required): %v", rerr)
		}
	}()

	// ACL を崩す: ディレクトリと鍵ファイルの継承を再有効化する
	if out, err := runRemotePowerShell(client, pathExpr+
		"; icacls $sshDir /inheritance:e | Out-Null; icacls $keyFile /inheritance:e | Out-Null"+
		"; if ((Get-Acl $keyFile).AreAccessRulesProtected) { Write-Output 'CORRUPT:False' } else { Write-Output 'CORRUPT:True' }"); err != nil {
		t.Fatalf("failed to corrupt ACL: %v", err)
	} else if !hasMarkerLine(out, "CORRUPT:True") {
		t.Fatalf("precondition: key file ACL should be inheritable after corruption\noutput:\n%s", out)
	}

	// 再実行で修復されること
	if err := DeployKey(client, pubKey, false); err != nil {
		t.Fatalf("redeployment failed: %v", err)
	}

	// 検証は remote 側で完結させ、マーカーだけを返す。継承が外れ（保護フラグ）、継承 ACE が無く、
	// SYSTEM / Administrators / 実行ユーザーの FullControl が揃っていること。
	// 3 SID 以外の明示 ACE は数えない。icacls /grant は既存の明示 ACE を消さないため（設計上の範囲外）、
	// 他テストのバックアップ・復元で作り直したファイルには作成者トークン既定の
	// LogonSessionId（S-1-5-5-0-*）の読み取り ACE が残る。
	check := pathExpr + "; $sid = ([System.Security.Principal.WindowsIdentity]::GetCurrent()).User.Value" +
		"; $allowed = @('S-1-5-18', 'S-1-5-32-544', $sid)" +
		"; foreach ($e in @(@('DIR', $sshDir), @('FILE', $keyFile))) {" +
		"  $acl = Get-Acl $e[1]" +
		"; $rules = @($acl.GetAccessRules($true, $true, [System.Security.Principal.SecurityIdentifier]))" +
		"; $inherited = @($rules | Where-Object { $_.IsInherited }).Count" +
		"; $full = @($rules | Where-Object { $allowed -contains $_.IdentityReference.Value -and $_.AccessControlType -eq 'Allow' -and $_.FileSystemRights -eq 'FullControl' }).Count" +
		"; Write-Output \"$($e[0])_PROTECTED:$($acl.AreAccessRulesProtected)\"" +
		"; Write-Output \"$($e[0])_INHERITED:$inherited\"" +
		"; Write-Output \"$($e[0])_FULL:$full\"" +
		" }"
	out, err := runRemotePowerShell(client, check)
	if err != nil {
		t.Fatalf("failed to inspect ACL: %v", err)
	}
	for _, want := range []string{
		"DIR_PROTECTED:True", "DIR_INHERITED:0", "DIR_FULL:3",
		"FILE_PROTECTED:True", "FILE_INHERITED:0", "FILE_FULL:3",
	} {
		if !hasMarkerLine(out, want) {
			t.Errorf("ACL should be repaired on redeploy of an existing key (missing %q)\noutput:\n%s", want, out)
		}
	}
}

func trimOutput(s string) string {
	// PowerShellの出力にはCRLFが含まれる
	b := []byte(s)
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return string(b)
}
