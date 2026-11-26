# Code Signing Policy

Free code signing provided by [SignPath.io](https://signpath.io/), certificate by [SignPath Foundation](https://signpath.org/).

## Scope

This policy applies to Windows release binaries of **WoW Tools Forever** built from the public GitHub repository [`kejith/wow-tools-forever`](https://github.com/kejith/wow-tools-forever):

- `WoW Tools Forever.exe` (desktop application and integrated CLI bridge)
- `Uninstall.exe` (NSIS uninstaller)
- `WoW-Tools-Forever-Setup-<version>.exe` (NSIS installer)

Only binaries built from tagged releases (`v*`) in the public GitHub repository on **GitHub-hosted runners** are submitted for signing. Binaries compiled locally on developer machines or from pull requests are never signed.

## Team Roles and Security Controls

- **Committers and Reviewers:** The project is maintained by `kejith`. Any external contributions submitted via pull requests are reviewed before being merged into `main`.
- **Approvers:** Every signing request on SignPath requires manual review and approval by the project maintainer (`kejith`) before a signature is issued.
- **Multi-Factor Authentication (MFA):** MFA is enforced on both GitHub and SignPath for all accounts with commit, review, or signing approval privileges.

## Privacy and Network Activity

WoW Tools Forever runs locally on the user's machine. It does **not** collect telemetry, usage analytics, or crash reports, and it does not require any user account.

The application makes outbound HTTPS requests only for the following documented features:

1. **Game data and item icons (`wago.tools`):** Downloads public World of Warcraft DB2 tables and item icons on demand, sending only table names, FileDataIDs, and the game client build number.
2. **Addon releases (`api.github.com` / `github.com`):** Checks and downloads official releases of the companion addons [`kejith/WoWToolsForever`](https://github.com/kejith/WoWToolsForever) and [`kejith/GoldPerHour`](https://github.com/kejith/GoldPerHour).
3. **Application updates (`api.github.com` / `github.com`):** Checks for newer releases of `kejith/wow-tools-forever`, downloads the installer, and verifies its size and SHA-256 checksum against `SHA256SUMS.txt`. Automatic downloads and installation can be turned off at any time via the **"download and install updates automatically"** switch on the **Connection** page (or by setting `"autoUpdate": false` in `%APPDATA%\wowmcp-app\settings.json`).

## License

WoW Tools Forever is open-source software licensed under the [GNU General Public License v3.0](LICENSE).
