# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Checks that the German catalog (sim/internal/i18n/locale_de.json, read through locale_de.py) covers every
text of the scripts: uv run scripts/test_i18n.py

Walks the syntax tree of every script for `tr("...")` and `mark("...")`. Each of these texts needs a German
entry with the same placeholders, and the catalog must not hold entries no script uses any more. While the
scripts move to Go, a text counts as used also when Go code hands it to `i18n.T(...)` or `i18n.Mark(...)`; the
Go test (sim/internal/i18n) checks the Go texts against the same catalog.
"""

import ast
import glob
import json
import os
import re
import string
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
SIM = os.path.join(os.path.dirname(HERE), "sim")
sys.path.insert(0, HERE)
import i18n  # noqa: E402
import locale_de  # noqa: E402

# The first argument of i18n.T or i18n.Mark in Go: an interpreted string literal (JSON's escapes cover the
# ones the texts use) or a raw one in backquotes.
GO_STRING = r'"(?:[^"\\\n]|\\.)*"|`[^`]*`'
# A text is a string literal, or several joined with + (a long text over several lines).
GO_TEXT = re.compile(r'\bi18n\.(?:T|Mark)\(\s*((?:%s)(?:\s*\+\s*(?:%s))*)' % (GO_STRING, GO_STRING))


def texts_in(path):
    """(text, line) for every string literal handed to tr() or mark() in a file."""
    with open(path, encoding="utf-8") as f:
        tree = ast.parse(f.read(), path)
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call) or not node.args:
            continue
        fn = node.func
        name = fn.id if isinstance(fn, ast.Name) else fn.attr if isinstance(fn, ast.Attribute) else None
        if name not in ("tr", "mark"):
            continue
        arg = node.args[0]
        if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
            yield arg.value, node.lineno
        elif name == "mark":
            raise AssertionError(f"{path}:{node.lineno}: mark() needs a string literal")


def fields(text):
    """Placeholders of a format string with their format spec, e.g. {"cap:g", "target:"}."""
    return {f"{name}:{spec}" for _, name, spec, _ in string.Formatter().parse(text) if name is not None}


def source_texts():
    out = {}
    for path in sorted(glob.glob(os.path.join(HERE, "*.py"))):
        if os.path.basename(path).startswith(("test_", "locale_")):
            continue
        for text, line in texts_in(path):
            out.setdefault(text, f"{os.path.basename(path)}:{line}")
    return out


def go_texts():
    """Texts the Go code hands to i18n.T or i18n.Mark, with file and line."""
    out = {}
    for path in sorted(glob.glob(os.path.join(SIM, "**", "*.go"), recursive=True)):
        if path.endswith("_test.go"):
            continue
        with open(path, encoding="utf-8") as f:
            src = f.read()
        for m in GO_TEXT.finditer(src):
            text = "".join(json.loads(part) if part.startswith('"') else part[1:-1]
                           for part in re.findall(GO_STRING, m.group(1)))
            line = src.count("\n", 0, m.start()) + 1
            out.setdefault(text, f"{os.path.relpath(path, SIM)}:{line}")
    return out


def test_catalog_is_complete():
    used = source_texts()
    assert len(used) > 100, len(used)
    missing = [f"{where}: {text!r}" for text, where in used.items() if text not in locale_de.TEXT]
    assert not missing, "no German text for:\n" + "\n".join(missing)
    in_go = go_texts()
    unused = [text for text in locale_de.TEXT if text not in used and text not in in_go]
    assert not unused, "catalog entries no script uses:\n" + "\n".join(unused)
    print(f"ok test_catalog_is_complete ({len(used)} texts, {len(in_go)} of them also or only in Go)")


def test_no_text_twice():
    seen = {}
    for section, texts in locale_de.sections().items():
        for text in texts:
            assert text not in seen, f"{text!r} is in {seen[text]!r} and in {section!r}"
            seen[text] = section
    print("ok test_no_text_twice")


def test_placeholders_match():
    for text, german in locale_de.TEXT.items():
        assert german.strip(), text
        assert fields(text) == fields(german), (text, german)
    print("ok test_placeholders_match")


def test_language_selection():
    wow = tempfile.mkdtemp()
    os.makedirs(os.path.join(wow, "WTF"))
    config = os.path.join(wow, "WTF", "Config.wtf")
    saved = os.environ.pop("WOW_TOOLS_LANG", None)
    try:
        assert i18n.client_locale(wow) is None and i18n.set_language("auto", wow) == "en"  # no Config.wtf: English
        with open(config, "w", encoding="utf-8") as f:
            f.write('SET portal "EU"\nSET textLocale "deDE"\nSET audioLocale "enUS"\n')
        assert i18n.client_locale(wow) == "deDE"
        assert i18n.set_language("auto", wow) == "de" and i18n.tr("Trainer") == "Lehrer"
        assert i18n.set_language("en", wow) == "en" and i18n.tr("Trainer") == "Trainer"  # --lang wins over the client
        os.environ["WOW_TOOLS_LANG"] = "en"
        assert i18n.set_language(None, wow) == "en"  # the environment wins over the client
        os.environ["WOW_TOOLS_LANG"] = "de"
        assert i18n.set_language("auto", wow) == "de" and i18n.short_date("2026-11-14") == "14.11."
        with open(config, "w", encoding="utf-8") as f:
            f.write('SET textLocale "frFR"\n')
        del os.environ["WOW_TOOLS_LANG"]
        assert i18n.set_language("auto", wow) == "en" and i18n.short_date("2026-11-14") == "11/14"
        assert i18n.tr("only {n} items in the AH", n=3) == "only 3 items in the AH"
        assert i18n.tr("a text nobody translated") == "a text nobody translated"
    finally:
        if saved is not None:
            os.environ["WOW_TOOLS_LANG"] = saved
        i18n.set_language("en")
    print("ok test_language_selection")


if __name__ == "__main__":
    test_catalog_is_complete()
    test_no_text_twice()
    test_placeholders_match()
    test_language_selection()
