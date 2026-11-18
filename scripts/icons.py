# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0", "pillow>=10"]
# ///
"""Item icons for the desktop app: item ID -> icon FileDataID -> PNG in scripts/cache/icons/.

    uv run scripts/icons.py 9492 287004 ...

The game itself only knows icons as FileDataIDs. The mapping comes from Forever's DB2 tables on wago.tools
(product wowf): ItemModifiedAppearance -> ItemAppearance.DefaultIconFileDataID, with Item.IconFileDataID as
fallback (newer Forever items have 0 there). The icon itself is a BLP from wago.tools' CASC endpoint, which
Pillow decodes. Everything is cached, so each table and icon is downloaded once.
"""

from __future__ import annotations

import csv
import glob
import io
import json
import os
import re
import sys
import urllib.request
from concurrent.futures import ThreadPoolExecutor

HERE = os.path.dirname(os.path.abspath(__file__))
# The packaged desktop app points WOW_TOOLS_CACHE at a writable folder; a checkout caches next to the scripts.
CACHE = os.environ.get("WOW_TOOLS_CACHE") or os.path.join(HERE, "cache")
ICON_DIR = os.path.join(CACHE, "icons")
QUESTION_MARK = 134400  # INV_Misc_QuestionMark
UA = {"User-Agent": "Mozilla/5.0 (wow-forever-tools)"}
LOCALIZED = {"ItemSparse", "SpellName"}  # tables with text; cached per client locale
DEFAULT_LOCALE = "enUS"


def _get(url: str, timeout: float = 30) -> bytes:
    with urllib.request.urlopen(urllib.request.Request(url, headers=UA), timeout=timeout) as r:
        return r.read()


def client_build(wow: str) -> str | None:
    """Newest client build from the character snapshots, e.g. "1.60.1.70938"."""
    sys.path.insert(0, HERE)
    import upgrades  # noqa: E402  (lupa only when needed)
    import writs  # noqa: E402

    best = None
    for path in writs.find_chars(wow):
        try:
            cl = upgrades.load_char(path).get("client") or {}
        except Exception:
            continue
        if cl.get("version") and cl.get("build"):
            b = f"{cl['version']}.{int(cl['build'])}"
            if best is None or int(cl["build"]) > int(best.rsplit(".", 1)[1]):
                best = b
    return best


def _build_number(path: str) -> int:
    m = re.search(r"\.(\d+)(?:-[A-Za-z]{4})?\.csv$", path)
    return int(m.group(1)) if m else 0


def table(name: str, build: str | None, locale: str | None = None) -> list[dict]:
    """DB2 table as rows; downloads it for `build`, otherwise uses the newest cached copy. Tables with text
    (LOCALIZED) are fetched and cached per locale, e.g. ItemSparse-1.60.1.70938-deDE.csv."""
    os.makedirs(CACHE, exist_ok=True)
    suffix, query = "", ""
    if name in LOCALIZED:
        locale = locale or DEFAULT_LOCALE
        suffix, query = f"-{locale}", f"&locale={locale}"
    path = os.path.join(CACHE, f"{name}-{build}{suffix}.csv") if build else None
    if path and not os.path.exists(path):
        try:
            data = _get(f"https://wago.tools/db2/{name}/csv?build={build}{query}", timeout=60)
            if data.startswith(b"ID,"):
                with open(path, "wb") as f:
                    f.write(data)
        except OSError:
            pass
    if not path or not os.path.exists(path):
        cached = sorted(glob.glob(os.path.join(CACHE, f"{name}-*{suffix}.csv")), key=_build_number)
        if not cached:
            return []
        path = cached[-1]
    with open(path, encoding="utf-8", newline="") as f:
        return list(csv.DictReader(f))


def icon_map(item_ids: set[int], build: str | None) -> dict[int, int]:
    """Item ID -> icon FileDataID for the requested items."""
    out: dict[int, int] = {}
    appearance_of: dict[int, tuple[int, int]] = {}  # item -> (modifier, appearance); modifier 0 wins
    for r in table("ItemModifiedAppearance", build):
        iid = int(r["ItemID"])
        if iid in item_ids:
            key = (int(r["ItemAppearanceModifierID"]), int(r["ItemAppearanceID"]))
            if iid not in appearance_of or key < appearance_of[iid]:
                appearance_of[iid] = key
    wanted = {a for _, a in appearance_of.values()}
    icon_of_appearance = {int(r["ID"]): int(r["DefaultIconFileDataID"])
                          for r in table("ItemAppearance", build) if int(r["ID"]) in wanted}
    for iid, (_, a) in appearance_of.items():
        if icon_of_appearance.get(a):
            out[iid] = icon_of_appearance[a]
    missing = item_ids - set(out)
    if missing:
        for r in table("Item", build):
            iid = int(r["ID"])
            if iid in missing and int(r["IconFileDataID"] or 0):
                out[iid] = int(r["IconFileDataID"])
    return out


def icon_path(fdid: int) -> str:
    return os.path.join(ICON_DIR, f"{fdid}.png")


def fetch_icon(fdid: int) -> bool:
    """Downloads one icon BLP and stores it as PNG. True if the PNG exists afterwards."""
    path = icon_path(fdid)
    if os.path.exists(path):
        return True
    from PIL import Image

    try:
        data = _get(f"https://wago.tools/api/casc/{fdid}?download")
        img = Image.open(io.BytesIO(data)).convert("RGBA")
    except Exception:
        return False
    tmp = f"{path}.tmp"
    img.save(tmp, "PNG")
    os.replace(tmp, path)
    return True


def resolve(wow: str, item_ids: list[int]) -> dict:
    """{"icons": {itemId: fdid or null}, "dir": ICON_DIR}; downloads what is not cached yet."""
    ids = {int(i) for i in item_ids if int(i) > 0}
    mapping = icon_map(ids, client_build(wow)) if ids else {}
    os.makedirs(ICON_DIR, exist_ok=True)
    need = sorted({*mapping.values(), QUESTION_MARK})
    with ThreadPoolExecutor(max_workers=8) as pool:
        ok = dict(zip(need, pool.map(fetch_icon, need)))
    icons = {str(i): (mapping[i] if ok.get(mapping.get(i)) else None) for i in sorted(ids)}
    return {"icons": icons, "fallback": QUESTION_MARK if ok.get(QUESTION_MARK) else None, "dir": ICON_DIR}


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    import writs  # noqa: E402

    print(json.dumps(resolve(writs.DEFAULT_WOW, [int(a) for a in sys.argv[1:]])))
