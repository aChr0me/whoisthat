# WhoisThat (Omarchy bar-widget plugin)

Shows proxy connection state, mode (proxy/tun) and kill-switch arm status in
the Omarchy bar, with a click-open panel to connect/disconnect, switch
mode, and arm/disarm the kill-switch. Wraps the `whoisthat` CLI.

Plugin id: `io.github.achr0me.whoisthat`

## Bar icon colors

| Connected | Kill-switch | Color |
|---|---|---|
| yes | armed | green |
| yes | unarmed | amber |
| no | unarmed (known) | red |
| no | unknown / core offline | grey |

## Install (from this repo)

```sh
contrib/omarchy-plugin/install.sh        # symlink into ~/.config/omarchy/plugins
omarchy plugin list --json | grep achr0me   # confirm discovery
contrib/omarchy-plugin/install.sh --uninstall
```

The script links `contrib/omarchy-plugin` to
`~/.config/omarchy/plugins/io.github.achr0me.whoisthat` (honouring
`XDG_CONFIG_HOME`), so repo edits are live after a shell rescan. It refuses to
replace a real directory left by an older copy install. Note that `pinned.json`
is written inside the plugin directory, i.e. into the repo checkout.

Then enable it from the bar settings (or `omarchy plugin enable
io.github.achr0me.whoisthat right`) if it isn't auto-placed.

## Requirements

- The `whoisthat` CLI binary must be reachable. By default the state-read
  script looks for it at `~/Projects/whoisthat/target/release/whoisthat`,
  falling back to `whoisthat` on `$PATH`. Override with the `WHOISTHAT_BIN`
  environment variable if your build lives elsewhere.
- `whoisthat` needs `XDG_DATA_HOME`/`XDG_CONFIG_HOME` pointed at its data
  dirs; the state-read script defaults these to
  `~/.hermes/cache/scratch/wt-data` / `wt-config` to match this dev box, but
  will respect whatever is already set in your environment.

## Usage

- The bar icon shows `WT` plus a colored dot. Left-click opens the panel,
  middle-click forces an immediate refresh.
- The panel shows status, mode, kill-switch state, RX/TX speed and the
  last-observed timestamp, polled every `refreshIntervalSec` seconds
  (default 3, configurable in widget settings).
- Three buttons are **click-only**, never auto-triggered by polling:
  - **Connect / Disconnect** — runs `whoisthat connect <profile> --wait` or
    `whoisthat disconnect --wait`. The profile is the
    `defaultProfile` widget setting (`group:profile`) when set, otherwise the
    browsed or active profile, otherwise the first loaded one. Until the
    subscription list has loaded the button reads "NO PROFILES LOADED" and is
    disabled.
  - **Switch mode** — toggles `whoisthat mode proxy|tun`.
  - **Arm/Disarm kill-switch** — runs `whoisthat settings set
    kill-switch=true|false`.

⚠️ **Arming/disarming the kill-switch or switching modes while connected can
interrupt live network traffic on this machine.** Only click these buttons
when you intend that effect; the plugin will never trigger them on its own.

## Architecture notes

- `scripts/whoisthat-state-read` is a small Python script that shells out to
  `whoisthat status -j` (and, if the core is reachable, `whoisthat settings
  get kill-switch -j`) with bounded timeouts, and always prints exactly one
  JSON object matching the schema `StateModel.js` expects — even on
  failure, so QML never blocks or crashes on a dead core.
- `StateModel.js` validates that JSON defensively (`accept()`/`fallback()`,
  same pattern as `hermarchy-agent`'s `StateModel.js`): any shape mismatch
  silently degrades to a safe "core offline" fallback record instead of
  propagating a parse error into the UI.
- `Panel.qml` polls via a `Quickshell.Io.Process` + `StdioCollector` on a
  `Timer`, so the read never blocks the UI thread; actions use a second,
  separate `Process` fired only from button `onClicked` handlers.
- `BarWidget.qml` just renders `StateModel.indicator()` and delegates panel
  lifecycle to `Panel.qml`, mirroring `hermarchy-agent`'s split.

## Validation

```sh
omarchy plugin validate ~/.config/omarchy/plugins/io.github.achr0me.whoisthat
qmllint ~/.config/omarchy/plugins/io.github.achr0me.whoisthat/BarWidget.qml
qmllint ~/.config/omarchy/plugins/io.github.achr0me.whoisthat/Panel.qml
omarchy plugin list --json
```
