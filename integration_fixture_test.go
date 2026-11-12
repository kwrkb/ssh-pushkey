package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// 統合テストが実機の authorized_keys を一時的に書き換えるときの退避・復元スクリプト。
// build tag を付けないファイルに置き、生成スクリプトの安全性（他人の退避ファイルを消さない、
// 退避が無いときに本体を消さない）を通常の go test ./... で検証できるようにする。
// 実際に使うのは integration_test.go の backupRemoteKeyFile。
//
// 旧方式は固定名の .pushkey-test-bak を「既存なら削除 → コピー → 復元時に move」し、
// 退避が無ければ「元々ファイルが無かった」とみなして本体を削除していた。同じアカウント
// （管理者は共有の administrators_authorized_keys）に対して 2 つの実行が重なると、
// 片方がもう片方の退避を消して上書きし、最後は退避不在と判断して本体を削除するため、
// 元の鍵が失われた。中断後に残った退避を次の実行が無条件に消すのも同じ原因（Issue #33 AUD-004）。
//
// 新方式:
//   - 排他: $keyFile.pushkey-test-lock を CreateNew で作る（既にあれば IOException＝別の実行中か中断の跡。
//     権限不足などそれ以外の例外はロック中と取り違えず、スクリプトごと失敗させる）
//   - 退避名に実行ごとの ID を付け、他の実行の退避には触らない
//   - 中断の跡（.pushkey-test-* が残っている）があれば何も変えずに止め、手動での確認を求める
//   - 「元々ファイルが無い」ことは推測せず、.pushkey-test-absent-<id> の目印で記録する
//   - 復元できたときだけロックを外す。退避も目印も無ければ本体に触らず、ロックも残して次の実行を止める

const (
	fixtureMarkerReady    = "FIXTURE_READY"
	fixtureMarkerLocked   = "FIXTURE_LOCKED:"
	fixtureMarkerLeftover = "FIXTURE_LEFTOVER:"
	fixtureMarkerRestored = "FIXTURE_RESTORED:"
	fixtureMarkerMissing  = "FIXTURE_RESTORE_MISSING"
)

// remoteKeyFileExpr は buildDeployScript と同じ式で $keyFile を決める PowerShell 片を返す。
func remoteKeyFileExpr(isAdmin bool) string {
	if isAdmin {
		return "$keyFile = 'C:\\ProgramData\\ssh\\administrators_authorized_keys'"
	}
	return "$keyFile = Join-Path $env:USERPROFILE '.ssh\\authorized_keys'"
}

// newFixtureID は退避ファイル名とロックの所有者を区別する実行ごとの ID を返す。
func newFixtureID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generate fixture id: %v", err)
	}
	return hex.EncodeToString(b)
}

// fixturePathsExpr は $keyFile を決めたうえでロック・退避・目印のパスを定義する PowerShell 片を返す。
// id は hex のみなので引用符のエスケープは不要。
func fixturePathsExpr(keyFileExpr, id string) string {
	return keyFileExpr + "\n" +
		"$lock = \"$keyFile.pushkey-test-lock\"\n" +
		fmt.Sprintf("$bak = \"$keyFile.pushkey-test-bak-%s\"\n", id) +
		fmt.Sprintf("$absent = \"$keyFile.pushkey-test-absent-%s\"\n", id) +
		fmt.Sprintf("$owner = '%s'\n", id)
}

// buildFixtureBackupScript はロックを取り、authorized_keys を ID 付きの名前で退避するスクリプトを返す。
// 成功時は FIXTURE_READY、別の実行中なら FIXTURE_LOCKED:<path>、中断の跡があれば
// FIXTURE_LEFTOVER:<names> を出す。後者 2 つでは何も変更しない（自分で作ったロックは外す）。
func buildFixtureBackupScript(keyFileExpr, id string) string {
	return "$ErrorActionPreference = 'Stop'\n" +
		fixturePathsExpr(keyFileExpr, id) +
		`$dir = Split-Path -Parent $keyFile
if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
try {
  $fs = [System.IO.File]::Open($lock, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write)
} catch [System.IO.IOException] {
  Write-Output "FIXTURE_LOCKED:$lock"
  exit 0
}
try {
  $bytes = [System.Text.Encoding]::ASCII.GetBytes($owner)
  $fs.Write($bytes, 0, $bytes.Length)
} finally {
  $fs.Close()
}
try {
  $prefix = (Split-Path -Leaf $keyFile) + '.pushkey-test-'
  $left = @(Get-ChildItem -LiteralPath $dir -Force | Where-Object { $_.Name.StartsWith($prefix) -and $_.FullName -ne $lock })
  if ($left.Count -gt 0) {
    Remove-Item -LiteralPath $lock -Force
    Write-Output ("FIXTURE_LEFTOVER:" + (($left | ForEach-Object { $_.Name }) -join ','))
    exit 0
  }
  if (Test-Path -LiteralPath $keyFile) {
    Copy-Item -LiteralPath $keyFile -Destination $bak
  } else {
    New-Item -ItemType File -Path $absent | Out-Null
  }
} catch {
  Remove-Item -LiteralPath $lock -Force -ErrorAction SilentlyContinue
  throw
}
Write-Output 'FIXTURE_READY'
`
}

// buildFixtureRestoreScript は自分の退避（または「元々無かった」目印）から authorized_keys を戻し、
// 自分のロックを外すスクリプトを返す。退避も目印も無ければ本体に触らず FIXTURE_RESTORE_MISSING を出し、
// ロックは残す（次の実行を止めて、手動で確かめさせる）。
func buildFixtureRestoreScript(keyFileExpr, id string) string {
	return "$ErrorActionPreference = 'Stop'\n" +
		fixturePathsExpr(keyFileExpr, id) +
		`if (Test-Path -LiteralPath $bak) {
  Move-Item -LiteralPath $bak -Destination $keyFile -Force
  Write-Output 'FIXTURE_RESTORED:backup'
} elseif (Test-Path -LiteralPath $absent) {
  if (Test-Path -LiteralPath $keyFile) { Remove-Item -LiteralPath $keyFile -Force }
  Remove-Item -LiteralPath $absent -Force
  Write-Output 'FIXTURE_RESTORED:absent'
} else {
  Write-Output 'FIXTURE_RESTORE_MISSING'
  exit 0
}
if ((Test-Path -LiteralPath $lock) -and ((Get-Content -LiteralPath $lock -Raw) -ceq $owner)) {
  Remove-Item -LiteralPath $lock -Force
}
`
}

func TestBuildFixtureBackupScript(t *testing.T) {
	const id = "0123456789abcdef"
	script := buildFixtureBackupScript(remoteKeyFileExpr(true), id)

	for _, want := range []string{
		"$ErrorActionPreference = 'Stop'",
		`$keyFile = 'C:\ProgramData\ssh\administrators_authorized_keys'`,
		`$bak = "$keyFile.pushkey-test-bak-0123456789abcdef"`,
		`$absent = "$keyFile.pushkey-test-absent-0123456789abcdef"`,
		"[System.IO.FileMode]::CreateNew",
		"catch [System.IO.IOException]",
		`Write-Output "FIXTURE_LOCKED:$lock"`,
		"FIXTURE_LEFTOVER:",
		"Copy-Item -LiteralPath $keyFile -Destination $bak",
		"New-Item -ItemType File -Path $absent",
		"Write-Output 'FIXTURE_READY'",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("backup script should contain %q\n\nscript:\n%s", want, script)
		}
	}

	// 中断の跡や他の実行の退避を消さないこと。旧方式は既存の退避を無条件に削除していた。
	for _, banned := range []string{
		"Remove-Item -Force $bak",
		"Remove-Item -LiteralPath $bak",
		"pushkey-test-bak\"",
	} {
		if strings.Contains(script, banned) {
			t.Errorf("backup script must not contain %q\n\nscript:\n%s", banned, script)
		}
	}
	// 中断の跡を見つけたら FIXTURE_READY より前に止まること
	if strings.Index(script, "FIXTURE_LEFTOVER:") > strings.Index(script, "Write-Output 'FIXTURE_READY'") {
		t.Errorf("leftover check must come before FIXTURE_READY\n\nscript:\n%s", script)
	}
	// 退避の前にロックを取ること
	if strings.Index(script, "CreateNew") > strings.Index(script, "Copy-Item") {
		t.Errorf("lock must be acquired before the backup copy\n\nscript:\n%s", script)
	}
}

func TestBuildFixtureRestoreScript(t *testing.T) {
	const id = "0123456789abcdef"
	script := buildFixtureRestoreScript(remoteKeyFileExpr(false), id)

	for _, want := range []string{
		`$keyFile = Join-Path $env:USERPROFILE '.ssh\authorized_keys'`,
		"Move-Item -LiteralPath $bak -Destination $keyFile -Force",
		"Write-Output 'FIXTURE_RESTORED:backup'",
		"Write-Output 'FIXTURE_RESTORED:absent'",
		"Write-Output 'FIXTURE_RESTORE_MISSING'",
		"(Get-Content -LiteralPath $lock -Raw) -ceq $owner",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("restore script should contain %q\n\nscript:\n%s", want, script)
		}
	}

	// 本体の削除は「元々無かった」目印がある分岐の中だけにあること。
	// 旧方式は退避が無いだけで本体を削除していたため、他の実行に退避を奪われると鍵を失った。
	absentIdx := strings.Index(script, "elseif (Test-Path -LiteralPath $absent)")
	removeIdx := strings.Index(script, "Remove-Item -LiteralPath $keyFile")
	missingIdx := strings.Index(script, "FIXTURE_RESTORE_MISSING")
	if absentIdx < 0 || removeIdx < absentIdx || removeIdx > missingIdx {
		t.Errorf("key file may only be removed inside the absent-marker branch\n\nscript:\n%s", script)
	}
	if strings.Count(script, "Remove-Item -LiteralPath $keyFile") != 1 {
		t.Errorf("key file must be removed in exactly one place\n\nscript:\n%s", script)
	}
	// 復元できなかったときはロックを外さずに終わること
	if strings.Index(script, "exit 0") > strings.Index(script, "Remove-Item -LiteralPath $lock") {
		t.Errorf("restore must exit before releasing the lock when nothing was restored\n\nscript:\n%s", script)
	}
}

func TestFixtureMarkers_IgnoreCLIXMLNoise(t *testing.T) {
	out := "#< CLIXML\r\n<Objs Version=\"1.1.0.1\"><Obj S=\"progress\">Preparing modules for first use.</Obj></Objs>\r\n" +
		"FIXTURE_LEFTOVER:authorized_keys.pushkey-test-bak\r\n"
	if hasMarkerLine(out, fixtureMarkerReady) {
		t.Errorf("FIXTURE_READY must not be detected in %q", out)
	}
	if got := markerValue(out, fixtureMarkerLeftover); got != "authorized_keys.pushkey-test-bak" {
		t.Errorf("leftover = %q", got)
	}
}
