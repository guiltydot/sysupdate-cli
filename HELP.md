# sysupdate

sysupdate updates the whole WSL/Ubuntu development environment from one place: system packages, language toolchains, global tools and your own commands. It shows what is installed and what is outdated, lets you pick which updaters run, runs them one after another with live output, and keeps a log and history of every run.

A failing step never aborts the others (unless you turn on *Stop on first failure*). Nothing runs until you start a run.

## What gets updated

| Step | What it does |
|---|---|
| **Ubuntu packages** (`apt`, sudo) | `apt-get update`, then `full-upgrade` (or `upgrade`), and optionally `autoremove --purge` and `autoclean`. It runs non-interactively and keeps your existing config files. |
| **Go** (`go`, sudo) | Downloads the latest stable Go from go.dev, verifies its SHA256, and replaces the install root (`/usr/local/go`). If the download fails, the old install stays untouched. |
| **Go binaries** (`gup`) | `gup update`: rebuilds the binaries in `$GOBIN` that `go install` put there when a newer version exists or they were built with an older Go. It runs right after the Go step, so a Go upgrade also rebuilds your tools. Binaries in `gup.exclude` are left alone. |
| **Rust toolchain** (`rustup`) | `rustup update`: the installed toolchains and rustup itself. |
| **Cargo binaries** (`cargo`) | `cargo install --locked` for each crate installed from crates.io. Cargo rebuilds only the outdated ones. Crates installed from git or a path, and any in `cargo.exclude`, are skipped. |
| **Node.js** (`node`) | `nvm install` for the newest release of your current major (or the latest LTS), carrying global packages over. It then makes that release the nvm default and the Node that later steps use. |
| **pnpm + globals** (`pnpm`) | `pnpm self-update`, then `pnpm update -g`. |
| **uv + tools** (`uv`) | `uv self update`, then `uv tool upgrade --all`. |
| **pyenv + plugins** (`pyenv`) | `pyenv update` if the plugin is present, otherwise `git pull --ff-only` in `~/.pyenv`. |
| **pip packages** (`pip`) | For each Python pyenv installed: upgrades pip, then the packages you installed with pip. Only top-level packages are upgraded; pip updates their dependencies when the new version needs it. Packages in `pip.exclude` are skipped. Virtualenvs and the system Python (managed by APT) are left alone. |
| **Claude Code** (`claude`) | `claude update`. |
| **gh extensions** (`gh-ext`) | `gh extension upgrade --all`. gh itself is an APT package, so the APT step updates it. |
| **Docker images** (`docker`, off by default) | `docker pull` for each image you pulled from a registry. Locally built images and those in `docker.exclude` are skipped. |
| **Custom steps** | Your own shell commands, run after the built-in steps. Add them on the Custom steps screen (`m`) or in the config file. |

Steps whose tool isn't installed are shown as *not installed* and skipped.

## Checking for updates

Each built-in step can look for pending updates without changing anything:

- **APT**: a simulated `apt-get upgrade` (or `full-upgrade`) on the cached package lists, so it reflects your last `apt update`. Phased updates, which Ubuntu releases to machines gradually, are shown as held back rather than pending, because apt will not install them yet.
- **Go**: the go.dev release feed
- **Go binaries**: `gup check`
- **Rust**: `rustup check`
- **Cargo**: the crates.io index
- **Node**: nodejs.org
- **pnpm**: the npm registry and `pnpm outdated -g`
- **uv**: `uv self update --dry-run` and `uv tool list --outdated`
- **pyenv**: `git fetch`
- **pip**: `pip list --outdated --not-required` for each pyenv Python
- **Claude Code**: the npm registry
- **gh extensions**: `gh extension upgrade --all --dry-run`
- **Docker**: compares each image's digest with its registry (`docker buildx imagetools inspect`)

Checks run when the app opens (you can turn this off in Settings) and again after each run.

### Things that could be removed

Checks also look for leftovers. They are listed with the command that removes them, and are only removed when you ask:

- **APT**: packages `apt autoremove` would remove (simulated with `apt-get -s`), and removed packages whose config files are still on disk (`apt purge`)
- **Node**: older Node versions nvm left behind after an upgrade (`nvm uninstall`)
- **Docker**: untagged images left behind by newer pulls (`docker image prune`)

They appear under "Could be removed" in the detail pane (the Updates column shows "· N to clean") and at the end of `sysupdate check`. To remove them, press `d` on the step in the TUI, or run `sysupdate clean`. Both show the exact commands and ask before running them. APT's `autoremove --purge` after every upgrade is off by default; turn it on in Settings if you want it to happen automatically.

## Screens and keys

### Setup

The first time you open sysupdate (when there is no config file yet), it starts with setup. It detects which tools are installed on this machine and pre-selects the installed ones that are on by default. Tools that aren't installed are shown greyed out. Pick what you want sysupdate to update, then press `enter` to save. Run `sysupdate setup` to come back to this screen later.

| Key | Action |
|---|---|
| `↑` `↓` / `j` `k` | Move |
| `space` / `x` | Select or deselect the step |
| `a` | Select all / none of the installed steps |
| `enter` | Save and go to the overview |
| `esc` | Skip: keep the default selection (setup won't open again) |
| `q` | Quit without saving (setup opens again next time) |

### Overview

The table lists every step with its checkbox, installed version, pending updates and last run. `#` marks steps that use sudo. Below the table you see the highlighted step's description, the exact commands it will run, and the list of pending updates.

| Key | Action |
|---|---|
| `↑` `↓` / `j` `k` | Move |
| `space` / `x` | Enable or disable the step |
| `a` | Select all / none (e.g. after `u`, press `a` to go back to everything) |
| `u` | Select only the steps with pending updates |
| `enter` / `r` | Run all enabled steps |
| `o` | Run only the highlighted step |
| `c` / `C` | Check all steps / check the highlighted step |
| `d` | Remove the highlighted step's leftovers (asks first) |
| `s` | Settings |
| `m` | Custom steps |
| `e` | Edit the config file in `$EDITOR` |
| `l` | Run logs |
| `?` | This help |
| `q` | Quit |

### Run

The run screen shows a progress bar, a status for each step (✓ ok, ✗ failed, ↷ skipped, ■ cancelled) and that step's live output.

| Key | Action |
|---|---|
| `↑` `↓` | Show another step's output |
| `f` | Follow the running step again |
| `pgup` `pgdn` / mouse wheel | Scroll the output |
| `X` / `ctrl+c` | Cancel. The running command and its children are stopped. |
| `r` | After the run: retry the failed steps |
| `l` | After the run: open the logs |
| `enter` / `esc` | After the run: back to the overview |

### Settings

Every setting on one page, grouped into APT, toolchains (the Node version to track, whether global npm packages are carried over, the Go install root), exclusions (crates, gup binaries and Docker images to leave alone) and general options (*Stop on first failure*, *Check for updates on start*, the number of logs kept). The line under the list explains the highlighted setting. Changes are saved as soon as you make them.

| Key | Action |
|---|---|
| `↑` `↓` / `j` `k` / `tab` | Move |
| `space` / `enter` | Toggle an on/off setting |
| `←` `→` / `space` | Change a choice |
| `enter` | Edit a text value; `enter` again saves it, `esc` cancels the edit |
| `esc` / `q` / `s` | Back to the overview |

### Custom steps

Lists your own steps in the order they run.

| Key | Action |
|---|---|
| `a` | Add a step |
| `enter` | Edit the highlighted step |
| `d` | Delete it (asks first) |
| `K` / `J` | Move it up or down |
| `esc` | Back to the overview |

A step has a name, an ID (made from the name if you leave it empty), and a command run with `bash -c`. Optionally it can also run with sudo, name a program it requires (the step shows as *not installed* without it), give a command that prints its version, and carry a description.

You can still edit everything by hand in the config file with `e` from the overview. The app reloads it when the editor closes.

### Logs

Every run writes a full log. Use `←` and `→` to move between older and newer logs, and `g` / `G` to jump to the top or bottom.

## sudo

If a selected step needs sudo, the app hands the terminal to `sudo -v` once, before the run starts, and keeps the sudo timestamp fresh while the run is going. Commands use `sudo -n`, so they fail instead of hanging if sudo is no longer authenticated.

## Command line

Without arguments, `sysupdate` opens this interface. For scripts and cron:

```
sysupdate setup               choose which steps to use (opens automatically on first run)
sysupdate run                 run the enabled steps with plain output
sysupdate run --only apt,go   run just these step IDs
sysupdate run --skip cargo    skip these step IDs
sysupdate run --all           run every installed step, ignoring the config
sysupdate run --dry-run       print the commands without running them
sysupdate check               show which steps have updates
sysupdate clean               remove leftovers the checks found (asks first; --yes, --dry-run, --only)
sysupdate list                list IDs, enabled state, versions and last runs
sysupdate config [path|init|edit]
sysupdate logs [-n N] [-list]
sysupdate help
```

`run` exits with 0 if everything succeeded, 1 if a step failed, 2 on a usage or config error, and 130 if it was cancelled.

## Configuration

The config file is `~/.config/sysupdate/config.yaml` (override it with `SYSUPDATE_CONFIG`). It is written the first time you change a setting, or by `sysupdate config init`.

```yaml
enabled:              # step ID -> on/off; missing IDs default to on
  cargo: false
apt:
  full_upgrade: true
  autoremove: false
  autoclean: false
go:
  root: /usr/local/go
cargo:
  exclude: [some-crate]
gup:
  exclude: [some-binary]
pip:
  exclude: [some-package]  # pip itself is always upgraded
docker:
  exclude: [postgres]     # repo or repo:tag; the docker step is off by default
node:
  channel: current-major   # or: lts
  reinstall_packages: true
general:
  stop_on_failure: false
  check_on_start: true
  keep_logs: 20
custom:
  - id: flatpak
    name: Flatpak
    command: flatpak update -y
    requires: flatpak        # skipped as "not installed" if missing
    version: flatpak --version
  - id: snap
    name: Snaps
    command: snap refresh
    sudo: true
```

## Files

- `~/.config/sysupdate/config.yaml`: settings
- `~/.local/state/sysupdate/state.json`: last result and duration of each step
- `~/.local/state/sysupdate/logs/`: one log per run

## Restart required

If an update leaves `/var/run/reboot-required` behind, the app tells you. To restart the WSL distribution, run `wsl --shutdown` from Windows and reopen the shell.
