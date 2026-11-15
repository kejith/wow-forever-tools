-- WoWMCP: snapshots character state into per-character SavedVariables for an external MCP server.
-- Target: WoW Forever (Interface 16001, Mainline API family). Addons cannot do network or file I/O,
-- so the snapshot reaches disk only when the client writes SavedVariables on /reload or logout.

local ADDON_NAME = ...
local ADDON_VERSION = C_AddOns and C_AddOns.GetAddOnMetadata(ADDON_NAME, "Version") or "?"
local SCHEMA = 1
local THROTTLE_SECONDS = 2

-- Legacy trait trees on Forever 1.60.1 (Constants.LegacyConsts).
local LEGACY_TREES = { [1187] = "Professions", [1188] = "Adventure", [1189] = "Progression" }
local PVP_RANK_FACTION = 2800

local STATS = { "strength", "agility", "stamina", "intellect", "spirit" }
local SPELL_SCHOOLS = { [2] = "holy", [3] = "fire", [4] = "nature", [5] = "frost", [6] = "shadow", [7] = "arcane" }
local RESISTANCES = { [1] = "holy", [2] = "fire", [3] = "nature", [4] = "frost", [5] = "shadow", [6] = "arcane" }

---------------------------------------------------------------------------------------------------
-- Helpers
---------------------------------------------------------------------------------------------------

local function unpackOk(ok, ...)
    if ok then return ... end
end

-- Calls fn protected; returns its results, or nothing if fn is missing or raises.
local function try(fn, ...)
    if type(fn) ~= "function" then return end
    return unpackOk(pcall(fn, ...))
end

local function isSecret(v)
    return issecretvalue ~= nil and issecretvalue(v) or false
end

local function num(v)
    if type(v) == "number" and not isSecret(v) then return v end
end

local function str(v)
    if type(v) == "string" and not isSecret(v) and v ~= "" then return v end
end

-- Shallow copy of string/number/boolean fields, dropping secret values and nested tables.
local function copyPlain(t)
    if type(t) ~= "table" then return nil end
    local out = {}
    for k, v in pairs(t) do
        local tv = type(v)
        if (tv == "string" or tv == "number" or tv == "boolean") and not isSecret(v) then
            out[k] = v
        end
    end
    return out
end

local function itemRecord(link, itemID, count)
    local name, _, quality, itemLevel, reqLevel, itemType, itemSubType, _, equipLoc =
        try(C_Item.GetItemInfo, link or itemID)
    return {
        id = itemID,
        link = link,
        -- GetItemInfo returns nil for items not yet in the client cache; the link always carries the name.
        name = name or (link and link:match("%[(.-)%]")),
        count = count or 1,
        quality = quality,
        ilvl = itemLevel,
        reqLevel = (num(reqLevel) and reqLevel > 1) and reqLevel or nil,
        type = str(itemType),
        subType = str(itemSubType),
        equipLoc = str(equipLoc),
    }
end

---------------------------------------------------------------------------------------------------
-- Character and stats
---------------------------------------------------------------------------------------------------

local function sumAttackPower(base, pos, neg)
    if num(base) then return base + (num(pos) or 0) + (num(neg) or 0) end
end

local function collectStats()
    local s = { spellDamage = {}, spellCrit = {}, resistances = {} }
    for i, key in ipairs(STATS) do
        local _, effective = try(UnitStat, "player", i)
        s[key] = num(effective)
    end
    local _, armor = try(UnitArmor, "player")
    s.armor = num(armor)
    s.attackPower = sumAttackPower(try(UnitAttackPower, "player"))
    s.rangedAttackPower = sumAttackPower(try(UnitRangedAttackPower, "player"))
    s.meleeCrit = num(try(GetCritChance))
    s.rangedCrit = num(try(GetRangedCritChance))
    s.meleeHit = num(try(GetHitModifier))
    s.spellHit = num(try(GetSpellHitModifier))
    s.dodge = num(try(GetDodgeChance))
    s.parry = num(try(GetParryChance))
    s.block = num(try(GetBlockChance))
    s.healing = num(try(GetSpellBonusHealing))
    for school, key in pairs(SPELL_SCHOOLS) do
        s.spellDamage[key] = num(try(GetSpellBonusDamage, school))
        s.spellCrit[key] = num(try(GetSpellCritChance, school))
    end
    for school, key in pairs(RESISTANCES) do
        local _, total = try(UnitResistance, "player", school)
        s.resistances[key] = num(total)
    end
    s.healthMax = num(try(UnitHealthMax, "player"))
    s.powerMax = num(try(UnitPowerMax, "player"))
    local _, powerToken = try(UnitPowerType, "player")
    s.powerType = str(powerToken)
    return s
end

local function collectPvpRank()
    local info = try(C_MajorFactions and C_MajorFactions.GetMajorFactionProgressionInfo, PVP_RANK_FACTION)
    local rank = info and num(info.renownLevel)
    if not rank then return nil end
    local factionIndex = UnitFactionGroup("player") == "Alliance" and 1 or 0
    return { rank = rank, title = _G[("PVP_RANK_%d_%d"):format(rank, factionIndex)] }
end

local function percent(v)
    if num(v) then return math.floor(v * 1000 + 0.5) / 10 end
end

local function collectCharacter()
    local first, surname = UnitName("player")
    local className, classFile = UnitClass("player")
    local raceName, raceFile = UnitRace("player")
    local guildName, guildRank = GetGuildInfo("player")
    local c = {
        name = first,
        surname = str(surname), -- Forever returns the surname where other clients return the realm
        level = UnitLevel("player"),
        class = className,
        classFile = classFile,
        race = raceName,
        raceFile = raceFile,
        faction = UnitFactionGroup("player"),
        sex = ({ [2] = "male", [3] = "female" })[UnitSex("player")],
        money = GetMoney(),
        xp = num(UnitXP("player")),
        xpMax = num(UnitXPMax("player")),
        rested = num(GetXPExhaustion()),
        zone = str(GetRealZoneText()),
        subzone = str(GetSubZoneText()),
        guild = guildName and { name = guildName, rank = guildRank } or nil,
        dead = UnitIsDeadOrGhost("player") or nil,
        pvpRank = collectPvpRank(),
        stats = collectStats(),
    }
    local mapID = try(C_Map.GetBestMapForUnit, "player")
    if mapID then
        local info = try(C_Map.GetMapInfo, mapID)
        c.map = { id = mapID, name = info and info.name }
        local pos = try(C_Map.GetPlayerMapPosition, mapID, "player")
        if pos then
            local x, y = try(pos.GetXY, pos)
            c.map.x, c.map.y = percent(x), percent(y)
        end
    end
    return c
end

---------------------------------------------------------------------------------------------------
-- Items
---------------------------------------------------------------------------------------------------

local function collectEquipment()
    local items = {}
    for slot = 0, 19 do
        local link = try(GetInventoryItemLink, "player", slot)
        if link then
            local rec = itemRecord(link, try(GetInventoryItemID, "player", slot), 1)
            rec.slot = slot
            local cur, max = try(GetInventoryItemDurability, slot)
            if num(cur) and num(max) then rec.durability = { cur, max } end
            rec.stats = copyPlain(try(C_Item.GetItemStats, link))
            items[#items + 1] = rec
        end
    end
    return items
end

local function bagIDs(match)
    local ids, seen = {}, {}
    for key, id in pairs(Enum.BagIndex or {}) do
        if type(key) == "string" and type(id) == "number" and not seen[id] and match(key) then
            seen[id] = true
            ids[#ids + 1] = id
        end
    end
    table.sort(ids)
    return ids
end

local function isCarriedBag(key)
    return key == "Backpack" or key == "ReagentBag" or key:match("^Bag_%d+$") ~= nil
end

-- Forever's bank container IDs are unverified (Retail moved from Bank/BankBag_n to CharacterBankTab_n),
-- so take every character bank index the client defines and skip the account (warband) bank.
local function isCharacterBank(key)
    local k = key:lower()
    return k:find("bank", 1, true) ~= nil and not k:find("account", 1, true) and not k:find("warband", 1, true)
end

local function scanContainer(bag)
    local size = try(C_Container.GetContainerNumSlots, bag)
    if not num(size) or size == 0 then return nil end
    local items = {}
    for slot = 1, size do
        local info = try(C_Container.GetContainerItemInfo, bag, slot)
        if info and info.itemID then
            local rec = itemRecord(info.hyperlink, info.itemID, info.stackCount)
            rec.slot = slot
            rec.bound = info.isBound or nil
            items[#items + 1] = rec
        end
    end
    return {
        id = bag,
        name = str(try(C_Container.GetBagName, bag)),
        size = size,
        free = num(try(C_Container.GetContainerNumFreeSlots, bag)),
        items = items,
    }
end

local function collectBags()
    local ids = bagIDs(isCarriedBag)
    if #ids == 0 then
        for bag = 0, NUM_BAG_SLOTS or 4 do ids[#ids + 1] = bag end
    end
    local containers = {}
    for _, bag in ipairs(ids) do
        containers[#containers + 1] = scanContainer(bag)
    end
    return containers
end

local bankOpen = false

-- Bank contents are only readable while the bank is open; the last scan persists across sessions.
local function scanBank()
    local db = WoWMCP_Char
    local containers = {}
    for _, bag in ipairs(bagIDs(isCharacterBank)) do
        containers[#containers + 1] = scanContainer(bag)
    end
    if #containers > 0 then
        db.bank = { updated = time(), containers = containers }
        db.errors.bank = nil
        return
    end
    local keys = {}
    for key in pairs(Enum.BagIndex or {}) do keys[#keys + 1] = tostring(key) end
    table.sort(keys)
    db.errors.bank = "no readable bank containers; Enum.BagIndex keys: " .. table.concat(keys, ",")
end

---------------------------------------------------------------------------------------------------
-- Quests
---------------------------------------------------------------------------------------------------

local function collectQuests()
    local quests, category = {}, nil
    local firstDescription, descriptionsDiffer = nil, false
    for index = 1, C_QuestLog.GetNumQuestLogEntries() do
        local info = C_QuestLog.GetInfo(index)
        if info and info.isHeader then
            category = info.title
        elseif info and not info.isHidden and (info.questID or 0) > 0 then
            local id = info.questID
            local q = {
                id = id,
                title = info.title,
                level = info.level,
                suggestedGroup = (num(info.suggestedGroup) or 0) > 0 and info.suggestedGroup or nil,
                category = category,
                complete = try(C_QuestLog.IsComplete, id) or nil,
                failed = try(C_QuestLog.IsFailed, id) or nil,
                rewardXP = num(try(GetQuestLogRewardXP, id)),
                rewardMoney = num(try(GetQuestLogRewardMoney, id)),
                objectives = {},
            }
            for _, o in ipairs(try(C_QuestLog.GetQuestObjectives, id) or {}) do
                q.objectives[#q.objectives + 1] = {
                    text = str(o.text),
                    finished = o.finished,
                    have = num(o.numFulfilled),
                    need = num(o.numRequired),
                }
            end
            local description, objectiveText = try(GetQuestLogQuestText, index)
            q.description, q.objectiveText = str(description), str(objectiveText)
            if q.description then
                if firstDescription == nil then
                    firstDescription = q.description
                elseif firstDescription ~= q.description then
                    descriptionsDiffer = true
                end
            end
            quests[#quests + 1] = q
        end
    end
    -- If GetQuestLogQuestText ignores the index it returns the selected quest's text for every entry;
    -- identical texts across several quests mean it did, so drop them rather than export wrong data.
    if #quests > 1 and firstDescription and not descriptionsDiffer then
        for _, q in ipairs(quests) do q.description, q.objectiveText = nil, nil end
    end
    return quests
end

local function collectCompletedQuests()
    local ids = try(C_QuestLog.GetAllCompletedQuestIDs)
    if type(ids) ~= "table" then return nil end
    table.sort(ids)
    return ids
end

---------------------------------------------------------------------------------------------------
-- Reputation, skills, talents
---------------------------------------------------------------------------------------------------

local function joinCategory(header, subheader)
    if header and subheader then return header .. " / " .. subheader end
    return subheader or header
end

local function collectReputation()
    -- Collapsed headers hide their factions from the index API; this expands the reputation panel.
    try(C_Reputation.ExpandAllFactionHeaders)
    local factions, header, subheader = {}, nil, nil
    for index = 1, C_Reputation.GetNumFactions() do
        local d = C_Reputation.GetFactionDataByIndex(index)
        if d then
            local category = joinCategory(header, subheader)
            if d.isHeader and not d.isChild then
                header, subheader = d.name, nil
            elseif d.isHeader then
                subheader = d.name
            end
            if not d.isHeader or d.isHeaderWithRep then
                factions[#factions + 1] = {
                    id = d.factionID,
                    name = d.name,
                    category = category,
                    reaction = d.reaction,
                    standing = _G["FACTION_STANDING_LABEL" .. tostring(d.reaction)],
                    value = num(d.currentStanding),
                    min = num(d.currentReactionThreshold),
                    max = num(d.nextReactionThreshold),
                    atWar = d.atWarWith or nil,
                }
            end
        end
    end
    return factions
end

local function collectSkills()
    local out = { professions = {}, skills = {} }
    for _, index in pairs({ try(GetProfessions) }) do
        local name, _, rank, maxRank, _, _, skillLine, modifier = try(GetProfessionInfo, index)
        if name then
            out.professions[#out.professions + 1] = {
                name = name,
                rank = num(rank),
                max = num(maxRank),
                modifier = (num(modifier) or 0) ~= 0 and modifier or nil,
                skillLine = skillLine,
            }
        end
    end
    -- C_SkillInfo backs Forever's Skills tab (weapon skills, languages, professions); no count
    -- function is documented, so read until the index runs out.
    local category
    for index = 1, 500 do
        local row = copyPlain(try(C_SkillInfo and C_SkillInfo.GetSkillLineInfo, index))
        if not row then break end
        if row.isHeader then
            category = row.name or row.skillName
        else
            row.category = category
            out.skills[#out.skills + 1] = row
        end
    end
    return out
end

local function exportTree(configID, treeID)
    local tree = { treeID = treeID, groups = {}, talents = {} }

    local currency = try(C_Traits.GetTreeCurrencyInfo, configID, treeID, false)
    if type(currency) == "table" and currency[1] then
        tree.spent, tree.unspent = num(currency[1].spent), num(currency[1].quantity)
    end

    -- On Forever the talent tabs (e.g. Arms/Fury/Protection) are trait groups within one tree.
    local groupName, groupIDs = {}, {}
    for _, g in ipairs(try(C_Traits.GetGroupDisplayInfoByTreeID, treeID) or {}) do
        groupName[g.groupID] = g.displayName
        groupIDs[#groupIDs + 1] = g.groupID
    end
    if #groupIDs > 0 then
        local spent = {}
        for _, e in ipairs(try(C_Traits.GetGroupCurrencyInfo, configID, groupIDs) or {}) do
            local info = e.currencyInfos and e.currencyInfos[1]
            if info then spent[e.traitNodeGroupID] = num(info.spent) end
        end
        for _, id in ipairs(groupIDs) do
            -- Groups without points are omitted from GetGroupCurrencyInfo's result.
            tree.groups[#tree.groups + 1] = { id = id, name = groupName[id], spent = spent[id] or 0 }
        end
    end

    for _, nodeID in ipairs(try(C_Traits.GetTreeNodes, treeID) or {}) do
        local node = try(C_Traits.GetNodeInfo, configID, nodeID)
        local rank = node and num(node.activeRank) or 0
        if rank > 0 then
            local entryID = (node.activeEntry and node.activeEntry.entryID) or (node.entryIDs and node.entryIDs[1])
            local entry = entryID and try(C_Traits.GetEntryInfo, configID, entryID)
            local definition = entry and entry.definitionID and try(C_Traits.GetDefinitionInfo, entry.definitionID)
            local spellID = definition and (definition.overriddenSpellID or definition.spellID)
            local name = definition and str(definition.overrideName)
            if not name and spellID then name = str(try(C_Spell.GetSpellName, spellID)) end
            -- A node's tab is whichever of its groupIDs is a display group, not necessarily the first.
            local group
            for _, gid in ipairs(node.groupIDs or {}) do
                if groupName[gid] then group = groupName[gid] break end
            end
            tree.talents[#tree.talents + 1] = {
                name = name,
                spellID = spellID,
                rank = rank,
                maxRank = num(node.maxRanks),
                group = group,
            }
        end
    end
    return tree
end

local function collectTalents()
    local out = { class = {}, legacy = {} }
    local configID = try(C_ClassTalents.GetActiveConfigID)
    local config = configID and try(C_Traits.GetConfigInfo, configID)
    if config then
        for _, treeID in ipairs(config.treeIDs or {}) do
            out.class[#out.class + 1] = exportTree(configID, treeID)
        end
    end
    for treeID, label in pairs(LEGACY_TREES) do
        local legacyConfigID = try(C_Traits.GetConfigIDByTreeID, treeID)
        if legacyConfigID then
            local tree = exportTree(legacyConfigID, treeID)
            tree.label = label
            out.legacy[#out.legacy + 1] = tree
        end
    end
    return out
end

---------------------------------------------------------------------------------------------------
-- Snapshot scheduling
---------------------------------------------------------------------------------------------------

local SECTIONS = {
    character = collectCharacter,
    equipment = collectEquipment,
    bags = collectBags,
    quests = collectQuests,
    completedQuests = collectCompletedQuests,
    reputation = collectReputation,
    skills = collectSkills,
    talents = collectTalents,
}

-- A failing section keeps its previous data and records the error for the server to report.
local function snapshot()
    local db = WoWMCP_Char
    for key, collect in pairs(SECTIONS) do
        local ok, result = pcall(collect)
        if ok and result ~= nil then
            db[key], db.errors[key] = result, nil
        else
            db.errors[key] = ok and "no data returned" or tostring(result)
        end
    end
    if bankOpen then pcall(scanBank) end
    local version, build, _, interface = GetBuildInfo()
    db.schema = SCHEMA
    db.updated = time()
    db.client = { version = version, build = tonumber(build), interface = interface }
    db.addon = { version = ADDON_VERSION }
end

local pending, dirty = false, false

local function inCombat()
    return InCombatLockdown() or UnitAffectingCombat("player")
end

local function runPending()
    pending = false
    -- Combat makes many values secret; PLAYER_REGEN_ENABLED re-requests the snapshot.
    if inCombat() then return end
    dirty = false
    snapshot()
end

local function requestSnapshot()
    dirty = true
    if not pending then
        pending = true
        C_Timer.After(THROTTLE_SECONDS, runPending)
    end
end

---------------------------------------------------------------------------------------------------
-- Item export ("/wowmcp items"): stats and tooltip text for equippable items seen in the auction house.
-- Account-wide and incremental: WoWMCP_Items keeps every item already read for the current client
-- build, so a run after the next AH scan only asks the server for items it has not seen yet.
---------------------------------------------------------------------------------------------------

-- Always included: Spellstones and wizard oils, so their tooltips (duration, slot) can be compared.
local EXTRA_ITEMS = { 5522, 13602, 13603, 20746, 20750 }
local ITEM_BATCH = 40       -- new item requests per tick
local ITEM_TICK = 0.25      -- seconds
local ITEM_IDLE_TICKS = 40  -- stop after ~10 s without progress
local AH_RECENT_DAYS = 4    -- Auctionator scan days that count as "currently listed"

local itemRun

local function clientBuild()
    local _, build = GetBuildInfo()
    return tonumber(build)
end

local function tooltipLines(itemID)
    local data = try(C_TooltipInfo.GetItemByID, itemID)
    if type(data) ~= "table" or type(data.lines) ~= "table" then return nil end
    local lines = {}
    for i = 2, #data.lines do -- line 1 is the item name
        local line = data.lines[i]
        local text, right = str(line.leftText), str(line.rightText)
        if right then text = (text or "") .. " | " .. right end
        if text then lines[#lines + 1] = text end
    end
    return lines
end

local function itemInfoRecord(itemID)
    local name, link, quality, itemLevel, reqLevel, _, _, _, equipLoc, _, _, classID, subclassID, bindType =
        try(C_Item.GetItemInfo, itemID)
    if not str(name) then return nil end
    return {
        name = name,
        link = str(link),
        quality = num(quality),
        ilvl = num(itemLevel),
        reqLevel = num(reqLevel),
        equipLoc = str(equipLoc),
        classID = num(classID),
        subclassID = num(subclassID),
        bind = num(bindType),
        stats = str(link) and copyPlain(try(C_Item.GetItemStats, link)) or nil,
        tooltip = tooltipLines(itemID),
    }
end

-- Armor and weapons that can be equipped; class and slot come from the client files, no server request.
local function isEquippable(itemID)
    local id, _, _, equipLoc, _, classID = try(C_Item.GetItemInfoInstant, itemID)
    return id ~= nil and (classID == 2 or classID == 4) and str(equipLoc) ~= nil
end

-- Item IDs Auctionator saw in its scans of the last AH_RECENT_DAYS days. Auctionator decodes its price
-- database on login, so this needs no generated file and no game restart.
local function auctionatorItems()
    local database = Auctionator and Auctionator.Database
    local db = database and database.db
    local day0 = Auctionator and Auctionator.Constants and Auctionator.Constants.SCAN_DAY_0
    if type(db) ~= "table" or not day0 then return nil end
    local cutoff = math.floor((time() - day0) / 86400) - AH_RECENT_DAYS + 1
    local ids = {}
    for key, data in pairs(db) do
        local id = tonumber(key)
        if id and type(data) == "table" then
            for day in pairs(data.h or {}) do
                if (tonumber(day) or 0) >= cutoff then
                    ids[#ids + 1] = id
                    break
                end
            end
        end
    end
    return ids
end

local function itemStore()
    local build = clientBuild()
    if type(WoWMCP_Items) ~= "table" or WoWMCP_Items.build ~= build then
        WoWMCP_Items = { build = build, items = {} }
    end
    WoWMCP_Items.items = type(WoWMCP_Items.items) == "table" and WoWMCP_Items.items or {}
    return WoWMCP_Items
end

local function finishItemRun()
    local run = itemRun
    itemRun = nil
    run.ticker:Cancel()
    local missing = {}
    for id in pairs(run.todo) do missing[#missing + 1] = id end
    table.sort(missing)
    local store = itemStore()
    local count = 0
    for _ in pairs(store.items) do count = count + 1 end
    store.updated, store.missing, store.count, store.source = time(), missing, count, run.source
    store.lastRun = { new = run.done, total = run.total, missing = #missing, started = run.started }
    if run.requestID then
        WoWMCP_Account.lastRequest = { id = run.requestID, action = "items", done = time(), new = run.done }
    end
    if not run.quiet or run.done > 0 then
        print(("WoWMCP: %d new items read, %d in the export (%d not loaded). Type /reload so the app sees them.")
            :format(run.done, count, #missing))
    end
end

local function itemTick()
    local run = itemRun
    if not run or inCombat() then return end
    local store = itemStore()
    local progressed, requested = false, 0
    for id in pairs(run.todo) do
        if not run.requested[id] then
            if requested < ITEM_BATCH then
                requested = requested + 1
                run.requested[id] = true
                try(C_Item.RequestLoadItemDataByID, id)
            end
        else
            local rec = itemInfoRecord(id)
            if rec then
                store.items[id], run.todo[id] = rec, nil
                run.done = run.done + 1
                progressed = true
                if run.done % 250 == 0 then
                    print(("WoWMCP: %d/%d items..."):format(run.done, run.total))
                end
            end
        end
    end
    run.idle = (progressed or requested > 0) and 0 or run.idle + 1
    if next(run.todo) == nil or run.idle >= ITEM_IDLE_TICKS then finishItemRun() end
end

-- opts.full re-reads items already in the export; opts.items replaces the Auctionator list.
local function startItemRun(opts)
    opts = opts or {}
    if itemRun then
        print(("WoWMCP: item export already running (%d/%d)."):format(itemRun.done, itemRun.total))
        return
    end
    local source, ids = "auctionator", opts.items
    if type(ids) == "table" then
        source = "app"
    else
        ids = auctionatorItems()
        if not ids then
            if not opts.quiet then
                print("WoWMCP: Auctionator price data not found. Install Auctionator and scan the auction house first.")
            end
            return
        end
    end
    local store = itemStore()
    local run = { todo = {}, requested = {}, done = 0, total = 0, idle = 0, source = source,
                  started = time(), requestID = opts.requestID, quiet = opts.quiet }
    local function add(id)
        if type(id) == "number" and not run.todo[id] and (opts.full or not store.items[id]) and isEquippable(id) then
            run.todo[id] = true
            run.total = run.total + 1
        end
    end
    for _, id in ipairs(ids) do add(id) end
    for _, id in ipairs(EXTRA_ITEMS) do add(id) end
    itemRun = run
    if run.total == 0 then
        run.ticker = { Cancel = function() end }
        finishItemRun()
        return
    end
    run.ticker = C_Timer.NewTicker(ITEM_TICK, itemTick)
    print(("WoWMCP: reading %d new items (%s)..."):format(run.total, source))
end

---------------------------------------------------------------------------------------------------
-- App requests: the desktop app writes AppRequest.lua into this folder; the client loads it on the
-- next /reload. A request runs once, its id is remembered in WoWMCP_Account.lastRequest.
---------------------------------------------------------------------------------------------------

local function runAppRequest()
    local req = WoWMCP_AppRequest
    if type(req) ~= "table" or type(req.id) ~= "number" then return end
    local last = WoWMCP_Account.lastRequest
    if type(last) == "table" and last.id == req.id then return end
    if req.action == "items" then
        print(("WoWMCP: app request #%d: item export"):format(req.id))
        startItemRun({ full = req.full == true, items = type(req.items) == "table" and req.items or nil,
                       requestID = req.id })
    elseif req.action == "sync" then
        print(("WoWMCP: app request #%d: snapshot"):format(req.id))
        snapshot()
        WoWMCP_Account.lastRequest = { id = req.id, action = "sync", done = time() }
        print("WoWMCP: snapshot ready. Type /reload so the app sees it.")
    else
        WoWMCP_Account.lastRequest = { id = req.id, action = tostring(req.action), done = time(), error = "unknown action" }
    end
end

---------------------------------------------------------------------------------------------------
-- Events
---------------------------------------------------------------------------------------------------

local frame = CreateFrame("Frame")

-- Registering an event the client lacks throws and would abort this file.
local function register(event)
    if C_EventUtils and C_EventUtils.IsEventValid and not C_EventUtils.IsEventValid(event) then return end
    pcall(frame.RegisterEvent, frame, event)
end

local SNAPSHOT_EVENTS = {
    "PLAYER_ENTERING_WORLD", "BAG_UPDATE_DELAYED", "PLAYER_EQUIPMENT_CHANGED", "QUEST_LOG_UPDATE",
    "UPDATE_FACTION", "SKILL_LINES_CHANGED", "TRAIT_CONFIG_UPDATED", "PLAYER_MONEY", "PLAYER_LEVEL_UP",
    "PLAYER_XP_UPDATE", "ZONE_CHANGED", "ZONE_CHANGED_NEW_AREA", "PLAYER_GUILD_UPDATE",
}
local SNAPSHOT_EVENT = {}
for _, event in ipairs(SNAPSHOT_EVENTS) do
    SNAPSHOT_EVENT[event] = true
    register(event)
end
for _, event in ipairs({ "ADDON_LOADED", "PLAYER_REGEN_ENABLED", "AUCTION_HOUSE_CLOSED",
                         "BANKFRAME_OPENED", "BANKFRAME_CLOSED", "PLAYERBANKSLOTS_CHANGED" }) do
    register(event)
end

local function initSavedVariables()
    WoWMCP_Char = type(WoWMCP_Char) == "table" and WoWMCP_Char or {}
    WoWMCP_Char.errors = type(WoWMCP_Char.errors) == "table" and WoWMCP_Char.errors or {}
    WoWMCP_Account = type(WoWMCP_Account) == "table" and WoWMCP_Account or {}
    if WoWMCP_Account.autoItems == nil then WoWMCP_Account.autoItems = true end
    -- 1.0 kept the item export per character; move it into the shared export once.
    local old = WoWMCP_Char.itemInfo
    if type(old) == "table" and type(old.items) == "table" then
        local store = itemStore()
        for id, rec in pairs(old.items) do
            if store.items[id] == nil then store.items[id] = rec end
        end
        store.updated = store.updated or old.updated
    end
    WoWMCP_Char.itemInfo = nil
end

local enteredWorld = false

frame:SetScript("OnEvent", function(_, event, arg1)
    if event == "ADDON_LOADED" then
        if arg1 == ADDON_NAME then
            initSavedVariables()
            frame:UnregisterEvent("ADDON_LOADED")
        end
    elseif not WoWMCP_Char then
        return
    -- No snapshot on PLAYER_LOGOUT: by then the client has torn down money, XP, stats, inventory
    -- and talents (they read as 0/empty), so it would overwrite the last good snapshot.
    elseif event == "PLAYER_REGEN_ENABLED" then
        if dirty then requestSnapshot() end
    elseif event == "AUCTION_HOUSE_CLOSED" then
        -- Auctionator has just stored the scan; read the items it found that are not in the export yet.
        if WoWMCP_Account.autoItems then
            C_Timer.After(1, function() startItemRun({ quiet = true }) end)
        end
    elseif event == "BANKFRAME_OPENED" then
        bankOpen = true
        pcall(scanBank)
        requestSnapshot() -- rescans once the bank tabs have loaded
    elseif event == "BANKFRAME_CLOSED" then
        bankOpen = false
    elseif event == "PLAYERBANKSLOTS_CHANGED" then
        if bankOpen then requestSnapshot() end
    elseif SNAPSHOT_EVENT[event] then
        if event == "PLAYER_ENTERING_WORLD" and not enteredWorld then
            enteredWorld = true
            C_Timer.After(3, runAppRequest)
        end
        requestSnapshot()
    end
end)

---------------------------------------------------------------------------------------------------
-- Slash command
---------------------------------------------------------------------------------------------------

local function countItems()
    local n = 0
    for _ in pairs(itemStore().items) do n = n + 1 end
    return n
end

SLASH_WOWMCP1 = "/wowmcp"
SlashCmdList.WOWMCP = function(msg)
    local cmd, arg = strtrim(msg or ""):lower():match("^(%S*)%s*(.-)$")
    local db = WoWMCP_Char
    if cmd == "sync" then
        snapshot()
        dirty = false
        if C_UI and C_UI.Reload then C_UI.Reload() else ReloadUI() end
    elseif cmd == "items" then
        startItemRun({ full = arg == "full" })
    elseif cmd == "auto" then
        if arg == "on" or arg == "off" then WoWMCP_Account.autoItems = arg == "on" end
        print("WoWMCP: item export after closing the auction house is "
            .. (WoWMCP_Account.autoItems and "on" or "off") .. " (/wowmcp auto on|off)")
    elseif cmd == "status" then
        print(("WoWMCP %s: last snapshot %s"):format(ADDON_VERSION,
            db.updated and date("%Y-%m-%d %H:%M:%S", db.updated) or "never"))
        print("  bank: " .. (db.bank and date("%Y-%m-%d %H:%M", db.bank.updated) or "never scanned (open your bank once)"))
        local store = itemStore()
        print(("  item export: %d items, %s, auto %s"):format(countItems(),
            store.updated and date("%Y-%m-%d %H:%M", store.updated) or "never run",
            WoWMCP_Account.autoItems and "on" or "off"))
        local last = WoWMCP_Account.lastRequest
        if type(last) == "table" then
            print(("  last app request: #%s (%s) %s"):format(tostring(last.id), tostring(last.action),
                last.done and date("%H:%M", last.done) or "running"))
        end
        for key, err in pairs(db.errors) do
            print(("  %s: %s"):format(key, err))
        end
    else
        print("WoWMCP: /wowmcp sync - snapshot and reload so the app and MCP server see current data")
        print("WoWMCP: /wowmcp items [full] - read stats of new auction house items (full: all again), then /reload")
        print("WoWMCP: /wowmcp auto on|off - run the item export automatically when the auction house closes")
        print("WoWMCP: /wowmcp status - snapshot time, item export, app requests and export errors")
    end
end
