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
  readonly property color signalColor: panelLoader.item
    ? panelLoader.item.signalColor
    : "#696e78"
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
    fixedWidth: signalRow.implicitWidth + Style.space(17)
    tooltipText: "WhoisThat status"

    onPressed: function(buttonCode) {
      if (buttonCode === Qt.MiddleButton) root.refresh()
      else root.toggle()
    }

    Row {
      id: signalRow
      anchors.centerIn: parent
      spacing: Style.space(5)

      Text {
        text: root.indicator.label
        color: button.foreground
        font.family: button.fontFamily
        font.pixelSize: Style.font.bodySmall
        font.bold: true
        font.letterSpacing: 0.8
        renderType: Text.NativeRendering
      }

      Rectangle {
        id: stateDot
        width: Style.space(7)
        height: Style.space(7)
        radius: width / 2
        anchors.verticalCenter: parent.verticalCenter
        color: root.signalColor
        border.width: root.indicator.signal === "grey" ? 1 : 0
        border.color: button.foreground

        Behavior on color { ColorAnimation { duration: 160 } }
      }

      Text {
        text: root.indicator.suffix
        color: root.signalColor
        font.family: button.fontFamily
        font.pixelSize: Style.font.bodySmall
        font.bold: root.indicator.signal !== "grey"
        font.letterSpacing: 0.4
        renderType: Text.NativeRendering

        Behavior on color { ColorAnimation { duration: 160 } }
      }
    }
  }
}
