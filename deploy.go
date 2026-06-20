package main

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// buildDeployScript は鍵配置の PowerShell スクリプトを生成する。
// keyBlob は重複判定用の鍵本体（type + base64、コメント除去済み）。
// dryRun=true の場合は配置先パスと重複判定だけを出力し、書き込み・ACL 設定・
// ディレクトリ作成は一切行わない（本番と同じパス決定・重複ロジックを共有してドリフトを防ぐ）。
func buildDeployScript(pubKey, keyBlob string, isAdmin bool, dryRun bool) string {
	escapedKey := strings.ReplaceAll(pubKey, "'", "''")
	escapedBlob := strings.ReplaceAll(keyBlob, "'", "''")

	var sb strings.Builder

	sb.WriteString("$ErrorActionPreference = 'Stop'\n")
	sb.WriteString(fmt.Sprintf("$pubKey = '%s'\n", escapedKey))
	sb.WriteString(fmt.Sprintf("$keyBlob = '%s'\n", escapedBlob))
	sb.WriteString(fmt.Sprintf("$dryRun = $%t\n", dryRun))

	if isAdmin {
		sb.WriteString("$keyFile = 'C:\\ProgramData\\ssh\\administrators_authorized_keys'\n")
		sb.WriteString("$sshDir = 'C:\\ProgramData\\ssh'\n")
	} else {
		sb.WriteString("$keyFile = Join-Path $env:USERPROFILE '.ssh\\authorized_keys'\n")
		sb.WriteString("$sshDir = Join-Path $env:USERPROFILE '.ssh'\n")
	}

	// 重複チェック（読み取りのみ。dry-run でも本番でも先に評価する）。
	// 鍵 blob（type + base64）のみで比較し、コメント差異があっても同一鍵を検知する。
	// base64 は大小区別が必要なため -ceq（case-sensitive）で比較する。
	// options 前置行（command="..." ssh-rsa ...）は先頭フィールドが type にならず
	// 検知漏れし得るが、最悪でも「重複追記」になるだけで破損はしない（ssh-pushkey 自身の鍵では発生しない）。
	sb.WriteString("$exists = $false\n")
	sb.WriteString("if (Test-Path $keyFile) {\n")
	sb.WriteString("  foreach ($line in (Get-Content $keyFile)) {\n")
	sb.WriteString("    $t = $line.Trim()\n")
	sb.WriteString("    if ($t -eq '' -or $t.StartsWith('#')) { continue }\n")
	sb.WriteString("    $parts = $t -split '\\s+'\n")
	sb.WriteString("    if ($parts.Count -ge 2 -and ($parts[0] + ' ' + $parts[1]) -ceq $keyBlob) { $exists = $true; break }\n")
	sb.WriteString("  }\n")
	sb.WriteString("}\n")

	// dry-run: 配置先と重複状態だけを報告して終了（書き込み・ディレクトリ作成なし）
	sb.WriteString("if ($dryRun) {\n")
	sb.WriteString("  Write-Output \"DRY_RUN_TARGET:$keyFile\"\n")
	sb.WriteString("  if ($exists) { Write-Output 'DRY_RUN_DUP:True' } else { Write-Output 'DRY_RUN_DUP:False' }\n")
	sb.WriteString("  exit 0\n")
	sb.WriteString("}\n")

	// 既に存在すればスキップ
	sb.WriteString("if ($exists) { Write-Output 'KEY_ALREADY_EXISTS'; exit 0 }\n")

	// ディレクトリ作成
	sb.WriteString("if (-not (Test-Path $sshDir)) { New-Item -ItemType Directory -Path $sshDir -Force | Out-Null }\n")

	// BOMなしUTF-8で追記
	sb.WriteString("[System.IO.File]::AppendAllText($keyFile, $pubKey + \"`n\", (New-Object System.Text.UTF8Encoding $false))\n")

	// ACL設定（SIDベース：言語非依存）
	// S-1-5-18 = NT AUTHORITY\SYSTEM
	// S-1-5-32-544 = BUILTIN\Administrators
	// ユーザーSIDは動的に取得
	// icacls の出力は 2>&1 で捕捉し、失敗時にマーカーへ付加して Go 側へ実エラーを伝搬する。
	// Windows PowerShell 5.1 では ErrorActionPreference='Stop' のまま native コマンドの stderr を
	// 2>&1 でリダイレクトすると、icacls が stderr に書いた瞬間に NativeCommandError が終端エラーとして
	// throw され、後続の $LASTEXITCODE チェック（マーカー出力）に到達しない。$LASTEXITCODE を明示的に
	// 検査しているため Stop は不要。icacls 区間だけ Continue に落とし、マーカー出力を保証する。
	sb.WriteString("$ErrorActionPreference = 'Continue'\n")
	sb.WriteString("$userSid = ([System.Security.Principal.WindowsIdentity]::GetCurrent()).User.Value\n")
	sb.WriteString("$aclOut = & icacls $sshDir /inheritance:r /grant '*S-1-5-18:(F)' /grant '*S-1-5-32-544:(F)' /grant \"*${userSid}:(F)\" 2>&1\n")
	sb.WriteString("if ($LASTEXITCODE -ne 0) { Write-Output \"ACL_SET_FAILED_DIR|$aclOut\"; exit 1 }\n")
	sb.WriteString("$fileAclOut = & icacls $keyFile /inheritance:r /grant '*S-1-5-18:(F)' /grant '*S-1-5-32-544:(F)' /grant \"*${userSid}:(F)\" 2>&1\n")
	sb.WriteString("if ($LASTEXITCODE -ne 0) { Write-Output \"ACL_SET_FAILED_FILE|$fileAclOut\"; exit 1 }\n")

	sb.WriteString("Write-Output 'KEY_DEPLOYED'\n")

	return sb.String()
}

// extractAclErrorDetail は ACL 失敗マーカー行（"<marker>|<icacls 実エラー>"）から
// パイプ以降の実エラーメッセージを抽出する。見つからなければ "(unknown error)" を返す。
func extractAclErrorDetail(output, marker string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, marker) {
			if _, detail, ok := strings.Cut(line, "|"); ok {
				if d := strings.TrimSpace(detail); d != "" {
					return d
				}
			}
		}
	}
	return "(unknown error)"
}

// printDryRunResult は dry-run スクリプト出力から配置先と重複状態を表示する。
func printDryRunResult(output string) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "DRY_RUN_TARGET:"):
			fmt.Printf("=> [DRY-RUN] Deployment target: %s\n", strings.TrimPrefix(line, "DRY_RUN_TARGET:"))
		case line == "DRY_RUN_DUP:True":
			fmt.Println("=> [DRY-RUN] Key already exists, would skip")
		case line == "DRY_RUN_DUP:False":
			fmt.Println("=> [DRY-RUN] Key would be newly deployed")
		}
	}
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
		"unknown command",
		"no such file or directory",
		"not supported on this platform",
		"platformnotsupported",
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
$ErrorActionPreference = 'Stop'
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

func DeployKey(client *ssh.Client, pubKey string, dryRun bool) error {
	target, err := resolveKeyFileTarget(client)
	if err != nil {
		return err
	}
	fmt.Printf("=> Target: %s\n", target.reason)

	blob, err := pubKeyBlob(pubKey)
	if err != nil {
		return fmt.Errorf("cannot parse public key: %w", err)
	}

	if dryRun {
		fmt.Println("=> [DRY-RUN] Previewing deployment (no changes will be made)...")
	} else {
		fmt.Println("=> Deploying public key...")
	}
	script := buildDeployScript(pubKey, blob, target.isAdmin, dryRun)
	output, err := runRemotePowerShell(client, script)
	result := strings.TrimSpace(output)

	// exit 1 でエラーが返る場合もマーカーを優先チェック
	switch {
	case strings.Contains(result, "ACL_SET_FAILED_DIR"):
		return fmt.Errorf("failed to set directory ACL: %s", extractAclErrorDetail(result, "ACL_SET_FAILED_DIR"))
	case strings.Contains(result, "ACL_SET_FAILED_FILE"):
		return fmt.Errorf("failed to set key file ACL: %s", extractAclErrorDetail(result, "ACL_SET_FAILED_FILE"))
	case err != nil:
		return fmt.Errorf("key deployment failed: %w", err)
	case strings.Contains(result, "DRY_RUN_TARGET:"):
		printDryRunResult(result)
	case strings.Contains(result, "KEY_ALREADY_EXISTS"):
		fmt.Println("=> Key already exists, skipping")
	case strings.Contains(result, "KEY_DEPLOYED"):
		fmt.Println("=> Key deployed successfully")
	default:
		fmt.Printf("=> Output: %s\n", result)
	}

	return nil
}
