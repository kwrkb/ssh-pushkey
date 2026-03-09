package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

var version = "dev"

func main() {
	keyPath := flag.String("i", defaultPubKeyPath(), "公開鍵ファイルのパス")
	port := flag.Int("p", 22, "SSHポート番号")
	showVersion := flag.Bool("version", false, "バージョンを表示")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: ssh-pushkey [-i identity_file] [-p port] user@host\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("ssh-pushkey %s\n", version)
		os.Exit(0)
	}

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	user, host, err := parseTarget(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	pubKey, err := readPubKey(*keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=> 公開鍵を読み込みました: %s\n", *keyPath)

	fmt.Printf("=> %s@%s:%d に接続します...\n", user, host, *port)
	password, err := promptPassword(fmt.Sprintf("%s@%s's password: ", user, host))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	client, err := dialSSH(user, host, *port, password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: SSH接続に失敗しました: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()
	fmt.Println("=> SSH接続に成功しました")

	if err := DeployKey(client, pubKey); err != nil {
		fmt.Fprintf(os.Stderr, "Error: 鍵の配置に失敗しました: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=> 公開鍵の配置が完了しました！")
}

func defaultPubKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "id_ed25519.pub")
}

func parseTarget(target string) (user, host string, err error) {
	parts := strings.SplitN(target, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid target: %q (expected user@host)", target)
	}
	return parts[0], parts[1], nil
}

func readPubKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("公開鍵ファイルを読み込めません: %w", err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("公開鍵ファイルが空です: %s", path)
	}
	parts := strings.Fields(key)
	if len(parts) < 2 {
		return "", fmt.Errorf("無効な公開鍵フォーマットです: %s", path)
	}
	return key, nil
}

func promptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("パスワード読み込みに失敗: %w", err)
	}
	return string(password), nil
}
