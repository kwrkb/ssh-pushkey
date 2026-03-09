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
	keyPath := flag.String("i", defaultPubKeyPath(), "path to public key file")
	port := flag.Int("p", 22, "SSH port number")
	showVersion := flag.Bool("version", false, "show version")
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

	fmt.Printf("=> Public key loaded: %s\n", *keyPath)

	fmt.Printf("=> Connecting to %s@%s:%d...\n", user, host, *port)
	password, err := promptPassword(fmt.Sprintf("%s@%s's password: ", user, host))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	client, err := dialSSH(user, host, *port, password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: SSH connection failed: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()
	fmt.Println("=> SSH connection established")

	if err := DeployKey(client, pubKey); err != nil {
		fmt.Fprintf(os.Stderr, "Error: key deployment failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=> Key deployment completed!")
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
		return "", fmt.Errorf("cannot read public key file: %w", err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("public key file is empty: %s", path)
	}
	parts := strings.Fields(key)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid public key format: %s", path)
	}
	return key, nil
}

func promptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	return string(password), nil
}
