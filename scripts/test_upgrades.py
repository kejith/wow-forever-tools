# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Checks for the scoring in upgrades.py, without the game:  uv run scripts/test_upgrades.py

Tooltip lines appear the way an English and a German client write them; both must give the same result.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import i18n  # noqa: E402
import upgrades as u  # noqa: E402

SUPPRESSION, IMPROVED_LIFE_TAP, PANDEMIC = u.SUPPRESSION, u.IMPROVED_LIFE_TAP_ID, u.PANDEMIC


def char(level=46, hit=5, talents=(), equipment=()):
    return {"character": {"level": level, "stats": {"spellHit": hit, "spellDamage": {"shadow": 180}}},
            "talents": {"class": [{"talents": [{"spellID": n, "rank": r} for n, r in talents]}]},
            "equipment": list(equipment)}


def test_norm_stats_reads_forever_keys():
    # The keys the client really exports: one rating for melee and spells, percent only in the tooltip.
    rec = {"stats": {"ITEM_MOD_HIT_RATING_SHORT": 10, "ITEM_MOD_CRIT_RATING_SHORT": 28, "ITEM_MOD_SPELL_POWER_SHORT": 9,
                     "ITEM_MOD_SHADOW_DAMAGE_DONE_SHORT": 4, "ITEM_MOD_SPELL_HEALING_DONE_SHORT": 30,
                     "ITEM_MOD_FIRE_DAMAGE_DONE_SHORT": 50, "ITEM_MOD_MANA_REGENERATION_SHORT": 4,
                     "ITEM_MOD_DAMAGE_PER_SECOND_SHORT": 40, "RESISTANCE0_NAME": 60},
           "tooltip": ["Anlegen: Verbessert Eure Trefferchance um 1.0%.",
                       "Anlegen: Erhöht Eure kritische Trefferchance um 2.0%."]}
    assert u.norm_stats(rec) == {"sp": 13, "mp5": 4, "armor": 60, "hit": 1.0, "crit": 2.0}
    english = dict(rec, tooltip=["Equip: Improves your chance to hit by 1.0%.",
                                 "Equip: Improves your chance to get a critical strike by 2.0%."])
    assert u.norm_stats(english) == u.norm_stats(rec)
    assert u.norm_stats(rec, ranged=True)["wand_dps"] == 40
    # Without a tooltip (equipped items of old exports) the rating is converted.
    assert u.norm_stats({"stats": rec["stats"]})["crit"] == 2.0
    assert u.norm_stats({"stats": rec["stats"]}, ratings={"hit": 20, "crit": 14})["hit"] == 0.5


def test_learn_ratings():
    items = {1: {"stats": {"ITEM_MOD_HIT_RATING_SHORT": 24}, "tooltip": ["Anlegen: Verbessert Eure Trefferchance um 2.0%."]}}
    assert u.learn_ratings(items) == {"hit": 12.0, "crit": 14.0}
    items = {1: {"stats": {"ITEM_MOD_CRIT_RATING_SHORT": 30},
                 "tooltip": ["Equip: Improves your chance to get a critical strike by 2.0%."]}}
    assert u.learn_ratings(items) == {"hit": 10.0, "crit": 15.0}


def test_hit_counts_only_up_to_the_cap():
    lvl = u.build_scale(char(hit=5))  # levelling: 6 % miss against +2, 1 % always stays, 5 % already there
    assert lvl["profile"] == "leveling"
    assert u.set_value({"hit": 3}, lvl) == 0

    dung = u.build_scale(char(hit=5, talents=[(SUPPRESSION, 5)]), "dungeon")  # 16 % needed, 11 missing
    one, two, cap, over = (u.set_value({"hit": h}, dung) for h in (1, 2, 11, 20))
    assert one > 0 and cap == over  # nothing beyond the cap
    assert abs((two - one) - 0.25 * one) < 1e-9  # after 1 % the affliction spells are capped by Suppression

    assert u.build_scale(char(level=60))["profile"] == "dungeon"


def test_crit_depends_on_talents():
    plain = u.build_scale(char())["weights"]["crit"]
    pandemic = u.build_scale(char(talents=[(PANDEMIC, 3)]))["weights"]["crit"]
    assert 0 < plain < pandemic


def pooled(level=46, sp=180, intellect=100, health=1500, mana=2000, talents=(), **extra):
    c = char(level=level, talents=talents)
    c["character"]["stats"].update(intellect=intellect, healthMax=health, powerMax=mana, spellCrit={"shadow": 3.7}, **extra)
    c["character"]["stats"]["spellDamage"]["shadow"] = sp
    return c


def test_intellect_and_stamina_follow_the_character():
    # Without health/mana in the export the fixed profile weights stay.
    fixed = u.build_scale(char())["weights"]
    assert (fixed["intellect"], fixed["stamina"]) == (0.35, 0.5)

    # With them: Int = 15 mana + crit from intellect, Stamina = 10 health (Life Tap + survival).
    s = u.build_scale(pooled(critFromIntellect=2.0, lifeTap={"id": 11688, "text": "Wandelt 310 Gesundheit in 310 Mana um."}))
    pct, k = s["pct"], u.PROFILES["leveling"]
    per_mana = k["mana_share"] * pct * 100 / (2000 + 1500)
    assert abs(s["weights"]["intellect"] - round(15 * per_mana + 0.02 * s["weights"]["crit"], 3)) < 1e-9
    assert abs(s["weights"]["stamina"] - round(10 * per_mana + 10 * k["survival_share"] * pct * 100 / 1500, 3)) < 1e-9

    # Without critFromIntellect (addon < 1.3) the crit comes from the spell crit minus the base: (3.7 - 1.7) / 100.
    assert abs(u.crit_per_intellect(pooled()["character"]["stats"])[0] - 0.02) < 1e-9

    # A bigger pool makes each point worth less; Improved Life Tap makes stamina worth more.
    assert u.build_scale(pooled(mana=4000))["weights"]["intellect"] < u.build_scale(pooled())["weights"]["intellect"]
    ilt = u.build_scale(pooled(talents=[(IMPROVED_LIFE_TAP, 2)]))["weights"]["stamina"]
    assert ilt > u.build_scale(pooled())["weights"]["stamina"]


def test_life_tap_ratio():
    st = {"lifeTap": {"text": "Wandelt 424 Gesundheit in 424 Mana um."}, "hpPerStamina": 10}
    assert u.life_tap_ratio(st, 56, 0)[0] == 1.0
    assert abs(u.life_tap_ratio(st, 56, 2)[0] - 1.2) < 1e-9
    assert u.life_tap_ratio({"hpPerStamina": 10}, 46, 2)[0] == 0.0  # addon 1.3 exported: no Life Tap known
    assert u.life_tap_ratio({}, 46, 0)[0] == 1.0  # old export: assume the Classic 1:1
    assert u.life_tap_ratio({}, 4, 0)[0] == 0.0
    # English client, rank with thousands separators in either notation.
    en = {"lifeTap": {"text": "Converts 1,200 Health into 1,500 Mana for you. Spirit increases the amount converted."}}
    de = {"lifeTap": {"text": "Wandelt 1.200 Gesundheit in 1.500 Mana für Euch um."}}
    assert u.life_tap_ratio(en, 60, 0)[0] == u.life_tap_ratio(de, 60, 0)[0] == 1.25


def test_special_effects():
    tip = ["Anlegen: +10 Angriffskraft.", "Anlegen: Verbessert Eure Trefferchance um 1.0%.",
           "Anlegen: Erhöht durch magische Zauber und Effekte verursachten Schaden und Heilung um bis zu 9.",
           "Anlegen: Erhöht durch sämtliche Zauber und magische Effekte verursachte Heilung um bis zu 7 und den verursachten Schaden um bis zu 3.",
           "Anlegen: Stellt alle 5 Sek. 6 Mana wieder her.", "Trefferchance: Heilt 12 Sek. lang 78 Schaden.",
           "Anlegen: Erhöht den durch magische Zauber und Effekte verursachten Schaden gegen Wildtiere um bis zu 26.",
           "Benutzen: Entfernt alle Unbeweglichkeitseffekte."]
    assert u.special_effects(tip) == [tip[6], tip[7]]
    tip = ["Equip: +10 Attack Power.", "Equip: Improves your chance to hit by 1.0%.",
           "Equip: Increases damage and healing done by magical spells and effects by up to 9.",
           "Equip: Increases healing done by up to 7 and damage done by up to 3 for all magical spells and effects.",
           "Equip: Restores 6 mana per 5 sec.", "Chance on hit: Heals 78 damage over 12 sec.",
           "Equip: Increases damage done to Beasts by magical spells and effects by up to 26.",
           "Use: Removes all immobilizing effects.", "Equip: Increased Defense +7.",
           "Equip: Increases damage done by Shadow spells and effects by up to 14."]
    assert u.special_effects(tip) == [tip[6], tip[7]]


def test_class_and_skill_requirements():
    skills = {"Engineering": 200, "Ingenieurskunst": 200}
    assert u.meets_skill_requirements(["Requires Engineering (150)"], skills)
    assert not u.meets_skill_requirements(["Requires Engineering (215)"], skills)
    assert not u.meets_skill_requirements(["Benötigt Ingenieurskunst (215)"], skills)
    assert not u.meets_skill_requirements(["Requires Tailoring (1)"], skills)
    assert u.meets_skill_requirements(["Requires Level 40"], skills)


def test_texts_follow_the_language():
    assert u.slot_name(16) == "Main Hand" and u.short({"ITEM_MOD_STAMINA_SHORT": 5}) == "Sta 5"
    rebuilt = u.tooltip({"name": "Old Staff", "reqLevel": 40, "stats": {"ITEM_MOD_SPELL_POWER_SHORT": 9}}, "Main Hand")
    assert rebuilt["rebuilt"] and "Requires Level 40" in rebuilt["lines"]
    assert rebuilt["lines"][-1].startswith("Equip: Increases damage and healing")
    i18n.set_language("de")
    try:
        assert u.slot_name(16) == "Waffenhand" and u.short({"ITEM_MOD_STAMINA_SHORT": 5}) == "Ausd 5"
        assert u.build_scale(char())["label"] == "Leveln"
        rebuilt = u.tooltip({"name": "Old Staff", "reqLevel": 40, "stats": {"ITEM_MOD_SPELL_POWER_SHORT": 9}})
        assert "Benötigt Stufe 40" in rebuilt["lines"] and rebuilt["lines"][-1].startswith("Anlegen: Erhöht")
    finally:
        i18n.set_language("en")
    assert u.build_scale(char())["label"] == "Leveling"


def test_best_package():
    a, b = [(30, 10.0, ("a1",)), (50, 14.0, ("a2",))], [(40, 9.0, ("b1",)), (90, 30.0, ("b2",))]
    assert u.best_package([a, b], 0) == (0, 0.0, ())
    assert u.best_package([a, b], 70) == (70, 19.0, ("a1", "b1"))
    assert u.best_package([a, b], 100) == (90, 30.0, ("b2",))  # one expensive item beats two cheap ones
    assert u.best_package([a, b], 1000) == (140, 44.0, ("a2", "b2"))


def write_fixture(tmp: str) -> dict:
    """A temporary WoW folder with a warlock and an item export; returns the prices."""
    wtf = os.path.join(tmp, "WTF", "Account", "TEST")
    char_dir = os.path.join(wtf, "93", "Tester - Realm", "SavedVariables")
    os.makedirs(char_dir)
    os.makedirs(os.path.join(wtf, "SavedVariables"))

    def item(name, loc, sp, cls=4, sub=1, tip=""):
        return (f'{{ name = "{name}", quality = 2, reqLevel = 40, equipLoc = "{loc}", classID = {cls}, '
                f'subclassID = {sub}, stats = {{ ITEM_MOD_SPELL_POWER_SHORT = {sp} }}, tooltip = {{ {tip} }} }}')

    with open(os.path.join(char_dir, "WoWMCP.lua"), "w", encoding="utf-8") as f:
        f.write('WoWMCP_Char = { character = { name = "Tester", class = "Warlock", classFile = "WARLOCK", '
                'level = 46, money = 1000000, '
                'stats = { spellHit = 5, spellDamage = { shadow = 100 } } }, '
                'client = { version = "9.9.9", build = 9, locale = "deDE" }, equipment = {'
                '{ slot = 16, name = "Old Staff", equipLoc = "INVTYPE_2HWEAPON", stats = { ITEM_MOD_SPELL_POWER_SHORT = 30 } },'
                '{ slot = 11, name = "Ring A", equipLoc = "INVTYPE_FINGER", stats = { ITEM_MOD_SPELL_POWER_SHORT = 2 } },'
                '{ slot = 12, name = "Ring B", equipLoc = "INVTYPE_FINGER", stats = { ITEM_MOD_SPELL_POWER_SHORT = 8 } },'
                '} }\n')
    with open(os.path.join(wtf, "SavedVariables", "WoWMCP.lua"), "w", encoding="utf-8") as f:
        f.write("WoWMCP_Items = { updated = 1, items = {\n"
                f'[101] = {item("Dagger", "INVTYPE_WEAPONMAINHAND", 25, 2, 15)},\n'
                f'[102] = {item("Orb", "INVTYPE_HOLDABLE", 20, 4, 0)},\n'
                f'[103] = {item("New Staff", "INVTYPE_2HWEAPON", 40, 2, 10)},\n'
                f'[104] = {item("Ring C", "INVTYPE_FINGER", 10, 4, 0)},\n'
                f'[105] = {item("Ring D", "INVTYPE_FINGER", 12, 4, 0)},\n'
                f'[106] = {item("Axe", "INVTYPE_2HWEAPON", 99, 2, 1)},\n'
                f"""[107] = {item("Mage Ring", "INVTYPE_FINGER", 50, 4, 0, '"Classes: Mage, Priest"')},\n"""
                f"""[108] = {item("Goggles", "INVTYPE_HEAD", 50, 4, 1, '"Requires Engineering (215)"')},\n"""
                f"""[109] = {item("Warlock Hood", "INVTYPE_HEAD", 6, 4, 1, '"Classes: Warlock"')},\n"""
                "} }\n")
    return {str(i): {"m": 10000, "a": {"100": 1}} for i in range(101, 110)}


def test_compute_weapons_and_package(tmp: str):
    """End to end against a temporary WoW folder: a staff is compared with main hand + off hand."""
    prices = write_fixture(tmp)
    r = u.compute(tmp, "Tester", prices=prices, top=5)
    by_slot = {s["slot"]: {c["name"]: c for c in s["candidates"]} for s in r["slots"]}

    assert by_slot[16]["New Staff"]["gain"] == 10 and by_slot[16]["New Staff"]["gainPct"] == 33
    assert "Dagger" not in by_slot[16] and 17 not in by_slot  # each alone is worse than the staff
    assert "Axe" not in by_slot[16]  # not a warlock weapon
    assert by_slot[11]["Ring D"]["gain"] == 10  # against the weaker ring
    assert "Mage Ring" not in by_slot[11]  # the class line of the tooltip excludes it
    assert set(by_slot[1]) == {"Warlock Hood"}  # the goggles need a skill the character does not have
    names = {p["name"]: p for p in r["package"]["items"]}
    assert set(names) == {"Dagger", "Orb", "Ring C", "Ring D", "Warlock Hood"}  # the pair beats the new staff, both rings go
    assert r["package"]["gain"] == 15 + 12 + 6 and r["package"]["cost"] == 50000
    assert names["Dagger"]["replaces"] == ["Old Staff"] and names["Orb"]["replaces"] == []

    r = u.compute(tmp, "Tester", prices=prices, max_price=1)  # one gold: the best single purchase
    assert [p["name"] for p in r["package"]["items"]] in (["New Staff"], ["Ring D"])
    assert r["package"]["gain"] == 10


STUB_ENGINE = '''
import json, sys
data = json.load(sys.stdin)
assert data["character"]["classFile"] == "WARLOCK" and data["build"] == "9.9.9.9" and data["cacheDir"]
assert data["fight"] == {"seconds": 45.0, "targetDelta": 3}
res = {e["key"]: {"gain": 2.0 * e["delta"].get("sp", 0), "value": 100 + 2.0 * e["delta"].get("sp", 0),
                 "gainPct": 0} for e in data["evaluations"]}
json.dump({"engine": "stub", "spellbookSource": "assumed",
           "baseline": {"metric": "dps", "value": 100.0, "ttoomSeconds": 42.0,
                        "manaSpentPs": 5.0, "manaBudgetPs": 6.0,
                        "breakdown": [{"label": "Shadow Bolt", "value": 60.0, "sharePct": 80.0, "kind": "filler"},
                                      {"label": "Life Tap", "kind": "lifetap", "perMin": 3.0, "sharePct": 5.0}]},
           "results": res}, sys.stdout)
'''


def test_compute_with_engine(tmp: str):
    """The engine path: a stub binary answers (gain = 2 x spell power change), its numbers land unchanged.
    The stub also proves what the bundle carries (class, build, fight). No network: the table download is
    stubbed out, the engine binary is the Python script above (simengine runs *.py through the interpreter)."""
    import icons
    import simengine
    prices = write_fixture(tmp)
    stub = os.path.join(tmp, "engine_stub.py")
    with open(stub, "w", encoding="utf-8") as f:
        f.write(STUB_ENGINE)
    real_table, real_env = icons.table, os.environ.get("SIMFOREVER_BIN")
    icons.table = lambda *a, **k: []
    os.environ["SIMFOREVER_BIN"] = stub
    try:
        r = u.compute(tmp, "Tester", prices=prices, top=5, engine="on", fight_seconds=45, target_delta=3)
    finally:
        icons.table = real_table
        os.environ.pop("SIMFOREVER_BIN", None)
        if real_env:
            os.environ["SIMFOREVER_BIN"] = real_env

    by_slot = {s["slot"]: {c["name"]: c for c in s["candidates"]} for s in r["slots"]}
    assert by_slot[16]["New Staff"]["gain"] == 20  # 2 x (40 - 30) spell power, from the stub
    assert by_slot[11]["Ring D"]["gain"] == 20  # still against the weaker ring (2 x (12 - 2))
    assert r["setValue"] == 100 and r["engine"]["metric"] == "dps" and r["engine"]["ttoomSeconds"] == 42
    assert r["engine"]["spellbookSource"] == "assumed" and r["engine"]["fight"] == {"seconds": 45.0, "targetDelta": 3}
    assert [row["label"] for row in r["scale"]] == ["Shadow Bolt", "Life Tap"]
    assert r["package"]["gain"] == round(sum(p["gain"] for p in r["package"]["items"]), 1)
    assert r["package"]["gainPct"] == round(r["package"]["gain"] / 100 * 100)

    # Without the binary, "auto" falls back to the weight scale and "on" refuses.
    os.environ["SIMFOREVER_BIN"] = os.path.join(tmp, "missing")
    try:
        r = u.compute(tmp, "Tester", prices=prices, top=5, engine="auto")
        assert r["engine"] is None and r["setValue"] != 100
        try:
            u.compute(tmp, "Tester", prices=prices, engine="on")
            raise AssertionError("engine=on must fail without the binary")
        except LookupError:
            pass
    finally:
        os.environ.pop("SIMFOREVER_BIN", None)
        if real_env:
            os.environ["SIMFOREVER_BIN"] = real_env


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
