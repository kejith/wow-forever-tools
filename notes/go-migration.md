# Migration of the Python scripts to Go

Status: phases 0 to 4 done (2026-11-22): every bridge command is in Go and answers like Python on the
fixture and the real data. Next: phase 5.

Goal: the app computes everything with one Go binary. uv, Python, lupa and Pillow leave the installer and the
user's machine, and the answers stay the same as today, field for field.

Non-goals for this migration: changing what a page shows, the JSON contract between `main.js` and the window,
the addons. `server/wow_mcp.py` (MCP server `wow-forever`) is not part of the app and stays in Python; it can be
ported later with the official Go MCP SDK.

## Why (measured on 2026-11-21, Windows, real data)

| Bridge call | Today | Of that in the engine | Go, estimated |
| --- | --- | --- | --- |
| `character --char Malgruk` | 0.23 s | – | ~0.03 s |
| `writs --char Malgruk` | 0.59 s | – | ~0.05 s |
| `status` | 0.60 s | – | ~0.05 s |
| `pricehistory` | 0.98 s | – | ~0.1 s |
| `damage --char Malgruk` | 1.37 s | part | ~0.3 s |
| `upgrades --char Malgruk` | 1.57 s | ~0.4 s | ~0.3 s |
| `ingame` (every character) | 3.18 s | ~0.8 s | ~0.4 s |
| `consumables --char Malgruk` | 3.71 s | ~1.8 s (11 engine processes) | ~0.4 s |

Where the time goes today:

- ~200 ms per call before any work: `uv run` (~70 ms), the interpreter, the imports.
- ~0.5 s per call decoding Auctionator's price database in pure Python (`writs.lua_string`, `writs.cbor`);
  `ingame` does it once per character.
- ~0.5 s per call reading DB2 CSVs in Python (`icons.table`), also when the engine reads them again.
- Every `Engine.batch` is a new `simforever` process that loads the catalog again (~160 ms each).

Size: the installer is 129 MB. Electron is most of it; `uv.exe` is 30 MB (about 13 MB packed). The first start
downloads Python, lupa and Pillow (an estimated 100–150 MB on disk, the bridge timeout is 300 s for it). After
the migration: installer ~115–118 MB, no first-start download, works offline from the first call.

## Target layout

The existing Go module `sim/` (`github.com/kejith/wow-forever-tools/sim`) becomes the home of all Go code. The
folder keeps its name, so the engine's paths, tests and documentation stay valid.

```
sim/
  cmd/wowtools/        the one binary: `bridge <command>`, the CLIs (`writs`, `upgrades`, …), `engine` (stdin bundle)
  cmd/simforever/      stays until phase 5, then removed (wowtools engine replaces it)
  catalog/ db2/ engine/ run/ talents/ classes/   the engine, unchanged
  internal/i18n/       tr, mark, set_language, client_locale, short_date; catalog from JSON
  internal/sv/         SavedVariables reader (replaces lupa) and Auctionator's price database
  internal/game/       WoW folder, accounts, characters, snapshots, client build and locale
  internal/tables/     DB2 download and cache (icons.table: atomic write, is_table, per locale)
  internal/icons/      item → FileDataID, BLP decoder, PNG cache (replaces Pillow)
  tools/<name>/        one package per script: writs, upgrades, recipes, consumables, rotations, healing,
                       damage, combatlog, pricehistory, goldperhour, ingame, enginebundle (= simengine.py)
  bridge/              the command dispatch of app_bridge.py, one JSON object out, errors as {"error"}
```

The engine is called in-process: `enginebundle` builds a `run.Input` and calls `run.Run` directly, and the
catalog is loaded once per process (a cache in `run` keyed by cache folder, build and locale). This is where
most of the gain of `consumables`, `upgrades` and `ingame` comes from.

## Ground rules for every phase

1. **Same answer.** A ported command must give the same JSON as the Python one for the same data: same keys,
   same order of lists, numbers equal within 1e-9 relative (after rounding: equal). The parity check (phase 0)
   decides, not a look at the page.
2. **Python and Go side by side.** `main.js` gets a set `GO_COMMANDS`; a command in it runs
   `wowtools bridge <command> …`, every other one `uv run scripts/app_bridge.py <command> …` as today, with the
   same arguments and the same JSON on stdout. A release can ship in the middle of the migration.
3. **A Python module is deleted only when no Python caller is left.** `ingame.py` reads the results of
   `upgrades`, `writs`, `recipes` and `consumables`, so those Python modules stay until `ingame` is ported.
4. **Tests move with the code.** Each `scripts/test_<name>.py` becomes `sim/tools/<name>/*_test.go` with the same
   hand-made data and the same assertions, then the Python test is deleted with its module.
5. **Nothing private in fixtures.** Real SavedVariables and combat logs name the account and other players.
   Parity against real data runs locally only and its output stays in a git-ignored folder.

## Phase 0: parity harness and fixtures

- A made-up WoW folder `scripts/fixtures/forever/` (a warlock and a priest with snapshots of addon 1.12, the
  account `WoWMCP.lua` with an item export, `Auctionator.lua` with a few scan days, `GoldPerHour.lua`), built by a
  generator script so it can be written again. The existing `scripts/fixtures/goldperhour` stays.
- `sim/cmd/parity`: runs every bridge command (× character × `--lang en|de` × the important options) through
  both implementations against a WoW folder and prints a JSON diff (path, Python value, Go value). Ignores the
  fields that carry the current time. Exit code 1 on a difference.
- Baseline timings of every command (the table above) into the parity output, so each phase reports its gain.
- Done when: `go run ./cmd/parity --wow <fixture>` runs Python on both sides (Go side still missing → reported
  as "not ported") and the fixture covers every command without an error.

**Done (2026-11-21).** What came of it:

- The fixture is derived from the real snapshots, not written by hand: the engine needs real spell IDs,
  talents and spell texts, which only the game has. `scripts/fixtures/make_forever.py` replaces every name
  (characters, surnames, the account folder, realms) and fails if one is left; the combat log is the made-up
  one of `test_combatlog.py`. 1.4 MB, the same files for the same input.
- `sim/cmd/parity` with 70 cases (5 account-wide commands, 15 per character, two characters, two languages).
  All 70 answer on the fixture and on the real data; the expected errors are part of the answers (healing for
  a warlock, the weight scale for a priest, a combat log without the priest).
- `sim/cmd/wowtools` and `sim/bridge` exist and answer `bridge --list` with `[]`.
- **Found: the Python answers have no fixed key order.** lupa runs Lua 5.4, which seeds its string hashes per
  process, so every table read from a SavedVariables file comes back in another order on each run (37 objects
  in `character` alone). The parity check therefore only notes a different key order (`--strict-order`
  counts it), and the Go side is free to keep the file's order. Values and the order of lists always count.
- Volatile values: besides `generated` none, on the fixture and on the real data.
- Python times per command in WSL (the parity check prints them each run; Windows is slower, see the table
  above): `status` 0.15 s, `character` 0.06 s, `writs` 0.23 s, `upgrades` 1.1 s, `consumables` 1.3 s,
  `rotations` 2.0 s, `damage` 1.0 s, `ingame` 1.9 s on the fixture.
- Not in CI yet: the parity check needs the DB2 tables (about 80 MB from wago.tools) and both runtimes. It
  goes into `build.yml` in phase 1, with the tables in `actions/cache`, once there is a ported command to check.

## Phase 1: foundations

| Python | Go | Notes |
| --- | --- | --- |
| `i18n.py`, `locale_de.py` | `internal/i18n` | Move `TEXT` to `scripts/data/locale_de.json`, read by both Python and Go until the end. Implement the subset of `str.format` the texts use (`{name}`, `{name:.1f}`, …; list it with a scan of the catalog). `test_i18n.py` → a Go test that walks `go/ast` for `tr("…")`/`mark("…")` calls, same rules (missing entry, unused entry, placeholders). |
| `upgrades.load_sv`, `lua_to_py` (lupa) | `internal/sv` | A parser for the literal subset the game writes: `name = {…}`, nested tables with `[key] =` and list entries, strings with escapes, numbers, `true`/`false`/`nil`. Bytes are UTF-8 as the game writes them. Faster and safer than running Lua. Test against the fixture files and, locally, every real SavedVariables file. |
| `writs.load_prices`, `lua_string`, `cbor` | `internal/sv` | Auctionator's database as Go values. |
| `icons.table` and the build/locale helpers | `internal/tables` (on top of `sim/db2`) | Same cache files, same names, same atomic write, so Python and Go share `scripts/cache` during the migration. |
| `upgrades.load_char`, character lists, paths | `internal/game` | `DEFAULT_WOW` by OS, `WOW_FOREVER_DIR` wins, `WOW_TOOLS_CACHE`. |

Done when: `wowtools bridge status` and `character` pass parity; `GO_COMMANDS = {status, character}`.

**Done (2026-11-21).** What came of it:

- `GO_COMMANDS` is not a list in `main.js`: the app asks `wowtools bridge --list` once per run, so a port
  only touches Go. Without a `wowtools` binary everything still goes to Python.
- The catalog is `sim/internal/i18n/locale_de.json` (one object per script), embedded in Go and read by
  `scripts/locale_de.py`; the installed app gets it as `resources/scripts/i18n/locale_de.json`.
  `test_i18n.py` counts a text Go uses as used; the Go test checks the Go texts.
- `sim/internal/sv` reads SavedVariables without Lua. Against lupa: all 82 SavedVariables files of the real
  account agree (every addon's, not only ours), and Auctionator's price database is byte for byte what
  `writs.load_prices` makes of it (7321 entries). One file is not comparable: EllesmereUI writes a table with
  both `[168]` and `["168"]`, which are the same key in JSON.
- **Found: Python's `sum()` is compensated.** Since Python 3.12, `sum()` of floats is a Neumaier sum, so it
  differs from adding one after the other in the last bits; a score rounded to one decimal then flips at its
  half (Ilse's legs: 14.65 → Python 14.7, a plain Go loop 14.6). `pyfmt.Sum` and `pyfmt.SumAny` do what
  CPython 3.12 and 3.13 do, tested against 300 Python sums. Python 3.14 also compensates the ints among floats,
  so the installed app (uv downloads the newest Python) and WSL (3.12) can already disagree in the last bit
  today. Every `sum(` of the scripts must become `pyfmt.Sum` in Go.
- `sim/internal/pyfmt` formats as Python does (str(), the format specs the catalog uses, round()), tested
  against 800 outputs of Python itself (`gencases.py`).
- Speed: on the fixture `status` 144 → 36 ms, `character` 60 → 4 ms. On the real data under /mnt/c the files
  dominate: `status` 456 → 216 ms, `character` 104 → 49 ms (a first version was slower than Python, because
  its glob opened every file as a folder: each try is a round trip to Windows).
- CI (`build.yml`): the Linux test job runs `go run ./cmd/parity --ported` (needs no DB2 tables for these two
  commands); the installer job builds `wowtools.exe` into the app and the smoke test checks that the bundled
  one answered.

## Phase 2: the engine in-process, and its direct users

1. `simengine.py` → `tools/enginebundle`: `sheet_numbers`, `spec_of`, `norm_item`, `KEY_MAP`, `tooltip_check`,
   `Engine.start/batch/situations/heal_table/damage_table` as calls into `run`. Catalog cache in `run`.
2. `rotations.py`, `healing.py`, `damage.py` (about 960 lines together; they share the sentences of
   `rotations.py`).
3. `combatlog.py` (752 lines; its test writes its own log, port that generator as well).

Done when: `rotations`, `healing`, `damage`, `combatlog` pass parity against the fixture and locally against
Malgruk and Siegfried; `GO_COMMANDS` has them. Expect the biggest relative gain on `damage` here.

**Done (2026-11-21).** What came of it:

- `sim/tools/enginebundle` builds `run.Input` from the snapshot and calls `run.Run` in the same process; it works
  on the engine's own structs, not on its JSON. The DB2 download (`icons.table`) is `sim/internal/tables`.
  Instead of a catalog cache in `run`, `sim/db2` keeps every table it read (keyed by file, size and time):
  `warlock.Prepare` changes the catalog, the tables are never changed.
- `rotations --consumables` needs `consumables.fight_buffs`, which is phase 3. A command can therefore answer
  `{"error": "not ported to Go yet: …", "notPorted": true}`; `main.js` then asks the Python bridge, and the
  parity check counts the case as not ported. Once `consumables` is in Go the variant goes too.
- `combatlog` replays Python's readers exactly: the csv module (CPython's state machine: a quoted field may run
  into the next line), universal newlines, `int()`, `float()`, `int(x, 16)`, `date.toordinal()`, `math.fsum`
  (`statistics.fmean`) and `statistics.median`. `gencases.py` next to each writes Python's own answers as Go
  test tables. Found on the real log: a day count from year 1 overflows Go's `time.Duration`; it is computed
  from Unix seconds now.
- The parity check sets `WOW_TOOLS_CACHE` for both sides to the repo's `scripts/cache` (the Go binary of the check
  lives in a temporary folder and found no cache of its own). CI keeps the DB2 tables of the fixture's build in
  `actions/cache` (`scripts/cache/*.csv`), the first run downloads them.
- `i18n.Mark` and `i18n.T` take a text written as literals joined with `+`; both translation tests read them.
- Equal on the fixture (16 cases) and on the real data (Malgruk and Siegfried, both languages; the real combat
  log of 113 fights included). Times in WSL, Python → Go, on the real data: `rotations` 2.1 s → 0.2 s, `healing`
  0.99 s → 0.20 s, `damage` 1.0 s → 0.27 s (three engine runs), `combatlog` 1.0 s → 0.47 s (most of it reading
  the 2 MB log from /mnt/c and the engine's catalog).
- Not done here, on purpose: the Python modules, their CLIs (`uv run scripts/rotations.py …`) and their tests
  stay. `app_bridge.py` still needs `rotations.py` for the variant above, and the CLIs are documented tools; the
  tests move to Go when a module is deleted (ground rule 4), at the latest in phase 5. `simengine.tooltip_check`
  is not ported yet: only `upgrades` uses it (phase 3).

## Phase 3: the big calculations

1. `upgrades.py` (1076 lines): the weight scale, the stat keys, the package, owned gear, the engine path.
   `STAT_LINE` uses two negative lookaheads (`(?!damage done to )`, `(?!.*Schaden gegen )`), which Go's RE2 does
   not have: split it into the match plus a second check that the excluded text is absent, and keep the tests
   for both languages.
2. `consumables.py` (897 lines): catalog, decoding, the plans; `choose` calls the engine once per round, now
   in-process.
3. `writs.py`, `recipes.py`, `pricehistory.py`, `goldperhour.py`.

Done when: all of them pass parity, `GO_COMMANDS` has them, and the upgrades page, the consumables page and
the Character page are compared once as captures (`npm run capture`) against the Python version.

**Done (2026-11-22).** What came of it:

- Four ports ran side by side, each in its own git worktree and branch (`writs`, `recipes`, `goldperhour` with
  `pricehistory`, then `consumables`), `upgrades` on main; a command registers itself from its own file in
  `sim/bridge`, so the branches merged without conflicts. Shared groundwork first: `internal/tables` (DB2 rows as
  `csv.DictReader` gives them, compared with Python on all 69 cached tables), `internal/pycsv`, `game.Money`.
- **Found: Python's order of integer keys is Lua's, and it is fixed.** lupa randomizes string hashes per process,
  but integer and boolean keys hash the same in every Lua 5.4 process: a table like `WoWMCP_Items.items` comes
  out in the node order of Lua's hash part, which depends on the constructor (it sizes the part for its record
  fields) and on the insertion order. Sorts with ties keep that order (the package of `upgrades` sorted by a
  rounded gain per gold), so the Go reader lays out such tables the way `luaH_newkey` would
  (`sv/luaorder.go`; equal to lupa on 177 tables of the real game folder). Tables with string keys stay in file
  order: Python's is random there.
- `STAT_LINE`'s two negative lookaheads became a match plus a check of the text after the prefix
  (`upgrades/owned.go`); Python's `\d` is `\p{Nd}` in Go.
- `upgrades --consumables` and `rotations --consumables` run in Go: `tools/rotations` gets the consumables as a
  function from the bridge, so it does not import `tools/consumables` (which imports it).
- The shared caches (`recipes-…json`, `consumables-…json`, `itemnames-…json`) are written byte for byte as Python
  writes them; each port checked both directions from a cache with only the CSVs.
- The Python side of the engine commands needs `sim/simforever` (gitignored): CI builds it before the parity
  check, and the DB2 cache key in CI now changes with the code (restoring the last set, adding what is new).
- Equal on the fixture (all ported cases) and on the real data. Times in WSL, Python → Go, averages over the
  parity cases on the real data: `upgrades` 1.4 s → 0.5 s, `consumables` 1.5 s → 0.5 s (with the engine
  2–3 s → 0.8 s), `rotations` 2.1 s → 0.65 s (with consumables 2.6–4.2 s → 1.1 s), `writs` 0.43 s → 0.26 s,
  `recipes` 0.36 s → 0.21 s, `pricehistory` 0.58 s → 0.32 s, `goldperhour` 73 → 22 ms. What is left on the
  real data is mostly reading the files under /mnt/c (the account `WoWMCP.lua` is parsed by several steps) and,
  for the consumables, the engine re-optimizing the rotation for every candidate of every round.
- Known differences, all on inputs the game does not write (a non-numeric client build, non-ASCII digits, two
  accounts that saw the same vendor item: Go takes the files in name order, Python in file-system order).
- The app rendered every page in capture mode through `wowtools` without a page error (`report.json`), and the
  upgrades, consumables and professions pages were looked at. They were not compared image by image with the
  Python version: the parity check compares the JSON they are drawn from.

## Phase 4: icons and the lists for the game

1. `icons.py` → `internal/icons`: the FileDataID lookup from the DB2 tables and a BLP2 decoder (palettized with
   0/1/8-bit alpha, DXT1/3/5, raw BGRA) writing PNG with `image/png`. Compare the PNGs of every icon in the local
   cache pixel by pixel with Pillow's.
2. `ingame.py` → `tools/ingame`: the Lua writer, `file_state`, the write rules. The `hash` is a SHA-1 over
   `json.dumps` of the payload; Go's JSON bytes differ (separators, escaping, key order), so either reproduce
   Python's serialization exactly or accept that every user gets one rewrite of `AppData.lua` after the update
   (harmless: the game loads it on the next `/reload`). Recommendation: accept the one rewrite and use Go's
   own canonical form.

Done when: every bridge command is in `GO_COMMANDS`.

**Done (2026-11-22).** What came of it:

- `tools/ingame` writes `AppData.lua` byte for byte as Python does (compared on the fixture and the real data:
  `INGAME_WOW=… INGAME_DUMP=… go test ./tools/ingame -run DumpLua` against `ingame.to_lua` with the time fixed),
  and the `hash` is Python's: `json.dumps(sort_keys=True)` reproduced (int keys of `tips` and `vendor` sort as
  numbers). So the recommendation above was not needed: an update rewrites nothing, and the game is not asked to
  reload a file whose content did not change. The tests of `test_ingame.py` are repeated in Go, with the four
  calculations as package variables the tests replace.
- `tools/icons`: the FileDataID lookup and a BLP2 decoder that follows Pillow's `BlpImagePlugin` step by step
  (the palette's own alpha, DXT1 with and without its alpha bit, DXT3, DXT5, and the flat buffer Pillow hands to
  `set_as_raw`, so its odd cases come out alike). `gencases.py` has Pillow decode 15 made-up files of every kind
  (and the ones it refuses); all 578 icons of the local cache were fetched again as BLPs and decode to the pixels of
  Pillow's PNGs (`BLP_DIR=… ICON_DIR=scripts/cache/icons go test ./tools/icons -run CacheLikePillow`).
  Not supported, as in Pillow for BLP2: the raw BGRA encoding; BLP1 (Warcraft III) is refused,
  where Pillow would decode it (wago.tools serves BLP2 for icons). Fetched for the comparison, wago.tools answered
  eight parallel requests with 429 after a few dozen; the bridge fetches eight at a time as Python did, and an
  icon that fails shows the question mark until the app restarts (main.js remembers it for the session).
- Parity: `ingame` and `icons` equal on both folders.

## Phase 5: remove Python from the app

- `main.js`: drop `GO_COMMANDS`, the uv lookup (`settings.uvPath`, bundled uv, WinGet, `~/.local/bin`) and the
  300 s timeout; look for `wowtools` in `resources/bin`, next to the repo (`sim/wowtools(.exe)`) and in the PATH.
  `getSettings()`/the capture report name the binary instead of uv.
- `app/package.json`: `extraResources` without `scripts`, `bin` with `wowtools(.exe)` only.
- `build.yml`: no `setup-uv`, no Python tests; `go test ./...` and `go vet ./...` on Ubuntu and Windows; build
  `wowtools.exe` into `app/build/bin`; the capture check asserts the bundled `wowtools` answered.
- Delete `scripts/*.py` (but keep `server/` and what it imports; check `server/wow_mcp.py`, it has its own lupa
  reader), `scripts/test_*.py`, `cmd/simforever`.
- CLAUDE.md: the scripts table becomes `wowtools <command>` calls, "Check after changes" becomes Go tests, the
  WSL tool list loses uv for the app (uv stays for the MCP server).
- Release as a minor version (`1.x.0`): the user-visible change is the installer and the first start.

## Optional afterwards

- **A long-running process** (`wowtools serve`, JSON lines over stdin/stdout, started once by `main.js`): keeps
  the snapshots, the price database and the catalog in memory and reloads a file only when its time changes.
  Calls then take milliseconds. Independent of the migration, but only cheap once everything is Go.
- **Wails instead of Electron:** the window is vanilla JS without a build step and would run in WebView2 almost
  as it is; `main.js` (IPC, `fs.watch`, the `wowicon://` protocol, addon install, GitHub releases) would move to
  Go. The installer would shrink from ~118 MB to an estimated 15–25 MB. A project of its own, decide after phase 5.

## Risks to watch in the parity diffs

- **Rounding:** Python's `round()` rounds half to even (`round(2.5) == 2`); use `math.RoundToEven` and the same
  number of digits. About 150 `round(` calls in the scripts. Use `pyfmt.Round`.
- **Sums:** Python's `sum()` of floats is compensated (see phase 1); use `pyfmt.Sum` for every `sum(`, and add
  in the order Python does.
- **Order:** Python dicts keep insertion order, Go maps do not. Use structs or ordered slices wherever the window
  or a list shows the order; `encoding/json` sorts map keys, which is fine only for lookups (`icons`, `tips`).
  Where Python builds a list by walking a table read from SavedVariables and sorts it with ties, its order is
  random today (see phase 0); a list that differs only there is a Python bug to fix with an explicit tie-break
  on both sides, not something to copy.
- **Stable sorting:** Python's `sorted` is stable; use `sort.SliceStable` with the same keys, including
  the tie-breakers Python gets from the original order.
- **Formatting:** `f"{x:.1f}"`, thousands separators and dates in both languages through one helper, tested
  against the Python output.
- **`statistics.median`** and other small library functions: port with tests on even and odd counts.
- **Regular expressions:** RE2 has no lookaround or backreferences (only `STAT_LINE` uses them today).
- **Text encoding:** SavedVariables and CSVs are UTF-8; the Windows console issue (cp1252) goes away with Go.

## Effort

About 7,400 lines of Python and 3,700 lines of tests; in Go roughly 11,000 and 5,000. Phases 1–4 are the bulk;
each one can ship on its own because the two implementations run side by side.
