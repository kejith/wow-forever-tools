# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Checks for the damage analysis (damage.py) without game, network and engine:  uv run scripts/test_damage.py

The combat engine is a stub that answers with a hand-made table, three situations and fixed gains; what is
checked is what damage.py makes of them: the labels, the derived columns (damage per second kept up and per
cooldown, casting nothing else), the marks, where each rank is cast, the order and the sentences of the
rotations, the character's numbers and the stat weights.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import damage  # noqa: E402
import i18n  # noqa: E402

STUB_ENGINE = '''
import json, os, sys
data = json.load(sys.stdin)
assert data["metric"] == "dps" and data["character"]["classFile"] == "WARLOCK"
seconds = data["fight"]["seconds"]
assert seconds in (90.0, 300.0) and data["fight"]["targetDelta"] == 2
out = {"engine": "stub", "spellbookSource": "export", "fight": data["fight"],
       "baseline": {"metric": "dps", "value": 210.0 if seconds == 90.0 else 180.0, "breakdown": []}}
if data.get("damageTable"):
    assert data["situations"] is True and data["evaluations"] == [] and seconds == 90.0
    out["damageTable"] = {} if os.environ.get("TEST_NO_ROWS") else {
        "schools": [{"school": "shadow", "power": 200, "critPct": 5.25}, {"school": "fire", "power": 150, "critPct": 5.25}],
        "targetDelta": 2, "missPct": 1, "manaMax": 3000, "healthMax": 2000,
        "regenCastingPerSec": 2.0, "regenIdlePerSec": 20.0, "wandDps": 40.26, "tapMana": 500, "tapHealth": 420,
        "rows": [
            {"spellId": 686, "baseId": 686, "name": "Shadow Bolt", "rank": 1, "level": 1, "school": "shadow",
             "kind": "direct", "min": 100, "max": 110, "critPct": 5.25, "hitPct": 99, "perCast": 108.0, "mana": 20,
             "castSec": 1.5, "executeSec": 1.5, "dps": 72.0, "dpm": 5.4, "bonus": 80.0, "coefPct": 40.0},
            {"spellId": 11660, "baseId": 686, "name": "Shadow Bolt", "rank": 8, "level": 52, "school": "shadow",
             "kind": "direct", "min": 440, "max": 470, "critPct": 5.25, "hitPct": 99, "perCast": 480.0, "mana": 302,
             "castSec": 2.5, "executeSec": 2.5, "dps": 192.0, "dpm": 1.589, "bonus": 190.0, "coefPct": 95.0, "top": True},
            {"spellId": 11671, "baseId": 172, "name": "Corruption", "rank": 5, "level": 44, "school": "shadow",
             "kind": "dot", "perTick": 110, "ticks": 6, "durationSec": 18, "critPct": 5.25, "tickCrits": True,
             "hitPct": 99, "perCast": 720.0, "mana": 225, "castSec": 0, "executeSec": 1.5, "dps": 480.0, "dpm": 3.2,
             "bonus": 300.0, "coefPct": 150.0, "top": True},
            {"spellId": 11699, "baseId": 689, "name": "Drain Life", "rank": 5, "level": 46, "school": "shadow",
             "kind": "channel", "perTick": 86, "ticks": 5, "durationSec": 5, "critPct": 5.25, "hitPct": 99,
             "perCast": 450.0, "mana": 10, "castSec": 5, "executeSec": 5, "dps": 90.0, "dpm": 45.0, "bonus": 140.0,
             "coefPct": 70.0, "leech": 450.0, "top": True},
            {"spellId": 11683, "baseId": 1949, "name": "Hellfire", "rank": 2, "level": 42, "school": "fire",
             "kind": "channel", "perTick": 146, "ticks": 15, "durationSec": 15, "critPct": 5.25, "hitPct": 99,
             "perCast": 3000.0, "mana": 975, "castSec": 15, "executeSec": 15, "dps": 200.0, "dpm": 3.077,
             "bonus": 51.0, "coefPct": 34.0, "selfDamage": 2190.0, "area": True, "top": True},
            {"spellId": 17925, "baseId": 6789, "name": "Death Coil", "rank": 2, "level": 50, "school": "shadow",
             "kind": "direct", "min": 465, "max": 465, "critPct": 5.25, "hitPct": 99, "perCast": 480.0, "mana": 525,
             "castSec": 0, "executeSec": 1.5, "cooldownSec": 120, "dps": 320.0, "dpm": 0.914, "bonus": 48.0,
             "coefPct": 24.0, "leech": 480.0, "top": True}]}

    def steps(*kinds):
        every = {
            "dot": {"kind": "dot", "spellId": 11671, "name": "Corruption", "rank": 5, "casts": 5, "perMin": 3.3,
                    "firstSec": 0, "sharePct": 8.0, "value": 40.0},
            "cooldown": {"kind": "cooldown", "spellId": 17925, "name": "Death Coil", "rank": 2, "casts": 1,
                         "perMin": 0.7, "firstSec": 1.5, "sharePct": 2.0, "value": 5.0},
            "filler": {"kind": "filler", "spellId": 11660, "name": "Shadow Bolt", "rank": 8, "casts": 30,
                       "perMin": 20.0, "firstSec": 3, "sharePct": 80.0, "value": 170.0},
            "lifetap": {"kind": "lifetap", "spellId": 11689, "name": "Life Tap", "rank": 5, "casts": 6,
                        "perMin": 4.0, "firstSec": 20, "sharePct": 10.0, "value": 0},
            "recover": {"kind": "recover", "spellId": 11699, "name": "Drain Life", "rank": 5, "casts": 4,
                        "perMin": 0.4, "firstSec": 200, "sharePct": 3.0, "value": 6.0},
            "wand": {"kind": "wand", "casts": 20, "perMin": 2.0, "firstSec": 400, "sharePct": 5.0, "value": 2.0}}
        return [every[k] for k in kinds]

    fight = dict(data["fight"], killsPerMinute=0.67)
    out["situations"] = [
        {"key": "standard", "fight": fight, "metric": "dps", "value": 205.0, "oomSeconds": None,
         "steps": steps("dot", "filler", "lifetap"), "endManaPct": 12.0, "endHpPct": 70.0, "candidates": 8,
         "skipped": [{"kind": "cooldown", "spellId": 17925, "name": "Death Coil", "rank": 2, "loss": 3.25}]},
        {"key": "max", "fight": fight, "metric": "dps", "value": 215.0,
         "oomSeconds": None if os.environ.get("TEST_MANA_LASTS") else 48.0, "freeMana": True,
         "steps": steps("dot", "cooldown", "filler"), "endManaPct": 0.0, "endHpPct": 100.0, "candidates": 8},
        {"key": "endurance", "fight": dict(fight, seconds=600), "metric": "dps", "value": 190.0, "oomSeconds": 400.0,
         "steps": steps("dot", "filler", "lifetap", "recover", "wand"), "endManaPct": 2.0, "endHpPct": 61.0,
         "candidates": 8}]
else:
    keys = [e["key"] for e in data["evaluations"]]
    assert keys == ["sp", "int", "spi", "sta", "mp5", "hit", "crit"], keys
    assert data["evaluations"][0]["delta"] == {"sp": 10} and data["evaluations"][5]["delta"] == {"hit": 1}
    gains = {"sp": 5.0, "int": 1.0, "spi": 0.0, "sta": 0.5, "mp5": 0.0, "hit": 2.0, "crit": 1.5}
    if seconds == 300.0:
        gains = {"sp": 4.0, "int": 2.0, "spi": 1.0, "sta": 0.0, "mp5": 1.0, "hit": 0.0, "crit": 1.2}
    out["results"] = {k: {"gain": v} for k, v in gains.items()}
json.dump(out, sys.stdout)
'''


def write_fixture(tmp: str, class_file: str = "WARLOCK") -> None:
    char_dir = os.path.join(tmp, "WTF", "Account", "TEST", "93", "Tester - Realm", "SavedVariables")
    os.makedirs(char_dir)
    with open(os.path.join(char_dir, "WoWMCP.lua"), "w", encoding="utf-8") as f:
        f.write('WoWMCP_Char = { character = { name = "Tester", class = "Hexenmeister", classFile = "%s", level = 52, '
                'stats = { powerMax = 3000, healthMax = 2000, spirit = 100 } }, '
                'spellbook = { updated = 1, count = 3, spells = { '
                '[11660] = { name = "Schattenblitz", rank = "Rang 8" }, [11671] = { name = "Verderbnis", rank = "Rang 5" }, '
                '[11689] = { name = "Aderlass", rank = "Rang 5" }, [17925] = { name = "Todesmantel" } } }, '
                'talents = { class = {} }, client = { version = "9.9.9", build = 9, locale = "deDE" }, '
                'equipment = {} }\n' % class_file)


def run(tmp: str, **env) -> dict:
    """compute() against the stub engine; the table download is stubbed out."""
    import icons
    stub = os.path.join(tmp, "engine_stub.py")
    with open(stub, "w", encoding="utf-8") as f:
        f.write(STUB_ENGINE)
    real_table, real_env = icons.table, os.environ.get("SIMFOREVER_BIN")
    icons.table = lambda *a, **k: []
    os.environ["SIMFOREVER_BIN"] = stub
    os.environ.update(env)
    try:
        return damage.compute(tmp, "Tester", fight_seconds=90)
    finally:
        icons.table = real_table
        os.environ.pop("SIMFOREVER_BIN", None)
        for key in env:
            os.environ.pop(key, None)
        if real_env:
            os.environ["SIMFOREVER_BIN"] = real_env


def fresh(lang: str = "en", class_file: str = "WARLOCK", **env) -> dict:
    import tempfile
    i18n.set_language(lang)
    with tempfile.TemporaryDirectory() as tmp:
        write_fixture(tmp, class_file)
        return run(tmp, **env)


def row(r: dict, spell_id: int) -> dict:
    return next(x for x in r["rows"] if x["spellId"] == spell_id)


def test_rows():
    r = fresh()
    assert [x["spellId"] for x in r["rows"]] == [686, 11660, 11671, 11699, 11683, 17925], "the engine's order is kept"
    assert r["unit"] == "DPS" and r["engine"] == {"version": "stub", "spellbookSource": "export"}
    assert r["fight"] == {"seconds": 90, "targetDelta": 2}
    assert r["rated"] == {"seconds": 90, "value": 210.0}

    bolt = row(r, 11660)
    # The game's rank line from the spellbook export; the name is the engine's.
    assert bolt["label"] == "Shadow Bolt (Rang 8)" and bolt["top"] and bolt["schoolLabel"] == "Shadow"
    assert (bolt["min"], bolt["max"], bolt["perCast"], bolt["mana"]) == (440, 470, 480.0, 302)
    # Cast back to back: the damage per second is that of the casting time.
    assert bolt["dps"] == bolt["perCastSec"] == 192.0 and bolt["limit"] is None and bolt["spammable"]
    assert bolt["dpm"] == 1.59 and bolt["bonusPct"] == 39.6 and bolt["coefPct"] == 95.0
    assert bolt["castsPerBar"] == 9.9
    # Casting nothing else: 120.8 mana/s against 2 while casting -> 25.3 s of 192 dps.
    assert bolt["spamSeconds"] == 25.3 and bolt["spamDamage"] == 4848
    low = row(r, 686)
    assert low["label"] == "Shadow Bolt" and not low["top"], "rank 1 has no rank line"

    corruption = row(r, 11671)
    assert corruption["kind"] == "dot" and corruption["tickCrits"]
    assert (corruption["perTick"], corruption["ticks"], corruption["durationSec"]) == (110, 6, 18)
    # Kept on the enemy it does 720 per 18 s; a second of casting buys 480.
    assert (corruption["dps"], corruption["limit"], corruption["perCastSec"]) == (40.0, "kept", 480.0)
    assert not corruption["spammable"] and corruption["spamSeconds"] is None

    drain = row(r, 11699)
    assert drain["label"] == "Drain Life (Rank 5)", "no rank line in the export: the position in the chain"
    assert drain["leech"] == 450 and drain["dps"] == 90.0 and drain["limit"] is None
    # 2 mana/s against 2 regenerated: it never runs out.
    assert drain["spammable"] and drain["spamSeconds"] is None

    coil = row(r, 17925)
    assert coil["label"] == "Death Coil (Rank 2)"
    assert (coil["dps"], coil["limit"], coil["perCastSec"]) == (4.0, "cooldown", 320.0), "480 every 2 minutes"
    assert not coil["spammable"]

    hellfire = row(r, 11683)
    assert hellfire["area"] and hellfire["selfDamage"] == 2190 and hellfire["schoolLabel"] == "Fire"


def test_marks_and_uses():
    r = fresh()
    assert row(r, 11699)["best"] == ["dpm"], "the most damage per mana of all"
    # Hellfire does more per second but hits several enemies and is no filler against one.
    assert row(r, 11660)["best"] == ["dps"], "the most per second among the spells cast back to back"
    assert all(row(r, i)["best"] == [] for i in (686, 11671, 11683, 17925))
    uses = lambda spell_id: [u["key"] for u in row(r, spell_id)["uses"]]  # noqa: E731
    assert uses(11671) == ["max", "endurance", "standard"] and uses(17925) == ["max"]
    assert uses(11699) == ["endurance"] and uses(686) == [] and uses(11683) == []
    assert [u["title"] for u in row(r, 11671)["uses"]] == ["Maximum damage", "Sustained damage", "Normal fight"]


def test_rotations():
    r = fresh()
    rots = r["rotations"]
    # The two ends first, then the fight the gear is rated for.
    assert [x["key"] for x in rots] == ["max", "endurance", "standard"]
    top, long, normal = rots
    assert top["title"] == "Maximum damage" and top["value"] == 215.0 and top["freeMana"]
    assert top["oomSeconds"] == 48.0 and top["seconds"] == 90
    assert top["about"].startswith("The normal fight as if your mana never ran out: the most damage you can do in 90")
    assert top["verdict"] == ("Your mana pays for 48 s of it. A fight that lasts longer needs the rotation of the "
                              "normal fight.")
    assert [s["label"] for s in top["steps"]] == ["Corruption (Rang 5)", "Death Coil (Rank 2)", "Shadow Bolt (Rang 8)"]
    assert [s["rule"] for s in top["steps"]] == ["keep it on the enemy", "whenever it is ready",
                                                 "fills the rest of the time"]
    assert top["steps"][2]["value"] == 170.0 and top["steps"][2]["sharePct"] == 80.0 and top["skipped"] == []

    assert long["title"] == "Sustained damage" and not long["freeMana"] and long["seconds"] == 600
    assert long["about"] == "10 minutes of fighting without a break."
    assert [s["kind"] for s in long["steps"]] == ["dot", "filler", "lifetap", "recover", "wand"]
    assert long["steps"][2]["label"] == "Life Tap (Rang 5)" and long["steps"][4]["label"] == "Wand"
    assert "Drain Life (Rank 5) brings it back" in long["verdict"] and "the wand fills the gaps" in long["verdict"]

    assert normal["title"] == "Normal fight" and normal["oomSeconds"] is None
    assert normal["verdict"].startswith("The mana lasts with Life Tap (Rang 5).")
    assert normal["skipped"] == [{"label": "Death Coil (Rank 2)", "loss": 3.2}]

    # Mana that pays for the whole fight: the maximum is the normal fight's rotation.
    lasts = fresh(TEST_MANA_LASTS="1")["rotations"][0]
    assert lasts["oomSeconds"] is None
    assert lasts["verdict"] == "Your mana pays for the whole fight like this: it is the rotation of the normal fight."


def test_stats_and_notes():
    r = fresh()
    s = r["stats"]
    assert s["schools"] == [{"school": "shadow", "label": "Shadow", "power": 200, "critPct": 5.25},
                            {"school": "fire", "label": "Fire", "power": 150, "critPct": 5.25}]
    assert (s["missPct"], s["targetDelta"], s["manaMax"], s["healthMax"]) == (1, 2, 3000, 2000)
    assert (s["castingPer5"], s["idlePer5"], s["wandDps"]) == (10.0, 100.0, 40.3)
    assert (s["tapMana"], s["tapHealth"]) == (500, 420)
    assert len(r["notes"]) == 3 and "an enemy 2 levels above you" in r["notes"][0]


def test_weights():
    w = fresh()["weights"]
    assert (w["seconds"], w["longSeconds"]) == (90, 300.0)
    by_key = {x["key"]: x for x in w["rows"]}
    assert [x["key"] for x in w["rows"]] == ["sp", "int", "spi", "sta", "mp5", "hit", "crit"]
    assert by_key["sp"] == {"key": "sp", "label": "Spell power", "short": 0.5, "shortPower": 1.0, "long": 0.4,
                            "longPower": 1.0}
    # 1 DPS for 10 intellect against 5 for 10 spell power: a point of intellect does what 0.2 of spell power do.
    assert (by_key["int"]["short"], by_key["int"]["shortPower"]) == (0.1, 0.2)
    assert (by_key["int"]["long"], by_key["int"]["longPower"]) == (0.2, 0.5)
    assert (by_key["hit"]["short"], by_key["hit"]["shortPower"], by_key["hit"]["long"]) == (2.0, 4.0, 0.0)
    assert (by_key["crit"]["short"], by_key["crit"]["shortPower"], by_key["crit"]["longPower"]) == (1.5, 3.0, 3.0)
    assert by_key["sta"]["label"] == "Stamina" and by_key["hit"]["label"] == "Spell hit, per 1%"


def test_german():
    r = fresh("de")
    assert [u["title"] for u in row(r, 11671)["uses"]] == ["Maximaler Schaden", "Dauerkampf", "Normaler Kampf"]
    assert r["stats"]["schools"][0]["label"] == "Schatten" and row(r, 11683)["schoolLabel"] == "Feuer"
    assert r["weights"]["rows"][0]["label"] == "Zaubermacht" and r["weights"]["rows"][5]["label"] == "Zaubertreffer, pro 1 %"
    assert r["rotations"][0]["verdict"].startswith("Dein Mana reicht dafür 48 s.")
    assert r["rotations"][0]["steps"][0]["rule"] == "auf dem Gegner halten"
    assert "2 Stufen über dir" in r["notes"][0]
    i18n.set_language("en")


def test_without_damage_spells():
    import tempfile
    i18n.set_language("en")
    assert damage.supported("WARLOCK") and damage.supported("PRIEST") and not damage.supported("MAGE")
    assert not damage.supported(None)
    with tempfile.TemporaryDirectory() as tmp:
        write_fixture(tmp, "MAGE")
        try:
            run(tmp)
        except LookupError as e:
            assert "MAGE" in str(e)
        else:
            raise AssertionError("a class the engine does not model has no analysis")
    try:
        fresh(TEST_NO_ROWS="1")
    except LookupError as e:
        assert str(e) == "This character knows no damage spell the combat engine models."
    else:
        raise AssertionError("a table without rows is an error")


def test_texts():
    i18n.set_language("en")
    r = fresh()
    assert damage.hit_text(row(r, 11660)) == "440-470"
    assert damage.hit_text(row(r, 17925)) == "465"
    assert damage.hit_text(row(r, 11671)) == "6 x 110 over 18 s"
    assert damage.dps_text(row(r, 11660)) == "192.0*"
    assert damage.dps_text(row(r, 11671)) == "40.0 kept up"
    assert damage.dps_text(row(r, 17925)) == "4.0 per cooldown"


if __name__ == "__main__":
    for name, fn in sorted(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
            print("ok", name)
