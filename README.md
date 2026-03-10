# ssh-pushkey

[日本語](README_ja.md)

An `ssh-copy-id` alternative for Windows OpenSSH servers.

Connects via password authentication and automatically deploys your public key. Handles all Windows-specific pitfalls (BOM, Admin branching, ACL). Sets proper ACL on both the `.ssh` directory and key file.

## Installation

Download binaries from [GitLab Releases](https://gitlab.com/kwrkb/ssh-pushkey/-/releases) / [GitHub Releases](https://github.com/kwrkb/ssh-pushkey/releases), or:

```bash
go install gitlab.com/kwrkb/ssh-pushkey@latest
```

## Usage

```bash
ssh-pushkey user@host
```

Enter your password and the rest is fully automated.

### Options

| Flag | Default | Description |
|------|---------|-------------|
| `-i` | `~/.ssh/id_ed25519.pub` | Path to the public key file |
| `-p` | `22` | SSH port number |
| `--insecure` | `false` | Skip host key verification (not recommended) |
| `--version` | - | Show version |

### Examples

```bash
# Use the default key
ssh-pushkey admin@192.168.1.10

# Specify key and port
ssh-pushkey -i ~/.ssh/id_rsa.pub -p 2222 user@server
```

## What it does

1. Connects via SSH with password authentication
2. Detects whether the user is in the Administrators group
3. Checks if `administrators_authorized_keys` is enabled in `sshd_config`
4. Writes the public key in BOM-less UTF-8 to the appropriate file
5. Sets ACL on both the `.ssh` directory and key file via `icacls` (`SYSTEM:(F)`, `Administrators:(F)`, `User:(F)`)

| User type | Key destination |
|-----------|-----------------|
| Admin (`administrators_authorized_keys` enabled) | `C:\ProgramData\ssh\administrators_authorized_keys` |
| Admin (disabled) / Regular user | `~\.ssh\authorized_keys` |

## Security

### Host key verification

By default, ssh-pushkey verifies the remote host's key against `~/.ssh/known_hosts`, the same as OpenSSH. On first connection to an unknown host, you'll be prompted to verify the fingerprint (Trust on First Use). Accepted keys are automatically added to your `known_hosts` file.

If the host key has changed since a previous connection, the connection is refused with a warning (possible MITM attack).

Use `--insecure` to skip host key verification. **This is not recommended** as it makes the connection vulnerable to man-in-the-middle attacks, potentially exposing your password.

### ACL handling

ACL entries use well-known SIDs (`S-1-5-18` for SYSTEM, `S-1-5-32-544` for Administrators) instead of localized group names, ensuring correct behavior on non-English Windows installations and domain environments.

## Build

```bash
go build -ldflags "-X main.version=$(git describe --tags --abbrev=0)" -o ssh-pushkey
```

## Test

### Unit tests

```bash
go test ./...
```

### Integration tests

Integration tests connect to a real Windows OpenSSH server. They are gated behind the `integration` build tag and skipped when the required environment variables are not set.

**Setup:**

1. Copy the example env file and edit it:
   ```bash
   cp .env.integration.example .env.integration
   # Edit .env.integration with your host/user
   ```

2. Add your password (not stored in the file for security):
   ```bash
   read -rs SSH_TEST_PASSWORD && export SSH_TEST_PASSWORD
   ```

3. Run:
   ```bash
   source .env.integration && go test -tags=integration -v ./...
   ```

**Environment variables:**

| Variable | Required | Description |
|----------|----------|-------------|
| `SSH_TEST_HOST` | Yes | Windows SSH server IP or hostname |
| `SSH_TEST_USER` | Yes | SSH username |
| `SSH_TEST_PASSWORD` | Yes | SSH password (use `read -rs` to set) |
| `SSH_TEST_PORT` | No | SSH port (default: 22) |
| `SSH_TEST_PUBKEY` | No | Path to public key (default: `~/.ssh/id_ed25519.pub`) |

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for release history.

## License

MIT
