# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Tests of the price history without the game and without network: uv run scripts/test_pricehistory.py"""

import json
import os
import sys
import tempfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import i18n  # noqa: E402
import icons  # noqa: E402
import pricehistory as ph  # noqa: E402

BUILD = "1.60.1.70938"

# Auctionator format: h = highest minimum price of the day, l = lowest (only if below h), a = offers,
# m = last minimum price. An empty table arrives from CBOR as a list.
PRICES = {
    "2447": {"m": 158, "h": {"2512": 35, "2510": 40, "2513": 158}, "l": {"2512": 30, "2513": 999}, "a": {"2510": 50, "2513": 12}},
    "500": {"m": 0, "h": {"2513": 90000}, "l": [], "a": {"2513": 2}},  # equipment only the item export names
    "600": {"m": 12, "h": {"2511": 12}, "l": [], "a": []},             # only in a bag
    "700": {"m": 5, "h": {"2509": 5}},                                  # nobody knows its name
    "800": {"m": 5, "h": {}},                                           # no day left: not listed
    "p:123": {"m": 5, "h": {"2513": 5}},                                # battle pet key
    "version": 2,
}
NAMES = {"2447": ["Friedensblume", 1, 7], "500": [None, None, 4], "600": [None, None, 7], "700": [None, None, None]}

CHAR = """WoWMCP_Char = {
  ["client"] = { ["version"] = "1.60.1", ["build"] = "70938", ["locale"] = "deDE" },
  ["bags"] = { { ["items"] = { { ["id"] = 600, ["name"] = "Leinenstoff", ["count"] = 3, ["quality"] = 1 } } } },
}
"""
ACCOUNT = """WoWMCP_Items = { ["locale"] = "deDE", ["items"] = {
  [500] = { ["name"] = "Robe der Tests", ["quality"] = 3, ["classID"] = 4 },
} }
"""


def game(account=ACCOUNT, text_locale="deDE"):
    """A WoW folder with the client language, one character with a bag and the account's item export."""
    wow = tempfile.mkdtemp()
    sv = os.path.join(wow, "WTF", "Account", "TEST", "SavedVariables")
    char = os.path.join(wow, "WTF", "Account", "TEST", "93", "Tester-Realm", "SavedVariables")
    os.makedirs(sv)
    os.makedirs(char)
    with open(os.path.join(wow, "WTF", "Config.wtf"), "w", encoding="utf-8") as f:
        f.write(f'SET textLocale "{text_locale}"\n')
    with open(os.path.join(sv, "WoWMCP.lua"), "w", encoding="utf-8") as f:
        f.write(account)
    with open(os.path.join(char, "WoWMCP.lua"), "w", encoding="utf-8") as f:
        f.write(CHAR)
    return wow


def with_cache(names=NAMES, locale="deDE"):
    cache = tempfile.mkdtemp()
    with open(os.path.join(cache, f"itemnames-{BUILD}-{locale}.json"), "w", encoding="utf-8") as f:
        json.dump({"version": ph.NAMES_VERSION, "items": names}, f)
    return cache


def run(wow, cache):
    saved, icons.CACHE = icons.CACHE, cache
    try:
        return ph.compute(wow, prices=PRICES)
    finally:
        icons.CACHE = saved


def test_days_and_names():
    r = run(game(), with_cache())
    by_id = {it["id"]: it for it in r["items"]}
    assert sorted(by_id) == [500, 600, 700, 2447], sorted(by_id)
    assert r["count"] == 4 and r["unnamed"] == 1 and r["locale"] == "deDE"
    peace = by_id[2447]
    # Oldest day first; a lowest price above the day's highest is no lowest price.
    assert peace["days"] == [[2510, 40, 40, 50], [2512, 30, 35, None], [2513, 158, 158, 12]], peace["days"]
    assert (peace["name"], peace["q"], peace["cls"], peace["m"]) == ("Friedensblume", 1, 7, 158)
    # Names the game tables lack: the item export, then the bags; quality and class from there too.
    assert (by_id[500]["name"], by_id[500]["q"], by_id[500]["cls"], by_id[500]["m"]) == ("Robe der Tests", 3, 4, 90000)
    assert (by_id[600]["name"], by_id[600]["q"]) == ("Leinenstoff", 1)
    assert by_id[700]["name"] is None
    # Sorted by name, items without a name last.
    assert [it["id"] for it in r["items"]] == [2447, 600, 500, 700], [it["id"] for it in r["items"]]
    assert (r["lastDay"], r["lastDate"], r["firstDay"]) == (2513, "2026-11-18", 2509)
    # Day 2513 has 2 of the 4 items: still a scan. A day with less than 30% of the biggest day is not.
    assert r["lastScanDay"] == 2513 and r["lastScanDate"] == "2026-11-18"
    print("ok test_days_and_names")


def test_last_scan_ignores_single_searches():
    prices = {str(i): {"m": 10, "h": {"2512": 10}} for i in range(1, 11)}
    prices["1"]["h"]["2513"] = 12  # one search the next day
    saved, icons.CACHE = icons.CACHE, with_cache()
    try:
        r = ph.compute(game(), prices=prices)
    finally:
        icons.CACHE = saved
    assert (r["lastDay"], r["lastScanDay"]) == (2513, 2512), (r["lastDay"], r["lastScanDay"])
    print("ok test_last_scan_ignores_single_searches")


def test_export_of_another_language_is_ignored():
    r = run(game(account=ACCOUNT.replace('"deDE"', '"enUS"')), with_cache())
    by_id = {it["id"]: it for it in r["items"]}
    assert by_id[500]["name"] is None and r["unnamed"] == 2
    print("ok test_export_of_another_language_is_ignored")


def test_names_are_built_from_the_tables_and_cached():
    cache = tempfile.mkdtemp()
    tables = {"Item": [{"ID": "2447", "ClassID": "7"}, {"ID": "500", "ClassID": "4"}],
              "ItemSparse": [{"ID": "2447", "Display_lang": "Peacebloom", "OverallQualityID": "1"}]}
    asked = []
    saved = icons.CACHE, icons.table
    icons.CACHE = cache
    icons.table = lambda name, build, locale=None: asked.append((name, build, locale)) or tables[name]
    try:
        names = ph.load_names(BUILD, "enUS")
        assert names == {"2447": ["Peacebloom", 1, 7], "500": [None, None, 4]}, names
        assert asked == [("Item", BUILD, None), ("ItemSparse", BUILD, "enUS")], asked
        assert os.path.exists(os.path.join(cache, f"itemnames-{BUILD}-enUS.json"))
        # The second time comes from the cache, also without a build.
        asked.clear()
        assert ph.load_names(BUILD, "enUS") == names and ph.load_names(None, "enUS") == names and asked == []
    finally:
        icons.CACHE, icons.table = saved
    print("ok test_names_are_built_from_the_tables_and_cached")


def test_without_prices():
    saved, icons.CACHE = icons.CACHE, with_cache()
    try:
        ph.compute(game(), prices={"version": 2})
    except LookupError as e:
        assert "Auctionator.lua" in str(e)
    else:
        raise AssertionError("no items should be an error")
    finally:
        icons.CACHE = saved
    print("ok test_without_prices")


if __name__ == "__main__":
    i18n.set_language("en")
    test_days_and_names()
    test_last_scan_ignores_single_searches()
    test_export_of_another_language_is_ignored()
    test_names_are_built_from_the_tables_and_cached()
    test_without_prices()
