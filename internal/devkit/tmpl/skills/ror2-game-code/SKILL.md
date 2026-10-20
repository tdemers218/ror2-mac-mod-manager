---
name: ror2-game-code
description: Find and understand Risk of Rain 2's own code and assets before using them. Use when you need a game class, method, field, event or asset, are unsure an API exists or what it does, or hit a compile error about a missing member.
---

# Reading the game's code

Do not guess RoR2 APIs from memory. The game changes often (recent updates renamed
and obsoleted many members). Look them up in the installed game instead:

```sh
ror2 dev types CharacterBody          # find types whose name matches
ror2 dev decompile RoR2.CharacterBody # full decompiled C# of one type
ror2 dev decompile RoR2.CharacterBody | grep -n "RecalculateStats"
ror2 dev decompile --dll RoR2BepInExPack RoR2BepInExPack.GameAssetPathsBetter.RoR2_Base_Common
```

`decompile` uses ILSpy (installed automatically the first time). Read the actual
method body before hooking it: what it does, what it returns, when it runs.

Pay attention to `[Obsolete]` attributes. Example from the current game:
`Inventory.GetItemCount` is obsolete; use `GetItemCountEffective` (respects
temporary/disabled items) or `GetItemCountPermanent` (for consumption/purchases).

## Main classes

| Class | What it is |
|---|---|
| `RoR2.Run` | The current run. `Run.instance`, `Run.onRunStartGlobal`, difficulty, stage count. |
| `RoR2.Stage`, `SceneDirector`, `ClassicStageInfo` | A stage and how it spawns interactables/monsters. `Stage.onStageStartGlobal`. |
| `RoR2.CharacterMaster` | The persistent "player/AI" across stages: inventory, money, respawns. |
| `RoR2.CharacterBody` | The spawned body: stats (`damage`, `moveSpeed`, `maxHealth`…), buffs, `RecalculateStats()`. |
| `RoR2.HealthComponent` | Health, shields, `TakeDamage(DamageInfo)`, `Heal`. |
| `RoR2.Inventory` | Items and equipment on a master. |
| `RoR2.GlobalEventManager` | Global hooks: `OnCharacterDeath`, `OnHitEnemy`, `OnHitAll`, plus static events like `onCharacterDeathGlobal`. |
| `RoR2.DamageInfo`, `DamageReport` | One hit / one resolved hit (attacker, victim, damage, proc coefficient). |
| `RoR2.ItemDef`, `ItemCatalog`, `EquipmentDef`, `BuffDef` | Content definitions and lookups. |
| `RoR2.PlayerCharacterMasterController` | Marks a master as a human player. `.instances` lists players. |
| `RoR2.Chat` | `Chat.AddMessage("…")` for quick in-game debug output. |
| `EntityStates.*` | Skill and AI state machines (each skill is an EntityState subclass). |

## Hooking

Prefer hooks over patching game files:

- **On hooks** (MMHOOK, already referenced): wrap a method, call `orig` to run the
  original. `On.RoR2.CharacterBody.RecalculateStats += (orig, self) => { orig(self); … };`
  Signatures mirror the method; the decompiled code tells you the parameters.
- **IL hooks**: `IL.RoR2.X.Method += il => { var c = new ILCursor(il); … }` to edit the
  body. Needs `using MonoMod.Cil; using Mono.Cecil.Cil;`. Use only when an On hook
  cannot do it, and always check `TryGotoNext` succeeded (log and bail out if not).
- **Static events** (no hook needed): `GlobalEventManager.onCharacterDeathGlobal`,
  `Run.onRunStartGlobal`, `Stage.onStageStartGlobal`, `CharacterBody.onBodyStartGlobal`,
  `RoR2Application.onLoad`.
- Unsubscribe in `OnDestroy` only if the plugin can be unloaded; normally not needed.

## Assets (Addressables)

Load vanilla assets with typed keys from RoR2BepInExPack, never string paths:

```csharp
using RoR2BepInExPack.GameAssetPathsBetter;
using UnityEngine.AddressableAssets;

var tier = Addressables.LoadAssetAsync<ItemTierDef>(RoR2_Base_Common.Tier1Def_asset).WaitForCompletion();
```

Class names follow the asset folder (`RoR2_Base_Common`, `RoR2_DLC1_…`); members are
the file name with `.` replaced by `_`. Find one with
`ror2 dev decompile --dll RoR2BepInExPack RoR2BepInExPack.GameAssetPathsBetter.<Class>` or
`ror2 dev types --dll RoR2BepInExPack <word>`.

## Non-public members

Some fields are private (for example `ItemDef._itemTierDef`). Use Harmony's
`AccessTools.Field(typeof(T), "name").SetValue(obj, value)` (`using HarmonyLib;`).
Check the name in the decompiled code first.

## Multiplayer

The host runs the simulation. Change gameplay state only when `NetworkServer.active`.
Purely visual or UI changes run everywhere. If clients need custom data, use R2API's
Networking module rather than inventing messages.
