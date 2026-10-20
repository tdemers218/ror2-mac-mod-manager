---
name: ror2-thunderstore-release
description: Prepare this mod for release on Thunderstore - version bump, manifest, README, changelog, icon, and the upload zip. Use when the person wants to share, publish, release or package the mod.
---

# Releasing on Thunderstore

Thunderstore is where Risk of Rain 2 mods are published (thunderstore.io/c/riskofrain2).
Players install them with r2modman or this mod manager.

## Checklist

1. **Version.** Thunderstore needs `major.minor.patch` and never accepts the same
   version twice. Bump `version_number` in `manifest.json`, `PluginVersion` in
   `Plugin.cs` and `<Version>` in the csproj together. Bug fix: patch. New feature:
   minor. Breaking change (config, save, behavior): major.
2. **manifest.json.**
   - `name`: letters, digits, underscores only. Must match across versions.
   - `description`: at most 250 characters, one sentence about what it does.
   - `website_url`: a repo link or `""`.
   - `dependencies`: every mod needed at runtime as `Owner-Name-x.y.z`, including
     `bbepis-BepInExPack-…`. Use current versions (`ror2 info <mod>`).
3. **README.md.** What the mod does, how to configure it, compatibility notes,
   multiplayer requirements (does everyone need it, or only the host?). Rendered as
   markdown on the mod page.
4. **CHANGELOG.md.** Add a section for the new version at the top.
5. **icon.png.** Exactly 256×256 PNG. A placeholder was generated; ask the person
   if they want to supply their own.
6. **Build the zip:** `ror2 dev package` → `build/{{.Author}}-{{.Name}}-<version>.zip`
   containing `manifest.json`, `icon.png`, `README.md`, `CHANGELOG.md` and
   `plugins/{{.Name}}/{{.Name}}.dll`. It fails if required pieces are missing.
7. **Test the release build** with `ror2 dev deploy`, launch, check `ror2 dev log`.

## Uploading

Uploading needs the person's Thunderstore account (thunderstore.io, sign in, Upload,
pick a team, choose the zip, select community "Risk of Rain 2" and categories).
Do not try to upload for them; give them the zip path and the steps.
