"""German texts of the scripts. The key is the English source text (see i18n.py); placeholders must match.

test_i18n.py checks that every text handed to tr() or mark() has an entry here and that no entry is unused.
"""

TEXT = {
    # ---- app_bridge.py, shared ----
    'No WoWMCP export found for "{char}".': "Kein WoWMCP-Export für „{char}“ gefunden.",
    "The recipe data could not be loaded from wago.tools. Check the network connection and refresh.":
        "Die Rezeptdaten konnten nicht von wago.tools geladen werden. Netzwerkverbindung prüfen und aktualisieren.",

    # ---- writs.py ----
    "estimate from conquestcapped.com (Nov 2026): Journeyman 22–45 s, Expert 50–90 s, Artisan about 75 s":
        "Schätzung laut conquestcapped.com (Nov 2026): Geselle 22–45 s, Experte 50–90 s, Fachmann etwa 75 s",
    "Journeyman (75 reputation)": "Geselle (75 Ruf)",
    "Expert (125 reputation)": "Experte (125 Ruf)",
    "Artisan (200 reputation)": "Fachmann (200 Ruf)",
    "Alchemy": "Alchimie",
    "Blacksmithing": "Schmiedekunst",
    "Engineering": "Ingenieurskunst",
    "Leatherworking": "Lederverarbeitung",
    "Tailoring": "Schneiderei",
    "Apprentice": "Lehrling",
    "Journeyman": "Geselle",
    "Expert": "Experte",
    "Artisan": "Fachmann",
    "Herbs": "Kräuter",
    "Ore": "Erz",
    "Textiles": "Textilien",
    "Curiosities": "Kuriositäten",
    "Fabrics": "Stoffe",
    "Ingots": "Barren",
    "Parts": "Bauteile",
    "Earthly herbs": "Weltliche Kräuter",
    "Flowering herbs": "Blühende Kräuter",
    "No price data found in Auctionator.lua.": "Keine Preisdaten in Auctionator.lua gefunden.",
    "Auctionator.lua not found under {wow}": "Auctionator.lua nicht gefunden unter {wow}",
    "{n} items, {unnamed} without a name, last scan day {date}": "{n} Items, {unnamed} ohne Namen, letzter Scantag {date}",
    "measured: coin pouch opened {n}x, {low} to {high}": "gemessen: {n}x Münzbeutel geöffnet, {low} bis {high}",
    "measured: handed in {n}x": "gemessen: {n}x abgegeben",
    "you own the writ": "Verfügung im Besitz",
    "you own the item ({n}x)": "Item im Besitz ({n}x)",
    "only {n} items in the AH": "nur {n} Items im AH",
    "estimated, extrapolated from Apprentice 5, Journeyman 10, Expert 15":
        "geschätzt, fortgeschrieben aus Lehrling 5, Geselle 10, Experte 15",
    "from urgametips.com and wow.gg (Nov 2026)": "laut urgametips.com und wow.gg (Nov 2026)",
    "you own the crate ({n}x)": "Kiste im Besitz ({n}x)",
    "empty crate not in the AH (drops from enemies)": "leere Kiste nicht im AH (Drop von Gegnern)",
    "no bundle with an AH price": "kein Bündel mit AH-Preis",
    "you own the bundle ({n}x {name})": "Bündel im Besitz ({n}x {name})",
    "only {n}x {name} in the AH": "nur {n}x {name} im AH",
    "AH data: last scan day {date}  |  character: {char}": "AH-Daten: letzter Scan-Tag {date}  |  Charakter: {char}",
    "Reputation {name}: {standing} {value}/{max} ({left} to the next standing)":
        "Ruf {name}: {standing} {value}/{max} (noch {left} bis zur nächsten Stufe)",
    "Note: prices are the lowest buyout at the last scan, not live auctions.":
        "Hinweis: Preise sind der Mindest-Sofortkauf beim letzten Scan, keine Live-Angebote.",
    "writ {writ} + item {item} - pouch {pouch} = net {net}  {per} s/rep  [{listed} writs in the AH]":
        "Verfügung {writ} + Item {item} - Beutel {pouch} = netto {net}  {per} s/Ruf  [{listed} Verfügungen im AH]",
    "Tip: you have {n}x sealed writ of this tier. Open it first, it may be cheaper.":
        "Tipp: Du hast {n}x versiegelte Verfügung dieser Stufe. Erst öffnen, vielleicht ist sie günstiger.",
    "Per day (the best of each tier): {total}, net after coin pouches {net} for {rep} reputation":
        "Pro Tag (jeweils die beste): {total}, netto nach Münzbeuteln {net} für {rep} Ruf",
    "Coin pouches: {sources}": "Münzbeutel: {sources}",
    ", you have {n}": ", du hast {n}",
    "Trade crates (Merchant's Favor{have})": "Handelskisten (Händlergunst{have})",
    "(from level {level})": "(ab Stufe {level})",
    "{tier}: {favor} favor, green {green} ({source})": "{tier}: {favor} Gunst, grün {green} ({source})",
    "{per} s/favor": "{per} s/Gunst",
    "incomplete": "unvollständig",
    "crate {crate} + {bundle} = {total}  {per}": "Kiste {crate} + {bundle} = {total}  {per}",

    # ---- upgrades.py ----
    "Leveling": "Leveln",
    "Dungeon/raid": "Dungeon/Raid",
    "Head": "Kopf",
    "Neck": "Hals",
    "Shoulder": "Schulter",
    "Chest": "Brust",
    "Waist": "Taille",
    "Legs": "Beine",
    "Feet": "Füße",
    "Wrist": "Handgelenk",
    "Hands": "Hände",
    "Finger": "Finger",
    "Trinket": "Schmuck",
    "Back": "Rücken",
    "Main Hand": "Waffenhand",
    "Off Hand": "Schildhand",
    "Wand": "Zauberstab",
    "from the game": "laut Spiel",
    "estimated: spell crit − {base:g}%": "geschätzt: Zauberkrit − {base:g} %",
    "from the spell text": "laut Zaubertext",
    "Life Tap not known": "Aderlass nicht bekannt",
    "Life Tap only from level 6": "Aderlass erst ab Stufe 6",
    "assumed 1:1": "Annahme 1:1",
    ", Improved Life Tap {rank}/2": ", Verbesserter Aderlass {rank}/2",
    "{base:g}% without gear, the cap against +{target} levels is {cap:g}%: already reached":
        "{base:g} % ohne Ausrüstung vorhanden, Cap gegen +{target} Stufen ist {cap:g} %: schon erreicht",
    "cap against +{target} levels {cap:g}%, you have {base:g}%, {need:g}% missing":
        "Cap gegen +{target} Stufen {cap:g} %, {base:g} % vorhanden, es fehlen {need:g} %",
    " (Affliction spells only {need:g}% thanks to Suppression)":
        " (Gebrechen-Zauber durch Unterdrückung nur {need:g} %)",
    ", gear gives {gear:g}%": ", Ausrüstung bringt {gear:g} %",
    "1% damage = {points:.1f} spell power, {share:.0%} of the damage can crit":
        "1 % Schaden = {points:.1f} Zaubermacht, {share:.0%} des Schadens kann kritisch treffen",
    " (assumption: Pandemic lets DoTs crit)": " (Annahme: Pandemie lässt DoTs kritisch treffen)",
    "health, and mana through Life Tap (fixed value)": "Leben, über Aderlass auch Mana (fester Wert)",
    "15 mana and some crit (fixed value)": "15 Mana und etwas Krit (fester Wert)",
    "{mana:g} mana = {manaValue:.2f}, {crit:.4f}% crit ({source}) = {critValue:.2f}. "
    "1 mana = {perMana:.3f}: pool {pool:.0f} (mana + health through Life Tap), "
    "1% more pool counts like {share:g}% damage":
        "{mana:g} Mana = {manaValue:.2f}, {crit:.4f} % Krit ({source}) = {critValue:.2f}. "
        "1 Mana = {perMana:.3f}: Vorrat {pool:.0f} (Mana + Leben über Aderlass), "
        "1 % mehr Vorrat zählt wie {share:g} % Schaden",
    "{health:g} health: {tapMana:.1f} mana through Life Tap ({source}) = {tapValue:.2f}, "
    "survival = {liveValue:.2f} (1% more health counts like {share:g}% damage)":
        "{health:g} Leben: über Aderlass {tapMana:.1f} Mana ({source}) = {tapValue:.2f}, "
        "Überleben = {liveValue:.2f} (1 % mehr Leben zählt wie {share:g} % Schaden)",
    "Shadow / spell power": "Schatten-/Zaubermacht",
    "reference": "Bezugsgröße",
    "Stamina": "Ausdauer",
    "Intellect": "Intelligenz",
    "Spirit": "Willenskraft",
    "Strength": "Stärke",
    "Agility": "Beweglichkeit",
    "regeneration out of combat": "Regeneration außerhalb des Kampfes",
    "Mana per 5 sec.": "Mana pro 5 Sek.",
    "Hit, next 1%": "Treffer, nächstes 1 %",
    "Crit, per 1%": "Krit, je 1 %",
    "Wand DPS": "Zauberstab-DPS",
    "wand slot only": "nur im Zauberstab-Platz",
    "The combat engine does not model this class yet ({cls}).":
        "Die Kampf-Engine kennt diese Klasse noch nicht ({cls}).",
    "This class is rated by the combat engine (simforever), which did not run.":
        "Diese Klasse bewertet die Kampf-Engine (simforever), und die ist nicht gelaufen.",
    "Combat model: {dps:.1f} DPS against +{target} levels, {seconds:g}s fight":
        "Kampfmodell: {dps:.1f} DPS gegen +{target} Stufen, {seconds:g}s Kampf",
    "Combat model: {hps:.1f} HPS over a {seconds:g}s fight":
        "Kampfmodell: {hps:.1f} HPS über einen {seconds:g}s-Kampf",
    "Combat model: the mana lasts {value:.0f}s at full healing":
        "Kampfmodell: das Mana reicht {value:.0f}s bei voller Heilung",
    " · Monte-Carlo over {n} fights: spread ±{sd:.1f}, worst tenth {p10:.1f}":
        " · Monte-Carlo über {n} Kämpfe: Streuung ±{sd:.1f}, schlechtestes Zehntel {p10:.1f}",
    ", mana for {ttoom:.0f}s": ", Mana für {ttoom:.0f}s",
    ", spellbook assumed from level and talents": ", Zauberbuch aus Stufe und Talenten angenommen",
    "{share:.1f}% of the fight time": "{share:.1f} % der Kampfzeit",
    "{n:.1f} casts per minute": "{n:.1f} Mal pro Minute",

    # ---- simengine.py ----
    "The combat engine (simforever) was not found.": "Die Kampf-Engine (simforever) wurde nicht gefunden.",
    "The combat engine failed: {err}": "Die Kampf-Engine ist fehlgeschlagen: {err}",
    "The combat engine reports: {err}": "Die Kampf-Engine meldet: {err}",
    "The snapshot does not name the client build; /wowmcp sync and /reload first.":
        "Der Schnappschuss nennt den Client-Build nicht; erst /wowmcp sync und /reload.",
    "Shadow/spell power 1, Stamina {stamina:.2f}, Int {intellect:.2f}, Spirit {spirit:g}, Mana/5s {mp5:g}, "
    "Hit 1% = {hit:.1f}, Crit 1% = {crit:.1f}, Wand DPS {wand:g}":
        "Schatten/Zaubermacht 1, Ausdauer {stamina:.2f}, Int {intellect:.2f}, Willenskraft {spirit:g}, "
        "Mana/5s {mp5:g}, Treffer 1 % = {hit:.1f}, Krit 1 % = {crit:.1f}, Zauberstab-DPS {wand:g}",
    "SP": "ZM",
    "SD": "ZS",
    "Shadow": "Schatten",
    "Sta": "Ausd",
    "Int": "Int",
    "Spi": "Wil",
    "Hit rating": "Trefferwertung",
    "Crit rating": "Kritwertung",
    "Mana/5s": "Mana/5s",
    "DPS": "DPS",
    "{n:g} Armor": "{n:g} Rüstung",
    "({n:.1f} damage per second)": "({n:.1f} Schaden pro Sekunde)",
    "Durability {now} / {max}": "Haltbarkeit {now} / {max}",
    "Requires Level {n}": "Benötigt Stufe {n}",
    "Equip: Increases damage and healing done by magical spells and effects by up to {n}.":
        "Anlegen: Erhöht durch magische Zauber und Effekte verursachten Schaden und Heilung um bis zu {n}.",
    "Equip: Increases damage done by magical spells and effects by up to {n}.":
        "Anlegen: Erhöht den von magischen Zaubern und Effekten verursachten Schaden um bis zu {n}.",
    "Equip: Increases damage done by Shadow spells and effects by up to {n}.":
        "Anlegen: Erhöht den von Schattenzaubern und -effekten verursachten Schaden um bis zu {n}.",
    "Equip: Increases healing done by spells and effects by up to {n}.":
        "Anlegen: Erhöht durch Zauber und Effekte verursachte Heilung um bis zu {n}.",
    "No WoWMCP.lua found for this character.": "Keine WoWMCP.lua für diesen Charakter gefunden.",
    "No item export found: type /wowmcp items in the game, wait for the message, then /reload.":
        "Kein Item-Export gefunden: im Spiel /wowmcp items, auf die Meldung warten, dann /reload.",
    "new": "neu",
    "[NEW in Forever]": "[NEU in Forever]",
    "[two-handed]": "[Zweihand]",
    "AH scan: {date} | item export: {when} | level {level} | budget {budget} | {count} items, {missing} not loaded":
        "AH-Scan: {date} | Item-Export: {when} | Stufe {level} | Budget {budget} | {count} Items, "
        "{missing} nicht geladen",
    'Scale "{label}" (1 point = 1 shadow spell power), value of your gear now: {value}':
        "Skala „{label}“ (1 Punkt = 1 Schatten-Zaubermacht), Wert der Ausrüstung jetzt: {value}",
    "Prices are the lowest buyout at the scan.": "Preise = Mindest-Sofortkauf beim Scan.",
    "Best package for {budget}: {gain} {unit}{pct} for {cost}":
        "Bestes Paket für {budget}: {gain} {unit}{pct} für {cost}",
    "up to here {cost} for {gain} {unit}": "bis hier {cost} für {gain} {unit}",
    "points": "Punkte",
    ", replaces {items}": ", ersetzt {items}",
    "{slot}: now {name} ({stats})": "{slot}: jetzt {name} ({stats})",
    "With a special effect, please look for yourself": "Mit Spezialeffekt, bitte selbst ansehen",

    # ---- recipes.py ----
    "binds when picked up, cannot be sold": "beim Aufheben gebunden, nicht verkäuflich",
    "price is 1 day old": "Preis 1 Tag alt",
    "price is {days} days old": "Preis {days} Tage alt",
    "profit is falling (on {date} still {gold}g)": "Gewinn fällt (am {date} noch {gold}g)",
    "cheap right now (7-day average {gold}g)": "gerade billig (Ø 7 Tage {gold}g)",
    "no price for {items}": "kein Preis für {items}",
    "Recipe {id}": "Rezept {id}",
    "Recipe: {name}": "Rezept: {name}",
    "Trainer": "Lehrer",
    "{char}  |  AH scan {date}  |  build {build}  |  vendor prices {vendors}":
        "{char}  |  AH-Scan {date}  |  Build {build}  |  Händlerpreise {vendors}",
    "AH": "AH",
    "vendor": "Händler",
    "recipe item": "Rezept-Item",
    "trainer": "Lehrer",
    "yes": "ja",
    "no": "nein",
    "profession window read {when}": "Berufsfenster gelesen {when}",
    "learned recipes unknown (open the profession window)": "gelernte Rezepte unbekannt (Berufsfenster öffnen)",
    "Learned:": "Gelernt:",
    "Not learned yet:": "Noch nicht gelernt:",
    "All recipes:": "Alle Rezepte:",
    "profit {profit}  cost {cost}  AH {listed}x": "Gewinn {profit}  Kosten {cost}  AH {listed}x",
    "profit {profit}  learnable {learnable} from {skill} {source}":
        "Gewinn {profit}  lernbar {learnable} ab {skill} {source}",

    # ---- consumables.py ----
    "Potion": "Trank",
    "Elixir": "Elixier",
    "Flask": "Fläschchen",
    "Scroll": "Rolle",
    "Food": "Essen",
    "Bandage": "Verband",
    "Other": "Sonstiges",
    "Weapon oil": "Waffenöl",
    "Buff food": "Buff-Essen",
    "Healing potions": "Heiltränke",
    "Mana potions": "Manatränke",
    "Drinks": "Getränke",
    "Bandages": "Verbände",
    "Spell power": "Zaubermacht",
    "Shadow spell power": "Schattenzaubermacht",
    "Fire spell power": "Feuerzaubermacht",
    "Frost spell power": "Frostzaubermacht",
    "Arcane spell power": "Arkanzaubermacht",
    "Nature spell power": "Naturzaubermacht",
    "Holy spell power": "Heiligzaubermacht",
    "Healing": "Heilung",
    "Spell crit": "Zauberkrit",
    "Maximum health": "Maximale Gesundheit",
    "Health per 5 sec.": "Gesundheit alle 5 Sek.",
    "Armor": "Rüstung",
    "Attack power": "Angriffskraft",
    "Ranged attack power": "Distanzangriffskraft",
    "Crit": "Krit",
    "All attributes": "Alle Attribute",
    "Cooking": "Kochkunst",
    "First Aid": "Erste Hilfe",
    "Enchanting": "Verzauberkunst",
    "{n} h": "{n} Std.",
    "{n} min": "{n} Min.",
    "{n} sec.": "{n} Sek.",
    "restores {amount} health": "stellt {amount} Gesundheit wieder her",
    "restores {amount} mana": "stellt {amount} Mana wieder her",
    "{amount} health over {seconds} sec.": "{amount} Gesundheit in {seconds} Sek.",
    "{amount} mana over {seconds} sec.": "{amount} Mana in {seconds} Sek.",
    "The item data could not be loaded from wago.tools. Check the network connection and refresh.":
        "Die Itemdaten konnten nicht von wago.tools geladen werden. Netzwerkverbindung prüfen und aktualisieren.",
    "{char}  |  level {level}  |  AH scan {date}": "{char}  |  Stufe {level}  |  AH-Scan {date}",
    "{n} in bags": "{n} in den Taschen",
    "next: {name} from level {level}": "nächstes: {name} ab Stufe {level}",
    "next: {name} with {skill} {rank}": "nächstes: {name} mit {skill} {rank}",

    # ---- ingame.py ----
    "AH upgrades": "AH-Upgrades",
    "Writs": "Verfügungen",
    "Trade crates": "Handelskisten",
    "Professions": "Berufe",
    "Consumables": "Verbrauchsgüter",
    "Level {n}": "Stufe {n}",
    "Best package for the budget": "Bestes Paket fürs Budget",
    "{gain} points for {m}": "{gain} Punkte für {m}",
    "{gain} DPS for {m}": "{gain} DPS für {m}",
    "{gain} HPS for {m}": "{gain} HPS für {m}",
    "Upgrade for {slot}: {gain} DPS, no. {rank} in the AH scan ({m})":
        "Upgrade für {slot}: {gain} DPS, Nr. {rank} im AH-Scan ({m})",
    "Upgrade for {slot}: {gain} HPS, no. {rank} in the AH scan ({m})":
        "Upgrade für {slot}: {gain} HPS, Nr. {rank} im AH-Scan ({m})",
    "Budget {m} · combat model: {dps} DPS now": "Budget {m} · Kampfmodell: {dps} DPS jetzt",
    "Budget {m} · combat model: {hps} HPS now": "Budget {m} · Kampfmodell: {hps} HPS jetzt",
    "Upgrade for {slot}: {gain} points, no. {rank} in the AH scan ({m})":
        "Upgrade für {slot}: {gain} Punkte, Nr. {rank} im AH-Scan ({m})",
    "Part of the best package for your budget": "Teil des besten Pakets für dein Budget",
    "from level {level}": "ab Stufe {level}",
    "now: {name}": "jetzt: {name}",
    "empty": "leer",
    "With a special effect": "Mit Spezialeffekt",
    "the scale cannot rate these": "die Skala kann sie nicht bewerten",
    "Budget {m} · scale: {profile}": "Budget {m} · Skala: {profile}",
    "you have it": "hast du",
    "{m} per reputation": "{m} pro Ruf",
    "profit {m}": "Gewinn {m}",
    "net {m}": "netto {m}",
    "Writ ({tier}): needs {n}x {item}, {net}, {per}": "Verfügung ({tier}): braucht {n}x {item}, {net}, {per}",
    "For a writ ({tier}): hand in {n}x, {net}": "Für eine Verfügung ({tier}): {n}x abgeben, {net}",
    "{m} per favor": "{m} pro Gunst",
    "Cheapest Merchant's Favor": "Günstigste Händlergunst",
    "Fill with {n}x {item} ({m}): {per} per Merchant's Favor":
        "Mit {n}x {item} füllen ({m}): {per} pro Händlergunst",
    "Fills {crate}: {n}x": "Füllt {crate}: {n}x",
    "{n} favor each": "je {n} Gunst",
    "Merchant's Favor: {n}": "Händlergunst: {n}",
    "falling": "fallend",
    "rising": "steigend",
    "{profession}: {profit} per craft": "{profession}: {profit} pro Herstellung",
    "Reagent for {item}: {profit} per craft": "Reagenz für {item}: {profit} pro Herstellung",
    "{profession}: best crafts": "{profession}: beste Herstellungen",
    "{profession}: worth getting": "{profession}: lohnt sich zu holen",
    "profit per craft": "Gewinn pro Herstellung",
    "Teaches {recipe}: {profit} per craft": "Lehrt {recipe}: {profit} pro Herstellung",
    "paid off after {n} crafts": "bezahlt nach {n}x herstellen",
    "Pays more than your best recipe": "Lohnt mehr als dein bestes Rezept",
    "Needs {profession} {n}": "Benötigt {profession} {n}",
    "Best now ({category}): {effect}": "Jetzt am besten ({category}): {effect}",
    "The WoWMCP addon is not installed.": "Das Addon WoWMCP ist nicht installiert.",
    "WoWMCP {version} cannot show lists. Update the addon to 1.7 or newer.":
        "WoWMCP {version} kann keine Listen anzeigen. Aktualisiere das Addon auf 1.7 oder neuer.",
    "{n} items with a tooltip line, {v} vendor hints": "{n} Items mit Tooltip-Zeile, {v} Händlerhinweise",
    "AH scan {date}, computed {time}. * = goes into the Auctionator shopping list.":
        "AH-Scan {date}, berechnet {time}. * = kommt auf die Auctionator-Einkaufsliste.",
    "Written to {file}. Type /reload in the game.": "Nach {file} geschrieben. Tippe im Spiel /reload.",
    "Nothing changed, the file in the addon folder is current.":
        "Nichts geändert, die Datei im Addon-Ordner ist aktuell.",
}
