# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Rotation guide: what to cast in which situation, from the combat engine (sim/).

A damage dealer gets the best rotation for several kinds of fights (an enemy that dies quickly, the normal
fight, a boss, a long fight without a break): each fight is played cast by cast with every rotation the
character could cast, and the best one is shown as a priority list with what each spell did, the spells it
leaves out with what casting them would cost, and the start of the fight as a timeline with mana and health.

A healer gets a ladder of demand (light damage, a tank, heavy damage, everything, a group): for each level the
spells that supply that much healing per second for the least mana, and how long the mana lasts.

  uv run scripts/rotations.py --char Malgruk

Reads the character from WoWMCP.lua; the spell numbers come from the game tables the engine reads.
"""

from __future__ import annotations

import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import i18n  # noqa: E402
import simengine  # noqa: E402
import upgrades  # noqa: E402  (load_char, equipped_stats, the Life Tap text)
import writs  # noqa: E402  (find_chars, DEFAULT_WOW)
from i18n import mark, tr  # noqa: E402

UNITS = {"dps": "DPS", "hps": "HPS"}
GUIDES = {
    "dps": (mark("Damage"),
            mark("Every fight is played cast by cast with each rotation you could cast; this is the best one.")),
    "hps": (mark("Healing"),
            mark("How to heal a given amount per second for the least mana. The levels are shares of the most "
                 "you can heal.")),
}
# Heals the healer's plan leaves out, said once per guide.
GUIDE_NOTES = {
    "hps": [mark("Heals that reach several targets at once (Prayer of Healing, Holy Nova) are not in the model "
                 "yet; the group level uses heals on one target at a time."),
            mark("The casts are spread evenly: after each one the five-second rule holds the spirit "
                 "regeneration back, the rest of the pause regenerates in full.")],
}
SAME_DEMAND = mark("One target needs {value:.0f} healing per second: {pct:.0f}% of the most you can heal.")
SITUATIONS = {
    "quick": (mark("Quick kill"), mark("An enemy that dies in {seconds:.0f} seconds.")),
    "standard": (mark("Normal fight"),
                 mark("{seconds:.0f} seconds against an enemy {delta} levels above you: the fight your gear is "
                      "rated for.")),
    "boss": (mark("Boss"), mark("{minutes:.0f} minutes against an enemy {delta} levels above you.")),
    "endurance": (mark("Endurance"), mark("{minutes:.0f} minutes of fighting without a break.")),
    "light": (mark("Light damage"), SAME_DEMAND),
    "steady": (mark("Tank healing"), SAME_DEMAND),
    "heavy": (mark("Heavy damage"), SAME_DEMAND),
    "burst": (mark("Everything you have"),
              mark("The most you can heal on one target: {value:.0f} healing per second, whatever it costs.")),
    "group": (mark("Group damage"),
              mark("{targets} targets need {value:.0f} healing per second together: {pct:.0f}% of the most you "
                   "can heal there.")),
}
# What a line of the priority list means, per kind of step.
RULES = {
    "dot": mark("keep it on the enemy"),
    "cooldown": mark("whenever it is ready"),
    "filler": mark("fills the rest of the time"),
    "recover": mark("when the health is too low to trade for mana and the mana is running out (first after {time})"),
    "lifetap": mark("as soon as the health bar is full and the mana has room for it, and when the mana is gone"),
    "mana": mark("when mana is missing and the health allows it"),
    "wand": mark("when the mana is gone (first after {time})"),
}
# Life Tap next to a spell that stands by to return health: no reason to wait for a full health bar.
TAP_WITH_STANDBY = mark("as soon as the health allows it and the mana has room for it, and when the mana is gone")
# A rotation without a filler spell: the wand takes its place.
WAND_FILLS = mark("fills the rest of the time and costs no mana")
SKIPPED = {
    "filler": mark("the weaker filler in this fight"),
    "dot": mark("costs more casting time than it returns in this fight"),
    "cooldown": mark("costs more casting time than it returns in this fight"),
}


def clock(seconds: float) -> str:
    """A time in a fight: "45 s" or "2:24 min"."""
    if seconds < 60:
        return tr("{n:.0f} s", n=seconds)
    return tr("{m}:{s:02d} min", m=int(seconds // 60), s=int(seconds % 60))


def labeler(c: dict):
    """Spell label with the game's own rank line from the spellbook export (localized, "Rang 3"), else the
    position in the rank chain."""
    lines = {int(k): ((v or {}).get("rank") or "") for k, v in ((c.get("spellbook") or {}).get("spells") or {}).items()}

    def label(spell_id, name, rank) -> str:
        line = lines.get(spell_id) or (tr("Rank {n}", n=rank) if rank and rank > 1 else "")
        return f"{name} ({line})" if line else (name or "?")

    return label


def heal_rule(step: dict, steps: list, demand: dict) -> str:
    """What a line of a healer's plan says to do."""
    kind, targets = step.get("kind"), demand.get("targets") or 1
    if kind == "mana":
        if step.get("casts"):
            return tr("{n:.0f}x before the mana is gone: {mana:.0f} mana for {health:.0f} health, which you "
                      "heal back", n=step["casts"], mana=step.get("manaGain") or 0, health=step.get("healthCost") or 0)
        return tr("every {minutes:.0f} minutes: {mana:.0f} mana for {health:.0f} health, which you heal back; "
                  "with it the mana lasts", minutes=1 / max(step.get("perMin") or 0, 1e-9),
                  mana=step.get("manaGain") or 0, health=step.get("healthCost") or 0)
    if kind in ("hot", "cooldown"):
        kept = step.get("targets") or 0
        if targets > 1:
            return tr("keep it on {n:.0f} of the {targets} targets", n=kept, targets=targets)
        if kept >= 0.95:
            return tr("keep it on the target")
        return tr("on the target {pct:.0f}% of the time", pct=kept * 100)
    # Of several heals the one with the least mana per point is the default; the others fill in when it is
    # too slow for the damage.
    heals = [s for s in steps if s.get("kind") == "heal" and (s.get("value") or 0) > 0]
    cheapest = min(heals, key=lambda s: (s.get("manaPerSec") or 0) / s["value"], default=step)
    if len(heals) > 1 and step is not cheapest:
        return tr("in between, when the default heal is too slow")
    if (demand.get("castPct") or 0) < 99.5:
        return tr("the default heal, with pauses in between")
    return tr("the default heal")


def damage_verdict(s: dict, steps: list) -> str:
    """How the mana holds in a played fight: the phases a player notices."""
    by_kind = {st["kind"]: st for st in steps}
    trade = by_kind.get("lifetap") or by_kind.get("mana")
    recover = by_kind.get("recover")
    wand = by_kind.get("wand") if (by_kind.get("wand") or {}).get("sharePct", 0) >= 1 else None
    parts = []
    if "filler" not in by_kind and by_kind.get("wand"):
        # The wand by choice, not for lack of mana.
        return " ".join([tr("No filler spell: in this fight the mana is worth more on the spells above, and the "
                            "pauses on the wand regenerate it."),
                         tr("At the end {mana:.0f}% mana and {health:.0f}% health are left.",
                            mana=s.get("endManaPct") or 0, health=s.get("endHpPct") or 0)])
    if recover and trade:
        parts.append(tr("From {time} on the health is too low for {trade}: {recover} brings it back.",
                        time=clock(recover["firstSec"]), trade=trade["label"], recover=recover["label"]))
    if wand:
        parts.append(tr("From {time} on the mana is gone and the wand fills the gaps.", time=clock(wand["firstSec"])))
    if not parts:
        parts.append(tr("The mana lasts with {trade}.", trade=trade["label"]) if trade
                     else tr("The mana lasts for the whole fight."))
    parts.append(tr("At the end {mana:.0f}% mana and {health:.0f}% health are left.",
                    mana=s.get("endManaPct") or 0, health=s.get("endHpPct") or 0))
    return " ".join(parts)


def situation_out(s: dict, label, unit: str) -> dict:
    fight, demand = s.get("fight") or {}, s.get("demand")
    title, about = SITUATIONS.get(s["key"], (s["key"], ""))
    seconds = fight.get("seconds") or 0
    about = tr(about, seconds=seconds, minutes=seconds / 60, delta=fight.get("targetDelta") or 0,
               value=(demand or {}).get("value") or 0, pct=(demand or {}).get("pct") or 0,
               targets=(demand or {}).get("targets") or 1) if about else ""
    names = {("wand", 0): tr("Wand")}
    steps = []
    raw_steps = s.get("steps") or []
    has_filler = any(st.get("kind") == "filler" for st in raw_steps)
    has_standby = any(st.get("kind") == "recover" for st in raw_steps)
    for st in raw_steps:
        kind = st.get("kind")
        text = tr("Wand") if kind == "wand" else label(st.get("spellId"), st.get("name"), st.get("rank"))
        # The played fight names Life Tap and the wand by their kind, every other action by its spell.
        names[(kind, 0) if kind in ("wand", "lifetap") else ("", st.get("spellId") or 0)] = text
        if demand:
            rule = heal_rule(st, raw_steps, demand)
        elif kind == "wand" and not has_filler:
            rule = tr(WAND_FILLS)
        elif kind == "lifetap" and has_standby:
            rule = tr(TAP_WITH_STANDBY)
        else:
            rule = tr(RULES[kind], time=clock(st.get("firstSec") or 0)) if kind in RULES else ""
        steps.append({"kind": kind, "spellId": st.get("spellId"), "label": text, "rule": rule,
                      "casts": st.get("casts") or 0, "perMin": round(st.get("perMin") or 0, 2),
                      "firstSec": st.get("firstSec") or 0, "sharePct": round(st.get("sharePct") or 0, 1),
                      "value": round(st.get("value") or 0, 1), "perCast": round(st.get("perCast") or 0, 1),
                      "manaPerSec": round(st.get("manaPerSec") or 0, 2), "targets": round(st.get("targets") or 0, 2)})
    skipped = [{"kind": k.get("kind"), "spellId": k.get("spellId"),
                "label": label(k.get("spellId"), k.get("name"), k.get("rank")), "loss": round(k.get("loss") or 0, 1),
                "why": tr(SKIPPED[k["kind"]]) if k.get("kind") in SKIPPED else ""} for k in s.get("skipped") or []]
    timeline = []
    for e in s.get("timeline") or []:
        key = (e["kind"], 0) if e["kind"] in ("wand", "lifetap") else ("", e.get("spellId") or 0)
        timeline.append({"t": round(e.get("t") or 0, 1), "kind": e["kind"], "label": names.get(key) or e["kind"],
                         "n": e.get("n") or 1, "manaPct": round(e.get("manaPct") or 0),
                         "hpPct": round(e.get("hpPct") or 0)})

    oom = s.get("oomSeconds")
    if demand:
        balance = dict(spent=demand.get("manaPerSec") or 0, regen=demand.get("regenPerSec") or 0)
        if oom is None:
            verdict = tr("The mana lasts: {spent:.1f} mana per second against {regen:.1f} regenerated.", **balance)
        else:
            verdict = tr("The mana lasts {time}: {spent:.1f} mana per second against {regen:.1f} regenerated.",
                         time=clock(oom), **balance)
        if (demand.get("castPct") or 0) < 99.5:
            verdict += " " + tr("You cast {cast:.0f}% of the time.", cast=demand.get("castPct") or 0)
        if (demand.get("freePct") or 0) >= 0.5:
            verdict += " " + tr("{free:.0f}% of the time is outside the five-second rule and regenerates in full.",
                                free=demand["freePct"])
    else:
        verdict = damage_verdict(s, steps)
    return {"key": s["key"], "title": tr(title), "about": about, "value": round(s.get("value") or 0, 1), "unit": unit,
            "verdict": verdict, "oomSeconds": oom, "steps": steps, "skipped": skipped, "timeline": timeline,
            "more": s.get("more") or 0, "candidates": s.get("candidates") or 0, "fight": fight, "demand": demand,
            "endManaPct": round(s.get("endManaPct") or 0), "endHpPct": round(s.get("endHpPct") or 0)}


def character(wow: str, char: str, fight_seconds: float = 60.0, target_delta: int = -1,
              kills_per_minute: float = -1):
    """The character's snapshot, the fight as asked for and a function that starts the engine for a metric.
    Shared with healing.py."""
    files = writs.find_chars(wow, char)
    if not files:
        raise FileNotFoundError(tr("No WoWMCP.lua found for this character."))
    c = upgrades.load_char(files[0])
    ch = c.get("character") or {}
    class_file = ch.get("classFile") or ""
    if not simengine.supported(class_file):
        raise LookupError(tr("The combat engine does not model this class yet ({cls}).", cls=class_file or "?"))
    st = ch.get("stats") or {}
    _, totals = upgrades.equipped_stats(c)
    life_tap = {"healthPerCast": 0.0, "manaPerCast": 0.0}
    m = upgrades.LIFE_TAP_TEXT.search((st.get("lifeTap") or {}).get("text") or "")
    if m and upgrades.number(m.group(1)) > 0:
        life_tap = {"healthPerCast": upgrades.number(m.group(1)), "manaPerCast": upgrades.number(m.group(2))}
    profile = "leveling" if (ch.get("level") or 60) < 60 else "dungeon"
    fight = {"seconds": fight_seconds,
             "targetDelta": target_delta if 0 <= target_delta <= 3 else upgrades.PROFILES[profile]["target"],
             "killsPerMinute": kills_per_minute if kills_per_minute >= 0 else -1}

    def start(metric: str, seconds: float | None = None) -> simengine.Engine:
        return simengine.start(c, totals.get("mp5", 0), totals.get("hp5", 0),
                               dict(fight, seconds=seconds) if seconds else fight, metric, life_tap)

    return c, fight, start


def compute(wow: str = writs.DEFAULT_WOW, char: str = "", fight_seconds: float = 60.0, target_delta: int = -1,
            kills_per_minute: float = -1) -> dict:
    """The rotation guide of a character as plain data: one guide per metric the class has (damage, healing),
    each with its situations."""
    c, fight, start = character(wow, char, fight_seconds, target_delta, kills_per_minute)
    ch = c.get("character") or {}
    class_file = ch.get("classFile") or ""
    level = ch.get("level") or 60

    # The spec's own metric first; the time-to-OOM metric has no guide of its own.
    first = simengine.default_metric(class_file, simengine.spec_of(c))
    metrics = sorted((x for x in simengine.METRICS.get(class_file) or [] if x in GUIDES), key=lambda x: x != first)
    label = labeler(c)
    guides, version, source = [], "", ""
    for metric in metrics:
        eng = start(metric)
        situations = eng.situations()
        version, source = eng.version, eng.spellbook_source
        if not situations:
            continue
        title, about = GUIDES[metric]
        guides.append({"metric": metric, "unit": UNITS[metric], "title": tr(title), "about": tr(about),
                       "notes": [tr(n) for n in GUIDE_NOTES.get(metric, [])],
                       "situations": [situation_out(s, label, UNITS[metric]) for s in situations]})
    if not guides:
        raise LookupError(tr("The combat engine has no rotation guide for this character."))
    return {"name": ch.get("name") or char, "level": level, "class": ch.get("class") or class_file,
            "classFile": class_file, "spec": simengine.spec_of(c), "guides": guides,
            "engine": {"version": version, "spellbookSource": source},
            "fight": {"seconds": fight_seconds, "targetDelta": fight["targetDelta"]}}


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--wow", default=writs.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--fight-seconds", type=float, default=60, help="length of the normal fight")
    ap.add_argument("--target-delta", type=int, default=-1, choices=[-1, 0, 1, 2, 3],
                    help="enemy levels above you in the normal fight (default: 2 below level 60, else 3)")
    ap.add_argument("--kills-per-minute", type=float, default=-1,
                    help="your killing blows per minute, for buffs like Spirit Tap (default: one per fight)")
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

    print(tr("{name}, level {level} {cls} | combat engine {version}", name=r["name"], level=r["level"],
             cls=r["class"], version=r["engine"]["version"]))
    if r["engine"]["spellbookSource"] == "assumed":
        print(tr("No spellbook in the export: every trainer rank up to your level is assumed."))
    for g in r["guides"]:
        print(f"\n=== {g['title']} ===\n{g['about']}")
        for s in g["situations"]:
            print(f"\n{s['title']}: {s['value']:.1f} {s['unit']}  ({s['about']})")
            print("  " + s["verdict"])
            for i, st in enumerate(s["steps"], 1):
                numbers = tr("{n:.1f}/min, {share:.0f}% of the time", n=st["perMin"], share=st["sharePct"])
                if st["value"]:
                    numbers += f", {st['value']:.1f} {s['unit']}"
                print(f"  {i}. {st['label']}: {st['rule']}  [{numbers}]")
            for k in s["skipped"]:
                print("  " + tr("Left out: {label} ({loss:.1f} {unit} less; {why})", label=k["label"], loss=k["loss"],
                                unit=s["unit"], why=k["why"]))
            if s["timeline"]:
                parts = [f"{clock(e['t'])} {e['label']}" + (f" x{e['n']}" if e["n"] > 1 else "") +
                         f" [{e['manaPct']}/{e['hpPct']}]" for e in s["timeline"]]
                print("  " + tr("Start (mana/health in % before each action):") + " " + " > ".join(parts) +
                      (" " + tr("... and {n} more actions", n=s["more"]) if s["more"] else ""))
        for n in g["notes"]:
            print("\n" + n)


if __name__ == "__main__":
    main()
