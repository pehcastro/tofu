const HOST = 'com.ephem.tofu';
const PROTOCOL_VERSION = 1;
const DEBUGGER_VERSION = '1.3';
const RECONNECT_MIN_MS = 1000;
const RECONNECT_MAX_MS = 30000;
const WAIT_MS = 500;
const READ_OPS = ['snapshot', 'fresh'];
const DRIVE_OPS = ['click', 'fill', 'select', 'scroll', 'wait'];
const MODES = ['read', 'drive'];
const DIRECTIONS = ['up', 'down'];
const OPENABLE_PROTOCOLS = ['http:', 'https:', 'file:'];
const SELECT_ALL_MODIFIER = navigator.userAgent.includes('Mac') ? 4 : 2;

const shared = new Map();
const opened = new Set();
let port = null;
let hostError = '';
let reconnectDelay = RECONNECT_MIN_MS;
let snapshotSource = null;

const tabInfo = tab => ({id: tab.id, url: tab.url ?? '', title: tab.title ?? ''});
const post = message => port?.postMessage(message);
const send = (tabId, method, params) => chrome.debugger.sendCommand({tabId}, method, params);

function connect() {
  if (port) return;
  port = chrome.runtime.connectNative(HOST);
  port.onMessage.addListener(message => {
    reconnectDelay = RECONNECT_MIN_MS;
    if (message.t === 'call') void answer(message);
  });
  port.onDisconnect.addListener(() => {
    hostError = chrome.runtime.lastError?.message ?? 'the tofu host exited';
    port = null;
    setTimeout(connect, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX_MS);
  });
  post({t: 'hello', version: PROTOCOL_VERSION, tabs: [...shared.values()]});
}

async function answer({id, tabId, op, args}) {
  try {
    post({t: 'result', id, ok: true, value: await perform(tabId, op, args ?? {})});
  } catch (error) {
    post({t: 'result', id, ok: false, error: error.message});
  }
}

async function perform(tabId, op, args) {
  if (op === 'open') return openTab(String(args.url ?? ''));
  if (op === 'close') return closeOpened(tabId);
  const tab = shared.get(tabId);
  if (!tab) throw new Error(`tab ${tabId} is not shared with tofu`);
  const fingerprint = String(args.fingerprint ?? '');
  if (READ_OPS.includes(op)) return evaluate(tabId, {op, fingerprint});
  if (!DRIVE_OPS.includes(op)) throw new Error(`unknown op ${op}`);
  if (tab.mode !== 'drive') throw new Error(`tab ${tabId} is shared for reading only`);
  if (op === 'scroll' && !DIRECTIONS.includes(args.direction)) throw new Error(`no scroll direction ${args.direction}`);
  const request = {op, fingerprint, element: Number(args.element), value: String(args.value ?? ''), direction: args.direction};
  const target = await evaluate(tabId, request);
  if (op === 'wait') await new Promise(resolve => setTimeout(resolve, WAIT_MS));
  if (op !== 'click' && op !== 'fill') return;
  for (const type of ['mousePressed', 'mouseReleased']) {
    await send(tabId, 'Input.dispatchMouseEvent', {type, x: target.x, y: target.y, button: 'left', clickCount: 1});
  }
  if (op !== 'fill') return;
  const selectAll = {key: 'a', code: 'KeyA', modifiers: SELECT_ALL_MODIFIER};
  await send(tabId, 'Input.dispatchKeyEvent', {...selectAll, type: 'keyDown', commands: ['selectAll']});
  await send(tabId, 'Input.dispatchKeyEvent', {...selectAll, type: 'keyUp'});
  await send(tabId, 'Input.insertText', {text: request.value});
}

async function evaluate(tabId, request) {
  snapshotSource ??= await (await fetch(chrome.runtime.getURL('snapshot.js'))).text();
  const expression = `(${snapshotSource})(${JSON.stringify(request)})`;
  const {result, exceptionDetails} = await send(tabId, 'Runtime.evaluate', {expression, returnByValue: true});
  if (exceptionDetails) throw new Error('stale: the page changed while tofu read it');
  const value = result.value;
  if (value?.stale) throw new Error(`stale: ${value.stale}`);
  if (value?.error) throw new Error(value.error);
  return value;
}

async function openTab(url) {
  if (!OPENABLE_PROTOCOLS.includes(URL.parse(url)?.protocol)) throw new Error(`tofu opens only http, https and file URLs, not ${url}`);
  const {id} = await chrome.tabs.create({url, active: false});
  opened.add(id);
  try {
    await share(id, 'drive');
  } catch (error) {
    await closeOpened(id);
    throw error;
  }
  return id;
}

async function closeOpened(tabId) {
  if (!opened.has(tabId)) throw new Error(`tab ${tabId} is the person's: tofu closes only tabs it opened`);
  await chrome.tabs.remove(tabId);
}

async function share(tabId, mode) {
  if (!MODES.includes(mode)) throw new Error(`no sharing mode ${mode}`);
  const tab = {...tabInfo(await chrome.tabs.get(tabId)), opened: opened.has(tabId)};
  if (!shared.has(tabId)) await chrome.debugger.attach({tabId}, DEBUGGER_VERSION);
  shared.set(tabId, {...tab, mode});
  post({t: 'shared', tab, mode});
}

async function unshare(tabId) {
  opened.delete(tabId);
  if (!shared.delete(tabId)) return;
  post({t: 'unshared', tabId});
  await chrome.debugger.detach({tabId}).catch(() => {});
}

async function popupAction({t, tabId, mode}) {
  let failure = '';
  try {
    if (t === 'share') await share(tabId, mode);
    if (t === 'stop') await unshare(tabId);
  } catch (error) {
    failure = error.message;
  }
  const state = {connected: port !== null, hostError, mode: shared.get(tabId)?.mode ?? '', failure};
  connect();
  return state;
}

chrome.runtime.onMessage.addListener((message, _sender, reply) => {
  void popupAction(message).then(reply);
  return true;
});
chrome.tabs.onRemoved.addListener(tabId => void unshare(tabId));
chrome.debugger.onDetach.addListener(({tabId}) => void unshare(tabId));
chrome.tabs.onUpdated.addListener((tabId, change, tab) => {
  const entry = shared.get(tabId);
  if (!entry || (change.url === undefined && change.title === undefined)) return;
  Object.assign(entry, tabInfo(tab));
  post({t: 'tabUpdated', tab: tabInfo(tab)});
});

chrome.debugger.getTargets()
  .then(targets => Promise.all(targets.filter(target => target.attached && target.tabId !== undefined)
    .map(target => chrome.debugger.detach({tabId: target.tabId}).catch(() => {}))))
  .finally(connect);
