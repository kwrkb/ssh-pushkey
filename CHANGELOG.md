# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

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

[Unreleased]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.1.2...HEAD
[1.1.2]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.1.1...v1.1.2
[1.1.1]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.1.0...v1.1.1
[1.1.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/compare/v1.0.0...v1.1.0
[1.0.0]: https://gitlab.com/kwrkb/ssh-pushkey/-/tags/v1.0.0
