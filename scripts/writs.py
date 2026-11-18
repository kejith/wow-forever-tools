"""Best craftsman's writ per tier (Journeyman / Expert / Artisan) by silver per reputation, and the trade
crates by silver per Merchant's Favor.

Reads the Auctionator scan (Auctionator.lua) and the character's inventory (WoWMCP.lua). For every writ:
price of the writ + price of the item it asks for (0 if it is already in your bags or bank) minus the coin
pouch the quest pays, shown as the best per tier.

    python scripts/writs.py [--char Malgruk] [--top 3] [--wow "C:\\...\\_forever_"] [--lang en|de]

After a full auction house scan type /reload or log out once, so Auctionator saves its data.
"""
import argparse, datetime, glob, json, os, re, struct, sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import i18n  # noqa: E402
from i18n import mark, tr  # noqa: E402

# WOW_FOREVER_DIR wins; under WSL the Windows install is reached through /mnt/c.
DEFAULT_WOW = os.environ.get("WOW_FOREVER_DIR") or (
    "/mnt/c/Program Files (x86)/World of Warcraft/_forever_" if os.name == "posix"
    else r"C:\Program Files (x86)\World of Warcraft\_forever_")
REP = {"J": 75, "E": 125, "A": 200}
TIER_NAME = {"J": mark("Journeyman (75 reputation)"), "E": mark("Expert (125 reputation)"),
             "A": mark("Artisan (200 reputation)")}
SEALED = {"J": 262888, "E": 262889, "A": 262890}
# The writ factions: Azeroth Commerce Authority (Alliance), Durotar Supply and Logistics (Horde).
FACTIONS = {2586, 2587}
PROFESSION = {"alchemy": mark("Alchemy"), "blacksmithing": mark("Blacksmithing"), "engineering": mark("Engineering"),
              "leatherworking": mark("Leatherworking"), "tailoring": mark("Tailoring")}

# Writ item ID | tier | item it asks for | amount | profession   (source: wow.gg, Nov 2026)
WRITS = """264012|J|3383|1|alchemy
264019|J|5996|1|alchemy
264013|J|3385|1|alchemy
264017|J|3388|1|alchemy
264014|J|6048|1|alchemy
264016|J|3390|1|alchemy
264011|J|3391|1|alchemy
264015|J|5634|1|alchemy
264018|J|8951|1|alchemy
264062|E|3827|1|alchemy
264057|E|3823|1|alchemy
264064|E|3824|1|alchemy
264061|E|5633|1|alchemy
264056|E|6050|1|alchemy
264066|E|8949|1|alchemy
264059|E|6052|1|alchemy
264063|E|6049|1|alchemy
264060|E|10592|1|alchemy
264065|E|9061|1|alchemy
264067|E|9144|1|alchemy
264058|E|4623|1|alchemy
264117|A|12190|1|alchemy
264120|A|9088|1|alchemy
264124|A|246948|1|alchemy
264116|A|9224|1|alchemy
264119|A|9233|1|alchemy
264122|A|3387|1|alchemy
264118|A|13457|1|alchemy
264123|A|18253|1|alchemy
264121|A|13513|1|alchemy
264023|J|15869|1|blacksmithing
264027|J|3848|1|blacksmithing
264020|J|5540|1|blacksmithing
264025|J|2865|1|blacksmithing
264021|J|11128|1|blacksmithing
264024|J|3491|1|blacksmithing
264022|J|5541|1|blacksmithing
264028|J|3484|1|blacksmithing
264026|J|3485|1|blacksmithing
264075|E|7964|1|blacksmithing
264073|E|3853|1|blacksmithing
264077|E|11144|1|blacksmithing
264070|E|6041|1|blacksmithing
264074|E|3837|1|blacksmithing
264069|E|9060|1|blacksmithing
264078|E|9366|1|blacksmithing
264068|E|7922|1|blacksmithing
264079|E|7941|1|blacksmithing
264071|E|7928|1|blacksmithing
264072|E|7942|1|blacksmithing
264076|E|7943|1|blacksmithing
264127|A|7929|1|blacksmithing
264128|A|7969|1|blacksmithing
264132|A|15872|1|blacksmithing
264126|A|12416|1|blacksmithing
264133|A|7947|1|blacksmithing
264131|A|12775|1|blacksmithing
264125|A|12417|1|blacksmithing
264129|A|12422|1|blacksmithing
264130|A|20549|1|blacksmithing
264033|J|6712|1|engineering
264037|J|8069|200|engineering
264036|J|4380|1|engineering
264034|J|4373|1|engineering
264035|J|7506|1|engineering
264029|J|5507|1|engineering
264032|J|4381|1|engineering
264031|J|4383|1|engineering
264030|J|4384|1|engineering
264089|E|10498|1|engineering
264081|E|4392|1|engineering
264086|E|4852|1|engineering
264085|E|10560|1|engineering
264090|E|11590|1|engineering
264082|E|4393|1|engineering
264083|E|4395|1|engineering
264088|E|10514|1|engineering
264080|E|4396|1|engineering
264087|E|10500|1|engineering
264084|E|10501|1|engineering
264091|E|16005|1|engineering
264141|A|10548|1|engineering
264135|A|10504|1|engineering
264140|A|18631|1|engineering
264137|A|18645|1|engineering
264136|A|15996|1|engineering
264138|A|18587|1|engineering
264134|A|16023|1|engineering
264139|A|18232|1|engineering
264142|A|21277|1|engineering
264041|J|2317|1|leatherworking
264039|J|5958|1|leatherworking
264046|J|7284|1|leatherworking
264038|J|18662|1|leatherworking
264040|J|7349|1|leatherworking
264042|J|7358|1|leatherworking
264044|J|4252|1|leatherworking
264043|J|7371|1|leatherworking
264045|J|4254|1|leatherworking
264093|E|5965|1|leatherworking
264094|E|7378|1|leatherworking
264092|E|8174|1|leatherworking
264098|E|7391|1|leatherworking
264099|E|8192|1|leatherworking
264102|E|8189|1|leatherworking
264101|E|8200|1|leatherworking
264096|E|8210|1|leatherworking
264100|E|8201|1|leatherworking
264095|E|8214|1|leatherworking
264097|E|8217|1|leatherworking
264103|E|8218|1|leatherworking
264147|A|15084|1|leatherworking
264150|A|15092|1|leatherworking
264151|A|15071|1|leatherworking
264145|A|15053|1|leatherworking
264143|A|15087|1|leatherworking
264144|A|18258|1|leatherworking
264146|A|15094|1|leatherworking
264148|A|15075|1|leatherworking
264149|A|15080|1|leatherworking
264052|J|4313|1|tailoring
264053|J|10048|1|tailoring
264048|J|6263|1|tailoring
264054|J|6264|1|tailoring
264051|J|4317|1|tailoring
264047|J|5766|1|tailoring
264049|J|7048|1|tailoring
264055|J|4321|1|tailoring
264050|J|4245|1|tailoring
264115|E|7061|1|tailoring
264106|E|7063|1|tailoring
264109|E|4329|1|tailoring
264110|E|9998|1|tailoring
264113|E|10008|1|tailoring
264112|E|10052|1|tailoring
264107|E|10004|1|tailoring
264111|E|10009|1|tailoring
264104|E|10050|1|tailoring
264105|E|10023|1|tailoring
264108|E|22246|1|tailoring
264114|E|10042|1|tailoring
264155|A|10036|1|tailoring
264158|A|10025|1|tailoring
264153|A|14046|1|tailoring
264160|A|21340|1|tailoring
264159|A|14141|1|tailoring
264157|A|22251|1|tailoring
264154|A|13864|1|tailoring
264156|A|14108|1|tailoring
264152|A|14130|1|tailoring"""

# Gold on completion: the quest itself pays no gold (RewardMoney 0 in the game's quest cache). It gives a
# coin pouch (items 284583-284586) whose coins the server rolls when the pouch is opened. The addon (1.5+)
# measures the content; until then these estimates per tier apply, in copper.
POUCH_ESTIMATE = {"J": 3300, "E": 7000, "A": 7500}
POUCH_ESTIMATE_SOURCE = mark("estimate from conquestcapped.com (Nov 2026): Journeyman 22–45 s, Expert 50–90 s, "
                             "Artisan about 75 s")

# Trade crates: a waylaid (empty) crate is filled with exactly ONE of its bundles and handed in as a sealed
# crate at the trade representative for Merchant's Favor (currency 3402). No gold, no reputation.
# Amounts checked against the game (spell "Fill Crate", dummy effects in SpellEffect), items from wow.gg and
# urgametips.com (Nov 2026). Tier L = Apprentice.
FAVOR_CURRENCY = 3402
CRATE_TIERS = "LJEA"
CRATE_TIER_NAME = {"L": mark("Apprentice"), "J": mark("Journeyman"), "E": mark("Expert"), "A": mark("Artisan")}
CRATE_LEVEL = {"L": 10, "J": 10, "E": 20, "A": 35}
# Favor per sealed crate (common, uncommon) from urgametips.com/wow.gg; Artisan is extrapolated.
CRATE_FAVOR = {"L": (5, 10), "J": (10, 20), "E": (15, 30), "A": (20, 40)}
CRATE_FAVOR_GUESS = {"A"}
SEALED_CRATE = {t: (248765 + i, 248770 + i) for i, t in enumerate(CRATE_TIERS)}  # common, uncommon
CRATE_KIND = {"herbs": mark("Herbs"), "ore": mark("Ore"), "textiles": mark("Textiles"),
              "curiosities": mark("Curiosities"), "fabrics": mark("Fabrics"), "ingots": mark("Ingots"),
              "parts": mark("Parts"), "earthly_herbs": mark("Earthly herbs"), "flowering_herbs": mark("Flowering herbs")}

# Crate item | tier | kind | bundles as item ID:amount, separated by ;
CRATES = """248682|L|herbs|2447:20;765:20;2449:10;785:10;2450:10
248683|L|ore|2770:20;2771:20
248684|L|textiles|2589:40;2318:20
248685|L|curiosities|10940:20;10939:4;10978:6;11084:2
248702|L|fabrics|4231:2;2996:20
248703|L|ingots|2840:20;3576:20;2841:10
248704|L|parts|4359:16;4361:16;4363:4
248686|J|herbs|3820:10;2453:10;3355:10;3369:10;3356:10
248687|J|ore|2775:10;2772:20
248688|J|textiles|2592:20;4306:40;2319:20
248689|J|curiosities|11083:20;11082:4;11138:6;11139:2
248705|J|fabrics|2997:6;4305:10;4233:2
248706|J|ingots|2842:10;3575:20
248707|J|parts|4371:8;4375:7;4382:4;10558:45
248690|E|earthly_herbs|3357:20;3818:10;3821:10;3358:10
248691|E|flowering_herbs|3819:10;4625:10;8831:20;8836:10
248692|E|ore|2776:10;3858:20
248693|E|textiles|4338:40;4234:20
248694|E|curiosities|11137:20;11135:4;11177:6;11178:2
248708|E|fabrics|4339:8;4236:2
248709|E|ingots|3860:20;3859:15;3577:10
248710|E|parts|4387:8;10559:5;10560:12;10561:5
248695|A|earthly_herbs|13464:10;8838:20;8839:10;8845:10;8846:10
248696|A|flowering_herbs|13463:20;13465:20;13466:20;13467:20
248697|A|ore|7911:10;10620:20;11370:20
248698|A|textiles|14047:40;4304:20;8170:20
248699|A|curiosities|11176:20;11175:4;14343:6;14344:2
248711|A|fabrics|14048:8;8172:2
248712|A|ingots|11371:2;6037:10;12359:20
248713|A|parts|15994:5;18631:5;16000:2"""


def crate_list():
    """[(crate item, tier, kind, [(item, amount), ...]), ...]"""
    out = []
    for line in CRATES.splitlines():
        cid, tier, kind, opts = line.split('|')
        options = [tuple(int(x) for x in o.split(':')) for o in opts.split(';')]
        out.append((int(cid), tier, kind, options))
    return out


_names = None


def item_name(item_id):
    """Name of a writ or crate item in the selected language (data/item_names.json, from ItemSparse on
    wago.tools in enUS and deDE); English if the language has none."""
    global _names
    if _names is None:
        path = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'data', 'item_names.json')
        with open(path, encoding='utf-8') as f:
            _names = json.load(f)
    key = str(item_id)
    return (_names.get(i18n.language()) or {}).get(key) or _names['en'].get(key) or f"Item {item_id}"


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
        raise LookupError(tr("No price data found in Auctionator.lua."))
    return max(realms.values(), key=len)  # the realm with the most entries


def load_char(path):
    """Inventory counts (bags + bank) and the standing with the writ faction from WoWMCP.lua."""
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
        if name and '["standing"]' in block and int(idm.group(1)) in FACTIONS:
            g = lambda k: re.search(rf'\["{k}"\] = "?([^",]*)"?,', block).group(1)
            rep = (name.group(1), g('standing'), int(g('value')), int(g('max')))
    return owned, rep


# ---------- calculation ----------
def find_auctionator(wow):
    acct = glob.glob(os.path.join(wow, 'WTF', 'Account', '*', 'SavedVariables', 'Auctionator.lua'))
    if not acct:
        raise FileNotFoundError(tr("Auctionator.lua not found under {wow}", wow=wow))
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
    cache = cache or os.environ.get('WOW_TOOLS_CACHE') or os.path.join(os.path.dirname(os.path.abspath(__file__)), 'cache')
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
    tier_of = {int(line.split('|')[0]): line.split('|')[1] for line in WRITS.splitlines()}
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
            source = tr("measured: coin pouch opened {n}x, {low} to {high}", n=s['n'], low=fmt(s['min']), high=fmt(s['max']))
            return round(avg) + money, source, True
        return POUCH_ESTIMATE[tier] + money, tr(POUCH_ESTIMATE_SOURCE), False
    return value


def compute(wow=DEFAULT_WOW, char='', top=3, owned_at_ah_price=False, prices=None, extras=None, quest_of=None):
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
        wid, tier, item, qty, prof = line.split('|')
        wid, item, qty = int(wid), int(item), int(qty)
        wp, wn, _ = price(wid)
        ip, inn, _ = price(item)
        have_writ = owned.get(wid, 0) > 0
        have_item = owned.get(item, 0) >= qty
        if (wp is None and not have_writ) or (ip is None and not have_item):
            continue
        wcost = 0 if have_writ else wp
        icost = 0 if (have_item and not owned_at_ah_price) else (ip or 0) * qty
        cost = wcost + icost
        payout, payout_source, measured = payout_of(wid, tier)
        net = cost - payout
        note = []
        if have_writ: note.append(tr("you own the writ"))
        if have_item: note.append(tr("you own the item ({n}x)", n=owned[item]))
        if not have_item and inn < qty * 10: note.append(tr("only {n} items in the AH", n=inn))
        rows.append(dict(tier=tier, name=item_name(item), prof=tr(PROFESSION[prof]), cost=cost, payout=payout, net=net,
                         per=net / REP[tier], payoutSource=payout_source, payoutMeasured=measured, wcost=wcost,
                         icost=icost, writs=wn, note=', '.join(note), qty=qty, writId=wid, itemId=item,
                         questId=quest_of.get(wid)))

    tiers = []
    for t in 'JEA':
        best = sorted((r for r in rows if r['tier'] == t), key=lambda r: r['per'])[:top]
        tiers.append(dict(tier=t, name=tr(TIER_NAME[t]), rep=REP[t], best=best, sealed=owned.get(SEALED[t], 0)))
    total = sum(t['best'][0]['cost'] for t in tiers if t['best'])
    total_net = sum(t['best'][0]['net'] for t in tiers if t['best'])
    reputation = dict(name=rep[0], standing=rep[1], value=rep[2], max=rep[3]) if rep else None
    sd = scan_date(prices)
    return dict(scanDate=sd.isoformat() if sd else None, character=who, reputation=reputation,
                tiers=tiers, dailyTotal=total, dailyNet=total_net, dailyRep=sum(REP.values()),
                crates=crates(price, owned, owned_at_ah_price, payouts, level, top), favor=favor, level=level)


def crates(price, owned, owned_at_ah_price, payouts, level, top):
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
            favor_source = tr("measured: handed in {n}x", n=measured[sc]['n'])
        elif t in CRATE_FAVOR_GUESS:
            favor_source = tr("estimated, extrapolated from Apprentice 5, Journeyman 10, Expert 15")
        else:
            favor_source = tr("from urgametips.com and wow.gg (Nov 2026)")
        rows = []
        for cid, tier, kind, options in crate_list():
            if tier != t:
                continue
            cp, cn, _ = price(cid)
            have_crate = owned.get(cid, 0) > 0
            opts = []
            for iid, qty in options:
                ip, inn, _ = price(iid)
                have = owned.get(iid, 0) >= qty
                if have and not owned_at_ah_price:
                    cost = 0
                elif ip is None:
                    cost = None
                else:
                    cost = ip * qty
                opts.append(dict(id=iid, name=item_name(iid), qty=qty, unit=ip, cost=cost, listed=inn,
                                 owned=owned.get(iid, 0), enough=have or inn >= qty))
            priced = [o for o in opts if o['cost'] is not None]
            # Cheapest bundle the AH can actually fill; a short supply only when nothing else is there.
            best = min(priced, key=lambda o: (not o['enough'], o['cost'])) if priced else None
            crate_cost = 0 if have_crate else cp
            note = []
            if have_crate:
                note.append(tr("you own the crate ({n}x)", n=owned[cid]))
            elif cp is None:
                note.append(tr("empty crate not in the AH (drops from enemies)"))
            if best is None:
                note.append(tr("no bundle with an AH price"))
            elif best['owned'] >= best['qty']:
                note.append(tr("you own the bundle ({n}x {name})", n=best['owned'], name=best['name']))
            elif not best['enough']:
                note.append(tr("only {n}x {name} in the AH", n=best['listed'], name=best['name']))
            complete = best is not None and crate_cost is not None
            cost = (crate_cost or 0) + (best['cost'] if best else 0)
            rows.append(dict(crateId=cid, kind=tr(CRATE_KIND[kind]), kindKey=kind, name=item_name(cid),
                             crateCost=crate_cost, crateListed=cn, haveCrate=have_crate,
                             options=opts, best=best, cost=cost, complete=complete,
                             per=cost / favor if complete else None,
                             perGreen=cost / favor_green if complete else None, note=', '.join(note)))
        rows.sort(key=lambda r: (not r['complete'], r['per'] if r['complete'] else r['cost']))
        tiers.append(dict(tier=t, name=tr(CRATE_TIER_NAME[t]), level=CRATE_LEVEL[t],
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
    ap = argparse.ArgumentParser(description=__doc__.split('\n\n')[0])
    ap.add_argument('--wow', default=DEFAULT_WOW)
    ap.add_argument('--char', default='', help='part of the character name (folder under WTF), e.g. Malgruk')
    ap.add_argument('--top', type=int, default=3, help='alternatives per tier')
    ap.add_argument('--owned-at-ah-price', action='store_true',
                    help='count your own items at their AH price instead of free (what you could sell them for)')
    ap.add_argument('--json', action='store_true', help='print the result as JSON (for the app)')
    i18n.add_argument(ap)
    a = ap.parse_args()
    i18n.set_language(a.lang, a.wow)
    try:
        r = compute(a.wow, a.char, a.top, a.owned_at_ah_price)
    except (FileNotFoundError, LookupError) as e:
        sys.exit(str(e))
    sys.stdout.reconfigure(encoding='utf-8')
    if a.json:
        print(json.dumps(r, ensure_ascii=False))
        return

    print(tr("AH data: last scan day {date}  |  character: {char}", date=r['scanDate'], char=r['character'] or '-'))
    rep = r['reputation']
    if rep:
        print(tr("Reputation {name}: {standing} {value}/{max} ({left} to the next standing)", name=rep['name'],
                 standing=rep['standing'], value=rep['value'], max=rep['max'], left=rep['max'] - rep['value']))
    print(tr("Note: prices are the lowest buyout at the last scan, not live auctions.") + "\n")
    for t in r['tiers']:
        print(f"== {t['name']} ==")
        for n, b in enumerate(t['best']):
            mark_ = '>>' if n == 0 else '  '
            q = f" ({b['qty']}x)" if b['qty'] > 1 else ''
            print(f" {mark_} {b['name'] + q:<36} {b['prof']:<16} "
                  + tr("writ {writ} + item {item} - pouch {pouch} = net {net}  {per} s/rep  [{listed} writs in the AH]",
                       writ=f"{fmt(b['wcost']):>8}", item=f"{fmt(b['icost']):>8}", pouch=f"{fmt(b['payout']):>7}",
                       net=f"{fmt(b['net']):>8}", per=f"{b['per'] / 100:.2f}", listed=b['writs'])
                  + f" {b['note']}")
        if t['sealed']:
            print("    " + tr("Tip: you have {n}x sealed writ of this tier. Open it first, it may be cheaper.", n=t['sealed']))
        print()
    print(tr("Per day (the best of each tier): {total}, net after coin pouches {net} for {rep} reputation",
             total=fmt(r['dailyTotal']), net=fmt(r['dailyNet']), rep=r['dailyRep']))
    src = {b['payoutSource'] for t in r['tiers'] for b in t['best']}
    print(tr("Coin pouches: {sources}", sources=' | '.join(sorted(src))) + "\n")

    have = tr(", you have {n}", n=r['favor']) if r['favor'] is not None else ''
    print("== " + tr("Trade crates (Merchant's Favor{have})", have=have) + " ==")
    for t in r['crates']['tiers']:
        low = "  " + tr("(from level {level})", level=t['level']) if t['tooLow'] else ''
        print("-- " + tr("{tier}: {favor} favor, green {green} ({source})", tier=t['name'], favor=f"{t['favor']:g}",
                         green=f"{t['favorGreen']:g}", source=t['favorSource']) + low)
        for c in t['rows'][:a.top]:
            b = c['best']
            bundle = f"{b['qty']}x {b['name']} {fmt(b['cost'])}" if b else '-'
            per = tr("{per} s/favor", per=f"{c['per'] / 100:.2f}") if c['complete'] else tr("incomplete")
            crate = fmt(c['crateCost']) if c['crateCost'] is not None else '?'
            print(f"    {c['kind']:<18} " + tr("crate {crate} + {bundle} = {total}  {per}", crate=f"{crate:>8}",
                                               bundle=f"{bundle:<40}", total=f"{fmt(c['cost']):>8}", per=per)
                  + f"  {c['note']}")


if __name__ == '__main__':
    main()
