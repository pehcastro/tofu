({watch, events, cap, nameChars}) => {
  const SCREENCAST_PERIOD_MS = 10, SCREENCAST_GAP_MS = 20, REPAINT_PAINTS = 4;
  const motion = window.__tofuMotion = {samples: [], trigger: null, running: true, discovered: [], seen: 0};
  const stamp = event => {
    motion.trigger = motion.trigger || {event: event.type, timeStamp: event.timeStamp, wallMs: performance.timeOrigin + event.timeStamp};
  };
  for (const type of events) document.addEventListener(type, stamp, true);
  const keys = new Map();
  const selectorOf = el => el.id ? '#' + CSS.escape(el.id)
    : `${el.parentElement === document.body ? 'body' : selectorOf(el.parentElement)} > ${el.tagName.toLowerCase()}:nth-child(${[...el.parentElement.children].indexOf(el) + 1})`;
  const adopt = el => {
    motion.seen++;
    if (watch.length >= cap || keys.has(el)) return;
    let up = el.parentElement;
    while (up && !keys.has(up)) up = up.parentElement;
    keys.set(el, watch.length);
    motion.discovered.push({selector: selectorOf(el), role: el.getAttribute('role') || el.tagName.toLowerCase(),
      name: (el.getAttribute('aria-label') || el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, nameChars), parent: up ? keys.get(up) : -1});
    watch.push({name: String(watch.length), el, text: true});
  };
  const shows = el => {
    const rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight &&
      getComputedStyle(el).visibility !== 'hidden';
  };
  let arrivals = null;
  if (!watch.length) {
    for (const el of document.body.querySelectorAll('*')) if (shows(el)) adopt(el);
    arrivals = new MutationObserver(records => { for (const record of records) for (const el of record.addedNodes) if (el.nodeType === 1) adopt(el); });
    arrivals.observe(document.body, {childList: true, subtree: true});
  }
  const read = w => {
    const el = w.el || document.querySelector(w.selector);
    if (!el) return null;
    const rect = el.getBoundingClientRect(), style = getComputedStyle(el);
    const seen = {x: rect.x, y: rect.y, width: rect.width, height: rect.height, opacity: Number(style.opacity),
      display: style.display, visibility: style.visibility, hidden: el.hidden};
    if (w.attributes) seen.attributes = Object.fromEntries(w.attributes.map(name => [name, el.getAttribute(name) ?? '']));
    if (w.styles) seen.styles = Object.fromEntries(w.styles.map(name => [name, style.getPropertyValue(name)]));
    if (w.text) seen.text = [...el.childNodes].filter(node => node.nodeType === 3).map(node => node.data).join('').trim();
    return seen;
  };
  const readAll = () => Object.fromEntries(watch.map(w => [w.name, read(w)]));
  const flat = (value, key = '', into = {}) => {
    if (value && typeof value === 'object') for (const [name, inner] of Object.entries(value)) flat(inner, `${key}.${name}`, into);
    else into[key] = value;
    return into;
  };
  const direction = (from, to) => typeof from === 'number' && typeof to === 'number' ? Math.sign(to - from) : Number(from !== to);
  const repaint = document.createElement('div');
  repaint.style.cssText = 'position: fixed; left: 0; top: 0; width: 1px; height: 1px; background: #000; opacity: 0; pointer-events: none;';
  document.documentElement.append(repaint);
  let last = flat(readAll()), lastMoves = {}, lastTs = 0, paintMs = Infinity, turnedLast = false;
  const sent = {};
  const tick = ts => {
    if (!motion.running) {
      arrivals?.disconnect();
      return repaint.remove();
    }
    const elements = readAll(), now = flat(elements), moves = {}, changed = {};
    let moving = false, turned = false, changing = false;
    for (const key of new Set([...Object.keys(last), ...Object.keys(now)])) {
      moves[key] = direction(last[key], now[key]);
      moving ||= Boolean(lastMoves[key]);
      changing ||= moves[key] !== 0;
      turned ||= moves[key] !== 0 && moves[key] !== (lastMoves[key] ?? 0);
    }
    if (lastTs) paintMs = Math.min(paintMs, ts - lastTs);
    const fast = paintMs < SCREENCAST_PERIOD_MS;
    if (fast && turnedLast && changing) for (const until = performance.now() + SCREENCAST_GAP_MS; performance.now() < until;);
    turnedLast = fast && moving && turned;
    if (turnedLast) repaint.animate([{opacity: 0.001}, {opacity: 0.002}], {duration: REPAINT_PAINTS * paintMs});
    for (const [name, seen] of Object.entries(elements)) {
      const json = JSON.stringify(seen);
      if (json !== sent[name]) {
        changed[name] = seen;
        sent[name] = json;
      }
    }
    last = now;
    lastMoves = moves;
    lastTs = ts;
    motion.samples.push({ts, elements: changed});
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
  return true;
}
