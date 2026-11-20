# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Gear upgrades from the auction house for a character (default: Affliction warlock).

Rating: one weight per stat like Pawn (1 point = 1 shadow spell power), but derived from the character (hit
only up to the cap, crit by talents) and always calculated for the whole set. Plus the best package for the
budget (knapsack over all slots, with ring pairs and main hand + off hand against a two-hander).

Reads
  * the Auctionator scan (lowest buyout, like scripts/writs.py),
  * from WoWMCP.lua the equipped gear (per character) and the addon's item export
    (account-wide, WoWMCP_Items: stats and tooltips of every equippable item in the Auctionator scans).

Steps: scan the auction house, close it (the addon reads new items by itself) or type "/wowmcp items",
wait for the message, then /reload. After that:
  uv run scripts/upgrades.py --char Malgruk

Works with an English and a German client: stats come from the game's locale-independent keys, the few
tooltip lines that are read are matched in both languages, talents by spell ID.
"""

from __future__ import annotations

import argparse
import datetime
import glob
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import i18n  # noqa: E402
import simengine  # noqa: E402  (optional Go combat engine)
import writs  # noqa: E402  (load_prices, DEFAULT_WOW)
from i18n import mark, tr  # noqa: E402

# Rating in the style of Pawn: every stat gets a weight, the unit is 1 point of shadow spell power.
# Unlike a fixed Pawn scale, hit and crit depend on the character (build_scale), and the whole set is
# always rated (set_value). That way hit only counts up to the cap.
PROFILES = {
    # target: how many levels the enemies are above you, aff_share: share of the damage from Affliction
    # spells (DoTs, drains).
    # mana_share: 1% more pool (mana + health usable through Life Tap) counts like mana_share % more damage.
    # survival_share: 1% more health additionally counts like survival_share % more damage (survival,
    # less downtime). Both are judgement values (chosen so that a level 46 warlock lands near the older
    # fixed weights); build_scale derives intellect and stamina from them with level, stats and talents.
    # weights: fixed weights; stamina/intellect only when the export has no health and mana values.
    "leveling": dict(label=mark("Leveling"), target=2, aff_share=0.75, mana_share=0.10, survival_share=0.05,
                     weights={"stamina": 0.5, "intellect": 0.35, "spirit": 0.25, "mp5": 0.7, "hp5": 0.15,
                              "armor": 0.01, "wand_dps": 0.6}),
    "dungeon": dict(label=mark("Dungeon/raid"), target=3, aff_share=0.75, mana_share=0.06, survival_share=0.02,
                    weights={"stamina": 0.25, "intellect": 0.25, "spirit": 0.05, "mp5": 0.3, "hp5": 0.0,
                             "armor": 0.0, "wand_dps": 0.1}),
}
BASE_SPELL_CRIT = 1.7  # spell crit without intellect (warlock); only for exports without critFromIntellect (addon < 1.3)
MANA_PER_INT = 15  # MANA_PER_INTELLECT in the game
HP_PER_STAMINA = 10  # UnitHPPerStamina in the game
IMPROVED_LIFE_TAP = 0.1  # Improved Life Tap: +10% mana per rank (Classic value, not checked in Forever)
# Talents by spell ID, so the client language does not matter.
SUPPRESSION, IMPROVED_LIFE_TAP_ID, PANDEMIC = 18174, 18182, 427712
# Chance of spells to miss enemies 0 to 3 levels above you. Source: Forever's own character pane
# (Blizzard_UIPanels_Game/Camelot/PaperDollFrameStats.lua, spellMissChances).
SPELL_MISS = {0: 4.0, 1: 5.0, 2: 6.0, 3: 17.0}
MIN_MISS = 1.0  # as in Classic: 1% miss always remains
SUPPRESSION_PER_RANK = 2.0  # Suppression: hit chance for Affliction spells per rank (Classic value)
BASE_PER_LEVEL = 13  # base damage / spell power coefficient of warlock spells: about 13 per level in Classic
CRIT_BONUS = 0.5  # spell crits deal 150% damage
DEFAULT_RATINGS = {"hit": 10.0, "crit": 14.0}  # rating per 1%, measured again from the tooltips of the export
SP_KEYS = ("spell_power", "spell_damage_done", "shadow_damage_done")
LINEAR_KEYS = {"stamina": "stamina", "intellect": "intellect", "spirit": "spirit", "mana_regeneration": "mp5",
               "health_regen": "hp5", "armor": "armor"}
# Tooltip lines the scripts read, for an English and a German client.
PCT_LINE = {
    "hit": re.compile(r"(?:Improves your chance to hit by|Verbessert Eure Trefferchance um) ([\d.,]+)\s*%"),
    "crit": re.compile(r"(?:Improves your chance to get a critical strike by|Erhöht Eure kritische Trefferchance um)"
                       r" ([\d.,]+)\s*%"),
}
EQUIP_PREFIX = ("Equip:", "Anlegen:")
USE_PREFIX = ("Use:", "Benutzen:")
CLASSES_PREFIX = ("Classes:", "Klassen:")
WARLOCK_NAMES = ("Warlock", "Hexenmeister", "Hexenmeisterin")
# "Equip:" lines that only spell out a value from `stats`. Everything else counts as a special effect.
STAT_LINE = re.compile(
    r"^(?:Equip: (?:Increases (?!damage done to ).*(?:damage|healing).*by up to \d+|\+\d+ Attack Power"
    r"|Improves your chance to hit by|Improves your chance to get a critical strike by|Your spells pierce \d+"
    r"|Decreases the magical resistances of your spell targets by \d+"
    r"|Restores \d+ (?:mana|health) per \d+ sec|Increased Defense \+\d+)"
    r"|Anlegen: (?:Erhöht (?!.*Schaden gegen ).*(?:Schaden|Heilung).*um bis zu \d+|\+\d+ Angriffskraft"
    r"|Verbessert Eure Trefferchance um|Erhöht Eure kritische Trefferchance um|Eure Zauber durchdringen \d+"
    r"|Stellt alle \d+ Sek\. \d+ (?:Mana|Gesundheit) wieder her|Verteidigung \+\d+))")
LIFE_TAP_TEXT = re.compile(r"([\d.,]+)\s*(?:Health|Gesundheit)\D+?([\d.,]+)\s*Mana")
SKILL_REQ = re.compile(r"^(?:Requires|Benötigt) (.+?) \((\d+)\)$")

SLOT_OF = {  # equipLoc -> inventory slots
    "INVTYPE_HEAD": [1], "INVTYPE_NECK": [2], "INVTYPE_SHOULDER": [3], "INVTYPE_CHEST": [5], "INVTYPE_ROBE": [5],
    "INVTYPE_WAIST": [6], "INVTYPE_LEGS": [7], "INVTYPE_FEET": [8], "INVTYPE_WRIST": [9], "INVTYPE_HAND": [10],
    "INVTYPE_FINGER": [11, 12], "INVTYPE_TRINKET": [13, 14], "INVTYPE_CLOAK": [15],
    "INVTYPE_WEAPON": [16], "INVTYPE_WEAPONMAINHAND": [16], "INVTYPE_HOLDABLE": [17],
    "INVTYPE_RANGED": [18], "INVTYPE_RANGEDRIGHT": [18], "INVTYPE_2HWEAPON": [16, 17],
}
SLOT_NAME = {1: mark("Head"), 2: mark("Neck"), 3: mark("Shoulder"), 5: mark("Chest"), 6: mark("Waist"),
             7: mark("Legs"), 8: mark("Feet"), 9: mark("Wrist"), 10: mark("Hands"), 11: mark("Finger"),
             12: mark("Finger"), 13: mark("Trinket"), 14: mark("Trinket"), 15: mark("Back"),
             16: mark("Main Hand"), 17: mark("Off Hand"), 18: mark("Wand")}
USABLE_WEAPONS = {7, 10, 15, 19}  # one-handed sword, staff, dagger, wand
NEW_ID = 200000  # item IDs above this exist only in Forever
PAIRS = {11: (11, 12), 12: (11, 12), 13: (13, 14), 14: (13, 14)}  # slots that exist twice


def slot_name(slot: int) -> str:
    return tr(SLOT_NAME[slot])


def number(text: str) -> float:
    """A whole number from a tooltip, with the thousands separator of either language (1,234 or 1.234)."""
    return float(re.sub(r"[.,]", "", text))


def stat_key(k: str) -> str:
    return "armor" if k == "RESISTANCE0_NAME" else k.removeprefix("ITEM_MOD_").removesuffix("_SHORT").lower()


def lua_to_py(x):
    from lupa.lua54 import lua_type
    if lua_type(x) != "table":
        return x
    keys = list(x.keys())
    if keys and all(isinstance(k, int) for k in keys) and sorted(keys) == list(range(1, len(keys) + 1)):
        return [lua_to_py(x[k]) for k in sorted(keys)]
    return {k: lua_to_py(x[k]) for k in keys}


def load_sv(path: str, name: str):
    """Runs a SavedVariables file in a sandboxed Lua and returns one global as Python data."""
    from lupa.lua54 import LuaRuntime
    lua = LuaRuntime(register_eval=False, register_builtins=False)
    for g in ("os", "io", "package", "require", "dofile", "loadfile", "debug"):
        lua.execute(f"{g} = nil")
    lua.execute(open(path, encoding="utf-8", errors="replace").read())
    return lua_to_py(lua.globals()[name])


def load_char(path: str) -> dict:
    return load_sv(path, "WoWMCP_Char") or {}


def learn_ratings(items: dict) -> dict:
    """Rating per 1 % hit/crit, measured from items whose tooltip spells out the percentage."""
    out = dict(DEFAULT_RATINGS)
    for kind, pat in PCT_LINE.items():
        ratios = []
        for it in items.values():
            rating = (it.get("stats") or {}).get(f"ITEM_MOD_{kind.upper()}_RATING_SHORT")
            pct = sum(float(m.group(1).replace(",", ".")) for t in it.get("tooltip") or [] for m in [pat.search(t)] if m)
            if rating and pct:
                ratios.append(rating / pct)
        if ratios:
            out[kind] = sorted(ratios)[len(ratios) // 2]
    return out


def norm_stats(rec: dict | None, ranged: bool = False, ratings: dict | None = None) -> dict:
    """An item's stats in the units of the scale: sp, stamina, intellect, spirit, mp5, hp5, armor, wand_dps and
    hit/crit in percent. Everything the scale ignores (healing, other schools, melee stats) is dropped."""
    ratings = ratings or DEFAULT_RATINGS
    rec = rec or {}
    raw = {stat_key(k): val for k, val in (rec.get("stats") or {}).items() if isinstance(val, (int, float))}
    n = {"sp": sum(raw.get(k, 0) for k in SP_KEYS)}
    for k, name in LINEAR_KEYS.items():
        n[name] = raw.get(k, 0)
    if ranged:
        n["wand_dps"] = raw.get("damage_per_second", 0)
    for kind, pat in PCT_LINE.items():
        rating = raw.get(f"{kind}_rating", 0)
        if rating:
            pct = sum(float(m.group(1).replace(",", ".")) for t in rec.get("tooltip") or [] for m in [pat.search(t)] if m)
            n[kind] = pct or rating / ratings[kind]
    return {k: val for k, val in n.items() if val}


def add_stats(total: dict, n: dict, sign: int = 1) -> None:
    for k, val in n.items():
        total[k] = total.get(k, 0) + sign * val


def crit_per_intellect(st: dict, gear_crit: float = 0.0) -> tuple[float | None, str]:
    """Spell crit in percent per point of intellect at the character's level, and where the number comes from."""
    intellect = st.get("intellect") or 0
    if intellect <= 0:
        return None, ""
    if st.get("critFromIntellect") is not None:
        return st["critFromIntellect"] / intellect, tr("from the game")
    crit = (st.get("spellCrit") or {}).get("shadow")
    if crit is None:
        return None, ""
    return (max(0.0, (crit - BASE_SPELL_CRIT - gear_crit) / intellect),
            tr("estimated: spell crit − {base:g}%", base=BASE_SPELL_CRIT))


def life_tap_ratio(st: dict, level: int, ilt_rank: int) -> tuple[float, str]:
    """Mana per point of health from Life Tap, with Improved Life Tap, and a note on the source."""
    lt = st.get("lifeTap")
    text = (lt or {}).get("text") or ""
    m = LIFE_TAP_TEXT.search(text)
    if m and number(m.group(1)) > 0:
        ratio = number(m.group(2)) / number(m.group(1))
        source = tr("from the spell text")
    elif lt is None and "hpPerStamina" in st:  # addon 1.3 exports and knows no Life Tap
        return 0.0, tr("Life Tap not known")
    elif level < 6:
        return 0.0, tr("Life Tap only from level 6")
    else:
        ratio, source = 1.0, tr("assumed 1:1")
    if ilt_rank:
        ratio *= 1 + IMPROVED_LIFE_TAP * ilt_rank
        source += tr(", Improved Life Tap {rank}/2", rank=ilt_rank)
    return ratio, source


def build_scale(char: dict, profile: str = "", gear_hit: float = 0.0, gear_crit: float = 0.0) -> dict:
    """Weights for this character, in points of shadow spell power.

    Hit and crit are derived instead of fixed: 1 % more damage is worth (base damage / coefficient + spell power)
    / 100 spell power, hit counts only up to the cap against the profile's enemies, crit only for the share of
    damage that can crit. Intellect and stamina follow from what one point gives at this level (mana, crit from
    intellect, health, Life Tap) and the profile's mana_share/survival_share. Spirit and regeneration are
    judgement values per profile."""
    ch = char.get("character") or {}
    st = ch.get("stats") or {}
    level = ch.get("level") or 60
    profile = profile if profile in PROFILES else ("leveling" if level < 60 else "dungeon")
    p = PROFILES[profile]
    talents = {t.get("spellID"): t.get("rank") or 0 for tree in ((char.get("talents") or {}).get("class") or [])
               for t in (tree.get("talents") or []) if isinstance(t, dict)}
    spell_power = (st.get("spellDamage") or {}).get("shadow") or 0
    pct = (BASE_PER_LEVEL * level + spell_power) / 100

    base_hit = st.get("spellHit") or 0  # GetSpellHitModifier: hit from talents and auras, without gear
    cap = SPELL_MISS[p["target"]] - MIN_MISS
    need = max(0.0, cap - base_hit)
    suppression = SUPPRESSION_PER_RANK * talents.get(SUPPRESSION, 0)
    need_aff = max(0.0, need - suppression)
    hit_steps = [(need_aff, p["aff_share"]), (need, 1 - p["aff_share"])]

    pandemic = talents.get(PANDEMIC, 0)
    crit_share = 0.25 + 0.6 * min(pandemic, 3) / 3  # without Pandemic only Shadow Bolt and the like crit
    weights = {"sp": 1.0, "crit": round(pct * crit_share * CRIT_BONUS, 2), **p["weights"]}

    hit_now = pct * sum(share for limit, share in hit_steps if gear_hit < limit)
    if need <= 0:
        hit_note = tr("{base:g}% without gear, the cap against +{target} levels is {cap:g}%: already reached",
                      base=base_hit, target=p["target"], cap=cap)
    else:
        hit_note = tr("cap against +{target} levels {cap:g}%, you have {base:g}%, {need:g}% missing",
                      target=p["target"], cap=cap, base=base_hit, need=need)
        if suppression:
            hit_note += tr(" (Affliction spells only {need:g}% thanks to Suppression)", need=need_aff)
        hit_note += tr(", gear gives {gear:g}%", gear=gear_hit)
    crit_note = tr("1% damage = {points:.1f} spell power, {share:.0%} of the damage can crit", points=pct, share=crit_share)
    if pandemic:
        crit_note += tr(" (assumption: Pandemic lets DoTs crit)")

    # Intellect and stamina: what one point gives at this level, valued against the mana pool.
    stamina_note = tr("health, and mana through Life Tap (fixed value)")
    intellect_note = tr("15 mana and some crit (fixed value)")
    mana, health = st.get("powerMax") or 0, st.get("healthMax") or 0
    cpi, cpi_source = crit_per_intellect(st, gear_crit)
    if mana > 0 and health > 0 and cpi is not None:
        tap, tap_source = life_tap_ratio(st, level, talents.get(IMPROVED_LIFE_TAP_ID, 0))
        pool = mana + health * tap
        per_mana = p["mana_share"] * pct * 100 / pool
        per_health = p["survival_share"] * pct * 100 / health
        mpi = st.get("manaPerIntellect") or MANA_PER_INT
        hps = st.get("hpPerStamina") or HP_PER_STAMINA
        int_mana, int_crit = mpi * per_mana, cpi * weights["crit"]
        sta_tap, sta_live = hps * tap * per_mana, hps * per_health
        weights["intellect"] = round(int_mana + int_crit, 3)
        weights["stamina"] = round(sta_tap + sta_live, 3)
        intellect_note = tr("{mana:g} mana = {manaValue:.2f}, {crit:.4f}% crit ({source}) = {critValue:.2f}. "
                            "1 mana = {perMana:.3f}: pool {pool:.0f} (mana + health through Life Tap), "
                            "1% more pool counts like {share:g}% damage", mana=mpi, manaValue=int_mana, crit=cpi,
                            source=cpi_source, critValue=int_crit, perMana=per_mana, pool=pool, share=p["mana_share"])
        stamina_note = tr("{health:g} health: {tapMana:.1f} mana through Life Tap ({source}) = {tapValue:.2f}, "
                          "survival = {liveValue:.2f} (1% more health counts like {share:g}% damage)", health=hps,
                          tapMana=hps * tap, source=tap_source, tapValue=sta_tap, liveValue=sta_live,
                          share=p["survival_share"])
    w = weights
    rows = [
        dict(label=tr("Shadow / spell power"), weight=1.0, note=tr("reference")),
        dict(label=tr("Stamina"), weight=w["stamina"], note=stamina_note),
        dict(label=tr("Intellect"), weight=w["intellect"], note=intellect_note),
        dict(label=tr("Spirit"), weight=w["spirit"], note=tr("regeneration out of combat")),
        dict(label=tr("Mana per 5 sec."), weight=w["mp5"], note=""),
        dict(label=tr("Hit, next 1%"), weight=round(hit_now, 2), note=hit_note),
        dict(label=tr("Crit, per 1%"), weight=w["crit"], note=crit_note),
        dict(label=tr("Wand DPS"), weight=w["wand_dps"], note=tr("wand slot only")),
    ]
    text = tr("Shadow/spell power 1, Stamina {stamina:.2f}, Int {intellect:.2f}, Spirit {spirit:g}, Mana/5s {mp5:g}, "
              "Hit 1% = {hit:.1f}, Crit 1% = {crit:.1f}, Wand DPS {wand:g}", stamina=w["stamina"],
              intellect=w["intellect"], spirit=w["spirit"], mp5=w["mp5"], hit=hit_now, crit=w["crit"], wand=w["wand_dps"])
    return dict(profile=profile, label=tr(p["label"]), pct=pct, weights=weights, hitSteps=hit_steps, rows=rows, text=text)


def set_value(total: dict, scale: dict) -> float:
    """Value of a whole set of stats. Linear except for hit, which stops counting at the cap."""
    val = sum(w * total.get(k, 0) for k, w in scale["weights"].items())
    hit = max(0.0, total.get("hit", 0))
    return val + scale["pct"] * sum(share * min(hit, limit) for limit, share in scale["hitSteps"])


def equipped_stats(char: dict, ratings: dict | None = None) -> tuple[dict, dict]:
    """(normalised stats per slot, their sum) for the character's equipped items."""
    eq = {e["slot"]: norm_stats(e, e["slot"] == 18, ratings) for e in (char.get("equipment") or []) if e.get("slot")}
    total: dict = {}
    for n in eq.values():
        add_stats(total, n)
    return eq, total


def special_effects(tip: list[str]) -> list[str]:
    """Tooltip effects the scale cannot rate. "Chance on hit:" procs need melee hits and are left out."""
    return [t for t in tip if t.startswith(USE_PREFIX) or (t.startswith(EQUIP_PREFIX) and not STAT_LINE.match(t))]


def pareto(states: list[tuple]) -> list[tuple]:
    """Keeps the (cost, gain, ...) states where more money buys more gain."""
    out, best = [], float("-inf")
    for st in sorted(states, key=lambda x: (x[0], -x[1])):
        if st[1] > best + 1e-9:
            out.append(st)
            best = st[1]
    return out


def best_package(groups: list[list[tuple]], budget: int) -> tuple:
    """Multiple-choice knapsack: at most one option (cost, gain, picks) per group, highest total gain within budget."""
    frontier = [(0, 0.0, ())]
    for options in groups:
        options = pareto([o for o in options if o[1] > 0 and o[0] <= budget])
        states = list(frontier)
        for cost, gain, picks in frontier:
            states += [(cost + oc, gain + og, picks + op) for oc, og, op in options if cost + oc <= budget]
        frontier = pareto(states)
        if len(frontier) > 4000:  # thin out: best state per price bucket
            step = max(1, budget // 4000)
            frontier = pareto(list({st[0] // step: st for st in frontier}.values()))
    return frontier[-1]


SHORT_NAME = {"spell_power": mark("SP"), "spell_damage_done": mark("SD"), "shadow_damage_done": mark("Shadow"),
              "stamina": mark("Sta"), "intellect": mark("Int"), "spirit": mark("Spi"), "hit_rating": mark("Hit rating"),
              "crit_rating": mark("Crit rating"), "mana_regeneration": mark("Mana/5s"), "damage_per_second": mark("DPS")}


def short(stats: dict | None) -> str:
    raw = {stat_key(k): val for k, val in (stats or {}).items()}  # Lua hands the keys over in random order
    return ", ".join(f"{tr(n)} {round(raw[k], 1):g}" if isinstance(raw[k], (int, float)) else tr(n)
                     for k, n in SHORT_NAME.items() if k in raw)


def meets_skill_requirements(tooltip: list[str], skills: dict[str, int]) -> bool:
    """False for tooltip lines like "Requires Engineering (215)" the character does not meet. The skill names
    in the tooltip and in the character's skill list are both in the client's language."""
    for t in tooltip:
        m = SKILL_REQ.match(t)
        if m and skills.get(m.group(1), 0) < int(m.group(2)):
            return False
    return True


STAT_LABEL = {"stamina": mark("Stamina"), "intellect": mark("Intellect"), "spirit": mark("Spirit"),
              "strength": mark("Strength"), "agility": mark("Agility")}
SPELL_LABEL = {
    "spell_power": mark("Equip: Increases damage and healing done by magical spells and effects by up to {n}."),
    "spell_damage_done": mark("Equip: Increases damage done by magical spells and effects by up to {n}."),
    "shadow_damage_done": mark("Equip: Increases damage done by Shadow spells and effects by up to {n}."),
    "spell_healing_done": mark("Equip: Increases healing done by spells and effects by up to {n}."),
}


def tooltip(rec: dict, slot_label: str | None = None) -> dict:
    """Tooltip for the app: the game's own lines when the addon exported them (1.2+: with colours), otherwise
    rebuilt from the stats so older snapshots still show something useful."""
    out = {"name": rec.get("name"), "quality": rec.get("quality"), "ilvl": rec.get("ilvl")}
    if rec.get("tooltip"):
        out["lines"] = rec["tooltip"]
        out["colors"] = rec.get("tipColors")
        return out
    lines, colors, equip = [], [], []
    if slot_label or rec.get("subType"):
        lines.append(f"{slot_label or ''} | {rec.get('subType') or ''}".strip(" |"))
        colors.append("ffffff")
    stats = {stat_key(k): val for k, val in (rec.get("stats") or {}).items()}
    if stats.get("armor"):
        lines.append(tr("{n:g} Armor", n=stats["armor"]))
        colors.append("ffffff")
    if stats.get("damage_per_second"):
        lines.append(tr("({n:.1f} damage per second)", n=stats["damage_per_second"]))
        colors.append("ffffff")
    for k, label in STAT_LABEL.items():
        if stats.get(k):
            lines.append(f"+{stats[k]:g} {tr(label)}")
            colors.append("ffffff")
    dur = rec.get("durability")
    if isinstance(dur, list) and len(dur) == 2:
        lines.append(tr("Durability {now} / {max}", now=dur[0], max=dur[1]))
        colors.append("ffffff")
    if rec.get("reqLevel"):
        lines.append(tr("Requires Level {n}", n=rec["reqLevel"]))
        colors.append("ffffff")
    for k, text in SPELL_LABEL.items():
        if stats.get(k):
            equip.append(tr(text, n=f"{stats[k]:g}"))
    lines += equip
    colors += ["1eff00"] * len(equip)
    out.update(lines=lines, colors=colors, rebuilt=True)
    return out


def money(c: int) -> str:
    return f"{c // 10000}g {c // 100 % 100:02d}s" if c >= 10000 else f"{c // 100}s {c % 100:02d}c"


def load_item_export(wow: str, char: dict) -> dict:
    """Item export of "/wowmcp items": account-wide WoWMCP_Items (addon 1.1+), else the older per-character copy."""
    best: dict = {}
    for path in glob.glob(os.path.join(wow, "WTF", "Account", "*", "SavedVariables", "WoWMCP.lua")):
        data = load_sv(path, "WoWMCP_Items") or {}
        if (data.get("updated") or 0) > (best.get("updated") or 0):
            best = data
    if not best.get("items"):
        best = char.get("itemInfo") or {}
    return best


def compute(wow: str = writs.DEFAULT_WOW, char: str = "", level: int = 0, preview: int = 4, max_price: float = 0,
            top: int = 3, prices: dict | None = None, profile: str = "", sort: str = "gain",
            engine: str = "off", fight_seconds: float = 60.0, target_delta: int = -1, metric: str = "") -> dict:
    """Upgrade candidates per slot and the best purchase for the budget, as plain data (copper prices).

    With engine "on" (must work) or "auto" (falls back to the weight scale) the gains come from the combat
    simulation (sim/): the metric change (DPS) of each swap over a fight of `fight_seconds` against enemies
    `target_delta` levels above you (default: the profile's target)."""
    prices = prices if prices is not None else writs.load_prices(writs.find_auctionator(wow))
    last = max(int(k) for e in prices.values() if isinstance(e, dict) for k in (e.get("a") or {}))
    files = writs.find_chars(wow, char)
    if not files:
        raise FileNotFoundError(tr("No WoWMCP.lua found for this character."))
    c = load_char(files[0])
    info = load_item_export(wow, c)
    items = {int(k): x for k, x in (info.get("items") or {}).items()}
    if not items:
        raise LookupError(tr("No item export found: type /wowmcp items in the game, wait for the message, then /reload."))
    level = level or (c.get("character") or {}).get("level") or 60
    gold = (c.get("character") or {}).get("money") or 0
    budget = int(max_price * 10000) if max_price else gold

    equipped = {e["slot"]: e for e in (c.get("equipment") or [])}
    skills = {s["name"]: s.get("rank") or 0 for s in ((c.get("skills") or {}).get("skills") or [])
              if isinstance(s, dict) and s.get("name")}
    ratings = learn_ratings(items)
    eq, totals = equipped_stats(c, ratings)
    scale = build_scale(c, profile, totals.get("hit", 0), totals.get("crit", 0))
    base = set_value(totals, scale)
    two_handed = (equipped.get(16) or {}).get("equipLoc") == "INVTYPE_2HWEAPON"
    # The class line of a tooltip names the classes in the client's language, like the character's class.
    own_class = (c.get("character") or {}).get("class")
    class_names = (own_class,) if own_class else WARLOCK_NAMES

    # The combat engine, when asked for: "on" must work, "auto" falls back to the weight scale.
    eng = None
    if engine != "off":
        try:
            class_file = (c.get("character") or {}).get("classFile") or ""
            if not simengine.supported(class_file):
                raise LookupError(tr("The combat engine does not model this class yet ({cls}).",
                                     cls=class_file or "?"))
            st = (c.get("character") or {}).get("stats") or {}
            life_tap = {"healthPerCast": 0.0, "manaPerCast": 0.0}
            m = LIFE_TAP_TEXT.search((st.get("lifeTap") or {}).get("text") or "")
            if m and number(m.group(1)) > 0:
                life_tap = {"healthPerCast": number(m.group(1)), "manaPerCast": number(m.group(2))}
            fight = {"seconds": fight_seconds,
                     "targetDelta": target_delta if 0 <= target_delta <= 3 else PROFILES[scale["profile"]]["target"]}
            eng = simengine.start(c, totals.get("mp5", 0), totals.get("hp5", 0), fight, metric, life_tap)
        except LookupError:
            if engine == "on":
                raise
            eng = None

    def swap(remove: list[int], adds: list[dict]) -> float:
        """Change of the set's value when the items in the `remove` slots make room for `adds`."""
        t = dict(totals)
        for s in remove:
            add_stats(t, eq.get(s, {}), -1)
        for n in adds:
            add_stats(t, n)
        return set_value(t, scale) - base

    def value_batch(evals: list[tuple[str, list[dict], list[int]]]) -> dict[str, float]:
        """Gain per evaluation (key, added stat dicts, emptied slots): the engine's metric change when it
        runs, otherwise the weight scale's point change. One engine run values the whole batch."""
        if eng:
            return eng.batch([(k, simengine.delta(adds, [eq.get(s, {}) for s in rm])) for k, adds, rm in evals])
        return {k: swap(list(rm), list(adds)) for k, adds, rm in evals}

    pool, review = [], []
    for iid, it in items.items():
        slots = SLOT_OF.get(it.get("equipLoc") or "")
        req = it.get("reqLevel") or 0
        if not slots or req > level + preview or (it.get("quality") or 0) < 2:
            continue
        if it.get("classID") == 2 and it.get("subclassID") not in USABLE_WEAPONS:
            continue
        if it.get("classID") == 4 and it.get("subclassID") not in (0, 1):
            continue
        tip = it.get("tooltip") or []
        if any(t.startswith(CLASSES_PREFIX) and not any(n in t for n in class_names) for t in tip):
            continue
        if not meets_skill_requirements(tip, skills):
            continue
        e = prices.get(str(iid))
        if not isinstance(e, dict) or not e.get("m"):
            continue
        if not any(int(d) >= last - 3 for d in (e.get("a") or {})):
            continue
        price = e["m"]
        if price > budget:
            continue
        n = norm_stats(it, slots == [18], ratings)
        if slots == [16, 17]:
            slot, variants = 16, [[16, 17]]
        elif len(slots) == 2:  # ring or trinket: like Pawn, against the weaker of the two (the bigger gain)
            slot, variants = 0, [[slots[0]], [slots[1]]]
        elif slots == [17] and two_handed:  # an off-hand item means giving up the staff
            slot, variants = 17, [[16]]
        else:
            slot, variants = slots[0], [slots[0:1]]
        pool.append(dict(id=iid, it=it, price=price, slot=slot, variants=variants, norm=n,
                         extra=special_effects(tip), soon=req > level))

    # One batch values everything: each candidate's swap (both slots for rings and trinkets), its add-only
    # points (used to pick the pair candidates below), and what the current item of every slot is worth.
    evals = [(f"pt:{i}", [x["norm"]], []) for i, x in enumerate(pool)]
    evals += [(f"sw:{i}:{j}", [x["norm"]], rm) for i, x in enumerate(pool) for j, rm in enumerate(x["variants"])]
    removes_seen = {(s,) for s in SLOT_NAME} | {tuple(rm) for x in pool for rm in x["variants"]}
    evals += [("rm:" + "+".join(map(str, rm)), [], list(rm)) for rm in sorted(removes_seen)]
    values = value_batch(evals)

    def old_of(remove: list[int]) -> float:
        """What the current item(s) of these slots contribute (for "of the item" percentages)."""
        return -values["rm:" + "+".join(map(str, remove))]

    def item_out(x: dict, gain: float | None = None) -> dict:
        it, gain = x["it"], x["gain"] if gain is None else gain
        old = old_of(x["remove"])
        return dict(id=x["id"], name=it.get("name"), quality=it.get("quality"), reqLevel=it.get("reqLevel") or 0,
                    price=x["price"], gain=round(gain, 1), gainPct=round(gain / old * 100) if old > 0.5 else None,
                    perGold=round(gain / (x["price"] / 10000), 1), stats=short(it.get("stats")),
                    isNew=x["id"] >= NEW_ID, soon=x["soon"], effects=x["extra"], twoHand=len(x["remove"]) == 2,
                    tip=tooltip(it))

    for i, x in enumerate(pool):
        j = max(range(len(x["variants"])), key=lambda j: values[f"sw:{i}:{j}"])
        x["remove"], x["gain"], x["points"] = x["variants"][j], values[f"sw:{i}:{j}"], values[f"pt:{i}"]
        if not x["slot"]:
            x["slot"] = x["remove"][0]
        if x["extra"] and x["gain"] < 5 and (x["it"].get("reqLevel") or 0) >= level - 15:
            review.append(dict(slot=slot_name(x["slot"]), **item_out(x)))

    rows = [x for x in pool if x["gain"] > 0.5 and not (x["extra"] and x["gain"] < 5)]
    order = (lambda x: (-x["gain"] / x["price"], -x["gain"])) if sort == "price" else (lambda x: (-x["gain"], x["price"]))
    slots_out = []
    for slot in sorted({x["slot"] for x in rows}):
        cur = equipped.get(slot)
        best = sorted((x for x in rows if x["slot"] == slot), key=order)[:top]
        slots_out.append(dict(
            slot=slot, slotName=slot_name(slot),
            current=dict(name=cur.get("name"), id=cur.get("id"), quality=cur.get("quality"),
                         stats=short(cur.get("stats")), tip=tooltip(cur, slot_name(slot))) if cur else None,
            candidates=[item_out(x) for x in best]))

    # Best purchase for the budget: one option per slot group, pairs for rings/trinkets and main hand + off hand.
    usable = [x for x in pool if not x["soon"]]
    groups: dict = {}
    for x in rows:
        if not x["soon"]:
            key = PAIRS.get(x["slot"], ("weapon",) if x["slot"] in (16, 17) else (x["slot"],))
            groups.setdefault(key, []).append((x["price"], x["gain"], ((x, x["remove"]),)))
    combos = []  # (group key, x, y, x's slots, y's slots), valued in one batch
    for a, b in set(PAIRS.values()):
        both = sorted((x for x in usable if x["slot"] in (a, b)), key=lambda x: -x["points"])[:12]
        combos += [((a, b), x, y, [a], [b]) for i, x in enumerate(both) for y in both[i + 1:]]
    one_hand = sorted((x for x in usable if x["slot"] == 16 and len(x["remove"]) == 1), key=lambda x: -x["points"])[:10]
    off_hand = sorted((x for x in usable if x["slot"] == 17), key=lambda x: -x["points"])[:10]
    combos += [(("weapon",), x, y, [16], [17]) for x in one_hand for y in off_hand]
    cvals = value_batch([(f"cb:{i}", [x["norm"], y["norm"]], ra + rb)
                         for i, (_, x, y, ra, rb) in enumerate(combos)])
    for i, (key, x, y, ra, rb) in enumerate(combos):
        groups.setdefault(key, []).append((x["price"] + y["price"], cvals[f"cb:{i}"], ((x, ra), (y, rb))))
    cost, _, picks = best_package(list(groups.values()), budget)

    # Gain per item in the order of the set (each prefix valued exactly), so the lines add up to the total.
    prefix_adds, emptied, prefix_evals = [], [], []
    for i, (x, remove) in enumerate(picks):
        emptied += [s for s in remove if s in eq and s not in emptied]
        prefix_adds.append(x["norm"])
        prefix_evals.append((f"px:{i}", list(prefix_adds), list(emptied)))
    pvals = value_batch(prefix_evals)
    package_items, value = [], 0.0
    for i, (x, remove) in enumerate(picks):
        step, value = pvals[f"px:{i}"] - value, pvals[f"px:{i}"]
        replaced = [equipped[s].get("name") for s in remove if s in equipped]
        old = next((equipped[s] for s in remove if s in equipped), None)
        package_items.append(dict(slot=x["slot"], slotName=slot_name(x["slot"]), replaces=replaced,
                                  compareTip=tooltip(old, slot_name(old["slot"])) if old else None,
                                  **item_out(dict(x, remove=remove), step)))
    package_items.sort(key=lambda p: -p["perGold"])  # best value first: stop anywhere down the list
    cum_cost = cum_gain = 0
    for p in package_items:
        cum_cost, cum_gain = cum_cost + p["price"], cum_gain + p["gain"]
        p.update(cumCost=cum_cost, cumGain=round(cum_gain, 1))
    base_value = (eng.baseline.get("value") or 0) if eng else base
    package = dict(cost=cost, gain=round(value, 1), gainPct=round(value / base_value * 100) if base_value else None,
                   items=package_items)

    # What the header shows: the engine's combat model when it ran, the weight scale otherwise.
    weights_text, scale_rows = scale["text"], scale["rows"]
    engine_out = None
    if eng:
        b = eng.baseline
        scale_rows = []
        for row in b.get("breakdown") or []:
            note = tr("{share:.1f}% of the fight time", share=row.get("sharePct") or 0)
            if row.get("kind") == "lifetap":
                note = tr("{n:.1f} casts per minute", n=row.get("perMin") or 0) + ", " + note
            scale_rows.append(dict(label=row.get("label") or row.get("kind") or "?",
                                   weight=round(row.get("value") or 0, 1), note=note))
        fight = eng.bundle["fight"]
        weights_text = tr("Combat model: {dps:.1f} DPS against +{target} levels, {seconds:g}s fight",
                          dps=b.get("value") or 0, target=fight["targetDelta"], seconds=fight["seconds"])
        if b.get("ttoomSeconds") is not None:
            weights_text += tr(", mana for {ttoom:.0f}s", ttoom=b["ttoomSeconds"])
        if eng.spellbook_source == "assumed":
            weights_text += tr(", spellbook assumed from level and talents")
        engine_out = dict(version=eng.version, metric=b.get("metric"), value=b.get("value"),
                          ttoomSeconds=b.get("ttoomSeconds"), spellbookSource=eng.spellbook_source,
                          manaSpentPs=b.get("manaSpentPs"), manaBudgetPs=b.get("manaBudgetPs"),
                          fight=fight)

    review.sort(key=lambda r: r["price"])
    return dict(
        scanDate=(datetime.date(2020, 1, 1) + datetime.timedelta(last)).isoformat(),
        exportUpdated=info.get("updated"), exportCount=len(items), exportMissing=len(info.get("missing") or []),
        level=level, budget=budget, character=os.path.basename(os.path.dirname(os.path.dirname(files[0]))),
        weights=weights_text, profile=scale["profile"], profileLabel=scale["label"], scale=scale_rows,
        setValue=round(base_value, 1), sort=sort, engine=engine_out, package=package, slots=slots_out,
        review=review[:15])


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--wow", default=writs.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--level", type=int, default=0, help="override the level (default: from the export)")
    ap.add_argument("--preview", type=int, default=4, help="also show items up to level + N")
    ap.add_argument("--max-price", type=float, default=0,
                    help="budget in gold, per item and for the package (default: your own gold)")
    ap.add_argument("--top", type=int, default=3, help="suggestions per slot")
    ap.add_argument("--profile", choices=sorted(PROFILES), default="", help="default: leveling below level 60")
    ap.add_argument("--sort", choices=["gain", "price"], default="gain", help="price = points per gold")
    ap.add_argument("--engine", choices=["auto", "on", "off"], default="off",
                    help="rate with the combat simulation (sim/simforever) instead of the weight scale; "
                         "auto falls back to the scale when the engine cannot run")
    ap.add_argument("--fight-seconds", type=float, default=60, help="fight length for the combat model")
    ap.add_argument("--target-delta", type=int, default=-1, choices=[-1, 0, 1, 2, 3],
                    help="enemy levels above you for the combat model (default: from the profile)")
    ap.add_argument("--metric", default="", help="metric of the combat model (default: per class)")
    ap.add_argument("--json", action="store_true", help="print the result as JSON (for the app)")
    i18n.add_argument(ap)
    a = ap.parse_args()
    i18n.set_language(a.lang, a.wow)

    try:
        r = compute(a.wow, a.char, a.level, a.preview, a.max_price, a.top, profile=a.profile, sort=a.sort,
                    engine=a.engine, fight_seconds=a.fight_seconds, target_delta=a.target_delta, metric=a.metric)
    except (FileNotFoundError, LookupError) as e:
        sys.exit(str(e))
    sys.stdout.reconfigure(encoding="utf-8")
    if a.json:
        import json
        print(json.dumps(r, ensure_ascii=False))
        return

    def line(c: dict) -> str:
        pct = f"{c['gainPct']:+4d}%" if c["gainPct"] is not None else f"{tr('new'):>5}"
        flag = " " + tr("[NEW in Forever]") if c["isNew"] else ""
        soon = " " + tr("(from level {level})", level=c["reqLevel"]) if c["soon"] else ""
        both = " " + tr("[two-handed]") if c["twoHand"] else ""
        return (f"{c['gain']:+6.1f} {pct}  {money(c['price']):>9}  {c['perGold']:6.1f}/g  {c['name']}{flag}{soon}{both}"
                f"  [{c['stats']}]  id {c['id']}")

    when = datetime.datetime.fromtimestamp(r["exportUpdated"] or 0)
    print(tr("AH scan: {date} | item export: {when} | level {level} | budget {budget} | {count} items, "
             "{missing} not loaded", date=r["scanDate"], when=f"{when:%Y-%m-%d %H:%M}", level=r["level"],
             budget=money(r["budget"]), count=r["exportCount"], missing=r["exportMissing"]))
    if r.get("engine"):
        print(r["weights"])  # the combat model's summary; gains below are the metric change (DPS)
    else:
        print(tr("Scale \"{label}\" (1 point = 1 shadow spell power), value of your gear now: {value}",
                 label=r["profileLabel"], value=r["setValue"]))
    for row in r["scale"]:
        print(f"   {row['label']:24} {row['weight']:5.2f}  {row['note']}")
    print(tr("Prices are the lowest buyout at the scan.") + "\n")
    p = r["package"]
    if p["items"]:
        pct = f" ({p['gainPct']:+d}%)" if p["gainPct"] is not None else ""
        print("== " + tr("Best package for {budget}: {gain} points{pct} for {cost}", budget=money(r["budget"]),
                         gain=f"{p['gain']:+.1f}", pct=pct, cost=money(p["cost"])))
        for c in p["items"]:
            print(f"   {c['slotName']:10} {line(c)}")
            print(f"   {'':10} " + tr("up to here {cost} for {gain} points", cost=money(c["cumCost"]),
                                      gain=f"{c['cumGain']:+.1f}")
                  + (tr(", replaces {items}", items=" + ".join(c["replaces"])) if c["replaces"] else ""))
        print()
    for s in r["slots"]:
        cur = s["current"]
        print("== " + tr("{slot}: now {name} ({stats})", slot=s["slotName"], name=cur["name"] if cur else "-",
                         stats=cur["stats"] if cur else ""))
        for c in s["candidates"]:
            print(f"   {line(c)}")
            for t in c["effects"]:
                print(f"            {t}")
    if r["review"]:
        print("\n== " + tr("With a special effect, please look for yourself"))
        for x in r["review"]:
            print(f"   {x['slot']:10} {money(x['price']):>9}  {x['name']}: {' / '.join(x['effects'])[:160]}")


if __name__ == "__main__":
    main()
