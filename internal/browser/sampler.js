({watch, events}) => {
  const HOLD_MS = 20;
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
  let shown = JSON.stringify(readAll()), fresh = false;
  const tick = ts => {
    if (!motion.running) return;
    const elements = readAll(), now = JSON.stringify(elements);
    if (fresh && now !== shown) for (const until = performance.now() + HOLD_MS; performance.now() < until;);
    fresh = now !== shown;
    shown = now;
    motion.samples.push({ts, elements});
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
  return true;
}
