"""Bridge to the combat engine (sim/, Go binary `simforever`).

The engine evaluates a character's rotation deterministically and rates stat
deltas as metric changes (DPS for now). This module builds the engine's input
bundle from a WoWMCP snapshot, makes sure the DB2 tables the engine reads are
in the cache, and runs the binary: one JSON bundle on stdin, one JSON answer
on stdout. The engine is optional — find_binary() returns None when it is not
installed, and upgrades.py then keeps its weight scale.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import icons  # noqa: E402
from i18n import tr  # noqa: E402

# Classes the engine models and their metrics (sim/run/run.go must agree);
# the first metric is the default for a damage spec.
METRICS = {"WARLOCK": ["dps"], "PRIEST": ["hps", "ttoom", "dps"]}
SUPPORTED = set(METRICS)
# Talent tree order per class as the client shows it (the addon exports the
# trees in that order); used to name the spec from where the points sit.
TREES = {"WARLOCK": ["affliction", "demonology", "destruction"],
         "PRIEST": ["discipline", "holy", "shadow"]}
# Tables the engine reads from the shared cache (sim/catalog/catalog.go). The three trait tables hold the
# exact value of every talent rank; without them the engine falls back to the single value in SpellEffect.
TABLES = ["SpellClassOptions", "SpellMisc", "SpellEffect", "SpellCastTimes", "SpellDuration",
          "SpellPower", "SpellLevels", "SpellCooldowns", "SkillLineAbility", "SpellName",
          "TraitDefinition", "TraitDefinitionEffectPoints", "CurvePoint"]
REFERENCE_LOCALE = "enUS"  # sim/catalog.ReferenceLocale
WANDS_SKILL_ID = 228
# ITEM_MOD stats (stat_key form) -> the engine's delta keys. Armor and
# resistances are dropped: the modeled metrics cannot see them yet.
ITEM_KEYS = {"spell_power": "sp", "spell_damage_done": "spDamage", "spell_healing_done": "healing",
             "holy_damage_done": "spHoly", "fire_damage_done": "spFire", "nature_damage_done": "spNature",
             "frost_damage_done": "spFrost", "shadow_damage_done": "spShadow", "arcane_damage_done": "spArcane",
             "stamina": "sta", "intellect": "int", "spirit": "spi",
             "mana_regeneration": "mp5", "health_regen": "hp5"}


def supported(class_file: str | None) -> bool:
    return (class_file or "") in SUPPORTED


def spec_of(c: dict) -> str:
    """Names the spec from where the most talent points sit (the export lists
    the trees in the client's tab order)."""
    class_file = (c.get("character") or {}).get("classFile") or ""
    names = TREES.get(class_file)
    trees = (c.get("talents") or {}).get("class") or []
    if not names or not trees:
        return ""
    spent = [t.get("spent") or 0 for t in trees]
    if not any(spent):
        return ""
    best = max(range(len(spent)), key=lambda i: spent[i])
    return names[best] if best < len(names) else ""


def default_metric(class_file: str, spec: str) -> str:
    if class_file == "PRIEST" and spec != "shadow":
        return "hps"
    return "dps"


def norm_item(rec: dict | None, ranged: bool = False, ratings: dict | None = None,
              pct_line: dict | None = None) -> dict:
    """An item's stats as engine delta keys. Unlike the weight scale's
    norm_stats this keeps healing, per-school damage and the split between
    "damage done" and "damage and healing done". Hit and crit come as percent
    from the tooltip (pct_line = upgrades.PCT_LINE), else rating / ratings."""
    rec = rec or {}
    n: dict = {}
    for key, val in (rec.get("stats") or {}).items():
        if not isinstance(val, (int, float)):
            continue
        k = key.removeprefix("ITEM_MOD_").removesuffix("_SHORT").lower()
        if k in ITEM_KEYS:
            n[ITEM_KEYS[k]] = n.get(ITEM_KEYS[k], 0) + val
        elif k == "damage_per_second" and ranged:
            n["wandDps"] = val
        elif k in ("hit_rating", "crit_rating") and ratings and pct_line:
            kind = k.removesuffix("_rating")
            pct = sum(float(m.group(1).replace(",", ".")) for t in rec.get("tooltip") or []
                      for m in [pct_line[kind].search(t)] if m)
            n[kind] = pct or val / ratings[kind]
    return {k: v for k, v in n.items() if v}


def find_binary() -> str | None:
    """The engine binary: SIMFOREVER_BIN, next to the repo (sim/), next to the
    packaged scripts (../bin, where the app also keeps uv), or in the PATH."""
    exe = "simforever.exe" if os.name == "nt" else "simforever"
    env = os.environ.get("SIMFOREVER_BIN")
    if env:
        return env if os.path.isfile(env) else None
    here = os.path.dirname(os.path.abspath(__file__))
    for p in (os.path.join(here, "..", "sim", exe), os.path.join(here, "..", "bin", exe)):
        if os.path.isfile(p):
            return p
    return shutil.which("simforever")


def delta(adds: list[dict], removes: list[dict] = ()) -> dict:
    """Engine delta from norm_item dicts (already in engine keys)."""
    d: dict = {}
    for sign, group in ((1, adds), (-1, removes)):
        for n in group:
            for k, v in (n or {}).items():
                if isinstance(v, (int, float)):
                    d[k] = d.get(k, 0) + sign * v
    return {k: v for k, v in d.items() if v}


NUMBER = re.compile(r"\d+(?:[.,]\d+)?")


GCD_SEC = 1.5  # the engine's global cooldown: no cast occupies less time


def tooltip_check(spells: list, book: dict, coverage: dict | None = None) -> dict | None:
    """Lays the model's numbers next to the game's own spell texts.

    The spellbook export has, per learned rank, the description with the numbers resolved for this
    character: spell power, talents and the loss of an outgrown rank are in them. A rank agrees if each of
    the model's amounts (lowest and highest hit, all ticks together or one tick, the shield) is a number of
    that text, give or take the rounding. Language does not matter: only the numbers are read. With a
    coverage report only the spells the model casts or tried are checked (it has no use for the others).

    Since addon 1.12 the export also has each rank's mana cost and cast time as the game computes them
    (talents and haste included); a rank agrees only if those are the model's too. An instant spell has no
    cast time there, and a cast shorter than the global cooldown takes the global cooldown in the model.

    Returns {"checked", "matched", "costs" (the export had costs to compare), "off": [{"spellId", "name",
    "rank", "kind" ("amount", "cost" or "cast"), "model": [...], "text" (the game's text) or "game" (the
    game's number)}]}, or None without an exported spellbook or without numbers from the engine."""
    if not spells or not book:
        return None
    bases = None
    if coverage is not None:
        base_of = {n.get("spellId"): n.get("baseId") for n in spells}
        bases = {base_of.get(e.get("spellId")) for e in coverage.get("spells") or []
                 if e.get("status") in ("used", "idle")}
    checked, matched, off, costs = 0, 0, [], False
    for n in spells:
        entry = book.get(n.get("spellId")) or book.get(str(n.get("spellId"))) or {}
        text = entry.get("text") or ""
        if not text or (bases is not None and n.get("baseId") not in bases):
            continue
        seen = [float(x.replace(",", ".")) for x in NUMBER.findall(text)]
        amounts = [[n.get(k)] for k in ("directMin", "directMax", "absorb") if n.get(k)]
        if n.get("periodic"):
            amounts.append([n["periodic"], n["periodic"] / n["ticks"]] if n.get("ticks") else [n["periodic"]])
        # Another power than mana (health, a pet's energy) is not the model's mana.
        cost = None if entry.get("costType") else entry.get("cost")
        cast = (entry.get("castTime") or 0) / 1000
        if not amounts and cost is None and not cast:
            continue
        checked += 1
        before = len(off)
        who = {"spellId": n.get("spellId"), "name": entry.get("name") or "", "rank": entry.get("rank") or ""}
        # The client rounds the ends of the base range and then the sum: up to a point and a bit.
        if not all(any(abs(x - v) <= 1 + 0.002 * v for v in forms for x in seen) for forms in amounts):
            off.append(dict(who, kind="amount", model=[round(forms[0]) for forms in amounts], text=text))
        if cost is not None:
            costs = True
            if abs(cost - (n.get("mana") or 0)) > 1:
                off.append(dict(who, kind="cost", model=[round(n.get("mana") or 0)], game=cost))
        if cast and abs(max(cast, GCD_SEC) - (n.get("castSec") or 0)) > 0.02:
            off.append(dict(who, kind="cast", model=[round(n.get("castSec") or 0, 2)], game=round(cast, 2)))
        matched += len(off) == before
    return {"checked": checked, "matched": matched, "costs": costs, "off": off} if checked else None


class Engine:
    """One started engine: the bundle without evaluations, run per batch."""

    def __init__(self, binary: str, bundle: dict):
        self.binary = binary
        self.bundle = bundle
        self.baseline: dict = {}
        self.version = ""
        self.spellbook_source = ""
        self.downrank: dict | None = None
        self.heal_options: list = []
        self.coverage: dict = {}  # what the model did with every known spell and talent
        self.spells: list = []  # per known spell rank what one cast does, as a tooltip shows it
        self.fight: dict = dict(bundle.get("fight") or {})  # as evaluated: the engine resolves automatic values

    def batch(self, evals: list[tuple[str, dict]], reoptimize: bool = False) -> dict[str, float]:
        """Evaluates deltas; returns the metric gain per key. With `reoptimize` the engine picks the best
        rotation again for each delta instead of keeping the baseline's (for big deltas: consumables)."""
        out = self._run(dict(self.bundle, evaluations=[{"key": k, "delta": d} for k, d in evals],
                             reoptimize=reoptimize))
        return {k: (v.get("gain") or 0.0) for k, v in (out.get("results") or {}).items()}

    def situations(self) -> list:
        """The rotation guide (sim/run/situations.go): the best rotation in several kinds of fights for a
        damage metric, the cheapest spells per level of demand for a healer."""
        return self._run(dict(self.bundle, evaluations=[], situations=True)).get("situations") or []

    def heal_table(self) -> tuple[dict, list]:
        """Every known heal and shield rank with what one cast gives and costs (sim/run/healtable.go), and the
        rotation guide, which says when to cast which of them. One run."""
        out = self._run(dict(self.bundle, evaluations=[], healTable=True, situations=True))
        return out.get("healTable") or {}, out.get("situations") or []

    def _run(self, payload: dict) -> dict:
        cmd = [sys.executable, self.binary] if self.binary.endswith(".py") else [self.binary]  # .py: test stub
        try:
            proc = subprocess.run(cmd, input=json.dumps(payload), capture_output=True,
                                  text=True, encoding="utf-8", errors="replace", timeout=120)
        except (OSError, subprocess.TimeoutExpired) as e:
            raise LookupError(tr("The combat engine failed: {err}", err=str(e)[:200]))
        if proc.returncode != 0:
            raise LookupError(tr("The combat engine failed: {err}", err=(proc.stderr or "").strip()[:200]))
        try:
            out = json.loads(proc.stdout)
        except json.JSONDecodeError:
            raise LookupError(tr("The combat engine failed: {err}", err=proc.stdout.strip()[:200]))
        if out.get("error"):
            raise LookupError(tr("The combat engine reports: {err}", err=out["error"]))
        self.baseline = out.get("baseline") or {}
        self.version = out.get("engine") or ""
        self.spellbook_source = out.get("spellbookSource") or ""
        self.downrank = out.get("downrank")
        self.heal_options = out.get("healOptions") or []
        self.coverage = out.get("coverage") or {}
        self.spells = out.get("spells") or self.spells
        self.fight = out.get("fight") or self.fight
        return out


def sheet_numbers(st: dict) -> dict:
    """The character sheet's own haste and regeneration (addon 1.12) as the engine's stats: spell haste in
    percent, mana per second from spirit outside the five-second rule, and everything while casting. The
    engine takes the two regeneration numbers instead of its class formula and of the gear's mp5; like every
    other number of the sheet they include the buffs of the moment. An older snapshot has none of them."""
    out = {"hastePct": st.get("spellHaste") or 0}
    regen = st.get("manaRegen") or {}
    if regen.get("spirit"):
        out["spiritRegen"] = regen["spirit"]
        if regen.get("casting") is not None:
            out["castingRegen"] = regen["casting"]
    return out


def start(c: dict, gear_mp5: float, gear_hp5: float, fight: dict, metric: str,
          life_tap: dict, binary: str | None = None, method: str = "", iterations: int = 0,
          buffs: dict | None = None) -> Engine:
    """Builds the bundle from a WoWMCP snapshot and returns a ready Engine.

    `life_tap` is {"healthPerCast", "manaPerCast"}, parsed from the spell text
    by the caller (upgrades.py owns the tooltip languages). `fight` is
    {"seconds", "targetDelta", "killsPerMinute"}; negative kills mean automatic
    (the engine's answer has the fight as evaluated, Engine.fight). `buffs` is a stat delta in the engine's
    keys that every number is to include: the consumables of consumables.py."""
    binary = binary or find_binary()
    if not binary:
        raise LookupError(tr("The combat engine (simforever) was not found."))
    ch = c.get("character") or {}
    st = ch.get("stats") or {}
    spec = spec_of(c)
    if not metric:
        metric = default_metric(ch.get("classFile") or "", spec)
    client = c.get("client") or {}
    if not client.get("version") or not client.get("build"):
        raise LookupError(tr("The snapshot does not name the client build; /wowmcp sync and /reload first."))
    build = f"{client['version']}.{client['build']}"
    locale = client.get("locale") or "deDE"
    for t in TABLES:
        icons.table(t, build, locale)
    # The rank chains are grouped by the English spell names: a translation may name the ranks of one spell
    # differently (deDE: Smite rank 1 "Heilige Pein", rank 2 "Goettliche Pein"). See sim/catalog.
    if locale != REFERENCE_LOCALE:
        icons.table("SpellName", build, REFERENCE_LOCALE)

    intellect = st.get("intellect") or 0
    crit_per_int = 0.0
    if intellect and st.get("critFromIntellect") is not None:
        crit_per_int = st["critFromIntellect"] / intellect
    wand_skill = 0
    for s in ((c.get("skills") or {}).get("skills") or []):
        if isinstance(s, dict) and s.get("skillID") == WANDS_SKILL_ID:
            wand_skill = (s.get("rank") or 0) + (s.get("modifier") or 0)
    wand = next((e for e in (c.get("equipment") or []) if e.get("slot") == 18), None) or {}
    wand_dps = norm_item(wand, ranged=True).get("wandDps", 0)
    talents = {str(t.get("spellID")): t.get("rank") or 0
               for tree in ((c.get("talents") or {}).get("class") or [])
               for t in (tree.get("talents") or []) if isinstance(t, dict) and t.get("spellID")}
    spellbook = sorted(int(k) for k in ((c.get("spellbook") or {}).get("spells") or {}))

    bundle = {
        "cacheDir": icons.CACHE, "build": build, "locale": locale,
        "character": {
            "classFile": ch.get("classFile") or "", "level": ch.get("level") or 60,
            "specOverride": spec,
            "stats": {
                "spellDamage": st.get("spellDamage") or {},
                "spellCrit": st.get("spellCrit") or {},
                "spellHit": st.get("spellHit") or 0,
                "healing": st.get("healing") or 0,
                "intellect": intellect, "spirit": st.get("spirit") or 0,
                "mp5": gear_mp5, "hp5": gear_hp5,
                "healthMax": st.get("healthMax") or 0, "manaMax": st.get("powerMax") or 0,
                "critPerInt": crit_per_int,
                "manaPerInt": st.get("manaPerIntellect") or 15,
                "hpPerSta": st.get("hpPerStamina") or 10,
                "rangedCrit": st.get("rangedCrit") or 0,
                "wand": {"dps": wand_dps, "skill": wand_skill},
                "lifeTap": life_tap,
                **sheet_numbers(st),
            },
            "talents": talents, "spellbook": spellbook,
        },
        "fight": fight, "metric": metric, "method": method, "iterations": iterations,
    }
    if buffs:
        bundle["buffs"] = buffs
    return Engine(binary, bundle)
