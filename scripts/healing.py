# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Heal table: every heal and shield rank the character knows, with what one cast gives and costs.

Per rank: the hit as the tooltip shows it, the average with crits, mana, cast time, healing per second of
casting, healing per mana, how much of the heal comes from the healing bonus (the rank's effective
coefficient, with talents and the loss of an outgrown rank), how long the mana lasts casting nothing else, and
in which situations of the rotation guide the rank is the one to cast. Plus the regeneration (mana per 5 s
while casting and outside the five-second rule) and what a point of each stat adds to the healing.

  uv run scripts/healing.py --char Siegfried

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

METRIC = "hps"
LONG_FIGHT = 300.0  # the second fight the stat weights are given for
# What a point of each stat adds: the engine's delta key, the label, how much is added for the measurement
# (enough to move the result, small enough to stay a slope).
WEIGHTS = [("healing", mark("Healing bonus"), 10), ("int", mark("Intellect"), 10), ("spi", mark("Spirit"), 10),
           ("mp5", mark("Mana per 5 sec."), 5), ("crit", mark("Spell crit, per 1%"), 1)]


def supported(class_file: str | None) -> bool:
    """Whether the combat engine has a healing metric for the class."""
    return METRIC in (simengine.METRICS.get(class_file or "") or [])


def weights_of(short: dict, long: dict, seconds: float) -> dict:
    """Stat weights from the gains of two fights: healing per second a point adds, and how many points of
    healing bonus do the same."""
    rows = []
    for key, label, amount in WEIGHTS:
        row = {"key": key, "label": tr(label)}
        for name, gains in (("short", short), ("long", long)):
            per = (gains.get(key) or 0.0) / amount
            base = (gains.get("healing") or 0.0) / WEIGHTS[0][2]
            row[name] = round(per, 3)
            row[name + "Bonus"] = round(per / base, 2) if base > 0 else None
        rows.append(row)
    return {"seconds": seconds, "longSeconds": LONG_FIGHT, "rows": rows}


def compute(wow: str = writs.DEFAULT_WOW, char: str = "", fight_seconds: float = 60.0, target_delta: int = -1) -> dict:
    """The heal table of a character as plain data."""
    c, fight, start = rotations.character(wow, char, fight_seconds, target_delta)
    ch = c.get("character") or {}
    class_file = ch.get("classFile") or ""
    if not supported(class_file):
        raise LookupError(tr("The combat engine has no heals for this class ({cls}).", cls=class_file or "?"))
    label = rotations.labeler(c)
    evals = [(key, {key: amount}) for key, _, amount in WEIGHTS]

    eng = start(METRIC)
    table, situations = eng.heal_table()
    if not table.get("rows"):
        raise LookupError(tr("This character knows no heal the combat engine models."))
    baseline = dict(eng.baseline)
    short = eng.batch(evals)
    long_eng = start(METRIC, LONG_FIGHT)
    long = long_eng.batch(evals)
    long_value = (long_eng.baseline or {}).get("value") or 0.0

    # Where the guide casts each rank.
    guide = [rotations.situation_out(s, label, rotations.UNITS[METRIC]) for s in situations]
    uses: dict[int, list] = {}
    for s in guide:
        for st in s["steps"]:
            if st["kind"] != "mana" and st.get("spellId"):
                uses.setdefault(st["spellId"], []).append({"key": s["key"], "title": s["title"]})
    sustained = [{"spellId": r.get("spellId"), "label": label(r.get("spellId"), r.get("label"), r.get("rank")),
                  "value": round(r.get("value") or 0, 1), "sharePct": round(r.get("sharePct") or 0, 1)}
                 for r in baseline.get("breakdown") or [] if r.get("spellId") and (r.get("value") or 0) > 0]
    for r in sustained:
        uses.setdefault(r["spellId"], []).append({"key": "sustained", "title": tr("Sustained")})

    casting, idle = table.get("regenCastingPerSec") or 0.0, table.get("regenIdlePerSec") or 0.0
    mana_max = table.get("manaMax") or 0.0
    rows = []
    for r in table["rows"]:
        cast, mana, execute = r.get("perCast") or 0.0, r.get("mana") or 0.0, r.get("executeSec") or 0.0
        kind = r.get("kind") or "direct"
        # A HoT or a shield sits on a target for its time: that is its rate there, whatever the casting takes.
        on_target = r.get("onTargetSec") or 0.0
        # Casting nothing else: how long the mana lasts and what it heals until then.
        spam_oom = spam_total = None
        if kind in ("direct", "both") and execute > 0 and mana > 0:
            net = mana / execute - casting
            if net > 0:
                spam_oom = mana_max / net
                spam_total = spam_oom * cast / execute
        rows.append({
            "spellId": r["spellId"], "baseId": r.get("baseId"), "name": r.get("name") or "",
            "label": label(r["spellId"], r.get("name"), r.get("rank")), "rank": r.get("rank") or 0,
            "level": r.get("level") or 0, "kind": kind,
            "min": round(r.get("min") or 0), "max": round(r.get("max") or 0),
            "perTick": round(r.get("perTick") or 0, 1), "ticks": r.get("ticks") or 0,
            "durationSec": r.get("durationSec") or 0, "absorb": round(r.get("absorb") or 0),
            "critPct": round(r.get("critPct") or 0, 1), "perCast": round(cast, 1), "mana": round(mana),
            "castSec": round(r.get("castSec") or 0, 2), "executeSec": round(execute, 2),
            "hps": round(r.get("hps") or 0, 1), "hpsOnTarget": round(cast / on_target, 1) if on_target else None,
            "hpm": round(r.get("hpm") or 0, 2), "bonus": round(r.get("bonus") or 0, 1),
            "bonusPct": round((r.get("bonus") or 0) / cast * 100, 1) if cast else 0,
            "coefPct": round(r.get("coefPct") or 0, 1), "bonusLossPct": round(r.get("bonusLossPct") or 0),
            "castsPerBar": round(mana_max / mana, 1) if mana else None,
            "spamSeconds": round(spam_oom, 1) if spam_oom else None,
            "spamHealing": round(spam_total) if spam_total else None,
            "uses": uses.get(r["spellId"]) or [], "best": [],
        })
    # The marks: the most healing per mana of all, the most per second among the heals that can be cast back
    # to back (a HoT or a shield fits on a target once).
    best_hpm = max(rows, key=lambda r: r["hpm"])
    if best_hpm["hpm"] > 0:
        best_hpm["best"].append("hpm")
    chain_casts = [r for r in rows if r["kind"] in ("direct", "both")]
    if chain_casts:
        max(chain_casts, key=lambda r: r["hps"])["best"].append("hps")

    pct = table.get("castingRegenPct") or 0.0
    gear = table.get("gearMp5") or 0.0
    stats = {"healing": round(table.get("healing") or 0), "critPct": round(table.get("critPct") or 0, 2),
             "manaMax": round(mana_max), "spirit": round(table.get("spirit") or 0), "gearMp5": round(gear, 1),
             "castingPer5": round(casting * 5, 1), "idlePer5": round(idle * 5, 1),
             "spiritPer5": round(idle * 5 - gear, 1), "castingRegenPct": round(pct, 1)}
    notes = [tr("Mana per 5 sec. on your gear counts all the time ({gear:.0f} now). The regeneration from spirit "
                "({spirit:.0f} mana per 5 sec.) stops while you cast and for five seconds after a cast that "
                "costs mana.", gear=gear, spirit=stats["spiritPer5"])]
    if pct > 0:
        notes.append(tr("Your talents let {pct:.0f}% of it through while casting.", pct=pct))
    notes.append(tr("The numbers are those of your last snapshot, with the buffs you had then."))

    recommended = [{"key": s["key"], "title": s["title"], "about": s["about"], "value": s["value"],
                    "oomSeconds": s["oomSeconds"], "verdict": s["verdict"],
                    "steps": [{"label": st["label"], "rule": st["rule"], "kind": st["kind"]} for st in s["steps"]]}
                   for s in guide]
    return {"name": ch.get("name") or char, "level": ch.get("level") or 60, "class": ch.get("class") or class_file,
            "classFile": class_file, "spec": simengine.spec_of(c), "unit": rotations.UNITS[METRIC],
            "rows": rows, "stats": stats, "notes": notes, "recommended": recommended,
            "sustained": {"seconds": fight_seconds, "value": round(baseline.get("value") or 0, 1),
                          "longSeconds": LONG_FIGHT, "longValue": round(long_value, 1), "steps": sustained},
            "weights": weights_of(short, long, fight_seconds),
            "engine": {"version": eng.version, "spellbookSource": eng.spellbook_source},
            "fight": {"seconds": fight_seconds, "targetDelta": fight["targetDelta"]}}


def hit_text(r: dict) -> str:
    """What one cast does without a crit, as a tooltip says it."""
    parts = []
    if r["absorb"]:
        parts.append(tr("absorbs {n}", n=r["absorb"]))
    if r["max"]:
        parts.append(f"{r['min']}-{r['max']}" if r["min"] != r["max"] else str(r["min"]))
    if r["ticks"]:
        parts.append(tr("{ticks:.0f} x {tick:.0f} over {seconds:.0f} s", ticks=r["ticks"], tick=r["perTick"],
                        seconds=r["durationSec"]))
    return " + ".join(parts)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--wow", default=writs.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--fight-seconds", type=float, default=60, help="length of the fight for the sustained "
                    "healing and the stat weights")
    ap.add_argument("--target-delta", type=int, default=-1, choices=[-1, 0, 1, 2, 3],
                    help="enemy levels above you (default: 2 below level 60, else 3)")
    ap.add_argument("--json", action="store_true", help="print the result as JSON (for the app)")
    i18n.add_argument(ap)
    a = ap.parse_args()
    i18n.set_language(a.lang, a.wow)
    try:
        r = compute(a.wow, a.char, a.fight_seconds, a.target_delta)
    except (FileNotFoundError, LookupError) as e:
        sys.exit(str(e))
    sys.stdout.reconfigure(encoding="utf-8")
    if a.json:
        import json
        print(json.dumps(r, ensure_ascii=False))
        return

    s = r["stats"]
    print(tr("{name}, level {level} {cls} | combat engine {version}", name=r["name"], level=r["level"],
             cls=r["class"], version=r["engine"]["version"]))
    if r["engine"]["spellbookSource"] == "assumed":
        print(tr("No spellbook in the export: every trainer rank up to your level is assumed."))
    print(tr("Healing bonus {healing}, crit {crit:.1f}%, mana {mana}, spirit {spirit}", healing=s["healing"],
             crit=s["critPct"], mana=s["manaMax"], spirit=s["spirit"]))
    print(tr("Mana per 5 sec.: {casting:.1f} while casting, {idle:.1f} outside the five-second rule",
             casting=s["castingPer5"], idle=s["idlePer5"]))
    for n in r["notes"]:
        print("  " + n)

    head = [tr("Spell"), tr("One cast"), tr("Average"), tr("Mana"), tr("Cast"), "HPS", tr("Per mana"),
            tr("From bonus"), tr("Coefficient"), tr("Use it for")]
    lines = []
    for row in r["rows"]:
        hps = f"{row['hps']:.1f}" + ("*" if "hps" in row["best"] else "")
        if row["hpsOnTarget"]:
            hps = tr("{n:.1f} on the target", n=row["hpsOnTarget"])
        lines.append([row["label"], hit_text(row), f"{row['perCast']:.1f}", str(row["mana"]),
                      tr("{n:.1f} s", n=row["castSec"]) if row["castSec"] else tr("instant"), hps,
                      f"{row['hpm']:.2f}" + ("*" if "hpm" in row["best"] else ""),
                      f"{row['bonusPct']:.0f}%", f"{row['coefPct']:.0f}%" +
                      (" " + tr("(outgrown: {pct:.0f}% less healing bonus)", pct=row["bonusLossPct"])
                       if row["bonusLossPct"] else ""),
                      ", ".join(u["title"] for u in row["uses"]) or "-"])
    widths = [max(len(x[i]) for x in [head] + lines) for i in range(len(head))]
    print()
    for x in [head] + lines:
        print("  ".join(v.ljust(w) for v, w in zip(x, widths)).rstrip())
    print(tr("* the most healing per mana, and the most per second among the heals you can cast back to back"))

    print("\n" + tr("Which rank when"))
    for rec in r["recommended"]:
        lasts = tr("mana lasts") if rec["oomSeconds"] is None else tr("mana lasts {time}",
                                                                     time=rotations.clock(rec["oomSeconds"]))
        spells = "; ".join(f"{st['label']}: {st['rule']}" for st in rec["steps"])
        print(f"  {rec['title']} ({rec['value']:.1f} {r['unit']}, {lasts}): {spells}")
    sus = r["sustained"]
    print("  " + tr("Sustained over {seconds:.0f} s ({value:.1f} {unit}, what your gear is rated by): {spells}",
                    seconds=sus["seconds"], value=sus["value"], unit=r["unit"],
                    spells=", ".join(st["label"] for st in sus["steps"])))

    w = r["weights"]
    print("\n" + tr("What a point adds ({unit}; in brackets: the points of healing bonus that do the same)",
                    unit=r["unit"]))
    for row in w["rows"]:
        def cell(name: str) -> str:
            eq = row[name + "Bonus"]
            return f"{row[name]:.3f}" + (f" ({eq:.2f})" if eq is not None else "")
        print("  " + tr("{label}: {short} in a fight of {seconds:.0f} s, {long} in one of {long_seconds:.0f} s",
                        label=row["label"], short=cell("short"), long=cell("long"), seconds=w["seconds"],
                        long_seconds=w["longSeconds"]))


if __name__ == "__main__":
    main()
