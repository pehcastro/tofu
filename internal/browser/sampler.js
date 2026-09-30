({watch, events}) => {
  const SCREENCAST_PERIOD_MS = 10, HOLD_PAINTS = 1.5;
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
  let last = flat(readAll()), lastMoves = {}, lastTs = 0, paintMs = Infinity;
  const tick = ts => {
    if (!motion.running) return;
    const elements = readAll(), now = flat(elements), moves = {};
    let moving = false, turned = false;
    for (const key of new Set([...Object.keys(last), ...Object.keys(now)])) {
      moves[key] = direction(last[key], now[key]);
      moving ||= Boolean(lastMoves[key]);
      turned ||= moves[key] !== 0 && moves[key] !== (lastMoves[key] ?? 0);
    }
    if (lastTs) paintMs = Math.min(paintMs, ts - lastTs);
    if (moving && turned && paintMs < SCREENCAST_PERIOD_MS) for (const until = performance.now() + HOLD_PAINTS * paintMs; performance.now() < until;);
    last = now;
    lastMoves = moves;
    lastTs = ts;
    motion.samples.push({ts, elements});
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
  return true;
}
