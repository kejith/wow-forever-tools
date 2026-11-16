# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Ausrüstungs-Upgrades aus dem Auktionshaus für einen Charakter (Standard: Gebrechen-Hexer).

Bewertung: Gewichte pro Wert wie bei Pawn (1 Punkt = 1 Schatten-Zaubermacht), aber aus dem Charakter abgeleitet
(Treffer nur bis zum Cap, Krit nach Talenten) und immer für das ganze Set gerechnet. Dazu das beste Paket für
das Budget (Rucksackproblem über alle Plätze, mit Ring-Paaren und Waffenhand + Schildhand gegen Zweihand).

Liest
  * den Auctionator-Scan (Mindest-Sofortkauf, wie scripts/verfuegungen.py),
  * aus WoWMCP.lua die angelegte Ausrüstung (pro Charakter) und den Item-Export des Addons
    (Account-weit, WoWMCP_Items: Werte und Tooltips aller ausrüstbaren Items aus den Auctionator-Scans).

Ablauf: im AH scannen, AH schließen (das Addon liest neue Items automatisch) oder "/wowmcp items",
auf die Meldung warten, dann /reload. Danach:
  uv run scripts\\upgrades.py --char Malgruk
"""

from __future__ import annotations

import argparse
import datetime
import glob
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import verfuegungen as v  # noqa: E402  (load_prices, DEFAULT_WOW)

# Bewertung im Stil von Pawn: jeder Wert bekommt ein Gewicht, die Einheit ist 1 Punkt Schatten-Zaubermacht.
# Anders als eine feste Pawn-Skala hängen Treffer und Krit vom Charakter ab (build_scale), und bewertet wird
# immer das ganze Set (set_value). So zählt Treffer nur bis zum Cap.
PROFILES = {
    # target: Stufenvorsprung der Gegner, aff_share: Anteil des Schadens aus Gebrechen-Zaubern (DoTs, Entzüge).
    # mana_share: 1 % mehr Vorrat (Mana + per Aderlass nutzbares Leben) zählt wie mana_share % mehr Schaden.
    # survival_share: 1 % mehr Leben zählt zusätzlich wie survival_share % mehr Schaden (Überleben, weniger Rast).
    # Beide sind Erfahrungswerte (so gewählt, dass Malgruk auf Stufe 46 ungefähr bei den alten festen Gewichten
    # landet); Intelligenz und Ausdauer rechnet build_scale daraus mit Stufe, Werten und Talenten aus.
    # weights: feste Gewichte; stamina/intellect nur, wenn der Export keine Leben-/Mana-Werte hat.
    "leveln": dict(label="Leveln", target=2, aff_share=0.75, mana_share=0.10, survival_share=0.05,
                   weights={"stamina": 0.5, "intellect": 0.35, "spirit": 0.25, "mp5": 0.7, "hp5": 0.15,
                            "armor": 0.01, "wand_dps": 0.6}),
    "dungeon": dict(label="Dungeon/Raid", target=3, aff_share=0.75, mana_share=0.06, survival_share=0.02,
                    weights={"stamina": 0.25, "intellect": 0.25, "spirit": 0.05, "mp5": 0.3, "hp5": 0.0,
                             "armor": 0.0, "wand_dps": 0.1}),
}
BASE_SPELL_CRIT = 1.7  # Zauberkrit ohne Intelligenz (Hexer); nur für Exporte ohne critFromIntellect (Addon < 1.3)
MANA_PER_INT = 15  # MANA_PER_INTELLECT im Spiel
HP_PER_STAMINA = 10  # UnitHPPerStamina im Spiel
IMPROVED_LIFE_TAP = 0.1  # Verbesserter Aderlass: +10 % Mana pro Rang (Classic-Wert, in Forever nicht geprüft)
# Fehlschlagchance von Zaubern gegen Gegner mit +0 bis +3 Stufen. Quelle: Forevers eigene Charakteranzeige
# (Blizzard_UIPanels_Game/Camelot/PaperDollFrameStats.lua, spellMissChances).
SPELL_MISS = {0: 4.0, 1: 5.0, 2: 6.0, 3: 17.0}
MIN_MISS = 1.0  # wie in Classic: 1 % Fehlschlag bleibt immer
SUPPRESSION_PER_RANK = 2.0  # Unterdrückung: Trefferchance für Gebrechen-Zauber pro Rang (Classic-Wert)
BASE_PER_LEVEL = 13  # Grundschaden / Zaubermacht-Koeffizient der Hexer-Zauber: in Classic rund 13 pro Stufe
CRIT_BONUS = 0.5  # Zauber-Krits machen 150 % Schaden
DEFAULT_RATINGS = {"hit": 10.0, "crit": 14.0}  # Wertung pro 1 %, wird aus den Tooltips des Exports nachgemessen
SP_KEYS = ("spell_power", "spell_damage_done", "shadow_damage_done")
LINEAR_KEYS = {"stamina": "stamina", "intellect": "intellect", "spirit": "spirit", "mana_regeneration": "mp5",
               "health_regen": "hp5", "armor": "armor"}
PCT_LINE = {"hit": re.compile(r"Verbessert Eure Trefferchance um ([\d.,]+)\s*%"),
            "crit": re.compile(r"Erhöht Eure kritische Trefferchance um ([\d.,]+)\s*%")}
# "Anlegen:"-Zeilen, die nur einen Wert aus `stats` ausschreiben. Alles andere gilt als Spezialeffekt.
STAT_LINE = re.compile(
    r"^Anlegen: (Erhöht (?!.*Schaden gegen ).*(Schaden|Heilung).*um bis zu \d+|\+\d+ Angriffskraft"
    r"|Verbessert Eure Trefferchance um|Erhöht Eure kritische Trefferchance um|Eure Zauber durchdringen \d+"
    r"|Stellt alle \d+ Sek\. \d+ (Mana|Gesundheit) wieder her|Verteidigung \+\d+)")

SLOT_OF = {  # equipLoc -> Inventarplätze
    "INVTYPE_HEAD": [1], "INVTYPE_NECK": [2], "INVTYPE_SHOULDER": [3], "INVTYPE_CHEST": [5], "INVTYPE_ROBE": [5],
    "INVTYPE_WAIST": [6], "INVTYPE_LEGS": [7], "INVTYPE_FEET": [8], "INVTYPE_WRIST": [9], "INVTYPE_HAND": [10],
    "INVTYPE_FINGER": [11, 12], "INVTYPE_TRINKET": [13, 14], "INVTYPE_CLOAK": [15],
    "INVTYPE_WEAPON": [16], "INVTYPE_WEAPONMAINHAND": [16], "INVTYPE_HOLDABLE": [17],
    "INVTYPE_RANGED": [18], "INVTYPE_RANGEDRIGHT": [18], "INVTYPE_2HWEAPON": [16, 17],
}
SLOT_NAME = {1: "Kopf", 2: "Hals", 3: "Schulter", 5: "Brust", 6: "Taille", 7: "Beine", 8: "Füße", 9: "Handgelenk",
             10: "Hände", 11: "Finger", 12: "Finger", 13: "Schmuck", 14: "Schmuck", 15: "Rücken",
             16: "Waffenhand", 17: "Schildhand", 18: "Zauberstab"}
USABLE_WEAPONS = {7, 10, 15, 19}  # Einhandschwert, Stab, Dolch, Zauberstab
NEW_ID = 200000  # Item-IDs darüber gibt es nur in Forever
PAIRS = {11: (11, 12), 12: (11, 12), 13: (13, 14), 14: (13, 14)}  # Plätze, die es doppelt gibt


def stat_key(k: str) -> str:
    return "armor" if k == "RESISTANCE0_NAME" else k.removeprefix("ITEM_MOD_").removesuffix("_SHORT").lower()


def lua_to_py(x):
    from lupa.lua54 import lua_type
    if lua_type(x) != "table":
        return x
    keys = list(x.keys())
    if keys and all(isinstance(k, int) for k in keys) and sorted(keys) == list(range(1, len(keys) + 1)):
        return [lua_to_py(x[k]) for k in sorted(keys)]
    return {k: lua_to_py(x[k]) for k in keys}


def load_sv(path: str, name: str):
    """Runs a SavedVariables file in a sandboxed Lua and returns one global as Python data."""
    from lupa.lua54 import LuaRuntime
    lua = LuaRuntime(register_eval=False, register_builtins=False)
    for g in ("os", "io", "package", "require", "dofile", "loadfile", "debug"):
        lua.execute(f"{g} = nil")
    lua.execute(open(path, encoding="utf-8", errors="replace").read())
    return lua_to_py(lua.globals()[name])


def load_char(path: str) -> dict:
    return load_sv(path, "WoWMCP_Char") or {}


def learn_ratings(items: dict) -> dict:
    """Rating per 1 % hit/crit, measured from items whose tooltip spells out the percentage."""
    out = dict(DEFAULT_RATINGS)
    for kind, pat in PCT_LINE.items():
        ratios = []
        for it in items.values():
            rating = (it.get("stats") or {}).get(f"ITEM_MOD_{kind.upper()}_RATING_SHORT")
            pct = sum(float(m.group(1).replace(",", ".")) for t in it.get("tooltip") or [] for m in [pat.search(t)] if m)
            if rating and pct:
                ratios.append(rating / pct)
        if ratios:
            out[kind] = sorted(ratios)[len(ratios) // 2]
    return out


def norm_stats(rec: dict | None, ranged: bool = False, ratings: dict | None = None) -> dict:
    """An item's stats in the units of the scale: sp, stamina, intellect, spirit, mp5, hp5, armor, wand_dps and
    hit/crit in percent. Everything the scale ignores (healing, other schools, melee stats) is dropped."""
    ratings = ratings or DEFAULT_RATINGS
    rec = rec or {}
    raw = {stat_key(k): val for k, val in (rec.get("stats") or {}).items() if isinstance(val, (int, float))}
    n = {"sp": sum(raw.get(k, 0) for k in SP_KEYS)}
    for k, name in LINEAR_KEYS.items():
        n[name] = raw.get(k, 0)
    if ranged:
        n["wand_dps"] = raw.get("damage_per_second", 0)
    for kind, pat in PCT_LINE.items():
        rating = raw.get(f"{kind}_rating", 0)
        if rating:
            pct = sum(float(m.group(1).replace(",", ".")) for t in rec.get("tooltip") or [] for m in [pat.search(t)] if m)
            n[kind] = pct or rating / ratings[kind]
    return {k: val for k, val in n.items() if val}


def add_stats(total: dict, n: dict, sign: int = 1) -> None:
    for k, val in n.items():
        total[k] = total.get(k, 0) + sign * val


def crit_per_intellect(st: dict, gear_crit: float = 0.0) -> tuple[float | None, str]:
    """Spell crit in percent per point of intellect at the character's level, and where the number comes from."""
    intellect = st.get("intellect") or 0
    if intellect <= 0:
        return None, ""
    if st.get("critFromIntellect") is not None:
        return st["critFromIntellect"] / intellect, "laut Spiel"
    crit = (st.get("spellCrit") or {}).get("shadow")
    if crit is None:
        return None, ""
    return max(0.0, (crit - BASE_SPELL_CRIT - gear_crit) / intellect), f"geschätzt: Zauberkrit − {BASE_SPELL_CRIT:g} %"


def life_tap_ratio(st: dict, level: int, ilt_rank: int) -> tuple[float, str]:
    """Mana per point of health from Life Tap, with Improved Life Tap, and a note on the source."""
    lt = st.get("lifeTap")
    text = (lt or {}).get("text") or ""
    m = re.search(r"([\d.]+)\s*Gesundheit\D+?([\d.]+)\s*Mana", text)
    if m and float(m.group(1).replace(".", "")) > 0:
        ratio = float(m.group(2).replace(".", "")) / float(m.group(1).replace(".", ""))
        source = "laut Zaubertext"
    elif lt is None and "hpPerStamina" in st:  # Addon 1.3 exportiert und kennt keinen Aderlass
        return 0.0, "Aderlass nicht bekannt"
    elif level < 6:
        return 0.0, "Aderlass erst ab Stufe 6"
    else:
        ratio, source = 1.0, "Annahme 1:1"
    if ilt_rank:
        ratio *= 1 + IMPROVED_LIFE_TAP * ilt_rank
        source += f", Verbesserter Aderlass {ilt_rank}/2"
    return ratio, source


def build_scale(char: dict, profile: str = "", gear_hit: float = 0.0, gear_crit: float = 0.0) -> dict:
    """Weights for this character, in points of shadow spell power.

    Hit and crit are derived instead of fixed: 1 % more damage is worth (base damage / coefficient + spell power)
    / 100 spell power, hit counts only up to the cap against the profile's enemies, crit only for the share of
    damage that can crit. Intellect and stamina follow from what one point gives at this level (mana, crit from
    intellect, health, Life Tap) and the profile's mana_share/survival_share. Spirit and regeneration are
    judgement values per profile."""
    ch = char.get("character") or {}
    st = ch.get("stats") or {}
    level = ch.get("level") or 60
    profile = profile if profile in PROFILES else ("leveln" if level < 60 else "dungeon")
    p = PROFILES[profile]
    talents = {t.get("name"): t.get("rank") or 0 for tree in ((char.get("talents") or {}).get("class") or [])
               for t in (tree.get("talents") or []) if isinstance(t, dict)}
    spell_power = (st.get("spellDamage") or {}).get("shadow") or 0
    pct = (BASE_PER_LEVEL * level + spell_power) / 100

    base_hit = st.get("spellHit") or 0  # GetSpellHitModifier: hit from talents and auras, without gear
    cap = SPELL_MISS[p["target"]] - MIN_MISS
    need = max(0.0, cap - base_hit)
    suppression = SUPPRESSION_PER_RANK * talents.get("Unterdrückung", 0)
    need_aff = max(0.0, need - suppression)
    hit_steps = [(need_aff, p["aff_share"]), (need, 1 - p["aff_share"])]

    pandemic = talents.get("Pandemie", 0)
    crit_share = 0.25 + 0.6 * min(pandemic, 3) / 3  # ohne Pandemie kritten nur Schattenblitz und Co.
    weights = {"sp": 1.0, "crit": round(pct * crit_share * CRIT_BONUS, 2), **p["weights"]}

    hit_now = pct * sum(share for limit, share in hit_steps if gear_hit < limit)
    if need <= 0:
        hit_note = f"{base_hit:g} % ohne Ausrüstung vorhanden, Cap gegen +{p['target']} Stufen ist {cap:g} %: schon erreicht"
    else:
        hit_note = (f"Cap gegen +{p['target']} Stufen {cap:g} %, {base_hit:g} % vorhanden, es fehlen {need:g} %"
                    + (f" (Gebrechen-Zauber durch Unterdrückung nur {need_aff:g} %)" if suppression else "")
                    + f", Ausrüstung bringt {gear_hit:g} %")
    crit_note = f"1 % Schaden = {pct:.1f} Zaubermacht, {crit_share:.0%} des Schadens kann kritisch treffen"
    if pandemic:
        crit_note += " (Annahme: Pandemie lässt DoTs kritisch treffen)"

    # Intellect and stamina: what one point gives at this level, valued against the mana pool.
    stamina_note, intellect_note = "Leben, über Aderlass auch Mana (fester Wert)", "15 Mana und etwas Krit (fester Wert)"
    mana, health = st.get("powerMax") or 0, st.get("healthMax") or 0
    cpi, cpi_source = crit_per_intellect(st, gear_crit)
    if mana > 0 and health > 0 and cpi is not None:
        tap, tap_source = life_tap_ratio(st, level, talents.get("Verbesserter Aderlass", 0))
        pool = mana + health * tap
        per_mana = p["mana_share"] * pct * 100 / pool
        per_health = p["survival_share"] * pct * 100 / health
        mpi = st.get("manaPerIntellect") or MANA_PER_INT
        hps = st.get("hpPerStamina") or HP_PER_STAMINA
        int_mana, int_crit = mpi * per_mana, cpi * weights["crit"]
        sta_tap, sta_live = hps * tap * per_mana, hps * per_health
        weights["intellect"] = round(int_mana + int_crit, 3)
        weights["stamina"] = round(sta_tap + sta_live, 3)
        intellect_note = (f"{mpi:g} Mana = {int_mana:.2f}, {cpi:.4f} % Krit ({cpi_source}) = {int_crit:.2f}. "
                          f"1 Mana = {per_mana:.3f}: Vorrat {pool:.0f} (Mana + Leben über Aderlass), "
                          f"1 % mehr Vorrat zählt wie {p['mana_share']:g} % Schaden")
        stamina_note = (f"{hps:g} Leben: über Aderlass {hps * tap:.1f} Mana ({tap_source}) = {sta_tap:.2f}, "
                        f"Überleben = {sta_live:.2f} (1 % mehr Leben zählt wie {p['survival_share']:g} % Schaden)")
    w = weights
    rows = [
        dict(label="Schatten-/Zaubermacht", weight=1.0, note="Bezugsgröße"),
        dict(label="Ausdauer", weight=w["stamina"], note=stamina_note),
        dict(label="Intelligenz", weight=w["intellect"], note=intellect_note),
        dict(label="Willenskraft", weight=w["spirit"], note="Regeneration außerhalb des Kampfes"),
        dict(label="Mana pro 5 Sek.", weight=w["mp5"], note=""),
        dict(label="Treffer, nächstes 1 %", weight=round(hit_now, 2), note=hit_note),
        dict(label="Krit, je 1 %", weight=w["crit"], note=crit_note),
        dict(label="Zauberstab-DPS", weight=w["wand_dps"], note="nur im Zauberstab-Platz"),
    ]
    text = (f"Schatten/Zaubermacht 1, Ausdauer {w['stamina']:.2f}, Int {w['intellect']:.2f}, Willenskraft {w['spirit']:g}, "
            f"Mana/5s {w['mp5']:g}, Treffer 1 % = {hit_now:.1f}, Krit 1 % = {w['crit']:.1f}, "
            f"Zauberstab-DPS {w['wand_dps']:g}")
    return dict(profile=profile, label=p["label"], pct=pct, weights=weights, hitSteps=hit_steps, rows=rows, text=text)


def set_value(total: dict, scale: dict) -> float:
    """Value of a whole set of stats. Linear except for hit, which stops counting at the cap."""
    val = sum(w * total.get(k, 0) for k, w in scale["weights"].items())
    hit = max(0.0, total.get("hit", 0))
    return val + scale["pct"] * sum(share * min(hit, limit) for limit, share in scale["hitSteps"])


def equipped_stats(char: dict, ratings: dict | None = None) -> tuple[dict, dict]:
    """(normalised stats per slot, their sum) for the character's equipped items."""
    eq = {e["slot"]: norm_stats(e, e["slot"] == 18, ratings) for e in (char.get("equipment") or []) if e.get("slot")}
    total: dict = {}
    for n in eq.values():
        add_stats(total, n)
    return eq, total


def special_effects(tip: list[str]) -> list[str]:
    """Tooltip effects the scale cannot rate. "Trefferchance:" procs need melee hits and are left out."""
    return [t for t in tip if t.startswith("Benutzen:") or (t.startswith("Anlegen:") and not STAT_LINE.match(t))]


def pareto(states: list[tuple]) -> list[tuple]:
    """Keeps the (cost, gain, ...) states where more money buys more gain."""
    out, best = [], float("-inf")
    for st in sorted(states, key=lambda x: (x[0], -x[1])):
        if st[1] > best + 1e-9:
            out.append(st)
            best = st[1]
    return out


def best_package(groups: list[list[tuple]], budget: int) -> tuple:
    """Multiple-choice knapsack: at most one option (cost, gain, picks) per group, highest total gain within budget."""
    frontier = [(0, 0.0, ())]
    for options in groups:
        options = pareto([o for o in options if o[1] > 0 and o[0] <= budget])
        states = list(frontier)
        for cost, gain, picks in frontier:
            states += [(cost + oc, gain + og, picks + op) for oc, og, op in options if cost + oc <= budget]
        frontier = pareto(states)
        if len(frontier) > 4000:  # thin out: best state per price bucket
            step = max(1, budget // 4000)
            frontier = pareto(list({st[0] // step: st for st in frontier}.values()))
    return frontier[-1]


def short(stats: dict | None) -> str:
    names = {"spell_power": "ZM", "spell_damage_done": "ZS", "shadow_damage_done": "Schatten", "stamina": "Ausd",
             "intellect": "Int", "spirit": "Wil", "hit_rating": "Trefferwertung", "crit_rating": "Kritwertung",
             "mana_regeneration": "Mana/5s", "damage_per_second": "DPS"}
    raw = {stat_key(k): val for k, val in (stats or {}).items()}  # Lua hands the keys over in random order
    return ", ".join(f"{n} {raw[k]:g}" if isinstance(raw[k], (int, float)) else n for k, n in names.items() if k in raw)


SKILL_REQ = re.compile(r"^Benötigt (.+?) \((\d+)\)$")


def meets_skill_requirements(tooltip: list[str], skills: dict[str, int]) -> bool:
    """False for tooltip lines like "Benötigt Ingenieurskunst (215)" the character does not meet."""
    for t in tooltip:
        m = SKILL_REQ.match(t)
        if m and skills.get(m.group(1), 0) < int(m.group(2)):
            return False
    return True


STAT_LABEL = {"stamina": "Ausdauer", "intellect": "Intelligenz", "spirit": "Willenskraft", "strength": "Stärke",
              "agility": "Beweglichkeit"}
SPELL_LABEL = {
    "spell_power": "Anlegen: Erhöht durch magische Zauber und Effekte verursachten Schaden und Heilung um bis zu {}.",
    "spell_damage_done": "Anlegen: Erhöht den von magischen Zaubern und Effekten verursachten Schaden um bis zu {}.",
    "shadow_damage_done": "Anlegen: Erhöht den von Schattenzaubern und -effekten verursachten Schaden um bis zu {}.",
    "spell_healing_done": "Anlegen: Erhöht durch Zauber und Effekte verursachte Heilung um bis zu {}.",
}


def tooltip(rec: dict, slot_name: str | None = None) -> dict:
    """Tooltip for the app: the game's own lines when the addon exported them (1.2+: with colours), otherwise
    rebuilt from the stats so older snapshots still show something useful."""
    out = {"name": rec.get("name"), "quality": rec.get("quality"), "ilvl": rec.get("ilvl")}
    if rec.get("tooltip"):
        out["lines"] = rec["tooltip"]
        out["colors"] = rec.get("tipColors")
        return out
    lines, colors, equip = [], [], []
    if slot_name or rec.get("subType"):
        lines.append(f"{slot_name or ''} | {rec.get('subType') or ''}".strip(" |"))
        colors.append("ffffff")
    stats = {stat_key(k): val for k, val in (rec.get("stats") or {}).items()}
    if stats.get("armor"):
        lines.append(f"{stats['armor']:g} Rüstung")
        colors.append("ffffff")
    if stats.get("damage_per_second"):
        lines.append(f"({stats['damage_per_second']:.1f} Schaden pro Sekunde)")
        colors.append("ffffff")
    for k, label in STAT_LABEL.items():
        if stats.get(k):
            lines.append(f"+{stats[k]:g} {label}")
            colors.append("ffffff")
    dur = rec.get("durability")
    if isinstance(dur, list) and len(dur) == 2:
        lines.append(f"Haltbarkeit {dur[0]} / {dur[1]}")
        colors.append("ffffff")
    if rec.get("reqLevel"):
        lines.append(f"Benötigt Stufe {rec['reqLevel']}")
        colors.append("ffffff")
    for k, text in SPELL_LABEL.items():
        if stats.get(k):
            equip.append(text.format(f"{stats[k]:g}"))
    lines += equip
    colors += ["1eff00"] * len(equip)
    out.update(lines=lines, colors=colors, rebuilt=True)
    return out


def money(c: int) -> str:
    return f"{c // 10000}g {c // 100 % 100:02d}s" if c >= 10000 else f"{c // 100}s {c % 100:02d}c"


def load_item_export(wow: str, char: dict) -> dict:
    """Item export of "/wowmcp items": account-wide WoWMCP_Items (addon 1.1+), else the older per-character copy."""
    best: dict = {}
    for path in glob.glob(os.path.join(wow, "WTF", "Account", "*", "SavedVariables", "WoWMCP.lua")):
        data = load_sv(path, "WoWMCP_Items") or {}
        if (data.get("updated") or 0) > (best.get("updated") or 0):
            best = data
    if not best.get("items"):
        best = char.get("itemInfo") or {}
    return best


def compute(wow: str = v.DEFAULT_WOW, char: str = "", level: int = 0, vorschau: int = 4, max_preis: float = 0,
            top: int = 3, prices: dict | None = None, profil: str = "", sort: str = "gewinn") -> dict:
    """Upgrade candidates per slot and the best purchase for the budget, as plain data (copper prices)."""
    prices = prices if prices is not None else v.load_prices(v.find_auctionator(wow))
    last = max(int(k) for e in prices.values() if isinstance(e, dict) for k in (e.get("a") or {}))
    files = v.find_chars(wow, char)
    if not files:
        raise FileNotFoundError("Keine WoWMCP.lua für diesen Charakter gefunden.")
    c = load_char(files[0])
    info = load_item_export(wow, c)
    items = {int(k): x for k, x in (info.get("items") or {}).items()}
    if not items:
        raise LookupError("Kein Item-Export gefunden: im Spiel /wowmcp items, auf die Meldung warten, dann /reload.")
    level = level or (c.get("character") or {}).get("level") or 60
    gold = (c.get("character") or {}).get("money") or 0
    budget = int(max_preis * 10000) if max_preis else gold

    equipped = {e["slot"]: e for e in (c.get("equipment") or [])}
    skills = {s["name"]: s.get("rank") or 0 for s in ((c.get("skills") or {}).get("skills") or [])
              if isinstance(s, dict) and s.get("name")}
    ratings = learn_ratings(items)
    eq, totals = equipped_stats(c, ratings)
    scale = build_scale(c, profil, totals.get("hit", 0), totals.get("crit", 0))
    base = set_value(totals, scale)
    two_handed = (equipped.get(16) or {}).get("equipLoc") == "INVTYPE_2HWEAPON"

    def swap(remove: list[int], adds: list[dict]) -> float:
        """Change of the set's value when the items in the `remove` slots make room for `adds`."""
        t = dict(totals)
        for s in remove:
            add_stats(t, eq.get(s, {}), -1)
        for n in adds:
            add_stats(t, n)
        return set_value(t, scale) - base

    def item_out(x: dict, gain: float | None = None) -> dict:
        it, gain = x["it"], x["gain"] if gain is None else gain
        old = -swap(x["remove"], [])
        return dict(id=x["id"], name=it.get("name"), quality=it.get("quality"), reqLevel=it.get("reqLevel") or 0,
                    price=x["price"], gain=round(gain, 1), gainPct=round(gain / old * 100) if old > 0.5 else None,
                    perGold=round(gain / (x["price"] / 10000), 1), stats=short(it.get("stats")),
                    isNew=x["id"] >= NEW_ID, soon=x["soon"], effects=x["extra"], twoHand=len(x["remove"]) == 2,
                    tip=tooltip(it))

    pool, review = [], []
    for iid, it in items.items():
        slots = SLOT_OF.get(it.get("equipLoc") or "")
        req = it.get("reqLevel") or 0
        if not slots or req > level + vorschau or (it.get("quality") or 0) < 2:
            continue
        if it.get("classID") == 2 and it.get("subclassID") not in USABLE_WEAPONS:
            continue
        if it.get("classID") == 4 and it.get("subclassID") not in (0, 1):
            continue
        tip = it.get("tooltip") or []
        if any(t.startswith("Klassen:") and "Hexenmeister" not in t for t in tip):
            continue
        if not meets_skill_requirements(tip, skills):
            continue
        e = prices.get(str(iid))
        if not isinstance(e, dict) or not e.get("m"):
            continue
        if not any(int(d) >= last - 3 for d in (e.get("a") or {})):
            continue
        price = e["m"]
        if price > budget:
            continue
        n = norm_stats(it, slots == [18], ratings)
        if slots == [16, 17]:
            slot, remove = 16, [16, 17]
        elif len(slots) == 2:  # ring or trinket: like Pawn, against the weaker of the two
            slot = min(slots, key=lambda s: -swap([s], []))
            remove = [slot]
        elif slots == [17] and two_handed:  # an off-hand item means giving up the staff
            slot, remove = 17, [16]
        else:
            slot, remove = slots[0], [slots[0]]
        x = dict(id=iid, it=it, price=price, slot=slot, remove=remove, norm=n, gain=swap(remove, [n]),
                 points=swap([], [n]), extra=special_effects(tip), soon=req > level)
        pool.append(x)
        if x["extra"] and x["gain"] < 5 and req >= level - 15:
            review.append(dict(slot=SLOT_NAME[slot], **item_out(x)))

    rows = [x for x in pool if x["gain"] > 0.5 and not (x["extra"] and x["gain"] < 5)]
    order = (lambda x: (-x["gain"] / x["price"], -x["gain"])) if sort == "preis" else (lambda x: (-x["gain"], x["price"]))
    slots_out = []
    for slot in sorted({x["slot"] for x in rows}):
        cur = equipped.get(slot)
        best = sorted((x for x in rows if x["slot"] == slot), key=order)[:top]
        slots_out.append(dict(
            slot=slot, slotName=SLOT_NAME[slot],
            current=dict(name=cur.get("name"), id=cur.get("id"), quality=cur.get("quality"),
                         stats=short(cur.get("stats")), tip=tooltip(cur, SLOT_NAME[slot])) if cur else None,
            candidates=[item_out(x) for x in best]))

    # Best purchase for the budget: one option per slot group, pairs for rings/trinkets and main hand + off hand.
    usable = [x for x in pool if not x["soon"]]
    groups: dict = {}
    for x in rows:
        if not x["soon"]:
            key = PAIRS.get(x["slot"], ("weapon",) if x["slot"] in (16, 17) else (x["slot"],))
            groups.setdefault(key, []).append((x["price"], x["gain"], ((x, x["remove"]),)))
    for a, b in set(PAIRS.values()):
        both = sorted((x for x in usable if x["slot"] in (a, b)), key=lambda x: -x["points"])[:12]
        groups.setdefault((a, b), []).extend(
            (x["price"] + y["price"], swap([a, b], [x["norm"], y["norm"]]), ((x, [a]), (y, [b])))
            for i, x in enumerate(both) for y in both[i + 1:])
    one_hand = sorted((x for x in usable if x["slot"] == 16 and len(x["remove"]) == 1), key=lambda x: -x["points"])[:10]
    off_hand = sorted((x for x in usable if x["slot"] == 17), key=lambda x: -x["points"])[:10]
    groups.setdefault(("weapon",), []).extend(
        (x["price"] + y["price"], swap([16, 17], [x["norm"], y["norm"]]), ((x, [16]), (y, [17])))
        for x in one_hand for y in off_hand)
    cost, _, picks = best_package(list(groups.values()), budget)

    package_items, running, value, gone = [], dict(totals), base, set()
    for x, remove in picks:  # gain per item in the order of the set, so the lines add up to the exact total
        for s in remove:
            if s in eq and s not in gone:
                add_stats(running, eq[s], -1)
                gone.add(s)
        add_stats(running, x["norm"])
        new_value = set_value(running, scale)
        replaced = [equipped[s].get("name") for s in remove if s in equipped]
        old = next((equipped[s] for s in remove if s in equipped), None)
        package_items.append(dict(slot=x["slot"], slotName=SLOT_NAME[x["slot"]], replaces=replaced,
                                  compareTip=tooltip(old, SLOT_NAME[old["slot"]]) if old else None,
                                  **item_out(dict(x, remove=remove), new_value - value)))
        value = new_value
    package_items.sort(key=lambda p: -p["perGold"])  # best value first: stop anywhere down the list
    cum_cost = cum_gain = 0
    for p in package_items:
        cum_cost, cum_gain = cum_cost + p["price"], cum_gain + p["gain"]
        p.update(cumCost=cum_cost, cumGain=round(cum_gain, 1))
    package = dict(cost=cost, gain=round(value - base, 1), gainPct=round((value - base) / base * 100) if base else None,
                   items=package_items)

    review.sort(key=lambda r: r["price"])
    return dict(
        scanDate=(datetime.date(2020, 1, 1) + datetime.timedelta(last)).isoformat(),
        exportUpdated=info.get("updated"), exportCount=len(items), exportMissing=len(info.get("missing") or []),
        level=level, budget=budget, character=os.path.basename(os.path.dirname(os.path.dirname(files[0]))),
        weights=scale["text"], profile=scale["profile"], profileLabel=scale["label"], scale=scale["rows"],
        setValue=round(base, 1), sort=sort, package=package, slots=slots_out, review=review[:15])


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--wow", default=v.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--level", type=int, default=0, help="Stufe überschreiben (Standard: aus dem Export)")
    ap.add_argument("--vorschau", type=int, default=4, help="auch Items bis Stufe + N zeigen")
    ap.add_argument("--max-preis", type=float, default=0,
                    help="Budget in Gold, pro Item und für das Paket (Standard: eigenes Gold)")
    ap.add_argument("--top", type=int, default=3, help="Vorschläge pro Platz")
    ap.add_argument("--profil", choices=sorted(PROFILES), default="", help="Standard: leveln unter Stufe 60")
    ap.add_argument("--sort", choices=["gewinn", "preis"], default="gewinn", help="preis = Punkte pro Gold")
    ap.add_argument("--json", action="store_true", help="Ergebnis als JSON ausgeben (für die App)")
    a = ap.parse_args()

    try:
        r = compute(a.wow, a.char, a.level, a.vorschau, a.max_preis, a.top, profil=a.profil, sort=a.sort)
    except (FileNotFoundError, LookupError) as e:
        sys.exit(str(e))
    if a.json:
        import json
        print(json.dumps(r, ensure_ascii=False))
        return

    def line(c: dict) -> str:
        pct = f"{c['gainPct']:+4d} %" if c["gainPct"] is not None else "   neu"
        flag = " [NEU in Forever]" if c["isNew"] else ""
        soon = f" (ab Stufe {c['reqLevel']})" if c["soon"] else ""
        both = " [Zweihand]" if c["twoHand"] else ""
        return (f"{c['gain']:+6.1f} {pct}  {money(c['price']):>9}  {c['perGold']:6.1f}/g  {c['name']}{flag}{soon}{both}"
                f"  [{c['stats']}]  id {c['id']}")

    when = datetime.datetime.fromtimestamp(r["exportUpdated"] or 0)
    print(f"AH-Scan: {r['scanDate']} | Item-Export: {when:%Y-%m-%d %H:%M} | Stufe {r['level']} | "
          f"Budget {money(r['budget'])} | {r['exportCount']} Items, {r['exportMissing']} nicht geladen")
    print(f"Skala „{r['profileLabel']}“ (1 Punkt = 1 Schatten-Zaubermacht), Wert der Ausrüstung jetzt: {r['setValue']}")
    for row in r["scale"]:
        print(f"   {row['label']:24} {row['weight']:5.2f}  {row['note']}")
    print("Preise = Mindest-Sofortkauf beim Scan.\n")
    p = r["package"]
    if p["items"]:
        pct = f" ({p['gainPct']:+d} %)" if p["gainPct"] is not None else ""
        print(f"== Bestes Paket für {money(r['budget'])}: {p['gain']:+.1f} Punkte{pct} für {money(p['cost'])}")
        for c in p["items"]:
            print(f"   {c['slotName']:10} {line(c)}")
            print(f"   {'':10} bis hier {money(c['cumCost'])} für {c['cumGain']:+.1f} Punkte"
                  + (f", ersetzt {' + '.join(c['replaces'])}" if c["replaces"] else ""))
        print()
    for s in r["slots"]:
        cur = s["current"]
        print(f"== {s['slotName']}: jetzt {cur['name'] if cur else '-'} ({cur['stats'] if cur else ''})")
        for c in s["candidates"]:
            print(f"   {line(c)}")
            for t in c["effects"]:
                print(f"            {t}")
    if r["review"]:
        print("\n== Mit Spezialeffekt, bitte selbst ansehen")
        for x in r["review"]:
            print(f"   {x['slot']:10} {money(x['price']):>9}  {x['name']}: {' / '.join(x['effects'])[:160]}")


if __name__ == "__main__":
    main()
