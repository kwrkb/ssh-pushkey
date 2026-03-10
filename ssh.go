package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func dialSSH(user, host string, port int, password string, insecure bool) (*ssh.Client, error) {
	var hostKeyCallback ssh.HostKeyCallback
	var hostKeyAlgorithms []string

	if insecure {
		fmt.Println("=> WARNING: Host key verification is disabled. This is vulnerable to MITM attacks.")
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	} else {
		var err error
		hostKeyCallback, hostKeyAlgorithms, err = createHostKeyCallback(host, port)
		if err != nil {
			return nil, fmt.Errorf("host key setup failed: %w", err)
		}
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: hostKeyAlgorithms,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil && len(hostKeyAlgorithms) > 0 {
		// HostKeyAlgorithmsの制限によりハンドシェイクが失敗した場合、
		// 制限を外してリトライし、HostKeyCallbackのインタラクティブ更新に委ねる。
		config.HostKeyAlgorithms = nil
		return ssh.Dial("tcp", addr, config)
	}
	return client, err
}

// createHostKeyCallback はknown_hostsファイルを使用したホスト鍵検証コールバックを作成する。
// 未知のホストに対してはTOFU（Trust on First Use）でフィンガープリントを表示し、
// ユーザーの承認後にknown_hostsに追記する。
// 戻り値のhostKeyAlgorithmsは、known_hostsに登録済みの鍵アルゴリズム一覧。
// Go SSHクライアントのネゴシエーションをOpenSSHと同じ挙動に制限するために使用する。
func createHostKeyCallback(host string, port int) (ssh.HostKeyCallback, []string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")

	// known_hostsファイルが存在しない場合は空ファイルを作成
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		sshDir := filepath.Dir(knownHostsPath)
		if err := os.MkdirAll(sshDir, 0700); err != nil {
			return nil, nil, fmt.Errorf("cannot create .ssh directory: %w", err)
		}
		if err := os.WriteFile(knownHostsPath, nil, 0600); err != nil {
			return nil, nil, fmt.Errorf("cannot create known_hosts file: %w", err)
		}
	}

	cb, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read known_hosts: %w", err)
	}

	addr := knownhosts.Normalize(fmt.Sprintf("%s:%d", host, port))

	// known_hostsから対象ホストの鍵アルゴリズムを取得。
	// OpenSSHはknown_hostsにあるアルゴリズムだけをネゴシエーションするが、
	// Goのknownhostsライブラリはこれを行わないため、自前で制限する。
	hostKeyAlgorithms := hostKeyAlgorithmsFromKnownHosts(knownHostsPath, addr)

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return err
		}

		if len(keyErr.Want) > 0 {
			// ホスト鍵が変更されている — MITM攻撃の可能性
			fmt.Println("@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@")
			fmt.Println("@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @")
			fmt.Println("@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@")
			fmt.Println("IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!")
			fmt.Println("Someone could be eavesdropping on you right now (man-in-the-middle attack)!")
			fmt.Println("It is also possible that a host key has just been changed.")
			fmt.Printf("The fingerprint for the %s key sent by the remote host is\n%s.\n",
				key.Type(), ssh.FingerprintSHA256(key))
			fmt.Printf("Do you want to update your known_hosts file (%s)? (yes/no) ", knownHostsPath)

			answer, err := readLineFromTerminal()
			if err != nil {
				return fmt.Errorf("failed to read response: %w", err)
			}
			if answer != "yes" {
				return fmt.Errorf("host key verification failed")
			}

			// known_hostsから古いエントリを除去して新しい鍵を追記
			if err := replaceHostKeyInKnownHosts(knownHostsPath, addr, key); err != nil {
				return fmt.Errorf("failed to update known_hosts: %w", err)
			}

			fmt.Printf("Warning: Updated host key for '%s' in known_hosts.\n", addr)
			return nil
		}

		// 未知のホスト — TOFUプロンプト
		fmt.Printf("The authenticity of host '%s (%s)' can't be established.\n",
			hostname, remote.String())
		fmt.Printf("%s key fingerprint is %s.\n", key.Type(), ssh.FingerprintSHA256(key))
		fmt.Print("Are you sure you want to continue connecting (yes/no)? ")

		// 端末から直接読み取る（パイプ入力と分離）
		answer, err := readLineFromTerminal()
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		if answer != "yes" {
			return fmt.Errorf("host key verification failed")
		}

		// known_hostsに追記
		line := knownhosts.Line([]string{addr}, key)
		f, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("cannot write to known_hosts: %w", err)
		}
		defer f.Close()

		if _, err := fmt.Fprintln(f, line); err != nil {
			return fmt.Errorf("cannot write to known_hosts: %w", err)
		}

		fmt.Printf("Warning: Permanently added '%s' to the list of known hosts.\n", addr)
		return nil
	}, hostKeyAlgorithms, nil
}

// matchHashedHost はハッシュ化されたknown_hostsエントリ（|1|<salt>|<hash>）が
// 指定アドレスにマッチするかをHMAC-SHA1で検証する。
func matchHashedHost(pattern, addr string) bool {
	// フォーマット: |1|<base64-salt>|<base64-hash>
	parts := strings.Split(pattern, "|")
	if len(parts) != 4 || parts[0] != "" || parts[1] != "1" {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expectedHash, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(addr))
	return hmac.Equal(mac.Sum(nil), expectedHash)
}

// hostMatchesAddr はknown_hostsのホストフィールド（plain-textまたはハッシュ形式）が
// 指定アドレスにマッチするかを判定する。
func hostMatchesAddr(host, addr string) bool {
	if strings.HasPrefix(host, "|") {
		return matchHashedHost(host, addr)
	}
	return host == addr
}

// hostKeyAlgorithmsFromKnownHosts はknown_hostsファイルから対象ホストの鍵アルゴリズム一覧を返す。
// ホストが未登録の場合はnilを返し、SSHクライアントのデフォルト動作に委ねる。
func hostKeyAlgorithmsFromKnownHosts(knownHostsPath string, addr string) []string {
	data, err := os.ReadFile(knownHostsPath)
	if err != nil {
		return nil
	}

	var algorithms []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 3 {
			continue
		}
		hosts := strings.Split(fields[0], ",")
		for _, h := range hosts {
			if hostMatchesAddr(h, addr) {
				algorithms = append(algorithms, fields[1])
				break
			}
		}
	}
	return algorithms
}

// replaceHostKeyInKnownHosts はknown_hostsファイルから指定ホストの古いエントリを除去し、
// 新しいホスト鍵を追記する。
func replaceHostKeyInKnownHosts(knownHostsPath string, addr string, newKey ssh.PublicKey) error {
	data, err := os.ReadFile(knownHostsPath)
	if err != nil {
		return fmt.Errorf("cannot read known_hosts: %w", err)
	}

	var kept []string
	hasHashed := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			kept = append(kept, line)
			continue
		}
		// 行の先頭フィールド（カンマ区切りのホスト一覧）をチェック
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			kept = append(kept, line)
			continue
		}
		hosts := strings.Split(fields[0], ",")
		var remaining []string
		matched := false
		for _, h := range hosts {
			if hostMatchesAddr(h, addr) {
				matched = true
				if strings.HasPrefix(h, "|") {
					hasHashed = true
				}
			} else {
				remaining = append(remaining, h)
			}
		}
		if !matched {
			// addrにマッチしない行 — そのまま保持
			kept = append(kept, line)
		} else if len(remaining) > 0 {
			// 他のエイリアスが残っている — addrだけ除去してフィールドから再構成
			fields[0] = strings.Join(remaining, ",")
			kept = append(kept, strings.Join(fields, " "))
		}
		// remaining が空の場合は行ごと削除（対象ホストのみの行）
	}

	// 末尾の空行を整理して書き戻す
	content := strings.Join(kept, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	// 新しいエントリを追記（既存エントリがハッシュ形式ならハッシュ化して追記）
	var hostEntry []string
	if hasHashed {
		hostEntry = []string{knownhosts.HashHostname(addr)}
	} else {
		hostEntry = []string{addr}
	}
	line := knownhosts.Line(hostEntry, newKey)
	content += line + "\n"

	if err := os.WriteFile(knownHostsPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("cannot write known_hosts: %w", err)
	}
	return nil
}

// readLineFromTerminal は端末から1行読み取る。
// パスワード入力後にStdinがパイプ化されている場合でも/dev/ttyから直接読み取る。
// rawモードは使用せず、行バッファリングを利用してバックスペース等の行編集を有効にする。
func readLineFromTerminal() (string, error) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		// /dev/ttyが使えない場合（Windows等）はStdinにフォールバック
		return readLine(os.Stdin)
	}
	defer tty.Close()
	return readLine(tty)
}

// readLine はリーダーから改行までの1行を読み取る。
func readLine(r *os.File) (string, error) {
	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// encodePowerShellCommand はPowerShellスクリプトをUTF-16LEのBase64にエンコードする。
// -EncodedCommand で使用することで、シェルエスケープ問題を完全に回避する。
func encodePowerShellCommand(script string) string {
	runes := utf16.Encode([]rune(script))
	bytes := make([]byte, len(runes)*2)
	for i, r := range runes {
		bytes[i*2] = byte(r)
		bytes[i*2+1] = byte(r >> 8)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

// runRemotePowerShell はPowerShellスクリプトを-EncodedCommand経由で安全に実行する。
func runRemotePowerShell(client *ssh.Client, script string) (string, error) {
	encoded := encodePowerShellCommand(script)
	command := fmt.Sprintf("powershell -NoProfile -EncodedCommand %s", encoded)
	return runRemoteCommand(client, command)
}

func runRemoteCommand(client *ssh.Client, command string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	if err != nil {
		return string(output), fmt.Errorf("command execution failed: %w\noutput: %s", err, output)
	}
	return string(output), nil
}
