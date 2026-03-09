package main

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

func buildDeployScript(pubKey string, isAdmin bool) string {
	escapedKey := strings.ReplaceAll(pubKey, "'", "''")

	var sb strings.Builder

	sb.WriteString("$ErrorActionPreference = 'Stop'\n")
	sb.WriteString(fmt.Sprintf("$pubKey = '%s'\n", escapedKey))

	if isAdmin {
		sb.WriteString("$keyFile = 'C:\\ProgramData\\ssh\\administrators_authorized_keys'\n")
		sb.WriteString("$sshDir = 'C:\\ProgramData\\ssh'\n")
	} else {
		sb.WriteString("$keyFile = Join-Path $env:USERPROFILE '.ssh\\authorized_keys'\n")
		sb.WriteString("$sshDir = Join-Path $env:USERPROFILE '.ssh'\n")
	}

	// ディレクトリ作成
	sb.WriteString("if (-not (Test-Path $sshDir)) { New-Item -ItemType Directory -Path $sshDir -Force | Out-Null }\n")

	// 重複チェック
	sb.WriteString("if (Test-Path $keyFile) {\n")
	sb.WriteString("  $existing = Select-String -Path $keyFile -Pattern $pubKey -SimpleMatch -Quiet\n")
	sb.WriteString("  if ($existing) { Write-Output 'KEY_ALREADY_EXISTS'; exit 0 }\n")
	sb.WriteString("}\n")

	// BOMなしUTF-8で追記
	sb.WriteString("[System.IO.File]::AppendAllText($keyFile, $pubKey + \"`n\", (New-Object System.Text.UTF8Encoding $false))\n")

	// ACL設定（ディレクトリ + ファイル）
	sb.WriteString("icacls $sshDir /inheritance:r /grant 'SYSTEM:(F)' /grant 'Administrators:(F)' /grant \"${env:USERNAME}:(F)\"\n")
	sb.WriteString("if ($LASTEXITCODE -ne 0) { Write-Output 'ACL_SET_FAILED_DIR'; exit 1 }\n")
	sb.WriteString("icacls $keyFile /inheritance:r /grant 'SYSTEM:(F)' /grant 'Administrators:(F)' /grant \"${env:USERNAME}:(F)\"\n")
	sb.WriteString("if ($LASTEXITCODE -ne 0) { Write-Output 'ACL_SET_FAILED_FILE'; exit 1 }\n")

	sb.WriteString("Write-Output 'KEY_DEPLOYED'\n")

	return sb.String()
}

// useAdminKeyFile はAdminユーザーかつsshd_configでadministrators_authorized_keysが有効かを判定する
func useAdminKeyFile(client *ssh.Client) bool {
	// Step 1: Administratorsグループ判定
	fmt.Println("=> Checking Administrators group...")
	checkScript := "$id = [System.Security.Principal.WindowsIdentity]::GetCurrent(); $principal = New-Object System.Security.Principal.WindowsPrincipal($id); $principal.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)"
	output, err := runRemotePowerShell(client, checkScript)
	if err != nil {
		fmt.Printf("=> Skipping admin check (error: %v)\n", err)
		return false
	}

	if !strings.Contains(output, "True") {
		fmt.Println("=> Standard user detected")
		return false
	}
	fmt.Println("=> User is in Administrators group")

	// Step 2: sshd_configでadministrators_authorized_keysが有効か確認
	fmt.Println("=> Checking sshd_config...")
	// Match Group administrators の行がコメントアウトされていなければ有効
	sshdScript := `Select-String -Path C:\ProgramData\ssh\sshd_config -Pattern '^\s*Match\s+Group\s+administrators' -Quiet`
	output, err = runRemotePowerShell(client, sshdScript)
	if err != nil {
		fmt.Println("=> Skipping sshd_config check, deploying to user directory")
		return false
	}

	if strings.Contains(output, "True") {
		fmt.Println("=> administrators_authorized_keys is enabled")
		return true
	}

	fmt.Println("=> [WARNING] User is in Administrators group but administrators_authorized_keys is disabled")
	fmt.Println("=>           If 'Match Group administrators' is not set in sshd_config,")
	fmt.Println("=>           user directory keys may not be used for authentication")
	return false
}

func DeployKey(client *ssh.Client, pubKey string) error {
	isAdmin := useAdminKeyFile(client)

	fmt.Println("=> Deploying public key...")
	script := buildDeployScript(pubKey, isAdmin)
	output, err := runRemotePowerShell(client, script)
	result := strings.TrimSpace(output)

	// exit 1 でエラーが返る場合もマーカーを優先チェック
	switch {
	case strings.Contains(result, "ACL_SET_FAILED_DIR"):
		return fmt.Errorf("failed to set directory ACL")
	case strings.Contains(result, "ACL_SET_FAILED_FILE"):
		return fmt.Errorf("failed to set key file ACL")
	case err != nil:
		return fmt.Errorf("key deployment failed: %w", err)
	case strings.Contains(result, "KEY_ALREADY_EXISTS"):
		fmt.Println("=> Key already exists, skipping")
	case strings.Contains(result, "KEY_DEPLOYED"):
		fmt.Println("=> Key deployed successfully")
	default:
		fmt.Printf("=> Output: %s\n", result)
	}

	return nil
}
