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

	// ACL設定（SIDベース：言語非依存）
	// S-1-5-18 = NT AUTHORITY\SYSTEM
	// S-1-5-32-544 = BUILTIN\Administrators
	// ユーザーSIDは動的に取得
	sb.WriteString("$userSid = ([System.Security.Principal.WindowsIdentity]::GetCurrent()).User.Value\n")
	sb.WriteString("icacls $sshDir /inheritance:r /grant '*S-1-5-18:(F)' /grant '*S-1-5-32-544:(F)' /grant \"*${userSid}:(F)\"\n")
	sb.WriteString("if ($LASTEXITCODE -ne 0) { Write-Output 'ACL_SET_FAILED_DIR'; exit 1 }\n")
	sb.WriteString("icacls $keyFile /inheritance:r /grant '*S-1-5-18:(F)' /grant '*S-1-5-32-544:(F)' /grant \"*${userSid}:(F)\"\n")
	sb.WriteString("if ($LASTEXITCODE -ne 0) { Write-Output 'ACL_SET_FAILED_FILE'; exit 1 }\n")

	sb.WriteString("Write-Output 'KEY_DEPLOYED'\n")

	return sb.String()
}

// keyFileTarget は鍵の配置先と判定理由を表す。
type keyFileTarget struct {
	isAdmin bool   // true => administrators_authorized_keys に配置
	reason  string // 判定理由（ログ表示用）
}

// effectiveAdminKeysFromSshdT は sshd -T 出力から authorizedkeysfile の実効値を解析する。
// SSHD_T_OK マーカーより後の行を対象にし、ok=false は行が見つからなかったことを示す。
func effectiveAdminKeysFromSshdT(output string) (isAdmin bool, ok bool) {
	const marker = "SSHD_T_OK"
	idx := strings.Index(output, marker)
	if idx < 0 {
		return false, false
	}
	rest := output[idx+len(marker):]
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line) // CRLF 対応
		lower := strings.ToLower(trimmed)
		const prefix = "authorizedkeysfile "
		if strings.HasPrefix(lower, prefix) {
			val := strings.TrimSpace(trimmed[len(prefix):])
			return strings.Contains(strings.ToLower(val), "administrators_authorized_keys"), true
		}
	}
	return false, false
}

// looksLikeNonWindows は PowerShell 実行エラー出力が Linux/非 Windows ホストを示すか判定する。
func looksLikeNonWindows(output string) bool {
	lower := strings.ToLower(output)
	for _, sig := range []string{
		"command not found",
		"not recognized as",
		"powershell: not found",
		"bash:",
		"sh:",
	} {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}

// resolveKeyFileTarget は鍵の配置先を決定する。
// sshd -T による Match 評価済み実効値を優先し、失敗時は sshd_config テキストパースにフォールバックする。
func resolveKeyFileTarget(client *ssh.Client) (keyFileTarget, error) {
	// Step 1: Administrators グループ判定
	fmt.Println("=> Checking Administrators group...")
	checkScript := "$id = [System.Security.Principal.WindowsIdentity]::GetCurrent(); $principal = New-Object System.Security.Principal.WindowsPrincipal($id); $principal.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)"
	output, err := runRemotePowerShell(client, checkScript)
	if err != nil {
		if looksLikeNonWindows(output) {
			return keyFileTarget{}, fmt.Errorf("remote host does not appear to be Windows; ssh-pushkey targets Windows OpenSSH servers")
		}
		fmt.Printf("=> Skipping admin check (error: %v)\n", err)
		return keyFileTarget{isAdmin: false, reason: "admin check failed, deploying to user directory"}, nil
	}

	if !strings.Contains(output, "True") {
		fmt.Println("=> Standard user detected")
		return keyFileTarget{isAdmin: false, reason: "standard user"}, nil
	}
	fmt.Println("=> User is in Administrators group")

	// Step 2: sshd -T で Match 評価済み実効値を取得
	fmt.Println("=> Checking effective sshd configuration (sshd -T)...")
	sshdTScript := `
$u = $env:USERNAME
try {
    $out = & sshd -T -C "user=$u,host=localhost,addr=127.0.0.1" 2>$null
    if ($LASTEXITCODE -eq 0) {
        Write-Output 'SSHD_T_OK'
        $out | Write-Output
    } else {
        Write-Output 'SSHD_T_FAILED'
    }
} catch {
    Write-Output 'SSHD_T_FAILED'
}`
	sshdTOut, sshdTErr := runRemotePowerShell(client, sshdTScript)
	if sshdTErr == nil && strings.Contains(sshdTOut, "SSHD_T_OK") {
		isAdmin, ok := effectiveAdminKeysFromSshdT(sshdTOut)
		if ok {
			if isAdmin {
				fmt.Println("=> administrators_authorized_keys is enabled (sshd -T)")
				return keyFileTarget{isAdmin: true, reason: "sshd -T: administrators_authorized_keys"}, nil
			}
			fmt.Println("=> sshd -T: AuthorizedKeysFile does not point to administrators_authorized_keys")
			return keyFileTarget{isAdmin: false, reason: "sshd -T: user AuthorizedKeysFile"}, nil
		}
		// authorizedkeysfile 行が出力になかった場合はフォールバックへ
	}

	// Step 3: フォールバック — sshd_config テキストパース（Match Group administrators ブロック）
	fmt.Println("=> Falling back to sshd_config parsing...")
	sshdScript := `
$configPath = 'C:\ProgramData\ssh\sshd_config'
if (-not (Test-Path $configPath)) { Write-Output 'NO_CONFIG'; exit 0 }
$lines = Get-Content $configPath
$inMatchBlock = $false
$foundAuthKeysFile = $false
foreach ($line in $lines) {
    $trimmed = $line.Trim()
    if ($trimmed -match '(?i)^\s*Match\s+Group\s+administrators\s*(#.*)?$') {
        $inMatchBlock = $true
        continue
    }
    if ($inMatchBlock) {
        if ($trimmed -match '(?i)^\s*Match\s') { break }
        if ($trimmed -match '(?i)^\s*AuthorizedKeysFile\s+(.+)') {
            $val = $Matches[1].Trim()
            if ($val -like '*administrators_authorized_keys*') {
                $foundAuthKeysFile = $true
            }
            break
        }
    }
}
if ($inMatchBlock -and $foundAuthKeysFile) {
    Write-Output 'ADMIN_KEYS_ENABLED'
} elseif ($inMatchBlock) {
    Write-Output 'MATCH_BLOCK_NO_ADMIN_KEYS'
} else {
    Write-Output 'NO_MATCH_BLOCK'
}`
	configOut, configErr := runRemotePowerShell(client, sshdScript)
	if configErr != nil {
		fmt.Println("=> Skipping sshd_config check, deploying to user directory")
		return keyFileTarget{isAdmin: false, reason: "sshd_config check failed, deploying to user directory"}, nil
	}

	if strings.Contains(configOut, "ADMIN_KEYS_ENABLED") {
		fmt.Println("=> administrators_authorized_keys is enabled")
		return keyFileTarget{isAdmin: true, reason: "sshd_config: administrators_authorized_keys"}, nil
	}

	if strings.Contains(configOut, "MATCH_BLOCK_NO_ADMIN_KEYS") {
		fmt.Println("=> [WARNING] Match Group administrators block exists but AuthorizedKeysFile")
		fmt.Println("=>           does not point to administrators_authorized_keys")
		fmt.Println("=>           Deploying to user directory instead")
		return keyFileTarget{isAdmin: false, reason: "sshd_config: Match block present but no admin key file"}, nil
	}

	fmt.Println("=> [WARNING] User is in Administrators group but administrators_authorized_keys is disabled")
	fmt.Println("=>           If 'Match Group administrators' is not set in sshd_config,")
	fmt.Println("=>           user directory keys may not be used for authentication")
	return keyFileTarget{isAdmin: false, reason: "sshd_config: no Match Group administrators block"}, nil
}

func DeployKey(client *ssh.Client, pubKey string) error {
	target, err := resolveKeyFileTarget(client)
	if err != nil {
		return err
	}
	fmt.Printf("=> Target: %s\n", target.reason)

	fmt.Println("=> Deploying public key...")
	script := buildDeployScript(pubKey, target.isAdmin)
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
