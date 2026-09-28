const HOST = 'com.ephem.tofu';
const PROTOCOL_VERSION = 2;
const DEBUGGER_VERSION = '1.3';
const RECONNECT_MIN_MS = 1000;
const RECONNECT_MAX_MS = 30000;
const WAIT_MS = 500;
const SETTLE_MS = 50;
const COMBOBOX_SETTLE_MS = 200;
const COMMIT_MS = 5000;
const COMMIT_POLL_MS = 20;
const DRIVE_OPS = ['click', 'fill', 'select', 'scroll', 'wait'];
const IN_PAGE_ACTS = ['scroll', 'select'];
const DIRECTIONS = ['up', 'down'];
const OPENABLE_PROTOCOLS = ['http:', 'https:', 'file:'];
const SELECT_ALL_MODIFIER = navigator.userAgent.includes('Mac') ? 4 : 2;
const NO_GROUP = -1;
const GROUP_COLOR = 'orange';
const BADGES = {
  idle: {text: 'on', color: '#1a7f37'},
  reading: {text: 'read', color: '#0969da'},
  acting: {text: 'act', color: '#d9480f'},
  off: {text: 'off', color: '#8b8b8b'},
};

const attached = new Map();
const opened = new Set();
const grouped = new Set();
const optedOut = new Set();
let port = null;
let hostError = '';
let reconnectDelay = RECONNECT_MIN_MS;
let snapshotSource = null;
let groups = null;
let groupWork = Promise.resolve();
let acting = false;

const tabInfo = tab => ({id: tab.id, url: tab.url ?? tab.pendingUrl ?? '', title: tab.title ?? '', opened: opened.has(tab.id)});
const post = message => port?.postMessage(message);
const send = (tabId, method, params) => chrome.debugger.sendCommand({tabId}, method, params);
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const groupTitle = () => acting ? 'tofu •' : 'tofu';
const ourGroups = async () => groups ??= (await chrome.storage.session.get({groups: {}})).groups;

function serially(work) {
  groupWork = groupWork.then(work).catch(() => {});
}

async function connect() {
  if (port) return;
  port = chrome.runtime.connectNative(HOST);
  port.onMessage.addListener(message => {
    reconnectDelay = RECONNECT_MIN_MS;
    if (message.t === 'call') void answer(message);
    if (message.t === 'status') void show(message.state);
  });
  port.onDisconnect.addListener(() => {
    hostError = chrome.runtime.lastError?.message ?? 'the tofu host exited';
    port = null;
    void show('off');
    serially(restoreGroups);
    for (const [tabId, attaching] of attached) attaching.then(() => chrome.debugger.detach({tabId})).catch(() => {});
    attached.clear();
    setTimeout(connect, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX_MS);
  });
  void show('idle');
  post({t: 'hello', version: PROTOCOL_VERSION, tabs: (await chrome.tabs.query({})).map(tabInfo)});
}

async function show(state) {
  const badge = BADGES[state];
  if (!badge) return;
  if (acting !== (state === 'acting')) {
    acting = state === 'acting';
    serially(async () => {
      for (const groupId of Object.values(await ourGroups())) await chrome.tabGroups.update(groupId, {title: groupTitle()});
    });
  }
  await chrome.action.setBadgeText({text: badge.text});
  await chrome.action.setBadgeBackgroundColor({color: badge.color});
  await chrome.action.setTitle({title: state === 'off' ? `tofu is not connected: ${hostError}` : `tofu is ${state}`});
}

async function answer({id, tabId, op, args}) {
  const timing = {evaluate_ms: 0, settle_ms: 0, act_ms: 0};
  try {
    post({t: 'result', id, ok: true, value: await perform(tabId, op, args ?? {}, timing), timing});
  } catch (error) {
    post({t: 'result', id, ok: false, error: error.message, timing});
  }
}

async function timed(timing, phase, work) {
  const start = performance.now();
  try {
    return await work();
  } finally {
    timing[phase] += performance.now() - start;
  }
}

async function perform(tabId, op, args, timing) {
  if (op === 'open') return openTab(String(args.url ?? ''));
  if (op === 'close') return closeOpened(tabId);
  if (op !== 'snapshot' && !DRIVE_OPS.includes(op)) throw new Error(`unknown op ${op}`);
  if (op === 'scroll' && !DIRECTIONS.includes(args.direction)) throw new Error(`no scroll direction ${args.direction}`);
  await attach(tabId);
  if (op === 'snapshot') return timed(timing, 'evaluate_ms', () => evaluate(tabId, {op}));
  if (!grouped.has(tabId)) serially(() => groupTab(tabId));
  const request = {op, element: Number(args.element), guard: String(args.guard ?? ''), value: String(args.value ?? ''), direction: args.direction};
  const target = await timed(timing, IN_PAGE_ACTS.includes(op) ? 'act_ms' : 'evaluate_ms', () => evaluate(tabId, request));
  if (target.stale) return target;
  await timed(timing, 'act_ms', async () => {
    if (op === 'wait') await sleep(WAIT_MS);
    if (op === 'click' || op === 'fill') {
      for (const type of ['mousePressed', 'mouseReleased']) {
        await send(tabId, 'Input.dispatchMouseEvent', {type, x: target.x, y: target.y, button: 'left', clickCount: 1});
      }
    }
    if (op === 'fill') {
      const selectAll = {key: 'a', code: 'KeyA', modifiers: SELECT_ALL_MODIFIER};
      await send(tabId, 'Input.dispatchKeyEvent', {...selectAll, type: 'keyDown', commands: ['selectAll']});
      await send(tabId, 'Input.dispatchKeyEvent', {...selectAll, type: 'keyUp'});
      await send(tabId, 'Input.insertText', {text: request.value});
    }
  });
  if (op !== 'wait') await timed(timing, 'settle_ms', () => settle(tabId, request.element, op === 'fill' && target.combobox));
  return {};
}

async function attach(tabId) {
  if (!attached.has(tabId)) {
    attached.set(tabId, chrome.debugger.attach({tabId}, DEBUGGER_VERSION).catch(error => {
      attached.delete(tabId);
      throw error;
    }));
  }
  await attached.get(tabId);
}

async function groupTab(tabId) {
  const tab = await chrome.tabs.get(tabId);
  const mine = (await ourGroups())[tab.windowId];
  if (tab.pinned || optedOut.has(tabId) || (tab.groupId !== NO_GROUP && tab.groupId !== mine)) return;
  const live = mine !== undefined && await chrome.tabGroups.get(mine).then(() => true, () => false);
  const groupId = await chrome.tabs.group(live ? {tabIds: [tabId], groupId: mine} : {tabIds: [tabId], createProperties: {windowId: tab.windowId}});
  groups[tab.windowId] = groupId;
  grouped.add(tabId);
  if (!live) await chrome.tabGroups.update(groupId, {title: groupTitle(), color: GROUP_COLOR});
  await chrome.storage.session.set({groups});
}

async function restoreGroups() {
  const dissolved = Object.values(await ourGroups());
  grouped.clear();
  groups = {};
  await chrome.storage.session.set({groups});
  for (const groupId of dissolved) {
    const ids = (await chrome.tabs.query({groupId})).map(tab => tab.id);
    if (ids.length > 0) await chrome.tabs.ungroup(ids);
  }
}

async function settle(tabId, element, combobox) {
  const wait = combobox ? COMBOBOX_SETTLE_MS : SETTLE_MS;
  await Promise.race([evaluate(tabId, {op: 'settle', element, combobox, wait}).catch(() => {}), sleep(wait)]);
  for (const until = Date.now() + COMMIT_MS; Date.now() < until && (await chrome.tabs.get(tabId)).pendingUrl;) {
    await sleep(COMMIT_POLL_MS);
  }
}

async function evaluate(tabId, request) {
  snapshotSource ??= await (await fetch(chrome.runtime.getURL('snapshot.js'))).text();
  const expression = `(${snapshotSource})(${JSON.stringify(request)})`;
  const {result, exceptionDetails} = await send(tabId, 'Runtime.evaluate', {expression, returnByValue: true, awaitPromise: true});
  if (exceptionDetails) return {stale: 'changed'};
  if (result.value?.error) throw new Error(result.value.error);
  return result.value;
}

async function openTab(url) {
  if (!OPENABLE_PROTOCOLS.includes(URL.parse(url)?.protocol)) throw new Error(`tofu opens only http, https and file URLs, not ${url}`);
  const tab = await chrome.tabs.create({url, active: false});
  opened.add(tab.id);
  post({t: 'tabUpdated', tab: tabInfo(tab)});
  serially(() => groupTab(tab.id));
  return tab.id;
}

async function closeOpened(tabId) {
  if (!opened.has(tabId)) throw new Error(`tab ${tabId} is the person's: tofu closes only tabs it opened`);
  await chrome.tabs.remove(tabId);
}

chrome.tabs.onCreated.addListener(tab => post({t: 'tabUpdated', tab: tabInfo(tab)}));
chrome.tabs.onRemoved.addListener(tabId => {
  for (const set of [attached, opened, grouped, optedOut]) set.delete(tabId);
  post({t: 'tabRemoved', tabId});
});
chrome.debugger.onDetach.addListener(({tabId}) => attached.delete(tabId));
chrome.tabs.onUpdated.addListener((tabId, change, tab) => {
  if (change.groupId !== undefined && grouped.has(tabId) && change.groupId !== groups?.[tab.windowId]) {
    grouped.delete(tabId);
    optedOut.add(tabId);
  }
  if (change.url !== undefined || change.title !== undefined) post({t: 'tabUpdated', tab: tabInfo(tab)});
});

chrome.debugger.getTargets()
  .then(targets => Promise.all(targets.filter(target => target.attached && target.tabId !== undefined)
    .map(target => chrome.debugger.detach({tabId: target.tabId}).catch(() => {}))))
  .finally(connect);
