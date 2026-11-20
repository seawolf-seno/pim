pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import qs.Ui as Ui
import "../theme"
import "../lib/Limits.js" as Limits
import "../lib/Paste.js" as Paste

// The fields for signing in to a school's own chat server: address,
// password, and optionally the server to connect to and a certificate
// file to trust. AccountSetup keeps what is typed, since it destroys this
// form whenever the window is inactive (see docs/decisions.md). Tab moves
// between fields; Enter in any field submits.
ColumnLayout {
  id: root

  // address, password, server and caFile are the drafts to show.
  property string address: ""
  property string password: ""
  property string server: ""
  property string caFile: ""
  // routeKey intercepts key presses before a field handles them; see
  // Composer.qml for why this is a function property, not a signal.
  property var routeKey: null

  // edited reports a field's new text, by field name.
  signal edited(string field, string text)
  // focused reports which field gained focus, or "" when one lost it.
  signal focused(string field)
  // submitted asks to sign in with what is typed.
  signal submitted()

  // focusField moves keyboard focus to the named field, or the address
  // when the name is unknown.
  function focusField(name: string): void {
    const fields = { password: passwordField, server: serverField, caFile: caFileField };
    (fields[name] || addressField).forceActiveFocus();
  }

  spacing: Theme.spacing.md

  // A text field with the paste and focus handling every setup field
  // shares; field names which draft it edits.
  component Field: Ui.TextField {
    id: field

    // name is the draft this field edits, reported with every change.
    property string name: ""

    Layout.fillWidth: true
    maximumLength: Limits.SERVER_FIELD
    // No context menu: its Paste would bypass the helper's size-limited clipboard read.
    ContextMenu.menu: null
    Keys.priority: Keys.BeforeItem
    // Route every paste key, even if rebound, through the helper's bounded paste.
    Keys.onPressed: event => {
      if (Paste.isPasteKey(event.key, event.modifiers)) {
        if (root.routeKey) root.routeKey(event.key, event.modifiers, event.text, true);
        event.accepted = true;
        return;
      }
      if (root.routeKey && root.routeKey(event.key, event.modifiers, event.text)) event.accepted = true
    }
    onAccepted: root.submitted()
    onTextChanged: root.edited(field.name, text)
    onActiveFocusChanged: root.focused(activeFocus ? field.name : "")

    MiddleClickGuard {}
  }

  Field {
    id: addressField
    objectName: "serverAddressField"
    name: "address"
    text: root.address
    placeholderText: "Address, like name@school.org"
    KeyNavigation.tab: passwordField
  }

  Field {
    id: passwordField
    objectName: "serverPasswordField"
    name: "password"
    text: root.password
    placeholderText: "Password"
    password: true
    KeyNavigation.tab: serverField
  }

  Field {
    id: serverField
    objectName: "serverHostField"
    name: "server"
    text: root.server
    placeholderText: "Server, like 192.168.1.20 (optional)"
    KeyNavigation.tab: caFileField
  }

  Field {
    id: caFileField
    objectName: "serverCAFileField"
    name: "caFile"
    text: root.caFile
    placeholderText: "Certificate file, full path (optional)"
    maximumLength: Limits.FILE_PATH
    KeyNavigation.tab: addressField
  }
}
