.pragma library

// The buddy list: how buddies from services with presence (XMPP) are
// grouped, ordered and shown. Like AIM, each group lists its buddies who
// are online, and everyone offline is gathered once in an "Offline" group
// at the bottom. A buddy in several groups appears in each.

// OFFLINE_GROUP names the group that gathers every offline buddy.
var OFFLINE_GROUP = 'Offline';

// UNGROUPED names the group for a buddy listed under no group.
var UNGROUPED = 'Buddies';

// PRESENCE describes each presence: its glyph (never shown without its
// label, so colour is never the only cue), its label and its order.
var PRESENCE = {
  available: { glyph: '●', label: 'Available', rank: 0 },
  away: { glyph: '◐', label: 'Away', rank: 1 },
  busy: { glyph: '⊘', label: 'Busy', rank: 2 },
  offline: { glyph: '○', label: 'Offline', rank: 3 }
};

// presence returns the description of show, treating anything unknown as
// offline.
function presence(show) {
  return PRESENCE[show] || PRESENCE.offline;
}

// key identifies a buddy across accounts.
function key(buddy) {
  return `${buddy.accountId}|${buddy.remoteId}`;
}

// upsert returns buddies with buddy added, or replacing the one with the
// same key, leaving the original array untouched.
function upsert(buddies, buddy) {
  const list = buddies || [];
  const id = key(buddy);
  const at = list.findIndex((b) => key(b) === id);
  if (at < 0) return list.concat([buddy]);

  const next = list.slice();
  next[at] = buddy;
  return next;
}

// withoutAccount returns buddies without those of accountId.
function withoutAccount(buddies, accountId) {
  return (buddies || []).filter((b) => b.accountId !== accountId);
}

// compare orders buddies by presence, then name, then key.
function compare(a, b) {
  const byPresence = presence(a.show).rank - presence(b.show).rank;
  if (byPresence !== 0) return byPresence;

  const byName = (a.name || '').localeCompare(b.name || '', undefined, { sensitivity: 'base' });
  return byName !== 0 ? byName : key(a).localeCompare(key(b));
}

// groupsOf returns the groups an online buddy is listed under.
function groupsOf(buddy) {
  const groups = (buddy.groups || []).filter((g) => g);
  return groups.length > 0 ? groups : [UNGROUPED];
}

// sections groups buddies for the list: one section per group of online
// buddies, alphabetical, then the Offline section. Each section carries
// its name, how many of its buddies are online, and its sorted buddies.
function sections(buddies) {
  const online = new Map();
  const offline = [];
  for (const buddy of buddies || []) {
    if (presence(buddy.show) === PRESENCE.offline) { offline.push(buddy); continue; }
    for (const group of groupsOf(buddy)) {
      if (!online.has(group)) online.set(group, []);
      online.get(group).push(buddy);
    }
  }

  const names = Array.from(online.keys()).sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }));
  const result = names.map((name) => section(name, online.get(name)));
  if (offline.length > 0) result.push(section(OFFLINE_GROUP, offline));
  return result;
}

// section builds one section from its buddies.
function section(name, buddies) {
  const sorted = buddies.slice().sort(compare);
  const onlineCount = sorted.filter((b) => presence(b.show) !== PRESENCE.offline).length;
  return { name: name, online: onlineCount, total: sorted.length, buddies: sorted };
}

// rows flattens sections into the list's rows: a header row per section
// and, unless the section is collapsed, a row per buddy. Every row has a
// unique key, so the cursor survives a reorder.
function rows(sectionList, collapsed) {
  const closed = collapsed || {};
  const result = [];
  for (const s of sectionList || []) {
    const isClosed = closed[s.name] === true;
    result.push({ kind: 'group', key: `group:${s.name}`, name: s.name, online: s.online, total: s.total, collapsed: isClosed });
    if (isClosed) continue;
    for (const b of s.buddies) {
      result.push({ kind: 'buddy', key: `buddy:${s.name}:${key(b)}`, accountId: b.accountId, remoteId: b.remoteId,
        name: b.name, show: b.show || 'offline', status: b.status || '' });
    }
  }
  return result;
}

// headerLabel is a group header's text, AIM style: "Biology (2/4)", or
// just the count for the Offline group.
function headerLabel(row) {
  if (row.name === OFFLINE_GROUP) return `${row.name} (${row.total})`;
  return `${row.name} (${row.online}/${row.total})`;
}

// detail is a buddy row's second line: the away message, else the
// presence label when not simply available.
function detail(row) {
  if (row.status) return row.status;
  return row.show === 'available' ? '' : presence(row.show).label;
}

// move returns the key of the row delta steps from currentKey, clamped to
// the ends; the first row when currentKey is not found.
function move(rowList, currentKey, delta) {
  if (!rowList || rowList.length === 0) return '';

  const at = rowList.findIndex((r) => r.key === currentKey);
  if (at < 0) return rowList[0].key;
  return rowList[Math.max(0, Math.min(rowList.length - 1, at + delta))].key;
}

// rowFor returns the row with rowKey, or null.
function rowFor(rowList, rowKey) {
  return (rowList || []).find((r) => r.key === rowKey) || null;
}
