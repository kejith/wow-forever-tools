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


def history_market(out_prices, herb=None, partial_day=None):
    """Vier volle Scan-Tage (20–23) für Trank A (100) aus 2 Kraut (101) und 1 Phiole (Händler 40c)."""
    days = [20, 21, 22, 23]
    herb = herb or {d: 200 for d in days}
    prices = {
        "100": {"m": out_prices[23], "a": {str(d): 10 * (i + 1) for i, d in enumerate(days) if d in out_prices},
                "h": {str(d): p for d, p in out_prices.items()}},
        "101": {"m": herb[23], "a": {str(d): 80 for d in herb}, "h": {str(d): p for d, p in herb.items()}},
    }
    for i in range(8):  # Füll-Items, damit die Tage als volle Scans zählen
        prices[str(500 + i)] = {"m": 1, "a": {str(d): 1 for d in days}, "h": {str(d): 1 for d in days}}
    if partial_day:  # einzelne Suche an einem Tag ohne vollen Scan
        prices["100"]["a"][str(partial_day)] = 3
        prices["100"]["h"][str(partial_day)] = 9999
    return r.Market(prices, {}, ITEMS)


def test_history_per_full_scan_day():
    m = history_market({20: 3000, 21: 2000, 22: 1000, 23: 1000}, partial_day=19)
    assert m.scan_days == [20, 21, 22, 23], m.scan_days  # Tag 19 ist kein voller Scan
    row = r.evaluate(1, CATALOG["lines"]["171"]["1"], None, 130, m, {}, {})
    # Kosten 2 × 200 + 40 = 440; Verkauf −5 %
    assert [h["profit"] for h in row["history"]] == [2410, 1460, 510, 510], row["history"]
    assert row["history"][-1]["profit"] == row["profit"]  # letzter Punkt = aktueller Gewinn
    assert row["history"][0] == {"date": "2020-01-21", "profit": 2410, "price": 3000, "listed": 10}
    assert row["trend"] == "fallend"
    assert any(n.startswith("Gewinn fällt (am 22.01. noch +0.15g)") for n in row["notes"]), row["notes"]
    assert not any("gerade billig" in n for n in row["notes"])  # 1000 < 60 % des Medians, aber kein Ausrutscher
    print("ok test_history_per_full_scan_day")


def test_history_uses_day_middle_and_skips_days_without_reagent_price():
    m = history_market({20: 1000, 21: 1000, 22: 1000, 23: 1000}, herb={20: 200, 22: 300, 23: 200})
    m.prices["100"]["l"] = {"22": 800}  # zwei Scans am Tag 22: 1000 und 800 → Mitte 900
    m.prices["101"]["l"] = {"22": 100}  # Kraut 300 und 100 → Mitte 200
    hist = r.profit_history(m, 100, 1, [(101, 2), (3372, 1)])
    assert [h["date"][8:] for h in hist] == ["21", "23", "24"], hist  # Tag 21: kein Krautpreis
    assert hist[1]["price"] == 900 and hist[1]["profit"] == round(900 * 0.95 - 440)
    print("ok test_history_uses_day_middle_and_skips_days_without_reagent_price")


def test_trend():
    pts = lambda *profits: [{"profit": p} for p in profits]  # noqa: E731
    assert r.trend_of(pts(500, 400)) is None  # zu wenige Scan-Tage
    assert r.trend_of(pts(1000, 1050, 950, 1000)) == "stabil"
    assert r.trend_of(pts(200, 300, 900, 1100)) == "steigend"
    assert r.trend_of(pts(4000, 3000, 700, 200)) == "fallend"
    assert r.trend_of(pts(200, 1200, 1100, 300)) == "fallend"  # erst gestiegen, zuletzt eingebrochen
    assert r.trend_of(pts(1800, -800, 270, 40, 280)) == "stabil"  # Auf und Ab ohne Richtung
    assert r.trend_of(pts(10, -10, 20, -20)) == "stabil"  # Kupferbeträge um null sind kein Trend
    row = r.evaluate(3, CATALOG["lines"]["171"]["3"], None, 130, market(), {}, {})
    assert row["history"] == [] and row["trend"] is None  # gebunden: kein AH-Verkauf, kein Verlauf
    print("ok test_trend")


if __name__ == "__main__":
    test_color()
    test_profit_uses_cheapest_reagent_and_ah_cut()
    test_seen_vendor_price_wins_and_ingame_reagents_override()
    test_bound_output_falls_back_to_vendor_sell()
    test_split_learned_missing_and_acquisition()
    test_without_scan_everything_is_unknown()
    test_history_per_full_scan_day()
    test_history_uses_day_middle_and_skips_days_without_reagent_price()
    test_trend()
