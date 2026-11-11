'use strict';

const { test } = require('node:test');
const assert = require('node:assert');
const { load } = require('./load.cjs');

const Buddies = load('lib/Buddies.js');

// The fictional school's buddies, as one student's account sees them.
const okafor = { accountId: 'x', remoteId: 'okafor@pim.test', name: 'Mr. Okafor', groups: ['Biology'], show: 'away', status: 'Teaching Biology · Block 2' };
const jules = { accountId: 'x', remoteId: 'jmarchetti29@pim.test', name: 'Jules Marchetti', groups: ['Biology', 'English 10'], show: 'available' };
const lena = { accountId: 'x', remoteId: 'lvance29@pim.test', name: 'Lena Vance', groups: ['Biology'], show: 'busy' };
const kenji = { accountId: 'x', remoteId: 'kosei30@pim.test', name: 'Kenji Osei', groups: ['English 10'], show: 'offline' };
const pell = { accountId: 'x', remoteId: 'pell@pim.test', name: 'Dr. Pell', groups: [], show: 'available' };

test('presence: describes each show, unknown as offline', () => {
  assert.deepEqual(Buddies.presence('away'), { glyph: '◐', label: 'Away', rank: 1 });
  assert.strictEqual(Buddies.presence('chat').label, 'Offline');
  assert.strictEqual(Buddies.presence(undefined).glyph, '○');
});

test('upsert: adds a new buddy and replaces an existing one, without mutating', () => {
  const one = Buddies.upsert([], okafor);
  const two = Buddies.upsert(one, jules);
  const back = Buddies.upsert(two, Object.assign({}, okafor, { show: 'available', status: '' }));

  assert.strictEqual(one.length, 1);
  assert.strictEqual(two.length, 2);
  assert.strictEqual(back.length, 2);
  assert.strictEqual(back[0].show, 'available');
  assert.strictEqual(two[0].show, 'away');
  assert.strictEqual(Buddies.upsert(null, okafor).length, 1);
});

test('withoutAccount: drops one account\'s buddies', () => {
  const other = Object.assign({}, okafor, { accountId: 'y' });
  assert.deepEqual(Buddies.withoutAccount([okafor, other], 'x').map((b) => b.accountId), ['y']);
  assert.deepEqual(Buddies.withoutAccount(null, 'x'), []);
});

test('sections: online buddies under each of their groups, alphabetical, then Offline', () => {
  const result = Buddies.sections([kenji, okafor, jules, lena, pell]);

  assert.deepEqual(result.map((s) => s.name), ['Biology', 'Buddies', 'English 10', 'Offline']);
  assert.deepEqual(result[0].buddies.map((b) => b.name), ['Jules Marchetti', 'Mr. Okafor', 'Lena Vance']);
  assert.strictEqual(result[0].online, 3);
  assert.deepEqual(result[1].buddies.map((b) => b.name), ['Dr. Pell']);
  assert.deepEqual(result[2].buddies.map((b) => b.name), ['Jules Marchetti']);
  assert.deepEqual(result[3], { name: 'Offline', online: 0, total: 1, buddies: [kenji] });
});

test('sections: ties on presence sort by name, then key; nothing gives nothing', () => {
  const a = { accountId: 'x', remoteId: 'b@x', name: 'Sam', show: 'available', groups: ['G'] };
  const b = { accountId: 'x', remoteId: 'a@x', name: 'sam', show: 'available', groups: ['G'] };
  const c = { accountId: 'x', remoteId: 'c@x', show: 'available', groups: ['G'] };

  assert.deepEqual(Buddies.sections([a, b, c])[0].buddies.map((x) => x.remoteId), ['c@x', 'a@x', 'b@x']);
  assert.deepEqual(Buddies.sections(null), []);
});

test('rows: a header per section and a row per buddy, skipping collapsed sections', () => {
  const sectionList = Buddies.sections([okafor, kenji]);
  const open = Buddies.rows(sectionList, null);
  const closed = Buddies.rows(sectionList, { Biology: true });

  assert.deepEqual(open.map((r) => r.key), [
    'group:Biology', 'buddy:Biology:x|okafor@pim.test', 'group:Offline', 'buddy:Offline:x|kosei30@pim.test'
  ]);
  assert.deepEqual(open[1], {
    kind: 'buddy', key: 'buddy:Biology:x|okafor@pim.test', accountId: 'x', remoteId: 'okafor@pim.test',
    name: 'Mr. Okafor', show: 'away', status: 'Teaching Biology · Block 2'
  });
  assert.deepEqual(closed.map((r) => r.key), ['group:Biology', 'group:Offline', 'buddy:Offline:x|kosei30@pim.test']);
  assert.strictEqual(closed[0].collapsed, true);
  assert.deepEqual(Buddies.rows(null), []);
  assert.strictEqual(Buddies.rows(Buddies.sections([{ accountId: 'x', remoteId: 'q', name: 'Q', groups: ['G'], show: 'away' }]))[1].status, '');
});

test('headerLabel: online over total, or just the total for Offline', () => {
  assert.strictEqual(Buddies.headerLabel({ name: 'Biology', online: 2, total: 4 }), 'Biology (2/4)');
  assert.strictEqual(Buddies.headerLabel({ name: 'Offline', online: 0, total: 5 }), 'Offline (5)');
});

test('detail: the away message, else the presence label unless available', () => {
  assert.strictEqual(Buddies.detail({ show: 'away', status: 'At practice' }), 'At practice');
  assert.strictEqual(Buddies.detail({ show: 'busy', status: '' }), 'Busy');
  assert.strictEqual(Buddies.detail({ show: 'available', status: '' }), '');
});

test('move and rowFor: clamp at the ends, start at the first row, find by key', () => {
  const list = [{ key: 'a' }, { key: 'b' }, { key: 'c' }];

  assert.strictEqual(Buddies.move(list, 'b', 1), 'c');
  assert.strictEqual(Buddies.move(list, 'c', 1), 'c');
  assert.strictEqual(Buddies.move(list, 'a', -1), 'a');
  assert.strictEqual(Buddies.move(list, 'gone', 1), 'a');
  assert.strictEqual(Buddies.move([], 'a', 1), '');
  assert.deepEqual(Buddies.rowFor(list, 'b'), { key: 'b' });
  assert.strictEqual(Buddies.rowFor(list, 'z'), null);
  assert.strictEqual(Buddies.rowFor(null, 'a'), null);
});
