package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// ホスト鍵検証パス（TOFU / --insecure / 不一致拒否 / 誤検知なし / 鍵更新 / HashKnownHosts）を、
// 実 Windows ホスト不要で検証する。ホスト鍵検証は完全にクライアント側の Go SSH 処理であり、
// OS 非依存なので 127.0.0.1 上に立てた標準ライブラリの SSH サーバで実コードパス
// （dialSSH -> createHostKeyCallback -> knownhosts）をそのまま exercise できる。
//
// 注意: これらのテストは HOME を t.Setenv で差し替え、パッケージ変数 readLineFromTerminal を
// 上書きするため t.Parallel() を使ってはならない（直列実行が前提）。
//
// 1点だけ自動化できない経路: readLineFromTerminal 内の実 /dev/tty オープン。
// ここはプロンプト応答を注入するため上書きしており、TTY 自体の挙動は手動確認に委ねる。

const (
	testSSHUser     = "tester"
	testSSHPassword = "s3cret"
)

// generateHostKey はテスト用の ed25519 ホスト鍵を生成する。
// 不一致テストでは同一種別（ed25519）の別鍵が必要なため、呼ぶたびにランダムな鍵を返す。
func generateHostKey(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	return signer
}

// startTestSSHServer は 127.0.0.1 上に password 認証のみの SSH サーバを起動し、ポートを返す。
// dialSSH はハンドシェイク＋認証完了で *ssh.Client を返す（チャネルは開かない）ため、
// チャネル要求は拒否で十分。リスナーは t.Cleanup でクローズする。
func startTestSSHServer(t *testing.T, hostKey ssh.Signer) int {
	t.Helper()

	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == testSSHUser && string(pass) == testSSHPassword {
				return nil, nil
			}
			return nil, fmt.Errorf("authentication failed")
		},
	}
	config.AddHostKey(hostKey)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // リスナーがクローズされた
			}
			go func() {
				sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
				if err != nil {
					conn.Close()
					return
				}
				go ssh.DiscardRequests(reqs)
				for newCh := range chans {
					newCh.Reject(ssh.Prohibited, "no channels in test server")
				}
				sshConn.Close()
			}()
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port
}

// normalizedAddr は createHostKeyCallback が known_hosts 照合に使うのと同じ正規化アドレスを返す。
func normalizedAddr(port int) string {
	return knownhosts.Normalize(fmt.Sprintf("127.0.0.1:%d", port))
}

// tempHome は HOME を新しい一時ディレクトリに差し替え、その known_hosts パスを返す。
// dialSSH -> createHostKeyCallback は os.UserHomeDir() 配下を参照する。
// os.UserHomeDir() は Unix では $HOME、Windows では %USERPROFILE% を優先するため、
// 両方を temp に振らないと Windows でのテスト実行時に開発者の実 known_hosts を汚染し得る。
func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return filepath.Join(home, ".ssh", "known_hosts")
}

// seedKnownHost は known_hosts に1エントリを書き込む。hashed=true でハッシュ化形式にする。
func seedKnownHost(t *testing.T, path, addr string, key ssh.PublicKey, hashed bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	hostField := addr
	if hashed {
		hostField = knownhosts.HashHostname(addr)
	}
	line := knownhosts.Line([]string{hostField}, key) + "\n"
	if err := os.WriteFile(path, []byte(line), 0600); err != nil {
		t.Fatalf("seed known_hosts: %v", err)
	}
}

// overridePrompt は readLineFromTerminal を固定応答で差し替え、呼び出し回数カウンタを返す。
func overridePrompt(t *testing.T, answer string) *int {
	t.Helper()
	prev := readLineFromTerminal
	count := 0
	readLineFromTerminal = func() (string, error) {
		count++
		return answer, nil
	}
	t.Cleanup(func() { readLineFromTerminal = prev })
	return &count
}

// failIfPrompted はプロンプトが一度でも呼ばれたらテストを失敗させる
// （既知ホスト・--insecure で誤ってプロンプトが出ないことを保証する）。
func failIfPrompted(t *testing.T) {
	t.Helper()
	prev := readLineFromTerminal
	readLineFromTerminal = func() (string, error) {
		t.Errorf("host key prompt was invoked but should not have been")
		return "no", nil
	}
	t.Cleanup(func() { readLineFromTerminal = prev })
}

// keyB64 は公開鍵の wire フォーマットを base64 化する（known_hosts 内の鍵照合用）。
func keyB64(key ssh.PublicKey) string {
	return base64.StdEncoding.EncodeToString(key.Marshal())
}

// TestHostKeyVerification_Insecure は --insecure が検証をスキップし、known_hosts を一切触らない
// ことを検証する（プロンプトも出ない）。
func TestHostKeyVerification_Insecure(t *testing.T) {
	hostKey := generateHostKey(t)
	port := startTestSSHServer(t, hostKey)
	khPath := tempHome(t)
	failIfPrompted(t)

	client, err := dialSSH(testSSHUser, "127.0.0.1", port, testSSHPassword, true)
	if err != nil {
		t.Fatalf("insecure dial should succeed: %v", err)
	}
	client.Close()

	if _, err := os.Stat(khPath); !os.IsNotExist(err) {
		t.Errorf("--insecure must not create or write known_hosts (stat err = %v)", err)
	}
}

// TestHostKeyVerification_TOFUAcceptThenNoFalsePositive は未知ホストへの TOFU 承認で
// known_hosts に追記されること、かつ追記後の再接続ではプロンプトが出ない（誤検知なし）ことを検証する。
func TestHostKeyVerification_TOFUAcceptThenNoFalsePositive(t *testing.T) {
	hostKey := generateHostKey(t)
	port := startTestSSHServer(t, hostKey)
	khPath := tempHome(t)

	// 1回目: 未知ホスト → "yes" で承認 → 追記
	count := overridePrompt(t, "yes")
	client, err := dialSSH(testSSHUser, "127.0.0.1", port, testSSHPassword, false)
	if err != nil {
		t.Fatalf("TOFU accept dial should succeed: %v", err)
	}
	client.Close()

	if *count != 1 {
		t.Errorf("expected exactly 1 prompt on first connect, got %d", *count)
	}

	data, err := os.ReadFile(khPath)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if !strings.Contains(string(data), keyB64(hostKey.PublicKey())) {
		t.Errorf("known_hosts must contain the accepted host key after TOFU\ncontent:\n%s", data)
	}

	// 2回目: 既知になったので誤検知でプロンプトを出してはならない
	failIfPrompted(t)
	client2, err := dialSSH(testSSHUser, "127.0.0.1", port, testSSHPassword, false)
	if err != nil {
		t.Fatalf("second connect to a now-known host should succeed without prompting: %v", err)
	}
	client2.Close()
}

// TestHostKeyVerification_TOFUReject は未知ホストへの TOFU 拒否で接続が失敗し、
// known_hosts に鍵が書き込まれないことを検証する。
func TestHostKeyVerification_TOFUReject(t *testing.T) {
	hostKey := generateHostKey(t)
	port := startTestSSHServer(t, hostKey)
	khPath := tempHome(t)
	overridePrompt(t, "no")

	client, err := dialSSH(testSSHUser, "127.0.0.1", port, testSSHPassword, false)
	if err == nil {
		client.Close()
		t.Fatal("TOFU reject must fail the connection")
	}
	if !strings.Contains(err.Error(), "host key verification failed") {
		t.Errorf("error should report host key verification failure, got: %v", err)
	}

	// createHostKeyCallback は空の known_hosts を作るが、拒否時に鍵は追記されてはならない。
	if data, rerr := os.ReadFile(khPath); rerr == nil && strings.Contains(string(data), keyB64(hostKey.PublicKey())) {
		t.Errorf("rejected host key must not be written to known_hosts\ncontent:\n%s", data)
	}
}

// TestHostKeyVerification_Mismatch_Reject は登録済みと異なるホスト鍵を提示された際、
// 更新プロンプトに "no" で答えると接続が拒否されることを検証する。
func TestHostKeyVerification_Mismatch_Reject(t *testing.T) {
	serverKey := generateHostKey(t)
	wrongKey := generateHostKey(t) // 同一種別(ed25519)の別鍵: HostKeyAlgorithms 制限の再試行経路を避ける
	port := startTestSSHServer(t, serverKey)
	khPath := tempHome(t)
	addr := normalizedAddr(port)

	seedKnownHost(t, khPath, addr, wrongKey.PublicKey(), false)
	overridePrompt(t, "no")

	client, err := dialSSH(testSSHUser, "127.0.0.1", port, testSSHPassword, false)
	if err == nil {
		client.Close()
		t.Fatal("host key mismatch with a 'no' answer must fail the connection")
	}
	if !strings.Contains(err.Error(), "host key verification failed") {
		t.Errorf("error should report host key verification failure, got: %v", err)
	}

	// 拒否したので known_hosts の鍵は旧（wrong）のまま、サーバ鍵に置き換わってはならない。
	data, rerr := os.ReadFile(khPath)
	if rerr != nil {
		t.Fatalf("read known_hosts: %v", rerr)
	}
	if strings.Contains(string(data), keyB64(serverKey.PublicKey())) {
		t.Errorf("rejected update must not write the new host key\ncontent:\n%s", data)
	}
}

// TestHostKeyVerification_Mismatch_UpdateAccept は登録済みと異なるホスト鍵を提示された際、
// 更新プロンプトに "yes" で答えると known_hosts が新しい鍵へ更新され接続できることを検証する。
func TestHostKeyVerification_Mismatch_UpdateAccept(t *testing.T) {
	serverKey := generateHostKey(t)
	wrongKey := generateHostKey(t)
	port := startTestSSHServer(t, serverKey)
	khPath := tempHome(t)
	addr := normalizedAddr(port)

	seedKnownHost(t, khPath, addr, wrongKey.PublicKey(), false)
	overridePrompt(t, "yes")

	client, err := dialSSH(testSSHUser, "127.0.0.1", port, testSSHPassword, false)
	if err != nil {
		t.Fatalf("accepting the updated host key should succeed: %v", err)
	}
	client.Close()

	data, err := os.ReadFile(khPath)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if !strings.Contains(string(data), keyB64(serverKey.PublicKey())) {
		t.Errorf("known_hosts must be updated to the new (server) host key\ncontent:\n%s", data)
	}
	if strings.Contains(string(data), keyB64(wrongKey.PublicKey())) {
		t.Errorf("the old (wrong) host key must be removed from known_hosts\ncontent:\n%s", data)
	}

	// 更新後の再接続はプロンプトなしで成功すること。
	failIfPrompted(t)
	client2, err := dialSSH(testSSHUser, "127.0.0.1", port, testSSHPassword, false)
	if err != nil {
		t.Fatalf("reconnect after update should succeed without prompting: %v", err)
	}
	client2.Close()
}

// TestHostKeyVerification_HashKnownHosts_MatchesWithoutPrompt は HashKnownHosts yes 相当の
// ハッシュ化エントリでも既知ホストとして正しく照合され、プロンプトが出ないことを検証する。
func TestHostKeyVerification_HashKnownHosts_MatchesWithoutPrompt(t *testing.T) {
	hostKey := generateHostKey(t)
	port := startTestSSHServer(t, hostKey)
	khPath := tempHome(t)
	addr := normalizedAddr(port)

	seedKnownHost(t, khPath, addr, hostKey.PublicKey(), true) // ハッシュ化エントリ
	failIfPrompted(t)

	client, err := dialSSH(testSSHUser, "127.0.0.1", port, testSSHPassword, false)
	if err != nil {
		t.Fatalf("a hashed known_hosts entry must validate without prompting: %v", err)
	}
	client.Close()
}

// TestHostKeyVerification_HashKnownHosts_AppendsHashed は、既存 known_hosts がハッシュ形式の
// エントリを含む場合、新規 TOFU 追記もハッシュ化されることを検証する（HashKnownHosts 環境での初回接続）。
func TestHostKeyVerification_HashKnownHosts_AppendsHashed(t *testing.T) {
	hostKey := generateHostKey(t)
	port := startTestSSHServer(t, hostKey)
	khPath := tempHome(t)

	// 別ホストのハッシュ化エントリを既存として置く（このサーバ自体は未知のまま）。
	unrelatedKey := generateHostKey(t)
	seedKnownHost(t, khPath, "[10.0.0.1]:22", unrelatedKey.PublicKey(), true)

	overridePrompt(t, "yes")
	client, err := dialSSH(testSSHUser, "127.0.0.1", port, testSSHPassword, false)
	if err != nil {
		t.Fatalf("TOFU accept should succeed: %v", err)
	}
	client.Close()

	data, err := os.ReadFile(khPath)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}

	// 新規追記行（サーバ鍵を含む行）がハッシュ化形式 |1| であること。
	var appended string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, keyB64(hostKey.PublicKey())) {
			appended = line
			break
		}
	}
	if appended == "" {
		t.Fatalf("appended entry for the server key not found\ncontent:\n%s", data)
	}
	if !strings.HasPrefix(strings.TrimSpace(appended), "|1|") {
		t.Errorf("new entry must be hashed when known_hosts already has hashed entries, got: %q", appended)
	}
}
