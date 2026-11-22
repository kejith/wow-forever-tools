#!/usr/bin/env bash
# Compares a script's text output with its Go port until the scripts are removed (notes/go-migration.md,
# phase 5): runs `uv run scripts/<name>.py <args>` and `wowtools <name> <args>` and diffs stdout, stderr's
# last line and the exit code.
#
#   scripts/clidiff.sh writs --wow scripts/fixtures/forever --char Brakk --lang de
#
# WOWTOOLS (default: sim/wowtools, built first), WOW_TOOLS_CACHE (default: the main checkout's scripts/cache)
# and SIMFOREVER_BIN (default: the main checkout's sim/simforever) can be set.
set -u
here=$(cd "$(dirname "$0")/.." && pwd)
name=$1
shift
export WOW_TOOLS_CACHE=${WOW_TOOLS_CACHE:-$HOME/work/wow/scripts/cache}
export SIMFOREVER_BIN=${SIMFOREVER_BIN:-$HOME/work/wow/sim/simforever}
bin=${WOWTOOLS:-$here/sim/wowtools}
if [ -z "${WOWTOOLS:-}" ]; then
  (cd "$here/sim" && go build -o wowtools ./cmd/wowtools) || exit 1
fi
tmp=$(mktemp -d)
(cd "$here" && uv run --quiet "scripts/$name.py" "$@" > "$tmp/py.out" 2> "$tmp/py.err"); py=$?
(cd "$here" && "$bin" "$name" "$@" > "$tmp/go.out" 2> "$tmp/go.err"); go=$?
status=0
if ! cmp -s "$tmp/py.out" "$tmp/go.out"; then
  echo "DIFFERENT stdout: $name $*"
  diff "$tmp/py.out" "$tmp/go.out" | head -${DIFF_LINES:-20}
  status=1
fi
pyerr=$(tail -1 "$tmp/py.err"); goerr=$(tail -1 "$tmp/go.err")
if [ "$py" != "$go" ] || { [ "$py" != 0 ] && [ "$pyerr" != "$goerr" ]; }; then
  echo "DIFFERENT exit: python $py ($pyerr), go $go ($goerr): $name $*"
  status=1
fi
[ $status = 0 ] && echo "equal ($(wc -l < "$tmp/py.out") lines, exit $py): $name $*"
rm -rf "$tmp"
exit $status
