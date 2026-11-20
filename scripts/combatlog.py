# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Combat log: what the character really did, next to what the combat model says.

Reads a combat log of the game (`/combatlog` writes Logs/WoWCombatLog-<date>.txt; advanced logging adds
health, mana, spell power and the enemy's level to the lines) and cuts it into fights: a fight
ends when nothing happened between the character and an enemy for some seconds. Per spell over all
fights: casts, hits, crits, misses, the average hit and tick before crit and mitigation next to the
model's numbers, what the enemies resisted, how fast the DoTs ticked, how much of the fight time each
debuff was up. Per fight: length, enemies, damage per second, mana at its start and end.

  uv run scripts/combatlog.py --char Malgruk            (the newest log)
  uv run scripts/combatlog.py --fight 12                (one fight cast by cast)

The log names every player who was near; this script reads only the lines of the character who wrote it
(the game marks them) and of that character's pets.
"""

from __future__ import annotations

import argparse
import csv
import datetime
import glob
import json
import os
import statistics
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import i18n  # noqa: E402
import rotations  # noqa: E402  (the character setup, labels, clock)
import simengine  # noqa: E402
import upgrades  # noqa: E402  (the hit table)
import writs  # noqa: E402  (DEFAULT_WOW)
from i18n import tr  # noqa: E402

GAP = 8.0  # seconds without anything between the character and an enemy: the fight is over
LEAD = 6.0  # a cast that starts at most this long before the first hit opens the fight
ADVANCED = 19  # fields advanced logging puts in front of the amounts
# Unit flags of the log (COMBATLOG_OBJECT_*).
MINE, FRIENDLY, PLAYER, PET, GUARDIAN = 0x1, 0x10, 0x400, 0x1000, 0x2000
SCHOOLS = {2: "holy", 4: "fire", 8: "nature", 16: "frost", 32: "shadow", 64: "arcane"}
DAMAGE = {"SPELL_DAMAGE", "SPELL_PERIODIC_DAMAGE", "RANGE_DAMAGE", "SWING_DAMAGE", "DAMAGE_SHIELD"}
MISSED = {"SPELL_MISSED", "SPELL_PERIODIC_MISSED", "RANGE_MISSED", "SWING_MISSED", "DAMAGE_SHIELD_MISSED"}
HEAL = {"SPELL_HEAL", "SPELL_PERIODIC_HEAL"}
ENERGIZE = {"SPELL_ENERGIZE", "SPELL_PERIODIC_ENERGIZE"}
AURA_ON = {"SPELL_AURA_APPLIED", "SPELL_AURA_REFRESH"}
NOBODY = "0000000000000000"
NOT_A_MISS = {"ABSORB", "IMMUNE", "EVADE", "REFLECT", "DEFLECT"}  # the cast was not missed or resisted
STALE = 300.0  # a debuff the log never saw end is forgotten after this many seconds
MAX_TIMELINE = 80
MAX_BUFFS = 20  # buffs the CLI names


def find_logs(wow: str) -> list[str]:
    """The combat logs of the game folder, newest first."""
    return sorted(glob.glob(os.path.join(wow, "Logs", "WoWCombatLog*.txt")), key=os.path.getmtime, reverse=True)


def read(path: str):
    """Yields (seconds, fields) per line. A line is a time stamp, two spaces and comma-separated fields
    (names in quotes). The stamp's digits after the milliseconds are the time zone."""
    days: dict[str, float] = {}
    stamp = [""]

    def payloads(f):
        for line in f:
            head, sep, rest = line.partition("  ")
            if sep:
                stamp[0] = head
                yield rest

    with open(path, encoding="utf-8", errors="replace", newline="") as f:
        for row in csv.reader(payloads(f)):
            day, _, clock = stamp[0].partition(" ")
            if day not in days:
                try:
                    m, d, y = (int(x) for x in day.split("/"))
                    days[day] = datetime.date(y, m, d).toordinal() * 86400.0
                except ValueError:
                    days[day] = 0.0
            try:
                h, mi, s = clock.split(":")
                yield days[day] + int(h) * 3600 + int(mi) * 60 + float(s[:6]), row
            except ValueError:
                continue


def num(x) -> float:
    try:
        return float(x)
    except (TypeError, ValueError):
        return 0.0


def flags(x: str) -> int:
    try:
        return int(x, 16)
    except (TypeError, ValueError):
        return 0


def collect(path: str) -> dict:
    """One pass over the log: the lines of the character who wrote it (the game marks its units as "mine")
    and of its pets, as plain events; when which unit died; the level of every unit seen."""
    info = {"version": 0, "advanced": False, "build": "", "first": 0.0, "last": 0.0, "lines": 0}
    chars: dict[str, dict] = {}  # player GUID -> {name, events}
    deaths: dict[str, list] = {}
    levels: dict[str, int] = {}
    encounters: list[dict] = []  # boss fights as the game marks them
    current = None  # the character the pets belong to: the one seen last
    for t, row in read(path):
        if not row:
            continue
        ev = row[0]
        info["lines"] += 1
        info["first"] = info["first"] or t
        info["last"] = t
        if ev == "COMBAT_LOG_VERSION":
            head = dict(zip(row[::2], row[1::2]))
            info.update(version=int(num(head.get("COMBAT_LOG_VERSION"))),
                        advanced=head.get("ADVANCED_LOG_ENABLED") == "1", build=head.get("BUILD_VERSION") or "")
            continue
        if ev == "ENCOUNTER_START" and len(row) > 2:
            encounters.append({"name": row[2], "start": t, "end": None, "kill": False})
            continue
        if ev == "ENCOUNTER_END" and encounters and encounters[-1]["end"] is None:
            encounters[-1].update(end=t, kill=len(row) > 5 and row[5] == "1")
            continue
        if len(row) < 9:
            continue
        src, src_name, src_flags, dst, dst_name, dst_flags = row[1], row[2], flags(row[3]), row[5], row[6], flags(row[7])
        if ev == "UNIT_DIED":
            deaths.setdefault(dst, []).append(t)
            continue
        adv = ADVANCED if info["advanced"] else 0
        swing = ev.startswith("SWING")
        if ev.startswith("ENVIRONMENTAL") or (not swing and len(row) < 12):
            continue  # falling and drowning are no fight; PARTY_KILL and the like have no spell
        at = 9 if swing else 12  # where the event's own fields start: a swing has no spell
        block = row[at:at + adv] if adv and len(row) >= at + adv else []
        # The block's last field is a creature's level; for a player it is the item level.
        if block and block[0].startswith(("Creature-", "Vehicle-")) and block[18].isdigit():
            levels[block[0]] = int(block[18])

        mine_src, mine_dst = bool(src_flags & MINE), bool(dst_flags & MINE)
        if not (mine_src or mine_dst):
            continue
        for guid, name, fl in ((src, src_name, src_flags), (dst, dst_name, dst_flags)):
            if fl & MINE and fl & PLAYER:
                current = chars.setdefault(guid, {"guid": guid, "name": name.split("-")[0], "events": []})
        if current is None:
            continue
        me = current["guid"]
        pet = mine_src and src != me and bool(src_flags & (PET | GUARDIAN))
        if mine_src and src != me and not pet:
            continue
        spell = (0, tr("Melee"), 1) if swing else (int(num(row[9])), row[10], flags(row[11]))
        e = {"t": t, "spell": spell[0], "name": spell[1], "school": spell[2], "dst": dst, "dstName": dst_name,
             "hostile": dst not in (NOBODY, me) and not dst_flags & FRIENDLY, "pet": src_name if pet else ""}
        rest = row[at + adv:]
        if ev in DAMAGE and len(rest) >= 8:
            e.update(amount=num(rest[0]), base=num(rest[1]), resisted=num(rest[4]),
                     blocked=num(rest[5]), absorbed=num(rest[6]), crit=rest[7] == "1",
                     periodic=ev == "SPELL_PERIODIC_DAMAGE")
            if mine_src and dst == me:
                e["kind"] = "self"  # a spell that burns its caster
            elif mine_src:
                e["kind"] = "damage"
            elif not src_flags & FRIENDLY:
                e["kind"] = "taken"
            else:
                continue
        elif ev in MISSED and mine_src:
            e.update(kind="miss", how=row[at] if len(row) > at else "", periodic=ev == "SPELL_PERIODIC_MISSED")
        elif ev in MISSED and not src_flags & FRIENDLY:
            e.update(kind="taken", amount=0.0)
        elif ev in HEAL and src == me and len(rest) >= 5:
            e.update(kind="heal", amount=num(rest[0]), base=num(rest[1]), over=num(rest[2]),
                     crit=rest[4] == "1", periodic=ev == "SPELL_PERIODIC_HEAL")
        elif ev in ENERGIZE and src == me and dst == me and rest:
            e.update(kind="energize", amount=num(rest[0]))
        elif ev == "SPELL_CAST_SUCCESS" and src == me:
            e["kind"] = "cast"
            if block and block[0] == me:
                e.update(hp=num(block[2]), hpMax=num(block[3]), power=num(block[5]))
                if block[10] == "0":
                    e.update(mana=num(block[11]), manaMax=num(block[12]), cost=num(block[13]))
        elif ev == "SPELL_CAST_START" and src == me:
            e["kind"] = "start"
        elif dst == me and len(row) > 12 and row[12] == "BUFF" and (ev in AURA_ON or ev == "SPELL_AURA_REMOVED"):
            # A buff on the character, whoever cast it: it explains spell power and regeneration.
            e.update(kind="buff+" if ev in AURA_ON else "buff-", own=src == me)
        elif ev in AURA_ON and mine_src and not pet and dst != me and len(row) > 12 and row[12] == "DEBUFF":
            e["kind"] = "aura+"
        elif ev == "SPELL_AURA_REMOVED" and mine_src and not pet and dst != me and len(row) > 12 and row[12] == "DEBUFF":
            e["kind"] = "aura-"
        else:
            continue
        current["events"].append(e)
    return {"info": info, "chars": chars, "deaths": deaths, "levels": levels, "encounters": encounters}


def fights_of(events: list, gap: float = GAP) -> list[tuple[float, float]]:
    """The fights as (start, end): what happens between the character and an enemy with at most `gap`
    seconds between two events. A cast that starts shortly before the first hit opens the fight."""
    out: list[list[float]] = []
    opener: dict | None = None
    for e in events:
        k = e["kind"]
        if k == "start":
            opener = e
            continue
        fights = (k in ("damage", "miss") and e["hostile"]) or k == "taken" or (k == "cast" and e["hostile"])
        if not fights:
            continue
        if out and e["t"] - out[-1][1] <= gap:
            out[-1][1] = e["t"]
        else:
            start = e["t"]
            if opener and opener["spell"] == e["spell"] and 0 <= start - opener["t"] <= LEAD and (
                    not out or opener["t"] > out[-1][1]):
                start = opener["t"]
            out.append([start, e["t"]])
    return [(a, b) for a, b in out]


def new_row(e: dict) -> dict:
    return {"spellId": e["spell"], "name": e["name"], "school": e["school"], "pet": e["pet"], "casts": 0,
            "hits": 0, "crits": 0, "ticks": 0, "tickCrits": 0, "misses": {}, "damage": 0.0, "resisted": 0.0,
            "absorbed": 0.0, "healing": 0.0, "overheal": 0.0, "heals": 0, "mana": 0.0, "energizes": 0,
            "selfDamage": 0.0, "_hit": [], "_tick": [], "_plain": [0.0, 0.0], "_crit": [0.0, 0.0],
            "_gaps": [], "_last": {}, "_cover": []}


def add(rows: dict, e: dict) -> dict:
    """Books one event on its spell's row (a pet's spells have rows of their own)."""
    r = rows.setdefault((e["spell"], e["pet"]), new_row(e))
    k = e["kind"]
    if k == "cast":
        r["casts"] += 1
    elif k == "damage":
        full = e["amount"] + e["resisted"] + e["absorbed"] + e["blocked"]
        r["damage"] += e["amount"]
        r["resisted"] += e["resisted"]
        r["absorbed"] += e["absorbed"]
        r["ticks" if e["periodic"] else "hits"] += 1
        if e["crit"]:
            r["tickCrits" if e["periodic"] else "crits"] += 1
        if e["base"] > 0:
            r["_tick" if e["periodic"] else "_hit"].append(e["base"])
            pair = r["_crit" if e["crit"] else "_plain"]
            pair[0] += full
            pair[1] += e["base"]
        if e["periodic"]:
            last = r["_last"].get(e["dst"])
            if last is not None and 0 < e["t"] - last <= 12:
                r["_gaps"].append(e["t"] - last)
            r["_last"][e["dst"]] = e["t"]
    elif k == "miss":
        r["misses"][e["how"]] = r["misses"].get(e["how"], 0) + 1
    elif k == "heal":
        r["healing"] += e["amount"] - e["over"]
        r["overheal"] += e["over"]
        r["heals"] += 1
    elif k == "energize":
        r["mana"] += e["amount"]
        r["energizes"] += 1
    elif k == "self":
        r["selfDamage"] += e["amount"]
    return r


def merge(rows: dict) -> None:
    """The game books some effects on a second spell of the same name: Life Tap's mana, the pulses of Hellfire
    and Rain of Fire. The row that was cast takes the rows of that name that never were."""
    names: dict = {}
    for key, r in rows.items():
        names.setdefault((r["name"], r["pet"]), []).append(key)
    for keys in names.values():
        cast = [k for k in keys if rows[k]["casts"]]
        rest = [k for k in keys if not rows[k]["casts"]]
        if len(cast) != 1 or not rest:
            continue
        into = rows[cast[0]]
        for k in rest:
            for field, v in rows.pop(k).items():
                if field in ("spellId", "name", "school", "pet", "_last"):
                    continue
                if field == "misses":
                    for how, n in v.items():
                        into[field][how] = into[field].get(how, 0) + n
                elif field in ("_plain", "_crit"):
                    into[field] = [a + b for a, b in zip(into[field], v)]
                else:
                    into[field] = into[field] + v  # numbers add up, lists join


def covered(spans: list[tuple[float, float]]) -> float:
    """Seconds covered by at least one of the spans."""
    total, end = 0.0, float("-inf")
    for a, b in sorted(spans):
        if b > end:
            total += b - max(a, end)
            end = b
    return total


def finish(r: dict, seconds: float, model: dict | None = None) -> dict:
    """A spell's row as it is given out: sums, averages, shares."""
    hit, tick, gaps = r.pop("_hit"), r.pop("_tick"), sorted(r.pop("_gaps"))
    plain, crit, cover = r.pop("_plain"), r.pop("_crit"), r.pop("_cover")
    r.pop("_last")
    # A cast that missed or was resisted in full, of the casts (a pet's casts are not in the log: of its hits).
    missed = sum(n for how, n in r["misses"].items() if how not in NOT_A_MISS)
    attempts = r["casts"] or r["hits"] + missed
    full = r["damage"] + r["resisted"] + r["absorbed"]
    out = dict(r, damage=round(r["damage"]), healing=round(r["healing"]), overheal=round(r["overheal"]),
               mana=round(r["mana"]), selfDamage=round(r["selfDamage"]), resisted=round(r["resisted"]),
               absorbed=round(r["absorbed"]),
               castsPerMin=round(r["casts"] / seconds * 60, 1) if seconds else 0.0,
               dps=round(r["damage"] / seconds, 1) if seconds else 0.0,
               critPct=round(r["crits"] / r["hits"] * 100, 1) if r["hits"] else None,
               tickCritPct=round(r["tickCrits"] / r["ticks"] * 100, 1) if r["ticks"] else None,
               missed=missed, missPct=round(min(missed / attempts, 1) * 100, 1) if attempts else None,
               resistedPct=round(r["resisted"] / full * 100, 1) if full else 0.0,
               avgHit=round(statistics.fmean(hit), 1) if hit else None,
               avgTick=round(statistics.fmean(tick), 1) if tick else None,
               # What the enemies added to or took off the amount before any resist, and what a crit multiplies.
               takenPct=round((plain[0] / plain[1] - 1) * 100, 1) if plain[1] else None,
               critMult=round(crit[0] / crit[1] / (plain[0] / plain[1]), 2) if crit[1] and plain[1] and plain[0] else None,
               tickSec=round(statistics.median(gaps), 2) if gaps else None,
               tickSecFast=round(gaps[len(gaps) // 10], 2) if gaps else None,
               tickSecSlow=round(gaps[len(gaps) * 9 // 10], 2) if gaps else None,
               uptimePct=round(covered(cover) / seconds * 100, 1) if cover and seconds else None)
    if model:
        m = {}
        if model.get("directMin") or model.get("directMax"):
            m["hit"] = round(((model.get("directMin") or 0) + (model.get("directMax") or 0)) / 2, 1)
        if model.get("periodic") and model.get("ticks"):
            m["tick"] = round(model["periodic"] / model["ticks"], 1)
        if out["avgHit"] and not out["avgTick"] and "hit" not in m and "tick" in m:
            m["hit"] = m.pop("tick")  # an area channel: the log has every pulse as a hit of its own
        for mine, theirs in (("avgHit", "hit"), ("avgTick", "tick")):
            if out[mine] and m.get(theirs):
                m[theirs + "DiffPct"] = round((out[mine] / m[theirs] - 1) * 100, 1)
        if m:
            out["model"] = m
    return out


def clock_of(t: float) -> str:
    s = int(t % 86400)
    return f"{s // 3600:02d}:{s // 60 % 60:02d}:{s % 60:02d}"


def snapshot_of(wow: str, char: str) -> dict:
    """The character's WoWMCP snapshot, or nothing: the log itself does not say the character's level."""
    files = writs.find_chars(wow, char)
    return upgrades.load_char(files[0]) if files else {}


def model_numbers(wow: str, char: str, spell_power: float = 0) -> dict:
    """What the combat model says one cast of each known spell rank does (as a tooltip shows it), at the
    spell power given: the log's, which moves with the buffs, instead of the snapshot's. The log has one
    number for it, which is the snapshot's highest school."""

    def at_power(c: dict) -> None:
        schools = ((c.get("character") or {}).get("stats") or {}).get("spellDamage") or {}
        if spell_power and schools:
            delta = spell_power - max(schools.values())
            for k in schools:
                schools[k] += delta

    c, _, start = rotations.character(wow, char, prepare=at_power)
    ch = c.get("character") or {}
    eng = start(simengine.default_metric(ch.get("classFile") or "", simengine.spec_of(c)))
    eng.batch([])
    return {n.get("spellId"): n for n in eng.spells}


def compute(wow: str = writs.DEFAULT_WOW, char: str = "", file: str = "", gap: float = GAP, model: bool = True) -> dict:
    """The evaluation of a combat log as plain data."""
    path = file or next(iter(find_logs(wow)), "")
    if not path or not os.path.isfile(path):
        raise FileNotFoundError(tr("No combat log found. Type /combatlog in the game before the fight; "
                                   "the game writes it to its Logs folder."))
    log = collect(path)
    chars = sorted(log["chars"].values(), key=lambda c: -len(c["events"]))
    if char:
        chars = [c for c in chars if c["name"].lower().startswith(char.lower())]
    if not chars:
        raise LookupError(tr("The combat log has no fights of this character."))
    me = chars[0]
    events = me["events"]
    spans = fights_of(events, gap)

    snapshot = snapshot_of(wow, me["name"])
    label = rotations.labeler(snapshot)

    rows: dict = {}
    fights, auras, i = [], {}, 0
    buffs, buff_rows = {}, {}  # the buffs up right now, and per buff when it was up during the fights
    level, power = (snapshot.get("character") or {}).get("level") or 0, []
    by_delta: dict = {}  # casts on enemies and how many missed, by the enemy's level above the character
    for start, end in spans:
        mine = []
        while i < len(events) and events[i]["t"] <= end:
            e = events[i]
            i += 1
            # Debuffs are followed outside the fights too: one may be older than the fight it works in.
            if e["kind"] == "aura+":
                auras.setdefault((e["spell"], e["dst"]), e)
            elif e["kind"] == "aura-":
                on = auras.pop((e["spell"], e["dst"]), None)
                if on is not None and e["t"] > start:
                    add(rows, dict(on, kind=""))["_cover"].append((max(on["t"], start), e["t"]))
            elif e["kind"] == "buff+":
                buffs.setdefault(e["spell"], e)
            elif e["kind"] == "buff-":
                on = buffs.pop(e["spell"], None)
                if on is not None and e["t"] > start:
                    buff_rows.setdefault(on["spell"], dict(on, cover=[]))["cover"].append((max(on["t"], start), e["t"]))
            if e["t"] >= start:
                mine.append(e)
        for on in buffs.values():
            buff_rows.setdefault(on["spell"], dict(on, cover=[]))["cover"].append((max(on["t"], start), end))
        for key, on in list(auras.items()):  # still up when the fight ends
            if on["t"] < start - STALE:
                del auras[key]
            else:
                add(rows, dict(on, kind=""))["_cover"].append((max(on["t"], start), end))
        f_rows: dict = {}
        enemies: dict = {}
        mana, hp, casts, taken = [], [], [], 0.0
        for e in mine:
            k = e["kind"]
            if k in ("aura+", "aura-", "buff+", "buff-", "start"):
                continue
            if k == "taken":
                taken += e["amount"]
                continue
            add(rows, e)
            add(f_rows, e)
            if k in ("damage", "miss") and e["hostile"]:
                enemies.setdefault(e["dst"], e["dstName"])
            if e["hostile"] and not e["pet"] and level and e["dst"] in log["levels"]:
                d = by_delta.setdefault(log["levels"][e["dst"]] - level, {"casts": 0, "misses": 0})
                if k == "cast":
                    d["casts"] += 1
                elif k == "miss" and not e["periodic"] and e["how"] not in NOT_A_MISS:
                    d["misses"] += 1
            if k == "cast":
                if e.get("manaMax"):
                    mana.append(e["mana"] / e["manaMax"] * 100)
                if e.get("hpMax"):
                    hp.append(e["hp"] / e["hpMax"] * 100)
                    power.append(e["power"])
                casts.append({"t": round(e["t"] - start, 1), "spellId": e["spell"],
                              "label": label(e["spell"], e["name"], 0),
                              "target": e["dstName"] if e["hostile"] else "",
                              "manaPct": round(mana[-1]) if e.get("manaMax") else None,
                              "healthPct": round(hp[-1]) if e.get("hpMax") else None})
        seconds = max(end - start, 1.0)
        damage = sum(r["damage"] for r in f_rows.values())
        if not damage and not any(r["healing"] for r in f_rows.values()):
            continue
        deltas = [log["levels"][g] - level for g in enemies if g in log["levels"] and level]
        merge(f_rows)
        boss = next((x for x in log["encounters"] if x["start"] <= end and (x["end"] or x["start"]) >= start), None)
        fights.append({
            "encounter": boss["name"] if boss else "",
            "n": len(fights) + 1, "start": clock_of(start), "seconds": round(seconds, 1),
            "enemies": len(enemies), "targets": sorted(set(enemies.values())),
            "levelDelta": max(deltas) if deltas else None,
            "kills": sum(1 for g in enemies if any(start <= t <= end + 2 for t in log["deaths"].get(g, []))),
            "damage": round(damage), "dps": round(damage / seconds, 1),
            "healing": round(sum(r["healing"] for r in f_rows.values())),
            "taken": round(taken),
            "manaStartPct": round(mana[0]) if mana else None, "manaEndPct": round(mana[-1]) if mana else None,
            "manaLowPct": round(min(mana)) if mana else None,
            "healthLowPct": round(min(hp)) if hp else None,
            "spells": sorted(({"spellId": r["spellId"], "label": label(r["spellId"], r["name"], 0), "pet": r["pet"],
                               "casts": r["casts"], "damage": round(r["damage"]),
                               "healing": round(r["healing"]), "mana": round(r["mana"])}
                              for r in f_rows.values() if r["casts"] or r["damage"] or r["healing"] or r["mana"]),
                             key=lambda x: -(x["damage"] + x["healing"])),
            "casts": casts[:MAX_TIMELINE], "moreCasts": max(0, len(casts) - MAX_TIMELINE),
        })

    active = sum(f["seconds"] for f in fights)
    numbers, model_error, model_power = {}, "", round(statistics.median(power)) if power else 0
    if model:
        try:
            numbers = model_numbers(wow, me["name"], model_power)
        except (FileNotFoundError, LookupError) as e:
            model_error = str(e)
    merge(rows)
    total = sum(f["damage"] for f in fights)
    spell_hit = ((snapshot.get("character") or {}).get("stats") or {}).get("spellHit") or 0
    hit = [{"levelDelta": d, "casts": v["casts"], "misses": v["misses"],
            "missPct": round(v["misses"] / v["casts"] * 100, 1) if v["casts"] else None,
            "modelMissPct": (max(upgrades.MIN_MISS, upgrades.SPELL_MISS[min(max(d, 0), 3)] - spell_hit)
                             if snapshot and d <= 3 else None)}
           for d, v in sorted(by_delta.items()) if v["casts"]]

    spells = []
    for r in rows.values():
        out = finish(r, active, numbers.get(r["spellId"]) if not r["pet"] else None)
        if not (out["casts"] or out["damage"] or out["healing"] or out["mana"] or out["selfDamage"]):
            continue
        out["label"] = label(out["spellId"], out["name"], 0)
        out["sharePct"] = round(out["damage"] / total * 100, 1) if total else 0.0
        crit = (((snapshot.get("character") or {}).get("stats") or {}).get("spellCrit") or {}).get(
            SCHOOLS.get(out["school"], ""))
        if crit is not None and not out["pet"]:
            out["sheetCritPct"] = round(crit, 1)
        spells.append(out)
    spells.sort(key=lambda x: -(x["damage"] + x["healing"]))

    info = log["info"]
    stats = (snapshot.get("character") or {}).get("stats") or {}
    return {
        "file": os.path.basename(path), "modified": os.path.getmtime(path),
        "started": clock_of(info["first"]), "seconds": round(info["last"] - info["first"]),
        "lines": info["lines"], "version": info["version"], "advanced": info["advanced"], "build": info["build"],
        "name": me["name"], "level": level or None, "others": [c["name"] for c in chars[1:]],
        "spellPower": {"low": round(min(power)), "high": round(max(power))} if power else None,
        "sheetSpellDamage": stats.get("spellDamage") or None,
        "modelSpellPower": model_power if numbers and model_power else None,
        "total": {"fights": len(fights), "seconds": round(active, 1), "damage": round(total),
                  "dps": round(total / active, 1) if active else 0.0,
                  "healing": sum(f["healing"] for f in fights),
                  "kills": sum(f["kills"] for f in fights)},
        "spells": spells, "hit": hit, "fights": fights,
        # Buffs the log saw start (one that was up before the log began is not in it).
        "buffs": sorted(({"spellId": b["spell"], "name": b["name"], "own": b["own"],
                          "uptimePct": round(covered(b["cover"]) / active * 100, 1)}
                         for b in buff_rows.values() if active and covered(b["cover"]) > 0),
                        key=lambda b: (-b["uptimePct"], b["name"])),
        "model": bool(numbers), "modelError": model_error,
    }


def cell(x, digits: int = 1, unit: str = "") -> str:
    return "-" if x is None else f"{x:.{digits}f}{unit}"


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--char", default="", help="start of the character's name (default: the one with the most lines)")
    ap.add_argument("--file", default="", help="the log to read (default: the newest in the game's Logs folder)")
    ap.add_argument("--gap", type=float, default=GAP, help="seconds without an event that end a fight")
    ap.add_argument("--fight", type=int, default=0, help="show this fight cast by cast")
    ap.add_argument("--no-model", action="store_true", help="do not ask the combat model for its numbers")
    ap.add_argument("--wow", default=writs.DEFAULT_WOW)
    ap.add_argument("--json", action="store_true")
    i18n.add_argument(ap)
    a = ap.parse_args()
    i18n.set_language(a.lang, a.wow)
    sys.stdout.reconfigure(encoding="utf-8")
    try:
        r = compute(a.wow, a.char, a.file, a.gap, not a.no_model)
    except (FileNotFoundError, LookupError) as e:
        sys.exit(str(e))
    if a.json:
        print(json.dumps(r, ensure_ascii=False, indent=1))
        return

    t = r["total"]
    print(tr("{file}: {name}, {fights} fights, {time} fighting, {damage:,.0f} damage, {dps:.1f} DPS while fighting",
             file=r["file"], name=r["name"], fights=t["fights"], time=rotations.clock(t["seconds"]),
             damage=t["damage"], dps=t["dps"]))
    if not r["advanced"]:
        print(tr("The log was written without advanced logging: no mana, no health and no enemy levels. "
                 "Turn it on in the game (System > Network > Advanced Combat Logging)."))
    if r["modelError"]:
        print(tr("No numbers of the combat model: {err}", err=r["modelError"]))
    if r["spellPower"]:
        line = tr("Spell power in the log: {low} to {high}.", **r["spellPower"])
        if r["modelSpellPower"]:
            line += " " + tr("The model's numbers are at {power}, the log's median.", power=r["modelSpellPower"])
        print(line)

    if a.fight:
        f = next((x for x in r["fights"] if x["n"] == a.fight), None)
        if not f:
            sys.exit(tr("The log has no fight {n}.", n=a.fight))
        print(fight_line(f))
        for c in f["casts"]:
            state = f"[{cell(c['manaPct'], 0)}/{cell(c['healthPct'], 0)}]"
            print(f"  {c['t']:6.1f} s  {state:10} {c['label']}" + (f" > {c['target']}" if c["target"] else ""))
        if f["moreCasts"]:
            print("  " + tr("... and {n} more casts", n=f["moreCasts"]))
        return

    head = [tr("Spell"), tr("Casts"), tr("/min"), tr("Hits"), tr("Crit"), tr("Missed"), tr("Avg hit"), tr("Model"),
            tr("Ticks"), tr("Crit"), tr("Avg tick"), tr("Model"), tr("Tick every"), tr("Up"), tr("Enemy"), tr("Crit x"),
            tr("Resisted"), tr("Damage"), tr("Share")]
    table = [head]
    for s in r["spells"]:
        m = s.get("model") or {}
        name = s["label"] + (f" ({s['pet']})" if s["pet"] else "")
        ticks = f"{s['tickSecFast']:.1f}/{s['tickSec']:.1f}/{s['tickSecSlow']:.1f} s" if s["tickSec"] else "-"
        table.append([name, str(s["casts"]), cell(s["castsPerMin"]), str(s["hits"]), cell(s["critPct"], 0, "%"),
                      cell(s["missPct"], 0, "%"), cell(s["avgHit"]), cell(m.get("hit")), str(s["ticks"]),
                      cell(s["tickCritPct"], 0, "%"), cell(s["avgTick"]), cell(m.get("tick")), ticks, cell(s["uptimePct"], 0, "%"),
                      "-" if s["takenPct"] is None else f"{round(s['takenPct']) or 0:+d}%", cell(s["critMult"], 2),
                      cell(s["resistedPct"], 1, "%"), f"{s['damage']:,}", cell(s["sharePct"], 1, "%")])
    widths = [max(len(row[i]) for row in table) for i in range(len(head))]
    for row in table:
        print("  ".join(c.ljust(w) if i == 0 else c.rjust(w) for i, (c, w) in enumerate(zip(row, widths))).rstrip())
    print(tr("Avg hit and avg tick are the amounts before crit and mitigation, as a tooltip shows them; "
             "\"Enemy\" is what the enemies added to them (debuffs) and \"Crit x\" what a crit multiplied. "
             "\"Tick every\" is the fastest tenth, the median and the slowest tenth of the time between two ticks."))
    for s in r["spells"]:
        extra = []
        if s["mana"]:
            extra.append(tr("{n:,} mana, {count}x ({each:.0f} each)", n=s["mana"], count=s["energizes"],
                            each=s["mana"] / max(s["energizes"], 1)))
        if s["healing"] or s["overheal"]:
            extra.append(tr("{n:,} healing, {over:,} overhealing", n=s["healing"], over=s["overheal"]))
        if s["selfDamage"]:
            extra.append(tr("{n:,} damage to yourself", n=s["selfDamage"]))
        if extra:
            print(f"  {s['label']}: " + ", ".join(extra))
    if r["buffs"]:
        print(tr("Buffs on you, of the time fighting: {list}",
                 list=", ".join(f"{b['name']} {b['uptimePct']:.0f}%" for b in r["buffs"][:MAX_BUFFS])))
    if r["hit"]:
        print(tr("Casts on enemies that missed, by the enemy's level above yours:"))
        for h in r["hit"]:
            line = tr("  {delta:+d}: {misses} of {casts} ({pct:.1f}%)", delta=h["levelDelta"], misses=h["misses"],
                      casts=h["casts"], pct=h["missPct"])
            if h["modelMissPct"] is not None:
                line += tr(", the model expects {pct:.0f}%", pct=h["modelMissPct"])
            print(line)
    print(tr("Fights (--fight N shows one cast by cast):"))
    for f in r["fights"]:
        print(fight_line(f))


def fight_line(f: dict) -> str:
    targets = ", ".join(f["targets"][:3]) + (", ..." if len(f["targets"]) > 3 else "")
    line = tr("{n:3d}  {start}  {time:>8}  enemies {enemies}  {damage:>7,} damage  {dps:7.1f} DPS",
              n=f["n"], start=f["start"], time=rotations.clock(f["seconds"]), enemies=f["enemies"],
              damage=f["damage"], dps=f["dps"])
    if f["manaStartPct"] is not None:
        line += tr("  mana {a}% > {b}%", a=f["manaStartPct"], b=f["manaEndPct"])
    if f["levelDelta"] is not None:
        line += tr("  level {delta:+d}", delta=f["levelDelta"])
    return line + f"  {targets}" + (f"  [{f['encounter']}]" if f["encounter"] else "")


if __name__ == "__main__":
    main()
