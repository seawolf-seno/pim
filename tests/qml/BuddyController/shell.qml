// Checks BuddyController's wiring: Ctrl+B's action is always its own and
// the list's cursor keys only while the buddy list shows; toggling puts
// the cursor on the first row; the keys move it; opening a group folds it;
// opening a buddy asks conversations.open for that buddy and hands the
// answer to opened(). Grouping and cursor arithmetic are Buddies.js's,
// proven in tests/unit/buddies.test.cjs.
import QtQuick
import Quickshell
import "ui/controllers"
import "Check.js" as Check

ShellRoot {
  id: root

  property var openedConversation: null

  // service is a scripted double: buddies are fixed, and request() keeps
  // the last call so the test can answer it.
  QtObject {
    id: service

    property var buddies: [
      { accountId: "x", remoteId: "okafor@pim.test", name: "Mr. Okafor", groups: ["Biology"], show: "away", status: "" },
      { accountId: "x", remoteId: "jmarchetti29@pim.test", name: "Jules Marchetti", groups: ["Biology"], show: "available" }
    ]
    property string lastMethod: ""
    property var lastParams: null
    property var lastCallback: null

    function request(method: string, params: var, callback: var): void {
      service.lastMethod = method;
      service.lastParams = params;
      service.lastCallback = callback;
    }
  }

  BuddyController {
    id: buddies
    service: service

    onOpened: conversation => root.openedConversation = conversation
  }

  Timer {
    running: true
    interval: 0
    onTriggered: root.run()
  }

  // run drives the controller through its actions and checks each effect.
  function run(): bool {
    if (!buddies.handles("buddies.toggle")) return Check.fail("does not own buddies.toggle");
    if (buddies.handles("cursor.down")) return Check.fail("took the list's keys while hidden");

    buddies.run("buddies.toggle");
    if (!buddies.showing || buddies.selectedKey !== "group:Biology") return Check.fail(`after toggle: ${buddies.showing} ${buddies.selectedKey}`);
    if (!buddies.handles("cursor.down") || !buddies.handles("chat.open")) return Check.fail("did not take the list's keys while shown");

    buddies.run("cursor.down");
    if (buddies.selectedKey !== "buddy:Biology:x|jmarchetti29@pim.test") return Check.fail(`cursor.down → ${buddies.selectedKey}`);
    buddies.run("cursor.bottom");
    if (buddies.selectedKey !== "buddy:Biology:x|okafor@pim.test") return Check.fail(`cursor.bottom → ${buddies.selectedKey}`);

    buddies.run("chat.open");
    if (service.lastMethod !== "conversations.open" || service.lastParams.contactId !== "okafor@pim.test" || service.lastParams.accountId !== "x") {
      return Check.fail(`chat.open asked ${service.lastMethod} ${JSON.stringify(service.lastParams)}`);
    }
    service.lastCallback(null, { id: "conv-okafor" });
    if (!root.openedConversation || root.openedConversation.id !== "conv-okafor") return Check.fail("opened() not called with the answer");

    buddies.run("cursor.top");
    buddies.run("chat.open");
    if (buddies.rows.length !== 1 || !buddies.rows[0].collapsed) return Check.fail(`folding left ${buddies.rows.length} rows`);

    buddies.run("buddies.toggle");
    if (buddies.showing || buddies.handles("cursor.down")) return Check.fail("still showing after the second toggle");

    console.log("PASS BuddyController");
    Qt.exit(0);
    return true;
  }
}
