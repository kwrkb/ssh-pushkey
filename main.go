package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

var version = "dev"

const usageText = `ssh-pushkey - deploy an SSH public key to a Windows OpenSSH server (ssh-copy-id for Windows)

Usage: ssh-pushkey [options] <user@host>

Prompts for the password, then deploys the key handling Windows specifics
automatically (BOM-less UTF-8, Administrators branching, icacls ACL).

Arguments:
  <user@host>            target Windows SSH server (e.g., admin@192.168.1.10)

Options:
  -i <path>              public key file to deploy (default: first key from ssh-agent,
                         then newest ~/.ssh/id_*.pub)
  -p <port>              SSH port number (default: 22)
  -n, --dry-run          preview the deployment target without writing the key
                         (still connects and prompts for the password)
  --insecure             skip host key verification (vulnerable to MITM)
  --help                 print this help
  --version              print version

Examples:
  ssh-pushkey admin@192.168.1.10                       # auto-discover key and deploy
  ssh-pushkey -i ~/.ssh/id_rsa.pub -p 2222 user@host   # explicit key and port
`

func init() {
	if version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
}

func main() {
	keyPath := flag.String("i", "", "public key file to deploy")
	port := flag.Int("p", 22, "SSH port number")
	insecure := flag.Bool("insecure", false, "skip host key verification (vulnerable to MITM)")
	dryRun := flag.Bool("dry-run", false, "preview deployment without writing")
	flag.BoolVar(dryRun, "n", false, "preview deployment without writing")
	showVersion := flag.Bool("version", false, "print version")
	showHelp := flag.Bool("help", false, "print help")
	flag.BoolVar(showHelp, "h", false, "print help")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usageText)
	}
	flag.Parse()

	if *showHelp {
		fmt.Print(usageText)
		os.Exit(0)
	}

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

	pubKey, keySource, err := resolveKey(*keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=> Public key loaded: %s\n", keySource)

	fmt.Printf("=> Connecting to %s@%s:%d...\n", user, host, *port)
	password, err := promptPassword(fmt.Sprintf("%s@%s's password: ", user, host))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	client, err := dialSSH(user, host, *port, password, *insecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: SSH connection failed: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()
	fmt.Println("=> SSH connection established")

	if err := DeployKey(client, pubKey, *dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "Error: key deployment failed: %v\n", err)
		os.Exit(1)
	}

	if *dryRun {
		fmt.Println("=> [DRY-RUN] Preview complete; no changes were made")
	} else {
		fmt.Println("=> Key deployment completed!")
	}
}

// resolveKey returns the public key content and a human-readable source.
// If explicitPath is non-empty, it reads from that file.
// Otherwise it tries ssh-agent first, then falls back to the newest
// ~/.ssh/id_*.pub file.
func resolveKey(explicitPath string) (key, source string, err error) {
	if explicitPath != "" {
		k, err := readPubKey(explicitPath)
		return k, explicitPath, err
	}

	if k, ok := keyFromAgent(); ok {
		return k, "(ssh-agent)", nil
	}

	path, err := findNewestPubKey()
	if err != nil {
		return "", "", err
	}
	k, err := readPubKey(path)
	if err != nil {
		return "", "", err
	}
	return k, path, nil
}

// parseSshAddOutput extracts the first key line from ssh-add -L output.
// Returns empty string if the output contains no valid keys.
func parseSshAddOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && looksLikeKeyType(fields[0]) {
			return line
		}
	}
	return ""
}

func looksLikeKeyType(s string) bool {
	return strings.HasPrefix(s, "ssh-") ||
		strings.HasPrefix(s, "ecdsa-") ||
		strings.HasPrefix(s, "sk-ssh-") ||
		strings.HasPrefix(s, "sk-ecdsa-")
}

func keyFromAgent() (string, bool) {
	out, err := exec.Command("ssh-add", "-L").Output()
	if err != nil {
		return "", false
	}
	key := parseSshAddOutput(string(out))
	if key == "" {
		return "", false
	}
	validated, err := validatePubKeyLine(key)
	if err != nil {
		return "", false
	}
	return validated, true
}

// validatePubKeyLine は単一行の公開鍵文字列を ssh.ParseAuthorizedKey で検証し、
// 前後の空白を除去した値を返す。フォーマット不正時はエラーを返す。
func validatePubKeyLine(line string) (string, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", fmt.Errorf("public key line is empty")
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(trimmed)); err != nil {
		return "", fmt.Errorf("invalid public key format: %w", err)
	}
	return trimmed, nil
}

// findNewestPubKey returns the path to the newest ~/.ssh/id_*.pub file by mtime.
func findNewestPubKey() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return findNewestPubKeyIn(filepath.Join(home, ".ssh"))
}

func findNewestPubKeyIn(sshDir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(sshDir, "id_*.pub"))
	if err != nil {
		return "", fmt.Errorf("failed to search for public keys: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no public key found; specify one with -i or generate a key with ssh-keygen")
	}

	var newest string
	var newestTime int64
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if t := info.ModTime().UnixNano(); t > newestTime {
			newestTime = t
			newest = m
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no readable public key found in %s", sshDir)
	}
	return newest, nil
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

	var nonEmpty []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}
	if len(nonEmpty) == 0 {
		return "", fmt.Errorf("public key file is empty: %s", path)
	}
	if len(nonEmpty) > 1 {
		return "", fmt.Errorf("public key file must contain exactly one key: %s", path)
	}

	key, err := validatePubKeyLine(nonEmpty[0])
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
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
