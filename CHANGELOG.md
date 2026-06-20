# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `-n` / `--dry-run` flag: preview the deployment target and whether the key already exists, without writing the key or changing ACLs (`ssh-copy-id -n` compatible). Still connects and prompts for the password, since the target is resolved on the remote host.
- Resolve `~/.ssh/config` for the target: a `<host>` may now be a `Host` alias, with `HostName`, `User` and `Port` resolved from the matching block. Resolution applies to any matching `Host` pattern (including `Host *`), not just explicit aliases, so a plain `user@host` also picks up matching config values. Precedence is CLI (`user@`, `-p`) > ssh_config > built-in default. `known_hosts` is keyed on the resolved `HostName`. `ProxyJump`, `HostKeyAlias`, `Match` and `IdentityFile` are not honored (`-i` is the public key to deploy, unrelated to `IdentityFile`); a missing or unparseable config is ignored gracefully.

### Changed

- Connection now times out after 30 seconds at the TCP dial stage, preventing indefinite hangs against unreachable hosts.
- ACL failures now propagate the underlying `icacls` error message (e.g. "Access is denied.") instead of a generic "failed to set ACL".

## [1.5.1] - 2026-06-04

### Security

- Upgraded `golang.org/x/crypto` v0.48.0 → v0.52.0 to resolve 7 reachable vulnerabilities (GO-2026-5013, GO-2026-5015, GO-2026-5017, GO-2026-5018, GO-2026-5019, GO-2026-5020, GO-2026-5021), including an authentication bypass in `knownhosts` (GO-2026-5021)

## [1.5.0] - 2026-06-04

### Added

- Explicit error when connecting to a non-Windows host; ssh-pushkey targets Windows OpenSSH servers
- Improved `--help` output: self-description line, `<required>`/`[optional]` notation, Arguments section with example, auto-discover fallback details for `-i`, and Examples section
- `-h` shorthand as alias for `--help` (stdout + exit 0)

### Changed

- Resolve the effective `AuthorizedKeysFile` via `sshd -T -C` (Match-aware) before falling back to textual `sshd_config` parsing, so admin-key detection handles configurations beyond a literal `Match Group administrators` block

## [1.4.1] - 2026-04-29

### Security

- Reject `.pub` files containing multiple non-empty lines and validate format with `ssh.ParseAuthorizedKey` (prevents malformed or unintended keys from landing in `authorized_keys`)
- Apply the same validation to keys returned by `ssh-add -L` so the agent path cannot bypass the gate
- Narrow `dialSSH` retry to host-key algorithm negotiation failures only; authentication failures no longer trigger a second password attempt (avoids server-side lockout and audit-log noise)

## [1.4.0] - 2026-03-10

### Added

- Auto-discover default public key when `-i` is not specified (ssh-copy-id compatible)
  - Try ssh-agent (`ssh-add -L`) first, use the first key if available
  - Fall back to newest `~/.ssh/id_*.pub` file by modification time
  - Support FIDO/U2F security key types (`sk-ssh-ed25519`, `sk-ecdsa-sha2-nistp256`)
- Display key source in output: file path or `(ssh-agent)`
- Unit tests for `parseSshAddOutput` and `findNewestPubKeyIn`

### Changed

- Replaced fixed `id_ed25519.pub` default with unified `resolveKey()` function

## [1.3.0] - 2026-03-10

### Added

- Interactive `yes/no` prompt to update `~/.ssh/known_hosts` when the remote host key has changed; approved entries are automatically appended
- Support hashed known_hosts entries (`HashKnownHosts yes`) with HMAC-SHA1 verification
- New TOFU entries preserve hashed format when existing known_hosts contains hashed entries
- Host key replacement preserves hashed format consistently across all code paths
- Unit tests for `matchHashedHost` and `hostMatchesAddr` helpers

### Changed

- Constrain `HostKeyAlgorithms` to algorithms already present in `known_hosts`; retry without the constraint when host-key algorithm negotiation fails, supporting key rotation without manual `known_hosts` edits

### Fixed

- Rewrite multi-alias `known_hosts` lines via field reconstruction instead of `strings.Replace`, eliminating false matches on Base64 content; remaining aliases are preserved when a single host entry is removed

## [1.1.2] - 2026-03-10

### Fixed

- Lower go directive to 1.26.0 for Termux compatibility (`go install` failed with Go 1.26.0)

## [1.1.1] - 2026-03-10

### Security

- Add host key verification using `~/.ssh/known_hosts` with TOFU (Trust on First Use) as default
- Add `--insecure` flag to explicitly opt-in to skip host key verification
- Replace English-named ACL principals (`SYSTEM`, `Administrators`) with well-known SIDs (`S-1-5-18`, `S-1-5-32-544`) for non-English Windows and domain environments
- Resolve user ACL entry via SID instead of `${env:USERNAME}` for domain account compatibility

### Fixed

- Admin key file detection now parses `AuthorizedKeysFile` inside `Match Group administrators` block instead of only checking block existence
- sshd_config parser now handles case-insensitive keywords and trailing comments
- TOFU prompt uses buffered line reading instead of raw terminal mode (backspace now works)

## [1.1.0] - 2026-03-10

### Added

- GitLab CI for running tests on merge requests
- GitHub Actions for automated cross-platform binary releases on tag push
- Integration tests with `//go:build integration` tag
- English README (`README.md`) with Japanese version as `README_ja.md`

### Changed

- All user-facing messages translated from Japanese to English (i18n)
- ACL ACEs unified to always grant `SYSTEM:(F)`, `Administrators:(F)`, `${env:USERNAME}:(F)` regardless of admin status

### Fixed

- Windows OpenSSH ACL compliance: apply ACL to both `.ssh` directory and key file
- Distinguish directory vs file errors on ACL failure
- Remove unreachable branch in ACL logic
- CLIXML output contamination breaking Admin/sshd_config detection (`strings.Contains` instead of exact match)
- `Select-String -SimpleMatch` with `[regex]::Escape()` causing duplicate key registration

## [1.0.0] - 2026-03-09

### Added

- Initial release
- SSH public key deployment for Windows OpenSSH servers (`ssh-copy-id` alternative)
- Password authentication support
- Administrators group detection with `administrators_authorized_keys` branching
- BOM-less UTF-8 key file writing
- Duplicate key detection
- PowerShell remote execution via `-EncodedCommand` (UTF-16LE Base64)

[Unreleased]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.4.1...HEAD
[1.4.1]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.4.0...v1.4.1
[1.4.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.3.0...v1.4.0
[1.1.2]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.1.1...v1.1.2
[1.1.1]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.1.0...v1.1.1
[1.1.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.0.0...v1.1.0
[1.0.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/tags/v1.0.0
