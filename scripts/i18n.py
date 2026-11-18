"""Language of everything the scripts print or hand to the desktop app.

The source text is English and doubles as the key: `tr("only {n} in the AH", n=3)`. Other languages live
in a catalog module (`locale_de.py` for German); a text without a translation stays English. Texts that are
looked up through a variable are wrapped in `mark("...")` where they are defined, so `test_i18n.py` finds
them and can check that the catalog is complete.

The language follows the game client unless it is set: `--lang en|de` on the command line, the environment
variable WOW_TOOLS_LANG, or the client's `textLocale` in WTF/Config.wtf (deDE -> German, anything else English).
Data that comes from the game (item, zone and profession names, tooltips) is always in the client's language.
"""

from __future__ import annotations

import importlib
import os
import re

LANGUAGES = ("en", "de")
DEFAULT_LOCALE = "enUS"
_language = "en"
_catalog: dict[str, str] = {}


def client_locale(wow: str | None) -> str | None:
    """Text locale of the game client, e.g. "deDE", from WTF/Config.wtf. None if it cannot be read."""
    if not wow:
        return None
    try:
        with open(os.path.join(wow, "WTF", "Config.wtf"), encoding="utf-8", errors="replace") as f:
            m = re.search(r'^SET\s+textLocale\s+"(\w+)"', f.read(), re.M)
    except OSError:
        return None
    return m.group(1) if m else None


def language_of(locale: str | None) -> str:
    """Language of this module for a client locale: "de" for deDE, "en" for everything else."""
    return "de" if (locale or "").lower().startswith("de") else "en"


def set_language(lang: str | None = None, wow: str | None = None) -> str:
    """Selects the language: `lang` if it is one of LANGUAGES, else WOW_TOOLS_LANG, else the client's."""
    global _language, _catalog
    choice = (lang or "").lower()
    if choice not in LANGUAGES:
        choice = (os.environ.get("WOW_TOOLS_LANG") or "").lower()
    if choice not in LANGUAGES:
        choice = language_of(client_locale(wow))
    _language = choice
    _catalog = {} if choice == "en" else importlib.import_module(f"locale_{choice}").TEXT
    return choice


def language() -> str:
    return _language


def mark(text: str) -> str:
    """Marks a text for translation without translating it yet (see the module docstring)."""
    return text


def tr(text: str, **values) -> str:
    """`text` in the selected language, with `{name}` placeholders filled in."""
    text = _catalog.get(text, text)
    return text.format(**values) if values else text


def short_date(iso: str) -> str:
    """Day and month of an ISO date the way the language writes it: 11/14 or 14.11."""
    month, day = iso[5:7], iso[8:10]
    return f"{day}.{month}." if _language == "de" else f"{month}/{day}"


def add_argument(parser) -> None:
    """Adds --lang to an argparse parser."""
    parser.add_argument("--lang", choices=["auto", *LANGUAGES], default="auto",
                        help="language of the output (default: the language of the game client)")
