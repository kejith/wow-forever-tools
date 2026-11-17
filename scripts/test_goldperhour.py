# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Tests für die GoldPerHour-Auswertung ohne Spiel. uv run scripts/test_goldperhour.py

Das Addon selbst wird in seinem eigenen Repository getestet (github.com/kejith/GoldPerHour,
tests/test_addon.py: Lua-Simulation mit nachgebauter WoW-API). Die Testdaten hier sind der Stand, den
das Addon am Ende dieser Simulation speichert. Neu erzeugen nach einer Änderung am Datenformat:

    cd ../GoldPerHour
    uv run tests/test_addon.py --dump ../wow/scripts/fixtures/goldperhour/WTF/Account/TEST/SavedVariables/GoldPerHour.lua
"""

import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import goldperhour as g  # noqa: E402

# A WoW folder with nothing but the SavedVariables file of GoldPerHour 0.3.
FIXTURE = os.path.join(HERE, "fixtures", "goldperhour")
LINK = "|cffffffff|Hitem:2447::::::::20:::::|h[Friedensblume]|h|r"


def test_saved_by_addon():
    # The simulated evening: a session with 3 herbs (1000 each) and 5 silver from a mob, two /reload, then a
    # request that changes the settings and resets (the session goes to the history), then 2 herbs at
    # vendor price in a new session that is running at the last save.
    out = g.compute(FIXTURE)
    assert out["found"] and not out["legacy"] and out["version"] == "0.3.0"
    assert out["file"].endswith("GoldPerHour.lua") and out["updated"]
    assert out["settings"] == {"scale": 2, "tooltipItems": 3, "priceSource": "vendor", "resumeAfterReload": True,
                               "autoStart": True, "shown": False, "herbSpells": [1234]}, out["settings"]
    assert out["lastRequest"]["id"] == 8 and "unknown action" in out["lastRequest"]["error"]
    assert out["current"]["total"] == 20 and out["current"]["runningAtSave"] is True
    assert out["current"]["perHour"] is None  # under a minute of session time
    h = out["history"][0]
    assert h["total"] == 3500 and h["perHour"] == 3500 / 500 * 3600 and h["zones"] == [{"zone": "Brachland", "value": 3500}]
    assert h["categories"]["loot"]["itemCount"] == 3 and h["chars"] == ["Malgruk"]
    assert h["categories"]["loot"]["items"] == [{"id": 2447, "name": "Friedensblume", "quality": 1, "count": 3, "value": 3000}]
    assert [s["t"] for s in h["samples"]] == [0, 400, 500] and h["samples"][1]["loot"] == 3500
    assert out["summary"]["sessions"] == 1 and out["summary"]["best"] == h["started"]
    print("ok test_saved_by_addon")


def test_old_savedvariables():
    # A 0.2 file: no settings, session or history; items only with link.
    old = {"elapsed": 1800, "gold": {"herb": 0, "loot": 9000}, "coin": {"herb": 0, "loot": 0},
           "items": {"herb": {}, "loot": {2447: {"id": 2447, "link": LINK, "count": 9, "value": 9000}}}, "herbSpells": {}}
    out = g.from_db(old)
    assert out["legacy"] and out["history"] == [] and out["settings"]["shown"] and out["settings"]["scale"] == 1
    item = out["current"]["categories"]["loot"]["items"][0]
    assert item["name"] == "Friedensblume" and item["quality"] == 1 and out["current"]["perHour"] == 18000
    assert g.compute(os.path.join(HERE, "does-not-exist"))["found"] is False
    print("ok test_old_savedvariables")


if __name__ == "__main__":
    test_saved_by_addon()
    test_old_savedvariables()
