# Omarchy Integration Options (Planning)

Status: planning artifact only. No code/system changes made.

## What Omarchy actually does (observed on this host)

- No waybar config shipped under `/usr/share/omarchy` or `~/.config/omarchy` — status/menu items are **bash scripts with metadata comments**, not a bar config file.
- Convention, e.g. `/usr/share/omarchy/bin/omarchy-network-status`:
  - Header comments: `# omarchy:summary=...`, `# omarchy:group=network`, `# omarchy:args=[--verbose]`
  - Plain-mode output: single tab-separated line (`type\tlabel\tsignal\tfreq`), `disconnected` sentinel on failure
  - `--verbose` mode: multi-line `key\tvalue` pairs for detail views
  - Uses `omarchy-cmd-present <bin>` guard before calling optional tools
- Other `omarchy-*-status` siblings exist: `omarchy-battery-status`, `omarchy-dev-status`, `omarchy-update-status`, `omarchy-weather-status` — same shape.
- Menu entries live in `omarchy-menu.jsonc` (default + user extension at `~/.config/omarchy/extensions/omarchy-menu.jsonc`), i18n'd via `default/i18n/<lang>/menu.json`.
- Theming lives under `/usr/share/omarchy/themes` and `~/.config/omarchy/themes` — no bespoke color handling needed if we just emit text.

## Option A — `omarchy-whoisthat-status` script (recommended)

Thin wrapper shell script matching the exact convention above, calling `whoisthat status -j` and reformatting.

- Drop-in at `/usr/share/omarchy/bin/` (or user override path Omarchy checks first, if one exists) named `omarchy-whoisthat-status`
- Metadata header: `# omarchy:summary=Show VPN/kill-switch status`, `# omarchy:group=network`
- Plain line: `mode\tkillswitch\tprofile` (mirrors network-status's tab format)
- `--verbose`: dump full `whoisthat status -j` fields line by line
- Add entry to user's `omarchy-menu.jsonc` extension pointing at `connect`/`disconnect`/`mode toggle` as menu actions calling `whoisthat -j ...`

**Why recommended:** zero new UI surface, matches an existing pattern Omarchy already renders/launches natively, survives Omarchy updates (we don't touch its files, only add a sibling + an extension entry), low maintenance.

## Option B — Waybar module

Not applicable here — this Omarchy install has no waybar instance in its default config tree. Would require introducing a bar Omarchy doesn't ship, fighting its conventions. Rejected unless user confirms they've manually added waybar outside Omarchy's control.

## Option C — Standalone GUI (tray icon / applet)

Possible via a Rust GTK/egui tray binary calling the CLI's `-j` output, but:
- No tray convention observed in Omarchy's shipped bin/config (menu-driven instead)
- Adds a persistent process + packaging surface the CLI-first design was explicitly avoiding
- Rejected as over-scope for current CLI-first direction; revisit only if user wants persistent visual state beyond on-demand status checks.

## Decision

**Go with Option A.** Next build-phase steps (not done here):
1. Write `omarchy-whoisthat-status` following the exact header/output contract above
2. Add `whoisthat` actions to the user's `omarchy-menu.jsonc` extension (not the system default)
3. Validate output against `omarchy-cmd-present` guard pattern for optional deps
4. Confirm with `omarchy-menu.jsonc` schema whether action entries call shell or need a `.desktop`-style definition
