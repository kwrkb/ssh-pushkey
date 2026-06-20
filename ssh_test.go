package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh/knownhosts"
)

func TestMatchHashedHost(t *testing.T) {
	addr := "[192.168.1.1]:22"
	hashed := knownhosts.HashHostname(addr)

	if !matchHashedHost(hashed, addr) {
		t.Errorf("matchHashedHost(%q, %q) = false, want true", hashed, addr)
	}

	if matchHashedHost(hashed, "[10.0.0.1]:22") {
		t.Error("matchHashedHost should not match a different address")
	}
}

func TestMatchHashedHost_InvalidFormats(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
	}{
		{"empty", ""},
		{"plain host", "example.com"},
		{"wrong magic", "|2|AAAA|BBBB"},
		{"missing fields", "|1|AAAA"},
		{"bad salt base64", "|1|!!!invalid!!!|AAAA"},
		{"bad hash base64", "|1|AAAA|!!!invalid!!!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if matchHashedHost(tt.pattern, "example.com") {
				t.Errorf("matchHashedHost(%q, ...) = true, want false", tt.pattern)
			}
		})
	}
}

func TestHostMatchesAddr_PlainText(t *testing.T) {
	if !hostMatchesAddr("example.com", "example.com") {
		t.Error("hostMatchesAddr should match identical plain-text hosts")
	}
	if hostMatchesAddr("example.com", "other.com") {
		t.Error("hostMatchesAddr should not match different plain-text hosts")
	}
}

func TestHostMatchesAddr_Hashed(t *testing.T) {
	addr := "example.com"
	hashed := knownhosts.HashHostname(addr)

	if !hostMatchesAddr(hashed, addr) {
		t.Errorf("hostMatchesAddr(%q, %q) = false, want true", hashed, addr)
	}
	if hostMatchesAddr(hashed, "other.com") {
		t.Error("hostMatchesAddr should not match a different address against hashed entry")
	}
}

func TestShouldRetryWithoutHostKeyAlgorithms(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "no common algorithm for host key",
			err:  errors.New("ssh: no common algorithm for host key; client offered: [ssh-ed25519], server offered: [ecdsa-sha2-nistp384]"),
			want: true,
		},
		{
			name: "no common algorithm uppercase",
			err:  errors.New("ssh: handshake failed: SSH: NO COMMON ALGORITHM FOR HOST KEY"),
			want: true,
		},
		{
			name: "authentication failure should not retry",
			err:  errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [password], no supported methods remain"),
			want: false,
		},
		{
			name: "connection refused should not retry",
			err:  errors.New("dial tcp 192.168.1.1:22: connect: connection refused"),
			want: false,
		},
		{
			name: "host key changed should not retry",
			err:  errors.New("ssh: host key mismatch"),
			want: false,
		},
		{
			name: "no common algorithm for kex (not host key) should not retry",
			err:  errors.New("ssh: no common algorithm for key exchange"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetryWithoutHostKeyAlgorithms(tt.err); got != tt.want {
				t.Errorf("shouldRetryWithoutHostKeyAlgorithms(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestAppendKnownHostsLine(t *testing.T) {
	cases := []struct {
		name    string
		initial string // ファイル初期内容（"" は存在するが空のファイル）
		want    string
	}{
		{
			name:    "existing file without trailing newline gets a separator",
			initial: "host1 ssh-ed25519 AAAA",
			want:    "host1 ssh-ed25519 AAAA\nhost2 ssh-ed25519 BBBB\n",
		},
		{
			name:    "existing file with trailing newline is not double-spaced",
			initial: "host1 ssh-ed25519 AAAA\n",
			want:    "host1 ssh-ed25519 AAAA\nhost2 ssh-ed25519 BBBB\n",
		},
		{
			name:    "empty file gets no leading newline",
			initial: "",
			want:    "host2 ssh-ed25519 BBBB\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "known_hosts")
			if err := os.WriteFile(path, []byte(c.initial), 0600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if err := appendKnownHostsLine(path, "host2 ssh-ed25519 BBBB"); err != nil {
				t.Fatalf("appendKnownHostsLine: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			if string(got) != c.want {
				t.Errorf("content = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAtomicWriteFile_CreatesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")

	// 新規作成
	if err := atomicWriteFile(path, []byte("first\n"), 0600); err != nil {
		t.Fatalf("atomicWriteFile (create): %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "first\n" {
		t.Errorf("content = %q, want %q", got, "first\n")
	}

	// 上書き（rename で置換）
	if err := atomicWriteFile(path, []byte("second\n"), 0600); err != nil {
		t.Fatalf("atomicWriteFile (overwrite): %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "second\n" {
		t.Errorf("content = %q, want %q", got, "second\n")
	}

	// temp ファイルが残っていないこと
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "known_hosts" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only known_hosts to remain, got %v", names)
	}
}
