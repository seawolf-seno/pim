import QtQuick
import "../lib/Actions.js" as Actions
import "../lib/Buddies.js" as Buddies
import "../lib/Rpc.js" as Rpc

// Owns the buddy list: whether it shows in place of the conversation list,
// which of its groups are collapsed, and the keyboard cursor. Opening a
// buddy opens (or creates) the direct conversation through
// conversations.open, the same call the new-chat dialog makes.
//
// While the buddy list shows, it takes over the list's own cursor and open
// keys (j/k, g/G, Enter), so the conversation list's controller never
// moves a cursor nobody can see.
//
// QtObject rather than Item: it holds no child objects.
QtObject {
  id: root

  // service is the Service instance that owns the helper connection.
  property var service: null

  // showing is true while the buddy list replaces the conversation list.
  property bool showing: false

  // collapsed maps a group name to true while that group is folded.
  property var collapsed: ({})

  // selectedKey is the cursor's row, by key.
  property string selectedKey: ""

  // lastError is the safe text of the most recent request failure.
  property string lastError: ""

  // rows are the buddy list's rows: group headers and buddies.
  readonly property var rows: Buddies.rows(Buddies.sections(root.service ? root.service.buddies : []), root.collapsed)

  // hasBuddies is true once any account has a buddy list.
  readonly property bool hasBuddies: root.service !== null && root.service.buddies.length > 0

  // opened hands off a conversation the buddy list just opened. The panel
  // wires this to the conversation controller's open(conversation).
  signal opened(var conversation)

  // _listKeys are the list actions the buddy list takes over while shown.
  readonly property var _listKeys: ["cursor.down", "cursor.up", "cursor.top", "cursor.bottom", "chat.open"]

  // handles reports whether this controller owns action: its own, always,
  // and the list's cursor and open keys while the buddy list shows.
  function handles(action: string): bool {
    return Actions.owner(action) === "buddies" || (root.showing && root._listKeys.includes(action));
  }

  // run performs action, the only entry point a key router needs.
  function run(action: string): void {
    const handlers = {
      "buddies.toggle": () => root.toggle(),
      "cursor.down": () => root.select(Buddies.move(root.rows, root.selectedKey, 1)),
      "cursor.up": () => root.select(Buddies.move(root.rows, root.selectedKey, -1)),
      "cursor.top": () => root.select(root.rows.length > 0 ? root.rows[0].key : ""),
      "cursor.bottom": () => root.select(root.rows.length > 0 ? root.rows[root.rows.length - 1].key : ""),
      "chat.open": () => root.activate(root.selectedKey)
    };

    const handler = handlers[action];
    if (handler) handler();
  }

  // toggle shows or hides the buddy list, putting the cursor on its first
  // row when it has none yet.
  function toggle(): void {
    root.showing = !root.showing;
    if (root.showing && !Buddies.rowFor(root.rows, root.selectedKey)) {
      root.select(root.rows.length > 0 ? root.rows[0].key : "");
    }
  }

  // select moves the cursor to rowKey.
  function select(rowKey: string): void {
    root.selectedKey = rowKey;
  }

  // activate acts on a row: a group header folds or unfolds its group; a
  // buddy opens its conversation.
  function activate(rowKey: string): void {
    const row = Buddies.rowFor(root.rows, rowKey);
    if (!row) return;

    root.select(rowKey);
    if (row.kind === "group") root.toggleGroup(row.name);
    else root.openBuddy(row.accountId, row.remoteId);
  }

  // toggleGroup folds or unfolds the group named name.
  function toggleGroup(name: string): void {
    const next = Object.assign({}, root.collapsed);
    if (next[name]) delete next[name];
    else next[name] = true;
    root.collapsed = next;
  }

  // openBuddy opens the direct conversation with remoteId on accountId and
  // hands it off through opened(). The buddy list stays shown, as AIM's
  // did, so the next buddy is one key away.
  function openBuddy(accountId: string, remoteId: string): void {
    if (!root.service) return;

    root.service.request("conversations.open", { accountId: accountId, contactId: remoteId }, function(error, result) {
      if (error) { root.lastError = Rpc.errorText(error); return; }
      root.lastError = "";
      root.opened(result);
    });
  }
}
