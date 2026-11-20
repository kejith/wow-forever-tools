# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Checks for the combat log evaluation (combatlog.py) without game, network and engine:
uv run scripts/test_combatlog.py

The log is written here line by line in the game's format (version 22, with and without advanced logging),
with made-up units: a warlock, its pet, two training dummies and a bystander whose lines must not count. The
combat engine is a stub that answers with two spell ranks. Every expected number is worked out by hand in
the comments.
"""

from __future__ import annotations

import contextlib
import io
import os
import sys
import tempfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import combatlog  # noqa: E402
import i18n  # noqa: E402

ME = ("Player-1-00000001", "Tester-Realm", "0x511")
PET = ("Pet-0-1-1-1-417-0100000001", "Grimpaw", "0x1111")
DUMMY = ("Creature-0-1-1-1-100-0000000001", "Training Dummy", "0xa48")
DUMMY2 = ("Creature-0-1-1-1-100-0000000002", "Training Dummy", "0xa48")
OTHER = ("Player-1-00000002", "Bystander-Realm", "0x518")
NOBODY = ("0000000000000000", None, "0x80000000")
BOLT = (686, "Shadow Bolt", "0x20")
CORRUPTION = (172, "Corruption", "0x20")
LIFE_TAP = (1454, "Life Tap", "0x20")
LIFE_TAP_MANA = (31818, "Life Tap", "0x20")  # the game books the mana on a second spell of the same name
DRAIN = (689, "Drain Life", "0x20")
ARMOR = (706, "Demon Armor", "0x20")
TRANCE = (17941, "Shadow Trance", "0x20")
MARK = (1126, "Mark of the Wild", "0x8")

STUB_ENGINE = '''
import json, sys
data = json.load(sys.stdin)
assert data["metric"] == "dps" and data["character"]["classFile"] == "WARLOCK"
# The log's spell power (110), not the sheet's 100 for shadow; fire keeps its distance to it.
assert data["character"]["stats"]["spellDamage"] == {"shadow": 110, "fire": 90}, data["character"]["stats"]
print(json.dumps({"engine": "stub", "spellbookSource": "export", "fight": data["fight"],
                  "baseline": {"metric": "dps", "value": 50.0, "breakdown": []},
                  "spells": [{"spellId": 686, "name": "Shadow Bolt", "directMin": 80, "directMax": 100},
                             {"spellId": 172, "name": "Corruption", "periodic": 120, "ticks": 3}]}))
'''


class Log:
    """Writes log lines. `advanced` adds the 19 fields of advanced logging to the events that carry them."""

    def __init__(self, advanced: bool = True):
        self.advanced = advanced
        self.lines = [self.stamp(0) + "COMBAT_LOG_VERSION,22,ADVANCED_LOG_ENABLED,%d,BUILD_VERSION,9.9.9,PROJECT_ID,18"
                      % advanced]

    @staticmethod
    def stamp(t: float) -> str:
        # 10:00:00 plus t seconds; the fourth digit after the point is the time zone.
        s = 36000 + t
        return "11/20/2026 %02d:%02d:%06.3f1  " % (s // 3600, s // 60 % 60, s % 60)

    @staticmethod
    def unit(u) -> str:
        return "%s,%s,%s,0x80000000" % (u[0], '"%s"' % u[1] if u[1] else "nil", u[2])

    def block(self, who, hp=(700, 700), mana=(0, 0, 0), level=0, power=100) -> str:
        if not self.advanced:
            return ""
        return ",%s,0000000000000000,%d,%d,0,%d,0,0,0,0,0,%d,%d,%d,1.00,2.00,1,0.5,%d" % (
            who[0], hp[0], hp[1], power, mana[0], mana[1], mana[2], level)

    def add(self, t, event, src, dst, spell=None, tail="") -> None:
        line = self.stamp(t) + "%s,%s,%s" % (event, self.unit(src), self.unit(dst))
        if spell:
            line += ',%d,"%s",%s' % spell
        self.lines.append(line + tail)

    def start(self, t, spell, src=ME) -> None:
        self.add(t, "SPELL_CAST_START", src, NOBODY, spell)

    def cast(self, t, spell, dst, mana, cost, hp=700, src=ME) -> None:
        self.add(t, "SPELL_CAST_SUCCESS", src, dst, spell,
                 self.block(src, (hp, 700), (mana, 1000, cost), 39, power=110))

    def damage(self, t, spell, dst, amount, base, level, resisted=0, crit=False, periodic=False, src=ME) -> None:
        event = "SWING_DAMAGE" if spell is None else "SPELL_PERIODIC_DAMAGE" if periodic else "SPELL_DAMAGE"
        self.add(t, event, src, dst, spell, self.block(dst, level=level) + ",%d,%d,-1,32,%d,0,0,%s,nil,nil,ST" % (
            amount, base, resisted, "1" if crit else "nil"))

    def aura(self, t, event, spell, dst, src=ME, kind="DEBUFF") -> None:
        self.add(t, event, src, dst, spell, "," + kind)

    def write(self, wow: str, name: str = "WoWCombatLog-112026_100000.txt") -> str:
        os.makedirs(os.path.join(wow, "Logs"), exist_ok=True)
        path = os.path.join(wow, "Logs", name)
        with open(path, "w", encoding="utf-8", newline="") as f:
            f.write("\r\n".join(self.lines) + "\r\n")
        return path


def sample(advanced: bool = True) -> Log:
    """Two fights of a level 27 warlock.

    Fight 1 against a dummy of level 29, which takes 10 % more damage:
       98.0  starts a Shadow Bolt (the opener: the fight starts here)
      100.0  Shadow Bolt lands, a crit of 165 (100 before crit and the dummy's 10 %); mana 900 of 1000
      100.5  Corruption; ticks at 103.5 (44 of 40), 106.5 (44) and 109.5 (a crit of 88)
      104.0  starts a Shadow Bolt; 106.0 it is resisted in full
      107.0  Shadow Bolt: 80 land, 30 resisted (100 before)
      108.0  Life Tap: 120 mana
      108.5  the dummy hits back for 50
      109.0  the pet hits for 20
      110.0  Corruption ends, the dummy dies
    Buffs: Demon Armor from 50.0 on and never off; Shadow Trance from 105.0 to 108.0; the bystander's Mark of
    the Wild on the warlock from 199.0 on, and one on the bystander, which is not the warlock's business.
    The game marks fight 1 as the boss fight "Dummy Boss" (99.0 to 110.5).
    A bystander fights something else at 150 s.
    Fight 2 against a dummy of level 30: a Shadow Bolt started at 198.0 lands at 200.0 for 110 (100 before),
    and a Drain Life tick at 199.5 heals 50, of which 10 are too much.
    """
    log = Log(advanced)
    log.aura(50.0, "SPELL_AURA_APPLIED", ARMOR, ME, kind="BUFF")
    log.start(98.0, BOLT)
    log.lines.append(log.stamp(99.0) + 'ENCOUNTER_START,7,"Dummy Boss",1,5,1')
    log.cast(100.0, BOLT, DUMMY, 900, 100)
    log.damage(100.0, BOLT, DUMMY, 165, 100, 29, crit=True)
    log.cast(100.5, CORRUPTION, DUMMY, 800, 100)
    log.aura(100.5, "SPELL_AURA_APPLIED", CORRUPTION, DUMMY)
    log.damage(103.5, CORRUPTION, DUMMY, 44, 40, 29, periodic=True)
    log.start(104.0, BOLT)
    log.aura(105.0, "SPELL_AURA_APPLIED", TRANCE, ME, kind="BUFF")
    log.cast(106.0, BOLT, DUMMY, 700, 100)
    log.add(106.0, "SPELL_MISSED", ME, DUMMY, BOLT, ",RESIST,nil")
    log.damage(106.5, CORRUPTION, DUMMY, 44, 40, 29, periodic=True)
    log.cast(107.0, BOLT, DUMMY, 600, 100)
    log.damage(107.0, BOLT, DUMMY, 80, 100, 29, resisted=30)
    log.aura(108.0, "SPELL_AURA_REMOVED", TRANCE, ME, kind="BUFF")
    log.cast(108.0, LIFE_TAP, NOBODY, 500, 0, hp=420)
    log.add(108.0, "SPELL_ENERGIZE", ME, ME, LIFE_TAP_MANA, log.block(ME) + ",120,0,0,1000")
    log.damage(108.5, None, ME, 50, 60, 39, src=DUMMY)
    log.damage(109.0, None, DUMMY, 20, 25, 29, src=PET)
    log.damage(109.5, CORRUPTION, DUMMY, 88, 40, 29, crit=True, periodic=True)
    log.aura(110.0, "SPELL_AURA_REMOVED", CORRUPTION, DUMMY)
    log.add(110.0, "UNIT_DIED", NOBODY, DUMMY, None, ",0")
    log.add(110.0, "PARTY_KILL", ME, DUMMY, None, ",0")
    log.lines.append(log.stamp(110.5) + 'ENCOUNTER_END,7,"Dummy Boss",1,5,1,11500')
    log.add(120.0, "ENVIRONMENTAL_DAMAGE", NOBODY, ME, None, ",Falling" + log.block(ME) + ",30,30,0,1,0,0,0,nil,nil,nil")
    log.cast(150.0, BOLT, DUMMY2, 900, 100, src=OTHER)
    log.damage(150.0, BOLT, DUMMY2, 500, 500, 30, src=OTHER)
    log.start(198.0, BOLT)
    log.aura(199.0, "SPELL_AURA_APPLIED", MARK, ME, src=OTHER, kind="BUFF")
    log.aura(199.0, "SPELL_AURA_APPLIED", MARK, OTHER, src=OTHER, kind="BUFF")
    log.add(199.5, "SPELL_PERIODIC_HEAL", ME, ME, DRAIN, log.block(ME) + ",50,50,10,0,nil")
    log.cast(200.0, BOLT, DUMMY2, 1000, 100)
    log.damage(200.0, BOLT, DUMMY2, 110, 100, 30)
    return log


def write_char(wow: str, name: str = "Tester", class_file: str = "WARLOCK") -> None:
    char_dir = os.path.join(wow, "WTF", "Account", "TEST", "93", name + " - Realm", "SavedVariables")
    os.makedirs(char_dir)
    with open(os.path.join(char_dir, "WoWMCP.lua"), "w", encoding="utf-8") as f:
        f.write('WoWMCP_Char = { character = { name = "%s", class = "Hexenmeister", classFile = "%s", level = 27, '
                'stats = { powerMax = 1000, healthMax = 700, spirit = 50, spellHit = 2, '
                'spellCrit = { shadow = 7.5 }, spellDamage = { shadow = 100, fire = 80 } } }, '
                'spellbook = { updated = 1, count = 2, spells = { '
                '[686] = { name = "Schattenblitz", rank = "Rang 1" }, [172] = { name = "Verderbnis", rank = "Rang 1" } } }, '
                'talents = { class = {} }, client = { version = "9.9.9", build = 9, locale = "deDE" }, '
                'equipment = {} }\n' % (name, class_file))


@contextlib.contextmanager
def stub_engine(tmp: str):
    """The engine is a Python stub; the table download is stubbed out."""
    import icons
    stub = os.path.join(tmp, "engine_stub.py")
    with open(stub, "w", encoding="utf-8") as f:
        f.write(STUB_ENGINE)
    real_table, real_env = icons.table, os.environ.get("SIMFOREVER_BIN")
    icons.table = lambda *a, **k: []
    os.environ["SIMFOREVER_BIN"] = stub
    try:
        yield
    finally:
        icons.table = real_table
        os.environ.pop("SIMFOREVER_BIN", None)
        if real_env:
            os.environ["SIMFOREVER_BIN"] = real_env


def spell(r: dict, spell_id: int, pet: str = "") -> dict:
    return next(x for x in r["spells"] if x["spellId"] == spell_id and x["pet"] == pet)


def near(what: str, got, want, eps: float = 0.051) -> None:
    assert got is not None and abs(got - want) <= eps, f"{what}: {got}, want {want}"


def test_fights() -> None:
    i18n.set_language("en")
    with tempfile.TemporaryDirectory() as tmp:
        sample().write(tmp)
        write_char(tmp)
        with stub_engine(tmp):
            r = combatlog.compute(tmp, "Tester")
    assert (r["name"], r["level"], r["version"], r["advanced"], r["build"]) == ("Tester", 27, 22, True, "9.9.9"), r
    assert r["started"] == "10:00:00" and r["others"] == [] and r["model"] and not r["modelError"], r
    assert r["spellPower"] == {"low": 110, "high": 110} and r["sheetSpellDamage"] == {"shadow": 100, "fire": 80}, r
    assert r["modelSpellPower"] == 110, r  # the stub checks that the engine was asked at this spell power

    # Fight 1 runs from the opener at 98.0 to the last tick at 109.5; the bystander's fight is not one.
    assert len(r["fights"]) == 2, r["fights"]
    one, two = r["fights"]
    assert (one["n"], one["start"], one["seconds"], one["enemies"]) == (1, "10:01:38", 11.5, 1), one
    assert one["targets"] == ["Training Dummy"] and one["levelDelta"] == 2 and one["kills"] == 1, one
    assert (one["encounter"], r["fights"][1]["encounter"]) == ("Dummy Boss", ""), r["fights"]
    # 165 + 80 from the bolts, 44 + 44 + 88 from Corruption, 20 from the pet.
    assert one["damage"] == 441 and one["taken"] == 50, one
    near("dps of fight 1", one["dps"], 441 / 11.5)
    assert (one["manaStartPct"], one["manaEndPct"], one["manaLowPct"], one["healthLowPct"]) == (90, 50, 50, 60), one
    assert [(c["t"], c["label"], c["target"], c["manaPct"]) for c in one["casts"]] == [
        (2.0, "Shadow Bolt (Rang 1)", "Training Dummy", 90), (2.5, "Corruption (Rang 1)", "Training Dummy", 80),
        (8.0, "Shadow Bolt (Rang 1)", "Training Dummy", 70), (9.0, "Shadow Bolt (Rang 1)", "Training Dummy", 60),
        (10.0, "Life Tap", "", 50)], one["casts"]
    assert [(s["label"], s["casts"], s["damage"]) for s in one["spells"][:2]] == [
        ("Shadow Bolt (Rang 1)", 3, 245), ("Corruption (Rang 1)", 1, 176)], one["spells"]
    assert (two["start"], two["seconds"], two["damage"], two["healing"], two["levelDelta"], two["kills"]) == (
        "10:03:18", 2.0, 110, 40, 3, 0), two
    total = r["total"]
    assert (total["fights"], total["seconds"], total["damage"], total["kills"]) == (2, 13.5, 551, 1), total
    near("dps over all fights", total["dps"], 551 / 13.5)

    # Shadow Bolt: four casts, three landed, one crit, one resisted in full.
    bolt = spell(r, 686)
    assert (bolt["casts"], bolt["hits"], bolt["crits"], bolt["misses"], bolt["missed"]) == (4, 3, 1, {"RESIST": 1}, 1), bolt
    near("crit share", bolt["critPct"], 33.3)
    near("missed", bolt["missPct"], 25.0)
    near("average hit before crit and mitigation", bolt["avgHit"], 100.0)
    # Without crit 110 + 110 of 100 + 100: the dummies add 10 %. The crit is 165 of 100: 1.5 on top of that.
    near("what the enemy adds", bolt["takenPct"], 10.0)
    near("crit multiplier", bolt["critMult"], 1.5, 0.006)
    # 30 resisted of 165 + 80 + 110 + 30.
    near("resisted", bolt["resistedPct"], 30 / 385 * 100)
    near("casts per minute", bolt["castsPerMin"], 4 / 13.5 * 60)
    near("share of the damage", bolt["sharePct"], 355 / 551 * 100)
    assert bolt["damage"] == 355 and bolt["sheetCritPct"] == 7.5, bolt
    # The model says 80 to 100: its 90 are 11.1 % below the log's 100.
    assert bolt["model"] == {"hit": 90.0, "hitDiffPct": 11.1}, bolt["model"]

    # Corruption: three ticks 3 s apart, one of them a crit of twice the amount; up from 100.5 to the end of
    # the fight at 109.5, which is 9 of the 13.5 s of fighting.
    dot = spell(r, 172)
    assert (dot["casts"], dot["hits"], dot["ticks"], dot["tickCrits"], dot["damage"]) == (1, 0, 3, 1, 176), dot
    near("tick crits", dot["tickCritPct"], 33.3)
    near("average tick", dot["avgTick"], 40.0)
    near("time between ticks", dot["tickSec"], 3.0, 0.001)
    near("tick crit multiplier", dot["critMult"], 2.0, 0.006)
    near("uptime", dot["uptimePct"], 9 / 13.5 * 100)
    assert dot["model"] == {"tick": 40.0, "tickDiffPct": 0.0} and dot["avgHit"] is None, dot

    # Life Tap's mana is booked on the spell that was cast; the row of the second spell is gone.
    assert not [x for x in r["spells"] if x["spellId"] == 31818], r["spells"]
    assert [x["mana"] for x in one["spells"] if x["spellId"] == 1454] == [120], one["spells"]
    tap = spell(r, 1454)
    assert (tap["casts"], tap["mana"], tap["energizes"], tap["damage"], tap["label"]) == (1, 120, 1, 0, "Life Tap"), tap
    drain = spell(r, 689)
    assert (drain["healing"], drain["overheal"], drain["heals"]) == (40, 10, 1), drain
    pet = spell(r, 0, "Grimpaw")
    assert (pet["name"], pet["hits"], pet["damage"], pet.get("model")) == ("Melee", 1, 20, None), pet

    # Buffs, of the 13.5 s of fighting: Demon Armor all of it, Shadow Trance 3 s, the bystander's mark 1 s.
    assert r["buffs"] == [
        {"spellId": 706, "name": "Demon Armor", "own": True, "uptimePct": 100.0},
        {"spellId": 17941, "name": "Shadow Trance", "own": True, "uptimePct": 22.2},
        {"spellId": 1126, "name": "Mark of the Wild", "own": False, "uptimePct": 7.4}], r["buffs"]

    # Casts on enemies by their level: four on the dummy two levels above (one resisted), one on the other.
    # The model: 6 % and 17 % miss less the sheet's 2 % hit.
    assert r["hit"] == [
        {"levelDelta": 2, "casts": 4, "misses": 1, "missPct": 25.0, "modelMissPct": 4},
        {"levelDelta": 3, "casts": 1, "misses": 0, "missPct": 0.0, "modelMissPct": 15}], r["hit"]


def test_gap_and_choice() -> None:
    i18n.set_language("en")
    with tempfile.TemporaryDirectory() as tmp:
        log = sample()
        # A second character on the same account wrote into the same log later; the one with more lines wins.
        alt = ("Player-1-00000003", "Alter-Realm", "0x511")
        log.cast(400.0, BOLT, DUMMY2, 500, 100, src=alt)
        log.damage(400.0, BOLT, DUMMY2, 70, 70, 30, src=alt)
        log.write(tmp)
        r = combatlog.compute(tmp, model=False)
        assert r["name"] == "Tester" and r["others"] == ["Alter"] and not r["model"], r
        assert r["level"] is None and r["hit"] == [] and r["fights"][0]["levelDelta"] is None, r  # no snapshot
        assert spell(r, 686)["label"] == "Shadow Bolt" and "model" not in spell(r, 686), r
        alt_r = combatlog.compute(tmp, "alt", model=False)
        assert alt_r["name"] == "Alter" and alt_r["total"]["damage"] == 70, alt_r
        # With 100 s between two events as one fight, everything is one fight.
        wide = combatlog.compute(tmp, "Tester", gap=100, model=False)
        assert len(wide["fights"]) == 1 and wide["fights"][0]["enemies"] == 2, wide["fights"]
        assert wide["fights"][0]["damage"] == 551 and wide["fights"][0]["seconds"] == 102.0, wide["fights"]
        # The newest log is read unless a file is named.
        older = Log()
        older.cast(10.0, BOLT, DUMMY, 900, 100)
        older.damage(10.0, BOLT, DUMMY, 7, 7, 29)
        path = older.write(tmp, "WoWCombatLog-111926_100000.txt")
        os.utime(path, (1, 1))
        assert combatlog.compute(tmp, "Tester", model=False)["file"] == "WoWCombatLog-112026_100000.txt"
        assert combatlog.compute(tmp, "Tester", file=path, model=False)["total"]["damage"] == 7

        for call, error in ((lambda: combatlog.compute(tmp, "Nobody", model=False), LookupError),
                            (lambda: combatlog.compute(os.path.join(tmp, "empty"), model=False), FileNotFoundError)):
            try:
                call()
            except error:
                pass
            else:
                raise AssertionError(f"no {error.__name__}")

        # A class the engine does not model: the log is evaluated all the same, without the model's numbers.
        write_char(tmp, class_file="WARRIOR")
        r = combatlog.compute(tmp, "Tester")
        assert not r["model"] and r["modelError"] and r["level"] == 27 and r["hit"][0]["levelDelta"] == 2, r


def test_without_advanced_logging() -> None:
    i18n.set_language("en")
    with tempfile.TemporaryDirectory() as tmp:
        sample(advanced=False).write(tmp)
        write_char(tmp)
        r = combatlog.compute(tmp, "Tester", model=False)
    assert not r["advanced"] and r["spellPower"] is None and r["hit"] == [], r
    one = r["fights"][0]
    assert (one["damage"], one["manaStartPct"], one["healthLowPct"], one["levelDelta"]) == (441, None, None, None), one
    assert [c["manaPct"] for c in one["casts"]] == [None] * 5, one["casts"]
    near("average hit", spell(r, 686)["avgHit"], 100.0)


def test_cli_both_languages() -> None:
    for lang, words in (("en", ("2 fights", "Shadow Bolt (Rang 1)", "Melee (Grimpaw)", "120 mana, 1x (120 each)",
                                "the model expects 4%", "Fights (--fight N",
                                "Buffs on you, of the time fighting: Demon Armor 100%, Shadow Trance 22%", "[Dummy Boss]")),
                        ("de", ("2 Kämpfe", "Shadow Bolt (Rang 1)", "Nahkampf (Grimpaw)", "120 Mana, 1x (je 120)",
                                "das Modell erwartet 4 %", "Kämpfe (--fight N",
                                "Buffs auf dir, Anteil der Kampfzeit: Demon Armor 100%, Shadow Trance 22%", "[Dummy Boss]"))):
        with tempfile.TemporaryDirectory() as tmp:
            sample().write(tmp)
            write_char(tmp)
            out = io.StringIO()
            argv = sys.argv
            sys.argv = ["combatlog.py", "--wow", tmp, "--char", "Tester", "--lang", lang, "--no-model"]
            real = sys.stdout
            try:
                sys.stdout = Console(out)
                combatlog.main()
                sys.argv += ["--fight", "1"]
                combatlog.main()
            finally:
                sys.stdout, sys.argv = real, argv
            text = out.getvalue()
            for w in words:
                assert w in text, f"{lang}: {w!r} not in:\n{text}"
            assert "Bystander" not in text, text


class Console(io.TextIOBase):
    """A stdout that accepts reconfigure(), which the script calls for the Windows console."""

    def __init__(self, to: io.StringIO):
        self.to = to

    def write(self, s: str) -> int:
        return self.to.write(s)

    def reconfigure(self, **kw) -> None:
        pass


def main() -> None:
    tests = [test_fights, test_gap_and_choice, test_without_advanced_logging, test_cli_both_languages]
    for t in tests:
        t()
        print(f"ok  {t.__name__}")
    print(f"{len(tests)} tests passed")


if __name__ == "__main__":
    main()
