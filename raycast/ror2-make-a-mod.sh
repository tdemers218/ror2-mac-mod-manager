#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title RoR2 Make a Mod
# @raycast.mode silent

# Optional parameters:
# @raycast.icon ./ror2-icon.png
# @raycast.packageName Risk of Rain 2

# Documentation:
# @raycast.description Open the RoR2 mod manager on the Create tab to build a mod with Claude Code or Codex.

exec open -na Ghostty --args --title="RoR2 Mod Manager" --window-width=170 --window-height=48 \
  -e /bin/zsh -lic 'exec "$HOME/.local/bin/ror2" --tab create'
