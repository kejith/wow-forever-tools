# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Consumables for the level of a character: the strongest healing and mana potions, food, drinks, bandages
and buffs (elixirs, flasks, scrolls, weapon oils, buff food) the character can use now, with the price in
the auction house or at a vendor, what is in the bags and which ones the character can craft.

    uv run scripts/consumables.py --char Malgruk [--top 5] [--all-buffs] [--engine auto|on|off]
                                  [--fight-seconds 60] [--target-delta 2] [--json] [--lang en|de]

Sources:
- What an item does comes from Forever's DB2 tables (wago.tools, product wowf): Item (class 0 = consumable),
  ItemSparse (name, required level and skill, vendor price), ItemEffect + ItemXItemEffect (the spell of the
  item), SpellEffect (heal, mana, auras), SpellMisc + SpellDuration (duration), SpellItemEnchantment (weapon
  oils). Cached as JSON per build and client locale.
- Prices from Auctionator (lowest buyout at the last scan), vendor prices from WoWMCP_Account.vendor or, for
  the food and water every innkeeper and food vendor sells, from ItemSparse.
- Bags, bank, level, class and learned recipes from the WoWMCP export; the recipe catalog of recipes.py says
  which profession makes an item.

"Best" is the strongest item the character can use now and get: in the bags, in the AH, at a vendor or
craftable with a learned recipe.

With the combat engine (sim/, through rotations.py) the consumables that work during a fight are also rated
by what they add to the character's DPS or HPS, sorted by kind (flask, elixirs, buff food, weapon oil,
scrolls, potions, runes, other buffs), and the best set is put together: what can be active at once comes
from the game's data where it says so (KINDS has the rule per kind and names what is an assumption).
"""

from __future__ import annotations

import argparse
import glob
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import i18n  # noqa: E402
import icons  # noqa: E402  (DB2 download and build)
import recipes  # noqa: E402  (recipe catalog, owned items)
import upgrades  # noqa: E402  (reads SavedVariables)
import writs  # noqa: E402  (Auctionator)
from i18n import mark, tr  # noqa: E402

CATALOG_VERSION = 2
NEW_ID = 200000

# Item subclasses of class 0 (consumable).
POTION, ELIXIR, FLASK, SCROLL, FOOD, ENHANCEMENT, BANDAGE, OTHER = 1, 2, 3, 4, 5, 6, 7, 8
TYPE_LABEL = {POTION: mark("Potion"), ELIXIR: mark("Elixir"), FLASK: mark("Flask"), SCROLL: mark("Scroll"),
              FOOD: mark("Food"), BANDAGE: mark("Bandage"), OTHER: mark("Other")}
OIL_LABEL = mark("Weapon oil")

# Restoring categories: key, title, what the amount is.
CATEGORIES = [
    ("healing", mark("Healing potions")),
    ("mana", mark("Mana potions")),
    ("food", mark("Food")),
    ("drink", mark("Drinks")),
    ("bandage", mark("Bandages")),
]

# Buff stats in the order of the page.
STATS = {
    "sp": mark("Spell power"), "sp_shadow": mark("Shadow spell power"), "sp_fire": mark("Fire spell power"),
    "sp_frost": mark("Frost spell power"), "sp_arcane": mark("Arcane spell power"),
    "sp_nature": mark("Nature spell power"), "sp_holy": mark("Holy spell power"), "healing": mark("Healing"),
    "spell_crit": mark("Spell crit"), "intellect": mark("Intellect"), "spirit": mark("Spirit"),
    "mp5": mark("Mana per 5 sec."), "stamina": mark("Stamina"), "health": mark("Maximum health"),
    "hp5": mark("Health per 5 sec."), "armor": mark("Armor"), "strength": mark("Strength"),
    "agility": mark("Agility"), "ap": mark("Attack power"), "rap": mark("Ranged attack power"),
    "crit": mark("Crit"), "all_stats": mark("All attributes"),
}
CASTER = {"sp", "intellect", "spirit", "mp5", "stamina", "health", "spell_crit", "all_stats"}
MELEE = {"strength", "agility", "ap", "crit", "stamina", "health", "armor", "hp5", "all_stats"}
RELEVANT = {
    "WARLOCK": CASTER | {"sp_shadow", "sp_fire"},
    "PRIEST": CASTER | {"sp_shadow", "sp_holy", "healing"},
    "MAGE": CASTER | {"sp_fire", "sp_frost", "sp_arcane"},
    "WARRIOR": MELEE, "ROGUE": MELEE,
    "HUNTER": {"agility", "ap", "rap", "crit", "intellect", "mp5", "stamina", "health", "all_stats"},
}  # Paladin, Shaman, Druid and unknown classes: every stat
CLASS_ID = {"WARRIOR": 1, "PALADIN": 2, "HUNTER": 3, "ROGUE": 4, "PRIEST": 5, "DEATHKNIGHT": 6, "SHAMAN": 7,
            "MAGE": 8, "WARLOCK": 9, "MONK": 10, "DRUID": 11}

SCHOOL = {2: "sp_holy", 4: "sp_fire", 8: "sp_nature", 16: "sp_frost", 32: "sp_shadow", 64: "sp_arcane"}
ATTRIBUTE = {0: "strength", 1: "agility", 2: "stamina", 3: "intellect", 4: "spirit", -1: "all_stats"}
ITEM_MOD = {3: "agility", 4: "strength", 5: "intellect", 6: "spirit", 7: "stamina"}  # enchantment stat type 5
# Aura type -> stat (or a function of the misc value). Food and drink auras (84/85) of eating are handled apart.
AURA = {
    13: lambda m: "sp" if m & 126 == 126 else SCHOOL.get(m),  # MOD_DAMAGE_DONE, misc = school mask
    135: lambda m: "healing", 29: lambda m: ATTRIBUTE.get(m), 34: lambda m: "health",
    99: lambda m: "ap", 124: lambda m: "rap", 85: lambda m: "mp5" if m == 0 else None,
    161: lambda m: "hp5", 84: lambda m: "hp5", 57: lambda m: "spell_crit", 71: lambda m: "spell_crit",
    52: lambda m: "crit", 290: lambda m: "crit",
    22: lambda m: "armor" if m == 1 else None, 674: lambda m: "armor" if m == 1 else None,
}
SKILL_ALIAS = {2942: 129}  # Forever's bandages ask for this skill line; the character has First Aid (129)
PROFESSION = {171: mark("Alchemy"), 185: mark("Cooking"), 129: mark("First Aid"), 202: mark("Engineering"),
              333: mark("Enchanting"), 197: mark("Tailoring"), 165: mark("Leatherworking"),
              164: mark("Blacksmithing")}
SKIP_WORDS = ("deprecated", "test", "[ph]", "qa ", "monster -", "unused", "zzold")
# Consumables the game files under another item class: Demonic Rune and Dark Rune are trade goods.
EXTRA_ITEMS = {12662, 20520}
# Spell categories (SpellCategory) that say what shares a cooldown: eating, drinking, potions, the runes and
# roots apart from the potions, the Zanza potions, the buffs from the Blasted Lands.
CAT_FOOD, CAT_DRINK, CAT_POTION, CAT_RUNE, CAT_ZANZA, CAT_BLASTED = 11, 59, 4, 1153, 79, 103

# Food and water every innkeeper or food vendor sells (bread, meat, fish, cheese, fruit, mushrooms, water).
VENDOR_FOOD = {
    159, 1179, 1205, 1708, 1645, 8766,
    4540, 4541, 4542, 4544, 4601, 8950, 117, 2287, 3770, 3771, 4599, 8952,
    787, 4592, 4593, 4594, 21552, 8957, 2070, 414, 422, 1707, 3927, 8932,
    4536, 4537, 4538, 4539, 4602, 8953, 4604, 4605, 4606, 4607, 4608, 8948,
}


def _int(x) -> int:
    try:
        return int(float(x or 0))
    except ValueError:
        return 0


def _float(x) -> float:
    try:
        return float(x or 0)
    except ValueError:
        return 0.0


# ---------- catalog ----------

def decode(sid: int, effects: dict, durations: dict, enchants: dict, subclass: int, depth: int = 0,
           eating: bool = False) -> dict:
    """What a spell does: {"restore": [...], "buffs": [...], "duration": seconds, "oil": bool, "auras": the
    spells whose aura is a buff, "healthCost": damage to the user}. Follows triggered spells (Forever's
    "Nutritious Food" triggers the eating spell and, after 10 s, Well Fed). `eating`: the spell is in the
    cooldown category of food or drink, whatever the item's subclass says."""
    out = {"restore": [], "buffs": [], "duration": durations.get(sid), "oil": False, "wellFed": False,
           "auras": [], "healthCost": 0}
    if depth > 2:
        return out
    for e in effects.get(sid, []):
        kind, aura, bp, misc = e["effect"], e["aura"], e["bp"], e["misc"]
        if kind == 10 and bp > 0:  # HEAL
            out["restore"].append({"kind": "healing", "amount": bp, "var": e["var"]})
        elif kind == 30 and misc == 0 and bp > 0:  # ENERGIZE mana
            out["restore"].append({"kind": "mana", "amount": bp, "var": e["var"]})
        elif kind == 2 and bp > 0 and depth == 0:  # SCHOOL_DAMAGE of an item's own spell: a rune burns its user
            out["healthCost"] += bp
        elif kind == 64 and e["trigger"]:  # TRIGGER_SPELL
            sub = decode(e["trigger"], effects, durations, enchants, subclass, depth + 1, eating)
            out["restore"] += sub["restore"]
            out["buffs"] += sub["buffs"]
            out["auras"] += sub["auras"]
            out["duration"] = out["duration"] or sub["duration"]
            out["wellFed"] = out["wellFed"] or sub["wellFed"]
        elif kind == 54 and misc in enchants:  # ENCHANT_ITEM_TEMPORARY: weapon oil
            ench = enchants[misc]
            for etype, points, arg in ench["effects"]:
                if etype == 3:  # equip spell
                    out["buffs"] += decode(arg, effects, durations, enchants, OTHER, depth + 1)["buffs"]
                elif etype == 5 and ITEM_MOD.get(arg):
                    out["buffs"].append({"stat": ITEM_MOD[arg], "amount": points})
            if out["buffs"]:
                out["oil"], out["duration"] = True, ench["duration"]
        elif kind == 6:  # APPLY_AURA
            seconds = (durations.get(sid) or 0)
            if (subclass == FOOD or eating) and aura in (84, 85) and depth <= 1:
                # Eating and drinking: the aura holds the amount per 5 seconds.
                total = bp * seconds / 5 if seconds else bp
                out["restore"].append({"kind": "food" if aura == 84 else "drink", "amount": round(total),
                                       "seconds": seconds})
            elif subclass == BANDAGE and aura == 8 and e["period"]:  # PERIODIC_HEAL
                total = bp * seconds * 1000 / e["period"] if seconds else bp
                out["restore"].append({"kind": "bandage", "amount": round(total), "seconds": seconds})
            elif aura in (23, 227) and e["trigger"]:  # periodic trigger: Well Fed after some seconds of eating
                sub = decode(e["trigger"], effects, durations, enchants, OTHER, depth + 1)
                for b in sub["buffs"]:
                    # "Nutritious Food" keeps the amount on its own effect ($s2), the Well Fed aura holds 1.
                    out["buffs"].append({**b, "amount": max(b["amount"], bp)})
                if sub["buffs"]:
                    out["wellFed"] = True
                    out["buffDuration"] = sub["duration"]
                    out["auras"] += sub["auras"]
            elif aura in AURA and bp > 0:
                stat = AURA[aura](misc)
                if stat:
                    out["buffs"].append({"stat": stat, "amount": bp})
                    out["auras"].append(sid)
    if not out["restore"]:
        out["healthCost"] = 0  # damage that pays for nothing is no cost
    return out


def build_catalog(build: str | None, locale: str | None = None) -> dict:
    """{"items": {itemId: consumable}} from the DB2 tables, names in `locale`."""
    t = lambda name: icons.table(name, build, locale)  # noqa: E731
    base = {_int(r["ID"]): _int(r["SubclassID"]) if _int(r["ClassID"]) == 0 else OTHER for r in t("Item")
            if _int(r["ClassID"]) == 0 or _int(r["ID"]) in EXTRA_ITEMS}
    sparse = {_int(r["ID"]): r for r in t("ItemSparse") if _int(r["ID"]) in base}
    # The cooldown category of an item's spell: on the item effect or, when that has none, on the spell.
    spell_category = {_int(r["SpellID"]): _int(r.get("Category")) for r in t("SpellCategories")}
    effect_spell, effect_category = {}, {}
    for r in t("ItemEffect"):
        effect_spell[_int(r["ID"])] = _int(r["SpellID"])
        effect_category[_int(r["ID"])] = _int(r.get("SpellCategoryID"))
    spell_of: dict[int, list[int]] = {}
    categories: dict[int, list[int]] = {}
    for r in t("ItemXItemEffect"):
        iid, eid = _int(r["ItemID"]), _int(r["ItemEffectID"])
        if iid in sparse and effect_spell.get(eid):
            spell_of.setdefault(iid, []).append(effect_spell[eid])
            categories.setdefault(iid, []).extend(
                x for x in (effect_category.get(eid), spell_category.get(effect_spell[eid])) if x)
    effects: dict[int, list[dict]] = {}
    for r in t("SpellEffect"):
        effects.setdefault(_int(r["SpellID"]), []).append({
            "effect": _int(r["Effect"]), "aura": _int(r["EffectAura"]), "bp": _int(r["EffectBasePointsF"]),
            "var": _float(r["Variance"]), "misc": _int(r["EffectMiscValue_0"]), "trigger": _int(r["EffectTriggerSpell"]),
            "period": _int(r["EffectAuraPeriod"])})
    length = {_int(r["ID"]): _int(r["Duration"]) for r in t("SpellDuration")}
    durations = {}
    for r in t("SpellMisc"):
        ms = length.get(_int(r["DurationIndex"]))
        if ms and ms > 0:
            durations[_int(r["SpellID"])] = ms // 1000
    enchants = {}
    for r in t("SpellItemEnchantment"):
        enchants[_int(r["ID"])] = {
            "duration": _int(r["Duration"]),
            "effects": [(_int(r[f"Effect_{i}"]), _int(r[f"EffectPointsMin_{i}"]), _int(r[f"EffectArg_{i}"]))
                        for i in range(3) if _int(r[f"Effect_{i}"])]}

    items = {}
    for iid, s in sparse.items():
        name = s["Display_lang"] or ""
        if not name or any(w in name.lower() for w in SKIP_WORDS) or _int(s["Flags_0"]) & 0x2:  # conjured
            continue
        sub = base[iid]
        cats = categories.get(iid, [])
        eating = any(x in (CAT_FOOD, CAT_DRINK) for x in cats)
        info = {"restore": [], "buffs": [], "duration": None, "oil": False, "wellFed": False,
                "auras": [], "healthCost": 0, "cat": cats[0] if cats else 0}
        for sid in dict.fromkeys(spell_of.get(iid, [])):
            d = decode(sid, effects, durations, enchants, sub, eating=eating)
            info["restore"] += d["restore"]
            info["buffs"] += d["buffs"]
            info["duration"] = info["duration"] or d.get("buffDuration") or d["duration"]
            info["oil"] = info["oil"] or d["oil"]
            info["wellFed"] = info["wellFed"] or d["wellFed"]
            info["auras"] = sorted(set(info["auras"]) | set(d["auras"]))
            info["healthCost"] += d["healthCost"]
        if sub == BANDAGE:
            info["restore"] = [r for r in info["restore"] if r["kind"] == "bandage"]
        elif sub != FOOD and not eating:
            info["restore"] = [r for r in info["restore"] if r["kind"] in ("healing", "mana")]
        if not info["restore"] and not info["buffs"]:
            continue
        items[str(iid)] = {
            "name": name, "quality": _int(s["OverallQualityID"]), "subclass": sub,
            "reqLevel": _int(s["RequiredLevel"]), "reqSkill": SKILL_ALIAS.get(_int(s["RequiredSkill"]), _int(s["RequiredSkill"])),
            "reqSkillRank": _int(s["RequiredSkillRank"]), "classes": _int(s["AllowableClass"]),
            "buy": _int(s["BuyPrice"]), "buyStack": max(1, _int(s["VendorStackCount"])), "sell": _int(s["SellPrice"]),
            **info,
        }
    return {"version": CATALOG_VERSION, "build": build, "locale": locale, "items": items}


def load_catalog(build: str | None, locale: str | None = None) -> dict:
    """Catalog from the cache or built from the DB2 tables, like recipes.load_catalog."""
    locale = locale or i18n.DEFAULT_LOCALE
    path = os.path.join(icons.CACHE, f"consumables-{build}-{locale}.json") if build else None
    if path and os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        if data.get("version") == CATALOG_VERSION:
            return data
    if not build:
        for pattern in (f"consumables-*-{locale}.json", "consumables-*.json"):
            found = sorted(glob.glob(os.path.join(icons.CACHE, pattern)), key=os.path.getmtime)
            if found:
                with open(found[-1], encoding="utf-8") as f:
                    return json.load(f)
    data = build_catalog(build, locale)
    if path and data["items"]:
        os.makedirs(icons.CACHE, exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False)
    return data


# ---------- texts ----------

def fmt_duration(seconds: int | None) -> str | None:
    if not seconds:
        return None
    if seconds >= 3600 and seconds % 3600 == 0:
        return tr("{n} h", n=seconds // 3600)
    if seconds >= 60:
        return tr("{n} min", n=round(seconds / 60))
    return tr("{n} sec.", n=seconds)


def restore_text(r: dict) -> str:
    amount = r["amount"]
    if r["kind"] in ("healing", "mana"):
        low, high = round(amount * (1 - r["var"] / 2)), round(amount * (1 + r["var"] / 2))
        span = f"{low}–{high}" if low != high else str(amount)
        return tr("restores {amount} health", amount=span) if r["kind"] == "healing" else \
            tr("restores {amount} mana", amount=span)
    seconds = r.get("seconds") or 0
    if r["kind"] == "drink":
        return tr("{amount} mana over {seconds} sec.", amount=amount, seconds=seconds)
    return tr("{amount} health over {seconds} sec.", amount=amount, seconds=seconds)


PERCENT = {"spell_crit", "crit"}


def buff_text(buffs: list[dict]) -> str:
    return ", ".join(f"+{b['amount']}{'%' if b['stat'] in PERCENT else ''} {tr(STATS[b['stat']])}" for b in buffs)


# ---------- calculation ----------

def class_allowed(mask: int, class_file: str | None) -> bool:
    cid = CLASS_ID.get(class_file or "")
    return mask in (0, -1) or not cid or bool(mask & (1 << (cid - 1)))


def craft_sources(catalog: dict, char: dict) -> tuple[dict[int, int], set[int]]:
    """item -> skill line of a recipe that makes it, and the items the character can craft (learned)."""
    made_by: dict[int, int] = {}
    for line, recs in (catalog.get("lines") or {}).items():
        for rec in recs.values():
            made_by.setdefault(rec["output"], int(line))
    learned = set()
    for scan in (char.get("recipes") or {}).values():
        for rec in (scan.get("recipes") or {}).values():
            if rec.get("learned") and rec.get("output"):
                learned.add(int(rec["output"]))
    return made_by, learned


def offer(iid: int, it: dict, market: recipes.Market, vendor: dict) -> dict:
    """AH and vendor price of one item: cheapest of the two as "price"."""
    ah = market.ah(iid)
    seen = vendor.get(iid) or vendor.get(str(iid))
    vp, npc = None, None
    if isinstance(seen, dict) and seen.get("price") and not seen.get("limited"):
        vp, npc = seen["price"], seen.get("npc")
    elif iid in VENDOR_FOOD and it.get("buy"):
        vp = it["buy"] / it.get("buyStack", 1)
    options = [(p, src) for p, src in ((ah["price"] if ah else None, "ah"), (vp, "vendor")) if p]
    price, source = min(options) if options else (None, None)
    return {"price": None if price is None else round(price), "source": source,
            "ahPrice": ah["price"] if ah else None, "listed": ah["listed"] if ah else 0, "ahAge": ah["age"] if ah else None,
            "vendorPrice": None if vp is None else round(vp), "vendorNpc": npc}


def row_of(iid: int, it: dict, amount: int, effect: str, ctx: dict) -> dict:
    o = offer(iid, it, ctx["market"], ctx["vendor"])
    owned = ctx["owned"].get(iid, 0)
    line = ctx["madeBy"].get(iid)
    craft = None
    if line:
        craft = {"profession": ctx["profNames"].get(line) or (tr(PROFESSION[line]) if line in PROFESSION else str(line)),
                 "learned": iid in ctx["learned"]}
    available = bool(owned or o["price"] or (craft and craft["learned"]))
    sub = it["subclass"]
    kind = tr(OIL_LABEL) if it.get("oil") else tr(TYPE_LABEL.get(sub, TYPE_LABEL[OTHER]))
    if it.get("wellFed"):
        kind = tr("Buff food")
    skill = None
    if it.get("reqSkill") and it.get("reqSkillRank"):
        line = it["reqSkill"]
        have = ctx["skills"].get(line, 0)
        skill = {"name": ctx["profNames"].get(line) or (tr(PROFESSION[line]) if line in PROFESSION else str(line)),
                 "rank": it["reqSkillRank"], "have": have, "ok": have >= it["reqSkillRank"]}
    return {"id": iid, "name": it["name"], "quality": it["quality"], "type": kind, "reqLevel": it["reqLevel"],
            "reqSkill": skill, "amount": amount, "effect": effect, "duration": fmt_duration(it.get("duration")),
            "owned": owned, "craft": craft, "available": available, "isNew": iid > NEW_ID,
            "alternatives": [], **o}


def usable(it: dict, level: int, ctx: dict) -> bool:
    if it["reqLevel"] > level or not class_allowed(it["classes"], ctx["classFile"]):
        return False
    return not (it.get("reqSkill") and it.get("reqSkillRank") and ctx["skills"].get(it["reqSkill"], 0) < it["reqSkillRank"])


def obtainable(row: dict) -> bool:
    """Something the character can get at all: owned, sold, or made by a profession."""
    return row["available"] or bool(row["craft"])


def merge_alike(rows: list[dict]) -> list[dict]:
    """Items with the same effect (bread, meat, fish, cheese ... of one level) as one row: the first of them
    (the one you can get, then the cheapest) stays, the others become its alternatives. Items in the bags or
    craftable by the character keep their own row."""
    out, seen = [], {}
    for r in rows:
        key = (r["amount"], r["effect"], r["available"])
        own = r["owned"] or (r["craft"] or {}).get("learned")
        if key in seen and not own:
            seen[key]["alternatives"].append({"id": r["id"], "name": r["name"], "price": r["price"], "source": r["source"]})
            continue
        seen.setdefault(key, r)
        out.append(r)
    return out


def rank(rows: list[dict], top: int) -> tuple[list[dict], dict | None]:
    """Strongest first, cut to `top`; the strongest one you can get now is "best" (returned even if the
    stronger ones you cannot get push it out of the list)."""
    rows = merge_alike(sorted(rows, key=lambda r: (-r["amount"], not r["available"], r["price"] or 0)))
    best = next((r for r in rows if r["available"]), None)
    if best:
        best["best"] = True
    return (rows[:top] if top else rows), best


def next_better(candidates: list[dict], best_amount: int) -> dict | None:
    """The first item above the level (or skill) that beats the best one now."""
    later = [r for r in candidates if r["amount"] > best_amount and obtainable(r)]
    if not later:
        return None
    r = min(later, key=lambda r: (r["reqLevel"], (r["reqSkill"] or {}).get("rank", 0), -r["amount"]))
    return {"id": r["id"], "name": r["name"], "quality": r["quality"], "reqLevel": r["reqLevel"],
            "reqSkill": r["reqSkill"], "amount": r["amount"], "effect": r["effect"]}


def context(c: dict, recipe_catalog: dict, prices: dict, vendor: dict) -> dict:
    """What row_of needs to know about the character and the market."""
    ch = c.get("character") or {}
    made_by, learned = craft_sources(recipe_catalog, c)
    professions = (c.get("skills") or {}).get("professions") or []
    return {
        "market": recipes.Market(prices, vendor, {}), "vendor": vendor, "owned": recipes.owned_counts(c),
        "madeBy": made_by, "learned": learned, "classFile": ch.get("classFile"),
        "skills": {p.get("skillLine"): p.get("rank") or 0 for p in professions},
        "profNames": {p.get("skillLine"): p.get("name") for p in professions if p.get("name")},
    }


def evaluate(c: dict, catalog: dict, recipe_catalog: dict, prices: dict, vendor: dict, top: int = 5,
             all_buffs: bool = False, start=None, seconds: float = 60.0, metrics: list | None = None) -> dict:
    """The page for one character export `c`. With `start` (a function that starts the combat engine for a
    metric, see rotations.character) also the consumables for a fight of `seconds`, rated by the engine."""
    ch = c.get("character") or {}
    level = ch.get("level") or 1
    ctx = context(c, recipe_catalog, prices, vendor)

    now: dict[str, list] = {k: [] for k, _ in CATEGORIES}
    later: dict[str, list] = {k: [] for k, _ in CATEGORIES}
    buffs_now: dict[str, list] = {k: [] for k in STATS}
    buffs_later: dict[str, list] = {k: [] for k in STATS}
    for key, it in catalog["items"].items():
        iid = int(key)
        if not class_allowed(it["classes"], ctx["classFile"]):
            continue
        ok = usable(it, level, ctx)
        for r in it["restore"]:
            row = row_of(iid, it, r["amount"], restore_text(r), ctx)
            if obtainable(row):
                (now if ok else later)[r["kind"]].append(row)
        for b in it["buffs"]:
            row = row_of(iid, it, b["amount"], buff_text(it["buffs"]), ctx)
            if obtainable(row):
                (buffs_now if ok else buffs_later)[b["stat"]].append(row)

    def group(key: str, label: str, rows_now: list, rows_later: list) -> dict:
        rows, best = rank(rows_now, top)
        return {"key": key, "label": tr(label), "items": rows,
                "next": next_better(rows_later, best["amount"] if best else 0)}

    categories = [group(key, label, now[key], later[key]) for key, label in CATEGORIES]
    relevant = RELEVANT.get(ch.get("classFile") or "", set(STATS))
    buffs = [{**group(key, label, buffs_now[key], buffs_later[key]), "relevant": key in relevant}
             for key, label in STATS.items()
             if (buffs_now[key] or buffs_later[key]) and (all_buffs or key in relevant)]
    sd = writs.scan_date(prices) if prices else None
    out = {"name": ch.get("name"), "level": level, "class": ch.get("class"), "classFile": ch.get("classFile"),
           "updated": c.get("updated"), "scanDate": sd.isoformat() if sd else None, "build": catalog.get("build"),
           "locale": catalog.get("locale"), "vendorPrices": len(vendor), "recipesScanned": bool(c.get("recipes")),
           "categories": categories, "buffs": buffs}
    if start:
        out["plans"] = fight_plans(c, catalog, ctx, start, seconds, top, metrics)
    return out


# ---------- consumables in a fight, rated by the combat engine ----------

MAX_HEALTH_COST = 0.5  # of the health pool: what costs more is never part of the set
POTION_COOLDOWN = 120  # seconds; the potions share one, the runes and roots another
EPS = 0.05  # a consumable that adds less to the metric is not worth a place in the set

# The kinds in the order of the page: key, title, what can be active at once, and what of that is an
# assumption (None: the game's data says it). Sources: the flasks' spell text ("You can only have the effect
# of one flask at a time"), the cooldown categories of the item effects (potions 4, runes and roots 1153,
# Zanza 79, Blasted Lands 103, food 11), no "Battle Elixir"/"Guardian Elixir" wording in any elixir text,
# and two guides that disagree about the rest (rankedboost.com, checked 2026-11-10: several elixirs, no
# battle or guardian slots, a flask next to them, one elixir per stat "not confirmed"; method.gg, written
# from the beta: flasks now stack with elixirs).
KINDS = [
    ("flask", mark("Flask"),
     mark("One flask at a time; its own text says so. It works next to elixirs, buff food and a weapon oil."), None),
    ("elixir", mark("Elixirs"),
     mark("Elixirs for different stats work together: the game has no battle and guardian elixirs here."),
     mark("Whether two elixirs for the same stat add up is not confirmed for Forever. The set takes one per stat.")),
    ("food", mark("Buff food"), mark("One Well Fed buff at a time: the next meal replaces it."), None),
    ("weapon", mark("Weapon oil"),
     mark("One temporary enchantment per weapon. By their texts a warlock's Spellstone and Firestone take the "
          "same place."), None),
    ("scroll", mark("Scrolls"), mark("One scroll per stat."),
     mark("In the original game a scroll did not add to the class buff of the same stat (Arcane Intellect, "
          "Divine Spirit, Power Word: Fortitude). Not checked for Forever: the set counts the scroll in full.")),
    ("potion", mark("Potions in the fight"),
     mark("All potions share one cooldown of 2 minutes. The gain is what the potion gives spread over the "
          "fight: its mana or health, or its buff for the seconds it lasts."), None),
    ("rune", mark("Runes and roots"),
     mark("Runes and the roots from Felwood share a cooldown of 2 minutes of their own, next to the potions."), None),
    ("other", mark("Other buffs"),
     mark("The buffs from the Blasted Lands share a cooldown of an hour, and one Zanza potion works at a time."),
     mark("For the others nothing says what they exclude: the set counts each of them on top.")),
]
# A consumable's stat -> the combat engine's keys (simengine, engine.ApplyDelta). "sp" is "damage done by
# spells" here (aura 13), which does not heal; "crit" is melee crit and has no key.
ENGINE_KEYS = {
    "sp": ("spDamage",), "sp_holy": ("spHoly",), "sp_fire": ("spFire",), "sp_nature": ("spNature",),
    "sp_frost": ("spFrost",), "sp_shadow": ("spShadow",), "sp_arcane": ("spArcane",), "healing": ("healing",),
    "spell_crit": ("crit",), "intellect": ("int",), "spirit": ("spi",), "stamina": ("sta",), "mp5": ("mp5",),
    "hp5": ("hp5",), "all_stats": ("int", "sta", "spi"),
}
PLAN_TITLES = {"dps": mark("Damage"), "hps": mark("Healing")}
PLAN_UNITS = {"dps": "DPS", "hps": "HPS"}


def fight_kind(it: dict) -> str | None:
    """The kind (KINDS) of a consumable that works during a fight; None for eating, drinking, bandages and
    items whose buff has no end the tables know (they are no consumables: runes that stay on the gear)."""
    cat, sub = it.get("cat") or 0, it["subclass"]
    restores = any(r["kind"] in ("healing", "mana") for r in it["restore"])
    if cat == CAT_RUNE:
        return "rune" if restores else None
    if cat == CAT_POTION or sub == POTION:
        return "potion" if restores or (it["buffs"] and it.get("duration")) else None
    if not it["buffs"] or not it.get("duration"):
        return None
    if it.get("wellFed") or sub == FOOD:
        return "food"
    if it.get("oil"):
        return "weapon"
    return {FLASK: "flask", ELIXIR: "elixir", SCROLL: "scroll"}.get(sub, "other")


def fight_slots(iid: int, it: dict, kind: str) -> list[str]:
    """What the item occupies: two consumables that share a slot cannot both be in the set."""
    if kind in ("flask", "food", "weapon", "potion", "rune"):
        return [kind]
    if kind in ("elixir", "scroll"):
        return sorted({f"{kind}:{b['stat']}" for b in it["buffs"]})
    if it.get("cat") in (CAT_ZANZA, CAT_BLASTED):
        return [f"cat:{it['cat']}"]
    return [f"item:{iid}"]


def uses_in(seconds: float) -> int:
    """How often something with the potion cooldown fits into a fight: at its start and every 2 minutes."""
    return 1 + int(max(0.0, seconds - 0.001) // POTION_COOLDOWN)


def fight_delta(it: dict, kind: str, seconds: float, hp_per_stamina: float = 10) -> dict:
    """What the item adds over a fight of `seconds`, in the combat engine's stat keys. A buff shorter than
    the fight counts for its share of the time; mana and health from a potion or a rune count as mana and
    health per 5 seconds over the fight (to the engine's mana budget that is the same), a rune's damage to
    its user as health lost."""
    out: dict[str, float] = {}

    def add(key: str, value: float) -> None:
        out[key] = out.get(key, 0.0) + value

    seconds = max(1.0, seconds)
    duration = it.get("duration") or 0
    share = min(1.0, duration / seconds) if duration else 1.0
    if kind in ("potion", "rune"):
        uses = uses_in(seconds)
        share = sum(min(duration, seconds - n * POTION_COOLDOWN) for n in range(uses)) / seconds
        for r in it["restore"]:
            if r["kind"] in ("healing", "mana"):
                add("mp5" if r["kind"] == "mana" else "hp5", r["amount"] * uses * 5 / seconds)
        if it.get("healthCost"):
            add("hp5", -it["healthCost"] * uses * 5 / seconds)
    for stat, amount in dict.fromkeys((b["stat"], b["amount"]) for b in it["buffs"]):
        if stat == "health":
            add("sta", amount * share / (hp_per_stamina or 10))
        for key in ENGINE_KEYS.get(stat, ()):
            add(key, amount * share)
    return {k: round(v, 3) for k, v in out.items() if round(v, 3)}


def add_delta(a: dict, b: dict) -> dict:
    out = dict(a)
    for k, v in b.items():
        out[k] = round(out.get(k, 0.0) + v, 3)
    return out


def fight_candidates(c: dict, catalog: dict, ctx: dict, seconds: float) -> list[dict]:
    """Every consumable for a fight the character can use and get, as a row with its kind, slots and delta.
    A buff that was on the character when the snapshot was taken is in the sheet's numbers already, so it
    holds its slot (`onYou` = the buff's name). If one item gives that buff, the item is "active" and adds
    nothing; a buff many items give (Well Fed) does not say which one it was, so those rows keep what they
    would add alone. One that costs more than half of the character's health (a Dark Rune at a low level)
    is "tooCostly" and never part of the set."""
    ch = c.get("character") or {}
    level = ch.get("level") or 1
    hp_per_stamina = (ch.get("stats") or {}).get("hpPerStamina") or 10
    health = (ch.get("stats") or {}).get("healthMax") or 0
    on = {b.get("id"): b.get("name") or "" for b in ch.get("buffs") or [] if isinstance(b, dict)}
    givers: dict[int, int] = {}  # buff spell -> how many consumables give it
    for it in catalog["items"].values():
        if fight_kind(it):
            for aura in it.get("auras") or []:
                givers[aura] = givers.get(aura, 0) + 1
    rows = []
    for key, it in catalog["items"].items():
        iid, kind = int(key), fight_kind(it)
        if not kind or not class_allowed(it["classes"], ctx["classFile"]):
            continue
        mine = [a for a in it.get("auras") or [] if a in on]
        active = any(givers[a] == 1 for a in mine)
        if not mine and not usable(it, level, ctx):
            continue
        effect = buff_text(list({(b["stat"], b["amount"]): b for b in it["buffs"]}.values())) if it["buffs"] else \
            ", ".join(restore_text(r) for r in it["restore"] if r["kind"] in ("healing", "mana"))
        if it.get("healthCost"):
            effect += ", " + tr("costs about {n} health", n=it["healthCost"])
        row = row_of(iid, it, 0, effect, ctx)
        if not active and not obtainable(row):
            continue
        if mine:
            row["onYou"] = on[mine[0]] or row["name"]
        del row["amount"], row["alternatives"]
        row.update(kind=kind, slots=fight_slots(iid, it, kind), active=active, gain=0.0,
                   delta={} if active else fight_delta(it, kind, seconds, hp_per_stamina))
        if kind in ("potion", "rune"):
            row["uses"] = uses_in(seconds)
        if it.get("healthCost"):
            row["healthPerSec"] = round(it["healthCost"] * row.get("uses", 1) / max(1.0, seconds), 2)
            row["tooCostly"] = bool(health) and it["healthCost"] > MAX_HEALTH_COST * health
        rows.append(row)
    return rows


def choose(eng, rows: list[dict], healer: bool = False) -> tuple[list[dict], dict, float]:
    """The best set: what each row adds alone (`gain`), then greedy by what a row adds on top of the ones
    chosen before it (`adds`; gains do not add up: once the mana lasts, more mana is worth nothing), among
    the rows the character can get now whose slots are free. Of rows that add the same, the one in the bags
    wins, then the cheaper one. The engine picks the best rotation again for every set it is asked about
    (more mana can make another heal rank or filler the best). For a `healer` the health an item costs is
    taken off what it adds: somebody has to heal it (the engine does the same with Dark Sacrifice; for a
    damage dealer the cost is in the delta as health lost). Returns the chosen rows in order, their summed
    delta and what they add together."""
    def cost(r: dict) -> float:
        return r.get("healthPerSec", 0.0) if healer else 0.0

    alone = eng.batch([(str(r["id"]), r["delta"]) for r in rows if r["delta"]], reoptimize=True)
    used: dict[str, str] = {}  # slot -> name of what holds it
    for r in rows:
        r["gain"] = round(alone.get(str(r["id"]), 0.0) - (cost(r) if r["delta"] else 0.0), 2)
        if r.get("onYou"):
            used.update({s: r["onYou"] for s in r["slots"]})
    held = dict(used)  # the slots a buff of the snapshot holds
    chosen, total, gained, paid = [], {}, 0.0, 0.0  # paid: the health the set costs, per second
    pool = [r for r in rows if r["available"] and r["delta"] and not r["active"] and not r.get("tooCostly")]
    while True:
        pool = [r for r in pool if not any(s in used for s in r["slots"])]
        if not pool:
            break
        gains = eng.batch([(str(r["id"]), add_delta(total, r["delta"])) for r in pool], reoptimize=True)

        def more(r: dict) -> float:
            return gains.get(str(r["id"]), 0.0) - paid - cost(r) - gained

        best = max(pool, key=lambda r: (round(more(r), 1), bool(r["owned"]), -(r["price"] or 0), -r["id"]))
        adds = more(best)
        if adds < EPS:
            break
        best["chosen"], best["adds"] = len(chosen) + 1, round(adds, 2)
        chosen.append(best)
        total, gained, paid = add_delta(total, best["delta"]), gained + adds, paid + cost(best)
        used.update({s: best["name"] for s in best["slots"]})
    for r in rows:
        if r.get("chosen") or r["active"]:
            continue
        buff = next((held[s] for s in r["slots"] if s in held), None)
        holder = next((used[s] for s in r["slots"] if s in used), None)
        if buff:
            r["onYou"] = buff
        elif holder:
            r["blockedBy"] = holder
    return chosen, total, gained


def fight_plan(metric: str, start, rows: list[dict], seconds: float, top: int) -> dict:
    """The consumables for one metric: the rows per kind with their gain, the best set and the metric with
    it (what a calculation started with the set's `delta` as buffs has as its baseline)."""
    rows = [dict(r) for r in rows]
    eng = start(metric)
    chosen, total, gained = choose(eng, rows, healer=metric == "hps")
    base = eng.baseline.get("value") or 0.0
    value = base + gained
    kinds = []
    for key, title, rule, assumed in KINDS:
        mine = sorted((r for r in rows if r["kind"] == key),
                      key=lambda r: (-r["gain"], not r["available"], r["price"] or 0, r["id"]))
        if not mine:
            continue
        keep = [r for i, r in enumerate(mine) if not top or i < top or r.get("chosen") or r["active"]]
        kinds.append({"key": key, "label": tr(title), "rule": tr(rule), "assumed": tr(assumed) if assumed else None,
                      "items": keep, "more": len(mine) - len(keep)})
    unit = PLAN_UNITS.get(metric, "")
    return {"metric": metric, "unit": unit, "title": tr(PLAN_TITLES.get(metric, metric)),
            "seconds": eng.fight.get("seconds") or seconds, "targetDelta": eng.fight.get("targetDelta"),
            "base": round(base, 2), "value": round(value, 2), "gain": round(value - base, 2),
            "gainPct": round((value - base) / base * 100, 1) if base else 0.0,
            "cost": sum(r["price"] or 0 for r in chosen if not r["owned"]),
            "unpriced": sum(1 for r in chosen if not r["owned"] and not r["price"]),
            "chosen": chosen, "kinds": kinds, "delta": total, "engine": eng.version,
            "active": list(dict.fromkeys(r["onYou"] for r in rows if r.get("onYou")))}


def fight_plans(c: dict, catalog: dict, ctx: dict, start, seconds: float = 60.0, top: int = 5,
                metrics: list | None = None) -> list[dict]:
    """One plan per metric of the class (the spec's own first; the time until the mana is gone only when
    asked for by name)."""
    import simengine  # the engine's metrics; rotations.py and upgrades.py import this module's users
    ch = c.get("character") or {}
    class_file = ch.get("classFile") or ""
    first = simengine.default_metric(class_file, simengine.spec_of(c))
    if not metrics:
        metrics = sorted((m for m in simengine.METRICS.get(class_file) or [] if m != "ttoom"),
                         key=lambda m: m != first)
    rows = fight_candidates(c, catalog, ctx, seconds)
    return [fight_plan(m or first, start, rows, seconds, top) for m in metrics]


def compute(wow: str = writs.DEFAULT_WOW, char: str = "", top: int = 5, all_buffs: bool = False,
            engine: str = "off", fight_seconds: float = 60.0, target_delta: int = -1,
            metrics: list | None = None) -> dict:
    """The page as plain data. `engine` "on" or "auto" adds the consumables for a fight, rated by the combat
    engine (`plans`); "auto" leaves them out when the engine does not run or does not model the class and
    says why in `planNote`."""
    files = writs.find_chars(wow, char)
    if not files:
        raise FileNotFoundError(tr("No WoWMCP export found for \"{char}\".", char=char))
    c = upgrades.load_char(files[0])
    try:
        prices = writs.load_prices(writs.find_auctionator(wow))
    except (FileNotFoundError, LookupError):
        prices = {}  # without an AH scan the page still shows vendor food, bags and recipes
    vendor: dict = {}
    for path in glob.glob(os.path.join(wow, "WTF", "Account", "*", "SavedVariables", "WoWMCP.lua")):
        vendor.update((upgrades.load_sv(path, "WoWMCP_Account") or {}).get("vendor") or {})
    cl = c.get("client") or {}
    build = f"{cl['version']}.{int(cl['build'])}" if cl.get("version") and cl.get("build") else icons.client_build(wow)
    # Names in the catalog are in the client's language, like everything else that comes from the game.
    locale = cl.get("locale") or i18n.client_locale(wow) or i18n.DEFAULT_LOCALE
    catalog = load_catalog(build, locale)
    if not catalog["items"]:
        raise RuntimeError(tr("The item data could not be loaded from wago.tools. Check the network connection "
                              "and refresh."))
    start, note = None, None
    if engine != "off":
        import rotations  # imports upgrades and the engine; this module is imported by neither
        try:
            _, _, start = rotations.character(wow, char, fight_seconds, target_delta)
        except LookupError as e:
            if engine == "on":
                raise
            note = str(e)
    recipe_catalog = recipes.load_catalog(build, locale)
    try:
        out = evaluate(c, catalog, recipe_catalog, prices, vendor, top, all_buffs, start, fight_seconds, metrics)
    except LookupError as e:  # the engine did not run
        if engine == "on" or not start:
            raise
        out, note = evaluate(c, catalog, recipe_catalog, prices, vendor, top, all_buffs), str(e)
    if note:
        out["planNote"] = note
    out["character"] = os.path.basename(os.path.dirname(os.path.dirname(files[0])))
    return out


def fight_buffs(wow: str, char: str, metric: str = "", fight_seconds: float = 60.0, target_delta: int = -1) -> dict:
    """The best set for one metric as what the other calculations need (upgrades.py, rotations.py): the
    engine's stat delta, the names and what the set adds. Raises LookupError when the engine does not run."""
    plan = compute(wow, char, engine="on", fight_seconds=fight_seconds, target_delta=target_delta,
                   metrics=[metric])["plans"][0]
    names = [r["name"] for r in plan["chosen"]]
    text = tr("With consumables: {names} (+{gain:.1f} {unit}).", names=", ".join(names), gain=plan["gain"],
              unit=plan["unit"]) if names else tr("No consumable you can get now adds anything.")
    return {"delta": plan["delta"], "names": names, "gain": plan["gain"], "unit": plan["unit"],
            "metric": plan["metric"], "text": text}


# ---------- CLI ----------

def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--wow", default=writs.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--top", type=int, default=5, help="items per category (0 = all)")
    ap.add_argument("--all-buffs", action="store_true", help="also buffs that do not help the class")
    ap.add_argument("--engine", choices=["auto", "on", "off"], default="auto",
                    help="rate the consumables for a fight with the combat engine (auto: when it runs)")
    ap.add_argument("--fight-seconds", type=float, default=60, help="length of the fight the engine plays")
    ap.add_argument("--target-delta", type=int, default=-1, choices=[-1, 0, 1, 2, 3],
                    help="enemy levels above you (default: +2 while leveling, +3 at 60)")
    ap.add_argument("--json", action="store_true")
    i18n.add_argument(ap)
    a = ap.parse_args()
    i18n.set_language(a.lang, a.wow)
    try:
        r = compute(a.wow, a.char, a.top, a.all_buffs, a.engine, a.fight_seconds, a.target_delta)
    except (FileNotFoundError, LookupError, RuntimeError) as e:
        sys.exit(str(e))
    sys.stdout.reconfigure(encoding="utf-8")
    if a.json:
        print(json.dumps(r, ensure_ascii=False))
        return
    g = lambda c: "–" if c is None else writs.fmt(c)  # noqa: E731
    print(tr("{char}  |  level {level}  |  AH scan {date}", char=r["character"], level=r["level"], date=r["scanDate"] or "–"))

    def show(group: dict) -> None:
        print(f"\n== {group['label']} ==")
        for x in group["items"]:
            flag = "*" if x.get("best") else " "
            have = tr("{n} in bags", n=x["owned"]) if x["owned"] else ""
            craft = (x["craft"] or {}).get("profession") or ""
            print(f" {flag}{x['name'][:36]:<36} {x['effect'][:44]:<44} {g(x['price']):>10}  {have}  {craft}")
        if group["next"]:
            n = group["next"]
            if n["reqSkill"] and not n["reqSkill"]["ok"]:
                print("   " + tr("next: {name} with {skill} {rank}", name=n["name"], skill=n["reqSkill"]["name"],
                               rank=n["reqSkill"]["rank"]))
            else:
                print("   " + tr("next: {name} from level {level}", name=n["name"], level=n["reqLevel"]))

    for group in r["categories"] + r["buffs"]:
        show(group)
    if r.get("planNote"):
        print("\n" + tr("No consumables for a fight: {why}", why=r["planNote"]))
    for plan in r.get("plans") or []:
        show_plan(plan, g)


def show_plan(plan: dict, g) -> None:
    """One plan of the combat engine: the best set, then every kind with its rows."""
    unit = plan["unit"]
    print("\n" + tr("=== Consumables for a fight of {seconds:.0f} s: {title} ===", seconds=plan["seconds"],
                    title=plan["title"]))
    print(tr("{base:.1f} {unit} without, {value:.1f} {unit} with the best set ({gain:+.1f}, {pct:+.1f} %), "
             "to buy: {cost}", base=plan["base"], value=plan["value"], unit=unit, gain=plan["gain"],
             pct=plan["gainPct"], cost=g(plan["cost"])))
    if plan["active"]:
        print(tr("On you at the snapshot, so already in the numbers: {names}", names=", ".join(plan["active"])))
    labels = {k["key"]: k["label"] for k in plan["kinds"]}
    for x in plan["chosen"]:
        print(f" {x['chosen']:>2}. {x['name'][:36]:<36} {labels[x['kind']][:20]:<20} {x['adds']:>+7.1f} {unit}  "
              f"{g(x['price']):>10}  {have(x)}")
    for kind in plan["kinds"]:
        print(f"\n-- {kind['label']}: {kind['rule']}" + (f" ({kind['assumed']})" if kind["assumed"] else ""))
        for x in kind["items"]:
            flag = "*" if x.get("chosen") else " "
            note = tr("on you") if x["active"] else tr("{name} is on you already", name=x["onYou"]) if x.get("onYou") \
                else tr("not in the AH") if not x["available"] \
                else tr("costs more than half your health") if x.get("tooCostly") \
                else tr("instead: {name}", name=x["blockedBy"]) if x.get("blockedBy") else ""
            print(f" {flag}{x['name'][:36]:<36} {x['effect'][:40]:<40} {x['gain']:>+7.1f} {unit}  {g(x['price']):>10}  "
                  f"{have(x)}  {note}")
        if kind["more"]:
            print("   " + tr("… and {n} more", n=kind["more"]))


def have(x: dict) -> str:
    return tr("{n} in bags", n=x["owned"]) if x["owned"] else ""


if __name__ == "__main__":
    main()
