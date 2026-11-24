**Install:** download `WoW-Forever-Tools-Setup-<version>.exe` below and run it. It installs for your Windows user
only and asks for no administrator rights. The installer is not code signed, so Windows SmartScreen asks once
("More info", then "Run anyway"). `SHA256SUMS.txt` holds the checksum of the file.

**Update:** an installed app of version 2.0.0 or newer fetches this release by itself and installs it when it is
closed, or right away with "Restart and update" on the "Connection" page.

**Coming from 1.12.0 or older:** those versions were built with Electron and installed into another folder.
Uninstall "WoW Forever Tools" once under Windows Settings > Apps; settings and downloaded data stay.

The app brings everything it needs; nothing else is installed or downloaded on the first start. Its window is
Microsoft Edge WebView2, which is part of Windows 10 and 11. It fetches game tables and item icons from
wago.tools when a page first needs them. The addons WoWMCP and GoldPerHour are installed from the "Connection"
page.
