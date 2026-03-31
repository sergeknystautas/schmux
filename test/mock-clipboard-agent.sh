#!/bin/bash
# Mock agent that reads clipboard images when it receives Ctrl+V (0x16).
# Simulates how Claude Code checks the X11 clipboard for image data.
# Does NOT hardcode DISPLAY — relies on the tmux environment, same as Claude Code.
echo "agent ready"
while IFS= read -r -n1 char; do
  if [[ "$char" == $'\x16' ]]; then
    size=$(xclip -selection clipboard -t image/png -o 2>/dev/null | wc -c)
    echo "clipboard-image-received:${size}bytes"
  fi
done
