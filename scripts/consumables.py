# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Consumables for the level of a character: the strongest healing and mana potions, food, drinks, bandages
and buffs (elixirs, flasks, scrolls, weapon oils, buff food) the character can use now, with the price in
the auction house or at a vendor, what is in the bags and which ones the character can craft.

    uv run scripts/consumables.py --char Malgruk [--top 5] [--all-buffs] [--json] [--lang en|de]

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
craftable with a learned recipe. Stacking rules between elixirs are not known for Forever and not checked.
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

CATALOG_VERSION = 1
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

def decode(sid: int, effects: dict, durations: dict, enchants: dict, subclass: int, depth: int = 0) -> dict:
    """What a spell does: {"restore": [...], "buffs": [...], "duration": seconds, "oil": bool}. Follows
    triggered spells (Forever's "Nutritious Food" triggers the eating spell and, after 10 s, Well Fed)."""
    out = {"restore": [], "buffs": [], "duration": durations.get(sid), "oil": False, "wellFed": False}
    if depth > 2:
        return out
    for e in effects.get(sid, []):
        kind, aura, bp, misc = e["effect"], e["aura"], e["bp"], e["misc"]
        if kind == 10 and bp > 0:  # HEAL
            out["restore"].append({"kind": "healing", "amount": bp, "var": e["var"]})
        elif kind == 30 and misc == 0 and bp > 0:  # ENERGIZE mana
            out["restore"].append({"kind": "mana", "amount": bp, "var": e["var"]})
        elif kind == 64 and e["trigger"]:  # TRIGGER_SPELL
            sub = decode(e["trigger"], effects, durations, enchants, subclass, depth + 1)
            out["restore"] += sub["restore"]
            out["buffs"] += sub["buffs"]
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
            if subclass == FOOD and aura in (84, 85) and depth <= 1:
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
            elif aura in AURA and bp > 0:
                stat = AURA[aura](misc)
                if stat:
                    out["buffs"].append({"stat": stat, "amount": bp})
    return out


def build_catalog(build: str | None, locale: str | None = None) -> dict:
    """{"items": {itemId: consumable}} from the DB2 tables, names in `locale`."""
    t = lambda name: icons.table(name, build, locale)  # noqa: E731
    base = {_int(r["ID"]): _int(r["SubclassID"]) for r in t("Item") if _int(r["ClassID"]) == 0}
    sparse = {_int(r["ID"]): r for r in t("ItemSparse") if _int(r["ID"]) in base}
    effect_spell = {_int(r["ID"]): _int(r["SpellID"]) for r in t("ItemEffect")}
    spell_of: dict[int, list[int]] = {}
    for r in t("ItemXItemEffect"):
        iid = _int(r["ItemID"])
        if iid in sparse and effect_spell.get(_int(r["ItemEffectID"])):
            spell_of.setdefault(iid, []).append(effect_spell[_int(r["ItemEffectID"])])
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
        info = {"restore": [], "buffs": [], "duration": None, "oil": False, "wellFed": False}
        for sid in spell_of.get(iid, []):
            d = decode(sid, effects, durations, enchants, sub)
            info["restore"] += d["restore"]
            info["buffs"] += d["buffs"]
            info["duration"] = info["duration"] or d.get("buffDuration") or d["duration"]
            info["oil"] = info["oil"] or d["oil"]
            info["wellFed"] = info["wellFed"] or d["wellFed"]
        if sub == BANDAGE:
            info["restore"] = [r for r in info["restore"] if r["kind"] == "bandage"]
        elif sub != FOOD:
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


def evaluate(c: dict, catalog: dict, recipe_catalog: dict, prices: dict, vendor: dict, top: int = 5,
             all_buffs: bool = False) -> dict:
    """The page for one character export `c`."""
    ch = c.get("character") or {}
    level = ch.get("level") or 1
    made_by, learned = craft_sources(recipe_catalog, c)
    professions = (c.get("skills") or {}).get("professions") or []
    ctx = {
        "market": recipes.Market(prices, vendor, {}), "vendor": vendor, "owned": recipes.owned_counts(c),
        "madeBy": made_by, "learned": learned, "classFile": ch.get("classFile"),
        "skills": {p.get("skillLine"): p.get("rank") or 0 for p in professions},
        "profNames": {p.get("skillLine"): p.get("name") for p in professions if p.get("name")},
    }

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
    return {"name": ch.get("name"), "level": level, "class": ch.get("class"), "classFile": ch.get("classFile"),
            "updated": c.get("updated"), "scanDate": sd.isoformat() if sd else None, "build": catalog.get("build"),
            "locale": catalog.get("locale"), "vendorPrices": len(vendor), "recipesScanned": bool(c.get("recipes")),
            "categories": categories, "buffs": buffs}


def compute(wow: str = writs.DEFAULT_WOW, char: str = "", top: int = 5, all_buffs: bool = False) -> dict:
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
    out = evaluate(c, catalog, recipes.load_catalog(build, locale), prices, vendor, top, all_buffs)
    out["character"] = os.path.basename(os.path.dirname(os.path.dirname(files[0])))
    return out


# ---------- CLI ----------

def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--wow", default=writs.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--top", type=int, default=5, help="items per category (0 = all)")
    ap.add_argument("--all-buffs", action="store_true", help="also buffs that do not help the class")
    ap.add_argument("--json", action="store_true")
    i18n.add_argument(ap)
    a = ap.parse_args()
    i18n.set_language(a.lang, a.wow)
    try:
        r = compute(a.wow, a.char, a.top, a.all_buffs)
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


if __name__ == "__main__":
    main()
