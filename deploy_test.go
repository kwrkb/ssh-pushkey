package main

import (
	"strings"
	"testing"
)

func TestBuildDeployScript_Admin(t *testing.T) {
	pubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample user@host"
	script := buildDeployScript(pubKey, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample", true, false)

	checks := []struct {
		name    string
		contain string
	}{
		{"admin key path", `C:\ProgramData\ssh\administrators_authorized_keys`},
		{"admin ssh dir", `C:\ProgramData\ssh`},
		{"public key present", pubKey},
		{"BOM-less UTF8", "New-Object System.Text.UTF8Encoding $false"},
		{"AppendAllText", "[System.IO.File]::AppendAllText"},
		{"icacls SYSTEM SID", "*S-1-5-18:(F)"},
		{"icacls Administrators SID", "*S-1-5-32-544:(F)"},
		{"icacls user SID", "*${userSid}:(F)"},
		{"user SID lookup", "WindowsIdentity]::GetCurrent()).User.Value"},
		{"duplicate check by blob", "-ceq $keyBlob"},
		{"key blob assigned", "$keyBlob = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample'"},
		{"inheritance remove keyFile", "icacls $keyFile /inheritance:r"},
		{"inheritance remove sshDir", "icacls $sshDir /inheritance:r"},
		{"LASTEXITCODE check", "LASTEXITCODE"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(script, c.contain) {
				t.Errorf("script should contain %q\n\nscript:\n%s", c.contain, script)
			}
		})
	}
}

func TestBuildDeployScript_NormalUser(t *testing.T) {
	pubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample user@host"
	script := buildDeployScript(pubKey, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample", false, false)

	checks := []struct {
		name    string
		contain string
	}{
		{"user key path", `.ssh\authorized_keys`},
		{"user ssh dir", `.ssh`},
		{"public key present", pubKey},
		{"BOM-less UTF8", "New-Object System.Text.UTF8Encoding $false"},
		{"icacls SYSTEM SID", "*S-1-5-18:(F)"},
		{"icacls Administrators SID", "*S-1-5-32-544:(F)"},
		{"icacls user SID", "*${userSid}:(F)"},
		{"user SID lookup", "WindowsIdentity]::GetCurrent()).User.Value"},
		{"duplicate check by blob", "-ceq $keyBlob"},
		{"inheritance remove sshDir", "icacls $sshDir /inheritance:r"},
		{"LASTEXITCODE check", "LASTEXITCODE"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(script, c.contain) {
				t.Errorf("script should contain %q\n\nscript:\n%s", c.contain, script)
			}
		})
	}

	// Admin専用のパスが含まれないことを確認
	if strings.Contains(script, `C:\ProgramData\ssh\administrators_authorized_keys`) {
		t.Error("normal user script should not contain admin key path")
	}
}

func TestBuildDeployScript_AclValidation(t *testing.T) {
	pubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample user@host"

	for _, isAdmin := range []bool{true, false} {
		name := "normal"
		if isAdmin {
			name = "admin"
		}
		t.Run(name, func(t *testing.T) {
			script := buildDeployScript(pubKey, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample", isAdmin, false)
			if !strings.Contains(script, "ACL_SET_FAILED_DIR") || !strings.Contains(script, "ACL_SET_FAILED_FILE") {
				t.Errorf("script should contain ACL_SET_FAILED_DIR and ACL_SET_FAILED_FILE markers\n\nscript:\n%s", script)
			}
		})
	}
}

func TestBuildDeployScript_EscapesSingleQuotes(t *testing.T) {
	pubKey := "ssh-ed25519 AAAAC3 it's a test"
	script := buildDeployScript(pubKey, "ssh-ed25519 AAAAC3", false, false)

	if !strings.Contains(script, "it''s a test") {
		t.Errorf("single quotes should be escaped with double single quotes\n\nscript:\n%s", script)
	}
}

func TestBuildDeployScript_ErrorActionPreference(t *testing.T) {
	script := buildDeployScript("ssh-ed25519 AAAA test", "ssh-ed25519 AAAA", false, false)

	if !strings.Contains(script, "$ErrorActionPreference = 'Stop'") {
		t.Error("script should set ErrorActionPreference to Stop")
	}
}

func TestBuildDeployScript_NoHardcodedPrincipalNames(t *testing.T) {
	script := buildDeployScript("ssh-ed25519 AAAA test", "ssh-ed25519 AAAA", false, false)

	// 名前ベースのプリンシパルがicaclsコマンドに含まれないことを確認
	// （SIDベースの *S-1-5-18 等を使用すべき）
	lines := strings.Split(script, "\n")
	for _, line := range lines {
		if !strings.Contains(line, "icacls") {
			continue
		}
		if strings.Contains(line, "'SYSTEM:(F)'") {
			t.Errorf("icacls should use SID (*S-1-5-18) instead of name 'SYSTEM'\nline: %s", line)
		}
		if strings.Contains(line, "'Administrators:(F)'") {
			t.Errorf("icacls should use SID (*S-1-5-32-544) instead of name 'Administrators'\nline: %s", line)
		}
	}
}

func TestBuildDeployScript_DryRun(t *testing.T) {
	pubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample user@host"
	script := buildDeployScript(pubKey, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample", true, true)

	// dry-run で出力されるべきもの
	for _, want := range []string{
		"$dryRun = $true",
		"DRY_RUN_TARGET:",
		"DRY_RUN_DUP:",
		"-ceq $keyBlob", // 重複チェックは実行する
	} {
		if !strings.Contains(script, want) {
			t.Errorf("dry-run script should contain %q\n\nscript:\n%s", want, script)
		}
	}

	// 単一スクリプト方式のため書き込み文はテキストとしては存在するが、
	// dry-run の `exit 0` ガードがそれらより前にあり実行時に到達不能であることを保証する。
	dryRunExit := strings.Index(script, "exit 0\n}")
	if dryRunExit < 0 {
		t.Fatalf("dry-run guard with exit 0 not found\n\nscript:\n%s", script)
	}
	for _, sideEffect := range []string{
		"[System.IO.File]::AppendAllText",
		"[System.IO.File]::ReadAllBytes",
		"icacls",
		"New-Item",
	} {
		if idx := strings.Index(script, sideEffect); idx >= 0 && idx < dryRunExit {
			t.Errorf("side effect %q appears before dry-run exit guard (would run in dry-run)\n\nscript:\n%s", sideEffect, script)
		}
	}
}

func TestBuildDeployScript_NewlineGuardBeforeAppend(t *testing.T) {
	for _, isAdmin := range []bool{true, false} {
		name := "normal"
		if isAdmin {
			name = "admin"
		}
		t.Run(name, func(t *testing.T) {
			script := buildDeployScript("ssh-ed25519 AAAA test", "ssh-ed25519 AAAA", isAdmin, false)

			// 末尾改行ガード（ReadAllBytes で最終バイトを検査）が追記より前にあること
			guardIdx := strings.Index(script, "$existing = [System.IO.File]::ReadAllBytes($keyFile)")
			appendIdx := strings.Index(script, "[System.IO.File]::AppendAllText")
			if guardIdx < 0 {
				t.Fatalf("script should contain trailing-newline guard\n\nscript:\n%s", script)
			}
			if appendIdx < 0 {
				t.Fatalf("script should contain AppendAllText\n\nscript:\n%s", script)
			}
			if guardIdx > appendIdx {
				t.Errorf("newline guard must precede AppendAllText\n\nscript:\n%s", script)
			}

			// 補った改行 $nl を鍵本体の前に連結して追記すること
			if !strings.Contains(script, "AppendAllText($keyFile, $nl + $pubKey + \"`n\"") {
				t.Errorf("AppendAllText should prepend $nl to $pubKey\n\nscript:\n%s", script)
			}
			// 末尾バイトが LF(10) かを判定していること
			if !strings.Contains(script, "$existing[$existing.Length - 1] -ne 10") {
				t.Errorf("guard should compare the last byte against LF (10)\n\nscript:\n%s", script)
			}
		})
	}
}

func TestBuildDeployScript_NonDryRunWrites(t *testing.T) {
	script := buildDeployScript("ssh-ed25519 AAAA test", "ssh-ed25519 AAAA", false, false)
	if !strings.Contains(script, "$dryRun = $false") {
		t.Errorf("non-dry-run script should set $dryRun = $false\n\nscript:\n%s", script)
	}
	if !strings.Contains(script, "[System.IO.File]::AppendAllText") {
		t.Errorf("non-dry-run script should write the key\n\nscript:\n%s", script)
	}
}

func TestBuildDeployScript_AclErrorCapture(t *testing.T) {
	script := buildDeployScript("ssh-ed25519 AAAA test", "ssh-ed25519 AAAA", false, false)
	// icacls の出力を 2>&1 で捕捉し、失敗マーカーへ実エラーを付加すること
	for _, want := range []string{
		"2>&1",
		`Write-Output "ACL_SET_FAILED_DIR|$aclOut"`,
		`Write-Output "ACL_SET_FAILED_FILE|$fileAclOut"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script should contain %q\n\nscript:\n%s", want, script)
		}
	}

	// PS 5.1 の NativeCommandError 回避: icacls の 2>&1 リダイレクトより前に
	// ErrorActionPreference を Continue に落としていること（失敗時もマーカー出力を保証）
	continueIdx := strings.Index(script, "$ErrorActionPreference = 'Continue'")
	icaclsIdx := strings.Index(script, "& icacls")
	if continueIdx < 0 {
		t.Errorf("script should switch ErrorActionPreference to Continue before icacls\n\nscript:\n%s", script)
	} else if icaclsIdx >= 0 && continueIdx > icaclsIdx {
		t.Errorf("ErrorActionPreference=Continue must come before the first icacls call\n\nscript:\n%s", script)
	}
}

// 通常実行でも実際の配置先パスを報告できるよう、結果マーカーに $keyFile を付けていること。
// 非 Admin 時の配置先は $env:USERPROFILE 展開後にしか確定しないため、Go 側では組み立てられない。
func TestBuildDeployScript_ResultMarkersCarryKeyFile(t *testing.T) {
	for _, isAdmin := range []bool{true, false} {
		name := "normal"
		if isAdmin {
			name = "admin"
		}
		t.Run(name, func(t *testing.T) {
			script := buildDeployScript("ssh-ed25519 AAAA test", "ssh-ed25519 AAAA", isAdmin, false)

			for _, want := range []string{
				`Write-Output "KEY_ALREADY_EXISTS:$keyFile"`,
				`Write-Output "KEY_DEPLOYED:$keyFile"`,
			} {
				if !strings.Contains(script, want) {
					t.Errorf("script should contain %q\n\nscript:\n%s", want, script)
				}
			}

			// $keyFile を展開させるには二重引用符が必要。単一引用符のままだとリテラル出力になる。
			for _, notWant := range []string{
				"Write-Output 'KEY_ALREADY_EXISTS'",
				"Write-Output 'KEY_DEPLOYED'",
			} {
				if strings.Contains(script, notWant) {
					t.Errorf("script should not contain bare marker %q (path would be lost)\n\nscript:\n%s", notWant, script)
				}
			}
		})
	}
}

func TestMarkerValue(t *testing.T) {
	cases := []struct {
		name   string
		output string
		marker string
		want   string
	}{
		{
			name:   "admin path",
			output: "KEY_DEPLOYED:C:\\ProgramData\\ssh\\administrators_authorized_keys",
			marker: "KEY_DEPLOYED:",
			want:   "C:\\ProgramData\\ssh\\administrators_authorized_keys",
		},
		{
			name:   "path with spaces after CLIXML noise",
			output: "#< CLIXML\r\n<Objs Version=\"1.1.0.1\"></Objs>\r\nKEY_DEPLOYED:C:\\Users\\John Doe\\.ssh\\authorized_keys\r\n",
			marker: "KEY_DEPLOYED:",
			want:   "C:\\Users\\John Doe\\.ssh\\authorized_keys",
		},
		{
			name:   "already exists marker",
			output: "noise\nKEY_ALREADY_EXISTS:C:\\Users\\test\\.ssh\\authorized_keys",
			marker: "KEY_ALREADY_EXISTS:",
			want:   "C:\\Users\\test\\.ssh\\authorized_keys",
		},
		{
			name:   "marker without value",
			output: "KEY_DEPLOYED:",
			marker: "KEY_DEPLOYED:",
			want:   "(unknown)",
		},
		{
			name:   "marker absent",
			output: "KEY_ALREADY_EXISTS:C:\\path",
			marker: "KEY_DEPLOYED:",
			want:   "(unknown)",
		},
		{
			// 配置先パスが他マーカーの文字列を含んでも拾わない（行頭一致を要求する）
			name:   "other marker appears inside the path",
			output: "KEY_DEPLOYED:C:\\Users\\KEY_ALREADY_EXISTS\\.ssh\\authorized_keys",
			marker: "KEY_ALREADY_EXISTS:",
			want:   "(unknown)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := markerValue(c.output, c.marker); got != c.want {
				t.Errorf("markerValue() = %q, want %q", got, c.want)
			}
		})
	}
}

// マーカーが配置先パスを載せるようになったため、出力全体への部分一致で分岐すると
// パスに含まれる別マーカーの文字列を拾って結果を取り違える。行頭一致を要求すること。
func TestHasMarkerLine(t *testing.T) {
	deployedIntoTrapPath := "KEY_DEPLOYED:C:\\Users\\KEY_ALREADY_EXISTS\\.ssh\\authorized_keys"

	cases := []struct {
		name   string
		output string
		marker string
		want   bool
	}{
		{
			name:   "path containing another marker is not matched",
			output: deployedIntoTrapPath,
			marker: "KEY_ALREADY_EXISTS:",
			want:   false,
		},
		{
			name:   "own marker still matched on the same line",
			output: deployedIntoTrapPath,
			marker: "KEY_DEPLOYED:",
			want:   true,
		},
		{
			name:   "marker after CLIXML noise",
			output: "#< CLIXML\r\n<Objs Version=\"1.1.0.1\"></Objs>\r\nKEY_DEPLOYED:C:\\path\r\n",
			marker: "KEY_DEPLOYED:",
			want:   true,
		},
		{
			name:   "acl failure marker with detail after pipe",
			output: "noise\nACL_SET_FAILED_DIR|Access is denied.",
			marker: "ACL_SET_FAILED_DIR",
			want:   true,
		},
		{
			name:   "acl marker mentioned mid-line is not matched",
			output: "KEY_DEPLOYED:C:\\Users\\ACL_SET_FAILED_DIR\\.ssh\\authorized_keys",
			marker: "ACL_SET_FAILED_DIR",
			want:   false,
		},
		{
			name:   "absent",
			output: "KEY_DEPLOYED:C:\\path",
			marker: "DRY_RUN_TARGET:",
			want:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasMarkerLine(c.output, c.marker); got != c.want {
				t.Errorf("hasMarkerLine(%q, %q) = %v, want %v", c.output, c.marker, got, c.want)
			}
		})
	}
}

func TestExtractAclErrorDetail(t *testing.T) {
	cases := []struct {
		name   string
		output string
		marker string
		want   string
	}{
		{
			name:   "detail after pipe",
			output: "some line\nACL_SET_FAILED_DIR|Access is denied.\nKEY_DEPLOYED",
			marker: "ACL_SET_FAILED_DIR",
			want:   "Access is denied.",
		},
		{
			name:   "file marker",
			output: "ACL_SET_FAILED_FILE|The system cannot find the path specified.",
			marker: "ACL_SET_FAILED_FILE",
			want:   "The system cannot find the path specified.",
		},
		{
			name:   "marker without detail",
			output: "ACL_SET_FAILED_DIR",
			marker: "ACL_SET_FAILED_DIR",
			want:   "(unknown error)",
		},
		{
			name:   "marker absent",
			output: "KEY_DEPLOYED",
			marker: "ACL_SET_FAILED_DIR",
			want:   "(unknown error)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractAclErrorDetail(c.output, c.marker); got != c.want {
				t.Errorf("extractAclErrorDetail() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestEffectiveAdminKeysFromSshdT(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		isAdmin bool
		ok      bool
	}{
		{
			name:    "admin keys enabled — multiple paths",
			input:   "SSHD_T_OK\nport 22\nauthorizedkeysfile __PROGRAMDATA__/ssh/administrators_authorized_keys .ssh/authorized_keys\n",
			isAdmin: true,
			ok:      true,
		},
		{
			name:    "user keys only",
			input:   "SSHD_T_OK\nauthorizedkeysfile .ssh/authorized_keys\n",
			isAdmin: false,
			ok:      true,
		},
		{
			name:    "no SSHD_T_OK marker",
			input:   "SSHD_T_FAILED\n",
			isAdmin: false,
			ok:      false,
		},
		{
			name:    "no authorizedkeysfile line",
			input:   "SSHD_T_OK\nport 22\nprotocol 2\n",
			isAdmin: false,
			ok:      false,
		},
		{
			name:    "case-insensitive key",
			input:   "SSHD_T_OK\nAuthorizedKeysFile administrators_authorized_keys\n",
			isAdmin: true,
			ok:      true,
		},
		{
			name:    "CRLF line endings",
			input:   "SSHD_T_OK\r\nauthorizedkeysfile administrators_authorized_keys\r\n",
			isAdmin: true,
			ok:      true,
		},
		{
			name:    "empty output",
			input:   "",
			isAdmin: false,
			ok:      false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotAdmin, gotOk := effectiveAdminKeysFromSshdT(c.input)
			if gotOk != c.ok {
				t.Errorf("ok: got %v, want %v", gotOk, c.ok)
			}
			if gotAdmin != c.isAdmin {
				t.Errorf("isAdmin: got %v, want %v", gotAdmin, c.isAdmin)
			}
		})
	}
}

func TestLooksLikeNonWindows(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect bool
	}{
		{"command not found", "bash: powershell: command not found", true},
		{"not recognized as", "'powershell' is not recognized as an internal or external command", true},
		{"powershell not found", "powershell: not found", true},
		{"bash prefix", "bash: line 1: powershell: command not found", true},
		{"sh prefix", "sh: powershell: not found", true},
		{"fish shell", "fish: Unknown command: powershell", true},
		{"env error", "env: 'powershell': No such file or directory", true},
		{"platform not supported", "PlatformNotSupportedException: The Windows Identity and Principal types are not supported on this platform.", true},
		{"normal windows output — True", "True", false},
		{"empty output", "", false},
		{"windows error message", "The system cannot find the file specified.", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := looksLikeNonWindows(c.input)
			if got != c.expect {
				t.Errorf("looksLikeNonWindows(%q) = %v, want %v", c.input, got, c.expect)
			}
		})
	}
}

func TestSshdMatchSpecSuffixFromOutput(t *testing.T) {
	const fallback = "host=localhost,addr=127.0.0.1"
	// 実機 Windows OpenSSH の runRemotePowerShell が返す CLIXML 混入出力（先頭の "#< CLIXML"
	// ヘッダ＋末尾の <Objs> プログレス XML に SSH_CONNECTION 行が挟まれる）。
	const clixmlContaminated = "#< CLIXML\r\n127.0.0.1 45576 127.0.0.1 22\r\n" +
		`<Objs Version="1.1.0.1" xmlns="http://schemas.microsoft.com/powershell/2004/04">` +
		`<Obj S="progress" RefId="0"><MS><PR N="Record"><AV>Preparing modules for first use.</AV>` +
		`<T>Completed</T></PR></MS></Obj></Objs>`

	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "IPv4 normal",
			input:  "203.0.113.5 51234 198.51.100.2 22",
			expect: "host=203.0.113.5,addr=203.0.113.5,laddr=198.51.100.2,lport=22",
		},
		{
			name:   "IPv6",
			input:  "::1 51234 ::1 22",
			expect: "host=::1,addr=::1,laddr=::1,lport=22",
		},
		{
			name:   "loopback IPv4",
			input:  "127.0.0.1 5000 127.0.0.1 22",
			expect: "host=127.0.0.1,addr=127.0.0.1,laddr=127.0.0.1,lport=22",
		},
		{
			name:   "extra whitespace tolerated",
			input:  "  203.0.113.5   51234   198.51.100.2   22  ",
			expect: "host=203.0.113.5,addr=203.0.113.5,laddr=198.51.100.2,lport=22",
		},
		{
			name:   "CLIXML-contaminated output → extracts the connection line",
			input:  clixmlContaminated,
			expect: "host=127.0.0.1,addr=127.0.0.1,laddr=127.0.0.1,lport=22",
		},
		{name: "empty → fallback", input: "", expect: fallback},
		{name: "only CLIXML noise → fallback", input: "#< CLIXML\r\n<Objs Version=\"1.1.0.1\"></Objs>", expect: fallback},
		{name: "too few fields → fallback", input: "203.0.113.5 51234 198.51.100.2", expect: fallback},
		{name: "too many fields → fallback", input: "203.0.113.5 51234 198.51.100.2 22 extra", expect: fallback},
		{name: "invalid client IP → fallback", input: "not-an-ip 51234 198.51.100.2 22", expect: fallback},
		{name: "invalid server IP → fallback", input: "203.0.113.5 51234 nope 22", expect: fallback},
		{name: "non-numeric port → fallback", input: "203.0.113.5 51234 198.51.100.2 ssh", expect: fallback},
		{name: "port out of range → fallback", input: "203.0.113.5 51234 198.51.100.2 70000", expect: fallback},
		{name: "port zero → fallback", input: "203.0.113.5 51234 198.51.100.2 0", expect: fallback},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sshdMatchSpecSuffixFromOutput(c.input)
			if got != c.expect {
				t.Errorf("sshdMatchSpecSuffixFromOutput(%q) = %q, want %q", c.input, got, c.expect)
			}
		})
	}
}
