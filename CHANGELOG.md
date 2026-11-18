# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html):

- **MAJOR** for incompatible changes to the CLI, exit codes or config file format
- **MINOR** for new steps, commands or settings that keep existing configs working
- **PATCH** for bug fixes

## [Unreleased]

### Fixed

- `self-update` and `version --check` no longer report a git-describe build such as `v0.4.0-3-gabc1234` as older than `v0.4.0`, so `--force` is no longer suggested for a downgrade.

## [0.5.0] - 2026-11-18

### Added

- Built-in `sysupdate` step and `sysupdate self-update` command to update Release binaries from GitHub, with checksum verification and build attestation verification through `gh` when it is installed and logged in.

## [0.4.0] - 2026-11-18

### Added

- pip step (on by default): for each pyenv Python, upgrades pip and the top-level packages installed with it. `pip.exclude` (also in Settings) lists packages to leave alone.

### Fixed

- The Cargo check no longer fails for crates installed at a prerelease version (for example one that has only alpha releases). It now compares against newer prereleases too, and the update asks cargo for `>=` the installed version.

## [0.3.0] - 2026-11-15

### Added

- First-run setup: when there is no config yet, sysupdate detects which tools are installed and lets you choose which steps to use before saving. `sysupdate setup` reopens it, and `sysupdate run` without a config says how to choose.

## [0.2.3] - 2026-11-15

### Changed

- Updated dependencies: glamour 1.0.0 (help screen rendering) and charmbracelet/x/ansi 0.11.9, plus their transitive updates.
- CI uses actions/checkout v7, actions/setup-go v7 and golangci-lint-action v9.

## [0.2.2] - 2026-11-15

### Fixed

- The Go step now reports a failed write of the downloaded archive (for example a full disk) instead of going on to verify a partial file.

## [0.2.1] - 2026-11-15

### Fixed

- CI and release builds use the newest stable Go instead of the oldest version go.mod allows, so release binaries include standard-library security fixes.

## [0.2.0] - 2026-11-15

### Added

- Leftovers found by the checks can be removed: `d` on a step in the TUI, or `sysupdate clean` (`--yes`, `--dry-run`, `--only`). Both show the commands and ask first.
- `make release` picks the next version from the commits since the last tag (Conventional Commits), dates the changelog, commits and tags. `make next-version` previews it.
- GitHub Actions: CI (vet, race tests, govulncheck, shellcheck) on every push, and a release workflow that publishes Linux binaries and changelog notes when a version tag is pushed.

### Changed

- Settings is now a single page showing every setting, saved as you change it, instead of a multi-page form.

### Fixed

- APT no longer reports phased updates as pending. The check simulates the real upgrade, and lists phased and kept-back packages as held back.
- pnpm self-update could keep an old version because pnpm answered from its metadata cache. sysupdate now passes the latest version from the registry.

## [0.1.0]

### Added

- Bubble Tea TUI: overview of all steps with installed versions, pending updates and last run; per-step selection; live run screen with progress, cancel and retry; log viewer; built-in help.
- Steps: APT, Go toolchain, gup (`go install` binaries), rustup, cargo crates, Node.js via nvm, pnpm and globals, uv and tools, pyenv, Claude Code, gh extensions, Docker images (off by default) and user-defined custom steps.
- Read-only update checks for every built-in step, and a report of leftovers worth removing (unused APT packages, leftover package configs, old nvm Node versions, untagged Docker images). Nothing is removed automatically.
- Settings and custom-step forms (Huh), YAML config at `~/.config/sysupdate/config.yaml`.
- Plain CLI: `run`, `check`, `list`, `config`, `logs`, `help`, `version`, styled with Lip Gloss.
- Run history and per-run logs in `~/.local/state/sysupdate`.

[Unreleased]: https://github.com/guiltydot/sysupdate-cli/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/guiltydot/sysupdate-cli/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/guiltydot/sysupdate-cli/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/guiltydot/sysupdate-cli/compare/v0.2.3...v0.3.0
[0.2.3]: https://github.com/guiltydot/sysupdate-cli/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/guiltydot/sysupdate-cli/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/guiltydot/sysupdate-cli/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/guiltydot/sysupdate-cli/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/guiltydot/sysupdate-cli/releases/tag/v0.1.0
