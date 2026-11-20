# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""What the WoWMCP addon shows in the game: the results of the other scripts as lists, tooltip lines and
vendor hints, written as Lua into the addon folder.

    uv run scripts/ingame.py                 # what would be sent, per character
    uv run scripts/ingame.py --write         # write it to Interface/AddOns/WoWMCP/AppData.lua
    uv run scripts/ingame.py --char Malgruk --json

Addons cannot read files, but the client loads every Lua file an addon lists in its .toc on /reload. So the
data travels as `WoWMCP_AppData = { ... }` in AppData.lua (addon 1.7 and newer). The addon is a plain viewer:

  chars[<first name>].lists   one tab of the window each: {key, title, note, sections = {{title, note, rows}}}
                              row = {id, name, q (quality), n (count), info, price (copper), shop, sub}
                              rows with `shop` also go into an Auctionator shopping list of that name
  chars[<first name>].tips    item ID -> lines the addon adds to the item's tooltip
  chars[<first name>].vendor  item ID -> line the addon prints when a merchant sells the item

Money inside a text is written as {m:<copper>}; the addon turns it into coins. Texts are in the language of
the game client, whatever --lang says: they are read in the game. `hash` covers everything but the time, so
the file is only written again when something changed.

Upgrades use the scale of the Affliction warlock and are left out for other classes.
"""

from __future__ import annotations

import argparse
import datetime
import glob
import hashlib
import json
import os
import re
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import consumables  # noqa: E402
import i18n  # noqa: E402
import recipes  # noqa: E402
import simengine  # noqa: E402
import upgrades  # noqa: E402
import writs  # noqa: E402
from i18n import tr  # noqa: E402

VERSION = 1
DATA_FILE = "AppData.lua"
GLOBAL = "WoWMCP_AppData"
MAX_TIPS = 4          # lines per item
TOP_CRAFTS = 5        # recipes per profession, each with its reagents
TOP_MISSING = 6       # recipes worth getting per profession
TOP_CRATES = 3        # crate kinds per tier
MAX_PAYBACK = 10      # crafts until a recipe has paid for itself, to make it onto the shopping list


# ---------- Building blocks ----------

def coin(copper) -> str:
    """Money placeholder the addon replaces with coins."""
    return "{m:%d}" % round(copper or 0)


def dec(value: float, signed: bool = False) -> str:
    """One decimal the way the language writes it: 4.5 or 4,5."""
    text = f"{value:+.1f}" if signed else f"{value:.1f}"
    return text.replace(".", ",") if i18n.language() == "de" else text


def clean(text) -> str:
    """Text from game data for display in the game: one line, and no "|" that starts a UI escape code."""
    return re.sub(r"\s+", " ", str(text or "")).replace("|", "||").strip()


def row(item_id, name, quality=None, count=1, info="", price=None, shop=False, sub=False) -> dict:
    out: dict = {"id": int(item_id), "name": clean(name)}
    if quality is not None:
        out["q"] = int(quality)
    if count and count != 1:
        out["n"] = int(count) if float(count).is_integer() else round(float(count), 1)
    if info:
        out["info"] = info
    if price:
        out["price"] = round(price)
    if shop:
        out["shop"] = True
    if sub:
        out["sub"] = True
    return out


def section(title: str, note: str, rows: list) -> dict:
    out = {"title": clean(title), "rows": rows}
    if note:
        out["note"] = note
    return out


def add_tip(tips: dict, item_id, line: str) -> None:
    lines = tips.setdefault(int(item_id), [])
    if line not in lines and len(lines) < MAX_TIPS:
        lines.append(line)


# ---------- Lists ----------

def owned(item: dict) -> str:
    """Where an item the account already has lies, as the end of a row's info; nothing for an AH item."""
    return " · " + clean(item["where"]) if item.get("where") else ""


def upgrade_list(r: dict, tips: dict) -> dict | None:
    """AH upgrades: the package for the budget (the shopping list), then the candidates per slot.
    With the combat engine the gains are DPS, with the weight scale points; the texts say which.
    Gear the account already has is in both with where it is instead of a price, and is not shopped for."""
    eng = r.get("engine")
    healer = bool(eng) and eng.get("metric") == "hps"
    sections = []
    package = r.get("package") or {}
    bought = {it["id"] for it in package.get("items") or []}
    if bought:
        rows = [row(it["id"], it["name"], it.get("quality"),
                    info=f"{clean(it.get('slotName'))} {dec(it['gain'], True)}" + owned(it),
                    price=it["price"], shop=bool(it["price"])) for it in package["items"]]
        gain, m = dec(package.get("gain") or 0, True), coin(package.get("cost"))
        if healer:
            note = tr("{gain} HPS for {m}", gain=gain, m=m)
        elif eng:
            note = tr("{gain} DPS for {m}", gain=gain, m=m)
        else:
            note = tr("{gain} points for {m}", gain=gain, m=m)
        sections.append(section(tr("Best package for the budget"), note, rows))
    for s in r.get("slots") or []:
        rows, rank = [], 0
        for c in s.get("candidates") or []:
            later = " · " + tr("from level {level}", level=c["reqLevel"]) if c.get("soon") else ""
            rows.append(row(c["id"], c["name"], c.get("quality"), info=dec(c["gain"], True) + later + owned(c),
                            price=c["price"]))
            slot, gain, m = clean(s.get("slotName")), dec(c["gain"], True), coin(c["price"])
            rank += 1 if c["price"] else 0  # the rank among what the auction house has
            if not c["price"]:
                where = clean(c.get("where"))
                if healer:
                    tip = tr("You own this upgrade for {slot}: {gain} HPS ({where})", slot=slot, gain=gain, where=where)
                elif eng:
                    tip = tr("You own this upgrade for {slot}: {gain} DPS ({where})", slot=slot, gain=gain, where=where)
                else:
                    tip = tr("You own this upgrade for {slot}: {gain} points ({where})", slot=slot, gain=gain,
                             where=where)
            elif healer:
                tip = tr("Upgrade for {slot}: {gain} HPS, no. {rank} in the AH scan ({m})",
                         slot=slot, gain=gain, rank=rank, m=m)
            elif eng:
                tip = tr("Upgrade for {slot}: {gain} DPS, no. {rank} in the AH scan ({m})",
                         slot=slot, gain=gain, rank=rank, m=m)
            else:
                tip = tr("Upgrade for {slot}: {gain} points, no. {rank} in the AH scan ({m})",
                         slot=slot, gain=gain, rank=rank, m=m)
            add_tip(tips, c["id"], tip)
            if c["id"] in bought:
                add_tip(tips, c["id"], tr("Part of the best package for your budget"))
        if rows:
            current = (s.get("current") or {}).get("name")
            sections.append(section(s.get("slotName"), tr("now: {name}", name=clean(current)) if current else tr("empty"),
                                    rows))
    review = [row(c["id"], c["name"], c.get("quality"), info=clean(c.get("slot")), price=c["price"])
              for c in r.get("review") or []]
    if review:
        sections.append(section(tr("With a special effect"), tr("the scale cannot rate these"), review))
    if not sections:
        return None
    if healer:
        note = tr("Budget {m} · combat model: {hps} HPS now", m=coin(r.get("budget")),
                  hps=dec(eng.get("value") or 0))
        if (eng.get("downrank") or {}).get("spell"):
            note += " · " + tr("heal with {spell}", spell=clean(eng["downrank"]["spell"]))
    elif eng:
        note = tr("Budget {m} · combat model: {dps} DPS now", m=coin(r.get("budget")),
                  dps=dec(eng.get("value") or 0))
    else:
        note = tr("Budget {m} · scale: {profile}", m=coin(r.get("budget")), profile=clean(r.get("profileLabel")))
    return {"key": "upgrades", "title": tr("AH upgrades"), "note": note, "sections": sections}


def have(owned: bool, info: str = "") -> str:
    """Marks a row the character does not need to buy."""
    if not owned:
        return info
    return tr("you have it") + (" · " + info if info else "")


def writ_list(r: dict, tips: dict) -> dict | None:
    """Writs per tier, cheapest reputation first; the first one of each tier is the shopping list."""
    sections = []
    for t in r.get("tiers") or []:
        rows = []
        for i, w in enumerate(t.get("best") or []):
            per = tr("{m} per reputation", m=coin(w["per"]))
            net = tr("profit {m}", m=coin(-w["net"])) if w["net"] < 0 else tr("net {m}", m=coin(w["net"]))
            rows.append(row(w["writId"], writs.item_name(w["writId"]), 1, info=have(not w["wcost"], f"{net} · {per}"),
                            price=w["wcost"], shop=i == 0 and w["wcost"] > 0))
            rows.append(row(w["itemId"], w["name"], count=w["qty"], info=have(not w["icost"], clean(w.get("prof"))),
                            price=w["icost"], shop=i == 0 and w["icost"] > 0, sub=True))
            add_tip(tips, w["writId"], tr("Writ ({tier}): needs {n}x {item}, {net}, {per}",
                                          tier=clean(t["name"]), n=w["qty"], item=clean(w["name"]), net=net, per=per))
            add_tip(tips, w["itemId"], tr("For a writ ({tier}): hand in {n}x, {net}", tier=clean(t["name"]), n=w["qty"],
                                          net=net))
        if rows:
            sections.append(section(t["name"], "", rows))
    if not sections:
        return None
    rep = r.get("reputation")
    note = f"{clean(rep['name'])}: {clean(rep['standing'])} {rep['value']}/{rep['max']}" if rep else ""
    return {"key": "writs", "title": tr("Writs"), "note": note, "sections": sections}


def crate_rows(c: dict, shop: bool) -> list:
    best = c["best"]
    per = tr("{m} per favor", m=coin(c["per"]))
    return [row(c["crateId"], c["name"], 1, info=have(bool(c.get("haveCrate")), per), price=c["crateCost"],
                shop=shop and bool(c["crateCost"])),
            row(best["id"], best["name"], count=best["qty"], info=have(not best["cost"]), price=best["cost"],
                shop=shop and bool(best["cost"]), sub=True)]


def crate_list(r: dict, tips: dict) -> dict | None:
    """Trade crates: the cheapest Merchant's Favor over all tiers (the shopping list), then per tier."""
    crates = r.get("crates") or {}
    sections = []
    best = [c for c in crates.get("best") or [] if c.get("best")]
    if best:
        rows = [x for c in best for x in crate_rows(c, True)]
        sections.append(section(tr("Cheapest Merchant's Favor"), "", rows))
    for t in crates.get("tiers") or []:
        complete = [c for c in t.get("rows") or [] if c.get("complete") and c.get("best")]
        for c in complete:
            b = c["best"]
            add_tip(tips, c["crateId"], tr("Fill with {n}x {item} ({m}): {per} per Merchant's Favor", n=b["qty"],
                                           item=clean(b["name"]), m=coin(b["cost"]), per=coin(c["per"])))
            for o in c.get("options") or []:
                add_tip(tips, o["id"], tr("Fills {crate}: {n}x", crate=clean(c["name"]), n=o["qty"]))
        rows = [x for c in complete[:TOP_CRATES] for x in crate_rows(c, False)]
        if rows:
            note = tr("from level {level}", level=t["level"]) if t.get("tooLow") else tr("{n} favor each", n=f"{t['favor']:g}")
            sections.append(section(t["name"], note, rows))
    if not sections:
        return None
    note = tr("Merchant's Favor: {n}", n=r.get("favor")) if r.get("favor") is not None else ""
    return {"key": "crates", "title": tr("Trade crates"), "note": note, "sections": sections}


def trend_text(trend: str | None) -> str:
    return {"falling": " · " + tr("falling"), "rising": " · " + tr("rising")}.get(trend or "", "")


def recipe_list(r: dict, tips: dict, vendor: dict) -> dict | None:
    """Professions: the best crafts with their reagents (the first one is the shopping list) and the recipes
    worth getting. Tooltip lines for results, reagents and recipe items."""
    sections = []
    for p in r.get("professions") or []:
        name = clean(p.get("name"))
        crafts = sorted((x for x in p.get("learned") or [] if (x.get("profit") or 0) > 0 and x.get("output")),
                        key=lambda x: -x["profit"])
        rows = []
        for i, x in enumerate(crafts):
            profit = tr("profit {m}", m=coin(x["profit"]))
            add_tip(tips, x["output"], tr("{profession}: {profit} per craft", profession=name, profit=profit)
                    + trend_text(x.get("trend")))
            for g in x.get("reagents") or []:
                add_tip(tips, g["id"], tr("Reagent for {item}: {profit} per craft", item=clean(x.get("outputName")),
                                          profit=profit))
            if i >= TOP_CRAFTS:
                continue
            cooldown = " · " + clean(x["cooldown"]) if x.get("cooldown") else ""
            rows.append(row(x["output"], x.get("outputName") or x.get("name"), x.get("quality"), x.get("qty") or 1,
                            info=profit + trend_text(x.get("trend")) + cooldown))
            for g in x.get("reagents") or []:
                missing = max(0, (g.get("count") or 0) - (g.get("owned") or 0))
                rows.append(row(g["id"], g.get("name"), g.get("quality"), g.get("count") or 1,
                                info=have(missing == 0, tr("vendor") if g.get("source") == "vendor" else ""),
                                price=g.get("cost"),
                                shop=i == 0 and g.get("source") == "ah" and missing > 0, sub=True))
        if rows:
            sections.append(section(tr("{profession}: best crafts", profession=name), "", rows))
        rows = []
        for x in p.get("missing") or []:
            acq = x.get("acquire") or {}
            if (x.get("profit") or 0) <= 0:
                continue
            profit = tr("profit {m}", m=coin(x["profit"]))
            for o in acq.get("options") or []:
                if not o.get("id"):
                    continue
                line = tr("Teaches {recipe}: {profit} per craft", recipe=clean(x.get("name")), profit=profit)
                if x.get("payback"):
                    line += ", " + tr("paid off after {n} crafts", n=f"{x['payback']:g}")
                add_tip(tips, o["id"], line)
                if x.get("better"):
                    add_tip(tips, o["id"], tr("Pays more than your best recipe"))
                if acq.get("learnable") is False and acq.get("reqSkill"):
                    add_tip(tips, o["id"], tr("Needs {profession} {n}", profession=name, n=acq["reqSkill"]))
                vendor[int(o["id"])] = line
            b = acq.get("best") or {}
            if len(rows) < TOP_MISSING and b.get("id") and b.get("price") and acq.get("learnable") is not False:
                where = " · " + tr("vendor") if b.get("kind") == "vendor" else ""
                payback = x.get("payback")
                after = " · " + tr("paid off after {n} crafts", n=f"{payback:g}") if payback else ""
                rows.append(row(b["id"], b.get("name"), b.get("quality"), info=profit + after + where, price=b["price"],
                                shop=bool(x.get("better")) and b.get("kind") == "ah"
                                and payback is not None and payback <= MAX_PAYBACK))
        if rows:
            sections.append(section(tr("{profession}: worth getting", profession=name), tr("profit per craft"), rows))
    if not sections:
        return None
    return {"key": "recipes", "title": tr("Professions"), "note": "", "sections": sections}


def consumable_list(r: dict, tips: dict) -> dict | None:
    """Consumables per category; "best now" items from the AH you do not carry are the shopping list."""
    sections = []
    buffs = [b for b in r.get("buffs") or [] if b.get("relevant")]
    for g in list(r.get("categories") or []) + buffs:
        rows = []
        for x in g.get("items") or []:
            if not x.get("available") and not x.get("price"):
                continue
            info = clean(x.get("effect"))
            if x.get("duration") and g in buffs:  # food and bandages name their time in the effect
                info += f" ({clean(x['duration'])})"
            if x.get("owned"):
                info = tr("{n} in bags", n=x["owned"]) + " · " + info
            rows.append(row(x["id"], x["name"], x.get("quality"), info=info, price=x.get("price"),
                            shop=bool(x.get("best")) and x.get("source") == "ah" and not x.get("owned")))
            if x.get("best"):
                add_tip(tips, x["id"], tr("Best now ({category}): {effect}", category=clean(g.get("label")),
                                          effect=clean(x.get("effect"))))
        if rows:
            sections.append(section(g.get("label"), "", rows))
    if not sections:
        return None
    return {"key": "consumables", "title": tr("Consumables"), "note": tr("Level {n}", n=r.get("level")),
            "sections": sections}


# ---------- Payload ----------

def character_data(wow: str, path: str, prices: dict | None) -> tuple[str | None, dict]:
    """(first name, {level, lists, tips, vendor, errors}) for one character export."""
    c = upgrades.load_char(path)
    who = c.get("character") or {}
    folder = os.path.basename(os.path.dirname(os.path.dirname(path)))
    tips: dict = {}
    vendor: dict = {}
    lists, errors = [], {}

    def part(key, build):
        try:
            made = build()
        except (FileNotFoundError, LookupError, ValueError, RuntimeError) as e:
            errors[key] = str(e)
            return
        for one in made if isinstance(made, list) else [made]:
            if one:
                lists.append(one)

    def writ_lists():
        r = writs.compute(wow, folder, prices=prices)
        return [writ_list(r, tips), crate_list(r, tips)]

    if simengine.supported(who.get("classFile")):
        part("upgrades", lambda: upgrade_list(upgrades.compute(wow, folder, prices=prices, engine="auto"), tips))
    part("writs", writ_lists)
    part("recipes", lambda: recipe_list(recipes.compute(wow, folder, prices=prices), tips, vendor))
    part("consumables", lambda: consumable_list(consumables.compute(wow, folder, 3), tips))
    out = {"level": who.get("level"), "lists": lists, "tips": tips, "vendor": vendor}
    if errors:
        out["errors"] = errors
    return who.get("name"), out


def digest(payload: dict) -> str:
    """Hash of everything but the time, so unchanged data is not written (and reloaded) again."""
    rest = {k: v for k, v in payload.items() if k not in ("generated", "hash")}
    text = json.dumps(rest, sort_keys=True, ensure_ascii=False, default=str)
    return hashlib.sha1(text.encode("utf-8")).hexdigest()[:16]


def compute(wow: str = writs.DEFAULT_WOW, char: str = "") -> dict:
    """The payload for every exported character (or the ones whose folder name contains `char`)."""
    locale = i18n.client_locale(wow)
    previous = i18n.language()
    i18n.set_language(i18n.language_of(locale))  # read in the game, so always the client's language
    try:
        try:
            prices = writs.load_prices(writs.find_auctionator(wow))
        except (FileNotFoundError, LookupError, ValueError):
            prices = None  # every part then reports the missing scan itself
        scan = writs.scan_date(prices) if prices else None
        chars: dict = {}
        for path in writs.find_chars(wow, char):
            try:
                name, data = character_data(wow, path, prices)
            except Exception as e:  # a half-written file while WoW is saving
                name, data = None, {"errors": {"export": str(e)}}
            # The newest export wins when two folders hold the same first name.
            if name and name not in chars:
                chars[name] = data
    finally:
        i18n.set_language(previous)
    payload = {"version": VERSION, "generated": int(time.time()), "locale": locale or i18n.DEFAULT_LOCALE,
               "scanDate": scan.isoformat() if scan else None, "chars": chars}
    payload["hash"] = digest(payload)
    return payload


# ---------- Lua ----------

def lua_string(text: str) -> str:
    """A Lua string literal. Everything outside printable ASCII is written as a decimal escape of its UTF-8
    bytes, so no input can end the string or reach the file as code."""
    out = []
    for byte in text.encode("utf-8"):
        ch = chr(byte)
        if ch in '"\\':
            out.append("\\" + ch)
        elif 32 <= byte < 127:
            out.append(ch)
        else:
            out.append(f"\\{byte:03d}")
    return '"' + "".join(out) + '"'


def lua_key(key) -> str:
    if isinstance(key, bool):
        raise TypeError("a boolean cannot be a key")
    if isinstance(key, int):
        return f"[{key}]"
    key = str(key)
    return key if re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key) and key not in LUA_KEYWORDS else f"[{lua_string(key)}]"


LUA_KEYWORDS = {"and", "break", "do", "else", "elseif", "end", "false", "for", "function", "goto", "if", "in", "local",
                "nil", "not", "or", "repeat", "return", "then", "true", "until", "while"}


def lua_value(value, indent: int = 0) -> str:
    """Python data as a Lua table constructor. None entries are left out."""
    pad, inner = "  " * indent, "  " * (indent + 1)
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int):
        return str(value)
    if isinstance(value, float):
        if value != value or value in (float("inf"), float("-inf")):
            raise ValueError("not a finite number")
        return str(int(value)) if value.is_integer() else repr(round(value, 4))
    if isinstance(value, str):
        return lua_string(value)
    if isinstance(value, (list, tuple)):
        parts = [lua_value(v, indent + 1) for v in value if v is not None]
        if not parts:
            return "{}"
        if all(isinstance(v, (str, int, float)) and not isinstance(v, bool) for v in value) and sum(map(len, parts)) < 90:
            return "{ " + ", ".join(parts) + " }"
        return "{\n" + "".join(f"{inner}{p},\n" for p in parts) + pad + "}"
    if isinstance(value, dict):
        parts = [f"{lua_key(k)} = {lua_value(v, indent + 1)}" for k, v in value.items() if v is not None]
        if not parts:
            return "{}"
        if not any(isinstance(v, (dict, list, tuple)) for v in value.values()) and sum(map(len, parts)) < 100:
            return "{ " + ", ".join(parts) + " }"
        return "{\n" + "".join(f"{inner}{p},\n" for p in parts) + pad + "}"
    raise TypeError(f"cannot write {type(value).__name__} as Lua")


NOTE = ("-- Written by the WoW Forever Tools desktop app (github.com/kejith/wow-forever-tools). The client reads this\n"
        "-- file on /reload; the WoWMCP addon shows it (/wowmcp show). Do not edit: the app overwrites it.\n")


def to_lua(payload: dict) -> str:
    """The data file. Time and hash come first: file_state() reads them from the head of the file."""
    head = ("version", "generated", "hash")
    ordered = {k: payload[k] for k in head if k in payload} | {k: v for k, v in payload.items() if k not in head}
    return f"{NOTE}{GLOBAL} = {lua_value(ordered)}\n"


# ---------- File ----------

def addon_dir(wow: str) -> str:
    return os.path.join(wow, "Interface", "AddOns", "WoWMCP")


def addon_state(wow: str) -> dict:
    """Is the addon installed, and does its .toc load AppData.lua (1.7 and newer)?"""
    folder = addon_dir(wow)
    try:
        with open(os.path.join(folder, "WoWMCP.toc"), encoding="utf-8-sig") as f:
            toc = f.read()
    except OSError:
        return {"installed": False, "ready": False, "version": None}
    version = re.search(r"^## Version:\s*(\S+)", toc, re.M)
    ready = any(line.strip().replace("\\", "/") == DATA_FILE for line in toc.splitlines())
    return {"installed": True, "ready": ready, "version": version.group(1) if version else None}


def file_state(wow: str) -> dict | None:
    """Time and hash of the data file the game will read on the next /reload."""
    try:
        with open(os.path.join(addon_dir(wow), DATA_FILE), encoding="utf-8") as f:
            head = f.read(2000)
    except OSError:
        return None
    generated = re.search(r"\bgenerated = (\d+)", head)
    found = re.search(r'\bhash = "(\w+)"', head)
    if not generated or not found:
        return None
    return {"generated": int(generated.group(1)), "hash": found.group(1)}


def account_files(wow: str) -> list:
    return glob.glob(os.path.join(wow, "WTF", "Account", "*", "SavedVariables", "WoWMCP.lua"))


def saved_time(wow: str) -> float | None:
    """When the game last wrote the addon's account data: on /reload or logout. A data file older than
    this has been read by the game (or will be at the next login)."""
    times = []
    for path in account_files(wow):
        try:
            times.append(os.path.getmtime(path))
        except OSError:
            pass
    return max(times) if times else None


def loaded_state(wow: str) -> dict | None:
    """What the game had loaded when it last saved: WoWMCP_Account.appData = {generated, hash, loaded}.
    One /reload behind: the addon notes it while loading, the game writes it at the next save."""
    newest = None
    for path in account_files(wow):
        try:
            seen = (upgrades.load_sv(path, "WoWMCP_Account") or {}).get("appData")
        except Exception:  # a half-written file while WoW is saving
            continue
        if isinstance(seen, dict) and (newest is None or (seen.get("loaded") or 0) > (newest.get("loaded") or 0)):
            newest = seen
    return newest


def write(wow: str, payload: dict) -> dict:
    """Writes the payload into the addon folder unless the addon cannot read it or nothing changed."""
    state = addon_state(wow)
    path = os.path.join(addon_dir(wow), DATA_FILE)
    if not state["installed"]:
        return {"written": False, "reason": tr("The WoWMCP addon is not installed.")}
    if not state["ready"]:
        return {"written": False, "reason": tr("WoWMCP {version} cannot show lists. Update the addon to 1.7 or newer.",
                                               version=state["version"] or "?")}
    old = file_state(wow)
    if old and old["hash"] == payload["hash"]:
        return {"written": False, "unchanged": True, "file": path, "generated": old["generated"]}
    tmp = f"{path}.{os.getpid()}.tmp"
    with open(tmp, "w", encoding="utf-8", newline="\n") as f:
        f.write(to_lua(payload))
    os.replace(tmp, path)  # in one step: the game never reads half a file
    return {"written": True, "file": path, "generated": payload["generated"]}


def summary(payload: dict) -> dict:
    """What the app shows about a payload: per character the lists with their sizes."""
    chars = []
    for name, c in payload["chars"].items():
        lists = [{"key": one["key"], "title": one["title"],
                  "rows": sum(len(s["rows"]) for s in one["sections"]),
                  "shop": sum(1 for s in one["sections"] for r in s["rows"] if r.get("shop"))}
                 for one in c.get("lists") or []]
        chars.append({"name": name, "level": c.get("level"), "lists": lists, "tips": len(c.get("tips") or {}),
                      "vendor": len(c.get("vendor") or {}), "errors": c.get("errors") or {}})
    return {"generated": payload["generated"], "hash": payload["hash"], "scanDate": payload["scanDate"],
            "locale": payload["locale"], "chars": chars}


def sync(wow: str = writs.DEFAULT_WOW, char: str = "", do_write: bool = False) -> dict:
    """Computes the payload and, with `do_write`, sends it to the game. Returns the summary for the app."""
    payload = compute(wow, char)
    out = summary(payload)
    out["addon"] = addon_state(wow)
    out["result"] = write(wow, payload) if do_write else None
    out["file"] = file_state(wow)
    if out["file"]:
        out["file"]["modified"] = os.path.getmtime(os.path.join(addon_dir(wow), DATA_FILE))
    out["saved"] = saved_time(wow)
    out["loaded"] = loaded_state(wow)
    return out


# ---------- CLI ----------

def plain(text: str) -> str:
    """Money placeholders as text, for the terminal."""
    def money(m):
        c = int(m.group(1))
        sign, c = ("-" if c < 0 else ""), abs(c)
        g, s, k = c // 10000, c // 100 % 100, c % 100
        return sign + " ".join(p for p in (f"{g}g" if g else "", f"{s}s" if s else "", f"{k}c" if k or not (g or s) else "") if p)
    return re.sub(r"\{m:(-?\d+)\}", money, text).replace("||", "|")


def print_payload(payload: dict) -> None:
    for name, c in payload["chars"].items():
        print(f"\n== {name} ({tr('Level {n}', n=c.get('level'))}) ==")
        for key, err in (c.get("errors") or {}).items():
            print(f"  ! {key}: {err}")
        for one in c.get("lists") or []:
            print(f"\n[{one['title']}] {plain(one.get('note') or '')}")
            for s in one["sections"]:
                print(f"  {s['title']}  {plain(s.get('note') or '')}")
                for r in s["rows"]:
                    count = f" x{r['n']}" if r.get("n") else ""
                    price = plain(coin(r["price"])) if r.get("price") else ""
                    mark = "*" if r.get("shop") else " "
                    print(f"   {mark}{'  ' if r.get('sub') else ''}{r['name']}{count}  {plain(r.get('info') or '')}  {price}")
        print(f"\n  {tr('{n} items with a tooltip line, {v} vendor hints', n=len(c.get('tips') or {}), v=len(c.get('vendor') or {}))}")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--wow", default=writs.DEFAULT_WOW)
    ap.add_argument("--char", default="", help="only characters whose folder name contains this")
    ap.add_argument("--write", action="store_true", help=f"write {DATA_FILE} into the installed addon")
    ap.add_argument("--json", action="store_true")
    i18n.add_argument(ap)
    a = ap.parse_args()
    i18n.set_language(a.lang, a.wow)
    sys.stdout.reconfigure(encoding="utf-8")
    payload = compute(a.wow, a.char)
    result = write(a.wow, payload) if a.write else None
    if a.json:
        print(json.dumps({"payload": payload, "result": result}, ensure_ascii=False))
        return
    print_payload(payload)
    when = datetime.datetime.fromtimestamp(payload["generated"]).strftime("%H:%M")
    print(f"\n{tr('AH scan {date}, computed {time}. * = goes into the Auctionator shopping list.', date=payload['scanDate'] or '?', time=when)}")
    if result:
        if result.get("written"):
            print(tr("Written to {file}. Type /reload in the game.", file=result["file"]))
        elif result.get("unchanged"):
            print(tr("Nothing changed, the file in the addon folder is current."))
        else:
            print(result["reason"])


if __name__ == "__main__":
    main()
