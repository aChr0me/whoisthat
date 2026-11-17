# Omarchy GUI design — whoisthat kill-switch control

Research + design only. No code, no system changes. Grounded in what's actually
installed on this box under `/usr/share/omarchy` and `~/.config/omarchy`.

## 1. How Omarchy plugins actually work here

This machine runs a **QML/Quickshell bar**, not classic Waybar (no `waybar`
binary found; `/usr/share/omarchy/shell/shell.qml` is the real bar). Key facts:

- **Plugin dir:** `~/.config/omarchy/plugins/<reverse-dns-id>/manifest.json`
  plus QML entry points. First-party plugins live under
  `/usr/share/omarchy/shell/plugins/`.
- **Manifest schema (`schemaVersion: 1`):** required fields `id`, `name`,
  `version`, `kinds`, `entryPoints`. `id` must be reverse-DNS-style, no `/`,
  no `..`, validated by `PluginRegistry.qml` (path-traversal guarded).
- **Two plugin `kinds` observed:**
  - `bar-widget` — a small `BarWidget.qml` button in the top bar
    (`defaultSection`: left/center/right; `allowMultiple`; a `schema` array
    driving a settings form Omarchy renders for you). Example:
    `io.github.archer-clawbot.hermarchy-agent` — polls a Python state
    collector every N seconds, renders a label + colored signal dot, opens a
    `Panel.qml` detail view on click.
  - `panel` — a standalone popup/applet (`io.voxtype.settings`), `keepLoaded:
    true`, summoned via `omarchy-shell shell toggle <id> '{}'`. No bar
    presence of its own; it's a settings applet other things launch.
- **IPC surface:** `omarchy-shell <target> <method> [json-args]` — a thin CLI
  wrapper that talks to the running shell over its Quickshell IPC (recovers
  `WAYLAND_DISPLAY` from the compositor socket, so it works from systemd units
  or SSH, not just inside the session). First-party tools like `omarchy menu`
  are themselves just wrappers around `omarchy-shell shell toggle|summon|call`.
- **State source pattern:** plugins don't talk to daemons directly in QML;
  they shell out to a small script/binary that emits validated JSON on stdout
  (see `hermarchy-state-read`: subreaper, timeouts, size caps, strict
  `collect`/`validate` split) and the QML layer polls that script on a timer.
  This is the house style for "daemon state → bar pixel" plumbing.
- **Theming:** handled centrally (`~/.config/omarchy/themes`,
  `branding/`, `shell.json`'s `bar` section) — widgets use `qs.Commons`/`qs.Ui`
  (`Style.*`, `WidgetButton`) rather than hand-rolled colors, so a
  theme-correct widget is mostly "use the provided components," not custom CSS.
- **Click semantics convention:** `WidgetButton.onPressed` dispatches on
  `buttonCode` (e.g. middle-click = force refresh, left-click = toggle panel)
  — matches the existing `omarchy.network`/`omarchy.bluetooth` widgets' feel.

## 2. Design options

### Option A — Bar-widget only, click-to-toggle

A `bar-widget` plugin showing connection state (icon/label + color) via a
polling script that runs `whoisthat status -j`. Click toggles connect/disconnect
(`whoisthat connect`/`disconnect -j`); middle-click forces a status refresh.
No separate panel.

- **Pros:** smallest surface, matches `hermarchy-agent`'s proven pattern
  almost exactly, cheapest to build and audit, no JSON schema design needed
  beyond what `status -j` already returns.
- **Cons:** no room for mode switch (proxy/tun), profile selection, or
  kill-switch arm/disarm — those need a modifier-click or get left out
  entirely, which is the kill-switch project's actual core feature.
- **Effort:** small (1 QML file + 1 polling script + manifest).

### Option B — Bar-widget + detail panel (recommended, see §3)

Same bar pixel as Option A, but click opens a `Panel.qml` (like
`hermarchy-agent`'s `panelLoader` pattern) showing: connect/disconnect,
mode toggle (proxy/tun), kill-switch arm/disarm, current profile, and a
reconnect/refresh action. The bar button itself stays a single glanceable
status chip; the panel is where every CLI capability gets a control.

- **Pros:** one plugin covers 100% of current CLI surface
  (connect/disconnect/mode/settings/status/profile), reuses the exact
  bar-widget+panel composition Omarchy already ships for both first-party
  (clock, weather) and third-party (`hermarchy-agent`) plugins — lowest
  "fights the framework" risk. Kill-switch arm state gets its own color
  signal in the bar (e.g. red dot = armed+disconnected = correctly fail-closed,
  vs. an ambiguous "disconnected" that could mean either safe or leaking).
- **Cons:** more surface to build/review than Option A; panel UI needs its
  own layout work (forms for mode/profile aren't a single poll-and-render).
- **Effort:** medium (2-3 QML files, polling script, a small command-dispatch
  script for actions, manifest with a config `schema` for poll interval).

### Option C — Full `panel`-kind settings applet, no bar widget

Like `io.voxtype.settings`: a `panel`-kind plugin with no bar presence at
all, summoned via `omarchy-shell shell toggle io.whoisthat.settings '{}'`
(bound to a keybind or launched from `omarchy menu`). Full settings surface:
profiles, sub management, autoconnect-mode, kill-switch policy, etc.

- **Pros:** most room for a "real" settings UI (profile list/switch, sub
  add, all `settings set` keys) without cramming into a bar dropdown; matches
  how Omarchy itself separates "status at a glance" (bar) from "configure
  this thing" (panel) for more complex subsystems.
- **Cons:** zero ambient visibility — a kill-switch's whole point is a user
  trusting it's armed/connected without having to open anything. No glance
  signal if the switch silently disarmed or the tunnel dropped. Doesn't match
  how this project's PLAN.md frames the widget ("comes after the client is
  proven") — a fully separate settings applet is more scope than a status
  surface needs right now.
- **Effort:** medium-large (richer panel UI, more settings plumbing, plus
  menu/keybind wiring since there's no bar entry point).

## 3. Recommendation: Option B

**Bar-widget with status dot + click-opened detail panel**, following the
`hermarchy-agent` composition pattern almost line-for-line.

Rationale:

- It's the only option that gives **both** the ambient fail-closed signal a
  kill-switch needs (bar dot: green=connected+unarmed-is-fine,
  red=disconnected+armed, amber=connecting/transitioning) **and** full control
  over everything the CLI already exposes, in one plugin.
- It reuses a pattern Omarchy already runs in production on this exact
  machine (`io.github.archer-clawbot.hermarchy-agent`), including its
  subreaper/timeout-guarded polling script — that script is directly portable
  to a `whoisthat status -j` poll with the collector binary swapped.
- It matches the project's own staging (`PLAN.md`: "Widget comes after the
  client is proven; not in scope here") — build the smallest thing that
  shows correct state honestly, don't front-load a full settings app before
  the daemon's host-acceptance tickets are closed (several are still
  `host-acceptance-pending` per `docs/PLAN.md`).
- Theming is free: `WidgetButton`/`Style.*` from `qs.Commons`/`qs.Ui` keep it
  visually consistent with `omarchy.network`/`omarchy.bluetooth` without any
  custom CSS/color work.

## 4. CLI/IPC surface: sufficient vs. needed

**Already sufficient (no CLI changes needed):**

- `whoisthat status -j` — connected/mode/kill_switch_armed, exactly what a
  polling script needs for the bar dot and panel summary.
- `whoisthat connect -j` / `disconnect -j` — panel's primary action buttons.
- `whoisthat mode proxy|tun -j` — panel mode toggle.
- `whoisthat settings set kill-switch=true|false -j` — panel arm/disarm
  toggle (note PLAN.md's `--force` flag for the safety-net disarm pattern;
  the panel shouldn't need `--force` for a normal user-initiated disarm).
- `whoisthat profile list/show -j` — panel's profile picker data source.
- `whoisthat sub add -j` — could sit in the panel's profile section, though
  it's arguably panel-C territory long-term (adding a subscription is a
  "settings" action, not a status/control action — fine to leave in the bar
  panel for now since there's exactly one sub flow today).

**Would need adding for a clean GUI (none are blocking for Option B's MVP,
but worth tracking):**

- **A push/event channel**, not just polling. Right now the GUI's only option
  is poll-on-timer via subprocess, which is what `hermarchy-agent` does, but
  for a kill-switch specifically a stale poll window (e.g. 2s) means the bar
  can show "connected" for up to 2s after a crash disarms nothing or a
  rotation re-arm event fires (see PLAN.md T7). A `whoisthat status --watch`
  / unix-socket event stream would let the widget react immediately instead
  of guessing a safe poll interval. Not required for correctness (status -j
  is still the ground truth polled every N seconds) but materially improves
  "glanceable trust."
- **A single combined state call** for the panel's first paint: today the
  panel would need `status -j` + `profile list -j` + a settings-get for
  kill-switch policy as 2-3 separate subprocess calls. A `whoisthat
  gui-state -j` (or `status -j --full`) that bundles status + current profile
  + kill-switch policy + mode in one JSON blob would cut subprocess spawns from
  3 to 1 per panel refresh — purely an ergonomics/perf nit, not a blocker.
- **Exit-code/error discipline for scripted use:** the polling script pattern
  (`hermarchy-state-read`) treats any non-zero exit or malformed JSON as "go
  dark" (muted signal) rather than showing stale data. Confirm `whoisthat
  status -j` always emits valid JSON on stdout even in failure modes (daemon
  not running, stale lock file, mid-reconcile after a crash) rather than a
  plain error string on stderr with no stdout — this needs a quick check
  against the current implementation before the widget is built, since
  `docs/host-acceptance.md` notes real crash/cleanup edge cases
  (`whoisthat-core --cleanup`) where state could be ambiguous at exactly the
  moment the widget most needs to show something true.
