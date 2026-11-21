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


# ---------- consumables in a fight ----------

# A second small world for the fight: every kind, two that share a slot, a rune the game files as a trade
# good, and a ration of subclass "other" that is only eaten.
FIGHT_ITEMS = {  # item id: (class, subclass, spell, cooldown category of the item effect, ItemSparse row)
    60: (0, 3, 2060, 2593, sparse(60, "Flask of Supreme Power", 50)),
    61: (0, 2, 2061, 2593, sparse(61, "Greater Arcane Elixir", 47)),
    62: (0, 2, 2062, 2593, sparse(62, "Arcane Elixir", 37)),
    63: (0, 2, 2063, 2593, sparse(63, "Elixir of Shadow Power", 40)),
    64: (0, 4, 2064, 0, sparse(64, "Scroll of Intellect IV", 50)),
    65: (0, 1, 2065, 0, sparse(65, "Potion of Spellblasting", 45)),  # its category is on the spell
    66: (0, 1, 2066, 4, sparse(66, "Mana Potion", 22)),
    67: (0, 5, 2067, 11, sparse(67, "Nightfin Soup", 35)),
    68: (0, 5, 2067, 11, sparse(68, "Baked Salmon", 35)),
    69: (0, 8, 2069, 0, sparse(69, "Wizard Oil", 40)),
    70: (0, 8, 2070, 11, sparse(70, "Field Ration", 25)),
    71: (0, 2, 2071, 2593, sparse(71, "Elixir of Fortitude", 25)),
    72: (0, 8, 2072, 103, sparse(72, "Cerebral Cortex Compound", 0)),
    73: (0, 8, 2073, 0, sparse(73, "Rune of Meditation", 0)),  # a buff without an end: no consumable
    20520: (7, 11, 2080, 1153, sparse(20520, "Dark Rune", 0)),
    999: (7, 11, 2080, 1153, sparse(999, "Other Trade Good", 0)),  # class 7 and not on the list
}
FIGHT_EFFECTS = [
    effect(2060, 6, aura=13, bp=150, misc=126),
    effect(2061, 6, aura=13, bp=35, misc=126),
    effect(2062, 6, aura=13, bp=20, misc=126),
    effect(2063, 6, aura=13, bp=40, misc=32),                  # shadow only
    effect(2064, 6, aura=29, bp=16, misc=3),                   # intellect
    effect(2065, 6, aura=13, bp=30, misc=126),                 # for 30 seconds
    effect(2066, 30, bp=520, var=0.25),
    effect(2067, 6, aura=84, bp=100),
    effect(2067, 6, aura=227, bp=22, trigger=2068, period=10000),
    effect(2068, 6, aura=13, bp=1, misc=126),
    effect(2069, 54, bp=1800, misc=2629),
    effect(2079, 6, aura=13, bp=24, misc=126),
    effect(2079, 6, aura=135, bp=24),
    effect(2070, 6, aura=84, bp=179), effect(2070, 6, aura=85, bp=367),  # a ration: health and mana while eating
    effect(2071, 6, aura=34, bp=120),                          # +120 maximum health
    effect(2072, 6, aura=29, bp=25, misc=3),
    effect(2073, 6, aura=85, bp=5),
    effect(2080, 30, bp=1200, var=0.5), effect(2080, 2, bp=800, var=0.5),  # mana for health
]
FIGHT_DURATIONS = {2060: 6, 2061: 6, 2062: 6, 2063: 6, 2064: 6, 2065: 2, 2067: 2, 2068: 6, 2070: 2, 2071: 6, 2072: 6}
FIGHT_TABLES = {
    "Item": [{"ID": str(i), "ClassID": str(cls), "SubclassID": str(sub)} for i, (cls, sub, *_) in FIGHT_ITEMS.items()],
    "ItemSparse": [row for *_, row in FIGHT_ITEMS.values()],
    "ItemEffect": [{"ID": str(i), "SpellID": str(spell), "SpellCategoryID": str(category)}
                   for i, (_, _, spell, category, _) in FIGHT_ITEMS.items()],
    "ItemXItemEffect": [{"ItemID": str(i), "ItemEffectID": str(i)} for i in FIGHT_ITEMS],
    "SpellCategories": [{"SpellID": "2065", "Category": "4"}],
    "SpellEffect": FIGHT_EFFECTS,
    "SpellDuration": TABLES["SpellDuration"],
    "SpellMisc": [{"SpellID": str(s), "DurationIndex": str(d)} for s, d in FIGHT_DURATIONS.items()],
    "SpellItemEnchantment": [{"ID": "2629", "Duration": "1800", "Effect_0": "3", "Effect_1": "0", "Effect_2": "0",
                              "EffectPointsMin_0": "0", "EffectPointsMin_1": "0", "EffectPointsMin_2": "0",
                              "EffectArg_0": "2079", "EffectArg_1": "0", "EffectArg_2": "0"}],
}
FIGHT_PRICES = {str(i): {"m": m, "a": {"10": 4}} for i, m in
                {60: 180000, 61: 11300, 62: 2000, 63: 17200, 64: 1200, 65: 6000, 66: 150, 67: 1000, 68: 1300,
                 71: 400, 72: 500, 20520: 21200}.items()}
FIGHT_RECIPES = {"lines": {"333": {"1": {"output": 69}}}}  # the oil: made by enchanters, none in the AH


def fight_catalog():
    real = icons.table
    icons.table = lambda name, build, locale=None: FIGHT_TABLES.get(name, [])
    try:
        return c.build_catalog("1.0.0.1", "enUS")
    finally:
        icons.table = real


class FakeEngine:
    """Half a point per point of spell damage, and mana that is worth something only until it lasts: at most
    8 points from mana per 5 seconds and intellect together."""
    version = "test"

    def __init__(self, metric):
        self.metric, self.baseline, self.fight, self.calls = metric, {}, {"seconds": 60.0, "targetDelta": 2}, []

    def batch(self, evals, reoptimize=False):
        self.baseline = {"value": 100.0}
        self.calls.append((len(evals), reoptimize))
        return {k: 0.5 * (d.get("spDamage", 0) + d.get("spShadow", 0))
                + min(8.0, 0.08 * d.get("mp5", 0) + 0.2 * d.get("int", 0)) for k, d in evals}


def fight_char(level=60, bags=(), buffs=()):
    ch = char(level=level, bags=bags)
    ch["character"].update(stats={"hpPerStamina": 10}, buffs=[{"id": i, "name": f"Buff {i}"} for i in buffs])
    return ch


def test_fight_catalog():
    items = fight_catalog()["items"]
    assert "999" not in items and items["20520"]["subclass"] == c.OTHER, "only the listed trade goods come in"
    rune = items["20520"]
    assert rune["restore"] == [{"kind": "mana", "amount": 1200, "var": 0.5}] and rune["healthCost"] == 800
    assert rune["cat"] == 1153 and items["65"]["cat"] == 4 and items["66"]["cat"] == 4 and items["72"]["cat"] == 103
    ration = items["70"]
    assert ration["buffs"] == [] and [r["kind"] for r in ration["restore"]] == ["food", "drink"], \
        "eating by its cooldown category, whatever the subclass says"
    assert items["67"]["auras"] == [2068] and items["61"]["auras"] == [2061]
    kinds = {name: c.fight_kind(items[str(i)]) for name, i in
             {"flask": 60, "elixir": 61, "scroll": 64, "potion": 65, "mana": 66, "food": 67, "weapon": 69,
              "ration": 70, "blasted": 72, "forever": 73, "rune": 20520}.items()}
    assert kinds == {"flask": "flask", "elixir": "elixir", "scroll": "scroll", "potion": "potion", "mana": "potion",
                     "food": "food", "weapon": "weapon", "ration": None, "blasted": "other", "forever": None,
                     "rune": "rune"}, kinds
    assert c.fight_slots(61, items["61"], "elixir") == c.fight_slots(62, items["62"], "elixir") == ["elixir:sp"]
    assert c.fight_slots(63, items["63"], "elixir") == ["elixir:sp_shadow"]
    assert c.fight_slots(72, items["72"], "other") == ["cat:103"]
    print("ok test_fight_catalog")


def test_fight_delta():
    items = fight_catalog()["items"]
    assert c.fight_delta(items["60"], "flask", 60) == {"spDamage": 150}
    assert c.fight_delta(items["63"], "elixir", 60) == {"spShadow": 40}
    assert c.fight_delta(items["69"], "weapon", 60) == {"spDamage": 24, "healing": 24}
    assert c.fight_delta(items["71"], "elixir", 60, hp_per_stamina=12) == {"sta": 10}, "health as stamina"
    # A potion: once per two minutes. 30 s of buff in a fight of 60 s are half of it; a second one fits into
    # 150 s, and its buff is cut off by the end of the fight after 30 of the 150 seconds.
    assert c.uses_in(60) == 1 and c.uses_in(120) == 1 and c.uses_in(150) == 2
    assert c.fight_delta(items["65"], "potion", 60) == {"spDamage": 15}
    assert c.fight_delta(items["65"], "potion", 150) == {"spDamage": 12}
    assert c.fight_delta(items["66"], "potion", 60) == {"mp5": 43.333}
    # The rune's mana and what it costs, as mana and health per 5 seconds over the fight.
    assert c.fight_delta(items["20520"], "rune", 60) == {"mp5": 100, "hp5": -66.667}
    print("ok test_fight_delta")


def plan_of(ch, top=5, prices=FIGHT_PRICES, metric="dps"):
    engines = []

    def start(metric, seconds=None, buffs=None):
        engines.append(FakeEngine(metric))
        return engines[-1]

    r = c.evaluate(ch, fight_catalog(), FIGHT_RECIPES, prices, {}, top, start=start, seconds=60, metrics=[metric])
    return r["plans"][0], engines


def test_best_set():
    plan, engines = plan_of(fight_char(bags=[(68, 3)]))
    names = [(x["name"], x["adds"]) for x in plan["chosen"]]
    # Biggest first; the salmon in the bags beats the soup that does the same; the rune's mana fills what mana
    # is worth here, so the mana potion and the scroll add nothing after it and stay out.
    assert names == [("Flask of Supreme Power", 75.0), ("Elixir of Shadow Power", 20.0),
                     ("Greater Arcane Elixir", 17.5), ("Baked Salmon", 11.0), ("Dark Rune", 8.0),
                     ("Potion of Spellblasting", 7.5)], names
    assert plan["base"] == 100.0 and plan["value"] == 239.0 and plan["gain"] == 139.0 and plan["gainPct"] == 139.0
    assert plan["delta"] == {"spDamage": 222, "spShadow": 40, "mp5": 100, "hp5": -66.667}
    assert plan["cost"] == 180000 + 17200 + 11300 + 21200 + 6000 and plan["unpriced"] == 0, "the salmon is owned"
    assert plan["unit"] == "DPS" and plan["title"] == "Damage" and plan["seconds"] == 60.0
    assert all(reoptimize for _, reoptimize in engines[0].calls), "the best rotation again for every set"
    kinds = {k["key"]: k for k in plan["kinds"]}
    assert list(kinds) == ["flask", "elixir", "food", "weapon", "scroll", "potion", "rune", "other"]
    elixirs = {x["name"]: x for x in kinds["elixir"]["items"]}
    assert elixirs["Arcane Elixir"]["blockedBy"] == "Greater Arcane Elixir" and elixirs["Arcane Elixir"]["gain"] == 10.0
    assert "blockedBy" not in elixirs["Elixir of Shadow Power"] and elixirs["Elixir of Fortitude"]["gain"] == 0.0
    assert kinds["elixir"]["assumed"] and not kinds["flask"]["assumed"]
    soup = next(x for x in kinds["food"]["items"] if x["name"] == "Nightfin Soup")
    assert soup["blockedBy"] == "Baked Salmon" and soup["gain"] == 11.0
    oil = kinds["weapon"]["items"][0]
    assert oil["gain"] == 12.0 and not oil["available"] and "chosen" not in oil, "nobody sells the oil"
    potions = {x["name"]: x for x in kinds["potion"]["items"]}
    assert potions["Mana Potion"]["gain"] == 3.47 and potions["Mana Potion"]["blockedBy"] == "Potion of Spellblasting"
    assert potions["Mana Potion"]["uses"] == 1
    scroll = kinds["scroll"]["items"][0]
    assert scroll["gain"] == 3.2 and "chosen" not in scroll and "blockedBy" not in scroll
    assert kinds["rune"]["items"][0]["effect"] == "restores 900–1500 mana, costs about 800 health"
    assert kinds["other"]["items"][0]["slots"] == ["cat:103"]
    print("ok test_best_set")


def test_level_active_buff_and_top():
    # Below level 50 there is no flask. Two buffs were on the character at the snapshot, so the sheet has
    # them. Well Fed (2068) comes from both meals: it holds the food slot without saying which meal it was,
    # and the rows keep what they would add alone. The shadow elixir's buff (2063) comes from that elixir
    # only: the row is "active" and adds nothing.
    plan, _ = plan_of(fight_char(level=47, buffs=[2068, 2063]), top=1)
    names = [x["name"] for x in plan["chosen"]]
    assert "Flask of Supreme Power" not in names and not any(k["key"] == "flask" for k in plan["kinds"])
    assert "Elixir of Shadow Power" not in names and not {"Nightfin Soup", "Baked Salmon"} & set(names)
    food = next(k for k in plan["kinds"] if k["key"] == "food")
    assert [(x["name"], x["active"], x["onYou"], x["gain"]) for x in food["items"]] == [
        ("Nightfin Soup", False, "Buff 2068", 11.0)] and food["more"] == 1
    assert plan["active"] == ["Buff 2063", "Buff 2068"]
    # top = 1 keeps the first row of a kind, whatever is chosen and what is on the character.
    elixirs = next(k for k in plan["kinds"] if k["key"] == "elixir")
    assert [(x["name"], x["active"]) for x in elixirs["items"]] == [("Greater Arcane Elixir", False),
                                                                    ("Elixir of Shadow Power", True)]
    shadow = elixirs["items"][1]
    assert shadow["delta"] == {} and shadow["gain"] == 0.0 and shadow["onYou"] == "Buff 2063"
    assert elixirs["more"] == 2
    # Without an engine the page has no plans.
    assert "plans" not in c.evaluate(fight_char(), fight_catalog(), FIGHT_RECIPES, FIGHT_PRICES, {})
    print("ok test_level_active_buff_and_top")


def test_health_cost():
    """A rune takes 800 health. With 1500 health that is more than half: never in the set, and the intellect
    of the compound and the scroll gets the mana's worth instead. A healer who can afford it pays with
    healing: 800 health in 60 s are 13.3 a second, more than the 8 its mana adds here."""
    ch = fight_char()
    ch["character"]["stats"]["healthMax"] = 1500
    plan, _ = plan_of(ch)
    rune = next(k for k in plan["kinds"] if k["key"] == "rune")["items"][0]
    assert rune["tooCostly"] and rune["gain"] == 8.0 and "chosen" not in rune and rune["healthPerSec"] == 13.33
    assert "Dark Rune" not in [x["name"] for x in plan["chosen"]]
    assert [(x["name"], x["adds"]) for x in plan["chosen"]][-2:] == [("Cerebral Cortex Compound", 5.0),
                                                                     ("Scroll of Intellect IV", 3.0)]
    assert plan["delta"] == {"spDamage": 222, "spShadow": 40, "int": 41}

    ch["character"]["stats"]["healthMax"] = 4000
    plan, _ = plan_of(ch, metric="hps")
    rune = next(k for k in plan["kinds"] if k["key"] == "rune")["items"][0]
    assert not rune["tooCostly"] and rune["gain"] == -5.33 and "chosen" not in rune
    assert plan["unit"] == "HPS" and plan["title"] == "Healing"
    plan, _ = plan_of(ch)  # a damage dealer has the cost in the delta instead
    assert next(k for k in plan["kinds"] if k["key"] == "rune")["items"][0]["chosen"] == 5
    print("ok test_health_cost")


def test_german_texts():
    i18n.set_language("de")
    try:
        r = c.evaluate(char(level=40), catalog(), RECIPES, PRICES, {})
        assert group(r, "healing")["label"] == "Heiltränke"
        assert group(r, "healing")["items"][0]["effect"] == "stellt 455–585 Gesundheit wieder her"
        assert group(r, "food")["items"][0]["effect"] == "600 Gesundheit in 30 Sek."
        assert group(r, "sp")["items"][0]["duration"] == "1 Std."
        plan, _ = plan_of(fight_char())
        assert plan["title"] == "Schaden" and plan["kinds"][0]["label"] == "Fläschchen"
        assert plan["kinds"][1]["assumed"].startswith("Ob sich zwei Elixiere")
        rune = next(k for k in plan["kinds"] if k["key"] == "rune")["items"][0]
        assert rune["effect"] == "stellt 900–1500 Mana wieder her, kostet etwa 800 Gesundheit"
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
    test_fight_catalog()
    test_fight_delta()
    test_best_set()
    test_level_active_buff_and_top()
    test_health_cost()
    test_german_texts()
