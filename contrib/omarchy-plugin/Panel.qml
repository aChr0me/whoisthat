import QtQuick
import QtQuick.Controls
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "StateModel.js" as StateModel

Panel {
  id: root
  moduleName: "io.github.achr0me.whoisthat"
  ipcTarget: "io.github.achr0me.whoisthat"
  manageIpc: false

  property var anchorItem: null
  property var hostWidget: null
  property var stateRecord: StateModel.fallback()
  property bool refreshing: false
  property bool actionRunning: false
  property int browseIndex: -1  // -1 = follow the currently active profile
  property bool pickerOpen: false
  // Frozen snapshot of the picker's row order, recomputed only when the
  // picker is opened (never while it stays open). Pinning/unpinning a row
  // while browsing updates its star in place via isPinned() but does NOT
  // reorder/rebuild this list -- that would destroy+recreate every
  // delegate and jump the container height on every click. The row order
  // (pinned-first grouping) only re-settles the next time you reopen.
  property var pickerRowsSnapshot: []
  property var pinnedIds: ({})  // map "groupId:profileId" -> true, persisted to pinned.json
  readonly property string pinnedPath: pluginDir + "/pinned.json"
  readonly property var barIdentity: hostWidget || root
  readonly property var indicator: StateModel.indicator(stateRecord)
  readonly property var presentation: StateModel.panel(stateRecord)
  readonly property int refreshSeconds: Math.max(1, Math.min(60, parseInt(setting("refreshIntervalSec", 3), 10) || 3))
  readonly property string pluginDir: Quickshell.env("HOME") + "/.config/omarchy/plugins/io.github.achr0me.whoisthat"
  readonly property string readerPath: pluginDir + "/scripts/whoisthat-state-read"
  readonly property string whoisthatBin: Quickshell.env("WHOISTHAT_BIN") || (Quickshell.env("HOME") + "/Projects/whoisthat/target/release/whoisthat")

  // --- whoisthat's own graphite-dark chrome, with 2 deliberate Omarchy ------
  // accent touches layered on top. Base chrome (bg/surface/text/border) is
  // the TUI's native palette straight from src/ui/theme.rs, so this still
  // reads as "whoisthat" first. Color.accent is used in exactly two spots
  // for desktop coherence: the focused/active border (borderActive) and the
  // connected-state glow ring (accentGlow). Semantic state colors are kept
  // as explicit literals, unchanged.
  readonly property QtObject palette: QtObject {
    readonly property color bg: "#121214"
    readonly property color surface: "#202125"
    readonly property color accent: Color.accent
    readonly property color accentGlow: Color.accent
    readonly property color text: "#f0f2f5"
    readonly property color textDim: "#969ba5"
    readonly property color textFaint: "#5f646e"
    readonly property color success: "#4ade80"
    readonly property color warn: "#fbbf24"
    readonly property color error: "#f87171"
    readonly property color disconnected: "#696e78"
    readonly property color border: "#2e3036"
    readonly property color borderActive: Color.accent
  }

  readonly property color foreground: palette.text
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property color signalColor: colorForSignal(indicator.signal)

  function colorForSignal(signal) {
    if (signal === "green") return palette.success
    if (signal === "amber") return palette.warn
    if (signal === "red") return palette.error
    return palette.disconnected
  }

  function refresh() {
    if (!stateProcess.running) {
      refreshing = true
      stateProcess.running = true
    }
  }

  function acceptOutput(raw) {
    try {
      stateRecord = StateModel.accept(JSON.parse(String(raw || "")))
    } catch (error) {
      stateRecord = StateModel.fallback()
    }
  }

  function open() {
    root.controller.show()
    refresh()
  }

  function close() { root.controller.hide() }
  function toggle() { root.opened ? close() : open() }

  function switchPanel(direction) {
    if (root.bar && typeof root.bar.switchPanelFrom === "function")
      return root.bar.switchPanelFrom(root.barIdentity, direction)
    return false
  }

  // Action buttons shell out through a short-lived Process, identical in
  // shape to the state-read Process below, so the UI thread never blocks.
  // These are user-click-only triggers; nothing here runs automatically.
  function runAction(args) {
    if (actionProcess.running) return
    actionRunning = true
    actionProcess.command = [root.whoisthatBin].concat(args)
    actionProcess.running = true
  }

  // Profile the CONNECT button targets: the explicit `defaultProfile` widget
  // setting, else the browsed row, else the active/first loaded profile.
  // Returns "" while the subscription list is not loaded, so the button is
  // disabled rather than sending a made-up group:index.
  function connectTarget() {
    var configured = setting("defaultProfile", "")
    if (typeof configured === "string" && configured !== "") return configured
    var profiles = root.presentation.profiles
    if (!profiles || profiles.length === 0) return ""
    var index = root.browseIndex >= 0 && root.browseIndex < profiles.length
      ? root.browseIndex
      : (root.presentation.activeProfileIndex >= 0 ? root.presentation.activeProfileIndex : 0)
    return root.pinKey(profiles[index])
  }

  function connect(target) { runAction(["connect", target, "--wait"]) }
  function disconnect() { runAction(["disconnect", "--wait"]) }
  function setMode(newMode) { runAction(["mode", newMode]) }
  function setKillSwitch(armed) { runAction(["settings", "set", "kill-switch=" + (armed ? "true" : "false")]) }

  // --- pinned-profiles persistence: a small JSON file inside this --------
  // plugin's own directory, loaded once on init and rewritten (atomically,
  // via FileView) whenever the pin set changes. Never touches the backend.
  function pinKey(profile) { return profile.groupId + ":" + profile.id }
  function isPinned(profile) { return root.pinnedIds[root.pinKey(profile)] === true }

  function togglePin(profile) {
    var key = root.pinKey(profile)
    var next = {}
    for (var existing in root.pinnedIds) next[existing] = true
    if (next[key]) delete next[key]
    else next[key] = true
    root.pinnedIds = next
    root.savePins()
    // Deliberately do NOT refresh pickerRowsSnapshot here -- see its
    // declaration. The star re-renders live (delegate reads isPinned()
    // directly) but row order/grouping stays put until next open.
  }

  // Recomputes the frozen row order. Call only when the picker transitions
  // closed -> open, never while it's already open (see pickerRowsSnapshot).
  function refreshPickerRows() {
    root.pickerRowsSnapshot = StateModel.pickerRows(root.presentation, root.pinnedIds)
  }

  function loadPins() {
    try {
      var raw = pinnedFile.text()
      var parsed = raw && raw.trim() !== "" ? JSON.parse(raw) : { pinned: [] }
      var ids = Array.isArray(parsed.pinned) ? parsed.pinned : []
      var map = {}
      for (var index = 0; index < ids.length; index++) {
        if (typeof ids[index] === "string") map[ids[index]] = true
      }
      root.pinnedIds = map
    } catch (error) {
      root.pinnedIds = {}
    }
  }

  function savePins() {
    var ids = Object.keys(root.pinnedIds)
    pinnedFile.setText(JSON.stringify({ pinned: ids }, null, 2))
  }

  FileView {
    id: pinnedFile
    path: root.pinnedPath
    printErrors: false
    blockLoading: true
    watchChanges: false
  }

  Component.onCompleted: root.loadPins()

  Process {
    id: stateProcess
    command: [root.readerPath]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.acceptOutput(text)
    }
    onExited: function(exitCode) {
      root.refreshing = false
      if (exitCode !== 0) root.stateRecord = StateModel.fallback()
    }
  }

  Process {
    id: actionProcess
    onExited: function(exitCode) {
      root.actionRunning = false
      root.refresh()
    }
  }

  Timer {
    interval: root.refreshSeconds * 1000
    running: true
    repeat: true
    triggeredOnStart: true
    onTriggered: root.refresh()
  }

  IpcHandler {
    target: root.ipcTarget
    function open(): void { root.open() }
    function close(): void { root.close() }
    function show(): void { root.open() }
    function hide(): void { root.close() }
    function toggle(): void { root.toggle() }
    function refresh(): string { root.refresh(); return "ok" }
    function state(): string { return root.stateRecord.status }
  }

  KeyboardPanel {
    id: panel
    anchorItem: root.anchorItem
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(380))
    contentHeight: panel.fittedContentHeight(frame.implicitHeight, Style.space(640))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onCloseRequested: root.close()
      onTabRequested: function(direction) { root.switchPanel(direction) }

      // --- panel frame: graphite-dark bg, matches TUI chrome -------------
      Rectangle {
        id: frame
        color: root.palette.bg
        clip: true
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        implicitHeight: content.implicitHeight + Style.space(28)
        radius: Style.space(6)
        border.width: 1
        border.color: root.palette.border

        Column {
          id: content
          anchors.left: parent.left
          anchors.right: parent.right
          anchors.top: parent.top
          anchors.margins: Style.space(14)
          spacing: Style.space(12)

          // --- header row: identity + overall status --------------------
          Row {
            width: parent.width
            spacing: Style.space(8)

            Column {
              id: identity
              spacing: Style.space(2)
              Text {
                text: "WHOISTHAT"
                color: root.palette.text
                font.family: root.fontFamily
                font.pixelSize: Style.font.title
                font.bold: true
                font.letterSpacing: 1.1
              }
              Text {
                text: "KILL-SWITCH PROXY"
                color: root.palette.textFaint
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                font.letterSpacing: 1.3
              }
            }

            Item { width: Math.max(0, parent.width - identity.implicitWidth - headerStatus.width - parent.spacing * 2); height: 1 }

            // Plain Item (not a Row) so both children can be truly
            // anchors.verticalCenter'd against one another -- Row is a
            // positioner and silently ignores/overrides per-child anchors,
            // which was the cause of the dot/text misalignment bug.
            Item {
              id: headerStatus
              anchors.verticalCenter: parent.verticalCenter
              width: dot.width + Style.space(6) + statusLabel.implicitWidth
              height: Math.max(dot.height, statusLabel.implicitHeight)

              Rectangle {
                id: dot
                width: Style.space(8)
                height: Style.space(8)
                radius: width / 2
                anchors.left: parent.left
                anchors.verticalCenter: parent.verticalCenter
                color: root.signalColor
                border.width: root.indicator.signal === "grey" ? 1 : (root.presentation.connected ? 2 : 0)
                border.color: root.indicator.signal === "grey" ? root.palette.textFaint : root.palette.accentGlow
                Behavior on color { ColorAnimation { duration: 160 } }
              }

              Text {
                id: statusLabel
                anchors.left: dot.right
                anchors.leftMargin: Style.space(6)
                anchors.verticalCenter: parent.verticalCenter
                text: root.presentation.connectionLabel
                color: root.signalColor
                font.family: root.fontFamily
                font.pixelSize: Style.font.bodySmall
                font.bold: true
                font.letterSpacing: 0.8
                Behavior on color { ColorAnimation { duration: 160 } }
              }
            }
          }

          Rectangle { width: parent.width; height: 1; color: root.palette.border }

          // --- main content area: bordered "surface" cards, TUI-style ---
          Grid {
            width: parent.width
            columns: 3
            columnSpacing: Style.space(8)
            rowSpacing: Style.space(8)

            StatCard {
              label: "STATUS"
              value: root.presentation.status
              accentColor: root.signalColor
            }
            StatCard {
              label: "MODE"
              value: root.presentation.mode
              accentColor: root.palette.accent
            }
            StatCard {
              label: "KILL-SWITCH"
              value: root.presentation.killSwitchLabel
              accentColor: StateModel.killSwitchColor(root.palette, root.stateRecord)
            }
          }

          Column {
            width: parent.width
            spacing: Style.space(6)
            InfoRow { label: "PROFILE"; value: root.presentation.profileName }
            InfoRow { label: "GROUP"; value: root.presentation.groupName }
            InfoRow { visible: root.presentation.connected || root.presentation.protocol !== "—"; label: "PROTOCOL"; value: root.presentation.protocol }
            InfoRow { visible: root.presentation.hasJitter; label: "JITTER"; value: root.presentation.jitterLabel }
            InfoRow { visible: root.presentation.hasLoss; label: "LOSS"; value: root.presentation.lossLabel }
            InfoRow { label: "THROUGHPUT"; value: root.presentation.rxHuman + " ↓ / " + root.presentation.txHuman + " ↑" }
          }

          Rectangle { width: parent.width; height: 1; color: root.palette.border }

          // --- profile picker: a scrollable inline list (not a one-at-a--
          // time cycler) showing every profile in the active group at
          // once. An inline expandable list was chosen over a nested
          // Popup because this panel is already hosted inside a
          // KeyboardPanel/Popup -- stacking a second Popup on top of it
          // is awkward for focus/keyboard routing in Quickshell's plugin
          // model, whereas an inline ListView just grows the existing
          // panel and reuses its focus scope. Pinned profiles are sorted
          // to a "PINNED" section at the top (see StateModel.pickerRows)
          // so frequent servers are reachable without scrolling past 47
          // entries. Connecting is still a deliberate, separate click.
          Column {
            width: parent.width
            spacing: Style.space(6)
            visible: root.presentation.hasProfiles

            Row {
              width: parent.width
              spacing: Style.space(8)

              Text {
                id: groupLabel
                text: "PROFILES IN GROUP (" + root.presentation.profiles.length + ")"
                color: root.palette.textFaint
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                font.letterSpacing: 1.3
                anchors.verticalCenter: parent.verticalCenter
              }

              // Flexible spacer: fills exactly what's left of the row after
              // the label, the active-profile text and the toggle button
              // have taken their space, so the button can never be pushed
              // past the frame's right edge and clipped away. A fixed
              // "parent.width - 220" guess here previously under-reserved
              // space for the active text + button, overflowing the Row by
              // ~150-180px and silently clipping the BROWSE button off-panel.
              Item {
                width: Math.max(0, parent.width - groupLabel.width - activeLabel.width
                  - pickerToggle.width - Style.space(8) * 3)
                height: 1
              }

              Text {
                id: activeLabel
                visible: !root.pickerOpen
                // Only show a real name once there is an actual active
                // profile (connected, with a valid index) -- otherwise
                // fall back to "—", matching the PROFILE/GROUP rows above.
                text: (root.presentation.connected && root.presentation.activeProfileIndex >= 0
                    && root.presentation.activeProfileIndex < root.presentation.profiles.length)
                  ? "ACTIVE: " + root.presentation.profiles[root.presentation.activeProfileIndex].name
                  : "ACTIVE: —"
                color: root.palette.text
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                font.bold: true
                elide: Text.ElideRight
                width: Math.min(implicitWidth, Style.space(140))
                maximumLineCount: 1
                anchors.verticalCenter: parent.verticalCenter
              }

              SmallButton {
                id: pickerToggle
                z: 5
                text: root.pickerOpen ? "CLOSE ▴" : "BROWSE ▾"
                anchors.verticalCenter: parent.verticalCenter
                onClicked: {
                  if (!root.pickerOpen) root.refreshPickerRows()
                  root.pickerOpen = !root.pickerOpen
                }
              }
            }

            // --- the picker itself: scrollable multi-row list ------------
            Rectangle {
              id: pickerFrame
              visible: root.pickerOpen
              width: parent.width
              height: Math.min(Style.space(260), pickerList.contentHeight + Style.space(4))
              radius: Style.space(5)
              color: root.palette.surface
              border.width: 1
              border.color: root.palette.border
              clip: true

              ListView {
                id: pickerList
                anchors.fill: parent
                anchors.margins: Style.space(2)
                clip: true
                boundsBehavior: Flickable.StopAtBounds
                model: root.pickerOpen ? root.pickerRowsSnapshot : []

                delegate: Loader {
                  width: pickerList.width
                  sourceComponent: modelData.isHeader ? pickerHeaderDelegate : pickerRowDelegate
                  property var rowData: modelData
                }

                Component {
                  id: pickerHeaderDelegate
                  Item {
                    width: pickerList.width
                    height: Style.space(20)
                    Text {
                      anchors.left: parent.left
                      anchors.leftMargin: Style.space(6)
                      anchors.verticalCenter: parent.verticalCenter
                      text: rowData.label
                      color: root.palette.textFaint
                      font.family: root.fontFamily
                      font.pixelSize: Style.font.caption
                      font.letterSpacing: 1.1
                      font.bold: true
                    }
                  }
                }

                Component {
                  id: pickerRowDelegate
                  Rectangle {
                    id: pickerRow
                    width: pickerList.width
                    height: Style.space(32)
                    color: rowData.active
                      ? Qt.rgba(root.palette.accent.r, root.palette.accent.g, root.palette.accent.b, 0.16)
                      : (rowArea.containsMouse ? root.palette.bg : "transparent")
                    border.width: rowData.active ? 1 : 0
                    border.color: root.palette.borderActive
                    radius: Style.space(3)

                    Row {
                      anchors.fill: parent
                      anchors.leftMargin: Style.space(6)
                      anchors.rightMargin: Style.space(6)
                      spacing: Style.space(8)

                      // Pin toggle -- marks/unmarks this profile as pinned
                      // and persists the whole pin set immediately.
                      SmallButton {
                        // Read live pin state directly (not the frozen
                        // rowData.pinned snapshot) so the star flips
                        // in-place immediately without touching row order.
                        text: root.isPinned(rowData.profile) ? "★" : "☆"
                        anchors.verticalCenter: parent.verticalCenter
                        onClicked: root.togglePin(rowData.profile)
                      }

                      Text {
                        width: Math.max(0, pickerRow.width - Style.space(100))
                        anchors.verticalCenter: parent.verticalCenter
                        text: rowData.profile.name + (rowData.active ? "  (ACTIVE)" : "")
                        color: rowData.active ? root.palette.accent : root.palette.text
                        font.family: root.fontFamily
                        font.pixelSize: Style.font.bodySmall
                        font.bold: rowData.active
                        elide: Text.ElideRight
                      }

                      SmallButton {
                        text: "USE"
                        enabled: !root.actionRunning && !rowData.active
                        anchors.verticalCenter: parent.verticalCenter
                        onClicked: {
                          root.browseIndex = rowData.index
                          root.connect(rowData.profile.groupId + ":" + rowData.profile.id)
                        }
                      }
                    }

                    MouseArea {
                      id: rowArea
                      anchors.fill: parent
                      hoverEnabled: true
                      acceptedButtons: Qt.NoButton
                    }
                  }
                }

                ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }
              }
            }
          }

          Text {
            text: "last poll " + root.presentation.observedAt
            color: root.palette.textFaint
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
          }

          Rectangle { width: parent.width; height: 1; color: root.palette.border }

          Text {
            text: "CONTROLS"
            color: root.palette.textFaint
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
            font.letterSpacing: 1.3
          }

          // --- footer / action row ---------------------------------------
          Column {
            width: parent.width
            spacing: Style.space(8)

            // Connect/disconnect — user-click only. Never triggered by this
            // plugin's own refresh/polling path.
            ActionButton {
              text: root.presentation.connected ? "DISCONNECT"
                : (root.connectTarget() === "" ? "NO PROFILES LOADED" : "CONNECT")
              enabled: !root.actionRunning
                && (root.presentation.connected || root.connectTarget() !== "")
              onClicked: {
                if (root.presentation.connected) root.disconnect()
                else root.connect(root.connectTarget())
              }
            }

            // Proxy/TUN mode toggle — user-click only.
            ActionButton {
              text: root.presentation.hasRealMode
                ? "SWITCH MODE (CURRENT: " + root.presentation.mode + ")"
                : "SWITCH MODE"
              enabled: !root.actionRunning
              onClicked: root.setMode(root.stateRecord.mode === "tun" ? "proxy" : "tun")
            }

            // Kill-switch arm/disarm — user-click only. Arming/disarming this
            // live can interrupt network traffic; never script-trigger it.
            ActionButton {
              text: root.presentation.killSwitchArmed === true ? "DISARM KILL-SWITCH" : "ARM KILL-SWITCH"
              enabled: !root.actionRunning
              onClicked: root.setKillSwitch(root.stateRecord.killSwitchArmed !== true)
            }

            Text {
              visible: root.actionRunning
              text: "RUNNING…"
              color: root.palette.textFaint
              font.family: root.fontFamily
              font.pixelSize: Style.font.caption
            }
          }
        }
      }
    }
  }

  // --- small "surface" card, same visual weight as the TUI traffic cards ---
  component StatCard: Rectangle {
    id: statCard
    property string label: ""
    property string value: ""
    property color accentColor: root.palette.accent
    width: (parent.width - Style.space(16)) / 3
    height: Style.space(56)
    radius: Style.space(5)
    color: root.palette.surface
    border.width: 1
    border.color: root.palette.border

    Column {
      anchors.centerIn: parent
      spacing: Style.space(4)

      Text {
        anchors.horizontalCenter: parent.horizontalCenter
        text: statCard.label
        color: root.palette.textFaint
        font.family: root.fontFamily
        font.pixelSize: Style.font.caption
        font.letterSpacing: 0.9
      }

      Text {
        anchors.horizontalCenter: parent.horizontalCenter
        text: statCard.value
        color: statCard.accentColor
        font.family: root.fontFamily
        font.pixelSize: Style.font.bodySmall
        font.bold: true
        elide: Text.ElideRight
      }
    }
  }

  component InfoRow: Row {
    id: infoRow
    property string label: ""
    property string value: ""
    width: parent.width
    spacing: Style.space(8)

    Text {
      text: infoRow.label
      color: root.palette.textFaint
      font.family: root.fontFamily
      font.pixelSize: Style.font.caption
      font.letterSpacing: 0.9
    }

    Item {
      width: Math.max(0, infoRow.width - infoRow.children[0].implicitWidth - infoRow.children[2].implicitWidth - infoRow.spacing * 2)
      height: 1
    }

    Text {
      width: Math.min(implicitWidth, infoRow.width * 0.68)
      text: infoRow.value
      color: root.palette.text
      font.family: root.fontFamily
      font.pixelSize: Style.font.bodySmall
      font.bold: true
      horizontalAlignment: Text.AlignRight
      elide: Text.ElideRight
    }
  }

  // --- compact pill button for the profile browser row, same chrome as --
  // ActionButton but sized for inline use next to the profile label.
  component SmallButton: Rectangle {
    id: smallButton
    property string text: ""
    property bool enabled: true
    signal clicked()
    width: label.implicitWidth + Style.space(16)
    height: Style.space(26)
    radius: Style.space(4)
    color: enabled ? (area.pressed ? root.palette.surface : "transparent") : "transparent"
    border.width: 1
    border.color: enabled ? (area.containsMouse ? root.palette.borderActive : root.palette.border) : root.palette.textFaint
    opacity: enabled ? 1.0 : 0.5

    Behavior on border.color { ColorAnimation { duration: 120 } }

    Text {
      id: label
      anchors.centerIn: parent
      text: smallButton.text
      color: root.palette.text
      font.family: root.fontFamily
      font.pixelSize: Style.font.caption
      font.bold: true
      font.letterSpacing: 0.4
    }

    MouseArea {
      id: area
      anchors.fill: parent
      hoverEnabled: true
      enabled: smallButton.enabled
      onClicked: smallButton.clicked()
    }
  }

  component ActionButton: Rectangle {
    id: actionButton
    property string text: ""
    property bool enabled: true
    signal clicked()
    width: parent.width
    height: Style.space(32)
    radius: Style.space(4)
    color: enabled ? (mouseArea.pressed ? root.palette.surface : "transparent") : "transparent"
    border.width: 1
    border.color: enabled ? (mouseArea.containsMouse ? root.palette.borderActive : root.palette.border) : root.palette.textFaint
    opacity: enabled ? 1.0 : 0.5

    Behavior on border.color { ColorAnimation { duration: 120 } }

    Text {
      anchors.centerIn: parent
      text: actionButton.text
      color: root.palette.text
      font.family: root.fontFamily
      font.pixelSize: Style.font.bodySmall
      font.bold: true
      font.letterSpacing: 0.6
    }

    MouseArea {
      id: mouseArea
      anchors.fill: parent
      hoverEnabled: true
      enabled: actionButton.enabled
      onClicked: actionButton.clicked()
    }
  }
}
