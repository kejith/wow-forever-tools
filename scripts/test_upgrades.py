# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Checks for the scoring in upgrades.py, without the game:  uv run scripts\\test_upgrades.py"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import upgrades as u  # noqa: E402


def char(level=46, hit=5, talents=(), equipment=()):
    return {"character": {"level": level, "stats": {"spellHit": hit, "spellDamage": {"shadow": 180}}},
            "talents": {"class": [{"talents": [{"name": n, "rank": r} for n, r in talents]}]},
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
    assert u.norm_stats(rec, ranged=True)["wand_dps"] == 40
    # Without a tooltip (equipped items of old exports) the rating is converted.
    assert u.norm_stats({"stats": rec["stats"]})["crit"] == 2.0
    assert u.norm_stats({"stats": rec["stats"]}, ratings={"hit": 20, "crit": 14})["hit"] == 0.5


def test_learn_ratings():
    items = {1: {"stats": {"ITEM_MOD_HIT_RATING_SHORT": 24}, "tooltip": ["Anlegen: Verbessert Eure Trefferchance um 2.0%."]}}
    assert u.learn_ratings(items) == {"hit": 12.0, "crit": 14.0}


def test_hit_counts_only_up_to_the_cap():
    lvl = u.build_scale(char(hit=5))  # levelling: 6 % miss against +2, 1 % always stays, 5 % already there
    assert lvl["profile"] == "leveln"
    assert u.set_value({"hit": 3}, lvl) == 0

    dung = u.build_scale(char(hit=5, talents=[("Unterdrückung", 5)]), "dungeon")  # 16 % needed, 11 missing
    one, two, cap, over = (u.set_value({"hit": h}, dung) for h in (1, 2, 11, 20))
    assert one > 0 and cap == over  # nothing beyond the cap
    assert abs((two - one) - 0.25 * one) < 1e-9  # after 1 % the affliction spells are capped by Unterdrückung

    assert u.build_scale(char(level=60))["profile"] == "dungeon"


def test_crit_depends_on_talents():
    plain = u.build_scale(char())["weights"]["crit"]
    pandemic = u.build_scale(char(talents=[("Pandemie", 3)]))["weights"]["crit"]
    assert 0 < plain < pandemic


def test_special_effects():
    tip = ["Anlegen: +10 Angriffskraft.", "Anlegen: Verbessert Eure Trefferchance um 1.0%.",
           "Anlegen: Erhöht durch magische Zauber und Effekte verursachten Schaden und Heilung um bis zu 9.",
           "Anlegen: Erhöht durch sämtliche Zauber und magische Effekte verursachte Heilung um bis zu 7 und den verursachten Schaden um bis zu 3.",
           "Anlegen: Stellt alle 5 Sek. 6 Mana wieder her.", "Trefferchance: Heilt 12 Sek. lang 78 Schaden.",
           "Anlegen: Erhöht den durch magische Zauber und Effekte verursachten Schaden gegen Wildtiere um bis zu 26.",
           "Benutzen: Entfernt alle Unbeweglichkeitseffekte."]
    assert u.special_effects(tip) == [tip[6], tip[7]]


def test_best_package():
    a, b = [(30, 10.0, ("a1",)), (50, 14.0, ("a2",))], [(40, 9.0, ("b1",)), (90, 30.0, ("b2",))]
    assert u.best_package([a, b], 0) == (0, 0.0, ())
    assert u.best_package([a, b], 70) == (70, 19.0, ("a1", "b1"))
    assert u.best_package([a, b], 100) == (90, 30.0, ("b2",))  # one expensive item beats two cheap ones
    assert u.best_package([a, b], 1000) == (140, 44.0, ("a2", "b2"))


def test_compute_weapons_and_package(tmp: str):
    """End to end against a temporary WoW folder: a staff is compared with main hand + off hand."""
    wtf = os.path.join(tmp, "WTF", "Account", "TEST")
    char_dir = os.path.join(wtf, "93", "Tester - Realm", "SavedVariables")
    os.makedirs(char_dir)
    os.makedirs(os.path.join(wtf, "SavedVariables"))

    def item(name, loc, sp, cls=4, sub=1):
        return (f'{{ name = "{name}", quality = 2, reqLevel = 40, equipLoc = "{loc}", classID = {cls}, '
                f'subclassID = {sub}, stats = {{ ITEM_MOD_SPELL_POWER_SHORT = {sp} }}, tooltip = {{}} }}')

    with open(os.path.join(char_dir, "WoWMCP.lua"), "w", encoding="utf-8") as f:
        f.write('WoWMCP_Char = { character = { name = "Tester", level = 46, money = 1000000, '
                'stats = { spellHit = 5, spellDamage = { shadow = 100 } } }, equipment = {'
                '{ slot = 16, name = "Alter Stab", equipLoc = "INVTYPE_2HWEAPON", stats = { ITEM_MOD_SPELL_POWER_SHORT = 30 } },'
                '{ slot = 11, name = "Ring A", equipLoc = "INVTYPE_FINGER", stats = { ITEM_MOD_SPELL_POWER_SHORT = 2 } },'
                '{ slot = 12, name = "Ring B", equipLoc = "INVTYPE_FINGER", stats = { ITEM_MOD_SPELL_POWER_SHORT = 8 } },'
                '} }\n')
    with open(os.path.join(wtf, "SavedVariables", "WoWMCP.lua"), "w", encoding="utf-8") as f:
        f.write("WoWMCP_Items = { updated = 1, items = {\n"
                f'[101] = {item("Dolch", "INVTYPE_WEAPONMAINHAND", 25, 2, 15)},\n'
                f'[102] = {item("Kugel", "INVTYPE_HOLDABLE", 20, 4, 0)},\n'
                f'[103] = {item("Neuer Stab", "INVTYPE_2HWEAPON", 40, 2, 10)},\n'
                f'[104] = {item("Ring C", "INVTYPE_FINGER", 10, 4, 0)},\n'
                f'[105] = {item("Ring D", "INVTYPE_FINGER", 12, 4, 0)},\n'
                f'[106] = {item("Axt", "INVTYPE_2HWEAPON", 99, 2, 1)},\n'
                "} }\n")
    prices = {str(i): {"m": 10000, "a": {"100": 1}} for i in range(101, 107)}
    r = u.compute(tmp, "Tester", prices=prices, top=5)
    by_slot = {s["slot"]: {c["name"]: c for c in s["candidates"]} for s in r["slots"]}

    assert by_slot[16]["Neuer Stab"]["gain"] == 10 and by_slot[16]["Neuer Stab"]["gainPct"] == 33
    assert "Dolch" not in by_slot[16] and 17 not in by_slot  # each alone is worse than the staff
    assert "Axt" not in by_slot[16]  # not a warlock weapon
    assert by_slot[11]["Ring D"]["gain"] == 10  # against the weaker ring
    names = {p["name"]: p for p in r["package"]["items"]}
    assert set(names) == {"Dolch", "Kugel", "Ring C", "Ring D"}  # the pair beats the new staff, both rings go
    assert r["package"]["gain"] == 15 + 12 and r["package"]["cost"] == 40000
    assert names["Dolch"]["replaces"] == ["Alter Stab"] and names["Kugel"]["replaces"] == []

    r = u.compute(tmp, "Tester", prices=prices, max_preis=1)  # one gold: the best single purchase
    assert [p["name"] for p in r["package"]["items"]] in (["Neuer Stab"], ["Ring D"])
    assert r["package"]["gain"] == 10


if __name__ == "__main__":
    import tempfile

    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            if fn.__code__.co_argcount:
                with tempfile.TemporaryDirectory() as d:
                    fn(d)
            else:
                fn()
            print("ok", name)
