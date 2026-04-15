#!/usr/bin/env bash
#
# Scans the working tree and the full git history for things that should not be
# public. There is no CI wired up, so nothing runs this for you. Run it by hand
# before pushing:
#
#     ./scripts/leak-check.sh
#
# It exits non-zero if it finds anything. No dependencies beyond git and grep,
# on purpose, so it keeps working without any setup.

set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 1

fail=0
report() {
  printf '\n[LEAK] %s\n' "$1"
  printf '%s\n' "$2" | head -20
  fail=1
}

# Everything the scan should ignore: placeholder values in the example env
# files, the fake connection string used in docs and error messages, and the
# fallback the dashboard uses when it has no credentials.
ALLOW='user:pass@host|your-project\.supabase\.co|unconfigured\.supabase\.co|YOUR_|example\.com|placeholder'

# ---------------------------------------------------------------------------
# 1. Files that should never be tracked at all.
# ---------------------------------------------------------------------------
bad_files=$(git ls-files | grep -Ei '(^|/)\.env$|(^|/)\.env\..*local|(^|/)proxies\.txt$|\.pem$|\.pfx$|\.p12$|(^|/)id_(rsa|dsa|ed25519)$|\.keystore$' || true)
[ -n "$bad_files" ] && report "files that should never be committed:" "$bad_files"

# ---------------------------------------------------------------------------
# 2. Secret shaped strings, in the working tree and in every commit ever made.
#    History matters as much as the current tree: deleting a key in a later
#    commit does not remove it from the repo.
# ---------------------------------------------------------------------------
# "regex%%%description". The separator is %%% and not ":" because half these
# patterns contain a colon.
PATTERNS=(
  'sk-ant-[A-Za-z0-9_-]{20,}%%%Anthropic API key'
  'sk-[A-Za-z0-9]{32,}%%%generic provider API key'
  'discord(app)?\.com/api/webhooks/[0-9]{17,}/[A-Za-z0-9_-]{50,}%%%Discord webhook URL'
  '[MNO][A-Za-z0-9_-]{23,}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{25,}%%%Discord user token'
  'eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.%%%JSON Web Token (Supabase keys look like this)'
  'postgres(ql)?://[^:/[:space:]]+:[^@[:space:]]+@%%%Postgres URL with real credentials'
  'https://[a-z0-9]{20}\.supabase\.co%%%real Supabase project URL'
  '[-]{5}BEGIN [A-Z ]*PRIVATE KEY[-]{5}%%%private key'
  'AKIA[0-9A-Z]{16}%%%AWS access key id'
  'ghp_[A-Za-z0-9]{36}%%%GitHub personal access token'
  'glpat-[A-Za-z0-9_-]{20}%%%GitLab token'
  '(/Users/|/home/)[a-z][a-z0-9_.-]+%%%an absolute path off someone machine'
  '\.\./\.\./%%%a path reaching outside the repo, which usually means a layout nobody else has'
)

echo "scanning working tree..."
tree_dump=$(git ls-files -z | xargs -0 grep -InEH --binary-files=without-match '' 2>/dev/null)

echo "scanning full history (every commit, every branch)..."
history_dump=$(git log --all -p --format='commit %H by %ae' 2>/dev/null)

sep='%%%'
for entry in "${PATTERNS[@]}"; do
  pattern="${entry%%"$sep"*}"
  label="${entry#*"$sep"}"

  hits=$(printf '%s\n' "$tree_dump" | grep -EI "$pattern" 2>/dev/null | grep -Ev "$ALLOW" || true)
  [ -n "$hits" ] && report "$label found in the working tree:" "$hits"

  hits=$(printf '%s\n' "$history_dump" | grep -EI "$pattern" 2>/dev/null | grep -Ev "$ALLOW" || true)
  [ -n "$hits" ] && report "$label found in git history:" "$hits"
done

# ---------------------------------------------------------------------------
# 3. The example env files must stay empty or hold obvious placeholders. This
#    catches the easy mistake of filling one in and committing it.
# ---------------------------------------------------------------------------
while IFS= read -r example; do
  [ -f "$example" ] || continue
  # Numbers and file paths are fine as defaults. Anything else with a value in
  # it is suspicious.
  filled=$(grep -E '^[A-Z_][A-Z0-9_]*=.+' "$example" \
    | grep -Ev "$ALLOW" \
    | grep -Ev '=[0-9]+$' \
    | grep -Ev '=\.{0,2}/' || true)
  [ -n "$filled" ] && report "$example has real looking values, not placeholders:" "$filled"
done < <(git ls-files | grep -E '\.env\.example$')

# ---------------------------------------------------------------------------
# 4. Personal identifiers. The history was rewritten to strip these, so if any
#    come back it means a rewrite regressed or a commit slipped through with
#    the wrong git identity configured.
# ---------------------------------------------------------------------------
identity=$(git log --all --format='%an <%ae>%n%cn <%ce>' | sort -u | grep -vi 'RightNxw' || true)
[ -n "$identity" ] && report "commits authored by an unexpected identity:" "$identity"

# ---------------------------------------------------------------------------
# 5. Code comments. Secrets get caught by the patterns above, but comments leak
#    a different way: a personal note, an internal hostname, a private repo
#    link, a real account name someone forgot to strip. Nothing here is
#    automatically a secret, so treat hits as "go read this line".
# ---------------------------------------------------------------------------
comment_lines=$(git ls-files -z \
  | xargs -0 grep -InEH --binary-files=without-match '^[[:space:]]*(//|#|/\*|\*)' 2>/dev/null \
  | grep -Ev '^(web/package-lock\.json|monitor/go\.sum)' || true)

COMMENT_PATTERNS=(
  '\b(localhost|127\.0\.0\.1|192\.168\.|10\.[0-9]+\.)[:0-9]*%%%local or internal address'
  '(TODO|FIXME|HACK|XXX)[^a-z]*(remove|delete|strip|hide|secret|key|token|password|before[[:space:]]+(push|commit|release))%%%a note about removing something'
  'https?://[a-z0-9.-]*\.(internal|local|corp|lan)\b%%%internal hostname'
  'github\.com/[A-Za-z0-9_-]+/(private|internal|secret)[A-Za-z0-9_-]*%%%link to a private looking repo'
  '\b(my|our)[[:space:]]+(password|api[[:space:]]?key|token|account|burner)\b%%%a comment referring to real credentials'
  '\b(ported|copied|adapted|taken)[[:space:]]+from\b.*/%%%a reference to another project or file path'
  '(^|[^a-z])(deps|vendor|third_party)/[a-z0-9_-]+-(fork|local|private)%%%a path that only exists on the author machine'
)

for entry in "${COMMENT_PATTERNS[@]}"; do
  pattern="${entry%%"$sep"*}"
  label="${entry#*"$sep"}"
  hits=$(printf '%s\n' "$comment_lines" | grep -EIi "$pattern" 2>/dev/null | grep -Ev "$ALLOW" || true)
  [ -n "$hits" ] && report "comment worth reviewing, $label:" "$hits"
done

if [ "$fail" -eq 0 ]; then
  echo
  echo "clean: no secrets in the working tree or in $(git rev-list --all --count) commits of history"
fi

exit "$fail"
