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
		{"icacls SYSTEM", "SYSTEM:(F)"},
		{"icacls Administrators", "Administrators:(F)"},
		{"icacls user", "${env:USERNAME}:(F)"},
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
		{"icacls user", "${env:USERNAME}:(F)"},
		{"icacls Administrators", "Administrators:(F)"},
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
