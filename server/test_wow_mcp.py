# /// script
# requires-python = ">=3.11"
# dependencies = ["mcp>=2.3,<3", "lupa>=2.0"]
# ///
"""Tests of the MCP server against a made-up character, without the game: uv run server/test_wow_mcp.py

Writes two SavedVariables files into a temporary WoW folder and calls the tools the way the MCP client does.
"""

import asyncio
import os
import sys
import tempfile
import time
from pathlib import Path

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import wow_mcp as server  # noqa: E402

NOW = int(time.time())

THRALL = """
WoWToolsForever_Char = {
  updated = %d,
  addon = { version = "1.6.0" },
  character = { name = "Thrall", surname = "Testhammer", level = 20, class = "Shaman", race = "Orc",
                zone = "Durotar", money = 123456, xp = 500, xpMax = 2000, rested = 100 },
  equipment = {
    { slot = 16, id = 2488, name = "Gladius", quality = 1, ilvl = 12, durability = { 30, 45 },
      link = "|cffffffff|Hitem:2488:15:0:0|h[Gladius]|h|r",
      stats = { ITEM_MOD_STAMINA_SHORT = 2, RESISTANCE0_NAME = 10 } },
  },
  bags = {
    { id = 0, name = "Backpack", size = 16, free = 13, items = {
        { slot = 1, id = 2447, name = "Peacebloom", count = 5, quality = 1 },
        { slot = 2, id = 2447, name = "Peacebloom", count = 20, quality = 1 },
        { slot = 3, id = 6948, name = "Hearthstone", quality = 1 },
    } },
  },
  bank = { updated = %d, containers = {
    { id = -1, name = "Bank", size = 24, free = 23, items = { { slot = 1, id = 2447, name = "Peacebloom", count = 7 } } },
  } },
  quests = {
    { id = 786, title = "Thwarting Kolkar Aggression", level = 8, complete = false, rewardMoney = 350,
      objectives = { { text = "Attack plans destroyed", need = 3, have = 1 }, { text = "Report back", finished = true } } },
  },
  completedQuests = { 4641, 788 },
  reputation = { { id = 76, name = "Orgrimmar", standing = "Friendly", value = 4500, min = 3000, max = 9000 } },
  skills = {
    professions = { { name = "Alchemy", rank = 75, max = 150, skillLine = 171 } },
    skills = {
      { name = "Alchemy", category = "Professions", rank = 75, maxRank = 150, skillID = 171 },
      { name = "Alchemy", category = "Professions", rank = 75, maxRank = 150, skillID = 2937 },
      { name = "Axes", category = "Weapon Skills", rank = 90, maxRank = 100, skillID = 44 },
    },
  },
  talents = { class = { { label = "Talents", spent = 11, unspent = 0,
    groups = { { id = 1, name = "Elemental", spent = 11 } },
    talents = { { name = "Convection", group = "Elemental", rank = 5, maxRank = 5, spellID = 16039 } } } }, legacy = {} },
  errors = {},
}
""" % (NOW - 7200, NOW - 86400)

# An older snapshot of a second character, written without a bank visit and with a failed section.
JAINA = """
WoWToolsForever_Char = {
  updated = %d,
  character = { name = "Jaina", level = 12, class = "Mage", race = "Human", money = 99 },
  bags = { { id = 0, size = 16, free = 16, items = {} } },
  errors = { quests = "attempt to index a nil value" },
}
""" % (NOW - 90000)


def make_wow() -> Path:
    root = Path(tempfile.mkdtemp())
    for name, text in (("Thrall - Testrealm", THRALL), ("Jaina - Testrealm", JAINA)):
        folder = root / "WTF" / "Account" / "TEST" / "93" / name / "SavedVariables"
        folder.mkdir(parents=True)
        (folder / "WoWToolsForever.lua").write_text(text, encoding="utf-8")
    return root


def fails(call, text: str) -> None:
    try:
        call()
    except server.ToolError as e:
        assert text in str(e), e
    else:
        raise AssertionError(f"expected a ToolError with {text!r}")


def test_characters():
    chars = server.list_characters()
    assert [c["character"] for c in chars] == ["Thrall Testhammer", "Jaina"], chars  # newest snapshot first
    assert chars[0]["ruleset"] == "93" and chars[0]["snapshot_age"] == "2h ago" and chars[1]["snapshot_age"] == "1d ago"
    c = server.get_character()  # no name: the most recently played character
    assert c["character"] == "Thrall Testhammer" and c["money"] == "12g 34s 56c"
    assert c["xp"] == {"current": 500, "max": 2000, "percent": 25.0, "rested": 100}
    assert "name" not in c and "export_errors" not in c
    assert server.get_character("jaina")["money"] == "0g 0s 99c"
    assert server.get_character("jai")["level"] == 12  # part of the name
    fails(lambda: server.get_character("Nobody"), "No character matches")
    fails(lambda: server.get_character("a"), "Ambiguous name")
    print("ok test_characters")


def test_equipment_and_inventory():
    e = server.get_equipment("Thrall")["equipment"]
    assert e == [{"slot": "Main Hand", "id": 2488, "name": "Gladius", "count": 1, "quality": "Common", "ilvl": 12,
                  "enchantId": 15, "stats": {"stamina": 2, "armor": 10}, "durability": "30/45"}], e
    inv = server.get_inventory("Thrall")
    assert inv["bag_slots"] == {"total": 16, "free": 13} and inv["bank_scanned"]["age"] == "1d ago"
    herb = next(i for i in inv["items"] if i["id"] == 2447)
    assert herb["count"] == 32 and herb["bags"] == 25 and herb["bank"] == 7, herb
    assert next(i for i in server.get_inventory("Thrall", include_bank=False)["items"] if i["id"] == 2447)["count"] == 25
    detailed = server.get_inventory("Thrall", detailed=True)["containers"]
    assert [c["location"] for c in detailed] == ["bags", "bank"] and len(detailed[0]["items"]) == 3
    assert "Never scanned" in server.get_inventory("Jaina")["bank"]
    print("ok test_equipment_and_inventory")


def test_find_item():
    hits = server.find_item("peace")["matches"]
    assert len(hits) == 3 and {h["location"] for h in hits} == {"bags", "bank"}, hits
    assert [h["location"] for h in server.find_item("2488")["matches"]] == ["equipped"]
    assert server.find_item("peace", "Jaina")["matches"] == []
    fails(lambda: server.find_item("  "), "must not be empty")
    print("ok test_find_item")


def test_quests_reputation_skills_talents():
    q = server.get_quests("Thrall")
    assert q["count"] == 1 and q["quests"][0]["rewardMoney"] == "0g 3s 50c"
    assert q["quests"][0]["objectives"] == [{"text": "Attack plans destroyed", "done": False, "progress": "1/3"},
                                            {"text": "Report back", "done": True}]
    done = server.check_completed_quests([788, 786], "Thrall")
    assert done["total_completed"] == 2 and done["results"] == {"788": True, "786": False}
    fails(lambda: server.get_quests("Jaina"), "addon error: attempt to index a nil value")
    fails(lambda: server.get_reputation("Jaina"), "has no reputation data.")
    assert server.get_reputation("Thrall")["factions"][0]["progress"] == "1500/6000"
    s = server.get_skills("Thrall")
    assert s["professions"][0]["skillLine"] == 171
    assert [x["name"] for x in s["skills"]] == ["Alchemy", "Axes"]  # the second Alchemy line is dropped
    t = server.get_talents("Thrall")
    assert t["class"][0]["tabs"] == {"Elemental": 11} and t["class"][0]["talents"][0]["rank"] == "5/5"
    assert t["legacy"] == []
    print("ok test_quests_reputation_skills_talents")


def test_half_written_file_keeps_the_last_snapshot():
    path = next(c.path for c in server.store.characters() if c.first_name == "Jaina")
    path.write_text("WoWToolsForever_Char = { updated = 1, character = {", encoding="utf-8")  # WoW is still writing
    os.utime(path, (NOW + 10, NOW + 10))
    assert server.get_character("Jaina")["level"] == 12
    path.write_text('os.execute("echo no")\nWoWToolsForever_Char = {}', encoding="utf-8")  # no stdlib in the sandbox
    os.utime(path, (NOW + 20, NOW + 20))
    assert server.get_character("Jaina")["level"] == 12
    print("ok test_half_written_file_keeps_the_last_snapshot")


def test_missing_folder():
    saved, server.store = server.store, server.Store(Path(tempfile.mkdtemp()))
    try:
        fails(server.list_characters, "WoW account folder not found")
    finally:
        server.store = saved
    print("ok test_missing_folder")


def test_tools_are_registered():
    tools = asyncio.run(server.mcp.list_tools())
    names = {t.name for t in tools}
    assert names == {"list_characters", "get_character", "get_equipment", "get_inventory", "find_item", "get_quests",
                     "check_completed_quests", "get_reputation", "get_skills", "get_talents"}, names
    assert all(t.description for t in tools)
    print("ok test_tools_are_registered")


if __name__ == "__main__":
    server.store = server.Store(make_wow())
    test_characters()
    test_equipment_and_inventory()
    test_find_item()
    test_quests_reputation_skills_talents()
    test_half_written_file_keeps_the_last_snapshot()
    test_missing_folder()
    test_tools_are_registered()
