#!/bin/sh
# E2E stand-in for the third-party fence binary. Mimics its CLI shape
# (fence -m --fence-log-file <log> --settings <json> /bin/sh <cmd.sh>)
# and its process topology: the child runs in a NEW process group, outside
# the pane group, exactly the split that makes real fenced disposal leak.
# setsid gives the child its own session+group, a stricter split than real
# fence; if the reaper handles this, it handles the real thing.
while [ $# -gt 0 ]; do
  case "$1" in
    -m) shift ;;
    --fence-log-file|--settings) shift 2 ;;
    *) break ;;
  esac
done
# -w is load-bearing: without it setsid forks and the parent (this stub, the
# tmux pane process) exits immediately, so tmux destroys the session at spawn.
# With -w the stub survives as the pane, like real fence's -m monitor role.
exec setsid -w "$@"
