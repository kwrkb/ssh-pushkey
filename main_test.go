package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// テスト用に固定の正規 ed25519 公開鍵
const validEd25519PubKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBwa4JTkbuiW41olDGfQiKbxFUH+2cU4Yqs1MWkyIAHX test@example"

func TestParseSshAddOutput(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "single key",
			input:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample user@host\n",
			expect: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample user@host",
		},
		{
			name:   "multiple keys returns first",
			input:  "ssh-rsa AAAAB3Nza first@host\nssh-ed25519 AAAAC3Nza second@host\n",
			expect: "ssh-rsa AAAAB3Nza first@host",
		},
		{
			name:   "empty output",
			input:  "",
			expect: "",
		},
		{
			name:   "whitespace only",
			input:  "  \n  \n",
			expect: "",
		},
		{
			name:   "sk-ssh-ed25519 FIDO key",
			input:  "sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1 fido@host\n",
			expect: "sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1 fido@host",
		},
		{
			name:   "sk-ecdsa FIDO key",
			input:  "sk-ecdsa-sha2-nistp256@openssh.com AAAAInNr fido@host\n",
			expect: "sk-ecdsa-sha2-nistp256@openssh.com AAAAInNr fido@host",
		},
		{
			name:   "agent has no identities message",
			input:  "The agent has no identities.\n",
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSshAddOutput(tt.input)
			if got != tt.expect {
				t.Errorf("parseSshAddOutput(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestFindNewestPubKey(t *testing.T) {
	dir := t.TempDir()

	// Create older key
	older := filepath.Join(dir, "id_rsa.pub")
	if err := os.WriteFile(older, []byte("ssh-rsa AAAAB3Nza old@host"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-1 * time.Hour)
	os.Chtimes(older, oldTime, oldTime)

	// Create newer key
	newer := filepath.Join(dir, "id_ed25519.pub")
	if err := os.WriteFile(newer, []byte("ssh-ed25519 AAAAC3Nza new@host"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := findNewestPubKeyIn(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != newer {
		t.Errorf("findNewestPubKeyIn() = %q, want %q", got, newer)
	}
}

func TestFindNewestPubKey_NoKeys(t *testing.T) {
	dir := t.TempDir()

	_, err := findNewestPubKeyIn(dir)
	if err == nil {
		t.Fatal("expected error for empty directory, got nil")
	}
}

func TestFindNewestPubKey_SingleKey(t *testing.T) {
	dir := t.TempDir()

	only := filepath.Join(dir, "id_ecdsa.pub")
	if err := os.WriteFile(only, []byte("ecdsa-sha2-nistp256 AAAAE2Vj key@host"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := findNewestPubKeyIn(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != only {
		t.Errorf("findNewestPubKeyIn() = %q, want %q", got, only)
	}
}

func TestFindNewestPubKey_IgnoresNonIdFiles(t *testing.T) {
	dir := t.TempDir()

	// This should NOT match the id_*.pub pattern
	other := filepath.Join(dir, "authorized_keys.pub")
	if err := os.WriteFile(other, []byte("ssh-rsa AAAAB3Nza other@host"), 0644); err != nil {
		t.Fatal(err)
	}

	// Only id_*.pub should match
	key := filepath.Join(dir, "id_rsa.pub")
	if err := os.WriteFile(key, []byte("ssh-rsa AAAAB3Nza user@host"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := findNewestPubKeyIn(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != key {
		t.Errorf("findNewestPubKeyIn() = %q, want %q", got, key)
	}
}

func writeTempPubKey(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pub")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp pub key: %v", err)
	}
	return path
}

func TestReadPubKey_ValidSingleKey(t *testing.T) {
	path := writeTempPubKey(t, validEd25519PubKey+"\n")

	got, err := readPubKey(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != validEd25519PubKey {
		t.Errorf("readPubKey returned %q, want %q", got, validEd25519PubKey)
	}
}

func TestReadPubKey_TrailingBlankLines(t *testing.T) {
	path := writeTempPubKey(t, validEd25519PubKey+"\n\n   \n")

	got, err := readPubKey(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != validEd25519PubKey {
		t.Errorf("readPubKey returned %q, want %q", got, validEd25519PubKey)
	}
}

func TestReadPubKey_Empty(t *testing.T) {
	path := writeTempPubKey(t, "")

	if _, err := readPubKey(path); err == nil {
		t.Error("expected error for empty file, got nil")
	} else if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty, got %v", err)
	}
}

func TestReadPubKey_WhitespaceOnly(t *testing.T) {
	path := writeTempPubKey(t, "   \n\n\t\n")

	if _, err := readPubKey(path); err == nil {
		t.Error("expected error for whitespace-only file, got nil")
	} else if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty, got %v", err)
	}
}

func TestReadPubKey_MultipleKeys(t *testing.T) {
	path := writeTempPubKey(t, validEd25519PubKey+"\n"+validEd25519PubKey+"\n")

	if _, err := readPubKey(path); err == nil {
		t.Error("expected error for multiple keys, got nil")
	} else if !strings.Contains(err.Error(), "exactly one key") {
		t.Errorf("error should mention 'exactly one key', got %v", err)
	}
}

func TestReadPubKey_InvalidFormat(t *testing.T) {
	path := writeTempPubKey(t, "ssh-rsa GARBAGE comment\n")

	if _, err := readPubKey(path); err == nil {
		t.Error("expected error for invalid key format, got nil")
	} else if !strings.Contains(err.Error(), "invalid public key format") {
		t.Errorf("error should mention 'invalid public key format', got %v", err)
	}
}

func TestReadPubKey_TooFewFields(t *testing.T) {
	path := writeTempPubKey(t, "just-one-token\n")

	if _, err := readPubKey(path); err == nil {
		t.Error("expected error for malformed key, got nil")
	}
}

func TestValidatePubKeyLine_Valid(t *testing.T) {
	got, err := validatePubKeyLine(validEd25519PubKey + "\r\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != validEd25519PubKey {
		t.Errorf("validatePubKeyLine returned %q, want %q", got, validEd25519PubKey)
	}
}

func TestValidatePubKeyLine_TrimsLeadingAndTrailingSpaces(t *testing.T) {
	got, err := validatePubKeyLine("  \t" + validEd25519PubKey + " \t\r\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != validEd25519PubKey {
		t.Errorf("validatePubKeyLine returned %q, want %q (no leading/trailing spaces)", got, validEd25519PubKey)
	}
}

func TestValidatePubKeyLine_Invalid(t *testing.T) {
	if _, err := validatePubKeyLine("not a key"); err == nil {
		t.Error("expected error for invalid input, got nil")
	}
}

// fakeGetter は ssh_config 解決をテストするためのマップバックト getter。
// キーは "alias|Keyword"。未登録は "" を返す。
func fakeGetter(m map[string]string) sshConfigGetter {
	return func(alias, key string) (string, error) {
		return m[alias+"|"+key], nil
	}
}

func TestSplitUserHost(t *testing.T) {
	cases := []struct {
		in       string
		wantUser string
		wantHost string
	}{
		{"admin@host", "admin", "host"},
		{"host", "", "host"},
		{"user@1.2.3.4", "user", "1.2.3.4"},
		{"@host", "", "host"},
		{"user@", "user", ""},
	}
	for _, c := range cases {
		u, h := splitUserHost(c.in)
		if u != c.wantUser || h != c.wantHost {
			t.Errorf("splitUserHost(%q) = (%q,%q), want (%q,%q)", c.in, u, h, c.wantUser, c.wantHost)
		}
	}
}

func TestResolveConnection(t *testing.T) {
	empty := fakeGetter(nil)

	t.Run("backward compat: user@host, no config -> identical to legacy", func(t *testing.T) {
		u, h, p, err := resolveConnection(empty, "admin@192.168.1.10", 22, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u != "admin" || h != "192.168.1.10" || p != 22 {
			t.Errorf("got (%q,%q,%d), want (admin,192.168.1.10,22)", u, h, p)
		}
	})

	t.Run("host-only with no config and no user -> error", func(t *testing.T) {
		_, _, _, err := resolveConnection(empty, "myserver", 22, false)
		if err == nil {
			t.Error("expected error when no user can be resolved")
		}
	})

	t.Run("alias resolves HostName/User/Port from config", func(t *testing.T) {
		get := fakeGetter(map[string]string{
			"myserver|HostName": "10.0.0.5",
			"myserver|User":     "admin",
			"myserver|Port":     "2222",
		})
		u, h, p, err := resolveConnection(get, "myserver", 22, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u != "admin" || h != "10.0.0.5" || p != 2222 {
			t.Errorf("got (%q,%q,%d), want (admin,10.0.0.5,2222)", u, h, p)
		}
	})

	t.Run("explicit user@ overrides config User", func(t *testing.T) {
		get := fakeGetter(map[string]string{"myserver|User": "configuser"})
		u, _, _, err := resolveConnection(get, "cliuser@myserver", 22, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u != "cliuser" {
			t.Errorf("user = %q, want cliuser (explicit user@ must win)", u)
		}
	})

	t.Run("config Port used when -p not explicit", func(t *testing.T) {
		get := fakeGetter(map[string]string{"myserver|User": "admin", "myserver|Port": "2222"})
		_, _, p, err := resolveConnection(get, "myserver", 22, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != 2222 {
			t.Errorf("port = %d, want 2222 (config Port should apply)", p)
		}
	})

	t.Run("explicit -p overrides config Port", func(t *testing.T) {
		get := fakeGetter(map[string]string{"myserver|User": "admin", "myserver|Port": "2222"})
		_, _, p, err := resolveConnection(get, "myserver", 2020, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != 2020 {
			t.Errorf("port = %d, want 2020 (explicit -p must win)", p)
		}
	})

	t.Run("invalid config Port is ignored, falls back to default", func(t *testing.T) {
		get := fakeGetter(map[string]string{"myserver|User": "admin", "myserver|Port": "notanumber"})
		_, _, p, err := resolveConnection(get, "myserver", 22, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != 22 {
			t.Errorf("port = %d, want 22 (invalid Port should fall back)", p)
		}
	})

	t.Run("config Port applies to plain user@host (Host * block) when -p absent", func(t *testing.T) {
		// 意図的挙動の固定: ssh_config は alias だけでなく任意の一致 Host パターン
		// （例 `Host *`）にも適用される。明示 user@ は維持しつつ、-p 不在なら config Port が効く。
		wildcard := func(alias, key string) (string, error) {
			if key == "Port" {
				return "2200", nil
			}
			return "", nil
		}
		u, h, p, err := resolveConnection(wildcard, "admin@1.2.3.4", 22, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u != "admin" || h != "1.2.3.4" || p != 2200 {
			t.Errorf("got (%q,%q,%d), want (admin,1.2.3.4,2200)", u, h, p)
		}
	})

	t.Run("empty target -> error", func(t *testing.T) {
		_, _, _, err := resolveConnection(empty, "", 22, false)
		if err == nil {
			t.Error("expected error for empty target")
		}
	})
}
