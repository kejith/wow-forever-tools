# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Berufsrezepte: Was bringt jedes gelernte Rezept gerade im AH, und welche Rezepte lohnen sich noch mehr?

    uv run scripts/recipes.py --char Malgruk [--beruf Alchimie] [--top 15] [--json]

Quellen:
- Rezeptkatalog aus Forevers DB2-Tabellen (wago.tools, Produkt wowf): SkillLineAbility (Rezepte pro Beruf,
  Farbgrenzen), SpellReagents, SpellEffect (hergestelltes Item, Menge, Lern-Effekt der Rezept-Items),
  ItemEffect + ItemXItemEffect (welches Item welches Rezept lehrt), ItemSparse (Namen, Händlerpreise,
  benötigter Skill, Bindung), SpellCooldowns (Transmutationen). Wird pro Build als JSON zwischengespeichert.
- Gelernte Rezepte aus dem Addon (ab 1.4): WoWMCP_Char.recipes, gelesen beim Öffnen des Berufsfensters.
  Ohne diesen Export ist „gelernt“ unbekannt.
- Preise aus Auctionator (Mindest-Sofortkauf beim letzten Scan), Händlerpreise aus WoWMCP_Account.vendor
  (beim Händlerbesuch gelesen) oder aus ItemSparse für Reagenzien, die jeder Handwerkshändler führt.

Gewinn pro Herstellung = AH-Preis × Menge × (1 − 5 % AH-Gebühr) − Reagenzien. Reagenzien zum günstigeren
Preis aus AH und Händler; eigene Reagenzien zählen zum Marktpreis (das, was du beim Verkauf bekämst).
"""

from __future__ import annotations

import argparse
import csv
import glob
import json
import os
import statistics
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import icons  # noqa: E402  (DB2-Download und Build)
import upgrades  # noqa: E402  (SavedVariables lesen)
import verfuegungen as v  # noqa: E402  (Auctionator)

AH_CUT = 0.05
CATALOG_VERSION = 2
GATHERING = {182, 186, 393, 356}  # Kräuterkunde, Bergbau, Kürschnerei, Angeln: keine Herstellung
NEW_ID = 200000

# Reagenzien, die jeder Handwerks- oder Gemischtwarenhändler führt. Preis = ItemSparse.BuyPrice / VendorStackCount,
# bis das Addon beim Händlerbesuch den echten Preis liest.
VENDOR_ITEMS = {
    3371, 3372, 8925, 18256,  # Phiolen
    2678, 2692, 3713, 159, 1179, 2596, 4289, 8150,  # Kochen: Gewürze, Wasser, Milch, Bier, Salz
    2320, 2321, 4291, 8343, 14341,  # Fäden
    2324, 2604, 6260, 4340, 4341, 4342, 2325, 10290,  # Bleiche und Farbstoffe
    3857, 2880, 3466, 18567,  # Kohle, Fluxus
    6217, 4470, 4399, 4400, 10647, 10648,  # Kupferrute, Holz, Kanister, Gelatine, Pergament
    6530,  # Nachtkriecher
}


def _int(x) -> int:
    try:
        return int(float(x or 0))
    except ValueError:
        return 0


# ---------- Katalog ----------

def build_catalog(build: str | None) -> dict:
    """{skillLine: {spellId: recipe}} plus Item-Infos, aus den DB2-Tabellen."""
    t = lambda name: icons.table(name, build)  # noqa: E731
    abilities = [r for r in t("SkillLineAbility") if _int(r["SkillLine"]) and _int(r["Spell"])]
    spells = {_int(r["Spell"]) for r in abilities}
    names = {_int(r["ID"]): r["Name_lang"] for r in t("SpellName") if _int(r["ID"]) in spells}
    reagents = {}
    for r in t("SpellReagents"):
        sid = _int(r["SpellID"])
        if sid in spells:
            reagents[sid] = [[_int(r[f"Reagent_{i}"]), _int(r[f"ReagentCount_{i}"])]
                             for i in range(8) if _int(r[f"Reagent_{i}"]) > 0 and _int(r[f"ReagentCount_{i}"]) > 0]
    creates, teaches = {}, {}
    for r in t("SpellEffect"):
        sid, effect = _int(r["SpellID"]), _int(r["Effect"])
        if effect == 24 and sid in spells and _int(r["EffectItemType"]):  # CREATE_ITEM
            creates[sid] = [_int(r["EffectItemType"]), max(1, _int(r.get("EffectBasePointsF")))]
        elif effect == 36 and _int(r["EffectTriggerSpell"]) in spells:  # LEARN_SPELL (Rezept-Item)
            teaches[sid] = _int(r["EffectTriggerSpell"])
    effect_spell = {_int(r["ID"]): _int(r["SpellID"]) for r in t("ItemEffect")}
    recipe_items: dict[int, list[int]] = {}
    for r in t("ItemXItemEffect"):
        sp = effect_spell.get(_int(r["ItemEffectID"]))
        # Rezept-Items lehren über einen Lern-Effekt oder tragen (wie in Classic) den Rezept-Zauber direkt.
        target = teaches.get(sp) or (sp if sp in spells else None)
        if target:
            recipe_items.setdefault(target, []).append(_int(r["ItemID"]))
    cooldown = {}
    for r in t("SpellCooldowns"):
        sid = _int(r["SpellID"])
        if sid in spells:
            ms = max(_int(r["CategoryRecoveryTime"]), _int(r["RecoveryTime"]))
            if ms >= 600000:
                cooldown[sid] = ms // 1000

    lines: dict[str, dict] = {}
    for r in abilities:
        sid, line = _int(r["Spell"]), _int(r["SkillLine"])
        if sid not in creates or sid not in reagents or not names.get(sid):
            continue
        lines.setdefault(str(line), {})[str(sid)] = {
            "name": names[sid], "output": creates[sid][0], "qty": creates[sid][1], "reagents": reagents[sid],
            "yellow": _int(r["TrivialSkillLineRankLow"]), "grey": _int(r["TrivialSkillLineRankHigh"]),
            "acquire": _int(r["AcquireMethod"]), "recipeItems": sorted(set(recipe_items.get(sid, []))),
            "cooldown": cooldown.get(sid),
        }
    wanted = set()
    for recipes in lines.values():
        for rec in recipes.values():
            wanted.update([rec["output"], *rec["recipeItems"], *(i for i, _ in rec["reagents"])])
    items = {}
    for r in t("ItemSparse"):
        iid = _int(r["ID"])
        if iid in wanted:
            items[str(iid)] = {"name": r["Display_lang"] or None, "quality": _int(r["OverallQualityID"]),
                               "buy": _int(r["BuyPrice"]), "buyStack": max(1, _int(r["VendorStackCount"])),
                               "sell": _int(r["SellPrice"]), "bind": _int(r["Bonding"]),
                               "reqSkill": _int(r["RequiredSkillRank"]), "reqSkillLine": _int(r["RequiredSkill"])}
    return {"version": CATALOG_VERSION, "build": build, "lines": lines, "items": items}


def load_catalog(build: str | None) -> dict:
    path = os.path.join(icons.CACHE, f"recipes-{build}.json") if build else None
    if path and os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        if data.get("version") == CATALOG_VERSION:
            return data
    if not build:
        cached = sorted(glob.glob(os.path.join(icons.CACHE, "recipes-*.json")), key=os.path.getmtime)
        if cached:
            with open(cached[-1], encoding="utf-8") as f:
                return json.load(f)
    data = build_catalog(build)
    if path and data["lines"]:
        os.makedirs(icons.CACHE, exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False)
    return data


# ---------- Preise ----------

class Market:
    def __init__(self, prices: dict, vendor: dict, items: dict):
        self.prices, self.vendor, self.items = prices, vendor, items
        days = [int(k) for e in prices.values() if isinstance(e, dict) for k in (e.get("a") or {})]
        self.last_day = max(days) if days else 0

    def ah(self, iid: int) -> dict | None:
        """Mindestpreis beim letzten Scan, Anzahl im AH, Median der Tagespreise der letzten 7 Scan-Tage."""
        e = self.prices.get(str(iid))
        if not isinstance(e, dict) or not e.get("m"):
            return None
        avail = e.get("a") or {}
        day = max((int(d) for d in avail), default=0)
        recent = [p for d, p in (e.get("h") or {}).items() if int(d) > self.last_day - 7 and p]
        return {"price": e["m"], "listed": avail.get(str(day), 0), "age": self.last_day - day if day else None,
                "median": int(statistics.median(recent)) if recent else e["m"]}

    def vendor_price(self, iid: int) -> tuple[float | None, str | None]:
        seen = self.vendor.get(iid) or self.vendor.get(str(iid))
        if isinstance(seen, dict) and seen.get("price") and not seen.get("limited"):
            return seen["price"], seen.get("npc") or "Händler"
        it = self.items.get(str(iid)) or {}
        if iid in VENDOR_ITEMS and it.get("buy"):
            return it["buy"] / it.get("buyStack", 1), "Händler"
        return None, None

    def reagent(self, iid: int) -> dict:
        """Günstigster Einkauf: AH oder Händler."""
        ah = self.ah(iid)
        vp, npc = self.vendor_price(iid)
        options = []
        if ah:
            options.append((ah["price"], "AH"))
        if vp:
            options.append((vp, npc))
        if not options:
            return {"unit": None, "source": None, "listed": 0}
        unit, source = min(options)
        return {"unit": unit, "source": source, "listed": ah["listed"] if ah else 0}


# ---------- Charakter ----------

def owned_counts(c: dict) -> dict[int, int]:
    out: dict[int, int] = {}
    containers = list(c.get("bags") or []) + list((c.get("bank") or {}).get("containers") or [])
    for bag in containers:
        for it in (bag or {}).get("items") or []:
            if it.get("id"):
                out[it["id"]] = out.get(it["id"], 0) + (it.get("count") or 1)
    return out


def color_of(rank: int, yellow: int, grey: int) -> int | None:
    """0 orange, 1 gelb, 2 grün, 3 grau, wie TradeskillRelativeDifficulty im Spiel."""
    if not grey:
        return None
    if rank >= grey:
        return 3
    if rank >= (yellow + grey) // 2:
        return 2
    if rank >= yellow:
        return 1
    return 0


def fmt_cd(seconds: int | None) -> str | None:
    if not seconds:
        return None
    hours = seconds / 3600
    return f"{hours:.0f} h" if hours >= 1 else f"{seconds // 60} min"


# ---------- Rechnung ----------

def evaluate(sid: int, rec: dict, scan: dict | None, rank: int, market: Market, owned: dict[int, int],
             names: dict[int, str]) -> dict:
    item = lambda i: market.items.get(str(i)) or {}  # noqa: E731
    name_of = lambda i: names.get(i) or item(i).get("name") or f"Item {i}"  # noqa: E731

    reagents = [(r["id"], r["count"]) for r in (scan or {}).get("reagents") or [] if not r.get("optional")] \
        or [tuple(r) for r in rec.get("reagents") or []]
    output = (scan or {}).get("output") or rec.get("output")
    qty = rec.get("qty") or 1
    if scan and scan.get("qtyMin"):
        qty = (scan["qtyMin"] + (scan.get("qtyMax") or scan["qtyMin"])) / 2

    rows, cost, missing_price, craftable = [], 0.0, [], None
    for iid, count in reagents:
        p = market.reagent(iid)
        line = None if p["unit"] is None else p["unit"] * count
        if line is None:
            missing_price.append(name_of(iid))
        else:
            cost += line
        have = owned.get(iid, 0)
        craftable = have // count if craftable is None else min(craftable, have // count)
        rows.append({"id": iid, "name": name_of(iid), "quality": item(iid).get("quality", 1), "count": count,
                     "unit": None if p["unit"] is None else round(p["unit"]), "cost": None if line is None else round(line),
                     "source": p["source"], "owned": have})

    out_item = item(output)
    ah = market.ah(output) if output else None
    bound = out_item.get("bind") == 1
    sell, sell_source = None, None
    if ah and not bound:
        sell, sell_source = ah["price"] * qty * (1 - AH_CUT), "AH"
    elif out_item.get("sell"):
        sell, sell_source = out_item["sell"] * qty, "Händler"
    profit = None if sell is None or missing_price else sell - cost
    difficulty = (scan or {}).get("difficulty")
    if difficulty is None:
        difficulty = color_of(rank, rec.get("yellow", 0), rec.get("grey", 0))
    notes = []
    if bound:
        notes.append("beim Aufheben gebunden, nicht verkäuflich")
    if ah and ah["age"]:
        notes.append(f"Preis {ah['age']} Tage alt")
    if ah and ah["price"] < 0.6 * ah["median"]:
        notes.append(f"gerade billig (Ø 7 Tage {ah['median'] / 10000:.2f}g)")
    if missing_price:
        notes.append("kein Preis für " + ", ".join(missing_price))
    return {
        "id": sid, "name": (scan or {}).get("name") or rec.get("name") or f"Rezept {sid}",
        "output": output, "outputName": name_of(output) if output else None, "quality": out_item.get("quality", 1),
        "qty": qty, "reagents": rows, "cost": round(cost) if not missing_price else None,
        "sell": None if sell is None else round(sell), "sellSource": sell_source,
        "unitPrice": ah["price"] if ah else None, "median": ah["median"] if ah else None,
        "listed": ah["listed"] if ah else 0,
        "profit": None if profit is None else round(profit),
        "margin": None if profit is None or not cost else round(profit / cost * 100),
        "craftable": craftable or 0, "difficulty": difficulty, "yellow": rec.get("yellow"), "grey": rec.get("grey"),
        "skillUps": (scan or {}).get("skillUps"), "cooldown": fmt_cd(rec.get("cooldown")),
        "isNew": bool(output and output > NEW_ID), "notes": notes,
    }


def acquisition(sid: int, rec: dict, scan: dict | None, rank: int, market: Market) -> dict:
    """Woher ein noch nicht gelerntes Rezept kommt und was es kostet."""
    options = []
    req = None
    for iid in rec.get("recipeItems") or []:
        it = market.items.get(str(iid)) or {}
        req = it.get("reqSkill") or req
        ah = market.ah(iid)
        seen = market.vendor.get(iid) or market.vendor.get(str(iid))
        name = it.get("name") or f"Rezept: {rec.get('name')}"
        if ah:
            options.append({"kind": "ah", "id": iid, "name": name, "price": ah["price"], "listed": ah["listed"],
                            "age": ah["age"], "quality": it.get("quality", 1)})
        if isinstance(seen, dict) and seen.get("price"):
            options.append({"kind": "vendor", "id": iid, "name": name, "price": round(seen["price"]),
                            "npc": seen.get("npc"), "limited": bool(seen.get("limited")), "quality": it.get("quality", 1)})
        if not ah and not seen:
            options.append({"kind": "item", "id": iid, "name": name, "price": None, "quality": it.get("quality", 1)})
    trainer = not rec.get("recipeItems") and rec.get("acquire") != 3
    if trainer:
        options.append({"kind": "trainer", "name": "Lehrer", "price": None})
    source_text = (scan or {}).get("source")
    priced = [o for o in options if o.get("price")]
    best = min(priced, key=lambda o: o["price"]) if priced else None
    if req:
        learnable = rank >= req
    elif trainer and rec.get("yellow"):
        learnable = True if rank >= rec["yellow"] else None  # Lernstufe liegt immer unter „gelb“
    else:
        learnable = None
    return {"options": options, "best": best, "reqSkill": req, "learnable": learnable, "sourceText": source_text,
            "trainer": trainer}


def profession_rows(line: int, catalog: dict, scan: dict | None, rank: int, market: Market,
                    owned: dict[int, int]) -> tuple[list, list]:
    recipes = catalog["lines"].get(str(line), {})
    scanned = (scan or {}).get("recipes") or {}
    names = {int(k): n for k, n in ((scan or {}).get("names") or {}).items() if n}
    ids = {int(k) for k in recipes} | {int(k) for k in scanned}
    learned, missing = [], []
    for sid in sorted(ids):
        rec = recipes.get(str(sid)) or {}
        s = scanned.get(sid) or scanned.get(str(sid))
        if not rec and not (s and s.get("output")):
            continue
        is_learned = bool(s and s.get("learned")) if scan else None
        if not scan and rec.get("acquire") == 3 and not rec.get("recipeItems"):
            continue
        row = evaluate(sid, rec, s, rank, market, owned, names)
        row["learned"] = is_learned
        if is_learned:
            learned.append(row)
        else:
            if rec.get("acquire") == 3 and not rec.get("recipeItems") and not (s and s.get("source")):
                continue  # nicht mehr erhältlich
            row["acquire"] = acquisition(sid, rec, s, rank, market)
            missing.append(row)
    return learned, missing


def sort_key(row: dict) -> tuple:
    return (row["profit"] is None, -(row["profit"] or 0))


def compute(wow: str = v.DEFAULT_WOW, char: str = "", beruf: str = "", top: int = 0,
            prices: dict | None = None) -> dict:
    files = v.find_chars(wow, char)
    if not files:
        raise FileNotFoundError(f"Kein WoWMCP-Export für „{char}“ gefunden.")
    c = upgrades.load_char(files[0])
    prices = prices if prices is not None else v.load_prices(v.find_auctionator(wow))
    vendor: dict = {}
    for path in glob.glob(os.path.join(wow, "WTF", "Account", "*", "SavedVariables", "WoWMCP.lua")):
        vendor.update((upgrades.load_sv(path, "WoWMCP_Account") or {}).get("vendor") or {})
    build = None
    cl = c.get("client") or {}
    if cl.get("version") and cl.get("build"):
        build = f"{cl['version']}.{int(cl['build'])}"
    catalog = load_catalog(build or icons.client_build(wow))
    market = Market(prices, vendor, catalog["items"])
    owned = owned_counts(c)
    scans = {int(k): s for k, s in (c.get("recipes") or {}).items()}

    out = []
    for p in (c.get("skills") or {}).get("professions") or []:
        line = p.get("skillLine")
        if not line or line in GATHERING or str(line) not in catalog["lines"]:
            continue
        if beruf and beruf.lower() not in (p.get("name") or "").lower():
            continue
        scan = scans.get(line)
        rank = (scan or {}).get("rank") or p.get("rank") or 0
        learned, missing = profession_rows(line, catalog, scan, rank, market, owned)
        learned.sort(key=sort_key)
        good = [r["profit"] for r in learned if r["profit"] is not None and r["listed"] >= 3]
        best_learned = max(good) if good else 0
        for r in missing:
            r["better"] = bool(scan) and r["profit"] is not None and r["profit"] > best_learned
            cost = (r["acquire"]["best"] or {}).get("price")
            r["payback"] = (round(cost / r["profit"], 1) if cost and r["profit"] and r["profit"] > 0 else None)
        # Erst, was du jetzt lernen kannst, dann nach Gewinn.
        missing.sort(key=lambda r: (r["acquire"]["learnable"] is not True, *sort_key(r)))
        if top:
            learned_rows, missing_rows = learned[:top], missing[:top]
        else:
            learned_rows, missing_rows = learned, missing
        out.append({
            "skillLine": line, "name": p.get("name"), "rank": rank, "max": (scan or {}).get("max") or p.get("max"),
            "scanned": (scan or {}).get("updated"), "learnedCount": len(learned), "missingCount": len(missing),
            "bestLearned": best_learned, "learned": learned_rows, "missing": missing_rows,
            "profitableCount": sum(1 for r in learned if (r["profit"] or 0) > 0),
        })
    sd = v.scan_date(prices)
    return {"character": os.path.basename(os.path.dirname(os.path.dirname(files[0]))),
            "level": (c.get("character") or {}).get("level"), "scanDate": sd.isoformat() if sd else None,
            "build": catalog.get("build"), "vendorPrices": len(vendor), "addonVersion": (c.get("addon") or {}).get("version"),
            "professions": out, "ahCut": AH_CUT}


# ---------- CLI ----------

def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--wow", default=os.environ.get("WOW_FOREVER_DIR") or v.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--beruf", default="", help="Teil des Berufsnamens, z. B. Alchimie")
    ap.add_argument("--top", type=int, default=15)
    ap.add_argument("--json", action="store_true")
    a = ap.parse_args()
    r = compute(a.wow, a.char, a.beruf, a.top)
    sys.stdout.reconfigure(encoding="utf-8")
    if a.json:
        print(json.dumps(r, ensure_ascii=False))
        return
    g = lambda c: "–" if c is None else v.fmt(int(c)) if c >= 0 else "-" + v.fmt(int(-c))  # noqa: E731
    print(f"{r['character']}  |  AH-Scan {r['scanDate']}  |  Build {r['build']}  |  Händlerpreise {r['vendorPrices']}")
    for p in r["professions"]:
        state = f"Berufsfenster gelesen {p['scanned']}" if p["scanned"] else "gelernte Rezepte unbekannt (Berufsfenster öffnen)"
        print(f"\n== {p['name']} {p['rank']}/{p['max']} ({state}) ==")
        if p["scanned"]:
            print("  Gelernt:")
            for x in p["learned"]:
                print(f"   {x['name'][:40]:<40} Gewinn {g(x['profit']):>10}  Kosten {g(x['cost']):>9}  AH {x['listed']:>5}x"
                      f"  {'; '.join(x['notes'])}")
        print("  Noch nicht gelernt:" if p["scanned"] else "  Alle Rezepte:")
        for x in p["missing"]:
            acq = x["acquire"]
            best = acq["best"]
            src = (f"{best['kind']} {g(best['price'])}" if best else
                   ", ".join(o["kind"] for o in acq["options"]) or "?")
            mark = "*" if x.get("better") else " "
            print(f"  {mark}{x['name'][:40]:<40} Gewinn {g(x['profit']):>10}  lernbar {acq['learnable']!s:<5}"
                  f" ab {acq['reqSkill'] or '?':<4} {src}")


if __name__ == "__main__":
    main()
