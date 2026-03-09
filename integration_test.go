//go:build integration

package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// 実行方法（パスワード手打ち）:
//   read -rs SSH_TEST_PASSWORD && export SSH_TEST_PASSWORD
//   SSH_TEST_HOST=192.168.1.10 SSH_TEST_USER=kiwar go test -tags=integration -v ./...

type testEnv struct {
	host     string
	port     int
	user     string
	password string
}

func loadTestEnv(t *testing.T) testEnv {
	t.Helper()

	host := os.Getenv("SSH_TEST_HOST")
	user := os.Getenv("SSH_TEST_USER")
	password := os.Getenv("SSH_TEST_PASSWORD")

	if host == "" || user == "" || password == "" {
		t.Skip("SSH_TEST_HOST, SSH_TEST_USER, SSH_TEST_PASSWORD are required")
	}

	port := 22
	if p := os.Getenv("SSH_TEST_PORT"); p != "" {
		var err error
		port, err = strconv.Atoi(p)
		if err != nil {
			t.Fatalf("invalid SSH_TEST_PORT: %s", p)
		}
	}

	return testEnv{host: host, port: port, user: user, password: password}
}

func TestIntegration_SSHConnect(t *testing.T) {
	env := loadTestEnv(t)

	client, err := dialSSH(env.user, env.host, env.port, env.password)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()
}

func TestIntegration_RemotePowerShell(t *testing.T) {
	env := loadTestEnv(t)

	client, err := dialSSH(env.user, env.host, env.port, env.password)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()

	// 変数展開が正しく動くことを確認（-EncodedCommandの検証）
	output, err := runRemotePowerShell(client, "$x = 'hello'; Write-Output $x")
	if err != nil {
		t.Fatalf("PowerShell execution failed: %v", err)
	}

	// PowerShellはモジュール初期化時にCLIXMLプログレスを出力することがある
	if !strings.Contains(output, "hello") {
		t.Errorf("output should contain %q, got %q", "hello", output)
	}
}

func TestIntegration_AdminDetection(t *testing.T) {
	env := loadTestEnv(t)

	client, err := dialSSH(env.user, env.host, env.port, env.password)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()

	// パニックせず結果を返すことを確認（True/False どちらでもOK）
	result := useAdminKeyFile(client)
	t.Logf("useAdminKeyFile = %v", result)
}

func TestIntegration_DeployKey(t *testing.T) {
	env := loadTestEnv(t)

	pubKeyPath := os.Getenv("SSH_TEST_PUBKEY")
	if pubKeyPath == "" {
		pubKeyPath = defaultPubKeyPath()
	}

	pubKey, err := readPubKey(pubKeyPath)
	if err != nil {
		t.Fatalf("failed to read public key: %v", err)
	}

	client, err := dialSSH(env.user, env.host, env.port, env.password)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer client.Close()

	if err := DeployKey(client, pubKey); err != nil {
		t.Fatalf("key deployment failed: %v", err)
	}

	// 2回目は重複スキップされることを確認
	if err := DeployKey(client, pubKey); err != nil {
		t.Fatalf("second deployment failed: %v", err)
	}
}

func trimOutput(s string) string {
	// PowerShellの出力にはCRLFが含まれる
	b := []byte(s)
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return string(b)
}
