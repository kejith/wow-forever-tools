#!/bin/bash -l
# Starts Claude Remote Control for this repo in the tmux session "wow-rc" (if it is not running yet) and
# keeps this process alive as long as that session exists, so WSL does not shut the distro down.
#
# From Windows (PowerShell), visible:  wsl.exe -d orebound -- bash -l ~/work/wow/scripts/wsl-remote-control.sh
# Look at it / answer prompts:          wsl.exe -d orebound -- tmux attach -t wow-rc   (detach: Ctrl+B, D)
# Stop:                                 wsl.exe -d orebound -- tmux kill-session -t wow-rc
set -u
REPO="$HOME/work/wow"
SESSION="wow-rc"
MODE="${WOW_RC_PERMISSION_MODE:-auto}"

if ! tmux has-session -t "$SESSION" 2>/dev/null; then
  tmux new-session -d -s "$SESSION" -x 200 -y 50 -c "$REPO" \
    "claude remote-control --name 'WoW Forever (orebound)' --permission-mode $MODE; echo 'remote-control ended'; sleep 600"
  echo "Remote Control started in tmux session $SESSION ($REPO)."
fi
while tmux has-session -t "$SESSION" 2>/dev/null; do sleep 60; done
