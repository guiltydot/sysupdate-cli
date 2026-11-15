# Security policy

## Supported versions

Only the latest release gets fixes. Update with `go install github.com/guiltydot/sysupdate-cli/cmd/sysupdate@latest` or download the newest binary from the Releases page.

## Reporting a vulnerability

Please report security problems privately through GitHub: open the repository's **Security** tab and choose **Report a vulnerability**. Don't open a public issue for them.

Include what you found, how to reproduce it and which version you used. You'll get a reply as soon as possible, and a fix will be released as a patch version with the details in the changelog.

## What sysupdate does with privileges

sysupdate runs package managers and, for the APT and Go steps, `sudo -n` after a single `sudo -v` prompt. Custom steps run the commands from your own config file. It downloads Go only from go.dev and checks the published SHA-256 before installing. Config, state and run logs are created readable only by you.
