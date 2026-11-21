# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Builds the test WoW folder scripts/fixtures/forever from a real one:

    uv run scripts/fixtures/make_forever.py [--from <_forever_ folder>] [--out scripts/fixtures/forever]

The fixture is what the parity check (sim/cmd/parity) runs both implementations of the bridge against, so it
has to reach every command: two characters with WoWMCP snapshots (a warlock and a priest), the account's
WoWMCP.lua (item export, vendor prices, payouts), Auctionator's price database, GoldPerHour (the test data of
scripts/fixtures/goldperhour), a combat log, the client language and an installed WoWMCP .toc.

The game data stays as it is (stats, spells, talents, items, prices); everything that names a person does not:

- characters get made-up names (first name and surname), the account folder is FIXTURE, the realm of the price
  database is "Fixture Realm"; every string of every file is searched for the real names and they are replaced;
- the combat log is the made-up one of scripts/test_combatlog.py, never a real log (real logs name every player
  who was near);
- the price database and the item export are trimmed to a sample (all non-equipment items, a quarter of the
  equipment around the characters' levels, everything the characters wear or carry), and the history to the
  last DAYS scan days;
- at the end every output file is checked for the real names and the account folder; the script fails if one
  is left.

Files are written in the format the game writes (`["key"] = value,`, one entry per line), because some readers
work on the text (writs.load_char).
"""

from __future__ import annotations

import argparse
import glob
import os
import shutil
import struct
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPTS = os.path.dirname(HERE)
sys.path.insert(0, SCRIPTS)
import test_combatlog  # noqa: E402
import writs  # noqa: E402
from upgrades import load_sv  # noqa: E402

ACCOUNT = "FIXTURE"
REALM = "Fixture Realm"
# Made-up names per class, in the order the characters are found (most recently written first).
NAMES = {"WARLOCK": [("Brakk", "Düsterfels")], "PRIEST": [("Ilse", "Grabeshauch")]}
DAYS = 9  # scan days kept: the profit history uses the last 8 full scan days
EQUIP_SHARE = 4  # one equipment item in this many is kept (by item ID)
LEVEL_BELOW, LEVEL_ABOVE = 8, 5  # the equipment kept around each character's level


# ---------- CBOR that keeps the key types (writs.cbor turns every key into a string) ----------
def cbor_decode(b: bytes, i: int = 0):
    ib = b[i]
    mt, ai = ib >> 5, ib & 31
    i += 1
    if mt == 7:
        if ai in (20, 21, 22):
            return {20: False, 21: True, 22: None}[ai], i
        fmt, n = {25: (">e", 2), 26: (">f", 4), 27: (">d", 8)}[ai]
        return struct.unpack(fmt, b[i:i + n])[0], i + n
    if ai >= 24:
        n = {24: 1, 25: 2, 26: 4, 27: 8}[ai]
        v = int.from_bytes(b[i:i + n], "big")
        i += n
    else:
        v = ai
    if mt == 0:
        return v, i
    if mt == 1:
        return -1 - v, i
    if mt in (2, 3):  # Auctionator writes its strings as byte strings; the readers take both as text
        return b[i:i + v].decode("utf-8", "replace"), i + v
    if mt == 4:
        out = []
        for _ in range(v):
            x, i = cbor_decode(b, i)
            out.append(x)
        return out, i
    if mt == 5:
        d = {}
        for _ in range(v):
            k, i = cbor_decode(b, i)
            x, i = cbor_decode(b, i)
            d[k] = x
        return d, i
    if mt == 6:
        return cbor_decode(b, i)
    raise ValueError(f"CBOR major type {mt}")


def cbor_head(mt: int, n: int) -> bytes:
    if n < 24:
        return bytes([mt << 5 | n])
    for ai, size in ((24, 1), (25, 2), (26, 4), (27, 8)):
        if n < 1 << (8 * size):
            return bytes([mt << 5 | ai]) + n.to_bytes(size, "big")
    raise ValueError("integer too large for CBOR")


def cbor_encode(v) -> bytes:
    if v is False:
        return b"\xf4"
    if v is True:
        return b"\xf5"
    if v is None:
        return b"\xf6"
    if isinstance(v, int):
        return cbor_head(0, v) if v >= 0 else cbor_head(1, -1 - v)
    if isinstance(v, float):
        return b"\xfb" + struct.pack(">d", v)
    if isinstance(v, bytes):
        return cbor_head(2, len(v)) + v
    if isinstance(v, str):
        raw = v.encode("utf-8")
        return cbor_head(3, len(raw)) + raw
    if isinstance(v, list):
        return cbor_head(4, len(v)) + b"".join(cbor_encode(x) for x in v)
    if isinstance(v, dict):
        return cbor_head(5, len(v)) + b"".join(cbor_encode(k) + cbor_encode(x) for k, x in v.items())
    raise TypeError(f"cannot encode {type(v).__name__} as CBOR")


# ---------- SavedVariables in the game's format ----------
def sv_string(text: str) -> bytes:
    return sv_bytes(text.encode("utf-8"))


def sv_bytes(raw: bytes) -> bytes:
    """A Lua string literal the way the game escapes it; every other byte stays raw."""
    out = bytearray(b'"')
    for c in raw:
        if c == 34:
            out += b'\\"'
        elif c == 92:
            out += b"\\\\"
        elif c == 10:
            out += b"\\n"
        elif c == 13:
            out += b"\\r"
        elif c == 0:
            out += b"\\000"  # three digits: a digit after it must not join the escape
        else:
            out.append(c)
    return bytes(out + b'"')


def sv_value(v) -> bytes:
    if isinstance(v, bool):
        return b"true" if v else b"false"
    if isinstance(v, int):
        return str(v).encode()
    if isinstance(v, float):
        return (str(int(v)) if v.is_integer() else repr(v)).encode()
    if isinstance(v, str):
        return sv_string(v)
    if isinstance(v, bytes):
        return sv_bytes(v)
    if isinstance(v, list):
        return b"{\n" + b"".join(sv_value(x) + b",\n" for x in v) + b"}"
    if isinstance(v, dict):
        # Sorted keys (numbers first): lupa hands the tables over in an order that changes from run to run,
        # and the fixture should only change in git when its data does.
        parts = []
        for k, x in sorted(v.items(), key=lambda kv: (not isinstance(kv[0], int), kv[0] if isinstance(kv[0], int)
                                                      else 0, str(kv[0]))):
            key = b"[%d]" % k if isinstance(k, int) else b"[" + sv_string(str(k)) + b"]"
            parts.append(key + b" = " + sv_value(x) + b",\n")
        return b"{\n" + b"".join(parts) + b"}"
    raise TypeError(f"cannot write {type(v).__name__}")


def sv_file(globals_: dict) -> bytes:
    return b"\n" + b"".join(name.encode() + b" = " + sv_value(v) + b"\n" for name, v in globals_.items())


# ---------- anonymizing ----------
class Names:
    """Real name → made-up name, applied to every string."""

    def __init__(self):
        self.pairs: list[tuple[str, str]] = []
        self.forbidden: set[str] = set()

    def add(self, real: str, fake: str) -> None:
        if real and real != fake:
            self.pairs.append((real, fake))
            self.forbidden.add(real)

    def forbid(self, real: str) -> None:
        if real:
            self.forbidden.add(real)

    def text(self, s: str) -> str:
        for real, fake in sorted(self.pairs, key=lambda p: -len(p[0])):  # "Malgruk-Höllenbrand" before "Malgruk"
            s = s.replace(real, fake)
        return s

    def walk(self, v):
        if isinstance(v, str):
            return self.text(v)
        if isinstance(v, list):
            return [self.walk(x) for x in v]
        if isinstance(v, dict):
            return {(self.text(k) if isinstance(k, str) else k): self.walk(x) for k, x in v.items()}
        return v


def account_dir(wow: str) -> str:
    found = [d for d in glob.glob(os.path.join(wow, "WTF", "Account", "*"))
             if os.path.isfile(os.path.join(d, "SavedVariables", "WoWMCP.lua"))]
    if not found:
        sys.exit(f"no account with WoWMCP.lua under {wow}")
    return found[0]


def load_realms(path: str) -> dict:
    """Auctionator's price database per realm, with the key types of the CBOR."""
    data = open(path, "rb").read()
    start = data.index(b"AUCTIONATOR_PRICE_DATABASE = {")
    end = data.index(b"\n}", start)
    import re
    realms = {}
    for m in re.finditer(rb'\["([^"]+)"\] = "', data[start:end]):
        raw, _ = writs.lua_string(data, start + m.end() - 1)
        try:
            obj, _ = cbor_decode(raw)
        except Exception:
            continue
        if isinstance(obj, dict):
            realms[m.group(1).decode("utf-8", "replace")] = obj
    return realms


def item_id(key) -> int | None:
    head = str(key).split(":")[0]
    return int(head) if head.isdigit() else None


def trim_days(entry, keep: set):
    """An entry of the price database with only the scan days in `keep`."""
    if not isinstance(entry, dict):
        return entry
    out = {}
    for k, v in entry.items():
        if k in ("a", "h", "l") and isinstance(v, dict):
            v = {d: x for d, x in v.items() if int(d) in keep}
        out[k] = v
    return out


def carried_ids(snapshot: dict) -> set[int]:
    ids = {e.get("id") for e in snapshot.get("equipment") or []}
    for bag in snapshot.get("bags") or []:
        ids |= {it.get("id") for it in bag.get("items") or []}
    for box in (snapshot.get("bank") or {}).get("containers") or []:
        ids |= {it.get("id") for it in box.get("items") or []}
    return {i for i in ids if isinstance(i, int)}


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--from", dest="src", default=writs.DEFAULT_WOW, help="the real _forever_ folder")
    ap.add_argument("--out", default=os.path.join(HERE, "forever"))
    a = ap.parse_args()
    src, out = a.src, os.path.abspath(a.out)
    acc = account_dir(src)
    names = Names()
    names.forbid(os.path.basename(acc))

    # Characters: the newest snapshot of each class that has made-up names left.
    left = {k: list(v) for k, v in NAMES.items()}
    chars = []
    for path in writs.find_chars(src):
        snap = load_sv(path, "WoWMCP_Char")
        ch = snap.get("character") or {}
        pool = left.get(ch.get("classFile") or "")
        if not pool:
            continue
        first, last = pool.pop(0)
        folder = os.path.basename(os.path.dirname(os.path.dirname(path)))
        realm_dir = os.path.basename(os.path.dirname(os.path.dirname(os.path.dirname(path))))
        names.add(folder, f"{first}-{last}")
        names.add(ch.get("name") or "", first)
        names.add(ch.get("surname") or "", last)
        chars.append((path, realm_dir, f"{first}-{last}", snap))
    if not chars:
        sys.exit("no warlock or priest snapshot found")
    # Every other character of the account is named nowhere either.
    for d in glob.glob(os.path.join(acc, "*", "*")):
        if os.path.isdir(d) and "-" in os.path.basename(d):
            for part in [os.path.basename(d), *os.path.basename(d).split("-")]:
                if not any(part == real for real, _ in names.pairs):
                    names.add(part, "Somebody")

    if os.path.isdir(out):
        shutil.rmtree(out)
    account = os.path.join(out, "WTF", "Account", ACCOUNT)
    written: list[str] = []

    def write(path: str, data: bytes) -> None:
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "wb") as f:
            f.write(data)
        written.append(path)

    levels = []
    keep_items = set()
    for path, realm_dir, folder, snap in chars:
        snap = names.walk(snap)
        levels.append((snap.get("character") or {}).get("level") or 60)
        keep_items |= carried_ids(snap)
        write(os.path.join(account, realm_dir, folder, "SavedVariables", "WoWMCP.lua"),
              sv_file({"WoWMCP_Char": snap}))

    # Price database: the realm with the most entries, the last DAYS scan days, a sample of the equipment.
    realms = load_realms(os.path.join(acc, "SavedVariables", "Auctionator.lua"))
    if not realms:
        sys.exit("no price database in Auctionator.lua")
    for realm in realms:
        names.forbid(realm)
    db = max(realms.values(), key=len)
    export_all = load_sv(os.path.join(acc, "SavedVariables", "WoWMCP.lua"), "WoWMCP_Items") or {}
    equipment = {int(k): v for k, v in (export_all.get("items") or {}).items()}

    def wanted(iid: int) -> bool:
        if iid in keep_items:
            return True
        it = equipment.get(iid)
        if it is None:
            return True  # not equipment: reagents, consumables, writs, crates
        req = it.get("reqLevel") or 0
        near = any(lvl - LEVEL_BELOW <= req <= lvl + LEVEL_ABOVE for lvl in levels)
        return near and iid % EQUIP_SHARE == 0

    days = sorted({int(d) for v in db.values() if isinstance(v, dict) for d in (v.get("a") or {})})
    keep_days = set(days[-DAYS:])
    trimmed = {}
    for k, v in db.items():
        iid = item_id(k)
        if iid is None:
            trimmed[k] = v
        elif wanted(iid):
            v = trim_days(v, keep_days)
            if not isinstance(v, dict) or v.get("a") or v.get("m"):
                trimmed[k] = v
    blob = cbor_encode(trimmed)
    write(os.path.join(account, "SavedVariables", "Auctionator.lua"),
          b"\nAUCTIONATOR_PRICE_DATABASE = {\n[\"__dbversion\"] = 8,\n[" + sv_string(REALM) + b"] = "
          + sv_bytes(blob) + b",\n}\n")

    # The account's WoWMCP.lua: the item export trimmed like the price database.
    export = dict(export_all)
    export["items"] = {iid: it for iid, it in equipment.items()
                       if iid in keep_items or (wanted(iid) and str(iid) in {str(item_id(k)) for k in trimmed})}
    export["count"] = len(export["items"])
    acct = load_sv(os.path.join(acc, "SavedVariables", "WoWMCP.lua"), "WoWMCP_Account") or {}
    write(os.path.join(account, "SavedVariables", "WoWMCP.lua"),
          sv_file({"WoWMCP_Account": names.walk(acct), "WoWMCP_Items": names.walk(export)}))

    # GoldPerHour: the test data of the evaluation (made-up already).
    gph = os.path.join(SCRIPTS, "fixtures", "goldperhour", "WTF", "Account", "TEST", "SavedVariables",
                       "GoldPerHour.lua")
    write(os.path.join(account, "SavedVariables", "GoldPerHour.lua"), open(gph, "rb").read())

    # Client language, an installed WoWMCP that loads AppData.lua, and the made-up combat log.
    write(os.path.join(out, "WTF", "Config.wtf"), b'SET textLocale "deDE"\n')
    write(os.path.join(out, "Interface", "AddOns", "WoWMCP", "WoWMCP.toc"),
          b"## Interface: 16001\n## Title: WoWMCP\n## Version: 1.12.0\n"
          b"## SavedVariables: WoWMCP_Account, WoWMCP_Items\n## SavedVariablesPerCharacter: WoWMCP_Char\n\n"
          b"AppRequest.lua\nAppData.lua\nLocales.lua\nWoWMCP.lua\nLists.lua\nHistory.lua\n")
    warlock = next((f for _, _, f, s in chars if (s.get("character") or {}).get("classFile") == "WARLOCK"), None)
    if warlock:
        log = test_combatlog.sample()
        log.lines = [line.replace(test_combatlog.ME[1], warlock) for line in log.lines]
        log.write(out)
        written.append(os.path.join(out, "Logs", "WoWCombatLog-112026_100000.txt"))

    # Nothing that names a person may be left.
    leaks = []
    for path in written:
        data = open(path, "rb").read()
        for real in names.forbidden:
            if real.encode("utf-8") in data:
                leaks.append(f"{os.path.relpath(path, out)}: a real name is left")
    if leaks:
        shutil.rmtree(out)
        sys.exit("\n".join(sorted(set(leaks))))
    size = sum(os.path.getsize(p) for p in written)
    print(f"{out}: {len(chars)} characters, {len(trimmed)} price entries ({len(keep_days)} days), "
          f"{len(export['items'])} export items, {size / 1e6:.1f} MB")


if __name__ == "__main__":
    main()
