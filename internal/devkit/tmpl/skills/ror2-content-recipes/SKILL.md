---
name: ror2-content-recipes
description: Recipes for adding things to Risk of Rain 2 with R2API - items, buffs, stat changes, equipment, text, and player-facing settings. Use when the mod adds new content or changes stats, and before adding any R2API dependency.
---

# Adding content

R2API is the shared modding library. It is split into modules; depend only on the
ones you use. For each module:

1. `ror2 install RiskofThunder-R2API_<Module>` (puts it in the game so you can build).
2. Add `<Reference Include="$(BepInExDir)/plugins/**/R2API.<Module>.dll" />` to the csproj.
3. Add `[BepInDependency(<Module>API.PluginGUID)]` on the plugin class.
4. Add `RiskofThunder-R2API_<Module>-<version>` to `manifest.json` dependencies
   (`ror2 info RiskofThunder-R2API_<Module>` shows the current version).

Common modules: `Items` (ItemAPI), `Language` (LanguageAPI), `RecalculateStats`
(RecalculateStatsAPI), `ContentManagement` (ContentAddition), `Prefab` (PrefabAPI),
`DamageType`, `Dot`, `Networking`, `Sound`, `Elites`, `Director` (spawn cards),
`Skins`, `Loadout`, `Colors`.

## Stat changes (no hook needed)

```csharp
RecalculateStatsAPI.GetStatCoefficients += (body, args) =>
{
    if (!body.inventory) return;
    int n = body.inventory.GetItemCountEffective(myItem);
    args.damageMultAdd += 0.10f * n;   // +10% damage per stack
};
```

Useful `args` fields: `baseHealthAdd`, `healthMultAdd`, `baseShieldAdd`, `shieldMultAdd`,
`baseRegenAdd`, `regenMultAdd`, `baseMoveSpeedAdd`, `moveSpeedMultAdd`,
`moveSpeedReductionMultAdd`, `sprintSpeedAdd`, `baseJumpPowerAdd`, `baseDamageAdd`,
`damageMultAdd`, `baseAttackSpeedAdd`, `attackSpeedMultAdd`, `critAdd`,
`critDamageMultAdd`, `armorAdd`, `cooldownMultAdd`, `cooldownReductionAdd`,
`primary/secondary/utility/specialCooldownMultAdd`, `bleedChanceAdd`, `luckAdd`,
`baseCurseAdd`, `levelFlatAdd`. `…MultAdd` values add up (0.1 = +10%).
Call `body.RecalculateStats()` (or `body.MarkAllStatsDirty()`) after changing what
feeds into them outside of item pickups.

## Items

See `Plugin.cs` of the item template:

- `ItemDef` via `ScriptableObject.CreateInstance<ItemDef>()`; set `name`, the four
  tokens (`nameToken`, `pickupToken`, `descriptionToken`, `loreToken`), `tags`.
- Tier: load an `ItemTierDef` (`RoR2_Base_Common.Tier1Def_asset` white,
  `Tier2Def_asset` green, `Tier3Def_asset` red, `BossTierDef_asset`,
  `LunarTierDef_asset`; check exact names with `ror2 dev decompile --dll RoR2BepInExPack
  RoR2BepInExPack.GameAssetPathsBetter.RoR2_Base_Common`) and set the private
  `_itemTierDef` field with `AccessTools.Field`.
- `ItemAPI.Add(new CustomItem(itemDef, new ItemDisplayRuleDict(null)))`. Display
  rules place a model on characters; empty is fine at first.
- Count with `inventory.GetItemCountEffective(itemDef)`.
- Effects usually go in `RecalculateStatsAPI` (stats) or a hook like
  `On.RoR2.GlobalEventManager.OnHitEnemy` / `OnCharacterDeath` (procs).
- Item description text uses style tags: `<style=cIsDamage>`, `cIsHealing`,
  `cIsUtility`, `cIsHealth`, `cStack` (for "(+x per stack)"), `cDeath`, `cShrine`.

## Equipment

`EquipmentDef` + `ItemAPI.Add(new CustomEquipment(def, new ItemDisplayRuleDict(null)))`.
Activation: hook `On.RoR2.EquipmentSlot.PerformEquipmentAction` and return `true`
when the equipment is yours and it fired (check `self.equipmentIndex`/`def` first,
otherwise call and return `orig`). Set `cooldown`, `canDrop`, `enigmaCompatible`.

## Buffs

```csharp
var buff = ScriptableObject.CreateInstance<BuffDef>();
buff.name = "MyBuff"; buff.canStack = true; buff.isDebuff = false;
buff.buffColor = Color.cyan;
buff.iconSprite = /* Addressables sprite */;
ContentAddition.AddBuffDef(buff);   // R2API.ContentManagement
// apply (host only): body.AddTimedBuff(buff, 5f);  read: body.GetBuffCount(buff)
```

Other registrations in `ContentAddition`: `AddBody`, `AddMaster`, `AddProjectile`,
`AddEffect`, `AddNetworkedObject`, `AddSkillDef`, `AddSkillFamily`, `AddEliteDef`,
`AddSurvivorDef`, `AddArtifactDef`, `AddUnlockableDef`, `AddSceneDef`.

## Text

`LanguageAPI.Add("TOKEN", "English text")`. Prefix tokens with the mod name in
capitals to avoid clashes.

## Settings players can change

- `Config.Bind(section, key, default, description)` writes
  `BepInEx/config/{{.GUID}}.cfg`. Read `.Value` when needed (not once in Awake) so
  edits apply.
- For an in-game settings menu, Risk Of Options is the standard: depend on
  `Rune580-Risk_Of_Options`, reference `RiskOfOptions.dll`, and call
  `ModSettingsManager.AddOption(new CheckBoxOption(entry))` /
  `SliderOption` / `IntSliderOption`. Make it a soft dependency
  (`BepInDependency(..., SoftDependency)` + a check) if the mod should work without it.

## Art

Real models and icons come from a Unity AssetBundle built with the game's Unity
version (Unity 2021.3). That needs the Unity editor and is a bigger step: get the
mechanics working with placeholder vanilla assets first, then tell the person what
art is needed.
