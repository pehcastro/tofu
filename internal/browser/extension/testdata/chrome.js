const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const [dir, scenario = 'groups'] = process.argv.slice(2);
const LOAD_AFTER_MS = 100;
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
        note('cursor', tabId, params.expression.includes('tofu-cursor-remove') ? 'remove' : 'move');
        return {};
      }
      if (method !== 'Runtime.evaluate') {
        note('input', tabId, method);
        if (tabId === 20 && params.type === 'mouseReleased') {
          tabs.set(21, {id: 21, windowId: 1, openerTabId: 20, url: '', pendingUrl: 'https://stays.test/listing', status: 'loading', pinned: false, groupId: -1});
          listeners.created({...tabs.get(21)});
          loadLater(21, 'https://stays.test/listing');
        }
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
