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

IMPROVED_LIFE_TAP, PANDEMIC = u.IMPROVED_LIFE_TAP_ID, u.PANDEMIC


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

    # 16 % needed, 11 missing. The 5 % of the sheet already hold Suppression (1 % per rank in Forever,
    # for every spell), so the talent adds nothing on top.
    dung = u.build_scale(char(hit=5, talents=[(18174, 5)]), "dungeon")
    one, two, cap, over = (u.set_value({"hit": h}, dung) for h in (1, 2, 11, 20))
    assert one > 0 and cap == over  # nothing beyond the cap
    assert abs(two - 2 * one) < 1e-9 and abs(cap - 11 * one) < 1e-9  # every percent up to the cap counts alike

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
                'spellbook = { updated = 1, count = 2, spells = { [686] = { name = "Shadow Bolt", rank = "Rank 3", '
                'text = "Sends a shadowy bolt at the enemy, causing 133 to 139 Shadow damage." }, '
                '[172] = { name = "Corruption" } } }, '
                'talents = { class = { { spent = 5, talents = { '
                '{ spellID = 18174, name = "Suppression", rank = 5, maxRank = 5 } } } } }, '
                'client = { version = "9.9.9", build = 9, locale = "deDE" }, equipment = {'
                '{ slot = 16, name = "Old Staff", equipLoc = "INVTYPE_2HWEAPON", stats = { ITEM_MOD_SPELL_POWER_SHORT = 30 } },'
                '{ slot = 11, name = "Ring A", equipLoc = "INVTYPE_FINGER", stats = { ITEM_MOD_SPELL_POWER_SHORT = 2 } },'
                '{ slot = 12, name = "Ring B", equipLoc = "INVTYPE_FINGER", stats = { ITEM_MOD_SPELL_POWER_SHORT = 8 } },'
                '{ slot = 18, name = "Old Wand", equipLoc = "INVTYPE_RANGEDRIGHT", '
                'stats = { ITEM_MOD_DAMAGE_PER_SECOND_SHORT = 20 } },'
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
assert data["fight"] == {"seconds": 45.0, "targetDelta": 3, "killsPerMinute": -1}, "negative kills: automatic"
assert data["character"]["spellbook"] == [172, 686], "the addon's spellbook export reaches the engine"
assert data["character"]["stats"]["wand"]["dps"] == 20, "the equipped wand's damage per second reaches the engine"
assert data["character"]["talents"] == {"18174": 5}
import os
cap = float(os.environ.get("STUB_SP_CAP") or 1e9)  # spell power above the cap adds nothing: gains stop adding up
gain = lambda e: 2.0 * min(e["delta"].get("sp", 0), cap)
res = {e["key"]: {"gain": gain(e), "value": 100 + gain(e), "gainPct": 0} for e in data["evaluations"]}
json.dump({"engine": "stub", "spellbookSource": "assumed",
           "fight": {"seconds": 45.0, "targetDelta": 3, "killsPerMinute": 0},
           "baseline": {"metric": "dps", "value": 100.0, "ttoomSeconds": 42.0,
                        "manaSpentPs": 5.0, "manaBudgetPs": 6.0, "selfHealPs": 4.5,
                        "breakdown": [{"label": "Shadow Bolt", "spellId": 686, "rank": 3, "value": 60.0,
                                       "sharePct": 80.0, "kind": "filler"},
                                      {"label": "Drain Life", "spellId": 689, "rank": 2, "value": 9.0,
                                       "sharePct": 5.0, "kind": "dot", "leech": 4.5},
                                      {"label": "Life Tap", "spellId": 1454, "kind": "lifetap", "perMin": 3.0,
                                       "sharePct": 5.0, "manaGain": 7.0, "healthCost": 6.0},
                                      {"label": "Power Word: Shield", "spellId": 17, "kind": "cooldown",
                                       "value": 8.0, "sharePct": 9.0, "absorb": True},
                                      {"label": "Dark Sacrifice", "spellId": 1277324, "kind": "mana",
                                       "perMin": 1.0, "sharePct": 2.5, "manaGain": 9.2, "healthCost": 7.25},
                                      {"label": "Curse of the Elements", "spellId": 440892, "kind": "debuff",
                                       "value": 0, "sharePct": 2.4, "takenPct": 10},
                                      {"label": "wand", "kind": "wand", "value": 1.0, "sharePct": 10.0}]},
           "coverage": {"talentValues": "tables", "candidates": 4,
                        "spells": [{"spellId": 5676, "name": "Searing Pain", "rank": 2, "status": "unmodeled",
                                    "reason": "damage"},
                                   {"spellId": 348, "name": "Immolate", "status": "idle", "reason": "weaker",
                                    "value": 97.5},
                                   {"spellId": 1277324, "name": "Dark Sacrifice", "status": "idle",
                                    "reason": "notneeded"},
                                   {"spellId": 689, "name": "Drain Life", "status": "idle", "reason": "nogain",
                                    "value": 100.0},
                                   {"spellId": 686, "name": "Shadow Bolt", "rank": 3, "status": "used",
                                    "reason": "rotation"}],
                        "talents": [{"spellId": 427712, "name": "Pandemic", "rank": 3, "status": "used",
                                     "reason": "rule", "unverified": True, "note": "Classic value."},
                                    {"spellId": 18174, "name": "", "rank": 5, "status": "sheet",
                                     "reason": "sheet"},
                                    {"spellId": 15270, "name": "Spirit Tap", "rank": 2, "status": "used",
                                     "reason": "kills", "value": 10.0},
                                    {"spellId": 1, "name": "Mystery", "rank": 1, "status": "unmodeled",
                                     "reason": "brand new code"}]},
           "spells": [{"spellId": 686, "baseId": 686, "rank": 3, "level": 12, "directMin": 133.8, "directMax": 138.2,
                       "mana": 25, "castSec": 1.7},
                      {"spellId": 172, "baseId": 172, "level": 4, "periodic": 999.0, "ticks": 4, "mana": 35,
                       "castSec": 1.5}],  # not in the coverage as cast or tried, and without a text: not checked
           "results": res}, sys.stdout)
'''


def test_tooltip_check():
    """The model's numbers per spell rank against the numbers in the game's text for that rank: both ends of
    a hit, all ticks together or one tick, a shield. Only the spells the model casts or tried count."""
    import simengine
    spells = [
        {"spellId": 2054, "baseId": 2054, "directMin": 366.0, "directMax": 406.5},  # 366 to 407: agrees
        {"spellId": 2055, "baseId": 2054, "directMin": 469.5, "directMax": 522.0},  # 470 to 521: rounding
        {"spellId": 139, "baseId": 139, "periodic": 156.6, "ticks": 5},  # all ticks together
        {"spellId": 689, "baseId": 689, "periodic": 215.2, "ticks": 5},  # the text names one tick
        {"spellId": 17, "baseId": 17, "absorb": 70.0},  # the game says 81
        {"spellId": 2050, "baseId": 2050, "directMin": 94.5, "directMax": 115.1},  # the range is off
        {"spellId": 755, "baseId": 755, "periodic": 54.5, "ticks": 10},  # a spell the model has no use for
        {"spellId": 8092, "baseId": 8092, "directMin": 63.6, "directMax": 70.1},  # not in the book
        {"spellId": 21562, "baseId": 21562},  # a buff: no amount to compare
    ]
    book = {
        2054: {"name": "Heal", "rank": "Rank 1", "text": "Heal your target for 366 to 407."},
        "2055": {"name": "Heal", "rank": "Rank 2", "text": "Heal your target for 470 to 521."},  # keys as text
        139: {"name": "Renew", "rank": "Rank 1", "text": "Heals the target of 156 damage over 15 sec."},
        689: {"name": "Drain Life", "text": "Transfers 43 health every 1 sec. Lasts 5 sec."},
        17: {"name": "Power Word: Shield", "rank": "Rank 1", "text": "Absorbs 81 damage. Lasts 30 sec."},
        2050: {"name": "Lesser Heal", "rank": "Rank 1", "text": "Heal your target for 67 to 78."},
        755: {"name": "Health Funnel", "text": "Gives 12 health per second to the pet for 10 sec."},
        21562: {"name": "Fortitude", "text": "Increases Stamina by 43."},
    }
    cov = {"spells": [{"spellId": i, "status": st} for i, st in (
        (2055, "used"), (139, "used"), (689, "idle"), (17, "idle"), (2050, "idle"), (755, "none"), (8092, "used"))]}
    r = simengine.tooltip_check(spells, book, cov)
    assert (r["checked"], r["matched"]) == (6, 4), r
    assert [(o["spellId"], o["name"], o["rank"], o["model"]) for o in r["off"]] == [
        (17, "Power Word: Shield", "Rank 1", [70]), (2050, "Lesser Heal", "Rank 1", [94, 115])], r["off"]
    assert r["off"][0]["text"].startswith("Absorbs 81")
    # Without a coverage report every spell with an amount and a text is checked.
    assert simengine.tooltip_check(spells, book)["checked"] == 7
    assert simengine.tooltip_check(spells, {}) is None and simengine.tooltip_check([], book) is None


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
        os.environ["STUB_SP_CAP"] = "10"
        capped = u.compute(tmp, "Tester", prices=prices, top=5, engine="on", fight_seconds=45, target_delta=3)
    finally:
        icons.table = real_table
        os.environ.pop("STUB_SP_CAP", None)
        os.environ.pop("SIMFOREVER_BIN", None)
        if real_env:
            os.environ["SIMFOREVER_BIN"] = real_env

    by_slot = {s["slot"]: {c["name"]: c for c in s["candidates"]} for s in r["slots"]}
    assert by_slot[16]["New Staff"]["gain"] == 20  # 2 x (40 - 30) spell power, from the stub
    assert by_slot[11]["Ring D"]["gain"] == 20  # still against the weaker ring (2 x (12 - 2))
    assert r["setValue"] == 100 and r["engine"]["metric"] == "dps" and r["engine"]["ttoomSeconds"] == 42
    # The fight as the engine evaluated it (automatic kills resolved), not as it was asked.
    assert r["engine"]["spellbookSource"] == "assumed"
    assert r["engine"]["fight"] == {"seconds": 45.0, "targetDelta": 3, "killsPerMinute": 0}
    assert [row["label"] for row in r["scale"]] == ["Shadow Bolt (Rank 3)", "Drain Life (Rank 2)", "Life Tap",
                                                    "Power Word: Shield", "Dark Sacrifice",
                                                    "Curse of the Elements", "Wand"]
    assert "returns 4.5 health per second" in r["scale"][1]["note"] and r["engine"]["selfHealPs"] == 4.5
    assert "3.0 casts per minute" in r["scale"][2]["note"]
    assert "7.0 mana per second for 6.0 health per second" in r["scale"][2]["note"]
    assert "taken off the healing" not in r["scale"][2]["note"]  # a damage metric has a health budget instead
    assert "a shield" in r["scale"][3]["note"] and "9.2 mana per second" in r["scale"][4]["note"]
    # A debuff has no value of its own; its note says what it does to the others.
    assert r["scale"][5]["weight"] == 0 and "the other spells deal 10% more under it" in r["scale"][5]["note"]
    assert r["engine"]["killBuff"] is True  # a talent that needs kills: the window offers the option

    # The coverage report: every spell and talent with what the model does with it, the used ones first.
    cov = r["engine"]["coverage"]
    assert cov["candidates"] == 4 and cov["exactTalents"] is True
    assert [(e["label"], e["status"]) for e in cov["spells"]] == [
        ("Shadow Bolt (Rank 3)", "used"), ("Immolate", "idle"), ("Dark Sacrifice", "idle"), ("Drain Life", "idle"),
        ("Searing Pain (Rank 2)", "unmodeled")]
    assert cov["spells"][3]["text"] == "tried next to the rotation above: this fight does not need it"
    assert cov["spells"][2]["text"] == "trades health for mana; the mana lasts without it in this fight"
    assert cov["spells"][0]["text"] == "cast in the rotation" and cov["spells"][0]["statusLabel"] == "used"
    assert cov["spells"][1]["text"] == "tried instead: 97.5 DPS, less than the rotation above"
    assert [(e["label"], e["status"]) for e in cov["talents"]] == [
        ("Pandemic (3)", "used"), ("Spirit Tap (2)", "used"), ("Suppression (5/5)", "sheet"),
        ("Mystery (1)", "unmodeled")]  # names: snapshot first
    assert cov["talents"][0]["unverified"] and cov["talents"][0]["note"].endswith("measured in the game. Classic value.")
    assert cov["talents"][1]["text"] == "active after your killing blows: 10% of the fight with the kills it assumes"
    assert "unverified" not in cov["talents"][2] and cov["talents"][2]["statusLabel"] == "in your stats"
    assert cov["talents"][3]["text"] == "an effect the model does not know"  # a reason code this version does not know
    # The model's numbers next to the game's spell text of the same rank.
    assert r["engine"]["tooltipCheck"] == dict(
        checked=1, matched=1, off=[],
        text="Checked against the spell texts of the game: 1 of 1 spell ranks agree.")
    assert r["package"]["gain"] == round(sum(p["gain"] for p in r["package"]["items"]), 1)
    assert r["package"]["gainPct"] == round(r["package"]["gain"] / 100 * 100)
    # Gains that do not add up: once the first purchases reach the cap, the others add nothing on top and
    # leave the package, and the cost is that of what stays.
    full, less = r["package"], capped["package"]
    assert less["gain"] == 20 and 0 < len(less["items"]) < len(full["items"]), (less["gain"], len(less["items"]))
    assert less["cost"] == sum(p["price"] for p in less["items"]) < full["cost"]

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
