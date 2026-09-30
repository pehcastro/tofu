const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const [dir, scenario = 'groups'] = process.argv.slice(2);
const LOAD_AFTER_MS = 100;
let popupOpener = 20;
let release = 'opens';
let nextTab = 21;
const heard = [];
const posted = [];
const storage = {};
const listeners = {};
const tabs = new Map([
  [9, {id: 9, windowId: 1, url: 'https://stays.test/', title: 'Stays', pinned: false, groupId: -1}],
  [3, {id: 3, windowId: 1, url: 'https://pinned.test/', title: 'Pinned', pinned: true, groupId: -1}],
  [5, {id: 5, windowId: 1, url: 'https://mine.test/', title: 'Mine', pinned: false, groupId: 7}],
  [6, {id: 6, windowId: 1, url: 'https://dragged.test/', title: 'Dragged', pinned: false, groupId: -1}],
]);
const groups = new Map([[7, {id: 7, title: 'work', color: 'blue'}]]);
let nextGroup = 100;
const event = name => ({addListener: listener => { listeners[name] = listener; }});
const note = (...entry) => heard.push(entry);
const loadLater = (id, url) => setTimeout(() => {
  Object.assign(tabs.get(id), {status: 'complete', url, pendingUrl: undefined});
  note('complete', id);
}, LOAD_AFTER_MS);
const createTab = (url, openerTabId) => {
  const id = nextTab++;
  tabs.set(id, {id, windowId: 1, openerTabId, url: '', pendingUrl: url, status: 'loading', pinned: false, groupId: -1});
  note('created', id);
  listeners.created({...tabs.get(id)});
  loadLater(id, url);
};
const documents = new Map();
const documentOf = tabId => {
  if (!documents.has(tabId)) {
    const window = {
      listeners: [],
      addEventListener: (type, listener) => window.listeners.push({type, listener}),
      open: url => createTab(url, undefined),
      location: {href: 'https://stays.test/', assign: url => note('load', tabId, url)},
      frames: {},
      document: {querySelector: () => null},
      HTMLFormElement: class {},
      URL,
    };
    window.window = window;
    documents.set(tabId, vm.createContext(window));
  }
  return documents.get(tabId);
};
const pageActs = {
  opens: () => createTab('https://stays.test/listing', popupOpener),
  link: page => {
    const link = {tagName: 'A', target: '_blank', href: 'https://stays.test/rooms/1'};
    const click = {type: 'click', composedPath: () => [link, page.document, page.window]};
    for (const {type, listener} of page.listeners) if (type === 'click') listener(click);
    if (link.target === '_blank') createTab(link.href, undefined);
    else page.location.assign(link.href);
  },
  windowOpen: page => page.window.open('/rooms/2'),
};
const page = {snapshot: {url: 'https://stays.test/', title: 'Stays', text: '', elements: []}, click: {x: 10, y: 20}, settle: {}};

const chrome = {
  runtime: {
    connectNative: () => ({postMessage: message => {
      posted.push(message);
      note('post', message.t, message.id ?? message.tab?.id ?? null);
    }, onMessage: event('message'), onDisconnect: event('disconnect')}),
    getURL: name => name,
  },
  debugger: {
    getTargets: async () => [],
    attach: async ({tabId}) => note('attach', tabId),
    detach: async ({tabId}) => note('detach', tabId),
    sendCommand: async ({tabId}, method, params) => {
      if (method === 'Runtime.evaluate' && params.expression.includes('data-tofu-cursor')) {
        note('cursor', tabId, params.expression.includes('tofu-cursor-remove') ? 'remove' : 'move', params.expression.match(/"(tofu[^"]*)"/)?.[1] ?? '');
        return {};
      }
      if (method === 'Page.addScriptToEvaluateOnNewDocument' || params.expression?.includes('tofu-keep')) {
        note('keep', tabId, method);
        vm.runInContext(params.expression ?? '', documentOf(tabId));
        return {};
      }
      if (method !== 'Runtime.evaluate') {
        note('input', tabId, method);
        if (tabId === 20 && params.type === 'mouseReleased') pageActs[release](documentOf(tabId));
        return {};
      }
      const request = JSON.parse(params.expression.slice(params.expression.lastIndexOf(')(') + 2, -1));
      note('evaluate', tabId, request.op);
      return {result: {value: page[request.op]}};
    },
    onDetach: event('detached'),
  },
  tabs: {
    get: async id => ({...tabs.get(id)}),
    create: async ({url}) => {
      tabs.set(20, {id: 20, windowId: 1, url: '', pendingUrl: url, status: 'loading', pinned: false, groupId: -1});
      listeners.created({...tabs.get(20)});
      loadLater(20, url);
      return {...tabs.get(20)};
    },
    update: async (id, {url}) => {
      note('navigate', id, url);
      Object.assign(tabs.get(id), {pendingUrl: url, status: 'loading'});
      loadLater(id, url);
      return {...tabs.get(id)};
    },
    query: async ({groupId} = {}) => [...tabs.values()].filter(tab => groupId === undefined || tab.groupId === groupId).map(tab => ({...tab})),
    group: async ({tabIds, groupId, createProperties}) => {
      const id = groupId ?? nextGroup++;
      if (!groups.has(id)) groups.set(id, {id, windowId: createProperties.windowId});
      for (const tabId of tabIds) tabs.get(tabId).groupId = id;
      note('group', tabIds, id);
      return id;
    },
    ungroup: async tabIds => {
      for (const tabId of tabIds) tabs.get(tabId).groupId = -1;
      note('ungroup', tabIds);
    },
    onCreated: event('created'),
    onRemoved: event('removed'),
    onUpdated: event('updated'),
  },
  tabGroups: {
    get: async id => {
      if (!groups.has(id)) throw new Error(`no group ${id}`);
      return groups.get(id);
    },
    update: async (id, change) => {
      Object.assign(groups.get(id), change);
      note('groupUpdate', id, change);
    },
  },
  storage: {session: {
    get: async defaults => structuredClone({...defaults, ...storage}),
    set: async values => Object.assign(storage, structuredClone(values)),
  }},
  action: {
    setBadgeText: async ({text}) => note('badge', text),
    setBadgeBackgroundColor: async () => {},
    setTitle: async ({title}) => note('title', title),
  },
};

const fetch = async name => ({text: async () => fs.readFileSync(path.join(dir, name), 'utf8')});
vm.runInContext(fs.readFileSync(path.join(dir, 'background.js'), 'utf8'),
  vm.createContext({chrome, fetch, navigator: {userAgent: 'node'}, setTimeout, performance, URL, console}));

const quiet = () => new Promise(resolve => setTimeout(resolve, 300));
const call = (id, tabId, op, args = {}) => listeners.message({t: 'call', id, tabId, op, args});
const scenarios = {groups: async () => {
  listeners.message({t: 'status', state: 'acting'});
  call(1, 9, 'click', {element: 1, guard: 'g'});
  call(2, 9, 'snapshot');
  await quiet();
  call(3, 3, 'click', {element: 1});
  call(4, 5, 'click', {element: 1});
  call(5, 6, 'click', {element: 1});
  await quiet();
  tabs.get(6).groupId = -1;
  listeners.updated(6, {groupId: -1}, {...tabs.get(6)});
  await quiet();
  call(6, 6, 'click', {element: 1});
  await quiet();
  listeners.message({t: 'status', state: 'idle'});
  await quiet();
  const grouped = structuredClone(storage);
  listeners.disconnect();
  await quiet();
  const restored = structuredClone(storage);
  await new Promise(resolve => setTimeout(resolve, 1000));
  listeners.message({t: 'status', state: 'acting'});
  call(7, 9, 'click', {element: 1});
  await quiet();
  return {grouped, restored};
}, orphan: async () => {
  popupOpener = undefined;
  call(1, 0, 'open', {url: 'https://stays.test/new'});
  await quiet();
  const release = {method: 'Input.dispatchMouseEvent', params: {type: 'mouseReleased', x: 40, y: 60, button: 'left'}};
  listeners.message({t: 'call', id: 2, tabId: 20, op: 'cdp', args: {calls: [release], act: true}});
  await new Promise(resolve => setTimeout(resolve, 3000));
  call(3, 20, 'click', {element: 1, guard: 'g'});
  await quiet();
  await new Promise(resolve => setTimeout(resolve, 3000));
  tabs.set(40, {id: 40, windowId: 1, url: 'https://news.test/', title: 'News', pinned: false, groupId: -1});
  listeners.created({...tabs.get(40)});
  await quiet();
  return {};
}, keep: async () => {
  call(1, 0, 'open', {url: 'https://stays.test/new'});
  await quiet();
  const click = {method: 'Input.dispatchMouseEvent', params: {type: 'mouseReleased', x: 40, y: 60, button: 'left'}};
  for (const [id, act] of [[2, 'link'], [3, 'windowOpen']]) {
    release = act;
    listeners.message({t: 'call', id, tabId: 20, op: 'cdp', args: {calls: [click], act: true}});
    await quiet();
  }
  call(4, 9, 'snapshot');
  await quiet();
  return {};
}, cursor: async () => {
  const clicks = [['mouseMoved', 'none'], ['mousePressed', 'left'], ['mouseReleased', 'left']].map(([type, button]) => ({method: 'Input.dispatchMouseEvent', params: {type, x: 40, y: 60, button}}));
  listeners.message({t: 'call', id: 1, tabId: 9, op: 'cdp', args: {calls: clicks, act: true}, cursor: true});
  await quiet();
  call(2, 0, 'open', {url: 'https://stays.test/new'});
  await quiet();
  listeners.message({t: 'call', id: 3, tabId: 20, op: 'cdp', args: {calls: clicks, act: true}, cursor: true});
  await quiet();
  listeners.message({t: 'call', id: 4, tabId: 20, op: 'cdp', args: {calls: clicks, act: true}});
  await quiet();
  listeners.message({t: 'call', id: 5, tabId: 20, op: 'cdp', args: {calls: [], act: true, point: {x: 110, y: 50, label: 'tofu typing'}}, cursor: true});
  await quiet();
  listeners.message({t: 'call', id: 6, tabId: 20, op: 'navigate', args: {url: 'https://www.airbnb.test/rooms/1'}, cursor: true});
  await quiet();
  listeners.disconnect();
  await quiet();
  return {};
}, open: async () => {
  call(1, 0, 'open', {url: 'https://stays.test/new'});
  await quiet();
  call(2, 20, 'click', {element: 1, guard: 'g'});
  await quiet();
  call(3, 20, 'navigate', {url: 'https://stays.test/other'});
  call(4, 9, 'navigate', {url: 'https://stays.test/other'});
  await quiet();
  return {};
}};
(async () => {
  await quiet();
  const kept = await scenarios[scenario]();
  console.log(JSON.stringify({heard, posted, ...kept}));
  process.exit(0);
})();
