# Changelog

## v1.01 — 2026-11-07

### Fixed
- Restored Add Location autofill from the working Beta 3.01 implementation.
- Populate MapID and X/Y/Z from the selected, verified WoW 3.3.5a build 12340 character snapshot.
- Populate Zone from the parent-zone client label and Location from the minimap label, falling back to subzone and then zone when needed. This preserves place labels such as `Goldshire` or `Lion's Pride Inn` while keeping the world-map zone such as `Elwynn Forest` in the Zone field.
- Preserve any already-entered values and leave Category, faction, subcategory, minimum level and comment untouched.

### Safety
- Location autofill uses bounded, read-only reads from the three Beta 3.01 client string-pointer addresses. The existing verified process handle and character snapshot reader are reused; no game-memory writes, process injection, memory scanning, or hooks were added for this feature.
- Disable Add Location while Control All is active for two or more live WoW clients. Single-client control remains supported.
- If the character is not fully loaded or a value cannot be read safely, the editor remains usable for manual entry.

### Release metadata
- Updated Windows FileVersion and ProductVersion to `1.01` (numeric version tuple `1.0.1.0`). All other VERSIONINFO strings and application identity fields remain unchanged.
- Updated the main window title bar and floating-icon hover tooltip to end with `v1.01`; the About panel shows `Application Version: 1.01`.
- No other UI layout, build flags, Go runtime settings, embedded assets, default catalog data, AppData paths, or user-data handling was intentionally changed.
