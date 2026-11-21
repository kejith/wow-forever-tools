"""German texts of the scripts. The key is the English source text (see i18n.py); placeholders must match.

The texts live in sim/internal/i18n/locale_de.json, one object per script, because the Go side reads the same
catalog while the scripts move to Go (notes/go-migration.md). The installed app has the file next to the
scripts, in i18n/locale_de.json (app/package.json, extraResources).

test_i18n.py checks that every text handed to tr() or mark() (and to i18n.T() or i18n.Mark() in Go) has an
entry and that no entry is unused.
"""

from __future__ import annotations

import json
import os

HERE = os.path.dirname(os.path.abspath(__file__))
PATHS = (os.path.join(HERE, "i18n", "locale_de.json"),  # the installed app
         os.path.join(HERE, "..", "sim", "internal", "i18n", "locale_de.json"))  # the repo


def sections() -> dict[str, dict[str, str]]:
    """The catalog as it is in the file: section (the script that uses the texts) -> English -> German."""
    for path in PATHS:
        if os.path.isfile(path):
            with open(path, encoding="utf-8") as f:
                return json.load(f)
    raise FileNotFoundError("locale_de.json not found: " + ", ".join(PATHS))


TEXT = {english: german for texts in sections().values() for english, german in texts.items()}
