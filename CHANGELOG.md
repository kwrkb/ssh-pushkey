# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- Updated dependencies: `golang.org/x/crypto` v0.55.0 → v0.56.0. Versions up to v0.55.0 are affected by GO-2026-6354 and GO-2026-6355, two denial-of-service issues in `golang.org/x/crypto/ssh` channel handling that `govulncheck` reports as reachable from this tool's `ssh.Dial` call. After the update `govulncheck ./...` reports no reachable vulnerabilities.

## [1.8.2] - 2026-08-15

### Changed

- Updated dependencies: `golang.org/x/crypto` v0.54.0 → v0.55.0.
- Added `toolchain go1.26.6` to `go.mod` so local and CI builds use Go 1.26.6, which fixes several standard-library vulnerabilities reported by `govulncheck` against 1.26.5 (`net/url`, `encoding/asn1`, `crypto/tls`, `net/http`, `encoding/xml`, `html/template`, `x/net/idna`). None were reachable from this tool's code. The `go 1.26.0` directive is unchanged, so `go install` on toolchains that cannot auto-download (e.g. Termux) keeps working.

## [1.8.1] - 2026-08-12

### Changed

- Windows distribution moved from WinGet to a [Scoop](https://scoop.sh/) bucket at [kwrkb/scoop-bucket](https://github.com/kwrkb/scoop-bucket). Install with `scoop bucket add kwrkb https://github.com/kwrkb/scoop-bucket` followed by `scoop install ssh-pushkey`. The WinGet manifest pull request opened for 1.8.0 was blocked by a Microsoft Defender false positive during `microsoft/winget-pkgs` validation. Because this tool connects over SSH, appends to `authorized_keys`, and rewrites ACLs with `icacls` — behaviour a heuristic scanner cannot distinguish from installing a backdoor — that block is likely to recur, which would make every release depend on an external review passing. A Scoop bucket is self-contained and cannot stall a release. WinGet is not published anymore.

## [1.8.0] - 2026-08-11

### Added

- WinGet distribution. Releases now generate a WinGet manifest and open a pull request against `microsoft/winget-pkgs`. Windows OpenSSH ships no `ssh-copy-id` at all, making it the one client platform with no alternative; WinGet is preinstalled on Windows 11 and, unlike a personal Scoop bucket or Homebrew tap, is searchable without adding a source first. The package installs as a portable, so the binary lands on `PATH` without an installer. **`winget install kwrkb.ssh-pushkey` does not work yet:** this release only opens the manifest pull request, and WinGet cannot find the package until Microsoft reviews and merges it. Until then, use the archives below or `go install`.

### Changed

- A successful run now prints the file the key was written to (e.g. `=> Key deployed to C:\ProgramData\ssh\administrators_authorized_keys`), and a skipped run prints where the key was already found. Previously only `--dry-run` reported the destination path, so a real run left it implicit.
- Documentation: installation now lists GitHub Releases first as the canonical distribution source, and describes GitLab Releases as a mirror. The artifact difference is stated explicitly: GitHub ships archives plus `checksums.txt` (GoReleaser), GitLab ships bare binaries without checksums.
- Documentation: the options table now documents `-n` / `--dry-run` and `--help`, which have shipped since 1.6.0 but were missing from both READMEs.

## [1.7.3] - 2026-08-11

### Changed

- Updated dependencies: `golang.org/x/crypto` v0.53.0 → v0.54.0, `golang.org/x/term` v0.44.0 → v0.45.0.

### Fixed

- Development only: `make itest-*` now resolves 1Password `op://` references in `.env.integration*` by delegating to `op run --env-file` instead of sourcing the file in the shell, which failed to parse those references. Falls back to the previous `source` behaviour when the file contains no `op://` reference.

## [1.7.2] - 2026-06-21

### Fixed

- Host-key-dependent `Match` rules (e.g. `Match Address`) are now evaluated against the real client address. The `sshd -T -C` connection spec (`host`/`addr`/`laddr`/`lport`) is derived from `SSH_CONNECTION` instead of the previously hard-coded `localhost`/`127.0.0.1`, so the tool reads the effective `AuthorizedKeysFile` for the actual source address when choosing where to deploy the key. Verified end-to-end on a non-loopback client (see `docs/match-address-verification.md`).

## [1.7.1] - 2026-06-21

### Changed

- GitHub releases are now produced by [GoReleaser](https://goreleaser.com/). Each platform ships as an archive (`ssh-pushkey_<os>_<arch>.tar.gz`, `.zip` on Windows) alongside a `checksums.txt`, instead of bare binaries. Release notes still come from `CHANGELOG.md`. GitHub is the canonical distribution source; GitLab releases are unchanged.

### Fixed

- Appending a key to an existing `authorized_keys` / `administrators_authorized_keys` that does **not** end in a newline no longer concatenates the new key onto the last existing line (which corrupted both keys and broke authentication). The deploy script now checks the file's final byte and inserts a separating newline when needed, matching `ssh-copy-id`'s behavior. Files this tool wrote were already safe; the bug affected files created manually or by other tools (common for `administrators_authorized_keys`).
- Adding a new host entry to `known_hosts` (TOFU first-connect) applies the same trailing-newline guard, so a `known_hosts` file that does not end in a newline no longer has its last entry corrupted by the appended line.

## [1.7.0] - 2026-06-20

### Changed

- Duplicate-key detection now compares only the key blob (type + base64), ignoring the comment field. A key that is already present under a different comment is now correctly recognized as a duplicate and skipped, instead of being appended again.
- Module path moved to `github.com/kwrkb/ssh-pushkey` to match the primary repository. Install with `go install github.com/kwrkb/ssh-pushkey@latest` (the old `gitlab.com/...` path is no longer used). Binary downloads are unaffected.

### Fixed

- `known_hosts` updates (host-key replacement) are now written atomically via a temp file in the same directory followed by `rename`, the same approach OpenSSH itself uses. This prevents corruption from a partial write or concurrent access; readers always see either the complete old or the complete new file.

## [1.6.0] - 2026-06-20

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

[Unreleased]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.8.2...HEAD
[1.8.2]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.8.1...v1.8.2
[1.8.1]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.8.0...v1.8.1
[1.8.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.7.3...v1.8.0
[1.7.3]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.7.2...v1.7.3
[1.7.2]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.7.1...v1.7.2
[1.7.1]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.7.0...v1.7.1
[1.7.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.6.0...v1.7.0
[1.6.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.5.1...v1.6.0
[1.5.1]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.5.0...v1.5.1
[1.5.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.4.1...v1.5.0
[1.4.1]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.4.0...v1.4.1
[1.4.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.3.0...v1.4.0
[1.1.2]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.1.1...v1.1.2
[1.1.1]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.1.0...v1.1.1
[1.1.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.0.0...v1.1.0
[1.0.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/tags/v1.0.0
