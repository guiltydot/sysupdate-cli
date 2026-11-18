# Plan: self-update from GitHub Releases

Status: proposed. Target release: v0.5.0 (minor: a new step, a new command, and existing configs keep working).

## Goal

sysupdate updates itself the same way it updates everything else. A built-in step,
`sysupdate`, is on by default and runs last. It checks GitHub for a newer release,
downloads the binary for this machine, verifies it, and swaps it in. There is also a
`sysupdate self-update` command for doing this on its own.

## What already exists

- `.github/workflows/release.yml` publishes `sysupdate_<tag>_linux_{amd64,arm64}.tar.gz`
  and `checksums.txt` (from `sha256sum ./*.tar.gz`, so its lines look like
  `<sha>  ./sysupdate_v0.4.0_linux_amd64.tar.gz`) whenever a `v*.*.*` tag is pushed.
- Each archive holds `sysupdate_<tag>_linux_<arch>/sysupdate` plus the docs.
- `main.version` is set by `-ldflags -X` (`make build`, the release workflow) or taken from
  the module version in build info (`go install ...@vX`). It is `dev` otherwise.
- The Go step (`internal/steps/golang.go`) already has a download helper that checks
  SHA256 and shows progress, and the steps package has `compareVersions` / `isPrerelease`.

## When the step does nothing

| Situation | How it is detected | What it reports |
|---|---|---|
| A local build: `dev`, `-dirty`, or commits after a tag (`v0.4.0-3-gabc1234`) | the version string | "local build, not updated". The step's Available reports false, so it is skipped. |
| Installed with `go install` | `debug.ReadBuildInfo` gives a module version and the binary sits in `$GOBIN` / `$(go env GOPATH)/bin` | "installed with go install: the gup step updates it" |
| The binary's directory is not writable (e.g. `/usr/local/bin`) | try creating a temp file there | an error saying where it is and to reinstall to `~/.local/bin` or run `sudo sysupdate self-update`. No automatic sudo. |
| The latest release is not newer | `compareVersions(latest, current) <= 0` | up to date. This also means it never downgrades. |
| No asset for `runtime.GOARCH` | look at the asset names | an error naming the architecture |

## Design

### 1. Version available to the steps package
New `internal/buildinfo` package with `var Version string`. `main.init` sets it after
working out `version`. Steps read `buildinfo.Version` and never `main`.

### 2. Release lookup (`internal/selfupdate/github.go`)
- `GET https://api.github.com/repos/guiltydot/sysupdate-cli/releases/latest`
  (this endpoint already leaves out drafts and prereleases). Send
  `Accept: application/vnd.github+json` and `User-Agent: sysupdate-cli`.
- If `GITHUB_TOKEN` or `GH_TOKEN` is set, send it as a bearer token. Without a token the
  limit is 60 requests an hour per IP, which a check per launch stays well under.
- Decode `tag_name`, `html_url` and `assets[].{name, browser_download_url, size}`.
- Keep the base URL in a package variable so tests can point it at `httptest`.

### 3. Check
`Check` returns `vX → vY` with the release URL in Details. Network errors are reported as
`check failed`, like the other steps.

### 4. Update (`internal/selfupdate/apply.go`)
1. Work out the target: `os.Executable()`, then `filepath.EvalSymlinks`.
2. Download `checksums.txt` and parse it. Strip a leading `./`, and fail if the archive's
   line is missing.
3. Download the archive into a temp dir, hashing it as it is written. Reject a size
   mismatch with the asset metadata and a hash mismatch with `checksums.txt`. Reuse or
   factor out the Go step's `download` helper.
4. Extract only `sysupdate_<tag>_linux_<arch>/sysupdate`, which must be a regular file
   under 64 MiB, into a temp file **in the target's directory**, so the rename stays on
   one filesystem. Set mode 0755.
5. Smoke test: run `<tmp> version` with a 10 s timeout and require that the output contains
   the new tag.
6. `os.Rename(tmp, target)`. This is atomic, and the running process keeps its old inode.
   Remove the temp file on every failure path, so the old binary is never touched unless
   everything above passed.
7. Print `sysupdate vX → vY. Restart sysupdate to use the new version.` In the TUI the
   same line shows in the run's output, and the summary marks the step as updated.

### 5. Wiring
- New step `selfStep()` (ID `sysupdate`, name "sysupdate", DefaultOn true, no sudo),
  appended **last** in `steps.All` so that a bad new binary cannot affect the current run.
- New command: `sysupdate self-update [--check] [--force]`. `--check` only reports.
  `--force` allows a local build to be replaced by the latest release.
- `sysupdate version` gains `--check`, which prints the latest release next to the
  installed one.
- No new config. It is turned off like any other step (`enabled.sysupdate: false`).

### 6. Release-side hardening (separate commit, same release)
- Add `actions/attest-build-provenance` to `release.yml` for the archives and
  `checksums.txt` (needs `id-token: write` and `attestations: write`).
- During the update, if `gh` is on PATH, run
  `gh attestation verify <archive> --repo guiltydot/sysupdate-cli` and fail if it fails.
  Without `gh`, log that only the checksum was checked.
- Reason: `checksums.txt` comes from the same release, so it catches corruption but not a
  tampered release. The attestation ties the archive to this repository's workflow.

## Tests

- `internal/selfupdate`: an `httptest` server serves a fake release JSON, a tarball built in
  the test, and `checksums.txt`. The cases:
  - the happy path replaces a fake target binary in `t.TempDir()`
  - a checksum mismatch, a missing checksum line, a wrong archive layout, an oversized
    entry, and a failing smoke test each leave the target unchanged
  - the target directory is not writable
  - the latest release is the same version or older (no downgrade)
- Version classification table: `dev`, `v0.4.0`, `v0.4.0-dirty`, `v0.4.0-3-gabc1234`,
  `v0.5.0-rc.1`.
- `checksums.txt` parsing, with and without `./`.

## Docs

README (steps table, the Install note that binaries from Releases update themselves),
HELP.md (step table, check list, the "When the step does nothing" table in short),
the usage text in `main.go`, CHANGELOG `[Unreleased]`, and SECURITY.md (what is verified).

## Order of work

1. `internal/buildinfo` and the version classification, with tests.
2. `internal/selfupdate`: the lookup, download and verify, extract, smoke test and swap,
   with tests.
3. The step and the `self-update` command. Docs.
4. The attestation in `release.yml`, plus `gh attestation verify` when `gh` is available.
5. `make check`, then a manual test: install v0.4.0 from Releases to `~/.local/bin`, cut
   v0.5.0, and run `sysupdate self-update`.
6. `make release` (v0.5.0).

## Open questions

- Should the TUI offer to restart itself (`syscall.Exec` of the new binary) after a
  successful self-update, instead of asking the user to restart? Proposed answer: not in
  v0.5.0.
- Should `sysupdate` run first instead of last, so the rest of the run uses the new code?
  Proposed answer: last. It is safer, and the next run picks up the new version.
