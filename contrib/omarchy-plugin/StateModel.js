.pragma library

// Defensive validate-with-fallback parser for whoisthat-state-read output,
// following the same pattern as hermarchy-agent's StateModel.js.

function fallback() {
  return {
    schemaVersion: 2,
    observedAt: "",
    coreReachable: false,
    connected: false,
    status: "unavailable",
    mode: "",
    killSwitchArmed: null,
    rxHuman: "",
    txHuman: "",
    groupId: 0,
    profileId: 0,
    profileName: "",
    groupName: "",
    protocol: "",
    jitterMs: 0,
    lossPct: 0,
    tested: false,
    address: "",
    port: 0,
    profiles: []
  }
}

function isObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value)
}

function hasExactKeys(value, expected) {
  if (!isObject(value)) return false
  var actual = Object.keys(value).sort()
  var wanted = expected.slice().sort()
  if (actual.length !== wanted.length) return false
  for (var index = 0; index < wanted.length; index++) {
    if (actual[index] !== wanted[index]) return false
  }
  return true
}

function isNullableBool(value) {
  return value === null || typeof value === "boolean"
}

// Validates the exact shape emitted by scripts/whoisthat-state-read.
// Anything unexpected (missing keys, wrong types, extra keys) is rejected
// and the caller gets a safe, fully-disconnected fallback record instead.
function accept(value) {
  var topKeys = ["schemaVersion", "observedAt", "coreReachable", "connected", "status", "mode", "killSwitchArmed",
    "rxHuman", "txHuman", "groupId", "profileId", "profileName", "groupName", "protocol", "jitterMs", "lossPct",
    "tested", "address", "port", "profiles"]
  if (!hasExactKeys(value, topKeys)) return fallback()
  if (value.schemaVersion !== 2) return fallback()
  if (typeof value.observedAt !== "string") return fallback()
  if (typeof value.coreReachable !== "boolean") return fallback()
  if (typeof value.connected !== "boolean") return fallback()
  if (typeof value.status !== "string") return fallback()
  if (typeof value.mode !== "string") return fallback()
  if (!isNullableBool(value.killSwitchArmed)) return fallback()
  if (typeof value.rxHuman !== "string") return fallback()
  if (typeof value.txHuman !== "string") return fallback()
  if (typeof value.groupId !== "number") return fallback()
  if (typeof value.profileId !== "number") return fallback()
  if (typeof value.profileName !== "string") return fallback()
  if (typeof value.groupName !== "string") return fallback()
  if (typeof value.protocol !== "string") return fallback()
  if (typeof value.jitterMs !== "number") return fallback()
  if (typeof value.lossPct !== "number") return fallback()
  if (typeof value.tested !== "boolean") return fallback()
  if (typeof value.address !== "string") return fallback()
  if (typeof value.port !== "number") return fallback()
  if (!Array.isArray(value.profiles)) return fallback()

  // A disconnected agent cannot plausibly report itself connected.
  if (!value.coreReachable && value.connected) return fallback()

  return value
}

// Bar-icon signal color key. Rule (per task spec):
//   connected + armed   -> green
//   connected + unarmed -> amber
//   disconnected        -> red if kill-switch known unarmed, grey otherwise
function signalFor(state) {
  if (!state.coreReachable) return "grey"
  if (state.connected) {
    return state.killSwitchArmed === true ? "green" : "amber"
  }
  // Disconnected: red flags an exposed/unarmed kill-switch, grey is neutral idle.
  if (state.killSwitchArmed === false) return "red"
  return "grey"
}

function indicator(value) {
  var state = accept(value)
  var signal = signalFor(state)
  var suffix = state.connected ? "●" : "·"
  return { label: "WT", suffix: suffix, signal: signal }
}

function display(value, fallbackValue) {
  if (value === undefined || value === null) return fallbackValue
  var text = String(value).trim()
  return text === "" ? fallbackValue : text
}

function upper(value, fallbackValue) {
  return display(value, fallbackValue).toUpperCase()
}

// Genuinely-unknown (null, not-yet-queried) reads as the calm at-rest
// default "DISARMED" in neutral grey, not amber "UNKNOWN" -- amber/warn is
// reserved for an actual actionable warning (connected-but-unarmed).
function killSwitchLabel(value) {
  if (value === true) return "ARMED"
  return "DISARMED"
}

// Color key for the kill-switch card. Mirrors the CLI's semantics:
//   armed              -> success (green)
//   explicitly unarmed  -> warn (amber) only when actually connected
//   unknown / at rest   -> disconnected (grey)
function killSwitchColor(palette, state) {
  if (state.killSwitchArmed === true) return palette.success
  if (state.connected && state.killSwitchArmed === false) return palette.warn
  return palette.disconnected
}

function connectionLabel(state) {
  if (!state.coreReachable) return "CORE OFFLINE"
  return state.connected ? "CONNECTED" : "DISCONNECTED"
}

function panel(value) {
  var state = accept(value)
  var hasJitter = state.jitterMs > 0
  var hasLoss = state.tested && state.lossPct > 0
  var profiles = state.profiles.filter(function(p) {
    return p && typeof p.id === "number" && typeof p.name === "string"
  })
  var activeIndex = -1
  for (var index = 0; index < profiles.length; index++) {
    if (profiles[index].id === state.profileId && profiles[index].groupId === state.groupId) {
      activeIndex = index
      break
    }
  }
  return {
    coreReachable: state.coreReachable,
    connected: state.connected,
    connectionLabel: connectionLabel(state),
    status: upper(state.status, "UNKNOWN"),
    mode: upper(state.mode, "PROXY"),
    hasRealMode: typeof state.mode === "string" && state.mode.trim() !== "",
    killSwitchArmed: state.killSwitchArmed,
    killSwitchLabel: killSwitchLabel(state.killSwitchArmed),
    rxHuman: display(state.rxHuman, "—"),
    txHuman: display(state.txHuman, "—"),
    observedAt: display(state.observedAt, "—"),
    profileName: display(state.profileName, state.connected ? "UNKNOWN" : "—"),
    groupName: display(state.groupName, "—"),
    protocol: state.protocol ? state.protocol.toUpperCase() : "—",
    hasProfileDetail: state.profileName !== "" || state.groupName !== "",
    hasJitter: hasJitter,
    jitterLabel: hasJitter ? ("±" + state.jitterMs + " ms") : "",
    hasLoss: hasLoss,
    lossLabel: hasLoss ? (state.lossPct + "%") : "",
    profiles: profiles,
    activeProfileIndex: activeIndex,
    hasProfiles: profiles.length > 0
  }
}

// Picks the next profile in the browser list, wrapping around. Pure
// read-only selection helper -- does NOT connect to anything by itself.
function nextProfile(presentation, currentIndex) {
  if (!presentation.hasProfiles) return null
  var count = presentation.profiles.length
  var base = currentIndex >= 0 ? currentIndex : (presentation.activeProfileIndex >= 0 ? presentation.activeProfileIndex : -1)
  var next = (base + 1) % count
  return { index: next, profile: presentation.profiles[next] }
}

// Builds the flat row list the picker's ListView renders: pinned profiles
// first (in group order), then everything else, each row tagged with
// whether it's pinned/active so the delegate needn't recompute it. Pure,
// read-only -- does not touch the backend or the pin store.
function pickerRows(presentation, pinnedIds) {
  var rows = []
  var pinned = []
  var rest = []
  for (var index = 0; index < presentation.profiles.length; index++) {
    var profile = presentation.profiles[index]
    var key = profile.groupId + ":" + profile.id
    var row = {
      index: index,
      profile: profile,
      pinned: pinnedIds[key] === true,
      active: index === presentation.activeProfileIndex
    }
    if (row.pinned) pinned.push(row); else rest.push(row)
  }
  if (pinned.length > 0) {
    rows.push({ isHeader: true, label: "PINNED" })
    rows = rows.concat(pinned)
    rows.push({ isHeader: true, label: "ALL PROFILES" })
  }
  return rows.concat(rest)
}
