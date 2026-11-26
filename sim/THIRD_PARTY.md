# Third-party notices

The engine's architecture follows the WoWSims project (https://github.com/wowsims,
MIT license): the attack-table constants in `engine/attack.go` are the
community-measured Classic formulas as WoWSims documents them. No WoWSims
source code has been copied so far. If code is ever ported, copy the WoWSims
MIT license text into this file next to the port's file list, as the license
requires.

timhul/ClassicSimForever (no license, all rights reserved) served as a
concept reference only — data-driven spells from the client tables, override
files with sources — and nothing of it was copied.

The fallback BiS item-source catalog (`sim/data/bis_sources.json.gz`) is
derived from the ForeverMyBIS open item-source dataset
(https://forevermybis.com), licensed under CC BY-SA 4.0
(https://creativecommons.org/licenses/by-sa/4.0/). Its upstream sources are:
- AllTheThings Forever database (https://github.com/ATTWoWAddon/AllTheThings, MIT License)
- Efficient Games EG Link output (https://github.com/JIVESCORP/eg-link-output-wowf, CC BY 4.0)
- Drop Tables contributors (https://github.com/valcheffnet/DropTables, CC BY-SA 4.0)
- Warcraft Wiki (https://warcraft.wiki.gg, CC BY-SA 4.0)
- wowsims Forever database (https://github.com/wowsims/forever, MIT License)

`sim/data/bis_sources.json.gz` is shared under CC BY-SA 4.0. Optional runtime
reading of locally installed addons (such as ForeverMyBIS or AtlasLootContinued)
never bundles their files into this repository.
