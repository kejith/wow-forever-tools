# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Tests of the data the addon shows in the game, without the game and without network: uv run scripts/test_ingame.py

The four calculations are replaced by hand-made results, so only the lists, tooltip lines, the Lua file and
the rules for writing it are tested here."""

import os
import sys
import tempfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import i18n  # noqa: E402
import ingame as g  # noqa: E402

UPGRADES = {
    "budget": 150000, "profileLabel": "Leveling",
    "package": {"gain": 12.5, "cost": 90000, "items": [
        {"id": 9001, "name": "Robe of Tests", "quality": 2, "slotName": "Chest", "gain": 12.5, "price": 90000}]},
    "slots": [{"slotName": "Chest", "current": {"name": "Old Robe"}, "candidates": [
        {"id": 9001, "name": "Robe of Tests", "quality": 2, "gain": 12.5, "price": 90000},
        {"id": 9002, "name": "Robe | of Pipes", "quality": 3, "gain": 4.0, "price": 500000, "soon": True, "reqLevel": 50}]},
        {"slotName": "Head", "current": None, "candidates": [
            {"id": 9003, "name": "Hat", "quality": 2, "gain": 1.0, "price": 100}]}],
    "review": [{"id": 9004, "name": "Odd Trinket", "quality": 3, "slot": "Trinket", "price": 7000}],
}

WRITS = {
    "reputation": {"name": "Durotar Supply and Logistics", "standing": "Friendly", "value": 100, "max": 6000},
    "favor": 25,
    "tiers": [{"name": "Journeyman", "best": [
        {"writId": 264012, "itemId": 3383, "name": "Elixir of Wisdom", "qty": 4, "prof": "Alchemy",
         "wcost": 5000, "icost": 8000, "net": 9700, "per": 129.3},
        {"writId": 264013, "itemId": 3384, "name": "Owned Thing", "qty": 2, "prof": "Alchemy",
         "wcost": 0, "icost": 0, "net": -3300, "per": -44.0}]}],
    "crates": {
        "best": [{"crateId": 248682, "name": "Herb Crate", "crateCost": 1000, "per": 500, "haveCrate": False,
                  "best": {"id": 2447, "name": "Peacebloom", "qty": 50, "cost": 1500}}],
        "tiers": [{"name": "Apprentice", "favor": 5, "level": 10, "tooLow": False, "rows": [
            {"crateId": 248682, "name": "Herb Crate", "crateCost": 1000, "per": 500, "complete": True,
             "best": {"id": 2447, "name": "Peacebloom", "qty": 50, "cost": 1500},
             "options": [{"id": 2447, "qty": 50}, {"id": 765, "qty": 40}]},
            {"crateId": 248683, "name": "Ore Crate", "complete": False, "best": None, "options": []}]}],
    },
}

RECIPES = {"professions": [{
    "name": "Alchemy",
    "learned": [
        {"name": "Mana Potion", "output": 3827, "outputName": "Mana Potion", "quality": 1, "qty": 1, "profit": 1200,
         "trend": "falling", "reagents": [
             {"id": 785, "name": "Mageroyal", "quality": 1, "count": 2, "owned": 0, "cost": 400, "source": "ah"},
             {"id": 3371, "name": "Empty Vial", "quality": 1, "count": 1, "owned": 0, "cost": 4, "source": "vendor"},
             {"id": 2450, "name": "Briarthorn", "quality": 1, "count": 1, "owned": 5, "cost": 100, "source": "ah"}]},
        {"name": "Loss Potion", "output": 3828, "outputName": "Loss Potion", "quality": 1, "qty": 1, "profit": -50,
         "reagents": [{"id": 785, "name": "Mageroyal", "count": 9, "owned": 0, "cost": 1800, "source": "ah"}]}],
    "missing": [
        {"name": "Elixir of Gain", "profit": 3000, "payback": 2.0, "better": True, "acquire": {
            "learnable": True, "best": {"id": 7001, "name": "Recipe: Elixir of Gain", "quality": 2, "price": 6000,
                                        "kind": "ah"},
            "options": [{"id": 7001}]}},
        {"name": "Elixir of Patience", "profit": 100, "payback": 400.0, "better": True, "acquire": {
            "learnable": True, "best": {"id": 7002, "name": "Recipe: Elixir of Patience", "quality": 2, "price": 40000,
                                        "kind": "ah"},
            "options": [{"id": 7002}]}},
        {"name": "Elixir of Later", "profit": 900, "payback": 5.0, "better": False, "acquire": {
            "learnable": False, "reqSkill": 250, "best": {"id": 7003, "name": "Recipe: Elixir of Later", "price": 4500,
                                                          "kind": "vendor"},
            "options": [{"id": 7003}]}}],
}]}

CONSUMABLES = {"level": 46, "categories": [{"label": "Healing potions", "items": [
    {"id": 3928, "name": "Superior Healing Potion", "quality": 1, "effect": "restores 700-900 health", "price": 2500,
     "source": "ah", "best": True, "available": True, "owned": 0},
    {"id": 1710, "name": "Greater Healing Potion", "quality": 1, "effect": "restores 455-585 health", "price": 900,
     "source": "ah", "best": False, "available": True, "owned": 3},
    {"id": 929, "name": "Unobtainable", "effect": "x", "available": False, "price": None}]}],
    "buffs": [
        {"label": "Intellect", "relevant": True, "items": [
            {"id": 9179, "name": "Elixir of Greater Intellect", "effect": "+25 intellect", "duration": "1 h",
             "price": 3000, "source": "ah", "best": True, "available": True, "owned": 2}]},
        {"label": "Strength", "relevant": False, "items": [
            {"id": 9206, "name": "Elixir of Giants", "effect": "+25 strength", "price": 1, "source": "ah",
             "best": True, "available": True}]}]}


def sections(one):
    return {s["title"]: s for s in one["sections"]}


def rows_of(one, title):
    return {r["id"]: r for r in sections(one)[title]["rows"]}


def test_upgrade_list():
    tips = {}
    one = g.upgrade_list(UPGRADES, tips)
    assert one["key"] == "upgrades" and "{m:150000}" in one["note"] and "Leveling" in one["note"]
    package = sections(one)["Best package for the budget"]
    assert package["note"] == "+12.5 points for {m:90000}"
    assert package["rows"] == [{"id": 9001, "name": "Robe of Tests", "q": 2, "info": "Chest +12.5", "price": 90000,
                                "shop": True}]
    chest = sections(one)["Chest"]
    assert chest["note"] == "now: Old Robe" and not any(r.get("shop") for r in chest["rows"])
    assert chest["rows"][1]["name"] == "Robe || of Pipes", "a | from game data must not start an escape code"
    assert chest["rows"][1]["info"] == "+4.0 · from level 50"
    assert sections(one)["Head"]["note"] == "empty"
    assert rows_of(one, "With a special effect")[9004]["info"] == "Trinket"
    assert tips[9001] == ["Upgrade for Chest: +12.5 points, no. 1 in the AH scan ({m:90000})",
                          "Part of the best package for your budget"]
    assert "no. 2" in tips[9002][0] and 9004 not in tips
    assert g.upgrade_list({"slots": [], "review": []}, {}) is None

    # With the combat engine the same lists speak of DPS instead of points.
    tips = {}
    one = g.upgrade_list(dict(UPGRADES, engine={"metric": "dps", "value": 98.7}), tips)
    assert "combat model" in one["note"] and "98.7 DPS" in one["note"]
    assert sections(one)["Best package for the budget"]["note"] == "+12.5 DPS for {m:90000}"
    assert tips[9001][0] == "Upgrade for Chest: +12.5 DPS, no. 1 in the AH scan ({m:90000})"

    # Gear the account already has: where it is instead of a price, never on the shopping list, and the
    # rank in the AH scan counts only what the auction house has.
    owned = {"id": 9005, "name": "Bank Robe", "quality": 2, "slotName": "Chest", "gain": 20.0, "price": 0,
             "source": "bank", "owner": None, "where": "in your bank"}
    with_owned = dict(UPGRADES, package={"gain": 32.5, "cost": 90000, "items": [owned] + UPGRADES["package"]["items"]},
                      slots=[dict(UPGRADES["slots"][0], candidates=[owned] + UPGRADES["slots"][0]["candidates"])])
    tips = {}
    one = g.upgrade_list(with_owned, tips)
    package = sections(one)["Best package for the budget"]
    assert package["rows"][0] == {"id": 9005, "name": "Bank Robe", "q": 2, "info": "Chest +20.0 · in your bank"}
    assert package["rows"][1]["shop"]
    assert sections(one)["Chest"]["rows"][0]["info"] == "+20.0 · in your bank"
    assert tips[9005] == ["You own this upgrade for Chest: +20.0 points (in your bank)",
                          "Part of the best package for your budget"]
    assert "no. 1 in the AH scan" in tips[9001][0]
    one = g.upgrade_list(dict(with_owned, engine={"metric": "hps", "value": 50.0}), tips := {})
    assert tips[9005][0] == "You own this upgrade for Chest: +20.0 HPS (in your bank)"

    # A healer's note says HPS and carries the downrank recommendation.
    one = g.upgrade_list(dict(UPGRADES, engine={"metric": "hps", "value": 50.0,
                                                "downrank": {"spell": "Heal (Rank 1)"}}), {})
    assert "50.0 HPS" in one["note"] and "heal with Heal (Rank 1)" in one["note"]
    print("ok test_upgrade_list")


def test_writ_list():
    tips = {}
    one = g.writ_list(WRITS, tips)
    assert one["note"] == "Durotar Supply and Logistics: Friendly 100/6000"
    rows = sections(one)["Journeyman"]["rows"]
    writ, item, owned_writ, owned_item = rows
    assert writ["shop"] and writ["price"] == 5000 and writ["info"] == "net {m:9700} · {m:129} per reputation"
    assert item == {"id": 3383, "name": "Elixir of Wisdom", "n": 4, "info": "Alchemy", "price": 8000, "shop": True,
                    "sub": True}
    # Only the cheapest writ of a tier is bought, and nothing the character already has.
    assert "shop" not in owned_writ and "shop" not in owned_item and "price" not in owned_writ
    assert owned_writ["info"].startswith("you have it · profit {m:3300}")
    assert owned_item["info"] == "you have it · Alchemy"
    assert tips[3383] == ["For a writ (Journeyman): hand in 4x, net {m:9700}"]
    assert "needs 4x Elixir of Wisdom" in tips[264012][0]
    print("ok test_writ_list")


def test_crate_list():
    tips = {}
    one = g.crate_list(WRITS, tips)
    assert one["note"] == "Merchant's Favor: 25"
    best = sections(one)["Cheapest Merchant's Favor"]["rows"]
    assert [r["id"] for r in best] == [248682, 2447] and all(r["shop"] for r in best)
    assert best[0]["info"] == "{m:500} per favor" and best[1]["n"] == 50 and best[1]["sub"]
    tier = sections(one)["Apprentice"]
    assert tier["note"] == "5 favor each" and [r["id"] for r in tier["rows"]] == [248682, 2447]
    assert not any(r.get("shop") for r in tier["rows"]), "the incomplete crate is left out, the tier is not a shopping list"
    assert tips[765] == ["Fills Herb Crate: 40x"]
    assert tips[248682] == ["Fill with 50x Peacebloom ({m:1500}): {m:500} per Merchant's Favor"]
    assert g.crate_list({"crates": {}}, {}) is None
    print("ok test_crate_list")


def test_recipe_list():
    tips, vendor = {}, {}
    one = g.recipe_list(RECIPES, tips, vendor)
    crafts = sections(one)["Alchemy: best crafts"]["rows"]
    assert [r["id"] for r in crafts] == [3827, 785, 3371, 2450], "recipes without profit are left out"
    assert crafts[0]["info"] == "profit {m:1200} · falling" and "shop" not in crafts[0]
    assert crafts[1]["shop"] and crafts[1]["n"] == 2 and crafts[1]["sub"]
    assert "shop" not in crafts[2] and crafts[2]["info"] == "vendor"
    assert "shop" not in crafts[3] and crafts[3]["info"] == "you have it"
    missing = sections(one)["Alchemy: worth getting"]["rows"]
    assert [r["id"] for r in missing] == [7001, 7002], "a recipe that cannot be learned yet is not offered"
    assert missing[0]["shop"] and missing[0]["info"] == "profit {m:3000} · paid off after 2 crafts"
    assert "shop" not in missing[1], "400 crafts until it pays is no shopping list entry"
    assert tips[3827] == ["Alchemy: profit {m:1200} per craft · falling"]
    assert tips[785] == ["Reagent for Mana Potion: profit {m:1200} per craft"], tips[785]
    assert tips[7001] == ["Teaches Elixir of Gain: profit {m:3000} per craft, paid off after 2 crafts",
                          "Pays more than your best recipe"]
    assert tips[7003][-1] == "Needs Alchemy 250"
    assert set(vendor) == {7001, 7002, 7003} and vendor[7003].startswith("Teaches Elixir of Later")
    print("ok test_recipe_list")


def test_consumable_list():
    tips = {}
    one = g.consumable_list(CONSUMABLES, tips)
    assert one["note"] == "Level 46" and set(sections(one)) == {"Healing potions", "Intellect"}
    potions = sections(one)["Healing potions"]["rows"]
    assert [r["id"] for r in potions] == [3928, 1710]
    assert potions[0]["shop"] and potions[0]["info"] == "restores 700-900 health"
    assert potions[1]["info"].startswith("3 in bags · ") and "shop" not in potions[1]
    elixir = sections(one)["Intellect"]["rows"][0]
    assert elixir["info"] == "2 in bags · +25 intellect (1 h)" and "shop" not in elixir, "owned: nothing to buy"
    assert tips[3928] == ["Best now (Healing potions): restores 700-900 health"] and 9206 not in tips
    print("ok test_consumable_list")


def test_tip_limit_and_duplicates():
    tips = {}
    for i in range(10):
        g.add_tip(tips, "5", f"line {i % 6}")
    assert tips == {5: ["line 0", "line 1", "line 2", "line 3"]}
    print("ok test_tip_limit_and_duplicates")


def lua_load(text):
    from lupa.lua54 import LuaRuntime

    lua = LuaRuntime(register_eval=False, register_builtins=False)
    lua.execute(text)
    return lua


def test_lua_round_trip():
    nasty = 'Günthers "Trank" \\ ]] --[[ \n|cffff0000 ' + " \x00"
    payload = {"version": 1, "generated": 5, "hash": "abc", "none": None, "flag": True, "ratio": 1.5, "whole": 2.0,
               "chars": {"Günther": {"lists": [{"title": nasty, "rows": [{"id": 1, "name": "A"}, {"id": 2}]}],
                                     "tips": {4711: ["one", "two"]}, "vendor": {}}},
               "end": {"while": 1}}
    text = g.to_lua(payload)
    assert all(ord(ch) < 127 for ch in text), "plain ASCII, whatever the names contain"
    lua = lua_load(text)
    data = lua.globals().WoWMCP_AppData
    assert data.version == 1 and data.flag is True and data.ratio == 1.5 and data.whole == 2 and data.none is None
    char = data.chars["Günther"]
    assert char.lists[1].title == nasty, repr(char.lists[1].title)
    assert char.tips[4711][2] == "two" and len(char.lists[1].rows) == 2 and data["end"]["while"] == 1
    for bad in (float("nan"), float("inf")):
        try:
            g.lua_value({"x": bad})
            raise AssertionError("a number Lua cannot read was written")
        except ValueError:
            pass
    print("ok test_lua_round_trip")


def addon(wow, toc_files, version="1.7.0"):
    folder = g.addon_dir(wow)
    os.makedirs(folder, exist_ok=True)
    with open(os.path.join(folder, "WoWMCP.toc"), "w", encoding="utf-8") as f:
        f.write(f"## Interface: 16001\n## Version: {version}\n\n" + "\n".join(toc_files) + "\n")
    return folder


def payload_of(text="A"):
    payload = {"version": 1, "generated": 100, "locale": "enUS", "scanDate": None,
               "chars": {"Tester": {"level": 1, "lists": [{"key": "k", "title": text, "sections": [
                   {"title": "S", "rows": [{"id": 1, "name": "A", "shop": True}]}]}], "tips": {}, "vendor": {}}}}
    payload["hash"] = g.digest(payload)
    return payload


def test_write_rules():
    wow = tempfile.mkdtemp()
    first = payload_of()
    r = g.write(wow, first)
    assert not r["written"] and "not installed" in r["reason"]

    folder = addon(wow, ["AppRequest.lua", "Locales.lua", "WoWMCP.lua"], "1.6.0")
    assert g.addon_state(wow) == {"installed": True, "ready": False, "version": "1.6.0"}
    r = g.write(wow, first)
    assert not r["written"] and "1.6.0" in r["reason"] and not os.path.exists(os.path.join(folder, g.DATA_FILE))

    addon(wow, ["AppRequest.lua", "AppData.lua", "Locales.lua", "WoWMCP.lua", "Lists.lua"])
    assert g.addon_state(wow)["ready"] and g.file_state(wow) is None
    r = g.write(wow, first)
    assert r["written"] and g.file_state(wow) == {"generated": 100, "hash": first["hash"]}
    assert os.listdir(folder).count(g.DATA_FILE) == 1 and not [f for f in os.listdir(folder) if f.endswith(".tmp")]
    assert lua_load(open(r["file"], encoding="utf-8").read()).globals().WoWMCP_AppData.chars.Tester.level == 1

    # The same content a minute later: the file stays, or the game would be asked to /reload forever.
    again = dict(first, generated=160)
    assert g.digest(again) == first["hash"]
    r = g.write(wow, again)
    assert not r["written"] and r["unchanged"] and g.file_state(wow)["generated"] == 100

    changed = payload_of("B")
    changed["generated"] = 200
    assert changed["hash"] != first["hash"]
    assert g.write(wow, changed)["written"] and g.file_state(wow) == {"generated": 200, "hash": changed["hash"]}
    print("ok test_write_rules")


def fake_wow(locale):
    """A game folder with one priest and one warlock export and nothing else."""
    wow = tempfile.mkdtemp()
    os.makedirs(os.path.join(wow, "WTF"))
    with open(os.path.join(wow, "WTF", "Config.wtf"), "w", encoding="utf-8") as f:
        f.write(f'SET textLocale "{locale}"\n')
    for name, cls in (("Tester", "WARLOCK"), ("Healer", "PRIEST")):
        folder = os.path.join(wow, "WTF", "Account", "TEST", "93", f"{name}-{name}", "SavedVariables")
        os.makedirs(folder)
        with open(os.path.join(folder, "WoWMCP.lua"), "w", encoding="utf-8") as f:
            f.write(f'WoWMCP_Char = {{ character = {{ name = "{name}", classFile = "{cls}", level = 46 }} }}\n')
    return wow


def with_fakes(run):
    """Runs `run` with the four calculations replaced by the hand-made results."""
    real = (g.upgrades.compute, g.writs.compute, g.recipes.compute, g.consumables.compute)

    def broken(*a, **k):
        raise LookupError("no recipes exported")

    g.upgrades.compute = lambda *a, **k: UPGRADES
    g.writs.compute = lambda *a, **k: WRITS
    g.recipes.compute = broken
    g.consumables.compute = lambda *a, **k: CONSUMABLES
    try:
        return run()
    finally:
        g.upgrades.compute, g.writs.compute, g.recipes.compute, g.consumables.compute = real


def test_payload_per_character():
    wow = fake_wow("enUS")
    payload = with_fakes(lambda: g.compute(wow))
    assert set(payload["chars"]) == {"Tester", "Healer"} and payload["locale"] == "enUS" and payload["version"] == g.VERSION
    lock, priest = payload["chars"]["Tester"], payload["chars"]["Healer"]
    assert [one["key"] for one in lock["lists"]] == ["upgrades", "writs", "crates", "consumables"]
    # Since the combat engine models the priest, the priest gets an upgrades list too (the faked
    # upgrades.compute stands in for a successful engine run).
    assert [one["key"] for one in priest["lists"]] == ["upgrades", "writs", "crates", "consumables"]
    assert lock["errors"] == {"recipes": "no recipes exported"}, "one failing part does not take the others with it"
    assert lock["level"] == 46 and 9001 in lock["tips"] and 9001 in priest["tips"]
    assert payload["hash"] == g.digest(payload) == with_fakes(lambda: g.compute(wow))["hash"]
    assert list(with_fakes(lambda: g.compute(wow, "Healer"))["chars"]) == ["Healer"]

    s = with_fakes(lambda: g.sync(wow))
    assert s["result"] is None and s["file"] is None and s["loaded"] is None and not s["addon"]["installed"]
    tester = next(c for c in s["chars"] if c["name"] == "Tester")
    assert tester["lists"][0] == {"key": "upgrades", "title": "AH upgrades", "rows": 5, "shop": 1}
    assert tester["tips"] == len(lock["tips"]) and tester["errors"] == lock["errors"]
    print("ok test_payload_per_character")


def test_texts_follow_the_client_not_the_app():
    wow = fake_wow("deDE")
    i18n.set_language("en")
    payload = with_fakes(lambda: g.compute(wow))
    assert i18n.language() == "en", "the language of the caller is restored"
    lock = payload["chars"]["Tester"]
    assert lock["lists"][0]["title"] == "AH-Upgrades" and payload["locale"] == "deDE"
    assert lock["lists"][0]["sections"][0]["note"] == "+12,5 Punkte für {m:90000}"
    assert lock["tips"][9001][1] == "Teil des besten Pakets für dein Budget"
    print("ok test_texts_follow_the_client_not_the_app")


def test_sync_writes_and_reads_what_the_game_loaded():
    wow = fake_wow("enUS")
    addon(wow, ["AppRequest.lua", "AppData.lua", "Locales.lua", "WoWMCP.lua", "Lists.lua"])
    s = with_fakes(lambda: g.sync(wow, do_write=True))
    assert s["result"]["written"] and s["file"]["hash"] == s["hash"] and s["loaded"] is None
    assert s["saved"] is None and s["file"]["modified"] > 0, "no account file yet: the game never saved"
    assert with_fakes(lambda: g.sync(wow, do_write=True))["result"]["unchanged"]
    # The game saves what it had loaded; the newest account file counts.
    for account, loaded in (("TEST", 50), ("OTHER", 70)):
        folder = os.path.join(wow, "WTF", "Account", account, "SavedVariables")
        os.makedirs(folder, exist_ok=True)
        with open(os.path.join(folder, "WoWMCP.lua"), "w", encoding="utf-8") as f:
            f.write(f'WoWMCP_Account = {{ appData = {{ generated = 1, hash = "{account}", loaded = {loaded} }} }}\n')
    assert g.loaded_state(wow) == {"generated": 1, "hash": "OTHER", "loaded": 70}
    s = with_fakes(lambda: g.sync(wow))
    assert s["saved"] >= s["file"]["modified"], "saved after the file was written: the game has read it"
    print("ok test_sync_writes_and_reads_what_the_game_loaded")


def test_plain_money():
    assert g.plain("{m:12345} and {m:-5} or {m:0} a || b") == "1g 23s 45c and -5c or 0c a | b"
    assert g.coin(None) == "{m:0}" and g.coin(12.6) == "{m:13}"
    print("ok test_plain_money")


if __name__ == "__main__":
    i18n.set_language("en")
    test_upgrade_list()
    test_writ_list()
    test_crate_list()
    test_recipe_list()
    test_consumable_list()
    test_tip_limit_and_duplicates()
    test_lua_round_trip()
    test_write_rules()
    test_payload_per_character()
    test_texts_follow_the_client_not_the_app()
    test_sync_writes_and_reads_what_the_game_loaded()
    test_plain_money()
