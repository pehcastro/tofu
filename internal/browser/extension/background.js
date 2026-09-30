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
const LOAD_MS = 15000;
const DRIVE_OPS = ['click', 'fill', 'select', 'scroll', 'wait'];
const IN_PAGE_ACTS = ['scroll', 'select'];
const DIRECTIONS = ['up', 'down'];
const OPENABLE_PROTOCOLS = ['http:', 'https:', 'file:'];
const SELECT_ALL_MODIFIER = navigator.userAgent.includes('Mac') ? 4 : 2;
const CURSOR_GLIDE_MS = 150;
const CURSOR_RING_MS = 250;
const CURSOR_IDLE_MS = 3000;
const THINK_MS = 30000;
const ORPHAN_MS = 2000;
const SCREENCAST_QUALITY = 80;
const SCREENCAST_CHUNK_BYTES = 1000000;
const SCREENCAST_SPARE_ACKS = 8;
const NO_GROUP = -1;
const GROUP_COLOR = 'orange';
const BADGES = {
  idle: {text: 'on', color: '#1a7f37', group: 'tofu ⏸️'},
  reading: {text: 'read', color: '#0969da', group: 'tofu 👀'},
  acting: {text: 'act', color: '#d9480f', group: 'tofu 🔄'},
  off: {text: 'off', color: '#8b8b8b', group: 'tofu ✅'},
};

const BUILD = fetch(chrome.runtime.getURL('manifest.json')).then(response => response.text()).then(text => (JSON.parse(text).version_name ?? '').split(' ').pop());

const attached = new Map();
const opened = new Set();
const grouped = new Set();
const optedOut = new Set();
const children = new Map();
const screencasts = new Map();
const cursorless = new Set();
let port = null;
let hostError = '';
let reconnectDelay = RECONNECT_MIN_MS;
let snapshotSource = null;
let groups = null;
let groupWork = Promise.resolve();
let groupTitle = 'tofu ⏸️';
let lastAct = {tabId: 0, windowId: 0, at: 0};

const tabInfo = tab => ({id: tab.id, url: tab.url ?? tab.pendingUrl ?? '', title: tab.title ?? '', opened: opened.has(tab.id)});
const post = message => port?.postMessage(message);
const send = (tabId, method, params) => chrome.debugger.sendCommand({tabId}, method, params);
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const ourGroups = async () => groups ??= (await chrome.storage.session.get({groups: {}})).groups;

function openable(url) {
  if (!OPENABLE_PROTOCOLS.includes(URL.parse(url)?.protocol)) throw new Error(`tofu opens only http, https and file URLs, not ${url}`);
}

function ownOnly(tabId, verb) {
  if (!opened.has(tabId)) throw new Error(`tab ${tabId} is the person's: tofu ${verb} only tabs it opened`);
}

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
    if (message.t === 'reload') chrome.runtime.reload();
  });
  port.onDisconnect.addListener(() => {
    hostError = chrome.runtime.lastError?.message ?? 'the tofu host exited';
    port = null;
    void show('off');
    cursorless.clear();
    serially(restoreGroups);
    for (const [tabId, attaching] of attached) {
      dropScreencast(tabId);
      attaching.then(() => send(tabId, 'Runtime.evaluate', {expression: `(${removeCursor})()`}).catch(() => {}))
        .then(() => chrome.debugger.detach({tabId})).catch(() => {});
    }
    attached.clear();
    setTimeout(connect, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX_MS);
  });
  void show('idle');
  post({t: 'hello', version: PROTOCOL_VERSION, build: await BUILD, tabs: (await chrome.tabs.query({})).map(tabInfo)});
}

async function show(state) {
  const badge = BADGES[state];
  if (!badge) return;
  if (groupTitle !== badge.group) {
    const title = groupTitle = badge.group;
    serially(async () => {
      for (const groupId of Object.values(await ourGroups())) await chrome.tabGroups.update(groupId, {title});
    });
  }
  await chrome.action.setBadgeText({text: badge.text});
  await chrome.action.setBadgeBackgroundColor({color: badge.color});
  await chrome.action.setTitle({title: state === 'off' ? `tofu is not connected: ${hostError}` : `tofu is ${state}`});
}

async function answer({id, tabId, op, args, cursor}) {
  const timing = {evaluate_ms: 0, settle_ms: 0, act_ms: 0};
  try {
    const value = op === 'screencast' ? await screencast(tabId, args?.action, id) : await perform(tabId, op, args ?? {}, timing, cursor && !cursorless.has(tabId));
    post({t: 'result', id, ok: true, value, timing});
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

async function perform(tabId, op, args, timing, cursor) {
  const url = String(args.url ?? '');
  if (opened.has(tabId) && (DRIVE_OPS.includes(op) || (op === 'cdp' && args.act))) {
    lastAct = {tabId, windowId: (await chrome.tabs.get(tabId)).windowId, at: Date.now()};
  }
  if (op === 'open') return openTab(url);
  if (op === 'navigate') return navigateOpened(tabId, url, cursor);
  if (op === 'close') return closeOpened(tabId);
  if (op === 'back') return goBack(tabId);
  if (op === 'cdp') return relay(tabId, args, cursor);
  if (op !== 'snapshot' && !DRIVE_OPS.includes(op)) throw new Error(`unknown op ${op}`);
  if (op === 'scroll' && !DIRECTIONS.includes(args.direction)) throw new Error(`no scroll direction ${args.direction}`);
  await attach(tabId);
  if (op === 'snapshot') return timed(timing, 'evaluate_ms', () => evaluate(tabId, {op}));
  if (!grouped.has(tabId)) serially(() => groupTab(tabId));
  const request = {op, element: Number(args.element), guard: String(args.guard ?? ''), value: String(args.value ?? ''), direction: args.direction};
  const target = await timed(timing, IN_PAGE_ACTS.includes(op) ? 'act_ms' : 'evaluate_ms', () => evaluate(tabId, request));
  if (target.stale) return target;
  children.delete(tabId);
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
  const child = children.get(tabId);
  if (child === undefined) return {};
  await loaded(child);
  return {opened: child};
}

async function attach(tabId) {
  if (!attached.has(tabId)) {
    attached.set(tabId, chrome.debugger.attach({tabId}, DEBUGGER_VERSION)
      .then(() => send(tabId, 'Emulation.setFocusEmulationEnabled', {enabled: true}))
      .then(() => send(tabId, 'Page.setWebLifecycleState', {state: 'active'}).catch(() => {}))
      .then(() => opened.has(tabId) && keepNavigationIn(tabId).catch(() => {}))
      .catch(error => {
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
  if (!live) await chrome.tabGroups.update(groupId, {title: groupTitle, color: GROUP_COLOR});
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

async function loaded(tabId) {
  for (const until = Date.now() + LOAD_MS; Date.now() < until; await sleep(COMMIT_POLL_MS)) {
    const tab = await chrome.tabs.get(tabId);
    if (tab.status === 'complete' && !tab.pendingUrl && tab.url && tab.url !== 'about:blank') return;
  }
}

async function openTab(url) {
  openable(url);
  const tab = await chrome.tabs.create({url, active: false});
  opened.add(tab.id);
  post({t: 'tabUpdated', tab: tabInfo(tab)});
  serially(() => groupTab(tab.id));
  await loaded(tab.id);
  return tab.id;
}

async function navigateOpened(tabId, url, cursor) {
  openable(url);
  ownOnly(tabId, 'navigates');
  await chrome.tabs.update(tabId, {url});
  await loaded(tabId);
  if (cursor) attach(tabId).then(() => paintAt(tabId, 'innerWidth / 2', 16, `tofu → ${URL.parse(url).host}`), () => {});
  return tabId;
}

async function goBack(tabId) {
  ownOnly(tabId, 'navigates');
  await chrome.tabs.goBack(tabId);
  await loaded(tabId);
  return tabId;
}

function paintAt(tabId, x, y, label) {
  send(tabId, 'Runtime.evaluate', {expression: `(${paintCursor})(${x}, ${y}, ${JSON.stringify(label)}, ${CURSOR_GLIDE_MS}, ${CURSOR_RING_MS}, ${CURSOR_IDLE_MS})`}).catch(() => {});
}

async function relay(tabId, {calls, act, point, thinking}, cursor) {
  await attach(tabId);
  if (act && !grouped.has(tabId)) serially(() => groupTab(tabId));
  const click = calls.find(({method}) => method === 'Input.dispatchMouseEvent');
  const at = point ?? (click && {...click.params, label: 'tofu'});
  if (cursor && at && opened.has(tabId)) paintAt(tabId, at.x, at.y, at.label);
  if (cursor && thinking && opened.has(tabId)) {
    send(tabId, 'Runtime.evaluate', {expression: `(${thinkCursor})(${JSON.stringify('tofu thinking')}, ${THINK_MS})`}).catch(() => {});
  }
  return Promise.all(calls.map(({method, params}) => send(tabId, method, params).then(result => ({result}), error => ({error: error.message}))));
}

async function screencast(tabId, action, id) {
  if (action === 'show') {
    cursorless.delete(tabId);
    return {};
  }
  await attach(tabId);
  if (action === 'hide') {
    cursorless.add(tabId);
    await send(tabId, 'Runtime.evaluate', {expression: `(${removeCursor})()`});
    return {};
  }
  if (action === 'start') {
    if (screencasts.has(tabId)) throw new Error(`tab ${tabId} is already recording a screencast`);
    const frames = [];
    const listener = (source, method, params) => {
      if (source.tabId !== tabId || method !== 'Page.screencastFrame') return;
      frames.push({data: params.data, timestamp: params.metadata.timestamp});
      for (let acks = frames.length === 1 ? 1 + SCREENCAST_SPARE_ACKS : 1; acks > 0; acks--) {
        send(tabId, 'Page.screencastFrameAck', {sessionId: params.sessionId}).catch(() => {});
      }
    };
    chrome.debugger.onEvent.addListener(listener);
    screencasts.set(tabId, {frames, listener});
    await send(tabId, 'Page.startScreencast', {format: 'jpeg', quality: SCREENCAST_QUALITY, everyNthFrame: 1}).catch(error => {
      dropScreencast(tabId);
      throw error;
    });
    return {};
  }
  if (action !== 'stop') throw new Error(`no screencast action ${action}`);
  const recording = screencasts.get(tabId);
  if (!recording) throw new Error(`tab ${tabId} is not recording a screencast`);
  await send(tabId, 'Page.stopScreencast', {}).finally(() => dropScreencast(tabId));
  const chunks = screencastChunks(recording.frames);
  for (const frames of chunks.slice(0, -1)) post({t: 'frames', id, frames});
  return chunks.at(-1);
}

function dropScreencast(tabId) {
  const recording = screencasts.get(tabId);
  if (recording) chrome.debugger.onEvent.removeListener(recording.listener);
  screencasts.delete(tabId);
}

function screencastChunks(frames) {
  const chunks = [[]];
  let size = 0;
  for (const frame of frames) {
    const bytes = JSON.stringify(frame).length + 1;
    if (bytes > SCREENCAST_CHUNK_BYTES) throw new Error(`a screencast frame of ${bytes} bytes is over the ${SCREENCAST_CHUNK_BYTES} byte message cap`);
    if (size + bytes > SCREENCAST_CHUNK_BYTES) {
      chunks.push([]);
      size = 0;
    }
    chunks.at(-1).push(frame);
    size += bytes;
  }
  return chunks;
}

function paintCursor(x, y, label, glideMs, ringMs, idleMs) {
  let host = document.querySelector('[data-tofu-cursor]');
  if (!host) {
    host = document.createElement('div');
    host.setAttribute('data-tofu-cursor', '');
    host.setAttribute('aria-hidden', 'true');
    host.style.cssText = 'position: fixed; left: 0; top: 0; z-index: 2147483647; pointer-events: none;';
    host.tofu = host.attachShadow({mode: 'closed'});
    host.tofu.innerHTML = `<style>
      .c { position: fixed; left: 0; top: 0; display: flex; gap: 4px; pointer-events: none; transition: transform ${glideMs}ms ease-out, opacity 400ms ease-out; }
      .c svg { filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.35)); }
      .c span { margin-top: 20px; padding: 2px 10px; border-radius: 12px; background: #d9480f; color: #fff; font: 600 20px/28px system-ui, sans-serif; box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.7), 0 1px 3px rgba(0, 0, 0, 0.3); }
      .r { position: fixed; left: -16px; top: -16px; width: 32px; height: 32px; box-sizing: border-box; border-radius: 50%; border: 2px solid rgba(217, 72, 15, 0.45); opacity: 0; pointer-events: none; }
      .r.on { animation: ring ${ringMs}ms ease-out ${glideMs}ms both; }
      @keyframes ring { from { opacity: 0.3; transform: scale(0.5); } to { opacity: 0; transform: scale(1); } }
      .c.think span { animation: breathe 1600ms ease-in-out infinite; }
      @keyframes breathe { 50% { opacity: 0.55; } }
      @media (prefers-reduced-motion: reduce) { .c.think span { animation: none; } }
    </style><div class="c"><svg width="22" height="24" viewBox="0 0 11 12"><path d="M1 1v9l2.6-2.2 1.8 3.7 1.4-.7-1.8-3.6h3.4z" fill="#fff" stroke="#262626" stroke-width=".6" stroke-linejoin="round"/></svg><span>tofu</span></div><i class="r"></i>`;
    document.documentElement.append(host);
  }
  const cursor = host.tofu.querySelector('.c'), ring = host.tofu.querySelector('.r');
  host.tofu.querySelector('span').textContent = label;
  cursor.classList.remove('think');
  cursor.style.opacity = '1';
  cursor.style.transform = `translate(${x}px, ${y}px)`;
  ring.style.translate = `${x}px ${y}px`;
  ring.classList.remove('on');
  void ring.offsetWidth;
  ring.classList.add('on');
  clearTimeout(host.fade);
  host.fade = setTimeout(() => { cursor.style.opacity = '0'; }, idleMs);
}

function thinkCursor(label, idleMs) {
  const host = document.querySelector('[data-tofu-cursor]');
  if (!host) return;
  const cursor = host.tofu.querySelector('.c');
  host.tofu.querySelector('span').textContent = label;
  cursor.classList.add('think');
  cursor.style.opacity = '1';
  clearTimeout(host.fade);
  host.fade = setTimeout(() => { cursor.style.opacity = '0'; }, idleMs);
}

async function keepNavigationIn(tabId) {
  const source = `(${keepInTab})()`;
  await send(tabId, 'Page.enable', {});
  await send(tabId, 'Page.addScriptToEvaluateOnNewDocument', {source});
  await send(tabId, 'Runtime.evaluate', {expression: source});
}

function keepInTab() {
  const mark = Symbol.for('tofu-keep');
  if (window[mark]) return;
  window[mark] = true;
  const leaves = target => target !== '' && !['_self', '_top', '_parent'].includes(target.toLowerCase()) && frames[target] === undefined;
  const stay = (element, key) => {
    if (leaves(element[key] || document.querySelector('base[target]')?.target || '')) element[key] = '_self';
  };
  window.addEventListener('click', event => {
    const link = event.composedPath().find(node => node.tagName === 'A' || node.tagName === 'AREA');
    if (link) stay(link, 'target');
  }, true);
  window.addEventListener('submit', ({target, submitter}) => {
    if (submitter?.formTarget) stay(submitter, 'formTarget');
    else stay(target, 'target');
  }, true);
  const submit = HTMLFormElement.prototype.submit;
  HTMLFormElement.prototype.submit = function () {
    stay(this, 'target');
    return submit.call(this);
  };
  const open = window.open;
  window.open = function (url, target, ...features) {
    if (!url || !leaves(target || '_blank')) return open.call(this, url, target, ...features);
    location.assign(new URL(url, location.href).href);
    return window;
  };
}

function removeCursor() {
  document.querySelector('[data-tofu-cursor]')?.remove();
  return 'tofu-cursor-remove';
}

async function closeOpened(tabId) {
  ownOnly(tabId, 'closes');
  await chrome.tabs.remove(tabId);
}

chrome.tabs.onCreated.addListener(tab => {
  const orphan = tab.openerTabId === undefined && lastAct.windowId === tab.windowId && Date.now() - lastAct.at < ORPHAN_MS;
  if (opened.has(tab.openerTabId) || orphan) {
    opened.add(tab.id);
    children.set(orphan ? lastAct.tabId : tab.openerTabId, tab.id);
    serially(() => groupTab(tab.id));
  }
  post({t: 'tabUpdated', tab: tabInfo(tab)});
});
chrome.tabs.onRemoved.addListener(tabId => {
  for (const set of [attached, opened, grouped, optedOut, children, cursorless]) set.delete(tabId);
  dropScreencast(tabId);
  post({t: 'tabRemoved', tabId});
});
chrome.debugger.onDetach.addListener(({tabId}) => {
  attached.delete(tabId);
  dropScreencast(tabId);
});
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
