"""Beste Handwerksverfügung pro Stufe (Geselle / Experte / Fachmann) nach Silber pro Ruf.

Liest den Auctionator-Scan (Auctionator.lua) und das Inventar des Charakters (WoWMCP.lua),
rechnet für jede Verfügung: Preis der Verfügung + Preis des verlangten Items
(0, wenn du das Item schon in Taschen/Bank hast) und zeigt pro Stufe die besten.

Aufruf:  python verfuegungen.py [--char Malgruk] [--top 3] [--wow "C:\\...\\_forever_"]
Nach einem vollen AH-Scan einmal /reload oder ausloggen, damit Auctionator speichert.
"""
import argparse, datetime, glob, os, re, struct, sys

DEFAULT_WOW = r"C:\Program Files (x86)\World of Warcraft\_forever_"
REP = {"J": 75, "E": 125, "A": 200}
TIER_NAME = {"J": "Geselle (75 Ruf)", "E": "Experte (125 Ruf)", "A": "Fachmann (200 Ruf)"}
SEALED = {"J": 262888, "E": 262889, "A": 262890}

# Verfügungs-Item-ID | Name | Stufe | verlangtes Item | Menge | Beruf   (Quelle: wow.gg, Nov 2026)
WRITS = """264012|Elixier der Weisheit|J|3383|1|Alch
264019|Draught of Water Breathing|J|5996|1|Alch
264013|Geringer Manatrank|J|3385|1|Alch
264017|Lesser Troll's Blood Elixir|J|3388|1|Alch
264014|Schattenschutztrank|J|6048|1|Alch
264016|Elixier der geringen Beweglichkeit|J|3390|1|Alch
264011|Elixier der Ogerstärke|J|3391|1|Alch
264015|Trank der freien Aktion|J|5634|1|Alch
264018|Elixier der Verteidigung|J|8951|1|Alch
264062|Manatrank|E|3827|1|Alch
264057|Geringer Unsichtbarkeitstrank|E|3823|1|Alch
264064|Schattenöl|E|3824|1|Alch
264061|Great Rage Potion|E|5633|1|Alch
264056|Frostschutztrank|E|6050|1|Alch
264066|Elixier der Beweglichkeit|E|8949|1|Alch
264059|Naturschutztrank|E|6052|1|Alch
264063|Feuerschutztrank|E|6049|1|Alch
264060|Katzenaugentrunk|E|10592|1|Alch
264065|Goblinraketentreibstoff|E|9061|1|Alch
264067|Wildvine Potion|E|9144|1|Alch
264058|Geringer Steinschildtrank|E|4623|1|Alch
264117|Dreamless Sleep Potion|A|12190|1|Alch
264120|Gift of Arthas|A|9088|1|Alch
264124|Distilled Firewater|A|246948|1|Alch
264116|Potion of Demonslaying|A|9224|1|Alch
264119|Draught of Detect Demon|A|9233|1|Alch
264122|Limited Invulnerability Potion|A|3387|1|Alch
264118|Greater Fire Protection Potion|A|13457|1|Alch
264123|Major Rejuvenation Potion|A|18253|1|Alch
264121|Flask of Chromatic Resistance|A|13513|1|Alch
264023|Silver Skeleton Key|J|15869|1|Schmied
264027|Big Bronze Knife|J|3848|1|Schmied
264020|Pearl-Handled Dagger|J|5540|1|Schmied
264025|Rough Bronze Leggings|J|2865|1|Schmied
264021|Golden Rod|J|11128|1|Schmied
264024|Heavy Bronze Mace|J|3491|1|Schmied
264022|Iridescent Hammer|J|5541|1|Schmied
264028|Green Iron Boots|J|3484|1|Schmied
264026|Green Iron Gauntlets|J|3485|1|Schmied
264075|Solid Sharpening Stone|E|7964|1|Schmied
264073|Moonsteel Broadsword|E|3853|1|Schmied
264077|Truesilver Rod|E|11144|1|Schmied
264070|Steel Weapon Chain|E|6041|1|Schmied
264074|Golden Scale Coif|E|3837|1|Schmied
264069|Inlaid Mithril Cylinder|E|9060|1|Schmied
264078|Golden Scale Gauntlets|E|9366|1|Schmied
264068|Steel Plate Helm|E|7922|1|Schmied
264079|Heavy Mithril Axe|E|7941|1|Schmied
264071|Ornate Mithril Shoulders|E|7928|1|Schmied
264072|Blue Glittering Axe|E|7942|1|Schmied
264076|Wicked Mithril Blade|E|7943|1|Schmied
264127|Orcish War Leggings|A|7929|1|Schmied
264128|Mithril Spurs|A|7969|1|Schmied
264132|Arcanite Skeleton Key|A|15872|1|Schmied
264126|Radiant Belt|A|12416|1|Schmied
264133|Ebon Shiv|A|7947|1|Schmied
264131|Huge Thorium Battleaxe|A|12775|1|Schmied
264125|Radiant Circlet|A|12417|1|Schmied
264129|Imperial Plate Chest|A|12422|1|Schmied
264130|Darkrune Gauntlets|A|20549|1|Schmied
264033|Practice Lock|J|6712|1|Ingi
264037|Crafted Solid Shot|J|8069|200|Ingi
264036|Big Bronze Bomb|J|4380|1|Ingi
264034|Shadow Goggles|J|4373|1|Ingi
264035|Gnomish Universal Remote|J|7506|1|Ingi
264029|Ornate Spyglass|J|5507|1|Ingi
264032|Minor Recombobulator|J|4381|1|Ingi
264031|Moonsight Rifle|J|4383|1|Ingi
264030|Explosive Sheep|J|4384|1|Ingi
264089|Gyromatic Micro-Adjustor|E|10498|1|Ingi
264081|Advanced Target Dummy|E|4392|1|Ingi
264086|Blitzstrahlbombe|E|4852|1|Ingi
264085|Unstable Trigger|E|10560|1|Ingi
264090|Mechanical Repair Kit|E|11590|1|Ingi
264082|Craftsman's Monocle|E|4393|1|Ingi
264083|Goblin-Landmine|E|4395|1|Ingi
264088|Mithril Frag Bomb|E|10514|1|Ingi
264080|Mechanical Dragonling|E|4396|1|Ingi
264087|Fire Goggles|E|10500|1|Ingi
264084|Catseye Ultra Goggles|E|10501|1|Ingi
264091|Dark Iron Bomb|E|16005|1|Ingi
264141|Sniper Scope|A|10548|1|Ingi
264135|Green Lens|A|10504|1|Ingi
264140|Truesilver Transformer|A|18631|1|Ingi
264137|Gnomish Alarm-O-Bot|A|18645|1|Ingi
264136|Lifelike Mechanical Toad|A|15996|1|Ingi
264138|Goblin Jumper Cables XL|A|18587|1|Ingi
264134|Masterwork Target Dummy|A|16023|1|Ingi
264139|Field Repair Bot 74A|A|18232|1|Ingi
264142|Tranquil Mechanical Yeti|A|21277|1|Ingi
264041|Dark Leather Tunic|J|2317|1|Leder
264039|Fine Leather Pants|J|5958|1|Leder
264046|Red Whelp Gloves|J|7284|1|Leder
264038|Heavy Leather Ball|J|18662|1|Leder
264040|Herbalist's Gloves|J|7349|1|Leder
264042|Pilferer's Gloves|J|7358|1|Leder
264044|Dark Leather Shoulders|J|4252|1|Leder
264043|Heavy Quiver|J|7371|1|Leder
264045|Barbaric Gloves|J|4254|1|Leder
264093|Guardian Cloak|E|5965|1|Leder
264094|Dusky Bracers|E|7378|1|Leder
264092|Comfortable Leather Hat|E|8174|1|Leder
264098|Swift Boots|E|7391|1|Leder
264099|Nightscape Shoulders|E|8192|1|Leder
264102|Turtle Scale Breastplate|E|8189|1|Leder
264101|Big Voodoo Robe|E|8200|1|Leder
264096|Wild Leather Shoulders|E|8210|1|Leder
264100|Big Voodoo Mask|E|8201|1|Leder
264095|Wild Leather Helmet|E|8214|1|Leder
264097|Quickdraw Quiver|E|8217|1|Leder
264103|Thick Leather Ammo Pouch|E|8218|1|Leder
264147|Wicked Leather Bracers|A|15084|1|Leder
264150|Runic Leather Bracers|A|15092|1|Leder
264151|Frostsaber Boots|A|15071|1|Leder
264145|Volcanic Breastplate|A|15053|1|Leder
264143|Wicked Leather Pants|A|15087|1|Leder
264144|Gordok Ogre Suit|A|18258|1|Leder
264146|Runic Leather Headband|A|15094|1|Leder
264148|Chimeric Vest|A|15075|1|Leder
264149|Heavy Scorpid Helm|A|15080|1|Leder
264052|Red Woolen Boots|J|4313|1|Schneider
264053|Colorful Kilt|J|10048|1|Schneider
264048|Blue Overalls|J|6263|1|Schneider
264054|Greater Adept's Robe|J|6264|1|Schneider
264051|Phoenix Pants|J|4317|1|Schneider
264047|Lesser Wizard's Robe|J|5766|1|Schneider
264049|Azure Silk Hood|J|7048|1|Schneider
264055|Spider Silk Slippers|J|4321|1|Schneider
264050|Small Silk Pack|J|4245|1|Schneider
264115|Earthen Silk Belt|E|7061|1|Schneider
264106|Crimson Silk Robe|E|7063|1|Schneider
264109|Star Belt|E|4329|1|Schneider
264110|Black Mageweave Vest|E|9998|1|Schneider
264113|White Bandit Mask|E|10008|1|Schneider
264112|Orange Martial Shirt|E|10052|1|Schneider
264107|Shadoweave Robe|E|10004|1|Schneider
264111|Red Mageweave Pants|E|10009|1|Schneider
264104|Mageweave Bag|E|10050|1|Schneider
264105|Shadoweave Gloves|E|10023|1|Schneider
264108|Enchanted Mageweave Pouch|E|22246|1|Schneider
264114|Cindercloth Robe|E|10042|1|Schneider
264155|Tuxedo Jacket|A|10036|1|Schneider
264158|Shadoweave Mask|A|10025|1|Schneider
264153|Runecloth Bag|A|14046|1|Schneider
264160|Soul Pouch|A|21340|1|Schneider
264159|Ghostweave Vest|A|14141|1|Schneider
264157|Cenarion Herb Bag|A|22251|1|Schneider
264154|Runecloth Boots|A|13864|1|Schneider
264156|Felcloth Boots|A|14108|1|Schneider
264152|Wizardweave Turban|A|14130|1|Schneider"""


# Gold beim Abschluss: Die Quest selbst zahlt kein Gold (RewardMoney 0 im Quest-Cache des Spiels), sie gibt
# einen Münzbeutel (Items 284583–284586), dessen Münzen der Server beim Öffnen auswürfelt. Das Addon (ab 1.5)
# misst den Inhalt; bis dahin gelten diese Schätzungen pro Stufe in Kupfer.
POUCH_ESTIMATE = {"J": 3300, "E": 7000, "A": 7500}
POUCH_ESTIMATE_SOURCE = "Schätzung laut conquestcapped.com (Nov 2026): Geselle 22–45 s, Experte 50–90 s, Fachmann ~75 s"

# Handelskisten: eine abgefangene (leere) Kiste wird mit genau EINEM der Bündel befüllt und als versiegelte
# Kiste beim Handelsvertreter gegen Händlergunst (Währung 3402) abgegeben. Kein Gold, kein Ruf.
# Mengen aus dem Spiel geprüft (Zauber „Kiste befüllen“, Dummy-Effekte in SpellEffect), Items laut wow.gg und
# urgametips.com (Nov 2026). Stufe L = Lehrling.
FAVOR_CURRENCY = 3402
CRATE_TIERS = "LJEA"
CRATE_TIER_NAME = {"L": "Lehrling", "J": "Geselle", "E": "Experte", "A": "Fachmann"}
CRATE_LEVEL = {"L": 10, "J": 10, "E": 20, "A": 35}
# Gunst pro versiegelter Kiste (gewöhnlich, ungewöhnlich) laut urgametips.com/wow.gg; Fachmann ist fortgeschrieben.
CRATE_FAVOR = {"L": (5, 10), "J": (10, 20), "E": (15, 30), "A": (20, 40)}
CRATE_FAVOR_GUESS = {"A"}
SEALED_CRATE = {t: (248765 + i, 248770 + i) for i, t in enumerate(CRATE_TIERS)}  # gewöhnlich, ungewöhnlich

# Kisten-Item | Stufe | Art | Bündel als Item-ID:Menge:Name, getrennt durch ;
CRATES = """248682|L|Kräuter|2447:20:Friedensblume;765:20:Silberblatt;2449:10:Erdwurzel;785:10:Maguskönigskraut;2450:10:Wilddornrose
248683|L|Erz|2770:20:Kupfererz;2771:20:Zinnerz
248684|L|Textilien|2589:40:Leinenstoff;2318:20:Leichtes Leder
248685|L|Kuriositäten|10940:20:Seltsamer Staub;10939:4:Große Magieessenz;10978:6:Kleiner gleißender Splitter;11084:2:Großer gleißender Splitter
248702|L|Stoffe|4231:2:Geschmeidiger leichter Balg;2996:20:Leinenstoffballen
248703|L|Barren|2840:20:Kupferbarren;3576:20:Zinnbarren;2841:10:Bronzebarren
248704|L|Bauteile|4359:16:Eine Hand voll Kupferbolzen;4361:16:Kupferrohr;4363:4:Kupfermodulator
248686|J|Kräuter|3820:10:Würgetang;2453:10:Beulengras;3355:10:Wildstahlblume;3369:10:Grabmoos;3356:10:Königsblut
248687|J|Erz|2775:10:Silbererz;2772:20:Eisenerz
248688|J|Textilien|2592:20:Wollstoff;4306:40:Seidenstoff;2319:20:Mittleres Leder
248689|J|Kuriositäten|11083:20:Seelenstaub;11082:4:Große Astralessenz;11138:6:Kleiner leuchtender Splitter;11139:2:Großer leuchtender Splitter
248705|J|Stoffe|2997:6:Wollstoffballen;4305:10:Seidenstoffballen;4233:2:Geschmeidiger mittlerer Balg
248706|J|Barren|2842:10:Silberbarren;3575:20:Eisenbarren
248707|J|Bauteile|4371:8:Bronzeröhre;4375:7:Surrendes bronzenes Dingsda;4382:4:Bronzegerüst;10558:45:Goldkraftkern
248690|E|Weltliche Kräuter|3357:20:Lebenswurz;3818:10:Blassblatt;3821:10:Golddorn;3358:10:Khadgars Schnurrbart
248691|E|Blühende Kräuter|3819:10:Winterbiss;4625:10:Feuerblüte;8831:20:Lila Lotus;8836:10:Arthas' Tränen
248692|E|Erz|2776:10:Golderz;3858:20:Mithrilerz
248693|E|Textilien|4338:40:Magiestoff;4234:20:Schweres Leder
248694|E|Kuriositäten|11137:20:Visionenstaub;11135:4:Große Mystikeressenz;11177:6:Kleiner strahlender Splitter;11178:2:Großer strahlender Splitter
248708|E|Stoffe|4339:8:Magiestoffballen;4236:2:Geschmeidiger schwerer Balg
248709|E|Barren|3860:20:Mithrilbarren;3859:15:Stahlbarren;3577:10:Goldbarren
248710|E|Bauteile|4387:8:Eisenstrebe;10559:5:Mithrilrohr;10560:12:Instabiler Auslöser;10561:5:Mithrilgehäuse
248695|A|Weltliche Kräuter|13464:10:Goldener Sansam;8838:20:Sonnengras;8839:10:Blindkraut;8845:10:Geisterpilz;8846:10:Gromsblut
248696|A|Blühende Kräuter|13463:20:Traumblatt;13465:20:Bergsilberweisling;13466:20:Pestblüte;13467:20:Eiskappe
248697|A|Erz|7911:10:Echtsilbererz;10620:20:Thoriumerz;11370:20:Dunkeleisenerz
248698|A|Textilien|14047:40:Runenstoff;4304:20:Dickes Leder;8170:20:Unverwüstliches Leder
248699|A|Kuriositäten|11176:20:Traumstaub;11175:4:Große Netheressenz;14343:6:Kleiner glänzender Splitter;14344:2:Großer glänzender Splitter
248711|A|Stoffe|14048:8:Runenstoffballen;8172:2:Geschmeidiger dicker Balg
248712|A|Barren|11371:2:Dunkeleisenbarren;6037:10:Echtsilberbarren;12359:20:Thoriumbarren
248713|A|Bauteile|15994:5:Thoriumapparat;18631:5:Echtsilberumwandler;16000:2:Thoriumröhre"""


def crate_list():
    out = []
    for line in CRATES.splitlines():
        cid, tier, kind, opts = line.split('|')
        options = []
        for o in opts.split(';'):
            iid, qty, name = o.split(':', 2)
            options.append((int(iid), int(qty), name))
        out.append((int(cid), tier, kind, options))
    return out

# ---------- Lua / CBOR decoding ----------
def lua_string(b, i):
    """b[i] is the opening quote of a Lua string literal; return (bytes, end index)."""
    esc = {ord('n'): 10, ord('r'): 13, ord('t'): 9, ord('"'): 34, ord('\\'): 92, ord("'"): 39,
           ord('a'): 7, ord('b'): 8, ord('f'): 12, ord('v'): 11, ord('\n'): 10}
    out = bytearray(); i += 1
    while True:
        c = b[i]
        if c == 34:
            return bytes(out), i + 1
        if c == 92:
            n = b[i + 1]
            if 48 <= n <= 57:
                j = i + 1
                while j < len(b) and j - i <= 3 and 48 <= b[j] <= 57:
                    j += 1
                out.append(int(b[i + 1:j])); i = j; continue
            out.append(esc[n]); i += 2; continue
        out.append(c); i += 1


def cbor(b, i=0):
    ib = b[i]; mt, ai = ib >> 5, ib & 31; i += 1
    if mt == 7:
        simple = {20: False, 21: True, 22: None}
        if ai in simple: return simple[ai], i
        fmt, n = {25: ('>e', 2), 26: ('>f', 4), 27: ('>d', 8)}[ai]
        return struct.unpack(fmt, b[i:i + n])[0], i + n
    if ai >= 24:
        n = {24: 1, 25: 2, 26: 4, 27: 8}[ai]
        v = int.from_bytes(b[i:i + n], 'big'); i += n
    else:
        v = ai
    if mt == 0: return v, i
    if mt == 1: return -1 - v, i
    if mt in (2, 3): return b[i:i + v].decode('utf-8', 'replace'), i + v
    if mt == 4:
        arr = []
        for _ in range(v):
            x, i = cbor(b, i); arr.append(x)
        return arr, i
    if mt == 5:
        d = {}
        for _ in range(v):
            k, i = cbor(b, i); x, i = cbor(b, i); d[str(k)] = x
        return d, i
    if mt == 6: return cbor(b, i)
    raise ValueError(f"CBOR major type {mt}")


def load_prices(path):
    data = open(path, 'rb').read()
    start = data.index(b'AUCTIONATOR_PRICE_DATABASE = {')
    end = data.index(b'\n}', start)
    realms = {}
    for m in re.finditer(rb'\["([^"]+)"\] = "', data[start:end]):
        raw, _ = lua_string(data, start + m.end() - 1)
        try:
            obj, _ = cbor(raw)
        except Exception:
            continue
        if isinstance(obj, dict):
            realms[m.group(1).decode('utf-8', 'replace')] = obj
    if not realms:
        sys.exit("Keine Preisdaten in Auctionator.lua gefunden.")
    return max(realms.values(), key=len)  # Realm mit den meisten Einträgen


def load_char(path):
    """Inventory counts (bags + bank) and the Durotar/ACA standing from WoWMCP.lua."""
    text = open(path, encoding='utf-8', errors='replace').read()
    owned, rep = {}, None
    for block in re.findall(r'\{([^{}]*)\}', text):
        idm = re.search(r'\["id"\] = (\d+)', block)
        if not idm:
            continue
        cnt = re.search(r'\["count"\] = (\d+)', block)
        if cnt and '["link"]' in block:
            owned[int(idm.group(1))] = owned.get(int(idm.group(1)), 0) + int(cnt.group(1))
        name = re.search(r'\["name"\] = "([^"]*)"', block)
        if name and ('Durotar' in name.group(1) or 'Commerce' in name.group(1) or 'Handels' in name.group(1)) \
                and '["standing"]' in block:
            g = lambda k: re.search(rf'\["{k}"\] = "?([^",]*)"?,', block).group(1)
            rep = (name.group(1), g('standing'), int(g('value')), int(g('max')))
    return owned, rep


# ---------- Berechnung ----------
def find_auctionator(wow):
    acct = glob.glob(os.path.join(wow, 'WTF', 'Account', '*', 'SavedVariables', 'Auctionator.lua'))
    if not acct:
        raise FileNotFoundError(f"Auctionator.lua nicht gefunden unter {wow}")
    return acct[0]


def find_chars(wow, char=''):
    """WoWMCP.lua per character, most recently written first, filtered by part of the folder name."""
    chars = sorted(glob.glob(os.path.join(wow, 'WTF', 'Account', '*', '*', '*', 'SavedVariables', 'WoWMCP.lua')),
                   key=os.path.getmtime, reverse=True)
    return [c for c in chars if char.lower() in c.lower()]


def scan_date(prices):
    days = [int(k) for v in prices.values() if isinstance(v, dict) for k in (v.get('a') or {})]
    return datetime.date(2020, 1, 1) + datetime.timedelta(max(days)) if days else None


def writ_quests(cache=None):
    """Writ item -> quest ID (ItemSparse.StartQuestID) from the newest cached DB2 copy; {} without one."""
    cache = cache or os.path.join(os.path.dirname(os.path.abspath(__file__)), 'cache')
    files = sorted(glob.glob(os.path.join(cache, 'ItemSparse-*.csv')), key=os.path.getmtime)
    if not files:
        return {}
    import csv
    wanted = {int(line.split('|')[0]) for line in WRITS.splitlines()}
    out = {}
    with open(files[-1], encoding='utf-8', newline='') as f:
        for r in csv.DictReader(f):
            iid = int(r['ID'])
            if iid in wanted and (r.get('StartQuestID') or '0').isdigit() and int(r['StartQuestID']):
                out[iid] = int(r['StartQuestID'])
    return out


def load_extras(wow, char_path):
    """Payouts the addon measured (account-wide), plus level and Merchant's Favor of the character.
    Needs lupa (upgrades.load_sv); without it the estimates are used."""
    payouts, level, favor = {}, None, None
    try:
        import upgrades  # lazy: upgrades imports this module
        for path in glob.glob(os.path.join(wow, 'WTF', 'Account', '*', 'SavedVariables', 'WoWMCP.lua')):
            p = (upgrades.load_sv(path, 'WoWMCP_Account') or {}).get('payouts') or {}
            for kind in ('pouches', 'crates', 'writQuests'):
                payouts.setdefault(kind, {}).update(p.get(kind) or {})
        if char_path:
            c = upgrades.load_char(char_path)
            level = (c.get('character') or {}).get('level')
            cur = (c.get('currencies') or {}).get(FAVOR_CURRENCY)
            favor = cur.get('quantity') if isinstance(cur, dict) else None
    except ImportError:  # lupa missing (plain python)
        pass
    return payouts, level, favor


def sample_avg(s):
    return s['sum'] / s['n'] if isinstance(s, dict) and s.get('n') else None


def pouch_values(payouts, quest_of):
    """Gold per writ from the measured pouches: quest -> pouch item (seen at turn-in), pouch -> average
    coins. A writ without its own turn-in uses the pouch the other writs of its tier gave."""
    pouches = payouts.get('pouches') or {}
    wq = payouts.get('writQuests') or {}
    tier_of = {int(line.split('|')[0]): line.split('|')[2] for line in WRITS.splitlines()}
    per_writ, tier_pouch = {}, {}
    for wid, q in quest_of.items():
        rec = wq.get(q)
        if isinstance(rec, dict) and rec.get('pouch'):
            per_writ[wid] = (rec['pouch'], rec.get('money') or 0)
            votes = tier_pouch.setdefault(tier_of[wid], {})
            votes[rec['pouch']] = votes.get(rec['pouch'], 0) + 1

    def value(wid, tier):
        pouch, money = per_writ.get(wid, (None, 0))
        if pouch is None and tier in tier_pouch:
            pouch = max(tier_pouch[tier], key=tier_pouch[tier].get)
        s = pouches.get(pouch) if pouch else None
        avg = sample_avg(s)
        if avg is not None:
            return (round(avg) + money,
                    f"gemessen: Münzbeutel {s['n']}x geöffnet, {fmt(s['min'])} bis {fmt(s['max'])}", True)
        return POUCH_ESTIMATE[tier] + money, POUCH_ESTIMATE_SOURCE, False
    return value


def compute(wow=DEFAULT_WOW, char='', top=3, besitz_zum_ah_preis=False, prices=None, extras=None, quest_of=None):
    """Best writs per tier by net cost per reputation, and the trade crates by cost per Merchant's Favor.
    Returns plain data (copper amounts) for the CLI and the app."""
    prices = prices if prices is not None else load_prices(find_auctionator(wow))
    chars = find_chars(wow, char)
    owned, rep, who = {}, None, None
    if chars:
        owned, rep = load_char(chars[0])
        who = os.path.basename(os.path.dirname(os.path.dirname(chars[0])))
    payouts, level, favor = extras if extras is not None else load_extras(wow, chars[0] if chars else None)
    quest_of = quest_of if quest_of is not None else writ_quests()
    payout_of = pouch_values(payouts, quest_of)

    def price(item_id):
        e = prices.get(str(item_id))
        if not isinstance(e, dict) or not e.get('m'):
            return None, 0, None
        a_ = e.get('a') or {}
        day = max(a_, key=int) if a_ else None
        return e['m'], (a_.get(day) or 0), day

    rows = []
    for line in WRITS.splitlines():
        wid, name, tier, item, qty, prof = line.split('|')
        wid, item, qty = int(wid), int(item), int(qty)
        wp, wn, _ = price(wid)
        ip, inn, _ = price(item)
        have_writ = owned.get(wid, 0) > 0
        have_item = owned.get(item, 0) >= qty
        if (wp is None and not have_writ) or (ip is None and not have_item):
            continue
        wcost = 0 if have_writ else wp
        icost = 0 if (have_item and not besitz_zum_ah_preis) else (ip or 0) * qty
        cost = wcost + icost
        payout, payout_source, measured = payout_of(wid, tier)
        net = cost - payout
        note = []
        if have_writ: note.append('Verfügung im Besitz')
        if have_item: note.append(f'Item im Besitz ({owned[item]}x)')
        if not have_item and inn < qty * 10: note.append(f'nur {inn} Items im AH')
        rows.append(dict(tier=tier, name=name, prof=prof, cost=cost, payout=payout, net=net, per=net / REP[tier],
                         payoutSource=payout_source, payoutMeasured=measured, wcost=wcost, icost=icost,
                         writs=wn, note=', '.join(note), qty=qty, writId=wid, itemId=item,
                         questId=quest_of.get(wid)))

    tiers = []
    for t in 'JEA':
        best = sorted((r for r in rows if r['tier'] == t), key=lambda r: r['per'])[:top]
        tiers.append(dict(tier=t, name=TIER_NAME[t], rep=REP[t], best=best, sealed=owned.get(SEALED[t], 0)))
    total = sum(t['best'][0]['cost'] for t in tiers if t['best'])
    total_net = sum(t['best'][0]['net'] for t in tiers if t['best'])
    reputation = dict(name=rep[0], standing=rep[1], value=rep[2], max=rep[3]) if rep else None
    sd = scan_date(prices)
    return dict(scanDate=sd.isoformat() if sd else None, character=who, reputation=reputation,
                tiers=tiers, dailyTotal=total, dailyNet=total_net, dailyRep=sum(REP.values()),
                crates=crates(price, owned, besitz_zum_ah_preis, payouts, level, top), favor=favor, level=level)


def crates(price, owned, besitz_zum_ah_preis, payouts, level, top):
    """Per crate tier: every crate type with its cheapest bundle and the cost per point of Merchant's Favor."""
    measured = payouts.get('crates') or {}
    tiers = []
    for t in CRATE_TIERS:
        common, uncommon = CRATE_FAVOR[t]
        sc, su = SEALED_CRATE[t]
        mc, mu = sample_avg(measured.get(sc)), sample_avg(measured.get(su))
        favor = mc if mc is not None else common
        favor_green = mu if mu is not None else uncommon
        if mc is not None:
            favor_source = f"gemessen: {measured[sc]['n']}x abgegeben"
        elif t in CRATE_FAVOR_GUESS:
            favor_source = 'geschätzt, fortgeschrieben aus Lehrling 5, Geselle 10, Experte 15'
        else:
            favor_source = 'laut urgametips.com und wow.gg (Nov 2026)'
        rows = []
        for cid, tier, kind, options in crate_list():
            if tier != t:
                continue
            cp, cn, _ = price(cid)
            have_crate = owned.get(cid, 0) > 0
            opts = []
            for iid, qty, name in options:
                ip, inn, _ = price(iid)
                have = owned.get(iid, 0) >= qty
                if have and not besitz_zum_ah_preis:
                    cost = 0
                elif ip is None:
                    cost = None
                else:
                    cost = ip * qty
                opts.append(dict(id=iid, name=name, qty=qty, unit=ip, cost=cost, listed=inn,
                                 owned=owned.get(iid, 0), enough=have or inn >= qty))
            priced = [o for o in opts if o['cost'] is not None]
            # Cheapest bundle the AH can actually fill; a short supply only when nothing else is there.
            best = min(priced, key=lambda o: (not o['enough'], o['cost'])) if priced else None
            crate_cost = 0 if have_crate else cp
            note = []
            if have_crate:
                note.append(f'Kiste im Besitz ({owned[cid]}x)')
            elif cp is None:
                note.append('leere Kiste nicht im AH (Drop von Gegnern)')
            if best is None:
                note.append('kein Bündel mit AH-Preis')
            elif best['owned'] >= best['qty']:
                note.append(f"Bündel im Besitz ({best['owned']}x {best['name']})")
            elif not best['enough']:
                note.append(f"nur {best['listed']}x {best['name']} im AH")
            complete = best is not None and crate_cost is not None
            cost = (crate_cost or 0) + (best['cost'] if best else 0)
            rows.append(dict(crateId=cid, kind=kind, crateCost=crate_cost, crateListed=cn, haveCrate=have_crate,
                             options=opts, best=best, cost=cost, complete=complete,
                             per=cost / favor if complete else None,
                             perGreen=cost / favor_green if complete else None, note=', '.join(note)))
        rows.sort(key=lambda r: (not r['complete'], r['per'] if r['complete'] else r['cost']))
        tiers.append(dict(tier=t, name=CRATE_TIER_NAME[t], level=CRATE_LEVEL[t],
                          tooLow=level is not None and level < CRATE_LEVEL[t], favor=favor, favorGreen=favor_green,
                          favorSource=favor_source, favorGuess=t in CRATE_FAVOR_GUESS and mc is None,
                          sealedId=sc, rows=rows))
    ranked = sorted((dict(r, tier=t['tier'], tierName=t['name'], favor=t['favor']) for t in tiers
                     for r in t['rows'] if r['complete'] and not t['tooLow']), key=lambda r: r['per'])
    return dict(tiers=tiers, best=ranked[:top])


def fmt(c):
    c = int(round(c))
    if c < 0:
        return '-' + fmt(-c)
    return f"{c // 10000}g {c % 10000 // 100:02d}s" if c >= 10000 else f"{c // 100}s {c % 100:02d}c"


# ---------- main ----------
def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--wow', default=DEFAULT_WOW)
    ap.add_argument('--char', default='', help='Teil des Charakternamens (Ordner unter WTF), z.B. Malgruk')
    ap.add_argument('--top', type=int, default=3, help='Alternativen pro Stufe')
    ap.add_argument('--besitz-zum-ah-preis', action='store_true',
                    help='eigene Items mit AH-Preis rechnen statt gratis (was du verkaufen könntest)')
    ap.add_argument('--json', action='store_true', help='Ergebnis als JSON ausgeben (für die App)')
    a = ap.parse_args()
    try:
        r = compute(a.wow, a.char, a.top, a.besitz_zum_ah_preis)
    except FileNotFoundError as e:
        sys.exit(str(e))
    if a.json:
        import json
        print(json.dumps(r, ensure_ascii=False))
        return

    print(f"AH-Daten: letzter Scan-Tag {r['scanDate']}  |  Charakter: {r['character'] or '-'}")
    rep = r['reputation']
    if rep:
        print(f"Ruf {rep['name']}: {rep['standing']} {rep['value']}/{rep['max']} "
              f"(noch {rep['max'] - rep['value']} bis zur nächsten Stufe)")
    print("Hinweis: Preise = Mindest-Sofortkauf beim letzten Scan, keine Live-Angebote.\n")
    for t in r['tiers']:
        print(f"== {t['name']} ==")
        for n, b in enumerate(t['best']):
            mark = '>>' if n == 0 else '  '
            q = f" ({b['qty']}x)" if b['qty'] > 1 else ''
            print(f" {mark} {b['name'] + q:<36} {b['prof']:<9} Verf. {fmt(b['wcost']):>8} + Item {fmt(b['icost']):>8}"
                  f" - Beutel {fmt(b['payout']):>7} = netto {fmt(b['net']):>8}  {b['per'] / 100:.2f} s/Ruf"
                  f"  [{b['writs']} Verf. im AH] {b['note']}")
        if t['sealed']:
            print(f"    Tipp: du hast {t['sealed']}x versiegelte Verfügung dieser Stufe – erst öffnen, "
                  f"vielleicht ist sie billiger.")
        print()
    print(f"Pro Tag (je Stufe die beste): {fmt(r['dailyTotal'])}, netto nach Münzbeuteln {fmt(r['dailyNet'])}"
          f" für {r['dailyRep']} Ruf")
    src = {b['payoutSource'] for t in r['tiers'] for b in t['best']}
    print("Münzbeutel: " + ' | '.join(sorted(src)) + "\n")

    print(f"== Handelskisten (Händlergunst{f', du hast {r['favor']}' if r['favor'] is not None else ''}) ==")
    for t in r['crates']['tiers']:
        low = f"  (erst ab Stufe {t['level']})" if t['tooLow'] else ''
        print(f"-- {t['name']}: {t['favor']:g} Gunst, grün {t['favorGreen']:g} ({t['favorSource']}){low}")
        for c in t['rows'][:a.top]:
            b = c['best']
            bundle = f"{b['qty']}x {b['name']} {fmt(b['cost'])}" if b else '-'
            per = f"{c['per'] / 100:.2f} s/Gunst" if c['complete'] else 'unvollständig'
            crate = fmt(c['crateCost']) if c['crateCost'] is not None else '?'
            print(f"    {c['kind']:<18} Kiste {crate:>8} + {bundle:<40} = {fmt(c['cost']):>8}  {per}  {c['note']}")


if __name__ == '__main__':
    main()
