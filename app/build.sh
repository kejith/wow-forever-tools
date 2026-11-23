#!/usr/bin/env bash
# Builds the Windows app from app/: build/bin/WoW Forever Tools.exe (the window and, as `bridge <command>`,
# every calculation of sim/), and with --installer also build/bin/WoW-Forever-Tools-Setup-<version>.exe.
# Runs in Git Bash on Windows and in WSL or Linux (cross-compiled; Wails needs no cgo on Windows). The
# installer needs makensis (NSIS 3): on Windows from https://nsis.sourceforge.io or `choco install nsis`, in
# WSL `sudo apt install nsis`.
#
#   ./build.sh               the exe as the installer ships it (-X main.packaged=1: it looks for its data next
#                            to itself and updates itself)
#   ./build.sh --source      an exe that behaves like a start from source: it finds the checkout above it
#   ./build.sh --installer   the exe and the installer
set -euo pipefail
cd "$(dirname "$0")"

version=$(tr -d '[:space:]' < VERSION)
mode=packaged
installer=false
for arg in "$@"; do
  case "$arg" in
    --source) mode=source ;;
    --installer) installer=true ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

go run ./build/winres -version "$version"
ldflags="-H windowsgui -s -w"
[ "$mode" = packaged ] && ldflags="$ldflags -X main.packaged=1"
mkdir -p build/bin
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags desktop,production -trimpath -ldflags "$ldflags" \
  -o "build/bin/WoW Forever Tools.exe" .
echo "built build/bin/WoW Forever Tools.exe ($version, $mode)"

if [ "$installer" = true ]; then
  makensis=$(command -v makensis || true)
  for candidate in "/c/Program Files (x86)/NSIS/makensis.exe" "/c/Program Files/NSIS/makensis.exe"; do
    [ -z "$makensis" ] && [ -x "$candidate" ] && makensis=$candidate
  done
  if [ -z "$makensis" ]; then
    echo "makensis not found: install NSIS 3" >&2
    exit 1
  fi
  "$makensis" -V2 -DVERSION="$version" -DOUTFILE="../build/bin/WoW-Forever-Tools-Setup-$version.exe" installer/installer.nsi
  echo "built build/bin/WoW-Forever-Tools-Setup-$version.exe"
fi
