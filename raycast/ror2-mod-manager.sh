#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title RoR2 Mod Manager
# @raycast.mode silent

# Optional parameters:
# @raycast.icon ./ror2-icon.png
# @raycast.packageName Risk of Rain 2
# @raycast.argument1 { "type": "text", "placeholder": "search mods", "optional": true }

# Documentation:
# @raycast.description Open the Risk of Rain 2 mod manager in Ghostty, optionally searching Thunderstore.

# Opens a new Ghostty window running ror2. A login shell is used so your PATH
# (claude, codex, dotnet) is the same as in a normal terminal.
exec open -na Ghostty --args --title="RoR2 Mod Manager" --window-width=170 --window-height=48 \
  -e /bin/zsh -lic 'exec "$HOME/.local/bin/ror2" browse "$@"' ror2 "$@"
