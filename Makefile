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

# make release V=0.2.0 checks that the tree is clean and CHANGELOG.md has a
# section for the version, then creates the annotated tag v0.2.0.
# Pushing stays manual.
release: check
	@echo "$(V)" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' || { echo "usage: make release V=MAJOR.MINOR.PATCH"; exit 2; }
	@test -z "$$(git status --porcelain)" || { echo "working tree is not clean"; exit 1; }
	@! git rev-parse -q --verify "refs/tags/v$(V)" >/dev/null || { echo "tag v$(V) already exists"; exit 1; }
	@grep -q '^## \[$(V)\]' CHANGELOG.md || { echo "CHANGELOG.md has no '## [$(V)]' section"; exit 1; }
	git tag -a "v$(V)" -m "sysupdate v$(V)"
	@echo "Tagged v$(V). Push with: git push origin main v$(V)"

clean:
	go clean
	$(RM) sysupdate

.PHONY: build install test vet check release clean
