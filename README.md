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

## Build

```bash
go build -ldflags "-X main.version=1.1.0" -o ssh-pushkey
```

## Test

```bash
# Unit tests
go test ./...

# Integration tests (requires a real Windows host)
export SSH_TEST_HOST=192.168.1.10
export SSH_TEST_USER=user
read -rs SSH_TEST_PASSWORD && export SSH_TEST_PASSWORD
go test -tags=integration -v ./...
```

## License

MIT
