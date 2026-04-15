#!/usr/bin/env bash
#
# Guards what gets committed. Secrets are the leak-check.sh job. This one is
# about the repo staying small and presentable: no dependency folders, no build
# output, no giant files, nothing generated that should be rebuilt instead.
#
#     ./scripts/repo-hygiene.sh
#
# Exits non-zero if anything looks wrong.

set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 1

fail=0
report() {
  printf '\n[HYGIENE] %s\n' "$1"
  printf '%s\n' "$2" | head -20
  fail=1
}

# Anything matching these should be installed or built, never committed.
JUNK='(^|/)node_modules/|(^|/)\.next/|(^|/)vendor/|(^|/)dist/|(^|/)build/|(^|/)__pycache__/|(^|/)\.venv/|\.DS_Store$|(^|/)target/|\.tsbuildinfo$|\.log$|(^|/)coverage/'

# 1. Not tracked now.
now=$(git ls-files | grep -E "$JUNK" || true)
[ -n "$now" ] && report "generated or dependency files are tracked:" "$now"

# 2. Never tracked in any commit. Deleting them later does not shrink the repo,
#    the objects stay in history and every clone still pays for them.
ever=$(git log --all --pretty=format: --name-only | sort -u | grep -E "$JUNK" || true)
[ -n "$ever" ] && report "generated or dependency files exist somewhere in history:" "$ever"

# 3. No oversized files. go.sum and package-lock.json are legitimately large,
#    and the solver carries two binary fixtures, so the limit is generous and
#    only catches things that are truly out of place.
LIMIT=$((2 * 1024 * 1024))
big=$(git ls-files -z | xargs -0 -I{} sh -c 'test -f "{}" && printf "%s %s\n" "$(wc -c < "{}")" "{}"' 2>/dev/null \
      | awk -v lim="$LIMIT" '$1 > lim {printf "%.1f MB  %s\n", $1/1048576, $2}' || true)
[ -n "$big" ] && report "files larger than 2 MB are tracked:" "$big"

# 4. The example env files must exist, since the README tells people to copy
#    them. A rename that forgets these breaks setup for everyone.
for expected in monitor/.env.example web/.env.example; do
  git ls-files --error-unmatch "$expected" >/dev/null 2>&1 || report "missing setup file:" "$expected"
done

# 5. Nothing left half-merged.
conflicts=$(git ls-files -z | xargs -0 grep -lE '^(<{7}|={7}|>{7}) ' 2>/dev/null || true)
[ -n "$conflicts" ] && report "merge conflict markers:" "$conflicts"

if [ "$fail" -eq 0 ]; then
  echo "clean: $(git ls-files | wc -l | tr -d ' ') tracked files, $(du -sh .git | cut -f1) of git history, nothing generated committed"
fi

exit "$fail"
