**Install:** download `WoW-Tools-Forever-Setup-<version>.exe` below and run it. It installs for your Windows user
only and asks for no administrator rights. The installer is not code signed, so Windows SmartScreen asks once
("More info", then "Run anyway"). `SHA256SUMS.txt` holds the checksum of the file.

**Update:** an installed app fetches newer releases by itself and installs them when it is closed, or right away
with "Restart and update" on the "Connection" page.

**Coming from "WoW Forever Tools" (2.1.0 or older):** the app and its companion addon were renamed to
"WoW Tools Forever" (`WoWToolsForever`, slash command `/wtf`). Install this release once manually and uninstall
the old "WoW Forever Tools" under Windows Settings > Apps; settings and downloaded data stay. Installing the
`WoWToolsForever` addon from the "Connection" page removes the old `WoWMCP` addon folder automatically.

The app brings everything it needs; nothing else is installed or downloaded on the first start. Its window is
Microsoft Edge WebView2, which is part of Windows 10 and 11. It fetches game tables and item icons from
wago.tools when a page first needs them. The addons WoWToolsForever and GoldPerHour are installed from the
"Connection" page.
