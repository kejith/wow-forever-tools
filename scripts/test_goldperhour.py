# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Tests für GoldPerHour ohne Spiel: das Addon läuft in Lua mit nachgebauter WoW-API, danach liest
goldperhour.py das Ergebnis. uv run scripts/test_goldperhour.py"""

import os
import sys

from lupa.lua54 import LuaRuntime

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import goldperhour as g  # noqa: E402
import upgrades  # noqa: E402

ADDON = os.path.join(HERE, "..", "addon", "GoldPerHour", "GoldPerHour.lua")

# Just enough of the client for GoldPerHour.lua: frames swallow every method, events go to the frame of
# the last load (a /reload re-runs the file against the same GoldPerHourDB).
STUBS = r"""
now, money, zone = 100, 10000, "Brachland"
loot = {}
tickers = {}
handler = nil
local function noop() end
local frameMeta = { __index = function(t, k)
  if k == "SetScript" then return function(self, name, fn) if name == "OnEvent" then handler = fn end end end
  if k == "IsShown" then return function(self) return self.shown ~= false end end
  if k == "SetShown" then return function(self, v) self.shown = v end end
  if k == "SetScale" then return function(self, v) self.scale = v end end
  if k == "GetPoint" then return function() return "CENTER", nil, "CENTER", 0, 0 end end
  if k == "GetBottom" then return function() return 300 end end
  if k == "CreateTexture" or k == "CreateFontString" then return function() return setmetatable({}, getmetatable(t)) end end
  return noop
end }
function CreateFrame() return setmetatable({}, frameMeta) end
UIParent = CreateFrame()
GameTooltip = setmetatable({ IsOwned = function() return false end }, frameMeta)
function fire(event, ...) handler(nil, event, ...) end
function GetLocale() return "deDE" end
Enum = { LootSlotType = { Item = 1 } }
function GetTime() return now end
function time() return 1763400000 + now end
function GetMoney() return money end
function GetMoneyString(c) return tostring(c) end
function GetRealZoneText() return zone end
function UnitName() return "Malgruk" end
function wipe(t) for k in pairs(t) do t[k] = nil end return t end
function strsplit(sep, s) return s:match("^([^" .. sep .. "]*)") end
StaticPopupDialogs = {}
SlashCmdList = {}
YES, NO = "Ja", "Nein"
printed = {}
function print(...) printed[#printed + 1] = table.concat({...}, " ") end
C_Timer = { NewTicker = function(_, fn) tickers[#tickers + 1] = fn end }
function tick() tickers[#tickers]() end
C_AddOns = { GetAddOnMetadata = function() return "0.3.0" end }
C_Spell = { GetSpellName = function(id) return id == 2366 and "Kräutersammeln" or "Anderes" end }
-- Item 2447 (Friedensblume): sells for 10 copper, Auctionator says 1000.
C_Item = {
  GetItemInfo = function(link) return "Friedensblume", link, 1, 5, 0, "", "", 20, "", 0, 10, 7, 9, 0 end,
  GetItemIconByID = function() return 134400 end,
}
Item = { CreateFromItemLink = function(_, link)
  return { IsItemEmpty = function() return false end, ContinueOnItemLoad = function(_, fn) fn() end,
           GetItemID = function() return tonumber(link:match("item:(%d+)")) end }
end }
Auctionator = { API = { v1 = { GetAuctionPriceByItemLink = function() return 1000 end } } }
function GetNumLootItems() return #loot end
function LootSlotHasItem(i) return loot[i] ~= nil end
function GetLootSourceInfo(i) return loot[i] and loot[i].guid end
function GetLootSlotType(i) return 1 end
function GetLootSlotLink(i) return loot[i] and loot[i].link end
function GetLootSlotInfo(i) return nil, nil, loot[i] and loot[i].qty end
"""

LINK = "|cffffffff|Hitem:2447::::::::20:::::|h[Friedensblume]|h|r"


def new_runtime():
    lua = LuaRuntime(register_eval=False)
    lua.execute(STUBS)
    lua.globals().LINK = LINK
    return lua


def load_addon(lua, request=None):
    """Runs the addon file like the client does on login or /reload."""
    lua.execute(f"GoldPerHour_AppRequest = {request or 'nil'}")
    chunk = lua.eval("function(src) return assert(load(src, 'GoldPerHour.lua')) end")(open(ADDON, encoding="utf-8").read())
    chunk("GoldPerHour")
    lua.execute('fire("ADDON_LOADED", "GoldPerHour"); fire("PLAYER_LOGIN")')


def mob_loot(lua, qty=3, coins=500):
    lua.execute(f"""
      loot = {{ {{ link = LINK, qty = {qty}, guid = "Creature-0-1-2-3-4-5" }} }}
      fire("LOOT_READY"); fire("LOOT_OPENED"); fire("LOOT_SLOT_CLEARED", 1)
      money = money + {coins}; fire("PLAYER_MONEY"); loot = {{}}; fire("LOOT_CLOSED")""")


def db(lua):
    return upgrades.lua_to_py(lua.globals().GoldPerHourDB)


def test_session_reload_request_and_history():
    lua = new_runtime()
    load_addon(lua)
    lua.execute('fire("PLAYER_ENTERING_WORLD", true, false)')
    lua.globals().SlashCmdList.GOLDPERHOUR("start")
    mob_loot(lua)
    lua.execute("now = now + 400; tick()")
    d = db(lua)
    assert d["gold"]["loot"] == 3 * 1000 + 500 and d["coin"]["loot"] == 500, d["gold"]
    assert d["session"]["zones"] == {"Brachland": 3500} and d["session"]["chars"] == {"Malgruk": True}
    assert [s[0] for s in g.values(d["session"]["samples"])] == [0, 400]

    # /reload: PLAYER_LOGOUT pauses and remembers the running session, the new load resumes it.
    lua.execute('fire("PLAYER_LOGOUT")')
    d = db(lua)
    assert d["resume"] is True and d["elapsed"] == 400 and d["updated"]
    load_addon(lua)
    lua.execute('fire("PLAYER_ENTERING_WORLD", false, true); now = now + 100')
    assert any("weiter" in p for p in g.values(upgrades.lua_to_py(lua.globals().printed)))
    lua.execute('fire("PLAYER_LOGOUT")')
    assert db(lua)["elapsed"] == 500

    # Next /reload carries an app request: settings and a reset, which archives the session.
    request = '{ id = 7, action = "reset", settings = { scale = 5, tooltipItems = 3, priceSource = "vendor", autoStart = true, shown = false, herbSpells = { 1234 }, bogus = 1 } }'
    load_addon(lua, request)
    lua.execute('fire("PLAYER_ENTERING_WORLD", false, true)')
    d = db(lua)
    assert d["lastRequest"]["id"] == 7 and d["lastRequest"]["action"] == "reset" and "error" not in d["lastRequest"]
    assert d["settings"] == {"scale": 2, "tooltipItems": 3, "priceSource": "vendor", "autoStart": True}, d["settings"]
    assert d["hidden"] is True and d["herbSpells"] == {1234: True}
    assert d["elapsed"] == 0 and d["gold"]["loot"] == 0 and d.get("resume") is None
    hist = g.values(d["history"])
    assert len(hist) == 1 and hist[0]["elapsed"] == 500 and hist[0]["gold"]["loot"] == 3500
    assert hist[0]["items"]["loot"] == [{"id": 2447, "name": "Friedensblume", "quality": 1, "count": 3, "value": 3000}]

    # autoStart: the next loot window starts the session; priceSource vendor values the herb at 10 copper.
    mob_loot(lua, qty=2, coins=0)
    d = db(lua)
    assert d["gold"]["loot"] == 20, d["gold"]

    # The same request id never runs twice, even after another /reload.
    lua.execute('fire("PLAYER_LOGOUT")')
    load_addon(lua, request)
    lua.execute('fire("PLAYER_ENTERING_WORLD", false, true)')
    d = db(lua)
    assert len(g.values(d["history"])) == 1 and d["gold"]["loot"] == 20

    # Unknown actions are reported back, not raised.
    lua.execute('fire("PLAYER_LOGOUT")')
    load_addon(lua, '{ id = 8, action = "explode" }')
    lua.execute('fire("PLAYER_ENTERING_WORLD", false, true)')
    assert "unknown action" in db(lua)["lastRequest"]["error"]

    # The app's view of the same data.
    lua.execute('fire("PLAYER_LOGOUT")')
    out = g.from_db(db(lua))
    assert out["settings"]["shown"] is False and out["settings"]["herbSpells"] == [1234]
    assert out["current"]["total"] == 20 and out["current"]["runningAtSave"] is True
    h = out["history"][0]
    assert h["total"] == 3500 and h["perHour"] == 3500 / 500 * 3600 and h["zones"] == [{"zone": "Brachland", "value": 3500}]
    assert h["categories"]["loot"]["itemCount"] == 3 and h["chars"] == ["Malgruk"]
    assert out["summary"]["sessions"] == 1 and out["summary"]["best"] == h["started"]
    print("ok test_session_reload_request_and_history")


def test_herb_gathering():
    lua = new_runtime()
    load_addon(lua)
    lua.execute("""fire("PLAYER_ENTERING_WORLD", true, false)
      SlashCmdList.GOLDPERHOUR("start")
      fire("UNIT_SPELLCAST_SENT", "player", nil, nil, 2366)
      loot = { { link = LINK, qty = 2, guid = "GameObject-0-1-2-3-4-5" } }
      fire("LOOT_READY"); fire("LOOT_SLOT_CLEARED", 1); loot = {}; fire("LOOT_CLOSED")""")
    d = db(lua)
    assert d["gold"]["herb"] == 2000 and d["gold"]["loot"] == 0
    print("ok test_herb_gathering")


def test_login_does_not_resume():
    lua = new_runtime()
    load_addon(lua)
    lua.execute('fire("PLAYER_ENTERING_WORLD", true, false); SlashCmdList.GOLDPERHOUR("start"); now = now + 60; fire("PLAYER_LOGOUT")')
    load_addon(lua)
    lua.execute('fire("PLAYER_ENTERING_WORLD", true, false); now = now + 60; fire("PLAYER_LOGOUT")')
    d = db(lua)
    assert d["elapsed"] == 60 and d.get("resume") is None
    print("ok test_login_does_not_resume")


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
    test_session_reload_request_and_history()
    test_herb_gathering()
    test_login_does_not_resume()
    test_old_savedvariables()
