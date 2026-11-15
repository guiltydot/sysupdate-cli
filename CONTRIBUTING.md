# Contributing

Thanks for your interest in sysupdate! Bug reports, ideas and pull requests are all welcome.

## Reporting bugs and ideas

Open an issue with:

- what you ran (`sysupdate run --only node`, a key in the TUI, …)
- what you expected and what happened
- the output of `sysupdate version` and your Ubuntu version
- for a failed step, the relevant part of `sysupdate logs`. Check it for anything private before pasting.

Security problems should **not** go in a public issue. See [SECURITY.md](SECURITY.md).

If you want to add a new updater step, it helps to open an issue first so we can agree on how it should behave.

## Development setup

You need Go 1.26 or newer, plus [golangci-lint](https://golangci-lint.run) and [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) for the full checks.

```bash
git clone https://github.com/guiltydot/sysupdate-cli.git
cd sysupdate-cli
make build          # ./sysupdate
make check          # vet, tests, golangci-lint, govulncheck
```

To try the TUI without touching your real config or history, point it at a scratch config and state directory:

```bash
SYSUPDATE_CONFIG=/tmp/su/config.yaml XDG_STATE_HOME=/tmp/su/state ./sysupdate
```

Steps really run when you start a run, and some use sudo, so use `sysupdate run --dry-run` to see the commands first.

## Project layout

| Path | What's there |
|---|---|
| `cmd/sysupdate` | The command: subcommands and plain CLI output |
| `internal/steps` | Each updater step: how it detects, checks, plans and cleans up |
| `internal/runner` | Runs steps one after another, streams output, writes logs |
| `internal/tui` | The Bubble Tea interface |
| `internal/config`, `internal/state` | The YAML config and run history |
| `HELP.md` | The user guide, built into the binary (`?` in the TUI) |

### Adding a step

1. Write a `fooStep()` function in `internal/steps` returning a `*Step`. Look at `claudeStep` for a small example and `cargoStep` for a larger one.
   - `Available` reports whether the tool is installed.
   - `Version` returns a short version string for the overview.
   - `Check` looks for updates **without changing anything**.
   - `Plan` returns the commands to run. Use `Exec` for a plain command (with `sudo` set when needed) and `Shell` when a shell is required.
   - Optionally, `Check` can report leftovers worth removing in `CheckResult.Cleanup`. They are only removed when the user asks.
2. Add it to `All()` in `internal/steps/step.go`, in the order it should run.
3. Document it in `HELP.md` and the steps table in `README.md`.
4. Add tests for any output you parse (see `apt_test.go`).

## Pull requests

- Keep each pull request to one change, and run `make check` before pushing. CI runs the same checks.
- Write commit messages as [Conventional Commits](https://www.conventionalcommits.org): `feat: …` for new behaviour, `fix: …` for bug fixes, `docs: …`, `refactor: …`, `test: …`, `ci: …` and so on. Add `!` (as in `feat!: …`) for changes that break existing configs or commands. Releases are versioned automatically from these messages.
- Add a line describing user-visible changes under `## [Unreleased]` in [CHANGELOG.md](CHANGELOG.md).
- Update `HELP.md` and `README.md` when behaviour or keys change.

The maintainer cuts releases; you don't need to change version numbers.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
