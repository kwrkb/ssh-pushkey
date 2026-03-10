package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
