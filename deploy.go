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
	sb.WriteString("if ($LASTEXITCODE -ne 0) { Write-Output 'ACL_SET_FAILED'; exit 1 }\n")
	sb.WriteString("icacls $keyFile /inheritance:r /grant 'SYSTEM:(F)' /grant 'Administrators:(F)' /grant \"${env:USERNAME}:(F)\"\n")
	sb.WriteString("if ($LASTEXITCODE -ne 0) { Write-Output 'ACL_SET_FAILED'; exit 1 }\n")

	sb.WriteString("Write-Output 'KEY_DEPLOYED'\n")

	return sb.String()
}

// useAdminKeyFile はAdminユーザーかつsshd_configでadministrators_authorized_keysが有効かを判定する
func useAdminKeyFile(client *ssh.Client) bool {
	// Step 1: Administratorsグループ判定
	fmt.Println("=> Administratorsグループを確認中...")
	checkScript := "$id = [System.Security.Principal.WindowsIdentity]::GetCurrent(); $principal = New-Object System.Security.Principal.WindowsPrincipal($id); $principal.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)"
	output, err := runRemotePowerShell(client, checkScript)
	if err != nil {
		fmt.Printf("=> Admin判定をスキップ（エラー: %v）\n", err)
		return false
	}

	if !strings.Contains(output, "True") {
		fmt.Println("=> 一般ユーザーです")
		return false
	}
	fmt.Println("=> Administratorsグループのユーザーです")

	// Step 2: sshd_configでadministrators_authorized_keysが有効か確認
	fmt.Println("=> sshd_config を確認中...")
	// Match Group administrators の行がコメントアウトされていなければ有効
	sshdScript := `Select-String -Path C:\ProgramData\ssh\sshd_config -Pattern '^\s*Match\s+Group\s+administrators' -Quiet`
	output, err = runRemotePowerShell(client, sshdScript)
	if err != nil {
		fmt.Println("=> sshd_config の確認をスキップ（ユーザーディレクトリに配置します）")
		return false
	}

	if strings.Contains(output, "True") {
		fmt.Println("=> administrators_authorized_keys が有効です")
		return true
	}

	fmt.Println("=> [警告] Administratorsグループのユーザーですが、administrators_authorized_keys が無効です")
	fmt.Println("=>         sshd_config で Match Group administrators が設定されていない場合、")
	fmt.Println("=>         ユーザーディレクトリの鍵が認証に使用されない可能性があります")
	return false
}

func DeployKey(client *ssh.Client, pubKey string) error {
	isAdmin := useAdminKeyFile(client)

	fmt.Println("=> 公開鍵を配置中...")
	script := buildDeployScript(pubKey, isAdmin)
	output, err := runRemotePowerShell(client, script)
	if err != nil {
		return fmt.Errorf("鍵の配置に失敗: %w", err)
	}

	result := strings.TrimSpace(output)
	switch {
	case strings.Contains(result, "KEY_ALREADY_EXISTS"):
		fmt.Println("=> 鍵は既に登録されています（スキップ）")
	case strings.Contains(result, "ACL_SET_FAILED"):
		return fmt.Errorf("ACLの設定に失敗しました")
	case strings.Contains(result, "KEY_DEPLOYED"):
		fmt.Println("=> 鍵を正常に配置しました")
	default:
		fmt.Printf("=> 出力: %s\n", result)
	}

	return nil
}
