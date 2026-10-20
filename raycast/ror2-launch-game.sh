#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title Launch Risk of Rain 2 (modded)
# @raycast.mode compact

# Optional parameters:
# @raycast.icon ./ror2-icon.png
# @raycast.packageName Risk of Rain 2

# Documentation:
# @raycast.description Start Risk of Rain 2 with your mods through Steam in the Sikarugir wrapper.

out=$(/bin/zsh -lc '"$HOME/.local/bin/ror2" launch' 2>&1)
if [ $? -eq 0 ]; then
  echo "Starting Risk of Rain 2…"
else
  echo "$out" | sed 's/\x1b\[[0-9;]*m//g' | grep -E "✖|error" | head -1
  exit 1
fi
