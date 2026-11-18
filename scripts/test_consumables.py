# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Tests of the consumables page without the game and without network: uv run scripts/test_consumables.py"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import consumables as c  # noqa: E402
import i18n  # noqa: E402
import icons  # noqa: E402


def effect(spell, kind, aura=0, bp=0, misc=0, trigger=0, period=0, var=0.0):
    return {"ID": "0", "SpellID": str(spell), "Effect": str(kind), "EffectAura": str(aura), "EffectBasePointsF": str(bp),
            "Variance": str(var), "EffectMiscValue_0": str(misc), "EffectTriggerSpell": str(trigger),
            "EffectAuraPeriod": str(period)}


def sparse(iid, name, level=1, buy=0, stack=1, skill=0, rank=0, flags=0, classes=-1):
    return {"ID": str(iid), "Display_lang": name, "OverallQualityID": "1", "RequiredLevel": str(level),
            "RequiredSkill": str(skill), "RequiredSkillRank": str(rank), "AllowableClass": str(classes),
            "BuyPrice": str(buy), "VendorStackCount": str(stack), "SellPrice": "1", "Flags_0": str(flags)}


# A small world in the DB2 format: item -> item effect -> spell -> effects.
ITEMS = {  # item id: (subclass, spell, ItemSparse row)
    10: (1, 1010, sparse(10, "Healing Potion", 12)),
    11: (1, 1011, sparse(11, "Greater Healing Potion", 21)),
    12: (1, 1012, sparse(12, "Mana Potion", 22)),
    20: (5, 1020, sparse(20, "Haunch of Meat", 25, buy=1000, stack=5)),  # VENDOR_FOOD id 2287 below
    21: (5, 1021, sparse(21, "Nightfin Soup", 35)),
    30: (7, 1030, sparse(30, "Heavy Mageweave Bandage", 0, skill=2942, rank=175)),
    40: (2, 1040, sparse(40, "Arcane Elixir", 37)),
    41: (8, 1041, sparse(41, "Brilliant Wizard Oil", 45)),
    42: (2, 1042, sparse(42, "Elixir of Giants", 46)),
    50: (2, 1040, sparse(50, "Deprecated Arcane Elixir", 1)),  # skipped by name
    51: (1, 1010, sparse(51, "Conjured Potion", 1, flags=2)),  # conjured: skipped
    52: (1, 1010, sparse(52, "Mage Potion", 1, classes=128)),  # only mages
}
ITEMS[2287] = (5, 1020, sparse(2287, "Haunch of Meat", 25, buy=1000, stack=5))
del ITEMS[20]
EFFECTS = [
    effect(1010, 10, bp=320, var=0.25),                        # HEAL 280-360
    effect(1011, 10, bp=520, var=0.25),
    effect(1012, 30, bp=520, var=0.25),                        # ENERGIZE mana
    effect(1020, 6, aura=84, bp=100),                          # eating: 100 per 5 s for 30 s
    effect(1021, 64, trigger=1020),                            # "Nutritious Food": eat ...
    effect(1021, 6, aura=227, bp=22, trigger=1022, period=10000),  # ... and become Well Fed with $s2 = 22
    effect(1022, 6, aura=13, bp=1, misc=126),
    effect(1030, 6, aura=8, bp=138, period=1000),              # 138 per second for 8 s
    effect(1040, 6, aura=13, bp=20, misc=126),                 # +20 spell power
    effect(1041, 54, bp=1800, misc=2628),                      # temporary enchantment 2628
    effect(1043, 6, aura=13, bp=36, misc=126),
    effect(1043, 6, aura=57, bp=1),
    effect(1042, 6, aura=29, bp=25, misc=0),                   # +25 strength
]
DURATIONS = {1020: 2, 1022: 6, 1030: 7, 1040: 6}  # SpellMisc.DurationIndex
TABLES = {
    "Item": [{"ID": str(i), "ClassID": "0", "SubclassID": str(sub)} for i, (sub, _, _) in ITEMS.items()]
    + [{"ID": "99", "ClassID": "4", "SubclassID": "1"}],
    "ItemSparse": [row for _, _, row in ITEMS.values()],
    "ItemEffect": [{"ID": str(i), "SpellID": str(spell)} for i, (_, spell, _) in ITEMS.items()],
    "ItemXItemEffect": [{"ItemID": str(i), "ItemEffectID": str(i)} for i in ITEMS],
    "SpellEffect": EFFECTS,
    "SpellDuration": [{"ID": "2", "Duration": "30000"}, {"ID": "6", "Duration": "3600000"}, {"ID": "7", "Duration": "8000"}],
    "SpellMisc": [{"SpellID": str(s), "DurationIndex": str(d)} for s, d in DURATIONS.items()],
    "SpellItemEnchantment": [{"ID": "2628", "Duration": "1800", "Effect_0": "3", "Effect_1": "0", "Effect_2": "0",
                              "EffectPointsMin_0": "0", "EffectPointsMin_1": "0", "EffectPointsMin_2": "0",
                              "EffectArg_0": "1043", "EffectArg_1": "0", "EffectArg_2": "0"}],
}


def catalog():
    real = icons.table
    icons.table = lambda name, build, locale=None: TABLES.get(name, [])
    try:
        return c.build_catalog("1.0.0.1", "enUS")
    finally:
        icons.table = real


def char(level=30, class_file="WARLOCK", first_aid=0, bags=(), learned=()):
    professions = [{"skillLine": 129, "name": "First Aid", "rank": first_aid}] if first_aid else []
    return {
        "character": {"name": "Test", "level": level, "classFile": class_file, "class": "Warlock"},
        "skills": {"professions": professions},
        "bags": [{"items": [{"id": i, "count": n} for i, n in bags]}],
        "recipes": {"171": {"recipes": {str(900 + n): {"learned": True, "output": i} for n, i in enumerate(learned)}}},
    }


PRICES = {  # Auctionator: m = lowest price, a = auctions per scan day
    "10": {"m": 80, "a": {"10": 20}},
    "11": {"m": 500, "a": {"10": 5}},
    "40": {"m": 1600, "a": {"10": 3}},
    "42": {"m": 2000, "a": {"10": 1}},
    "2287": {"m": 900, "a": {"10": 3}},  # more expensive than the vendor (200 each)
}
RECIPES = {"lines": {"171": {"1": {"output": 11}, "2": {"output": 12}}, "185": {"3": {"output": 21}},
                     "333": {"4": {"output": 41}}}}  # an item nobody sells nor makes is not listed


def test_catalog_decodes_effects():
    items = catalog()["items"]
    assert "50" not in items and "51" not in items, "deprecated and conjured items are skipped"
    assert items["10"]["restore"] == [{"kind": "healing", "amount": 320, "var": 0.25}]
    assert items["12"]["restore"][0]["kind"] == "mana"
    assert items["2287"]["restore"] == [{"kind": "food", "amount": 600, "seconds": 30}]
    soup = items["21"]
    assert soup["restore"][0]["kind"] == "food" and soup["wellFed"]
    assert soup["buffs"] == [{"stat": "sp", "amount": 22}] and soup["duration"] == 3600
    assert items["30"]["restore"] == [{"kind": "bandage", "amount": 1104, "seconds": 8}]
    assert items["30"]["reqSkill"] == 129, "Forever's bandage skill line counts as First Aid"
    oil = items["41"]
    assert oil["oil"] and oil["duration"] == 1800
    assert oil["buffs"] == [{"stat": "sp", "amount": 36}, {"stat": "spell_crit", "amount": 1}]
    assert items["42"]["buffs"] == [{"stat": "strength", "amount": 25}]
    print("ok test_catalog_decodes_effects")


def group(result, key):
    return next(g for g in result["categories"] + result["buffs"] if g["key"] == key)


def test_best_for_level_with_prices_bags_and_recipes():
    cat = catalog()
    r = c.evaluate(char(level=21), cat, RECIPES, PRICES, {})
    heal = group(r, "healing")
    assert [x["id"] for x in heal["items"]] == [11, 10], "strongest first, the mage potion is not for warlocks"
    assert heal["items"][0]["best"] and heal["items"][0]["price"] == 500 and heal["items"][0]["source"] == "ah"
    assert heal["items"][0]["craft"] == {"profession": "Alchemy", "learned": False}
    assert heal["next"] is None
    mana = group(r, "mana")
    assert mana["items"] == [] and mana["next"]["id"] == 12 and mana["next"]["reqLevel"] == 22
    # Level 22: the mana potion is usable but nobody sells it; with the recipe learned it counts as available.
    r = c.evaluate(char(level=22, learned=[12]), cat, RECIPES, PRICES, {})
    potion = group(r, "mana")["items"][0]
    assert potion["available"] and potion["best"] and potion["craft"]["learned"]
    print("ok test_best_for_level_with_prices_bags_and_recipes")


def test_unavailable_stronger_item_is_listed_but_not_best():
    r = c.evaluate(char(level=40), catalog(), RECIPES, {"10": PRICES["10"]}, {})
    heal = group(r, "healing")
    assert heal["items"][0]["id"] == 11 and not heal["items"][0]["available"] and not heal["items"][0].get("best")
    assert heal["items"][1]["id"] == 10 and heal["items"][1]["best"]
    # Cut to one row: "next" still compares with the best item you can get, which is not in the list.
    r = c.evaluate(char(level=20), catalog(), RECIPES, {"10": PRICES["10"]}, {}, top=1)
    assert [x["id"] for x in group(r, "healing")["items"]] == [10]
    assert group(r, "healing")["next"]["id"] == 11
    print("ok test_unavailable_stronger_item_is_listed_but_not_best")


def test_vendor_food_bags_and_seen_vendor():
    r = c.evaluate(char(level=40, bags=[(2287, 7)]), catalog(), RECIPES, PRICES, {})
    meat = group(r, "food")["items"][0]
    assert meat["id"] == 2287 and meat["owned"] == 7
    assert meat["source"] == "vendor" and meat["price"] == 200 and meat["ahPrice"] == 900
    r = c.evaluate(char(level=40), catalog(), RECIPES, PRICES, {2287: {"price": 150, "npc": "Innkeeper"}})
    meat = group(r, "food")["items"][0]
    assert meat["price"] == 150 and meat["vendorNpc"] == "Innkeeper"
    print("ok test_vendor_food_bags_and_seen_vendor")


def test_bandage_needs_first_aid():
    cat = catalog()
    r = c.evaluate(char(level=40, bags=[(30, 5)]), cat, RECIPES, PRICES, {})
    band = group(r, "bandage")
    assert band["items"] == [] and band["next"]["reqSkill"]["rank"] == 175 and not band["next"]["reqSkill"]["ok"]
    r = c.evaluate(char(level=40, first_aid=200, bags=[(30, 5)]), cat, RECIPES, PRICES, {})
    band = group(r, "bandage")["items"][0]
    assert band["best"] and band["owned"] == 5 and band["reqSkill"]["ok"]
    print("ok test_bandage_needs_first_aid")


def test_buff_groups_follow_the_class():
    cat = catalog()
    r = c.evaluate(char(level=50, learned=[21]), cat, RECIPES, PRICES, {})
    assert "strength" not in {g["key"] for g in r["buffs"]}
    sp = group(r, "sp")
    assert [x["id"] for x in sp["items"]] == [41, 21, 40], sp["items"]
    assert sp["items"][1]["best"] and sp["items"][1]["type"] == "Buff food"
    assert sp["items"][0]["effect"] == "+36 Spell power, +1% Spell crit" and sp["items"][0]["type"] == "Weapon oil"
    r = c.evaluate(char(level=50), cat, RECIPES, PRICES, {}, all_buffs=True)
    assert not group(r, "strength")["relevant"] and group(r, "sp")["relevant"]
    r = c.evaluate(char(level=50, class_file="WARRIOR"), cat, RECIPES, PRICES, {})
    assert "sp" not in {g["key"] for g in r["buffs"]}
    print("ok test_buff_groups_follow_the_class")


def test_same_effect_merges_unless_owned():
    rows = [{"id": i, "name": f"Bread {i}", "amount": 600, "effect": "x", "available": True, "owned": 0, "craft": None,
             "price": 200 + i, "source": "vendor", "alternatives": []} for i in range(3)]
    rows[2]["owned"] = 4
    out = c.merge_alike(rows)
    assert [r["id"] for r in out] == [0, 2] and [a["id"] for a in out[0]["alternatives"]] == [1]
    print("ok test_same_effect_merges_unless_owned")


def test_german_texts():
    i18n.set_language("de")
    try:
        r = c.evaluate(char(level=40), catalog(), RECIPES, PRICES, {})
        assert group(r, "healing")["label"] == "Heiltränke"
        assert group(r, "healing")["items"][0]["effect"] == "stellt 455–585 Gesundheit wieder her"
        assert group(r, "food")["items"][0]["effect"] == "600 Gesundheit in 30 Sek."
        assert group(r, "sp")["items"][0]["duration"] == "1 Std."
    finally:
        i18n.set_language("en")
    print("ok test_german_texts")


if __name__ == "__main__":
    test_catalog_decodes_effects()
    test_best_for_level_with_prices_bags_and_recipes()
    test_unavailable_stronger_item_is_listed_but_not_best()
    test_vendor_food_bags_and_seen_vendor()
    test_bandage_needs_first_aid()
    test_buff_groups_follow_the_class()
    test_same_effect_merges_unless_owned()
    test_german_texts()
