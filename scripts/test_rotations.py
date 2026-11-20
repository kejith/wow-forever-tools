# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Checks for the rotation guide (rotations.py) without game, network and engine:  uv run scripts/test_rotations.py

The combat engine is a stub that answers with hand-made situations; what is checked is what rotations.py makes
of them: the order of the guides, the sentences per step, the verdicts, the timeline labels, both languages.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import i18n  # noqa: E402
import rotations  # noqa: E402

STUB_ENGINE = '''
import json, sys
data = json.load(sys.stdin)
assert data["situations"] is True and data["evaluations"] == [], "the guide asks for situations, not for gear"
assert data["fight"] == {"seconds": 90.0, "targetDelta": 2, "killsPerMinute": -1}
assert data["character"]["classFile"] == "PRIEST" and data["character"]["spellbook"] == [139, 589, 2053]
fight = {"seconds": 90.0, "targetDelta": 2, "killsPerMinute": 0}
if data["metric"] == "hps":
    def demand(pct, targets=1, cast=100.0, free=0.0):
        return {"targets": targets, "pct": pct, "value": 2.0 * pct, "maxValue": 200.0, "manaPerSec": 12.5,
                "regenPerSec": 10.0, "castPct": cast, "freePct": free, "manaMax": 1500}
    situations = [
        {"key": "light", "fight": fight, "metric": "hps", "value": 50.0, "oomSeconds": None, "candidates": 7,
         "demand": demand(25, cast=40.0, free=20.0),
         "steps": [{"kind": "hot", "spellId": 139, "name": "Renew", "rank": 3, "perMin": 2.0, "sharePct": 5.0,
                    "value": 8.0, "perCast": 240.0, "manaPerSec": 3.5, "targets": 0.5},
                   {"kind": "heal", "spellId": 2053, "name": "Lesser Heal", "rank": 3, "perMin": 10.0,
                    "sharePct": 35.0, "value": 42.0, "perCast": 250.0, "manaPerSec": 9.0},
                   {"kind": "mana", "spellId": 1277324, "name": "Dark Sacrifice", "rank": 1, "casts": 0,
                    "perMin": 0.05, "manaGain": 552.0, "healthCost": 435.0}]},
        {"key": "heavy", "fight": fight, "metric": "hps", "value": 150.0, "oomSeconds": 62.4, "candidates": 7,
         "demand": demand(75),
         "steps": [{"kind": "cooldown", "spellId": 17, "name": "Shield", "rank": 4, "perMin": 4.0, "sharePct": 10.0,
                    "value": 20.0, "perCast": 290.0, "manaPerSec": 10.0, "targets": 1.0},
                   {"kind": "heal", "spellId": 2053, "name": "Lesser Heal", "rank": 3, "perMin": 12.0,
                    "sharePct": 50.0, "value": 50.0, "perCast": 250.0, "manaPerSec": 15.0},
                   {"kind": "heal", "spellId": 2055, "name": "Heal", "rank": 2, "perMin": 8.0, "sharePct": 40.0,
                    "value": 80.0, "perCast": 500.0, "manaPerSec": 30.0},
                   {"kind": "mana", "spellId": 1277324, "name": "Dark Sacrifice", "rank": 1, "casts": 1,
                    "perMin": 1.0, "manaGain": 552.0, "healthCost": 435.0}]},
        {"key": "group", "fight": fight, "metric": "hps", "value": 100.0, "oomSeconds": 120.0, "candidates": 7,
         "demand": demand(50, targets=5, cast=80.0),
         "steps": [{"kind": "hot", "spellId": 139, "name": "Renew", "rank": 3, "perMin": 12.0, "sharePct": 30.0,
                    "value": 48.0, "perCast": 240.0, "manaPerSec": 21.0, "targets": 3.0}]},
    ]
else:
    situations = [
        {"key": "standard", "fight": dict(fight, killsPerMinute=1), "metric": "dps", "value": 57.25,
         "oomSeconds": None, "candidates": 24, "endManaPct": 14.4, "endHpPct": 100.0, "more": 3,
         "steps": [{"kind": "dot", "spellId": 589, "name": "Shadow Word: Pain", "rank": 3, "casts": 5,
                    "perMin": 3.33, "firstSec": 0, "sharePct": 8.3, "value": 9.0},
                   {"kind": "filler", "spellId": 585, "name": "Smite", "rank": 4, "casts": 20, "perMin": 13.3,
                    "firstSec": 1.5, "sharePct": 80.0, "value": 40.0},
                   {"kind": "lifetap", "spellId": 1454, "name": "Life Tap", "rank": 1, "casts": 4, "perMin": 2.7,
                    "firstSec": 4.5, "sharePct": 6.7, "value": 0}],
         "skipped": [{"kind": "dot", "spellId": 14914, "name": "Holy Fire", "rank": 2, "loss": 1.26},
                     {"kind": "filler", "spellId": 15407, "name": "Mind Flay", "rank": 1, "loss": 7.0}],
         "timeline": [{"t": 0, "kind": "dot", "spellId": 589, "n": 1, "manaPct": 100, "hpPct": 100},
                      {"t": 1.5, "kind": "filler", "spellId": 585, "n": 3, "manaPct": 96.4, "hpPct": 100},
                      {"t": 9, "kind": "lifetap", "n": 1, "manaPct": 70, "hpPct": 99.6},
                      {"t": 151.5, "kind": "wand", "n": 2, "manaPct": 1, "hpPct": 60}]},
        {"key": "boss", "fight": {"seconds": 180.0, "targetDelta": 3, "killsPerMinute": 0}, "metric": "dps",
         "value": 40.0, "oomSeconds": 54.0, "candidates": 24, "endManaPct": 5.0, "endHpPct": 80.0,
         "steps": [{"kind": "filler", "spellId": 585, "name": "Smite", "rank": 4, "casts": 20, "perMin": 6.7,
                    "firstSec": 0, "sharePct": 30.0, "value": 20.0},
                   {"kind": "lifetap", "spellId": 1454, "name": "Life Tap", "rank": 1, "casts": 9, "perMin": 3.0,
                    "firstSec": 4.5, "sharePct": 7.5, "value": 0},
                   {"kind": "recover", "spellId": 689, "name": "Drain Life", "rank": 2, "casts": 4, "perMin": 1.3,
                    "firstSec": 142.5, "sharePct": 11.0, "value": 5.0},
                   {"kind": "wand", "casts": 60, "perMin": 20.0, "firstSec": 54.0, "sharePct": 50.0, "value": 15.0}]},
        {"key": "endurance", "fight": {"seconds": 600.0, "targetDelta": 2, "killsPerMinute": 0.1}, "metric": "dps",
         "value": 30.0, "oomSeconds": 4.5, "candidates": 24, "endManaPct": 40.0, "endHpPct": 100.0,
         "steps": [{"kind": "dot", "spellId": 589, "name": "Shadow Word: Pain", "rank": 3, "casts": 30,
                    "perMin": 3.0, "firstSec": 0, "sharePct": 7.5, "value": 9.0},
                   {"kind": "wand", "casts": 300, "perMin": 30.0, "firstSec": 1.5, "sharePct": 92.5, "value": 21.0}]},
    ]
json.dump({"engine": "stub", "spellbookSource": "export", "fight": fight,
           "baseline": {"metric": data["metric"], "value": 1.0}, "situations": situations}, sys.stdout)
'''


def write_fixture(tmp: str, class_file: str = "PRIEST") -> None:
    char_dir = os.path.join(tmp, "WTF", "Account", "TEST", "93", "Tester - Realm", "SavedVariables")
    os.makedirs(char_dir)
    with open(os.path.join(char_dir, "WoWMCP.lua"), "w", encoding="utf-8") as f:
        f.write('WoWMCP_Char = { character = { name = "Tester", class = "Priester", classFile = "%s", level = 27, '
                'stats = { healing = 100, powerMax = 1500, healthMax = 700, spirit = 100 } }, '
                'spellbook = { updated = 1, count = 3, spells = { '
                '[139] = { name = "Erneuerung", rank = "Rang 3" }, [2053] = { name = "Geringes Heilen", rank = "Rang 3" }, '
                '[589] = { name = "Schattenwort: Schmerz" } } }, '
                'talents = { class = {} }, client = { version = "9.9.9", build = 9, locale = "deDE" }, '
                'equipment = {} }\n' % class_file)


def run(tmp: str, **kw) -> dict:
    """compute() against the stub engine; the table download is stubbed out."""
    import icons
    stub = os.path.join(tmp, "engine_stub.py")
    with open(stub, "w", encoding="utf-8") as f:
        f.write(STUB_ENGINE)
    real_table, real_env = icons.table, os.environ.get("SIMFOREVER_BIN")
    icons.table = lambda *a, **k: []
    os.environ["SIMFOREVER_BIN"] = stub
    try:
        return rotations.compute(tmp, "Tester", fight_seconds=90, **kw)
    finally:
        icons.table = real_table
        os.environ.pop("SIMFOREVER_BIN", None)
        if real_env:
            os.environ["SIMFOREVER_BIN"] = real_env


def by_key(guide: dict) -> dict:
    return {s["key"]: s for s in guide["situations"]}


def test_guides(tmp: str):
    """A priest without talents heals: the healing guide comes first, the damage guide second; the
    time-to-OOM metric has none."""
    write_fixture(tmp)
    r = run(tmp)
    assert [g["metric"] for g in r["guides"]] == ["hps", "dps"] and [g["unit"] for g in r["guides"]] == ["HPS", "DPS"]
    assert r["name"] == "Tester" and r["level"] == 27 and r["classFile"] == "PRIEST"
    assert r["engine"] == {"version": "stub", "spellbookSource": "export"}
    assert r["fight"] == {"seconds": 90, "targetDelta": 2}, "below level 60 the enemies are two levels above"
    heal, damage = r["guides"]
    assert heal["title"] == "Healing" and len(heal["notes"]) == 2 and damage["notes"] == []
    assert [s["key"] for s in heal["situations"]] == ["light", "heavy", "group"]


def test_healer_plan(tmp: str):
    write_fixture(tmp)
    heal = by_key(run(tmp)["guides"][0])

    light = heal["light"]
    assert light["title"] == "Light damage" and light["unit"] == "HPS" and light["value"] == 50
    assert light["about"] == "One target needs 50 healing per second: 25% of the most you can heal."
    # The engine's name with the game's rank line from the spellbook export, in the client's language.
    assert [s["label"] for s in light["steps"]] == ["Renew (Rang 3)", "Lesser Heal (Rang 3)", "Dark Sacrifice"]
    renew, lesser, sacrifice = light["steps"]
    assert renew["rule"] == "on the target 50% of the time" and renew["perCast"] == 240
    assert lesser["rule"] == "the default heal, with pauses in between"
    assert sacrifice["rule"] == ("every 20 minutes: 552 mana for 435 health, which you heal back; "
                                 "with it the mana lasts")
    assert light["verdict"] == ("The mana lasts: 12.5 mana per second against 10.0 regenerated. You cast 40% of the "
                                "time. 20% of the time is outside the five-second rule and regenerates in full.")
    assert light["oomSeconds"] is None and light["timeline"] == [] and light["skipped"] == []

    heavy = heal["heavy"]
    shield, lesser, big, sacrifice = heavy["steps"]
    assert shield["rule"] == "keep it on the target"
    # Lesser Heal costs 0.3 mana per point, Heal 0.375: the cheaper one is the default.
    assert lesser["rule"] == "the default heal" and big["rule"] == "in between, when the default heal is too slow"
    assert big["label"] == "Heal (Rank 2)", "no rank line in the export: the position in the chain"
    assert sacrifice["rule"] == "1x before the mana is gone: 552 mana for 435 health, which you heal back"
    assert heavy["verdict"] == "The mana lasts 1:02 min: 12.5 mana per second against 10.0 regenerated."

    group = heal["group"]
    assert group["about"] == ("5 targets need 100 healing per second together: 50% of the most you can heal there.")
    assert group["steps"][0]["rule"] == "keep it on 3 of the 5 targets"
    assert group["verdict"].endswith("You cast 80% of the time.")


def test_damage_fights(tmp: str):
    write_fixture(tmp)
    damage = by_key(run(tmp)["guides"][1])

    std = damage["standard"]
    assert std["title"] == "Normal fight" and std["value"] in (57.2, 57.3)
    assert std["about"] == "90 seconds against an enemy 2 levels above you: the fight your gear is rated for."
    assert [s["rule"] for s in std["steps"]] == [
        "keep it on the enemy", "fills the rest of the time",
        "as soon as the health bar is full and the mana has room for it, and when the mana is gone"]
    assert std["verdict"] == "The mana lasts with Life Tap. At the end 14% mana and 100% health are left."
    assert [(k["label"], k["loss"], k["why"]) for k in std["skipped"]] == [
        ("Holy Fire (Rank 2)", 1.3, "costs more casting time than it returns in this fight"),
        ("Mind Flay", 7.0, "the weaker filler in this fight")]
    # The played fight names Life Tap and the wand by their kind; the wand has a name without a step too.
    assert [(e["label"], e["n"], e["manaPct"], e["hpPct"]) for e in std["timeline"]] == [
        ("Shadow Word: Pain (Rank 3)", 1, 100, 100), ("Smite (Rank 4)", 3, 96, 100), ("Life Tap", 1, 70, 100),
        ("Wand", 2, 1, 60)]
    assert std["more"] == 3 and std["candidates"] == 24

    # Health too low for Life Tap from 2:22 on, the wand from 0:54 on.
    boss = damage["boss"]
    assert boss["about"] == "3 minutes against an enemy 3 levels above you."
    assert boss["verdict"] == ("From 2:22 min on the health is too low for Life Tap: Drain Life (Rank 2) brings it "
                               "back. From 54 s on the mana is gone and the wand fills the gaps. "
                               "At the end 5% mana and 80% health are left.")
    rules = {s["kind"]: s["rule"] for s in boss["steps"]}
    assert rules["recover"] == ("when the health is too low to trade for mana and the mana is running out "
                                "(first after 2:22 min)")
    assert rules["wand"] == "when the mana is gone (first after 54 s)"
    assert boss["steps"][-1]["label"] == "Wand"

    # No filler: the wand is the choice, not the emergency.
    long = damage["endurance"]
    assert long["steps"][-1]["rule"] == "fills the rest of the time and costs no mana"
    assert long["verdict"].startswith("No filler spell:") and "40% mana" in long["verdict"]


def test_german(tmp: str):
    write_fixture(tmp)
    i18n.set_language("de")
    try:
        r = run(tmp)
    finally:
        i18n.set_language("en")
    heal, damage = r["guides"]
    assert heal["title"] == "Heilung" and damage["title"] == "Schaden"
    light = by_key(heal)["light"]
    assert light["title"] == "Leichter Schaden" and light["steps"][0]["rule"] == "50 % der Zeit auf dem Ziel"
    boss = by_key(damage)["boss"]
    assert boss["title"] == "Boss" and boss["steps"][-1]["label"] == "Zauberstab"
    assert "Ab 2:22 min ist das Leben zu niedrig für Life Tap" in boss["verdict"]
    assert by_key(damage)["standard"]["skipped"][1]["why"] == "in diesem Kampf der schwächere Füller"


def test_unsupported_class(tmp: str):
    write_fixture(tmp, "WARRIOR")
    try:
        run(tmp)
    except LookupError as e:
        assert "WARRIOR" in str(e)
    else:
        raise AssertionError("a class the engine does not model must be refused")
    try:
        rotations.compute(tmp, "Nobody")
    except FileNotFoundError:
        pass
    else:
        raise AssertionError("an unknown character must be reported")


def test_clock():
    assert rotations.clock(0) == "0 s" and rotations.clock(59.4) == "59 s"
    assert rotations.clock(60) == "1:00 min" and rotations.clock(142.5) == "2:22 min"


if __name__ == "__main__":
    import tempfile

    i18n.set_language("en")

    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            if fn.__code__.co_argcount:
                with tempfile.TemporaryDirectory() as d:
                    fn(d)
            else:
                fn()
            print("ok", name)
