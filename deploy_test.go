package main

import (
	"strings"
	"testing"
)

func TestBuildDeployScript_Admin(t *testing.T) {
	pubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample user@host"
	script := buildDeployScript(pubKey, true)

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
		{"duplicate check", "Select-String"},
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
	script := buildDeployScript(pubKey, false)

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
		{"duplicate check", "Select-String"},
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
			script := buildDeployScript(pubKey, isAdmin)
			if !strings.Contains(script, "ACL_SET_FAILED_DIR") || !strings.Contains(script, "ACL_SET_FAILED_FILE") {
				t.Errorf("script should contain ACL_SET_FAILED_DIR and ACL_SET_FAILED_FILE markers\n\nscript:\n%s", script)
			}
		})
	}
}

func TestBuildDeployScript_EscapesSingleQuotes(t *testing.T) {
	pubKey := "ssh-ed25519 AAAAC3 it's a test"
	script := buildDeployScript(pubKey, false)

	if !strings.Contains(script, "it''s a test") {
		t.Errorf("single quotes should be escaped with double single quotes\n\nscript:\n%s", script)
	}
}

func TestBuildDeployScript_ErrorActionPreference(t *testing.T) {
	script := buildDeployScript("ssh-ed25519 AAAA test", false)

	if !strings.Contains(script, "$ErrorActionPreference = 'Stop'") {
		t.Error("script should set ErrorActionPreference to Stop")
	}
}

func TestBuildDeployScript_NoHardcodedPrincipalNames(t *testing.T) {
	script := buildDeployScript("ssh-ed25519 AAAA test", false)

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
