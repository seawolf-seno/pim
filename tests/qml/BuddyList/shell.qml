// Checks BuddyList's rendering and intent, given fixed rows: a group
// header reads AIM style with its fold chevron, a buddy row shows its
// presence glyph beside its name and away message, a click reports that
// row's key, and no rows shows the empty state. Grouping and ordering are
// Buddies.js's, proven in tests/unit/buddies.test.cjs.
import QtQuick
import QtTest
import Quickshell
import "ui/components"
import "ui/lib/Buddies.js" as Buddies
import "Check.js" as Check

ShellRoot {
  id: root

  property var clicked: []

  FloatingWindow {
    id: win
    implicitWidth: 320
    implicitHeight: 400
    visible: true

    BuddyList {
      id: list
      anchors.fill: parent
      rows: Buddies.rows(Buddies.sections([
        { accountId: "x", remoteId: "okafor@pim.test", name: "Mr. Okafor", groups: ["Biology"], show: "away", status: "Teaching Biology · Block 2" },
        { accountId: "x", remoteId: "kosei30@pim.test", name: "Kenji Osei", groups: ["English 10"], show: "offline" }
      ]), ({ }))
      selectedKey: "buddy:Biology:x|okafor@pim.test"

      onActivated: key => root.clicked = root.clicked.concat([key])
    }
  }

  TestCase {
    id: t
    when: false
  }

  Timer {
    running: true
    interval: 50
    onTriggered: root.run()
  }

  // shown joins the text of every item under row that shows text.
  function shown(row: var): string {
    return Check.texts(row).map((item) => item.text).join("|");
  }

  // run checks the header and buddy rows, clicks both, then empties the list.
  function run(): bool {
    const header = Check.find(list, "buddyRow-group:Biology");
    const okafor = Check.find(list, "buddyRow-buddy:Biology:x|okafor@pim.test");
    const offline = Check.find(list, "buddyRow-group:Offline");
    if (!header || !okafor || !offline) return Check.fail("rows missing");

    const headerText = root.shown(header);
    if (headerText.indexOf("▾ Biology (1/1)") < 0) return Check.fail(`header reads ${headerText}`);
    const okaforText = root.shown(okafor);
    for (const want of ["◐", "Mr. Okafor", "Teaching Biology · Block 2"]) {
      if (okaforText.indexOf(want) < 0) return Check.fail(`buddy row lacks ${want}: ${okaforText}`);
    }
    if (root.shown(offline).indexOf("Offline (1)") < 0) return Check.fail("offline header");

    t.mouseClick(header, 10, header.height / 2);
    t.mouseClick(okafor, 10, okafor.height / 2);
    if (root.clicked.join(",") !== "group:Biology,buddy:Biology:x|okafor@pim.test") {
      return Check.fail(`clicked ${root.clicked}`);
    }

    list.rows = [];
    const empty = Check.find(list, "buddyEmptyState");
    if (!empty || !empty.visible) return Check.fail("no empty state for no rows");

    console.log("PASS BuddyList");
    Qt.exit(0);
    return true;
  }
}
