package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/term"
)

func dialSSH(user, host string, port int, password string, insecure bool) (*ssh.Client, error) {
	var hostKeyCallback ssh.HostKeyCallback

	if insecure {
		fmt.Println("=> WARNING: Host key verification is disabled. This is vulnerable to MITM attacks.")
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	} else {
		var err error
		hostKeyCallback, err = createHostKeyCallback(host, port)
		if err != nil {
			return nil, fmt.Errorf("host key setup failed: %w", err)
		}
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: hostKeyCallback,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	return ssh.Dial("tcp", addr, config)
}

// createHostKeyCallback はknown_hostsファイルを使用したホスト鍵検証コールバックを作成する。
// 未知のホストに対してはTOFU（Trust on First Use）でフィンガープリントを表示し、
// ユーザーの承認後にknown_hostsに追記する。
func createHostKeyCallback(host string, port int) (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")

	// known_hostsファイルが存在しない場合は空ファイルを作成
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		sshDir := filepath.Dir(knownHostsPath)
		if err := os.MkdirAll(sshDir, 0700); err != nil {
			return nil, fmt.Errorf("cannot create .ssh directory: %w", err)
		}
		if err := os.WriteFile(knownHostsPath, nil, 0600); err != nil {
			return nil, fmt.Errorf("cannot create known_hosts file: %w", err)
		}
	}

	cb, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read known_hosts: %w", err)
	}

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
			return fmt.Errorf("%s",
				"@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n"+
					"@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n"+
					"@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n"+
					"IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!\n"+
					"Someone could be eavesdropping on you right now (man-in-the-middle attack)!\n"+
					"It is also possible that a host key has just been changed.\n"+
					fmt.Sprintf("The fingerprint for the %s key sent by the remote host is\n%s.\n",
						key.Type(), ssh.FingerprintSHA256(key))+
					fmt.Sprintf("Please update your known_hosts file: %s", knownHostsPath))
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
		addr := knownhosts.Normalize(fmt.Sprintf("%s:%d", host, port))
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
	}, nil
}

// readLineFromTerminal は端末から1行読み取る。
// パスワード入力後にStdinがパイプ化されている場合でも/dev/ttyから直接読み取る。
func readLineFromTerminal() (string, error) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		// /dev/ttyが使えない場合（Windows等）はStdinにフォールバック
		var buf [256]byte
		n, err := os.Stdin.Read(buf[:])
		if err != nil {
			return "", err
		}
		return string(buf[:n-1]), nil // trim newline
	}
	defer tty.Close()

	oldState, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		// rawモードにできない場合はそのまま読む
		var buf [256]byte
		n, readErr := tty.Read(buf[:])
		if readErr != nil {
			return "", readErr
		}
		return string(buf[:n-1]), nil
	}
	defer term.Restore(int(tty.Fd()), oldState)

	var result []byte
	var b [1]byte
	for {
		n, err := tty.Read(b[:])
		if err != nil || n == 0 {
			break
		}
		if b[0] == '\r' || b[0] == '\n' {
			fmt.Fprint(os.Stderr, "\n")
			break
		}
		result = append(result, b[0])
		fmt.Fprintf(os.Stderr, "%c", b[0])
	}
	return string(result), nil
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
