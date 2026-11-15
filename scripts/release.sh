#!/usr/bin/env bash
# Cuts a release: decides the next semantic version from the commits since the
# last tag, moves CHANGELOG.md's [Unreleased] section under that version,
# commits, and creates an annotated tag. It never pushes.
#
#   scripts/release.sh             decide the bump from the commits
#   scripts/release.sh minor       force a bump (major, minor or patch)
#   scripts/release.sh --dry-run   only print what would happen
#
# How the bump is decided (Conventional Commits, https://www.conventionalcommits.org):
#   "type!:" or a "BREAKING CHANGE:" footer   -> major (minor while the version is 0.x)
#   "feat:"                                    -> minor
#   "fix:", "perf:", "refactor:", "revert:"    -> patch
#   "docs:", "test:", "ci:", "build:", "chore:", "style:" alone -> no release
# Commits without a type are read by their first word: Add/Allow/Support/Show/
# Make/Remove/... count as a feature, Fix/Correct as a fix, anything else as a patch.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

dry=false
force=""
for arg in "$@"; do
	case "$arg" in
	--dry-run | -n) dry=true ;;
	major | minor | patch) force="$arg" ;;
	*)
		echo "usage: $0 [--dry-run] [major|minor|patch]" >&2
		exit 2
		;;
	esac
done

last=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null || true)
range=${last:+$last..}HEAD
if [ -n "$last" ]; then
	cur=${last#v}
else
	cur=0.0.0
fi
IFS=. read -r major minor patch <<<"${cur%%-*}"

# 0 none, 1 patch, 2 minor, 3 major
level=0
bump_to() { if [ "$1" -gt "$level" ]; then level=$1; fi; }

while IFS= read -r -d $'\x1e' commit; do
	commit=${commit#$'\n'}
	subject=${commit%%$'\n'*}
	[ -z "$subject" ] && continue
	case "$subject" in "chore(release):"*) continue ;; esac
	if printf '%s\n' "$commit" | grep -Eq '^BREAKING[ -]CHANGE:' ||
		printf '%s\n' "$subject" | grep -Eq '^[a-z]+(\([^)]*\))?!:'; then
		bump_to 3
		continue
	fi
	type=$(printf '%s\n' "$subject" | sed -nE 's/^([a-z]+)(\([^)]*\))?:.*/\1/p')
	if [ -n "$type" ]; then
		case "$type" in
		feat) bump_to 2 ;;
		fix | perf | refactor | revert) bump_to 1 ;;
		esac
		continue
	fi
	case "${subject%% *}" in
	Add | Adds | Allow | Support | Show | Make | Remove | Introduce | Implement | Report) bump_to 2 ;;
	*) bump_to 1 ;;
	esac
done < <(git log --format='%B%x1e' "$range")

case "$force" in
major) level=3 ;;
minor) level=2 ;;
patch) level=1 ;;
esac

if [ "$level" -eq 0 ]; then
	echo "No releasable changes since ${last:-the first commit}."
	exit 0
fi
# Before 1.0, breaking changes bump the minor version (semver item 4).
if [ "$level" -eq 3 ] && [ "$major" -eq 0 ] && [ "$force" != major ]; then
	level=2
fi
case "$level" in
3) next="$((major + 1)).0.0" ;;
2) next="$major.$((minor + 1)).0" ;;
1) next="$major.$minor.$((patch + 1))" ;;
esac
kind=([1]=patch [2]=minor [3]=major)

echo "Last release: ${last:-none}"
echo "Next release: v$next (${kind[$level]})"
git log --format='  %h %s' "$range" | grep -v 'chore(release):' || true

if ! awk '/^## \[Unreleased\]/{f=1;next} /^## \[/{f=0} f && NF' CHANGELOG.md | grep -q .; then
	echo "CHANGELOG.md has nothing under [Unreleased]; add the changes there first." >&2
	exit 1
fi
$dry && exit 0

if [ -n "$(git status --porcelain)" ]; then
	echo "Working tree is not clean; commit or stash first." >&2
	exit 1
fi
if git rev-parse -q --verify "refs/tags/v$next" >/dev/null; then
	echo "Tag v$next already exists." >&2
	exit 1
fi

today=$(date +%Y-%m-%d)
repo=https://github.com/guiltydot/sysupdate-cli
awk -v v="$next" -v d="$today" -v repo="$repo" -v prev="$last" '
	/^## \[Unreleased\]/ { print; print ""; print "## [" v "] - " d; next }
	/^\[Unreleased\]:/ {
		print "[Unreleased]: " repo "/compare/v" v "...HEAD"
		if (prev != "") print "[" v "]: " repo "/compare/" prev "...v" v
		else print "[" v "]: " repo "/releases/tag/v" v
		next
	}
	{ print }
' CHANGELOG.md >CHANGELOG.md.tmp
mv CHANGELOG.md.tmp CHANGELOG.md

git add CHANGELOG.md
git commit -q -m "chore(release): v$next"
git tag -a "v$next" -m "sysupdate v$next"
echo "Committed and tagged v$next. Push with: git push origin main v$next"
