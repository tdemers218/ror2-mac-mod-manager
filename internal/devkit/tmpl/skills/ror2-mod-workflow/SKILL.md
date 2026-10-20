---
name: ror2-mod-workflow
description: Build, install, launch and debug this Risk of Rain 2 mod on this Mac. Use for any change to the mod's code, whenever you need to test it in the game, read its log output, or figure out why it did not load.
---

# Working on {{.Name}}

This is a BepInEx plugin for Risk of Rain 2 (C#, `netstandard2.1`). The game is a
Windows game running on macOS through a Wine wrapper (Sikarugir), with Steam inside
it. You cannot attach a debugger; you test by building, installing into the game,
launching it, and reading BepInEx's log.

Everything goes through the `ror2` command (the mod manager). Run it from this folder.

## The loop

1. Edit code in `Plugin.cs` (add more `.cs` files freely; they are all compiled).
2. `ror2 dev build` compiles. Fix every error before going on. Warnings about
   Unity assembly versions (MSB3277) are expected and harmless.
3. `ror2 dev deploy` builds and copies the DLL into the game at
   `BepInEx/plugins/dev-{{.Author}}-{{.Name}}/`. Never copy files into the game by hand.
4. `ror2 launch` starts the game through Steam (only if the person asked you to
   test in-game or agreed to it; it opens a window on their screen).
5. After they play (or right away for load errors): `ror2 dev log` shows this
   mod's lines from `BepInEx/LogOutput.log`, plus errors and exceptions.
   `ror2 dev log --all -n 200` shows everything.
6. `ror2 doctor` reports missing dependencies and plugins that failed to load.

The game must be closed for changes to take effect; BepInEx loads plugins once at
startup. Ask the person to close and relaunch, or to tell you what they saw.

## Reading the log

- `[Info   :{{.Name}}] {{.Name}} {{.Version}} loaded` means the plugin started.
- `Could not load [{{.Name}} …] because it has missing dependencies: X` means a
  `[BepInDependency]` is not installed. Install it with `ror2 install <mod>`.
- An exception with a stack trace through your namespace is your bug. Hooks that
  throw can break the game method they wrap, so read these carefully.
- `InvalidKeyException … Key=…` means an Addressables key is wrong. Use the
  constants in `RoR2BepInExPack.GameAssetPathsBetter` (see the ror2-game-code skill).

## Project layout

| File | Purpose |
|---|---|
| `{{.Name}}.csproj` | Build settings. References the game's DLLs from your install. Add a `<Reference>` here when you need another assembly. |
| `Plugin.cs` | Entry point: `[BepInPlugin]` class, `Awake()` runs once at game start. |
| `manifest.json` | Thunderstore metadata: name, version, description, dependencies. |
| `README.md`, `CHANGELOG.md`, `icon.png` | Shown on Thunderstore. Icon must be 256×256 PNG. |
| `ror2mod.json` | Lets the mod manager recognize this project. |

## Rules

- Keep `PluginVersion` in `Plugin.cs`, `<Version>` in the csproj and
  `version_number` in `manifest.json` identical. Bump all three together.
- Every mod you depend on at runtime needs both a `[BepInDependency("guid")]`
  attribute and an entry in `manifest.json` `dependencies`
  (`Owner-Name-x.y.z`, as on Thunderstore). `ror2 info <mod>` shows the full name.
- Only reference DLLs that ship with the game, BepInEx, or a dependency listed in
  the manifest. Anything else will be missing on players' machines.
- Gameplay changes must only run on the host: check `NetworkServer.active`
  (namespace `UnityEngine.Networking`) before changing health, items, gold, spawns.
- Commit to git after each working step so changes are easy to undo.

## Other commands

- `ror2 dev undeploy` removes the dev copy from the game.
- `ror2 dev package` builds a Thunderstore-ready zip in `build/` (see the
  ror2-thunderstore-release skill).
- `ror2 dev types <pattern>` and `ror2 dev decompile <Type>` read the game's code
  (see the ror2-game-code skill).
- `ror2 search <words>` / `ror2 info <mod>` look things up on Thunderstore.
