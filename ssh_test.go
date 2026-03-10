package main

import (
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
