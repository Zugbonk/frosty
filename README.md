# Frosty Zugbonk

**Version 1.0** — Your Everyday WoW Companion

Native, portable 32-bit Windows companion application for **World of Warcraft 3.3.5a**.

**Free for noncommercial use · Source-available · Commercial licensing by agreement.**

Frosty Zugbonk offers a teleport catalog, movement controls, world tracking, game-client selection, and optional online information. The application works without an installer, registration, activation, or a required network connection. The core runtime is written in Go, using native Win32/GDI with no CGo or third-party Go dependencies.

> Unofficial independent project. Not affiliated with or endorsed by Blizzard Entertainment. Using third-party tools with a game may be subject to the game's own rules and terms.

## Build

Install **Go 1.23 or newer** on Windows, then run:

```bat
BUILD_WINDOWS.bat
```

Or use the equivalent command:

```bat
set GOOS=windows
set GOARCH=386
set CGO_ENABLED=0
go build -ldflags "-H=windowsgui -s -w" -o "Frosty Zugbonk.exe" .
```

The resulting portable EXE embeds all graphics and the default teleport catalog. No external image, source, or TSV files are needed alongside it at runtime.

`icon_windows.syso` is included so Go can embed the EXE icon and Windows Details/version information with **no additional build-time tools**. If you later change the icon or version fields, use **Python 3 standard library** to regenerate it:

```bat
python tools\build_resources.py
```

The generated COFF resource contains the original application icon and English (United States) VERSIONINFO. Current product metadata uses the creator-approved project name, website, copyright, and version 1.0.

## Source layout

- `main_windows.go`, `render_windows.go`, `winapi_windows.go` — native Win32 main interface and rendering
- `*_windows.go` — runtime/UI modules: movement, tracking, teleport, multiple game clients, custom menus, information windows and online content
- `internal/` — standalone, unit-tested Go modules for catalog operations, UI policies, movement, tracking, teleport, QR generation, remote data and settings migration
- `assets/` — current embedded sprites, backgrounds, theme previews, sidebar fallback and icon only
- `data/teleports.tsv` — final embedded default catalog (**1,042 locations; 12-column TSV**)
- `tools/build_resources.py` — Windows icon and VERSIONINFO resource generator
- `BUILD_WINDOWS.bat` — build script, including Go unit tests

There are no earlier compiled release binaries or obsolete development changelogs inside this source folder.

## User data and settings

Frosty writes user preferences and the editable teleport database to:

```text
%APPDATA%\Zugbonk\Frosty\settings.json
%APPDATA%\Zugbonk\Frosty\teleports.tsv
```

If a compatible preferences file from a previous installation exists, the application migrates it to **`settings.json`** and preserves the agreed theme and first-run selections. It does not overwrite an existing `settings.json` with older preferences.

On first run, the embedded default teleport TSV is copied to AppData. Subsequent user edits are stored in AppData; upgrading the EXE does **not** overwrite them. **Restore Defaults** explicitly resets the working catalog after a confirmation. The catalog can also be imported and exported as plaintext TSV.

## Network behavior

Optional public information (banner, About, HowTo, Donations, Contacts) is retrieved using read-only HTTPS requests. Gameplay operation does not depend on these requests; an embedded fallback banner is included. Banner images are aspect-ratio-preserved in the portrait display area.

## Tests

```bat
go test ./internal/...
```

The unit tests validate Go-side functionality, not actual in-game behavior. Real Windows/gameplay acceptance and any third-party assets' redistribution rights should be verified before public distribution.

## License and attribution

**Frosty Zugbonk is source-available, free for noncommercial use.** It is not
OSI-defined open-source software, because the license reserves commercial use.

**Copyright © 2026 Zugbonk.** Original repository:
https://github.com/Zugbonk/frosty

The copyrightable project material owned by Zugbonk is licensed under the
[PolyForm Noncommercial License 1.0.0](LICENSE). In summary:

- **Noncommercial:** You may use, study, modify, and share the software for
  permitted noncommercial purposes, provided you comply with the license.
- **Attribution:** Redistributors must preserve the required copyright and
  original-repository attribution line in [NOTICE](NOTICE), as required by the
  license. You must also provide the license terms or their official URL.
- **Commercial:** Commercial permission is **not** included. Any commercial
  use requires a separate written license from Zugbonk, which may include
  fees or negotiated royalties. See [COMMERCIAL-LICENSING.md](COMMERCIAL-LICENSING.md).
- **Contributions:** Please read [CONTRIBUTING.md](CONTRIBUTING.md) before
  proposing code or art changes.

The full license governs; this summary does not add or replace license terms.
No fees or royalties are due automatically; those would be agreed separately.
The license cannot grant rights in third-party materials not owned by Zugbonk.
The rights to distribute any third-party artwork or borrowed components must
be checked before public distribution.

Website: **https://www.zugbonk.com**
