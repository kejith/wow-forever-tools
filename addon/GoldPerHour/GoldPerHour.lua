local ADDON = ...

local HERB, LOOT = "herb", "loot"
local CATEGORIES = { HERB, LOOT }

local CAST_WINDOW = 15      -- seconds between the gathering cast and its loot
local MONEY_GRACE = 1.5     -- seconds after LOOT_CLOSED in which PLAYER_MONEY still counts as loot
local HERB_GATHERING = 2366 -- retail spell ID; add others with /gph herbid <id>
local SAMPLE_EVERY = 300    -- seconds of session time between two points of the gold curve
local MAX_SAMPLES = 288     -- 24 h at 5 min; above that every second point is dropped
local MAX_HISTORY = 100     -- finished sessions kept for the WoWMCP app

-- Settings the WoWMCP app can change through AppRequest.lua (window visibility and herb spells live in
-- db.hidden and db.herbSpells, as before 0.3).
local DEFAULTS = {
    scale = 1,                 -- window scale
    tooltipItems = 6,          -- items listed per category in the tooltip
    priceSource = "auctionator", -- "auctionator" (vendor price as fallback) or "vendor"
    resumeAfterReload = true,  -- a running session keeps running across /reload
    autoStart = false,         -- the first loot window starts a stopped session
}

local ICON = {
    addon = "Interface\\Icons\\INV_Misc_Coin_01",
    [HERB] = "Interface\\Icons\\Trade_Herbalism",
    [LOOT] = "Interface\\Icons\\INV_Misc_Bag_08",
    total = "Interface\\Icons\\INV_Misc_Coin_02",
}

local L = {
    herb = "Herbs", loot = "Loot", total = "Total", perHour = "per hour",
    start = "Start", resume = "Resume", stop = "Stop", reset = "Reset",
    session = "Session", running = "running", paused = "paused",
    coins = "Coins", nothing = "Nothing yet", more = "+%d more",
    prices = "Prices", auction = "Auctionator, vendor price as fallback",
    vendorOnly = "vendor only (Auctionator not loaded)",
    vendorSetting = "vendor price (setting)",
    applied = "settings from the WoWMCP app applied",
    resumed = "session continues after /reload",
    hint = "Drag to move",
    resetConfirm = "Reset the GoldPerHour session?",
    hidden = "window hidden, /gph shows it again",
    herbAdded = "spell %d now counts as herb gathering",
    usage = "/gph [start | stop | reset | show | hide | debug | herbid <spellID>]",
}
if GetLocale() == "deDE" then
    L.herb, L.loot, L.total, L.perHour = "Kräuter", "Loot", "Gesamt", "pro Stunde"
    L.start, L.resume, L.stop, L.reset = "Start", "Weiter", "Stop", "Reset"
    L.session, L.running, L.paused = "Sitzung", "läuft", "pausiert"
    L.coins, L.nothing, L.more = "Münzen", "Noch nichts", "+%d weitere"
    L.prices, L.auction = "Preise", "Auctionator, sonst Händlerpreis"
    L.vendorOnly = "nur Händlerpreis (Auctionator nicht geladen)"
    L.vendorSetting = "Händlerpreis (Einstellung)"
    L.applied = "Einstellungen aus der WoWMCP-App übernommen"
    L.resumed = "Sitzung läuft nach /reload weiter"
    L.hint = "Ziehen zum Verschieben"
    L.resetConfirm = "GoldPerHour-Sitzung zurücksetzen?"
    L.hidden = "Fenster versteckt, /gph zeigt es wieder"
    L.herbAdded = "Zauber %d zählt jetzt als Kräutersammeln"
end

local db
local running, startedAt = false, 0
local debug = false
local lastMoney
local lastCast = { at = 0, herb = false }
local loot = { open = false, category = nil, moneyUntil = 0, slots = {} }

local SLOT_ITEM = Enum.LootSlotType and Enum.LootSlotType.Item or 1

local function Print(...)
    print("|cffffd100GoldPerHour:|r", ...)
end

-- Midnight hands some values to addons as opaque "secrets"; never compare those.
local function IsSecret(value)
    return issecretvalue ~= nil and issecretvalue(value)
end

local function Elapsed()
    return db.elapsed + (running and (GetTime() - startedAt) or 0)
end

local function Setting(key)
    local value = db and db.settings[key]
    if value == nil then return DEFAULTS[key] end
    return value
end

-- Metadata of the running session for the app: wall-clock start, zones, characters and the gold curve.
local function NewSession()
    return { zones = {}, chars = {}, samples = {} }
end

local function Sample()
    local s = db.session.samples
    local t = math.floor(Elapsed())
    local point = { t, math.floor(db.gold[HERB] + 0.5), math.floor(db.gold[LOOT] + 0.5) }
    if s[#s] and s[#s][1] == t then
        s[#s] = point
    else
        s[#s + 1] = point
    end
    if #s > MAX_SAMPLES then
        local thinned = {}
        for i = 1, #s, 2 do thinned[#thinned + 1] = s[i] end
        if thinned[#thinned] ~= s[#s] then thinned[#thinned + 1] = s[#s] end
        db.session.samples = thinned
    end
end

-- Where and by whom the gold was earned; zone and name are taken when the loot is picked up.
local function Where()
    local zone = GetRealZoneText()
    if not zone or zone == "" or IsSecret(zone) then zone = "?" end
    return zone, (UnitName("player"))
end

local function Attribute(zone, char, copper)
    db.session.zones[zone] = (db.session.zones[zone] or 0) + copper
    if char then db.session.chars[char] = true end
    db.session.lastActive = time()
end

---------------------------------------------------------------------------
-- Formatting
---------------------------------------------------------------------------
local function Money(copper)
    copper = math.floor(copper + 0.5)
    if copper >= 10000 then
        copper = copper - copper % 100 -- copper is noise once gold is involved
    end
    return GetMoneyString(copper, true)
end

local function PerHour(copper)
    local elapsed = Elapsed()
    if elapsed < 1 then return 0 end
    return copper / elapsed * 3600
end

local function Clock(seconds)
    seconds = math.floor(seconds)
    return ("%d:%02d:%02d"):format(math.floor(seconds / 3600), math.floor(seconds % 3600 / 60), seconds % 60)
end

local function IconText(texture)
    return ("|T%s:14|t"):format(tostring(texture))
end

---------------------------------------------------------------------------
-- Prices
---------------------------------------------------------------------------
local function AuctionatorAPI()
    return Auctionator and Auctionator.API and Auctionator.API.v1
end

local function AuctionPrice(link)
    local api = AuctionatorAPI()
    if not api then return nil end
    local ok, price = pcall(api.GetAuctionPriceByItemLink, ADDON, link)
    if ok and type(price) == "number" and price > 0 then return price end
end

-- Only call once the item data is loaded. Returns the value of one item and its quality.
local function ItemValue(link)
    local _, _, quality, _, _, _, _, _, _, _, sellPrice, _, _, bindType = C_Item.GetItemInfo(link)
    sellPrice = sellPrice or 0
    -- Poor quality (0) and bind-on-pickup (1) cannot be auctioned.
    if quality == 0 or bindType == 1 or Setting("priceSource") == "vendor" then return sellPrice, quality end
    return AuctionPrice(link) or sellPrice, quality
end

---------------------------------------------------------------------------
-- Window
---------------------------------------------------------------------------
local WIDTH, PAD, COLUMN = 260, 8, 86

local frame = CreateFrame("Frame", "GoldPerHourFrame", UIParent, "BackdropTemplate")
frame:SetSize(WIDTH, 136)
frame:SetClampedToScreen(true)
frame:SetMovable(true)
frame:EnableMouse(true)
frame:RegisterForDrag("LeftButton")
frame:SetBackdrop({
    bgFile = "Interface\\Tooltips\\UI-Tooltip-Background",
    edgeFile = "Interface\\Tooltips\\UI-Tooltip-Border",
    edgeSize = 12, insets = { left = 3, right = 3, top = 3, bottom = 3 },
})
frame:SetBackdropColor(0, 0, 0, 0.85)

local titleIcon = frame:CreateTexture(nil, "ARTWORK")
titleIcon:SetSize(16, 16)
titleIcon:SetPoint("TOPLEFT", PAD, -7)
titleIcon:SetTexture(ICON.addon)

local title = frame:CreateFontString(nil, "OVERLAY", "GameFontNormal")
title:SetPoint("LEFT", titleIcon, "RIGHT", 5, 0)
title:SetText("GoldPerHour")

local closeButton = CreateFrame("Button", nil, frame, "UIPanelCloseButton")
closeButton:SetSize(22, 22)
closeButton:SetPoint("TOPRIGHT", -2, -2)

local timeText = frame:CreateFontString(nil, "OVERLAY", "GameFontHighlight")
timeText:SetPoint("RIGHT", closeButton, "LEFT", -2, 0)

local function RightAligned(font, x, y, text)
    local fs = frame:CreateFontString(nil, "OVERLAY", font)
    fs:SetPoint("RIGHT", frame, "TOPRIGHT", x, y)
    fs:SetJustifyH("RIGHT")
    if text then fs:SetText(text) end
    return fs
end

RightAligned("GameFontDisableSmall", -PAD - COLUMN - 6, -34, "Gold")
RightAligned("GameFontDisableSmall", -PAD, -34, L.perHour)

local function MakeRow(key, y, font)
    local icon = frame:CreateTexture(nil, "ARTWORK")
    icon:SetSize(14, 14)
    icon:SetPoint("TOPLEFT", PAD, y)
    icon:SetTexture(ICON[key])
    icon:SetTexCoord(0.08, 0.92, 0.08, 0.92)

    local label = frame:CreateFontString(nil, "OVERLAY", font)
    label:SetPoint("LEFT", icon, "RIGHT", 4, 0)
    label:SetText(L[key])

    return {
        total = RightAligned(font, -PAD - COLUMN - 6, y - 7),
        perHour = RightAligned(font, -PAD, y - 7),
    }
end

local rows = {
    [HERB] = MakeRow(HERB, -44, "GameFontHighlightSmall"),
    [LOOT] = MakeRow(LOOT, -61, "GameFontHighlightSmall"),
    total = MakeRow("total", -83, "GameFontNormalSmall"),
}

local separator = frame:CreateTexture(nil, "ARTWORK")
separator:SetColorTexture(1, 1, 1, 0.15)
separator:SetHeight(1)
separator:SetPoint("TOPLEFT", PAD, -79)
separator:SetPoint("TOPRIGHT", -PAD, -79)

local buttonWidth = (WIDTH - 2 * PAD - 6) / 2
local toggleButton = CreateFrame("Button", nil, frame, "UIPanelButtonTemplate")
toggleButton:SetSize(buttonWidth, 22)
toggleButton:SetPoint("BOTTOMLEFT", PAD, PAD)
local resetButton = CreateFrame("Button", nil, frame, "UIPanelButtonTemplate")
resetButton:SetSize(buttonWidth, 22)
resetButton:SetPoint("BOTTOMRIGHT", -PAD, PAD)
resetButton:SetText(L.reset)

---------------------------------------------------------------------------
-- Tooltip
---------------------------------------------------------------------------
local function SortedItems(category)
    local list = {}
    for _, entry in pairs(db.items[category]) do
        list[#list + 1] = entry
    end
    table.sort(list, function(a, b)
        if a.value ~= b.value then return a.value > b.value end
        return a.id < b.id
    end)
    return list
end

local function AddCategory(category)
    GameTooltip:AddLine(" ")
    GameTooltip:AddDoubleLine(IconText(ICON[category]) .. " " .. L[category], Money(db.gold[category]),
        1, 0.82, 0, 1, 1, 1)

    local items = SortedItems(category)
    local shown = Setting("tooltipItems")
    for i = 1, math.min(#items, shown) do
        local entry = items[i]
        local icon = C_Item.GetItemIconByID(entry.id)
        GameTooltip:AddDoubleLine(("%s %s x%d"):format(icon and IconText(icon) or "", entry.link, entry.count),
            Money(entry.value), 1, 1, 1, 1, 1, 1)
    end
    if #items > shown then
        GameTooltip:AddLine(L.more:format(#items - shown), 0.6, 0.6, 0.6)
    end
    if db.coin[category] > 0 then
        GameTooltip:AddDoubleLine(L.coins, Money(db.coin[category]), 1, 1, 1, 1, 1, 1)
    end
    if #items == 0 and db.coin[category] == 0 then
        GameTooltip:AddLine(L.nothing, 0.6, 0.6, 0.6)
    end
end

local function FillTooltip()
    GameTooltip:ClearLines()
    GameTooltip:AddLine("GoldPerHour")
    GameTooltip:AddDoubleLine(L.session, ("%s (%s)"):format(Clock(Elapsed()), running and L.running or L.paused),
        1, 1, 1, 1, 1, 1)
    for _, category in ipairs(CATEGORIES) do
        AddCategory(category)
    end
    GameTooltip:AddLine(" ")
    if Setting("priceSource") == "vendor" then
        GameTooltip:AddDoubleLine(L.prices, L.vendorSetting, 0.6, 0.6, 0.6, 0.6, 0.6, 0.6)
    elseif AuctionatorAPI() then
        GameTooltip:AddDoubleLine(L.prices, L.auction, 0.6, 0.6, 0.6, 0.6, 0.6, 0.6)
    else
        GameTooltip:AddDoubleLine(L.prices, L.vendorOnly, 0.6, 0.6, 0.6, 1, 0.3, 0.3)
    end
    GameTooltip:AddLine(L.hint, 0.6, 0.6, 0.6)
    GameTooltip:Show()
end

local function ShowTooltip()
    GameTooltip:SetOwner(frame, "ANCHOR_NONE")
    if (frame:GetBottom() or 0) > 260 then
        GameTooltip:SetPoint("TOPLEFT", frame, "BOTTOMLEFT", 0, -2)
    else
        GameTooltip:SetPoint("BOTTOMLEFT", frame, "TOPLEFT", 0, 2)
    end
    FillTooltip()
end

---------------------------------------------------------------------------
-- Display
---------------------------------------------------------------------------
local function SetRow(row, copper)
    row.total:SetText(Money(copper))
    row.perHour:SetText(Money(PerHour(copper)))
end

local function Refresh()
    if not db then return end
    local herb, other = db.gold[HERB], db.gold[LOOT]
    SetRow(rows[HERB], herb)
    SetRow(rows[LOOT], other)
    SetRow(rows.total, herb + other)

    timeText:SetText(Clock(Elapsed()))
    if running then
        timeText:SetTextColor(0.3, 1, 0.3)
    else
        timeText:SetTextColor(0.6, 0.6, 0.6)
    end
    toggleButton:SetText(running and L.stop or (db.elapsed > 0 and L.resume or L.start))

    if GameTooltip:IsOwned(frame) then FillTooltip() end
end

local function SetVisible(shown)
    db.hidden = not shown or nil
    frame:SetShown(shown)
    if not shown then Print(L.hidden) end
end

---------------------------------------------------------------------------
-- Session
---------------------------------------------------------------------------
local nextSample = 0

local function Start()
    if running then return end
    running, startedAt = true, GetTime()
    db.session.started = db.session.started or time()
    local _, char = Where()
    if char then db.session.chars[char] = true end
    nextSample = (math.floor(Elapsed() / SAMPLE_EVERY) + 1) * SAMPLE_EVERY
    Sample()
    Refresh()
end

local function Stop()
    if not running then return end
    db.elapsed = Elapsed()
    running = false
    Sample()
    Refresh()
end

-- A finished session goes into db.history for the app; the item links are reduced to name and quality.
local function Archive()
    if db.elapsed < 1 and db.gold[HERB] + db.gold[LOOT] == 0 then return end
    local entry = {
        started = db.session.started, ended = db.session.lastActive or time(), archived = time(),
        elapsed = math.floor(db.elapsed), gold = {}, coin = {}, items = {},
        zones = db.session.zones, chars = db.session.chars, samples = db.session.samples,
    }
    for _, category in ipairs(CATEGORIES) do
        entry.gold[category] = math.floor(db.gold[category] + 0.5)
        entry.coin[category] = db.coin[category]
        local list = {}
        for _, item in ipairs(SortedItems(category)) do
            list[#list + 1] = { id = item.id, name = item.link:match("%[(.-)%]"), quality = item.quality,
                count = item.count, value = math.floor(item.value + 0.5) }
        end
        entry.items[category] = list
    end
    table.insert(db.history, entry)
    while #db.history > MAX_HISTORY do table.remove(db.history, 1) end
end

local function Reset()
    if running then
        db.elapsed = Elapsed()
        running = false
    end
    Archive()
    db.elapsed = 0
    db.session = NewSession()
    for _, category in ipairs(CATEGORIES) do
        db.gold[category], db.coin[category] = 0, 0
        wipe(db.items[category])
    end
    Refresh()
end

StaticPopupDialogs["GOLDPERHOUR_RESET"] = {
    text = L.resetConfirm,
    button1 = YES,
    button2 = NO,
    OnAccept = Reset,
    timeout = 0,
    whileDead = true,
    hideOnEscape = true,
    preferredIndex = 3,
}

---------------------------------------------------------------------------
-- Crediting
---------------------------------------------------------------------------
local function CreditMoney(category, copper)
    db.coin[category] = db.coin[category] + copper
    db.gold[category] = db.gold[category] + copper
    local zone, char = Where()
    Attribute(zone, char, copper)
    Refresh()
end

local function CreditItem(category, link, quantity)
    local item = Item:CreateFromItemLink(link)
    if item:IsItemEmpty() then return end
    local zone, char = Where()
    -- Sell price and quality are only available once the item is cached.
    item:ContinueOnItemLoad(function()
        local id = item:GetItemID()
        local unit, quality = ItemValue(link)
        local value = unit * quantity
        local entry = db.items[category][id]
        if not entry then
            entry = { id = id, link = link, count = 0, value = 0 }
            db.items[category][id] = entry
        end
        entry.quality = quality
        entry.count = entry.count + quantity
        entry.value = entry.value + value
        db.gold[category] = db.gold[category] + value
        Attribute(zone, char, value)
        Refresh()
    end)
end

---------------------------------------------------------------------------
-- Classification
---------------------------------------------------------------------------
local function IsHerbSpell(spellID)
    if spellID == HERB_GATHERING or db.herbSpells[spellID] then return true end
    -- Other ranks or variants share the localized name of the base spell.
    local herbName = C_Spell.GetSpellName(HERB_GATHERING)
    return herbName ~= nil and C_Spell.GetSpellName(spellID) == herbName
end

-- Herbs: the last cast was a herb gathering cast and every slot comes from a world object.
local function ClassifyLoot(slotCount)
    if not lastCast.herb or GetTime() - lastCast.at > CAST_WINDOW then return LOOT end
    local seen = false
    for slot = 1, slotCount do
        if LootSlotHasItem(slot) then
            local guid = GetLootSourceInfo(slot)
            if not guid or IsSecret(guid) or strsplit("-", guid) ~= "GameObject" then return LOOT end
            seen = true
        end
    end
    return seen and HERB or LOOT
end

-- LOOT_READY and LOOT_OPENED both land here and may repeat, so this must be idempotent.
local function SnapshotLoot()
    local slotCount = GetNumLootItems()
    if not loot.open then
        loot.open = true
        wipe(loot.slots)
        if not running and Setting("autoStart") then Start() end
        loot.category = ClassifyLoot(slotCount)
        if debug then
            Print("loot:", loot.category, "slots:", slotCount, "source:", (GetLootSourceInfo(1)))
        end
    end
    for slot = 1, slotCount do
        if not loot.slots[slot] and GetLootSlotType(slot) == SLOT_ITEM then
            local link = GetLootSlotLink(slot)
            local _, _, quantity = GetLootSlotInfo(slot)
            if link then
                loot.slots[slot] = { link = link, quantity = (quantity and quantity > 0) and quantity or 1 }
            end
        end
    end
end

---------------------------------------------------------------------------
-- Events
---------------------------------------------------------------------------
local handlers = {}

function handlers.ADDON_LOADED(name)
    if name ~= ADDON then return end
    frame:UnregisterEvent("ADDON_LOADED")

    GoldPerHourDB = GoldPerHourDB or {}
    db = GoldPerHourDB
    db.elapsed = db.elapsed or 0
    db.herbSpells = db.herbSpells or {}
    db.gold, db.coin, db.items = db.gold or {}, db.coin or {}, db.items or {}
    for _, category in ipairs(CATEGORIES) do
        db.gold[category] = db.gold[category] or 0
        db.coin[category] = db.coin[category] or 0
        db.items[category] = db.items[category] or {}
    end
    db.settings = db.settings or {}
    db.history = db.history or {}
    db.session = db.session or NewSession()
    db.session.zones = db.session.zones or {}
    db.session.chars = db.session.chars or {}
    db.session.samples = db.session.samples or {}
    db.version = C_AddOns and C_AddOns.GetAddOnMetadata and C_AddOns.GetAddOnMetadata(ADDON, "Version") or nil

    frame:SetScale(Setting("scale"))
    frame:ClearAllPoints()
    if db.pos then
        frame:SetPoint(db.pos[1], UIParent, db.pos[2], db.pos[3], db.pos[4])
    else
        frame:SetPoint("CENTER", UIParent, "CENTER", 0, 200)
    end
    frame:SetShown(not db.hidden)
    Refresh()
end

function handlers.PLAYER_LOGIN()
    lastMoney = GetMoney()
end

-- Requests from the WoWMCP app (AppRequest.lua in this folder, read by the client on /reload).
-- Each id runs once; the result goes to db.lastRequest, which the app reads after the next /reload.
local function ApplySettings(s)
    for key, default in pairs(DEFAULTS) do
        local value = s[key]
        if value ~= nil and type(value) == type(default) then db.settings[key] = value end
    end
    db.settings.scale = math.min(2, math.max(0.5, Setting("scale")))
    db.settings.tooltipItems = math.min(30, math.max(1, math.floor(Setting("tooltipItems"))))
    if db.settings.priceSource ~= "vendor" then db.settings.priceSource = "auctionator" end
    if type(s.herbSpells) == "table" then
        wipe(db.herbSpells)
        for _, id in ipairs(s.herbSpells) do
            if tonumber(id) then db.herbSpells[tonumber(id)] = true end
        end
    end
    frame:SetScale(Setting("scale"))
    if s.shown ~= nil then
        db.hidden = not s.shown or nil
        frame:SetShown(s.shown and true or false)
    end
end

local ACTIONS = { start = function() Start() end, stop = function() Stop() end, reset = function() Reset() end }

local function RunAppRequest()
    local r = GoldPerHour_AppRequest
    if type(r) ~= "table" or type(r.id) ~= "number" or r.id == db.lastRequestId then return end
    db.lastRequestId = r.id
    local result = { id = r.id, action = r.action, done = time() }
    local ok, err = pcall(function()
        if type(r.settings) == "table" then
            ApplySettings(r.settings)
            result.settings = true
            Print(L.applied)
        end
        if r.action ~= nil then
            local fn = ACTIONS[r.action]
            if not fn then error("unknown action " .. tostring(r.action)) end
            fn()
        end
    end)
    if not ok then result.error = tostring(err) end
    db.lastRequest = result
    Refresh()
end

local entered = false
function handlers.PLAYER_ENTERING_WORLD(_, isReloadingUi)
    if entered then return end -- fires again on every loading screen
    entered = true
    if isReloadingUi and db.resume and Setting("resumeAfterReload") then
        Start()
        Print(L.resumed)
    end
    db.resume = nil
    RunAppRequest()
end

function handlers.PLAYER_LOGOUT()
    -- GetTime() does not survive a logout, so the session pauses. On /reload it continues (resumeAfterReload).
    db.resume = running or nil
    Stop()
    db.updated = time()
end

function handlers.UNIT_SPELLCAST_SENT(unit, _, _, spellID)
    if unit ~= "player" then return end
    local secret = IsSecret(spellID)
    lastCast.at = GetTime()
    lastCast.herb = not secret and IsHerbSpell(spellID)
    if debug and not secret then
        Print("cast:", spellID, C_Spell.GetSpellName(spellID), lastCast.herb and "(herb)" or "")
    end
end

handlers.LOOT_READY = SnapshotLoot
handlers.LOOT_OPENED = SnapshotLoot

function handlers.LOOT_SLOT_CLEARED(slot)
    local entry = loot.slots[slot]
    if not entry then return end
    loot.slots[slot] = nil
    if running then
        CreditItem(loot.category, entry.link, entry.quantity)
    end
end

function handlers.LOOT_CLOSED()
    -- Slots are wiped on the next open, so a late LOOT_SLOT_CLEARED still counts.
    loot.open = false
    loot.moneyUntil = GetTime() + MONEY_GRACE
    lastCast.herb = false
end

function handlers.PLAYER_MONEY()
    local money = GetMoney()
    local delta = lastMoney and (money - lastMoney) or 0
    lastMoney = money
    if running and delta > 0 and loot.category and (loot.open or GetTime() <= loot.moneyUntil) then
        CreditMoney(loot.category, delta)
    end
end

frame:SetScript("OnEvent", function(_, event, ...)
    handlers[event](...)
end)
for event in pairs(handlers) do
    frame:RegisterEvent(event)
end

---------------------------------------------------------------------------
-- Interaction
---------------------------------------------------------------------------
frame:SetScript("OnDragStart", frame.StartMoving)
frame:SetScript("OnDragStop", function(self)
    self:StopMovingOrSizing()
    local point, _, relativePoint, x, y = self:GetPoint()
    db.pos = { point, relativePoint, x, y }
end)
frame:SetScript("OnEnter", ShowTooltip)
frame:SetScript("OnLeave", function()
    GameTooltip:Hide()
end)

local sinceRefresh = 0
frame:SetScript("OnUpdate", function(_, dt)
    sinceRefresh = sinceRefresh + dt
    if sinceRefresh >= 0.5 then
        sinceRefresh = 0
        Refresh()
    end
end)

-- The gold curve also grows while the window is hidden (OnUpdate only runs for shown frames).
C_Timer.NewTicker(5, function()
    if running and Elapsed() >= nextSample then
        Sample()
        nextSample = nextSample + SAMPLE_EVERY
    end
end)

closeButton:SetScript("OnClick", function()
    SetVisible(false)
end)
toggleButton:SetScript("OnClick", function()
    if running then Stop() else Start() end
end)
resetButton:SetScript("OnClick", function()
    if Elapsed() > 0 or db.gold[HERB] + db.gold[LOOT] > 0 then
        StaticPopup_Show("GOLDPERHOUR_RESET")
    end
end)

function GoldPerHour_OnAddonCompartmentClick()
    SetVisible(not frame:IsShown())
end

SLASH_GOLDPERHOUR1 = "/gph"
SLASH_GOLDPERHOUR2 = "/goldperhour"
SlashCmdList.GOLDPERHOUR = function(msg)
    local cmd, arg = msg:match("^%s*(%S*)%s*(.-)%s*$")
    cmd = cmd:lower()
    if cmd == "" then
        SetVisible(not frame:IsShown())
    elseif cmd == "start" then
        Start()
    elseif cmd == "stop" then
        Stop()
    elseif cmd == "reset" then
        Reset()
    elseif cmd == "show" then
        SetVisible(true)
    elseif cmd == "hide" then
        SetVisible(false)
    elseif cmd == "debug" then
        debug = not debug
        Print("debug", debug and "on" or "off")
    elseif cmd == "herbid" and tonumber(arg) then
        db.herbSpells[tonumber(arg)] = true
        Print(L.herbAdded:format(tonumber(arg)))
    else
        Print(L.usage)
    end
end
