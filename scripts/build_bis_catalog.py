#!/usr/bin/env python3
"""Build sim/data/bis_sources.json.gz from ForeverMyBis (CC BY-SA 4.0 / MIT) and WoW Forever DB2 CSVs.

Usage:
    python3 scripts/build_bis_catalog.py \
        --fmb "/path/to/Interface/AddOns/ForeverMyBis/Data" \
        --cache scripts/cache \
        --out sim/data/bis_sources.json.gz
"""
import argparse
import csv
import gzip
import json
import os
import re
from pathlib import Path

# Places in WoW: Forever that are 5/10-man dungeons (non-raid instances).
DUNGEON_PLACES = {
    "Ragefire Chasm",
    "Wailing Caverns",
    "The Deadmines", "Deadmines",
    "Shadowfang Keep",
    "Blackfathom Deeps",
    "The Stockade", "Stormwind Stockade",
    "Gnomeregan",
    "Razorfen Kraul",
    "Scarlet Monastery", "Scarlet Monastery Graveyard", "Scarlet Monastery Library",
    "Scarlet Monastery Armory", "Scarlet Monastery Cathedral",
    "Razorfen Downs",
    "Uldaman",
    "Zul'Farrak",
    "Maraudon",
    "The Temple of Atal'Hakkar", "Sunken Temple",
    "Blackrock Depths",
    "Blackrock Spire", "Lower Blackrock Spire", "Upper Blackrock Spire",
    "Dire Maul", "Dire Maul East", "Dire Maul West", "Dire Maul North",
    "Scholomance",
    "Stratholme", "Stratholme Live", "Stratholme Undead",
    # WoW: Forever new dungeons
    "Ruins of Lordaeron",
    "Excavation Site: Wetlands",
    "The Hall of Thanes",
    "The Drowned City",
    "City of Dalaran",
    "Krol'dok Stronghold",
    "Alcaz Prison",
    "The Shaper's Terrace", "Shaper's Terrace",
    "Blackmaw Hold",
}

RAID_PLACES = {
    "Molten Core",
    "Onyxia's Lair", "Onyxia",
    "Blackwing Lair",
    "Zul'Gurub",
    "Ruins of Ahn'Qiraj", "The Ruins of Ahn'Qiraj",
    "Temple of Ahn'Qiraj", "The Temple of Ahn'Qiraj", "Ahn'Qiraj", "Ahn'Qiraj Temple",
    "Naxxramas",
    "World Bosses",
    # WoW: Forever new raids
    "Mount Hyjal",
    "Barrow Deeps",
}

EQUIPPABLE_INV = {1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 20, 21, 22, 23, 25, 26, 28}

SIDE_MAP = {"a": "alliance", "h": "horde"}
CLASS_MAP = {
    "warrior": "WARRIOR", "paladin": "PALADIN", "hunter": "HUNTER",
    "rogue": "ROGUE", "priest": "PRIEST", "shaman": "SHAMAN",
    "mage": "MAGE", "warlock": "WARLOCK", "druid": "DRUID",
}
SKILL_NAMES = {
    164: "Blacksmithing", 165: "Leatherworking", 171: "Alchemy",
    182: "Herbalism", 185: "Cooking", 197: "Tailoring",
    202: "Engineering", 333: "Enchanting",
}

TOKEN_RE = re.compile(
    r'\s*(?:'
    r'(?P<lbrace>\{)|'
    r'(?P<rbrace>\})|'
    r'(?P<lbrack>\[)|'
    r'(?P<rbrack>\])|'
    r'(?P<eq>=)|'
    r'(?P<comma>,)|'
    r'(?P<str>"(?:\\.|[^"\\])*")|'
    r'(?P<num>-?\d+(?:\.\d+)?)|'
    r'(?P<ident>[A-Za-z_][A-Za-z0-9_]*)'
    r')'
)


def parse_lua_table(text: str, pos: int = 0):
    """Parse a single Lua table literal starting at '{'."""
    tokens = []
    # Find matching outer braces quickly using a small scanner.
    i = text.find("{", pos)
    if i < 0:
        return None, len(text)
    depth = 0
    in_str = False
    esc = False
    end = i
    for j in range(i, len(text)):
        ch = text[j]
        if in_str:
            if esc:
                esc = False
            elif ch == "\\":
                esc = True
            elif ch == '"':
                in_str = False
        else:
            if ch == '"':
                in_str = True
            elif ch == "{":
                depth += 1
            elif ch == "}":
                depth -= 1
                if depth == 0:
                    end = j + 1
                    break
    chunk = text[i:end]
    for m in TOKEN_RE.finditer(chunk):
        kind = m.lastgroup
        val = m.group(kind)
        tokens.append((kind, val))

    idx = 0
    n = len(tokens)

    def unescape(s: str) -> str:
        s = s[1:-1]
        if "\\" not in s:
            return s
        return re.sub(r'\\(.)', lambda m: {"n": "\n", "t": "\t", "r": "\r", '"': '"', "\\": "\\"}.get(m.group(1), m.group(1)), s)

    def parse_val():
        nonlocal idx
        kind, val = tokens[idx]
        if kind == "lbrace":
            return parse_tbl()
        idx += 1
        if kind == "str":
            return unescape(val)
        if kind == "num":
            return float(val) if "." in val else int(val)
        if kind == "ident":
            if val == "true":
                return True
            if val == "false":
                return False
            if val == "nil":
                return None
            return val
        raise ValueError(f"Unexpected token {kind}:{val}")

    def parse_tbl():
        nonlocal idx
        idx += 1  # '{'
        d = {}
        arr = []
        is_dict = False
        while idx < n and tokens[idx][0] != "rbrace":
            if tokens[idx][0] == "lbrack":
                is_dict = True
                idx += 1  # '['
                k = parse_val()
                idx += 1  # ']'
                idx += 1  # '='
                v = parse_val()
                d[k] = v
            elif tokens[idx][0] == "ident" and idx + 1 < n and tokens[idx + 1][0] == "eq":
                is_dict = True
                k = tokens[idx][1]
                idx += 2  # ident '='
                v = parse_val()
                d[k] = v
            else:
                v = parse_val()
                arr.append(v)
            if idx < n and tokens[idx][0] == "comma":
                idx += 1
        idx += 1  # '}'
        if is_dict:
            for k_i, v in enumerate(arr, 1):
                d[k_i] = v
            return d
        return arr

    return parse_tbl(), end


def parse_merged_lua(path: Path, var_prefix: str = "merge(") -> dict:
    txt = path.read_text(encoding="utf-8")
    out = {}
    pos = 0
    while True:
        idx = txt.find(var_prefix, pos)
        if idx < 0:
            break
        tbl, end = parse_lua_table(txt, idx)
        if isinstance(tbl, dict):
            out.update(tbl)
        pos = end
    return out


def parse_named_lua(txt: str, name: str):
    m = re.search(rf'\b{re.escape(name)}\s*=\s*\(function\(\)\s*return\s*', txt)
    if not m:
        return None
    tbl, _ = parse_lua_table(txt, m.end())
    return tbl


def parse_names_de(path: Path) -> tuple[dict, dict]:
    txt = path.read_text(encoding="utf-8")
    items_de = {}
    places_de = {}
    # N.i merges
    m_i = txt.find("N.i = {}")
    m_s = txt.find("N.s = {}")
    if m_i >= 0:
        seg = txt[m_i:m_s if m_s > m_i else len(txt)]
        pos = 0
        while True:
            idx = seg.find("merge(N.i,", pos)
            if idx < 0:
                break
            tbl, end = parse_lua_table(seg, idx)
            if isinstance(tbl, dict):
                for k, v in tbl.items():
                    if isinstance(k, int) and isinstance(v, str):
                        items_de[k] = v
            pos = end
    m_d = txt.find("N.d = {}")
    m_e = txt.find("N.e = {}")
    if m_d >= 0:
        seg = txt[m_d:m_e if m_e > m_d else len(txt)]
        pos = 0
        while True:
            idx = seg.find("merge(N.d,", pos)
            if idx < 0:
                break
            tbl, end = parse_lua_table(seg, idx)
            if isinstance(tbl, dict):
                for k, v in tbl.items():
                    if isinstance(k, str) and isinstance(v, str):
                        places_de[k] = v
            pos = end
    return items_de, places_de


def latest_csv(cache: Path, prefix: str) -> Path | None:
    files = sorted(cache.glob(f"{prefix}-*.csv"))
    return files[-1] if files else None


def classify_sources(it: dict, npcs: dict, quests: dict, places: list, place_maps: dict, skills: dict, factions: dict) -> list[dict]:
    out = []
    seen = set()
    dz_list = it.get("dz") or []
    dz_set = set(dz_list) if isinstance(dz_list, list) else set()
    for rec in (it.get("so") or []):
        if not isinstance(rec, dict):
            continue
        k = rec.get("k")
        rf = SIDE_MAP.get(rec.get("f", ""), "")
        if k == "d":
            w_idx = rec.get("w")
            place = ""
            if isinstance(w_idx, int) and 1 <= w_idx <= len(places):
                place = places[w_idx - 1]
            elif isinstance(w_idx, str):
                place = w_idx
            n_val = rec.get("n")
            who = ""
            if isinstance(n_val, int):
                who = str(npcs.get(n_val, ""))
            elif isinstance(n_val, str):
                who = n_val
            if who == "World Drop" and not place:
                entry = {"k": "world", "l": "World Drop"}
            else:
                # Determine raid vs dungeon vs world drop
                if place in RAID_PLACES:
                    cat = "raid"
                elif place in DUNGEON_PLACES or (dz_set and (not place or place in dz_set or any(place in d or d in place for d in dz_set))) or (isinstance(w_idx, int) and w_idx not in place_maps and place):
                    cat = "dungeon"
                elif dz_set:
                    cat = "dungeon"
                    if not place:
                        place = dz_list[0]
                else:
                    cat = "world"
                if who and place:
                    label = f"{who}, {place}"
                elif who:
                    label = who
                elif place:
                    label = place
                else:
                    label = "Drop"
                entry = {"k": cat, "l": label}
                if place:
                    entry["p"] = place
            if rf:
                entry["f"] = rf
        elif k == "q":
            q_val = rec.get("q")
            qid = q_val if isinstance(q_val, int) else 0
            qtitle = str(quests.get(q_val, q_val)) if q_val is not None else "Quest"
            qt = rec.get("t", "")
            cat = "raid" if qt == "r" else "quest"
            entry = {"k": cat, "l": qtitle}
            if qid:
                entry["qid"] = qid
            if isinstance(rec.get("lv"), int) and rec["lv"] > 0:
                entry["qlv"] = rec["lv"]
            if qt:
                entry["qt"] = qt
            if rf:
                entry["f"] = rf
        elif k == "c":
            sk = rec.get("sk") if isinstance(rec.get("sk"), int) else 0
            sr = rec.get("r") if isinstance(rec.get("r"), int) else 0
            sname = skills.get(sk, SKILL_NAMES.get(sk, "Crafted"))
            label = f"{sname} {sr}" if sr else sname
            entry = {"k": "crafted", "l": label}
            if sk:
                entry["sk"] = sk
            if sr:
                entry["sr"] = sr
        elif k == "v":
            n_val = rec.get("n")
            who = str(npcs.get(n_val, n_val)) if n_val is not None else "Vendor"
            w_idx = rec.get("w")
            place = places[w_idx - 1] if isinstance(w_idx, int) and 1 <= w_idx <= len(places) else ""
            label = f"{who}, {place}" if place else who
            entry = {"k": "vendor", "l": label}
            if place:
                entry["p"] = place
            if rf:
                entry["f"] = rf
        elif k == "r":
            rep = it.get("rep")
            label = "Reputation"
            if isinstance(rep, dict):
                fn = factions.get(rep.get("f"), rep.get("n", "Reputation"))
                label = str(fn)
            entry = {"k": "reputation", "l": label}
            if rf:
                entry["f"] = rf
        elif k in ("h", "hm"):
            rk = rec.get("rk")
            label = f"PvP Rank {rk}" if rk else "PvP / Honor"
            entry = {"k": "pvp", "l": label}
            if rf:
                entry["f"] = rf
        elif k == "w":
            entry = {"k": "world", "l": "World Drop"}
        else:
            continue
        sig = (entry.get("k"), entry.get("l"), entry.get("f", ""), entry.get("qid", 0))
        if sig not in seen:
            seen.add(sig)
            out.append(entry)
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--fmb", required=True, help="Path to ForeverMyBis/Data folder")
    ap.add_argument("--cache", default="scripts/cache", help="Path to DB2 CSV cache")
    ap.add_argument("--out", default="sim/data/bis_sources.json.gz", help="Output gzipped JSON path")
    args = ap.parse_args()

    fmb = Path(args.fmb)
    cache = Path(args.cache)

    items_txt = (fmb / "Items.lua").read_text(encoding="utf-8")
    fmb_items = parse_merged_lua(fmb / "Items.lua")
    fmb_stats = parse_merged_lua(fmb / "Stats.lua")
    fmb_procs = parse_merged_lua(fmb / "Procs.lua")
    npcs = parse_named_lua(items_txt, "ns.npcs") or {}
    quests = parse_named_lua(items_txt, "ns.quests") or {}
    places = parse_named_lua(items_txt, "ns.places") or []
    place_maps = parse_named_lua(items_txt, "ns.placeMaps") or {}
    skills = parse_named_lua(items_txt, "ns.skills") or {}
    factions = parse_named_lua(items_txt, "ns.factions") or {}
    items_de, places_de = parse_names_de(fmb / "Names_deDE.lua")

    # Load Item.db2 for authoritative (ClassID, SubclassID, InventoryType)
    item_db = {}
    item_csv = latest_csv(cache, "Item")
    build = ""
    if item_csv:
        m = re.search(r'Item-(.+)\.csv$', item_csv.name)
        if m:
            build = m.group(1)
        with item_csv.open(encoding="utf-8") as f:
            for r in csv.DictReader(f):
                item_db[int(r["ID"])] = (int(r["ClassID"] or 0), int(r["SubclassID"] or 0), int(r["InventoryType"] or 0))

    # Load localized German names from ItemSparse-*-deDE.csv as supplement
    sparse_de = latest_csv(cache, "ItemSparse")
    if sparse_de and "deDE" in sparse_de.name:
        with sparse_de.open(encoding="utf-8") as f:
            for r in csv.DictReader(f):
                iid = int(r["ID"])
                if iid not in items_de and r.get("Display_lang"):
                    items_de[iid] = r["Display_lang"]

    out_items = {}
    counts = {}
    for iid, it in fmb_items.items():
        if not isinstance(iid, int) or iid <= 0 or not isinstance(it, dict):
            continue
        if it.get("bi") or it.get("u"):
            continue
        q = int(it.get("q") or 0)
        if q < 2:
            continue
        cls, sub, inv = item_db.get(iid, (0, 0, 0))
        if inv not in EQUIPPABLE_INV:
            continue
        sources = classify_sources(it, npcs, quests, places, place_maps, skills, factions)
        if not sources:
            continue
        st = fmb_stats.get(iid, "")
        rec = {
            "n": str(it.get("n") or ""),
            "q": q,
            "i": int(it.get("i") or 0),
            "r": int(it.get("r") or 0),
            "c": cls,
            "sc": sub,
            "inv": inv,
            "so": sources,
        }
        if iid in items_de and items_de[iid] != rec["n"]:
            rec["nd"] = items_de[iid]
        if st:
            rec["st"] = st
        if it.get("b"):
            rec["b"] = it["b"]
        if it.get("f") in ("alliance", "horde"):
            rec["f"] = it["f"]
        if isinstance(it.get("cl"), list) and it["cl"]:
            rec["cl"] = [CLASS_MAP.get(c.lower(), c.upper()) for c in it["cl"] if isinstance(c, str)]
        if "u1" in st:
            rec["u"] = True
        procs = fmb_procs.get(iid)
        if isinstance(procs, list) and procs:
            pr = []
            for p in procs:
                if isinstance(p, dict) and p.get("n"):
                    rate = f" ({p['r']})" if p.get("r") else ""
                    pr.append(f"{p['n']}{rate}")
            if pr:
                rec["pr"] = pr
        out_items[str(iid)] = rec
        for s in sources:
            counts[s["k"]] = counts.get(s["k"], 0) + 1

    payload = {
        "version": 1,
        "build": build,
        "placesDe": places_de,
        "items": out_items,
    }
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    raw = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    with gzip.open(out_path, "wb", compresslevel=9) as f:
        f.write(raw)
    print(f"Wrote {out_path}: {len(out_items)} items, raw={len(raw)//1024} KB, gz={out_path.stat().st_size//1024} KB")
    print("Source counts:", counts)


if __name__ == "__main__":
    main()
