package main

import (
	"encoding/base64"
	"fmt"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
)

func dialSSH(user, host string, port int, password string) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		// TODO: known_hostsファイルによるホスト鍵検証を実装する
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	return ssh.Dial("tcp", addr, config)
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
		return "", fmt.Errorf("セッション作成に失敗: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	if err != nil {
		return string(output), fmt.Errorf("コマンド実行に失敗: %w\noutput: %s", err, output)
	}
	return string(output), nil
}
