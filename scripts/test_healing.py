# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Checks for the heal table (healing.py) without game, network and engine:  uv run scripts/test_healing.py

The combat engine is a stub that answers with a hand-made table, two situations and fixed gains; what is
checked is what healing.py makes of them: the labels, the derived columns (rate on the target, share of the
bonus, casting nothing else), the marks, where each rank is used, the regeneration and the stat weights.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import healing  # noqa: E402
import i18n  # noqa: E402

STUB_ENGINE = '''
import json, sys
data = json.load(sys.stdin)
assert data["metric"] == "hps" and data["character"]["classFile"] == "PRIEST"
seconds = data["fight"]["seconds"]
assert seconds in (90.0, 300.0) and data["fight"]["targetDelta"] == 2
out = {"engine": "stub", "spellbookSource": "export", "fight": data["fight"],
       "baseline": {"metric": "hps", "value": 120.0 if seconds == 90.0 else 80.0, "breakdown": [
           {"spellId": 139, "label": "Renew", "rank": 1, "kind": "hot", "value": 10.0, "sharePct": 10.0},
           {"spellId": 2055, "label": "Heal", "rank": 2, "kind": "heal", "value": 110.0, "sharePct": 70.0},
           {"spellId": 0, "label": "Wand", "kind": "wand", "value": 0, "sharePct": 20.0}]}}
if data.get("healTable"):
    assert data["situations"] is True and data["evaluations"] == [] and seconds == 90.0
    out["healTable"] = {
        "healing": 100, "critPct": 5.2, "manaMax": 1500, "spirit": 100, "gearMp5": 10,
        "regenCastingPerSec": 5.0, "regenIdlePerSec": 8.0, "castingRegenPct": 50,
        "rows": [
            {"spellId": 2053, "baseId": 2050, "name": "Lesser Heal", "rank": 3, "level": 10, "kind": "direct",
             "min": 240, "max": 260, "critPct": 5.2, "perCast": 250.0, "mana": 75, "castSec": 2.5,
             "executeSec": 2.5, "hps": 100.0, "hpm": 3.3333, "bonus": 50.0, "coefPct": 50.0},
            {"spellId": 139, "baseId": 139, "name": "Renew", "rank": 1, "level": 8, "kind": "hot",
             "perTick": 30, "ticks": 5, "durationSec": 15, "onTargetSec": 15, "perCast": 150.0, "mana": 30, "castSec": 0,
             "executeSec": 1.5, "hps": 100.0, "hpm": 5.0, "bonus": 90.0, "coefPct": 90.0, "bonusLossPct": 10},
            {"spellId": 17, "baseId": 17, "name": "Shield", "rank": 1, "level": 6, "kind": "shield",
             "absorb": 60, "perCast": 60.0, "mana": 40, "castSec": 0, "executeSec": 1.5, "onTargetSec": 15,
             "hps": 40.0, "hpm": 1.5, "bonus": 10.0, "coefPct": 10.0},
            {"spellId": 2055, "baseId": 2054, "name": "Heal", "rank": 2, "level": 22, "kind": "direct",
             "min": 470, "max": 530, "critPct": 5.2, "perCast": 510.0, "mana": 12, "castSec": 3.0,
             "executeSec": 3.0, "hps": 170.0, "hpm": 2.5, "bonus": 88.0, "coefPct": 88.0}]}
    demand = {"targets": 1, "pct": 25, "value": 50.0, "maxValue": 200.0, "manaPerSec": 12.5, "regenPerSec": 10.0,
              "castPct": 40.0, "freePct": 20.0, "manaMax": 1500}
    out["situations"] = [
        {"key": "light", "fight": data["fight"], "metric": "hps", "value": 50.0, "oomSeconds": None,
         "demand": demand,
         "steps": [{"kind": "hot", "spellId": 139, "name": "Renew", "rank": 1, "perMin": 4.0, "sharePct": 10.0,
                    "value": 10.0, "manaPerSec": 2.0, "targets": 1.0},
                   {"kind": "heal", "spellId": 2053, "name": "Lesser Heal", "rank": 3, "perMin": 10.0,
                    "sharePct": 30.0, "value": 40.0, "manaPerSec": 10.5},
                   {"kind": "mana", "spellId": 1277324, "name": "Dark Sacrifice", "rank": 1, "casts": 0,
                    "perMin": 0.05, "manaGain": 552.0, "healthCost": 435.0}]},
        {"key": "burst", "fight": data["fight"], "metric": "hps", "value": 200.0, "oomSeconds": 62.4,
         "demand": dict(demand, pct=100, value=200.0, castPct=100.0, freePct=0.0),
         "steps": [{"kind": "heal", "spellId": 2055, "name": "Heal", "rank": 2, "perMin": 20.0, "sharePct": 100.0,
                    "value": 200.0, "manaPerSec": 60.0}]}]
else:
    keys = [e["key"] for e in data["evaluations"]]
    assert keys == ["healing", "int", "spi", "mp5", "crit"], keys
    assert data["evaluations"][0]["delta"] == {"healing": 10} and data["evaluations"][3]["delta"] == {"mp5": 5}
    gains = {"healing": 2.0, "int": 6.0, "spi": 1.0, "mp5": 2.0, "crit": 0.5}
    if seconds == 300.0:
        gains = {"healing": 1.0, "int": 0.5, "spi": 2.0, "mp5": 2.5, "crit": 0.0}
    out["results"] = {k: {"gain": v} for k, v in gains.items()}
json.dump(out, sys.stdout)
'''


def write_fixture(tmp: str, class_file: str = "PRIEST") -> None:
    char_dir = os.path.join(tmp, "WTF", "Account", "TEST", "93", "Tester - Realm", "SavedVariables")
    os.makedirs(char_dir)
    with open(os.path.join(char_dir, "WoWMCP.lua"), "w", encoding="utf-8") as f:
        f.write('WoWMCP_Char = { character = { name = "Tester", class = "Priester", classFile = "%s", level = 27, '
                'stats = { healing = 100, powerMax = 1500, healthMax = 700, spirit = 100 } }, '
                'spellbook = { updated = 1, count = 3, spells = { '
                '[139] = { name = "Erneuerung", rank = "Rang 1" }, [2053] = { name = "Geringes Heilen", rank = "Rang 3" }, '
                '[17] = { name = "Machtwort: Schild" } } }, '
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
        return healing.compute(tmp, "Tester", fight_seconds=90, **kw)
    finally:
        icons.table = real_table
        os.environ.pop("SIMFOREVER_BIN", None)
        if real_env:
            os.environ["SIMFOREVER_BIN"] = real_env


def fresh(lang: str = "en", class_file: str = "PRIEST") -> dict:
    import tempfile
    i18n.set_language(lang)
    with tempfile.TemporaryDirectory() as tmp:
        write_fixture(tmp, class_file)
        return run(tmp)


def row(r: dict, spell_id: int) -> dict:
    return next(x for x in r["rows"] if x["spellId"] == spell_id)


def test_rows():
    r = fresh()
    assert [x["spellId"] for x in r["rows"]] == [2053, 139, 17, 2055], "the engine's order is kept"
    assert r["unit"] == "HPS" and r["engine"] == {"version": "stub", "spellbookSource": "export"}
    assert r["fight"] == {"seconds": 90, "targetDelta": 2}

    lesser = row(r, 2053)
    # The game's rank line from the spellbook export; the name is the engine's.
    assert lesser["label"] == "Lesser Heal (Rang 3)", lesser["label"]
    assert (lesser["min"], lesser["max"], lesser["perCast"], lesser["mana"]) == (240, 260, 250.0, 75)
    assert lesser["hps"] == 100.0 and lesser["hpm"] == 3.33 and lesser["hpsOnTarget"] is None
    assert lesser["bonusPct"] == 20.0 and lesser["coefPct"] == 50.0 and lesser["bonusLossPct"] == 0
    assert lesser["castsPerBar"] == 20.0
    # Casting nothing else: 30 mana/s against 5 while casting -> 60 s, 100 hps.
    assert lesser["spamSeconds"] == 60.0 and lesser["spamHealing"] == 6000

    renew = row(r, 139)
    assert renew["label"] == "Renew (Rang 1)" and renew["kind"] == "hot"
    assert renew["hpsOnTarget"] == 10.0, "150 over the 15 s it runs"
    assert renew["bonusLossPct"] == 10 and renew["spamSeconds"] is None and renew["spamHealing"] is None
    assert (renew["perTick"], renew["ticks"], renew["durationSec"]) == (30, 5, 15)

    shield = row(r, 17)
    # No rank line in the export and rank 1: the name alone.
    assert shield["label"] == "Shield" and shield["absorb"] == 60
    assert shield["hpsOnTarget"] == 4.0, "60 per 15 s of Weakened Soul"

    heal = row(r, 2055)
    assert heal["label"] == "Heal (Rank 2)", "no rank line in the export: the position in the chain"
    # 4 mana/s against 5 regenerated: it never runs out.
    assert heal["spamSeconds"] is None and heal["castsPerBar"] == 125.0


def test_marks_and_uses():
    r = fresh()
    assert row(r, 139)["best"] == ["hpm"], "the most healing per mana of all"
    assert row(r, 2055)["best"] == ["hps"], "the most per second among the heals cast back to back"
    assert row(r, 2053)["best"] == [] and row(r, 17)["best"] == []
    uses = lambda spell_id: [u["key"] for u in row(r, spell_id)["uses"]]  # noqa: E731
    assert uses(139) == ["light", "sustained"] and uses(2053) == ["light"]
    assert uses(2055) == ["burst", "sustained"] and uses(17) == []
    assert [u["title"] for u in row(r, 139)["uses"]] == ["Light damage", "Sustained"]

    rec = r["recommended"]
    assert [x["key"] for x in rec] == ["light", "burst"]
    assert rec[0]["oomSeconds"] is None and rec[1]["oomSeconds"] == 62.4
    assert [s["label"] for s in rec[0]["steps"]] == ["Renew (Rang 1)", "Lesser Heal (Rang 3)", "Dark Sacrifice"]
    assert rec[0]["steps"][0]["rule"] == "keep it on the target"
    assert rec[0]["steps"][1]["rule"] == "the default heal, with pauses in between"
    sus = r["sustained"]
    assert (sus["seconds"], sus["value"], sus["longSeconds"], sus["longValue"]) == (90, 120.0, 300.0, 80.0)
    assert [s["label"] for s in sus["steps"]] == ["Renew (Rang 1)", "Heal (Rank 2)"], "the wand heals nothing"


def test_regeneration():
    r = fresh()
    s = r["stats"]
    assert (s["healing"], s["critPct"], s["manaMax"], s["spirit"]) == (100, 5.2, 1500, 100)
    # 5 mana/s while casting, 8 outside the rule; 10 per 5 s of that are gear.
    assert (s["castingPer5"], s["idlePer5"], s["gearMp5"], s["spiritPer5"]) == (25.0, 40.0, 10, 30.0)
    assert s["castingRegenPct"] == 50
    assert len(r["notes"]) == 3
    assert "(10 now)" in r["notes"][0] and "(30 mana per 5 sec.)" in r["notes"][0]
    assert r["notes"][1] == "Your talents let 50% of it through while casting."


def test_weights():
    w = fresh()["weights"]
    assert (w["seconds"], w["longSeconds"]) == (90, 300.0)
    by_key = {x["key"]: x for x in w["rows"]}
    assert [x["key"] for x in w["rows"]] == ["healing", "int", "spi", "mp5", "crit"]
    assert by_key["healing"] == {"key": "healing", "label": "Healing bonus", "short": 0.2, "shortBonus": 1.0,
                                 "long": 0.1, "longBonus": 1.0}
    # 6 HPS for 10 intellect against 2 for 10 healing: a point of intellect does what 3 of healing do.
    assert (by_key["int"]["short"], by_key["int"]["shortBonus"]) == (0.6, 3.0)
    assert (by_key["int"]["long"], by_key["int"]["longBonus"]) == (0.05, 0.5)
    assert (by_key["mp5"]["short"], by_key["mp5"]["shortBonus"], by_key["mp5"]["longBonus"]) == (0.4, 2.0, 5.0)
    assert (by_key["crit"]["short"], by_key["crit"]["shortBonus"], by_key["crit"]["long"]) == (0.5, 2.5, 0.0)


def test_german():
    r = fresh("de")
    assert [u["title"] for u in row(r, 139)["uses"]] == ["Leichter Schaden", "Dauerhaft"]
    assert r["weights"]["rows"][0]["label"] == "Heilbonus" and r["weights"]["rows"][2]["label"] == "Willenskraft"
    assert r["notes"][1] == "Deine Talente lassen davon 50 % beim Zaubern durch."
    assert r["recommended"][0]["steps"][0]["rule"] == "auf dem Ziel halten"
    i18n.set_language("en")


def test_class_without_heals():
    import tempfile
    i18n.set_language("en")
    assert healing.supported("PRIEST") and not healing.supported("WARLOCK") and not healing.supported(None)
    with tempfile.TemporaryDirectory() as tmp:
        write_fixture(tmp, "WARLOCK")
        try:
            run(tmp)
        except LookupError as e:
            assert "WARLOCK" in str(e)
        else:
            raise AssertionError("a warlock has no heal table")


def test_hit_text():
    i18n.set_language("en")
    r = fresh()
    assert healing.hit_text(row(r, 2053)) == "240-260"
    assert healing.hit_text(row(r, 139)) == "5 x 30 over 15 s"
    assert healing.hit_text(row(r, 17)) == "absorbs 60"


if __name__ == "__main__":
    for name, fn in sorted(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
            print("ok", name)
