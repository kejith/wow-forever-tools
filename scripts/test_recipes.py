# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Tests der Rezept-Rechnung ohne Spiel und ohne Netz: uv run scripts/test_recipes.py"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import recipes as r  # noqa: E402

# Auctionator-Format: m = Mindestpreis, a = Angebote pro Scan-Tag, h = Tagespreis.
PRICES = {
    "100": {"m": 1000, "a": {"10": 50}, "h": {"10": 1000}},   # Ergebnis A: 10s
    "101": {"m": 200, "a": {"10": 80}, "h": {"10": 200}},     # Kraut: 2s
    "3372": {"m": 60, "a": {"10": 900}, "h": {"10": 60}},     # Verbleite Phiole im AH teurer als beim Händler
    "200": {"m": 5000, "a": {"10": 20}, "h": {"10": 5000}},   # Ergebnis B: 50s
    "900": {"m": 20000, "a": {"10": 2}, "h": {"10": 20000}},  # Rezept-Item für B: 2g
    "300": {"m": 100, "a": {"8": 5}, "h": {"8": 100, "10": 100}},  # Ergebnis C, Preis 2 Tage alt
}
ITEMS = {
    "3372": {"name": "Verbleite Phiole", "buy": 200, "buyStack": 5},  # 40c pro Stück
    "100": {"name": "Trank A", "quality": 1}, "101": {"name": "Kraut"}, "200": {"name": "Trank B"},
    "300": {"name": "Gebundenes C", "bind": 1, "sell": 25},
    "900": {"name": "Rezept: Trank B", "reqSkill": 150},
}
CATALOG = {"lines": {"171": {
    "1": {"name": "Trank A", "output": 100, "qty": 1, "reagents": [[101, 2], [3372, 1]], "yellow": 100, "grey": 140,
          "acquire": 0, "recipeItems": []},
    "2": {"name": "Trank B", "output": 200, "qty": 1, "reagents": [[101, 5]], "yellow": 160, "grey": 200,
          "acquire": 0, "recipeItems": [900]},
    "3": {"name": "C", "output": 300, "qty": 2, "reagents": [[101, 1]], "yellow": 90, "grey": 120,
          "acquire": 0, "recipeItems": []},
    "4": {"name": "Unerreichbar", "output": 100, "qty": 1, "reagents": [[101, 1]], "yellow": 1, "grey": 2,
          "acquire": 3, "recipeItems": []},
}}, "items": ITEMS}


def market(vendor=None):
    return r.Market(PRICES, vendor or {}, ITEMS)


def test_color():
    assert r.color_of(90, 100, 140) == 0
    assert r.color_of(100, 100, 140) == 1
    assert r.color_of(120, 100, 140) == 2
    assert r.color_of(140, 100, 140) == 3
    print("ok test_color")


def test_profit_uses_cheapest_reagent_and_ah_cut():
    row = r.evaluate(1, CATALOG["lines"]["171"]["1"], None, 130, market(), {101: 5}, {})
    # 2 × 2s Kraut + 40c Phiole vom Händler (AH: 60c) = 440c; Verkauf 1000c − 5 % = 950c
    assert row["cost"] == 440, row
    assert row["reagents"][1]["source"] == "Händler"
    assert row["sell"] == 950 and row["profit"] == 510
    assert row["craftable"] == 0  # keine Phiole im Besitz
    assert row["difficulty"] == 2
    print("ok test_profit_uses_cheapest_reagent_and_ah_cut")


def test_seen_vendor_price_wins_and_ingame_reagents_override():
    vendor = {3372: {"price": 50, "npc": "Hamuul"}}
    scan = {"output": 100, "qtyMin": 2, "qtyMax": 4, "reagents": [{"id": 101, "count": 1}, {"id": 3372, "count": 1}]}
    row = r.evaluate(1, CATALOG["lines"]["171"]["1"], scan, 130, market(vendor), {}, {})
    assert row["reagents"][1]["source"] == "Hamuul" and row["reagents"][1]["unit"] == 50
    assert row["cost"] == 250 and row["qty"] == 3 and row["sell"] == 2850
    print("ok test_seen_vendor_price_wins_and_ingame_reagents_override")


def test_bound_output_falls_back_to_vendor_sell():
    row = r.evaluate(3, CATALOG["lines"]["171"]["3"], None, 130, market(), {}, {})
    assert row["sellSource"] == "Händler" and row["sell"] == 50
    assert any("gebunden" in n for n in row["notes"]) and any("2 Tage alt" in n for n in row["notes"])
    print("ok test_bound_output_falls_back_to_vendor_sell")


def test_split_learned_missing_and_acquisition():
    scan = {"rank": 130, "recipes": {1: {"learned": True, "output": 100}, 2: {"learned": False}}}
    learned, missing = r.profession_rows(171, CATALOG, scan, 130, market(), {})
    assert [x["id"] for x in learned] == [1]
    assert {x["id"] for x in missing} == {2, 3}  # 4 ist nicht erhältlich
    b = next(x for x in missing if x["id"] == 2)
    assert b["acquire"]["learnable"] is False and b["acquire"]["reqSkill"] == 150
    assert b["acquire"]["best"]["kind"] == "ah" and b["acquire"]["best"]["price"] == 20000
    c = next(x for x in missing if x["id"] == 3)
    assert c["acquire"]["trainer"] and c["acquire"]["learnable"] is True  # gelb (90) liegt unter 130
    print("ok test_split_learned_missing_and_acquisition")


def test_without_scan_everything_is_unknown():
    learned, missing = r.profession_rows(171, CATALOG, None, 130, market(), {})
    assert learned == [] and all(x["learned"] is None for x in missing)
    assert 4 not in {x["id"] for x in missing}
    print("ok test_without_scan_everything_is_unknown")


if __name__ == "__main__":
    test_color()
    test_profit_uses_cheapest_reagent_and_ah_cut()
    test_seen_vendor_price_wins_and_ingame_reagents_override()
    test_bound_output_falls_back_to_vendor_sell()
    test_split_learned_missing_and_acquisition()
    test_without_scan_everything_is_unknown()
