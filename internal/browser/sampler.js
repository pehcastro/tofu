({watch, events}) => {
  const SCREENCAST_PERIOD_MS = 10, HOLD_PAINTS = 1.5, REPAINT_PAINTS = 4;
  const motion = window.__tofuMotion = {samples: [], trigger: null, running: true};
  const stamp = event => {
    motion.trigger = motion.trigger || {event: event.type, timeStamp: event.timeStamp, wallMs: performance.timeOrigin + event.timeStamp};
  };
  for (const type of events) document.addEventListener(type, stamp, true);
  const read = w => {
    const el = document.querySelector(w.selector);
    if (!el) return null;
    const rect = el.getBoundingClientRect(), style = getComputedStyle(el);
    const seen = {x: rect.x, y: rect.y, width: rect.width, height: rect.height, opacity: Number(style.opacity),
      display: style.display, visibility: style.visibility, hidden: el.hidden};
    if (w.attributes) seen.attributes = Object.fromEntries(w.attributes.map(name => [name, el.getAttribute(name) ?? '']));
    if (w.styles) seen.styles = Object.fromEntries(w.styles.map(name => [name, style.getPropertyValue(name)]));
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
  const tick = ts => {
    if (!motion.running) return repaint.remove();
    const elements = readAll(), now = flat(elements), moves = {};
    let moving = false, turned = false, changed = false;
    for (const key of new Set([...Object.keys(last), ...Object.keys(now)])) {
      moves[key] = direction(last[key], now[key]);
      moving ||= Boolean(lastMoves[key]);
      changed ||= moves[key] !== 0;
      turned ||= moves[key] !== 0 && moves[key] !== (lastMoves[key] ?? 0);
    }
    if (lastTs) paintMs = Math.min(paintMs, ts - lastTs);
    const fast = paintMs < SCREENCAST_PERIOD_MS;
    if (fast && turnedLast && changed) for (const until = performance.now() + HOLD_PAINTS * paintMs; performance.now() < until;);
    turnedLast = fast && moving && turned;
    if (turnedLast) repaint.animate([{opacity: 0.001}, {opacity: 0.002}], {duration: REPAINT_PAINTS * paintMs});
    last = now;
    lastMoves = moves;
    lastTs = ts;
    motion.samples.push({ts, elements});
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
  return true;
}
