// Tests for ui/lib/Setup.js
'use strict';

const { test } = require('node:test');
const assert = require('node:assert');
const { load } = require('./load.cjs');

const Setup = load('lib/Setup.js');

test('apiID reads a positive whole number, ignoring spaces', () => {
  assert.strictEqual(Setup.apiID(' 123456 '), 123456);
});

test('apiID rejects anything else as 0', () => {
  for (const text of ['', 'abc', '12a', '-5', '0', '1.5']) {
    assert.strictEqual(Setup.apiID(text), 0, text);
  }
});

test('credentialsReady needs an API id and a hash', () => {
  assert.strictEqual(Setup.credentialsReady('123', ' abc '), true);
  assert.strictEqual(Setup.credentialsReady('123', '  '), false);
  assert.strictEqual(Setup.credentialsReady('x', 'abc'), false);
});

test('field describes what each answered step asks for', () => {
  assert.deepEqual(Setup.field('phone'), { step: 'phone', placeholder: 'Phone number, with country code', password: false });
  assert.deepEqual(Setup.field('code'), { step: 'code', placeholder: 'Login code', password: false });
  assert.deepEqual(Setup.field('password'), { step: 'password', placeholder: 'Two-step verification password', password: true });
});

test('field is null for stages without a typed answer', () => {
  for (const stage of ['credentials', 'waiting', 'qr', 'linkcode', '']) {
    assert.strictEqual(Setup.field(stage), null, stage);
  }
});

test('qrSource turns the helper\'s base64 PNG into an image URL', () => {
  assert.strictEqual(Setup.qrSource('cG5n'), 'data:image/png;base64,cG5n');
  assert.strictEqual(Setup.qrSource(''), '');
});

test('serviceMnemonic names the letter a known service answers to, avoiding j and k', () => {
  assert.strictEqual(Setup.serviceMnemonic('telegram'), 't');
  assert.strictEqual(Setup.serviceMnemonic('whatsapp'), 'w');
  assert.strictEqual(Setup.serviceMnemonic('signal'), '');
});

test('mnemonicLabel prefixes the letter, or leaves text alone without one', () => {
  assert.strictEqual(Setup.mnemonicLabel('t', 'Telegram'), 't  Telegram');
  assert.strictEqual(Setup.mnemonicLabel('', 'Signal'), 'Signal');
});

test('wrapIndex moves through the list and wraps onto -1, the "nothing chosen" slot', () => {
  assert.strictEqual(Setup.wrapIndex(-1, 1, 2), 0);
  assert.strictEqual(Setup.wrapIndex(0, 1, 2), 1);
  assert.strictEqual(Setup.wrapIndex(1, 1, 2), -1);
  assert.strictEqual(Setup.wrapIndex(-1, -1, 2), 1);
  assert.strictEqual(Setup.wrapIndex(0, -1, 2), -1);
});

test('wrapIndex stays at -1 with nothing to move through', () => {
  assert.strictEqual(Setup.wrapIndex(-1, 1, 0), -1);
  assert.strictEqual(Setup.wrapIndex(-1, -1, 0), -1);
});

test('qrRefreshOnly is false for the first QR code, before an account is known', () => {
  assert.strictEqual(Setup.qrRefreshOnly('', 'waiting', 'qr'), false);
});

test('qrRefreshOnly is false while still on the QR stage, so a refresh still updates it', () => {
  assert.strictEqual(Setup.qrRefreshOnly('tg-1', 'qr', 'qr'), false);
});

test('qrRefreshOnly is true for a QR refresh once the user moved on to phone, code or password', () => {
  for (const stage of ['phone', 'code', 'password']) {
    assert.strictEqual(Setup.qrRefreshOnly('tg-1', stage, 'qr'), true, stage);
  }
});

test('qrRefreshOnly is false for a step that is not a QR code, even past the QR stage', () => {
  assert.strictEqual(Setup.qrRefreshOnly('tg-1', 'phone', 'code'), false);
  assert.strictEqual(Setup.qrRefreshOnly('tg-1', 'phone', 'password'), false);
});

test('firstStage opens the server form for a service signed into with an address and password', () => {
  assert.strictEqual(Setup.firstStage('xmpp'), 'server');
  assert.strictEqual(Setup.firstStage('telegram'), 'waiting');
  assert.strictEqual(Setup.firstStage('whatsapp'), 'waiting');
});

test('serviceMnemonic gives XMPP its own chooser letter', () => {
  assert.strictEqual(Setup.serviceMnemonic('xmpp'), 'x');
});

test('serverAccount trims the address and leaves out empty optional fields', () => {
  assert.deepEqual(Setup.serverAccount(' rteague29@pim.test ', ' pass ', ' ', ''), {
    error: '',
    options: { address: 'rteague29@pim.test', password: ' pass ' }
  });
});

test('serverAccount keeps the server and certificate file when given', () => {
  assert.deepEqual(Setup.serverAccount('a@pim.test', 'pw', ' 192.168.1.20:5222 ', ' /home/me/pim.test.crt '), {
    error: '',
    options: { address: 'a@pim.test', password: 'pw', server: '192.168.1.20:5222', caFile: '/home/me/pim.test.crt' }
  });
});

test('serverAccount refuses what the helper would refuse, with a sentence for the user', () => {
  const cases = [
    ['', 'pw', '', '', 'Enter your address, like name@school.org.'],
    ['pim.test', 'pw', '', '', 'Enter your address, like name@school.org.'],
    ['a@b@pim.test', 'pw', '', '', 'Enter your address, like name@school.org.'],
    ['a@pim.test/laptop', 'pw', '', '', 'Enter your address, like name@school.org.'],
    ['a@pim.test', '', '', '', 'Enter your password.'],
    ['a@pim.test', 'pw', '', '~/pim.test.crt', 'Enter the certificate file as a full path, starting with /.']
  ];
  for (const [address, password, server, caFile, error] of cases) {
    assert.deepEqual(Setup.serverAccount(address, password, server, caFile), { error: error, options: null }, address);
  }
});

test('serverFailure explains a refused sign-in and an unreachable server, and nothing else', () => {
  assert.strictEqual(Setup.serverFailure('needs-auth'), "The server didn't accept that address or password.");
  assert.match(Setup.serverFailure('error'), /^Couldn't connect to the server\./);
  for (const status of ['connecting', 'connected', '']) assert.strictEqual(Setup.serverFailure(status), '', status);
});
