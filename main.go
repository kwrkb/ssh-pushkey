package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

var version = "dev"

const usageText = `ssh-pushkey - deploy an SSH public key to a Windows OpenSSH server (ssh-copy-id for Windows)

Usage: ssh-pushkey [options] <[user@]host>

Prompts for the password, then deploys the key handling Windows specifics
automatically (BOM-less UTF-8, Administrators branching, icacls ACL).

A <host> may be a Host alias from ~/.ssh/config; HostName, User and Port are
resolved from it (CLI flags and an explicit user@ take precedence). ProxyJump,
IdentityFile and Match are not honored. Note: -i is the public key to DEPLOY,
not an SSH identity — it is unrelated to ssh_config IdentityFile.

Arguments:
  <[user@]host>          target Windows SSH server or ~/.ssh/config Host alias
                         (e.g., admin@192.168.1.10, or just myserver)

Options:
  -i <path>              public key file to deploy (default: first key from ssh-agent,
                         then newest ~/.ssh/id_*.pub)
  -p <port>              SSH port number (default: 22, or Port from ~/.ssh/config)
  --insecure             skip host key verification (vulnerable to MITM)
  --help                 print this help
  --version              print version

Examples:
  ssh-pushkey admin@192.168.1.10                       # auto-discover key and deploy
  ssh-pushkey -i ~/.ssh/id_rsa.pub -p 2222 user@host   # explicit key and port
  ssh-pushkey myserver                                 # resolve User/HostName/Port from ~/.ssh/config
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

	// -p が明示指定されたかを判定（CLI > ssh_config > 既定 の優先順位に使う）
	portExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "p" {
			portExplicit = true
		}
	})

	user, host, resolvedPort, err := resolveConnection(loadUserSSHConfig(), flag.Arg(0), *port, portExplicit)
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

	fmt.Printf("=> Connecting to %s@%s:%d...\n", user, host, resolvedPort)
	password, err := promptPassword(fmt.Sprintf("%s@%s's password: ", user, host))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	client, err := dialSSH(user, host, resolvedPort, password, *insecure)
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

// sshConfigGetter resolves an ssh_config keyword for a host alias.
// It returns "" with a nil error when the keyword is not set.
type sshConfigGetter func(alias, key string) (string, error)

// loadUserSSHConfig returns a getter backed by ~/.ssh/config.
// A missing file yields a getter that resolves nothing; a parse error is
// reported to stderr and likewise resolves nothing (graceful degradation —
// the connection still works without the alias). Only HostName/User/Port are
// ever consulted (see resolveConnection). IdentityFile is intentionally NOT
// read: the -i flag is the public key to DEPLOY, not an SSH auth identity, and
// this tool authenticates with a password.
func loadUserSSHConfig() sshConfigGetter {
	empty := func(string, string) (string, error) { return "", nil }
	home, err := os.UserHomeDir()
	if err != nil {
		return empty
	}
	f, err := os.Open(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return empty
	}
	defer f.Close()
	cfg, err := ssh_config.Decode(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: ignoring ~/.ssh/config (%v)\n", err)
		return empty
	}
	return cfg.Get
}

// resolveConnection determines the effective user/host/port from the CLI target,
// the -p flag, and ~/.ssh/config. Precedence per field:
// explicit CLI (user@, -p) > ssh_config > built-in default.
// Only HostName/User/Port are honored; ProxyJump/IdentityFile/HostKeyAlias/Match
// are unsupported. known_hosts is keyed on the resolved HostName (OpenSSH default).
func resolveConnection(get sshConfigGetter, rawTarget string, cliPort int, portExplicit bool) (user, host string, port int, err error) {
	user, alias := splitUserHost(rawTarget)
	if alias == "" {
		return "", "", 0, fmt.Errorf("invalid target: %q (expected [user@]host)", rawTarget)
	}

	// HostName: use the configured value as the connect address, else the alias.
	host = alias
	if hn, _ := get(alias, "HostName"); hn != "" {
		host = hn
	}

	// User: explicit user@ wins; otherwise ssh_config User; otherwise error.
	if user == "" {
		if cu, _ := get(alias, "User"); cu != "" {
			user = cu
		}
	}
	if user == "" {
		return "", "", 0, fmt.Errorf("no user for %q (use user@host or set 'User' in ~/.ssh/config)", alias)
	}

	// Port: explicit -p wins; otherwise ssh_config Port; otherwise default 22.
	port = cliPort
	if !portExplicit {
		if cp, _ := get(alias, "Port"); cp != "" {
			if p, perr := strconv.Atoi(cp); perr == nil && p > 0 {
				port = p
			}
		}
	}
	if port <= 0 {
		port = 22
	}
	return user, host, port, nil
}

// splitUserHost splits "user@host" on the first "@". With no "@", user is empty
// and the whole string is the host (alias). An empty host is rejected by the caller.
func splitUserHost(target string) (user, host string) {
	if i := strings.IndexByte(target, '@'); i >= 0 {
		return target[:i], target[i+1:]
	}
	return "", target
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
