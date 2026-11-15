# Versions follow Semantic Versioning (https://semver.org) and come from git
# tags of the form vMAJOR.MINOR.PATCH. Between tags, `git describe` gives
# e.g. v0.2.0-3-gabc1234 (3 commits after v0.2.0), plus -dirty for local edits.
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --match 'v[0-9]*' --dirty 2>/dev/null || echo v0.0.0-dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o sysupdate ./cmd/sysupdate

install: build
	install -Dm755 sysupdate $(PREFIX)/bin/sysupdate

test:
	go test ./...

vet:
	go vet ./...

# Runs vet, tests and govulncheck (when installed).
check: vet test
	@if command -v govulncheck >/dev/null; then govulncheck ./...; else echo "govulncheck not installed, skipping"; fi

# make release picks the next version from the commits since the last tag
# (see scripts/release.sh), dates the CHANGELOG section, commits and tags.
# BUMP=major|minor|patch overrides the decision. Pushing stays manual.
release: check
	scripts/release.sh $(BUMP)

# Shows what make release would do, without changing anything.
next-version:
	@scripts/release.sh --dry-run $(BUMP)

clean:
	go clean
	$(RM) sysupdate

.PHONY: build install test vet check release next-version clean
