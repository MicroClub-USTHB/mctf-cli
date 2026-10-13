#!/usr/bin/env bash
set -euo pipefail

# scripts/coverage-summary.sh
# Parses Go coverprofile and generates a Markdown summary table for GitHub Actions step summary
# and terminal output.

COV_FILE="${1:-coverage.out}"

if [ ! -f "$COV_FILE" ]; then
  echo "Error: coverage profile '$COV_FILE' not found" >&2
  exit 1
fi

TOTAL=$(go tool cover -func="$COV_FILE" | grep "^total:" | awk '{print $NF}')

PKG_ROWS=$(awk '
NR > 1 {
  split($1, parts, "/")
  pkg = ""
  for (i = 4; i < length(parts); i++) {
    if (pkg != "") pkg = pkg "/" parts[i]
    else pkg = parts[i]
  }
  if (pkg == "") {
    if (length(parts) == 4) pkg = "."
    else pkg = parts[4]
  }
  stmts = $2 + 0
  count = $3 + 0
  pkg_stmts[pkg] += stmts
  if (count > 0) {
    pkg_covered[pkg] += stmts
  }
}
END {
  for (p in pkg_stmts) {
    if (pkg_stmts[p] == 0) continue
    pct = (pkg_covered[p] / pkg_stmts[p]) * 100
    icon = "🔴"
    if (pct >= 70.0) icon = "🟢"
    else if (pct >= 30.0) icon = "🟡"
    printf "| `%s` | %.1f%% | %s |\n", p, pct, icon
  }
}
' "$COV_FILE" | sort)

SUMMARY_MD=$(cat <<EOF
## 📊 Go Test Coverage Report

**Overall Statement Coverage:** \`$TOTAL\`

| Package | Coverage | Status |
| :--- | :---: | :---: |
$PKG_ROWS

*Legend: 🟢 &ge; 70% &nbsp;|&nbsp; 🟡 30%–69% &nbsp;|&nbsp; 🔴 &lt; 30%*
EOF
)

# Output to stdout
echo "$SUMMARY_MD"

# If running in GitHub Actions, append to GITHUB_STEP_SUMMARY
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  echo "" >> "$GITHUB_STEP_SUMMARY"
  echo "$SUMMARY_MD" >> "$GITHUB_STEP_SUMMARY"
fi
