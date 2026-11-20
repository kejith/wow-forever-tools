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
import shutil
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import icons  # noqa: E402
from i18n import tr  # noqa: E402

# Classes the engine models so far (sim/run/run.go must agree).
SUPPORTED = {"WARLOCK"}
# Tables the engine reads from the shared cache (sim/catalog/catalog.go).
TABLES = ["SpellClassOptions", "SpellMisc", "SpellEffect", "SpellCastTimes", "SpellDuration",
          "SpellPower", "SpellLevels", "SpellCooldowns", "SkillLineAbility", "SpellName"]
WANDS_SKILL_ID = 228
# Canonical scale keys (norm_stats) -> the engine's delta keys. Armor is
# dropped: the damage metrics cannot see it.
KEY_MAP = {"sp": "sp", "stamina": "sta", "intellect": "int", "spirit": "spi",
           "mp5": "mp5", "hp5": "hp5", "wand_dps": "wandDps", "hit": "hit", "crit": "crit"}


def supported(class_file: str | None) -> bool:
    return (class_file or "") in SUPPORTED


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
    """Engine delta from normalised stat dicts (norm_stats keys)."""
    d: dict = {}
    for sign, group in ((1, adds), (-1, removes)):
        for n in group:
            for k, v in (n or {}).items():
                key = KEY_MAP.get(k)
                if key and isinstance(v, (int, float)):
                    d[key] = d.get(key, 0) + sign * v
    return {k: v for k, v in d.items() if v}


class Engine:
    """One started engine: the bundle without evaluations, run per batch."""

    def __init__(self, binary: str, bundle: dict):
        self.binary = binary
        self.bundle = bundle
        self.baseline: dict = {}
        self.version = ""
        self.spellbook_source = ""

    def batch(self, evals: list[tuple[str, dict]]) -> dict[str, float]:
        """Evaluates deltas; returns the metric gain per key."""
        payload = dict(self.bundle, evaluations=[{"key": k, "delta": d} for k, d in evals])
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
        return {k: (v.get("gain") or 0.0) for k, v in (out.get("results") or {}).items()}


def start(c: dict, gear_mp5: float, gear_hp5: float, fight: dict, metric: str,
          life_tap: dict, binary: str | None = None) -> Engine:
    """Builds the bundle from a WoWMCP snapshot and returns a ready Engine.

    `life_tap` is {"healthPerCast", "manaPerCast"}, parsed from the spell text
    by the caller (upgrades.py owns the tooltip languages)."""
    binary = binary or find_binary()
    if not binary:
        raise LookupError(tr("The combat engine (simforever) was not found."))
    ch = c.get("character") or {}
    st = ch.get("stats") or {}
    client = c.get("client") or {}
    if not client.get("version") or not client.get("build"):
        raise LookupError(tr("The snapshot does not name the client build; /wowmcp sync and /reload first."))
    build = f"{client['version']}.{client['build']}"
    locale = client.get("locale") or "deDE"
    for t in TABLES:
        icons.table(t, build, locale)

    intellect = st.get("intellect") or 0
    crit_per_int = 0.0
    if intellect and st.get("critFromIntellect") is not None:
        crit_per_int = st["critFromIntellect"] / intellect
    wand_skill = 0
    for s in ((c.get("skills") or {}).get("skills") or []):
        if isinstance(s, dict) and s.get("skillID") == WANDS_SKILL_ID:
            wand_skill = (s.get("rank") or 0) + (s.get("modifier") or 0)
    wand = next((e for e in (c.get("equipment") or []) if e.get("slot") == 18), None) or {}
    wand_dps = ((wand.get("stats") or {}).get("DAMAGE_PER_SECOND")) or 0
    talents = {str(t.get("spellID")): t.get("rank") or 0
               for tree in ((c.get("talents") or {}).get("class") or [])
               for t in (tree.get("talents") or []) if isinstance(t, dict) and t.get("spellID")}
    spellbook = sorted(int(k) for k in ((c.get("spellbook") or {}).get("spells") or {}))

    bundle = {
        "cacheDir": icons.CACHE, "build": build, "locale": locale,
        "character": {
            "classFile": ch.get("classFile") or "", "level": ch.get("level") or 60,
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
            },
            "talents": talents, "spellbook": spellbook,
        },
        "fight": fight, "metric": metric,
    }
    return Engine(binary, bundle)
