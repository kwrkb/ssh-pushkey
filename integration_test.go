//go:build integration

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// 実行方法: 正規の入口は make itest-admin / make itest-user（env ファイルを読み込み -count=1 を付ける）。
// 生コマンドで実行する場合（パスワード手打ち）:
//   read -rs SSH_TEST_PASSWORD && export SSH_TEST_PASSWORD
//   SSH_TEST_HOST=192.168.1.10 SSH_TEST_USER=pushuser go test -tags=integration -count=1 -v ./...
// -count=1 が無いと、同じ入力での再実行はキャッシュ済みの PASS を返し実機に接続しない。

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

// expectAdminKeyFile は SSH_TEST_EXPECT_KEYFILE（admin / user）から、このアカウントで期待する
// 配置先を返す。期待値はテスト対象（resolveKeyFileTarget）の外から与える必要がある。
// 同じ判定結果からファイルを選んで検証すると、判定が誤っていても検証が成功してしまうため（Issue #33 AUD-005）。
// 未設定は Skip ではなく Fatal にする（未設定のまま緑になると、どちらの判定でも通る元の状態に戻る）。
func expectAdminKeyFile(t *testing.T) bool {
	t.Helper()
	switch v := os.Getenv("SSH_TEST_EXPECT_KEYFILE"); v {
	case "admin":
		return true
	case "user":
		return false
	case "":
		t.Fatalf("SSH_TEST_EXPECT_KEYFILE is required: set it to admin (administrators_authorized_keys) or " +
			"user (%%USERPROFILE%%\\.ssh\\authorized_keys) in the env file, according to how sshd on the test host is configured for this account")
	default:
		t.Fatalf("SSH_TEST_EXPECT_KEYFILE must be admin or user, got %q", v)
	}
	return false
}

// assertExpectedKeyFile は配置先の判定結果が SSH_TEST_EXPECT_KEYFILE と一致することを確かめる。
func assertExpectedKeyFile(t *testing.T, target keyFileTarget) {
	t.Helper()
	want := expectAdminKeyFile(t)
	t.Logf("SSH_TEST_EXPECT_KEYFILE=%s, resolved target = %+v", os.Getenv("SSH_TEST_EXPECT_KEYFILE"), target)
	if target.isAdmin != want {
		t.Fatalf("resolveKeyFileTarget chose isAdmin=%v (%s), but SSH_TEST_EXPECT_KEYFILE expects isAdmin=%v",
			target.isAdmin, target.reason, want)
	}
}

// dialKeyOnly は公開鍵認証だけで接続する（パスワードには戻らない）。配置した鍵が sshd に
// 実際に受け入れられるかを確かめるため。
func dialKeyOnly(env testEnv, signer ssh.Signer) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            env.user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	return ssh.Dial("tcp", net.JoinHostPort(env.host, strconv.Itoa(env.port)), cfg)
}

// backupRemoteKeyFile はリモートの authorized_keys を排他付きで退避し、復元関数を返す。
// 呼び出し側は defer client.Close() より後で defer restore() すること（LIFO で接続が先に閉じないように）。
// 別の実行がロック中、または中断の跡（退避ファイルやロック）が残っている場合は何も変えずに t.Fatalf する。
// 退避・復元は remote PowerShell 内で完結させ、ファイル内容を Go 側へ吸い上げない
// （runRemotePowerShell の出力には CLIXML 初期化ノイズが混ざるため）。詳細は integration_fixture_test.go。
func backupRemoteKeyFile(t *testing.T, client *ssh.Client, keyFileExpr string) (restore func()) {
	t.Helper()
	id := newFixtureID(t)

	out, err := runRemotePowerShell(client, buildFixtureBackupScript(keyFileExpr, id))
	if err != nil {
		t.Fatalf("failed to back up authorized_keys: %v", err)
	}
	switch {
	case hasMarkerLine(out, fixtureMarkerLocked):
		t.Fatalf("another integration run holds %s, or a previous run was interrupted. "+
			"Make sure no other run is active, check that authorized_keys is intact, then delete the lock file.\noutput:\n%s",
			markerValue(out, fixtureMarkerLocked), out)
	case hasMarkerLine(out, fixtureMarkerLeftover):
		t.Fatalf("leftover files from an interrupted run were found next to authorized_keys: %s. "+
			"They may hold the original keys. Restore or delete them manually before re-running; nothing was changed.\noutput:\n%s",
			markerValue(out, fixtureMarkerLeftover), out)
	case !hasMarkerLine(out, fixtureMarkerReady):
		t.Fatalf("unexpected output while backing up authorized_keys\noutput:\n%s", out)
	}

	return func() {
		out, err := runRemotePowerShell(client, buildFixtureRestoreScript(keyFileExpr, id))
		if err != nil {
			t.Errorf("failed to restore authorized_keys (manual cleanup may be required, backup id %s): %v", id, err)
			return
		}
		switch {
		case hasMarkerLine(out, fixtureMarkerRestored):
		case hasMarkerLine(out, fixtureMarkerMissing):
			t.Errorf("backup for id %s was not found, so authorized_keys was left as is and the lock was kept. "+
				"Check authorized_keys manually.\noutput:\n%s", id, out)
		default:
			t.Errorf("unexpected output while restoring authorized_keys (backup id %s); the restore result is unknown. "+
				"Check authorized_keys and the .pushkey-test-* files manually.\noutput:\n%s", id, out)
		}
	}
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

	// 判定結果は、テスト対象の外から与えた期待値（SSH_TEST_EXPECT_KEYFILE）と照合する。
	target, err := resolveKeyFileTarget(client)
	if err != nil {
		t.Fatalf("resolveKeyFileTarget failed: %v", err)
	}
	assertExpectedKeyFile(t, target)
}

// TestIntegration_KeyAuthAfterDeploy は、配置した鍵で実際にログインできることを確かめる。
// ファイルの中身を見る検証は resolveKeyFileTarget と同じ判定から対象を選ぶため、判定が誤って
// sshd が読まないファイルへ書いても成功してしまう。sshd による公開鍵認証の成否は判定から独立した
// 検証になる（Issue #33 AUD-005）。
//
// 鍵はテストごとにメモリ上で生成し、秘密鍵はディスクに書かない。復元に失敗して公開鍵の行が
// 残っても、対応する秘密鍵はどこにも無いので何も認可しない。
func TestIntegration_KeyAuthAfterDeploy(t *testing.T) {
	env := loadTestEnv(t)

	client, err := dialSSH(env.user, env.host, env.port, env.password, true)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()

	// 期待する配置先と一致しなければ、鍵ファイルに触る前に止める。
	target, err := resolveKeyFileTarget(client)
	if err != nil {
		t.Fatalf("resolveKeyFileTarget failed: %v", err)
	}
	assertExpectedKeyFile(t, target)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ephemeral key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	pubLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " ssh-pushkey-itest-ephemeral"

	restore := backupRemoteKeyFile(t, client, remoteKeyFileExpr(target.isAdmin))
	defer restore()

	// 対照: 配置前はこの鍵で認証できないこと。接続断などを拒否と取り違えないよう、
	// 認証の拒否であることまで確かめる。
	if c, err := dialKeyOnly(env, signer); err == nil {
		c.Close()
		t.Fatalf("precondition: the ephemeral key must not be accepted before deployment")
	} else if !strings.Contains(err.Error(), "unable to authenticate") {
		t.Fatalf("precondition: expected an authentication rejection before deployment, got: %v", err)
	}

	if err := DeployKey(client, pubLine, false); err != nil {
		t.Fatalf("key deployment failed: %v", err)
	}

	keyClient, err := dialKeyOnly(env, signer)
	if err != nil {
		t.Fatalf("login with the deployed key failed (the key was likely written to a file sshd does not read): %v", err)
	}
	defer keyClient.Close()

	// 鍵認証のセッションでコマンドが動き、同じアカウントとしてログインしていること。
	out, err := runRemotePowerShell(keyClient, "Write-Output \"KEYAUTH_USER:$env:USERNAME\"")
	if err != nil {
		t.Fatalf("remote command over the key-authenticated session failed: %v", err)
	}
	if got := markerValue(out, "KEYAUTH_USER:"); !strings.EqualFold(got, env.user) {
		t.Errorf("key-authenticated session runs as %q, want %q\noutput:\n%s", got, env.user, out)
	}
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

	// 鍵ファイルを書き換えるテストは、すべて排他付きで退避してから始め、終了時に元へ戻す。
	target, err := resolveKeyFileTarget(client)
	if err != nil {
		t.Fatalf("resolveKeyFileTarget failed: %v", err)
	}
	restore := backupRemoteKeyFile(t, client, remoteKeyFileExpr(target.isAdmin))
	defer restore()

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

	target, err := resolveKeyFileTarget(client)
	if err != nil {
		t.Fatalf("resolveKeyFileTarget failed: %v", err)
	}
	// 鍵の配置から option 行での上書きまで、すべて排他付きの退避の内側で行う。
	keyFileExpr := remoteKeyFileExpr(target.isAdmin)
	restore := backupRemoteKeyFile(t, client, keyFileExpr)
	defer restore()

	// 鍵を配置しておく（既にあれば idempotent にスキップされる）
	if err := DeployKey(client, pubKey, false); err != nil {
		t.Fatalf("key deployment failed: %v", err)
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
	// 冒頭で退避済みなので、option 行のみで上書き → dry-run スキャン → defer で必ず元へ復元する
	// （残置すると実ホスト上の有効な authorization になるため）。
	// 平文の鍵行も残すと壊れたスキャナでも True になり判別力が失われるので、敢えて option 行だけにする。

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

	// $keyFile の決定は buildDeployScript と同じ式に合わせる。退避は排他付き
	// （.ssh ディレクトリが無ければ退避スクリプトが作る。WriteAllText はディレクトリが無いと失敗する）。
	keyFileExpr := remoteKeyFileExpr(target.isAdmin)
	restore := backupRemoteKeyFile(t, client, keyFileExpr)
	defer restore()

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

	target, err := resolveKeyFileTarget(client)
	if err != nil {
		t.Fatalf("resolveKeyFileTarget failed: %v", err)
	}

	// $keyFile / $sshDir の決定は buildDeployScript と同じ式に合わせる（$sshDir は $keyFile の親）。
	keyFileExpr := remoteKeyFileExpr(target.isAdmin)
	pathExpr := keyFileExpr + "; $sshDir = Split-Path -Parent $keyFile"

	// 鍵の配置から ACL の破壊・修復まで、すべて排他付きの退避の内側で行う。
	restore := backupRemoteKeyFile(t, client, keyFileExpr)
	defer restore()

	// 後始末: DeployKey が修復できなかった場合でも、ディレクトリと鍵ファイルの継承を外して
	// 3 SID に戻す（崩れたまま残すと sshd が鍵ファイルを拒否し得る）。
	// 退避（ロック取得）より後に登録する。defer は LIFO なので、この ACL 修復はロックを持ったまま
	// 鍵ファイルの内容の復元より先に走る。退避がロック中や中断の跡で t.Fatalf した場合は登録されず、
	// 他の実行のファイルに触らない。
	defer func() {
		restore := pathExpr + "; $sid = ([System.Security.Principal.WindowsIdentity]::GetCurrent()).User.Value" +
			"; foreach ($p in @($sshDir, $keyFile)) { icacls $p /inheritance:r /grant '*S-1-5-18:(F)' /grant '*S-1-5-32-544:(F)' /grant \"*${sid}:(F)\" | Out-Null }"
		if _, rerr := runRemotePowerShell(client, restore); rerr != nil {
			t.Errorf("failed to restore ACL (manual cleanup may be required): %v", rerr)
		}
	}()

	// 鍵を配置済みの状態にする（既にあれば重複扱い）
	if err := DeployKey(client, pubKey, false); err != nil {
		t.Fatalf("key deployment failed: %v", err)
	}

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
	// 以前の退避・復元方式（Move-Item で退避ファイルに置き換えていた）で作り直したファイルには、
	// 作成者トークン既定の LogonSessionId（S-1-5-5-0-*）の読み取り ACE が残っていることがある。
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
