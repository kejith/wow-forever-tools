# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Damage analysis: which rotation to cast when, and every damage spell rank with what one cast does.

The rotations: the most damage the character can do if mana is no limit (and how long the mana pays for it),
the damage it can sustain over ten minutes, and the other fights of the rotation guide, each as a priority
list. Per spell rank: the hit as the tooltip shows it, the average with crits and misses, mana, cast time,
damage per second (kept on the enemy for a spell over time, per cooldown for a spell with one), damage per
second of casting, damage per mana, how much of it comes from spell power (the rank's effective coefficient),
how long the mana lasts casting nothing else, and the fights in which the rotation casts the rank. Plus what a
point of each stat adds to the damage.

  uv run scripts/damage.py --char Malgruk

Reads the character from WoWMCP.lua; the spell numbers come from the game tables the combat engine reads.
"""

from __future__ import annotations

import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import i18n  # noqa: E402
import rotations  # noqa: E402  (the character setup, the guide's sentences)
import simengine  # noqa: E402
import writs  # noqa: E402  (DEFAULT_WOW)
from i18n import mark, tr  # noqa: E402

METRIC = "dps"
LONG_FIGHT = 300.0  # the second fight the stat weights are given for
# What a point of each stat adds: the engine's delta key, the label, how much is added for the measurement
# (enough to move the result, small enough to stay a slope).
WEIGHTS = [("sp", mark("Spell power"), 10), ("int", mark("Intellect"), 10), ("spi", mark("Spirit"), 10),
           ("sta", mark("Stamina"), 10), ("mp5", mark("Mana per 5 sec."), 5), ("hit", mark("Spell hit, per 1%"), 1),
           ("crit", mark("Spell crit, per 1%"), 1)]
SCHOOLS = {"shadow": mark("Shadow"), "fire": mark("Fire"), "holy": mark("Holy"), "frost": mark("Frost"),
           "nature": mark("Nature"), "arcane": mark("Arcane"), "physical": mark("Physical")}
# The order of the rotations: the two ends first (everything at once, what lasts), then the other fights.
ORDER = ["max", "endurance", "standard", "quick", "boss", "pack", "crowd"]
# Kinds of rows that are cast back to back: what "casting nothing else" and the best per second compare.
FILLERS = ("direct", "channel")


def supported(class_file: str | None) -> bool:
    """Whether the combat engine has a damage metric for the class."""
    return METRIC in (simengine.METRICS.get(class_file or "") or [])


def weights_of(short: dict, long: dict, seconds: float) -> dict:
    """Stat weights from the gains of two fights: damage per second a point adds, and how many points of
    spell power do the same."""
    rows = []
    for key, label, amount in WEIGHTS:
        row = {"key": key, "label": tr(label)}
        for name, gains in (("short", short), ("long", long)):
            per = (gains.get(key) or 0.0) / amount
            base = (gains.get("sp") or 0.0) / WEIGHTS[0][2]
            row[name] = round(per, 3)
            row[name + "Power"] = round(per / base, 2) if base > 0 else None
        rows.append(row)
    return {"seconds": seconds, "longSeconds": LONG_FIGHT, "rows": rows}


def row_out(r: dict, label, uses: dict, casting: float, mana_max: float) -> dict:
    """One rank of the engine's damage table with the columns derived from it."""
    cast, mana, execute = r.get("perCast") or 0.0, r.get("mana") or 0.0, r.get("executeSec") or 0.0
    kind = r.get("kind") or "direct"
    duration, cooldown = r.get("durationSec") or 0.0, r.get("cooldownSec") or 0.0
    per_cast_sec = r.get("dps") or 0.0
    # What the spell does per second when it is used as much as it can be on one enemy: a spell over time
    # is kept up, a cooldown is used whenever it is ready, everything else is cast back to back.
    dps, limit = per_cast_sec, None
    if cooldown > execute:
        dps, limit = cast / max(cooldown, duration), "cooldown"
    elif kind in ("dot", "both") and duration > execute:
        dps, limit = cast / duration, "kept"
    # Casting nothing else: how long the mana lasts and what it does until then.
    spam_oom = spam_total = None
    spammable = kind in FILLERS and not limit
    if spammable and execute > 0 and mana > 0:
        net = mana / execute - casting
        if net > 0:
            spam_oom = mana_max / net
            spam_total = spam_oom * cast / execute
    return {
        "spellId": r["spellId"], "baseId": r.get("baseId"), "name": r.get("name") or "",
        "label": label(r["spellId"], r.get("name"), r.get("rank")), "rank": r.get("rank") or 0,
        "level": r.get("level") or 0, "top": bool(r.get("top")), "kind": kind,
        "school": r.get("school") or "", "schoolLabel": tr(SCHOOLS[r["school"]]) if r.get("school") in SCHOOLS else "",
        "min": round(r.get("min") or 0), "max": round(r.get("max") or 0),
        "perTick": round(r.get("perTick") or 0, 1), "ticks": r.get("ticks") or 0, "durationSec": duration,
        "critPct": round(r.get("critPct") or 0, 1), "tickCrits": bool(r.get("tickCrits")),
        "hitPct": round(r.get("hitPct") or 0, 1), "perCast": round(cast, 1), "mana": round(mana),
        "castSec": round(r.get("castSec") or 0, 2), "executeSec": round(execute, 2), "cooldownSec": cooldown,
        "perCastSec": round(per_cast_sec, 1), "dps": round(dps, 1), "limit": limit,
        "dpm": round(r.get("dpm") or 0, 2), "bonus": round(r.get("bonus") or 0, 1),
        "bonusPct": round((r.get("bonus") or 0) / cast * 100, 1) if cast else 0,
        "coefPct": round(r.get("coefPct") or 0, 1), "leech": round(r.get("leech") or 0),
        "selfDamage": round(r.get("selfDamage") or 0), "area": bool(r.get("area")),
        "castsPerBar": round(mana_max / mana, 1) if mana else None, "spammable": spammable,
        "spamSeconds": round(spam_oom, 1) if spam_oom else None,
        "spamDamage": round(spam_total) if spam_total else None,
        "uses": uses.get(r["spellId"]) or [], "best": [],
    }


def compute(wow: str = writs.DEFAULT_WOW, char: str = "", fight_seconds: float = 60.0, target_delta: int = -1,
            kills_per_minute: float = -1) -> dict:
    """The damage analysis of a character as plain data."""
    c, fight, start = rotations.character(wow, char, fight_seconds, target_delta, kills_per_minute)
    ch = c.get("character") or {}
    class_file = ch.get("classFile") or ""
    if not supported(class_file):
        raise LookupError(tr("The combat engine has no damage spells for this class ({cls}).", cls=class_file or "?"))
    label = rotations.labeler(c)
    evals = [(key, {key: amount}) for key, _, amount in WEIGHTS]

    eng = start(METRIC)
    table, situations = eng.damage_table()
    if not table.get("rows"):
        raise LookupError(tr("This character knows no damage spell the combat engine models."))
    baseline = dict(eng.baseline)
    short = eng.batch(evals)
    long = start(METRIC, LONG_FIGHT).batch(evals)

    # The rotations, and where they cast each rank.
    guide = {s["key"]: rotations.situation_out(s, label, rotations.UNITS[METRIC]) for s in situations}
    played = [guide[k] for k in ORDER if k in guide] + [s for k, s in guide.items() if k not in ORDER]
    uses: dict[int, list] = {}
    for s in played:
        for st in s["steps"]:
            if st.get("spellId") and st["kind"] not in ("mana", "lifetap"):
                uses.setdefault(st["spellId"], []).append({"key": s["key"], "title": s["title"]})

    casting, idle = table.get("regenCastingPerSec") or 0.0, table.get("regenIdlePerSec") or 0.0
    mana_max = table.get("manaMax") or 0.0
    rows = [row_out(r, label, uses, casting, mana_max) for r in table["rows"]]
    # The marks: the most damage per mana of all, and the most per second among the spells cast back to back
    # on one enemy.
    priced = [r for r in rows if r["dpm"] > 0]
    if priced:
        max(priced, key=lambda r: r["dpm"])["best"].append("dpm")
    back_to_back = [r for r in rows if r["spammable"] and not r["area"]]
    if back_to_back:
        max(back_to_back, key=lambda r: r["dps"])["best"].append("dps")

    delta = table.get("targetDelta") or 0
    stats = {"schools": [{"school": s["school"], "label": tr(SCHOOLS[s["school"]]) if s["school"] in SCHOOLS else s["school"],
                          "power": round(s.get("power") or 0), "critPct": round(s.get("critPct") or 0, 2)}
                         for s in table.get("schools") or []],
             "missPct": round(table.get("missPct") or 0, 1), "targetDelta": delta,
             "manaMax": round(mana_max), "healthMax": round(table.get("healthMax") or 0),
             "castingPer5": round(casting * 5, 1), "idlePer5": round(idle * 5, 1),
             "wandDps": round(table.get("wandDps") or 0, 1),
             "tapMana": round(table.get("tapMana") or 0), "tapHealth": round(table.get("tapHealth") or 0)}
    notes = [tr("Every number is against an enemy {delta} levels above you, with your talents in it and the "
                "buffs you had at your last snapshot.", delta=delta),
             tr("A row of the spell table is one cast alone: what your other spells add to it (a curse on the "
                "enemy, talents that count your spells over time) shows in the rotations."),
             tr("The rotations cast the highest rank of each spell; a lower rank is in the table for its "
                "damage per mana.")]

    out_rotations = [{"key": s["key"], "title": s["title"], "about": s["about"], "value": s["value"],
                      "oomSeconds": s["oomSeconds"], "freeMana": s["freeMana"], "verdict": s["verdict"],
                      "seconds": (s.get("fight") or {}).get("seconds") or 0,
                      "steps": [{"label": st["label"], "rule": st["rule"], "kind": st["kind"], "value": st["value"],
                                 "sharePct": st["sharePct"], "perMin": st["perMin"]} for st in s["steps"]],
                      "skipped": [{"label": k["label"], "loss": k["loss"]} for k in s["skipped"]]}
                     for s in played]
    return {"name": ch.get("name") or char, "level": ch.get("level") or 60, "class": ch.get("class") or class_file,
            "classFile": class_file, "spec": simengine.spec_of(c), "unit": rotations.UNITS[METRIC],
            "rows": rows, "stats": stats, "notes": notes, "rotations": out_rotations,
            # The gear rating's number for the fight asked for: a faster estimate of the same fight.
            "rated": {"seconds": fight_seconds, "value": round(baseline.get("value") or 0, 1)},
            "weights": weights_of(short, long, fight_seconds),
            "engine": {"version": eng.version, "spellbookSource": eng.spellbook_source},
            "fight": {"seconds": fight_seconds, "targetDelta": fight["targetDelta"]}}


def hit_text(r: dict) -> str:
    """What one cast does without a crit, as a tooltip says it."""
    parts = []
    if r["max"]:
        parts.append(f"{r['min']}-{r['max']}" if r["min"] != r["max"] else str(r["min"]))
    if r["ticks"]:
        parts.append(tr("{ticks:.0f} x {tick:.0f} over {seconds:.0f} s", ticks=r["ticks"], tick=r["perTick"],
                        seconds=r["durationSec"]))
    return " + ".join(parts)


def dps_text(r: dict) -> str:
    """The damage per second of a row with what limits it."""
    text = f"{r['dps']:.1f}" + ("*" if "dps" in r["best"] else "")
    if r["limit"] == "kept":
        return tr("{dps} kept up", dps=text)
    if r["limit"] == "cooldown":
        return tr("{dps} per cooldown", dps=text)
    return text


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--wow", default=writs.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--fight-seconds", type=float, default=60, help="length of the normal fight")
    ap.add_argument("--target-delta", type=int, default=-1, choices=[-1, 0, 1, 2, 3],
                    help="enemy levels above you (default: 2 below level 60, else 3)")
    ap.add_argument("--kills-per-minute", type=float, default=-1,
                    help="your killing blows per minute, for buffs like Spirit Tap (default: one per fight)")
    ap.add_argument("--all-ranks", action="store_true", help="list every known rank, not only the highest")
    ap.add_argument("--json", action="store_true", help="print the result as JSON (for the app)")
    i18n.add_argument(ap)
    a = ap.parse_args()
    i18n.set_language(a.lang, a.wow)
    try:
        r = compute(a.wow, a.char, a.fight_seconds, a.target_delta, a.kills_per_minute)
    except (FileNotFoundError, LookupError) as e:
        sys.exit(str(e))
    sys.stdout.reconfigure(encoding="utf-8")
    if a.json:
        import json
        print(json.dumps(r, ensure_ascii=False))
        return

    s, unit = r["stats"], r["unit"]
    print(tr("{name}, level {level} {cls} | combat engine {version}", name=r["name"], level=r["level"],
             cls=r["class"], version=r["engine"]["version"]))
    if r["engine"]["spellbookSource"] == "assumed":
        print(tr("No spellbook in the export: every trainer rank up to your level is assumed."))
    print(" | ".join(tr("{school}: {power} spell power, {crit:.1f}% crit", school=x["label"], power=x["power"],
                        crit=x["critPct"]) for x in s["schools"]))
    line = [tr("{miss:g}% of your spells miss an enemy {delta} levels above you", miss=s["missPct"],
               delta=s["targetDelta"]), tr("mana {mana}", mana=s["manaMax"])]
    if s["wandDps"]:
        line.append(tr("wand {dps:.1f} DPS", dps=s["wandDps"]))
    print(" | ".join(line))
    line = [tr("Mana per 5 sec.: {casting:.1f} while casting, {idle:.1f} outside the five-second rule",
               casting=s["castingPer5"], idle=s["idlePer5"])]
    if s["tapMana"]:
        line.append(tr("Life Tap: {mana} mana for {health} health", mana=s["tapMana"], health=s["tapHealth"]))
    print(" | ".join(line))
    for n in r["notes"]:
        print("  " + n)

    print("\n" + tr("Which rotation when"))
    for rot in r["rotations"]:
        head = f"{rot['title']}: {rot['value']:.1f} {unit}"
        if rot["freeMana"]:
            head += ", " + (tr("your mana pays for all of it") if rot["oomSeconds"] is None
                            else tr("your mana pays for {time} of it", time=rotations.clock(rot["oomSeconds"])))
        print(f"  {head}")
        print(f"    {rot['about']} {rot['verdict']}")
        for i, st in enumerate(rot["steps"], 1):
            share = f" ({st['value']:.1f} {unit})" if st["value"] else ""
            print(f"    {i}. {st['label']}{share}: {st['rule']}")
        if rot["skipped"]:
            print("    " + tr("Left out: {spells}", spells=", ".join(
                tr("{label} (-{loss:.1f})", label=k["label"], loss=k["loss"]) for k in rot["skipped"])))
    rated = r["rated"]
    print("  " + tr("Your gear is rated by a faster estimate of the normal fight: {value:.1f} {unit} over "
                    "{seconds:.0f} s.", value=rated["value"], unit=unit, seconds=rated["seconds"]))

    shown = [row for row in r["rows"] if a.all_ranks or row["top"] or row["best"]]
    print("\n" + (tr("Every rank you know") if a.all_ranks
                  else tr("Your spells in their highest rank (--all-ranks lists every rank)")))
    head = [tr("Spell"), tr("One cast"), tr("Average"), tr("Mana"), tr("Cast"), unit, tr("Per cast second"),
            tr("Per mana"), tr("Coefficient"), tr("Nothing else"), tr("Cast in")]
    lines = []
    for row in shown:
        alone = "-"
        if row["spammable"]:
            alone = tr("lasts") if row["spamSeconds"] is None else rotations.clock(row["spamSeconds"])
        lines.append([row["label"], hit_text(row), f"{row['perCast']:.0f}", str(row["mana"]),
                      tr("{n:.1f} s", n=row["castSec"]) if row["castSec"] else tr("instant"), dps_text(row),
                      f"{row['perCastSec']:.1f}", f"{row['dpm']:.2f}" + ("*" if "dpm" in row["best"] else ""),
                      f"{row['coefPct']:.0f}%", alone, ", ".join(u["title"] for u in row["uses"]) or "-"])
    if s["wandDps"]:
        lines.append([tr("Wand"), "", "", "0", "", f"{s['wandDps']:.1f}", f"{s['wandDps']:.1f}", "", "",
                      tr("lasts"), ""])
    widths = [max(len(x[i]) for x in [head] + lines) for i in range(len(head))]
    for x in [head] + lines:
        print("  ".join(v.ljust(w) for v, w in zip(x, widths)).rstrip())
    print(tr("* the most damage per mana, and the most per second among the spells you can cast back to back"))

    w = r["weights"]
    print("\n" + tr("What a point adds ({unit}; in brackets: the points of spell power that do the same)", unit=unit))
    for row in w["rows"]:
        def cell(name: str) -> str:
            eq = row[name + "Power"]
            return f"{row[name]:.3f}" + (f" ({eq:.2f})" if eq is not None else "")
        print("  " + tr("{label}: {short} in a fight of {seconds:.0f} s, {long} in one of {long_seconds:.0f} s",
                        label=row["label"], short=cell("short"), long=cell("long"), seconds=w["seconds"],
                        long_seconds=w["longSeconds"]))


if __name__ == "__main__":
    main()
