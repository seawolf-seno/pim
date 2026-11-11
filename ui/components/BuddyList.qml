pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import qs.Commons
import "../theme"
import "../lib/Buddies.js" as Buddies

// The buddy list, AIM style: a header per group with how many of its
// buddies are online, the online buddies under it, and everyone offline in
// one group at the bottom. Presence is a glyph (filled, half, barred or
// hollow circle) beside the name and away message, so it never rests on
// colour alone. A view only: it takes rows from Buddies.rows and reports
// intent through signals.
Item {
  id: root

  // rows are the list's rows, from Buddies.rows: group headers and buddies.
  property var rows: []
  // selectedKey is the cursor's row, by key.
  property string selectedKey: ""

  // activated fires when a row is clicked: a header folds or unfolds its
  // group, a buddy opens its conversation.
  signal activated(string key)

  // positionOn scrolls the row with key into view.
  function positionOn(key: string): void {
    const index = (root.rows || []).findIndex((r) => r.key === key);
    if (index >= 0) list.positionViewAtIndex(index, ListView.Contain);
  }

  implicitWidth: Style.space(260)
  implicitHeight: Style.space(400)

  onSelectedKeyChanged: root.positionOn(root.selectedKey)

  ListView {
    id: list
    objectName: "buddyListView"
    anchors.fill: parent
    clip: true
    visible: root.rows.length > 0
    model: root.rows
    delegate: rowDelegate
    ScrollBar.vertical: ScrollBar {}
  }

  Text {
    objectName: "buddyEmptyState"
    visible: root.rows.length === 0
    anchors.centerIn: parent
    width: parent.width - Theme.spacing.xl * 2
    horizontalAlignment: Text.AlignHCenter
    wrapMode: Text.WordWrap
    textFormat: Text.PlainText
    text: "No buddies yet · add an XMPP account, such as your school's"
    color: Util.alpha(Theme.foreground, 0.6)
    font.family: Theme.font.family
    font.pixelSize: Theme.font.body
  }

  Component {
    id: rowDelegate

    Item {
      id: wrapper

      required property var modelData

      readonly property bool isGroup: wrapper.modelData.kind === "group"
      readonly property bool selected: wrapper.modelData.key === root.selectedKey

      objectName: "buddyRow-" + wrapper.modelData.key
      width: list.width
      height: wrapper.isGroup ? Style.space(30) : Style.space(44)

      HighlightFill {
        id: fill
        selected: wrapper.selected
        hovered: hover.containsMouse
      }

      Rectangle {
        anchors.fill: parent
        color: fill.color
      }

      // Selection bar: a 2px accent stripe on the left, as on a chat row.
      Rectangle {
        visible: wrapper.selected
        anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
        width: Style.space(2)
        color: Theme.accent
      }

      MouseArea {
        id: hover
        anchors.fill: parent
        hoverEnabled: true
        onClicked: root.activated(wrapper.modelData.key)
      }

      Loader {
        anchors { fill: parent; leftMargin: Theme.spacing.md; rightMargin: Theme.spacing.md }
        sourceComponent: wrapper.isGroup ? groupHeader : buddyRow
      }

      Component {
        id: groupHeader

        Text {
          objectName: "groupHeader"
          verticalAlignment: Text.AlignVCenter
          elide: Text.ElideRight
          textFormat: Text.PlainText
          text: (wrapper.modelData.collapsed ? "▸ " : "▾ ") + Buddies.headerLabel(wrapper.modelData)
          color: Theme.muted
          font { family: Theme.font.family; pixelSize: Theme.font.bodySmall; bold: true }
        }
      }

      Component {
        id: buddyRow

        RowLayout {
          spacing: Theme.spacing.sm

          Text {
            objectName: "presenceGlyph"
            Layout.alignment: Qt.AlignVCenter
            textFormat: Text.PlainText
            text: Buddies.presence(wrapper.modelData.show).glyph
            color: wrapper.modelData.show === "available" ? Theme.accent
              : wrapper.modelData.show === "busy" ? Theme.urgent : Theme.muted
            font { family: Theme.font.family; pixelSize: Theme.font.body }
          }

          ColumnLayout {
            Layout.fillWidth: true
            Layout.alignment: Qt.AlignVCenter
            spacing: 0

            Text {
              objectName: "buddyName"
              Layout.fillWidth: true
              elide: Text.ElideRight
              textFormat: Text.PlainText
              text: wrapper.modelData.name
              color: wrapper.modelData.show === "offline" ? Theme.muted : Theme.foreground
              font { family: Theme.font.family; pixelSize: Theme.font.body }
            }

            Text {
              objectName: "buddyDetail"
              Layout.fillWidth: true
              visible: text.length > 0
              elide: Text.ElideRight
              textFormat: Text.PlainText
              text: Buddies.detail(wrapper.modelData)
              color: Theme.muted
              font { family: Theme.font.family; pixelSize: Theme.font.bodySmall; italic: true }
            }
          }
        }
      }
    }
  }
}
