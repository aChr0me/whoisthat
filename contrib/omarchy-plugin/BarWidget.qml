import QtQuick
import qs.Commons
import qs.Ui
import "StateModel.js" as StateModel

BarWidget {
  id: root
  moduleName: "io.github.achr0me.whoisthat"

  readonly property var indicator: panelLoader.item
    ? panelLoader.item.indicator
    : StateModel.indicator(null)
  // Graphite-dark "disconnected" grey (src/ui/theme.rs) as the fallback
  // before the panel has loaded, so the bar dot never flashes a mismatched hue.
  readonly property color localSignalColor: ({
    "green": "#7ec699",
    "amber": "#e6b566",
    "red": "#e06c75",
    "grey": "#8d8d8d"
  })[indicator.signal] || "#8d8d8d"
  readonly property color signalColor: panelLoader.item
    ? panelLoader.item.signalColor
    : localSignalColor
  readonly property bool opened: panelLoader.item ? panelLoader.item.opened === true : false
  readonly property bool popoutSwitchClosing: panelLoader.item ? panelLoader.item.popoutSwitchClosing === true : false

  function injectPanel() {
    var target = panelLoader.item
    if (!target) return
    if ("bar" in target) target.bar = root.bar
    if ("settings" in target) target.settings = root.settings
    if ("anchorItem" in target) target.anchorItem = button
    if ("hostWidget" in target) target.hostWidget = root
  }

  function refresh() {
    if (panelLoader.item && panelLoader.item.refresh) panelLoader.item.refresh()
  }

  function open() { if (panelLoader.item) panelLoader.item.open() }
  function close() { if (panelLoader.item) panelLoader.item.close() }
  function toggle() { if (panelLoader.item) panelLoader.item.toggle() }
  function closeForPopoutSwitch() { if (panelLoader.item) panelLoader.item.closeForPopoutSwitch() }

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight
  onBarChanged: injectPanel()
  onSettingsChanged: injectPanel()

  Loader {
    id: panelLoader
    active: true
    source: Qt.resolvedUrl("Panel.qml")
    visible: false
    onLoaded: {
      root.injectPanel()
      Qt.callLater(root.injectPanel)
    }
  }

  WidgetButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: ""
    hasVisualContent: true
    fixedWidth: signalRow.implicitWidth + Style.space(11)
    tooltipText: "WhoisThat status"

    onPressed: function(buttonCode) {
      if (buttonCode === Qt.MiddleButton) root.refresh()
      else root.toggle()
    }

    Row {
      id: signalRow
      anchors.verticalCenter: button.bar ? button.bar.verticalCenter : parent.verticalCenter
      anchors.horizontalCenter: parent.horizontalCenter
      spacing: Style.space(5)

      Text {
        id: stateLabel
        text: root.indicator.label
        color: button.foreground
        font.family: button.fontFamily
        font.pixelSize: Style.font.bodySmall
        font.bold: true
        font.letterSpacing: 0.8
        renderType: Text.NativeRendering
      }

      // Single signal dot. A glyph rather than a Rectangle: it inherits the
      // label's font and baseline, so it cannot drift out of line and needs no
      // size or anchor maths.
      Text {
        id: stateDot
        text: "\u25cf"
        color: root.signalColor
        font.family: button.fontFamily
        font.pixelSize: Style.space(11)
        anchors.verticalCenter: stateLabel.verticalCenter
        renderType: Text.NativeRendering

        Behavior on color { ColorAnimation { duration: 160 } }
      }
    }
  }
}
