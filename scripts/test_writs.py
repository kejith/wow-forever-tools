# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Tests of the writ and trade crate calculation without the game and without network: uv run scripts/test_writs.py"""

import os
import sys
import tempfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import i18n  # noqa: E402
import writs as v  # noqa: E402

EMPTY_WOW = tempfile.mkdtemp()  # no character: nothing owned


def p(m, listed=50):
    return {"m": m, "a": {"10": listed}, "h": {"10": m}}


# Journeyman: Elixir of Wisdom (264012 -> 3383), Expert: Mana Potion (264062 -> 3827)
PRICES = {"264012": p(5000), "3383": p(2000), "264062": p(9000), "3827": p(4000),
          # Apprentice herb crate 248682: Peacebloom is cheap, Silverleaf cheaper but too few in the AH
          "248682": p(1000), "2447": p(30, 100), "765": p(10, 5), "2449": p(500, 30)}
QUESTS = {264012: 94212, 264062: 94262}


def run(payouts=None, level=46, prices=PRICES):
    return v.compute(EMPTY_WOW, "", 3, False, prices=dict(prices), extras=(payouts or {}, level, 120), quest_of=QUESTS)


def writ(r, wid):
    return next(b for t in r["tiers"] for b in t["best"] if b["writId"] == wid)


def test_estimate_without_measurement():
    w = writ(run(), 264012)
    assert w["cost"] == 7000 and w["payout"] == v.POUCH_ESTIMATE["J"] and not w["payoutMeasured"]
    assert w["net"] == 7000 - v.POUCH_ESTIMATE["J"] and w["per"] == w["net"] / 75
    print("ok test_estimate_without_measurement")


def test_measured_pouch_and_tier_fallback():
    payouts = {"writQuests": {94212: {"pouch": 284583, "money": 0, "n": 1}},
               "pouches": {284583: {"n": 2, "sum": 8000, "min": 3000, "max": 5000},
                           284584: {"n": 1, "sum": 9000, "min": 9000, "max": 9000}}}
    r = run(payouts)
    w = writ(r, 264012)
    assert w["payout"] == 4000 and w["payoutMeasured"] and "2x" in w["payoutSource"]
    # No Expert writ handed in: the pouch is unknown, so the estimate stays (do not guess 284584).
    e = writ(r, 264062)
    assert e["payout"] == v.POUCH_ESTIMATE["E"] and not e["payoutMeasured"]
    assert r["dailyNet"] == sum(t["best"][0]["net"] for t in r["tiers"] if t["best"])
    print("ok test_measured_pouch_and_tier_fallback")


def test_crate_cheapest_bundle_the_ah_can_fill():
    r = run()
    tier = next(t for t in r["crates"]["tiers"] if t["tier"] == "L")
    herbs = next(c for c in tier["rows"] if c["crateId"] == 248682)
    # Silverleaf costs 200c for 20, but only 5 are listed -> Peacebloom 20 x 30c = 600c
    assert herbs["best"]["id"] == 2447 and herbs["best"]["cost"] == 600
    assert herbs["cost"] == 1600 and herbs["per"] == 1600 / 5 and herbs["perGreen"] == 1600 / 10
    assert r["crates"]["best"][0]["crateId"] == 248682 and r["favor"] == 120
    print("ok test_crate_cheapest_bundle_the_ah_can_fill")


def test_crate_without_price_is_incomplete_and_level_gate():
    r = run(level=15)
    ore = next(c for t in r["crates"]["tiers"] for c in t["rows"] if c["crateId"] == 248683)
    assert not ore["complete"] and ore["per"] is None and "not in the AH" in ore["note"]
    expert = next(t for t in r["crates"]["tiers"] if t["tier"] == "E")
    assert expert["tooLow"] and all(b["tier"] != "E" for b in r["crates"]["best"])
    print("ok test_crate_without_price_is_incomplete_and_level_gate")


def test_measured_favor_overrides_guess():
    payouts = {"crates": {v.SEALED_CRATE["A"][0]: {"n": 3, "sum": 75, "min": 25, "max": 25}}}
    t = next(t for t in run(payouts)["crates"]["tiers"] if t["tier"] == "A")
    assert t["favor"] == 25 and not t["favorGuess"] and "3x" in t["favorSource"]
    t0 = next(t for t in run()["crates"]["tiers"] if t["tier"] == "A")
    assert t0["favorGuess"] and t0["favor"] == v.CRATE_FAVOR["A"][0]
    print("ok test_measured_favor_overrides_guess")


def test_crate_data_matches_game_amounts():
    # Amounts from SpellEffect (spell "Fill Crate"), build 1.60.1.70938
    game = {248682: [20, 20, 10, 10, 10], 248707: [8, 7, 4, 45], 248712: [2, 10, 20], 248696: [20, 20, 20, 20]}
    crates = {cid: [q for _, q in opts] for cid, _, _, opts in v.crate_list()}
    assert len(crates) == 30
    for cid, amounts in game.items():
        assert crates[cid] == amounts, (cid, crates[cid])
    print("ok test_crate_data_matches_game_amounts")


def test_names_follow_the_language():
    w = writ(run(), 264012)
    assert w["name"] == "Elixir of Wisdom" and w["prof"] == "Alchemy"
    tier = next(t for t in run()["crates"]["tiers"] if t["tier"] == "L")
    herbs = next(c for c in tier["rows"] if c["crateId"] == 248682)
    assert tier["name"] == "Apprentice" and herbs["kind"] == "Herbs" and herbs["best"]["name"] == "Peacebloom"
    i18n.set_language("de")
    try:
        r = run(level=15)
        w = writ(r, 264012)
        assert w["name"] == "Elixier der Weisheit" and w["prof"] == "Alchimie", w
        tier = next(t for t in r["crates"]["tiers"] if t["tier"] == "L")
        herbs = next(c for c in tier["rows"] if c["crateId"] == 248682)
        assert tier["name"] == "Lehrling" and herbs["kind"] == "Kräuter" and herbs["best"]["name"] == "Friedensblume"
        ore = next(c for c in tier["rows"] if c["crateId"] == 248683)
        assert "nicht im AH" in ore["note"], ore["note"]
    finally:
        i18n.set_language("en")
    print("ok test_names_follow_the_language")


def test_every_table_item_has_a_name_in_both_languages():
    ids = {int(x) for line in v.WRITS.splitlines() for x in (line.split("|")[0], line.split("|")[2])}
    for cid, _, _, opts in v.crate_list():
        ids.add(cid)
        ids.update(i for i, _ in opts)
    for lang in i18n.LANGUAGES:
        i18n.set_language(lang)
        missing = [i for i in sorted(ids) if v.item_name(i) == f"Item {i}"]
        assert not missing, (lang, missing)
    i18n.set_language("en")
    print("ok test_every_table_item_has_a_name_in_both_languages")


if __name__ == "__main__":
    i18n.set_language("en")
    test_names_follow_the_language()
    test_every_table_item_has_a_name_in_both_languages()
    test_estimate_without_measurement()
    test_measured_pouch_and_tier_fallback()
    test_crate_cheapest_bundle_the_ah_can_fill()
    test_crate_without_price_is_incomplete_and_level_gate()
    test_measured_favor_overrides_guess()
    test_crate_data_matches_game_amounts()
