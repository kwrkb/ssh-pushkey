package main

import (
	"errors"
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
