.pragma library

// Account setup: checks what the user typed for a new Telegram account
// or a school server account, describes the field each sign-in step asks
// for, and the keyboard mnemonics the service chooser and account removal
// show as hints.

// FIELDS describes the typed answer for each sign-in step.
var FIELDS = {
  phone: { placeholder: 'Phone number, with country code', password: false },
  code: { placeholder: 'Login code', password: false },
  password: { placeholder: 'Two-step verification password', password: true }
};

// MNEMONICS maps a known service id to the single letter its chooser
// button shows and answers to. Picked to avoid j and k, the list's own
// up/down keys.
var MNEMONICS = { telegram: 't', whatsapp: 'w', xmpp: 'x' };

// FORM_SERVICES lists the services signed into with an address and
// password typed into a form, rather than a QR code or a phone number.
var FORM_SERVICES = ['xmpp'];

// ADDRESS matches a bare address, name@domain, with no resource after a
// slash: the same shape the helper accepts.
var ADDRESS = /^[^@\s/]+@[^@\s/]+$/;

// apiID reads the API id from my.telegram.org, or 0 when it is not a
// positive whole number.
function apiID(text) {
  const trimmed = String(text).trim();

  return /^[1-9][0-9]*$/.test(trimmed) ? Number(trimmed) : 0;
}

// credentialsReady reports whether an API id and hash have been entered.
function credentialsReady(id, hash) {
  return apiID(id) > 0 && String(hash).trim() !== '';
}

// field returns {step, placeholder, password} for a stage the user
// answers by typing, or null.
function field(stage) {
  const f = FIELDS[stage];

  return f ? { step: stage, placeholder: f.placeholder, password: f.password } : null;
}

// firstStage returns the setup stage a new account for serviceId starts
// at: the server form for a service signed into with an address and
// password, else waiting for the connector's first sign-in step.
function firstStage(serviceId) {
  return FORM_SERVICES.includes(serviceId) ? 'server' : 'waiting';
}

// serverAccount checks the server form and returns {error, options}:
// options for accounts.add when it is complete, else null and a sentence
// saying what to fix. The server and certificate file are optional and
// left out when empty. The password is sent exactly as typed.
function serverAccount(address, password, server, caFile) {
  const addr = String(address).trim();
  const host = String(server).trim();
  const ca = String(caFile).trim();

  if (!ADDRESS.test(addr)) return { error: 'Enter your address, like name@school.org.', options: null };
  if (String(password) === '') return { error: 'Enter your password.', options: null };
  if (ca !== '' && !ca.startsWith('/')) return { error: 'Enter the certificate file as a full path, starting with /.', options: null };

  const options = { address: addr, password: String(password) };
  if (host !== '') options.server = host;
  if (ca !== '') options.caFile = ca;
  return { error: '', options: options };
}

// serverFailure returns what to tell someone whose new server account did
// not sign in, by its account status, or '' while it is still trying or
// has connected. The helper's own detail for a refused password tells
// someone to remove the account, which the form does for them.
function serverFailure(status) {
  if (status === 'needs-auth') return "The server didn't accept that address or password.";
  if (status === 'error') return "Couldn't connect to the server. Check the server address and certificate file, and that the server is running.";
  return '';
}

// qrSource turns the helper's base64 PNG into an image URL.
function qrSource(png) {
  return png ? `data:image/png;base64,${png}` : '';
}

// serviceMnemonic returns the single letter a chooser button for a known
// service answers to, or '' for a service with none assigned yet.
function serviceMnemonic(serviceId) {
  return MNEMONICS[serviceId] ?? '';
}

// mnemonicLabel prefixes text with its mnemonic letter, the way every
// choice list's hints read: "t  Telegram". Text alone when there is none.
function mnemonicLabel(letter, text) {
  return letter ? `${letter}  ${text}` : text;
}

// qrRefreshOnly reports whether an incoming "qr" step is just the QR
// login's background refresh, arriving after the user already left the
// QR stage to type a phone number, code or password. The connector keeps
// refreshing its QR code until the user actually submits something else,
// so applying every such step in full would throw the user back to a new
// QR code and lose what they were typing; the caller should instead only
// refresh the cached QR image, without changing the stage shown.
// accountId is empty until the very first step for the account has been
// applied, so that first "qr" step (which also sets accountId and opens
// setup) is never mistaken for a refresh.
function qrRefreshOnly(accountId, stage, stepKind) {
  return accountId !== '' && stepKind === 'qr' && stage !== 'qr' && stage !== 'waiting';
}

// wrapIndex moves a highlighted index by delta through count items, with
// one extra "nothing chosen" slot at -1 that sits before the first item
// and after the last, so a safe default (such as Cancel) can wrap in and
// out of a list the same way j/k already wrap within it. 0 items always
// stays at -1.
function wrapIndex(index, delta, count) {
  if (count <= 0) {
    return -1;
  }

  const span = count + 1;
  const shifted = (((index + 1) + delta) % span + span) % span;
  return shifted - 1;
}
