(request => {
  const ELEMENT_CEILING = 250;
  const TEXT_CEILING = 6000;
  const SCROLL_FRACTION = 0.8;
  const SCROLL_SLACK_PX = 2;
  if (!document.body) return {stale: 'the page has no body yet'};
  const cache = window.__tofu ||= {ids: new WeakMap(), nodes: new Map(), next: 1};
  const identity = e => {
    if (!cache.ids.has(e)) cache.ids.set(e, cache.next++);
    const id = cache.ids.get(e);
    cache.nodes.set(id, e);
    return id;
  };
  for (const [id, e] of cache.nodes) if (!e.isConnected) cache.nodes.delete(id);

  const excluded = ['password', 'file', 'hidden'];
  const safe = e => !excluded.includes(e.type);
  const visible = e => !e.closest('[aria-hidden="true"],[inert]') &&
    e.checkVisibility({checkOpacity: true, checkVisibilityCSS: true});
  const disabled = e => e.matches(':disabled') || e.closest('[aria-disabled="true"]') !== null;
  const readonly = e => e.readOnly === true || e.getAttribute('aria-readonly') === 'true';
  const centre = e => {
    const r = e.getBoundingClientRect(), x = r.x + r.width / 2, y = r.y + r.height / 2;
    return r.width > 0 && r.height > 0 && x >= 0 && y >= 0 && x < innerWidth && y < innerHeight ? {x, y} : null;
  };
  const name = (e, seen = new Set()) => {
    if (!e || seen.has(e)) return '';
    seen.add(e);
    const referenced = (e.getAttribute('aria-labelledby') || '').split(/\s+/)
      .map(id => name(document.getElementById(id), seen)).filter(Boolean).join(' ');
    return referenced || e.getAttribute('aria-label') ||
      [...(e.labels || [])].map(label => name(label, seen)).filter(Boolean).join(' ') ||
      (['button', 'submit', 'reset'].includes(e.type) ? e.value : '') || e.getAttribute('alt') ||
      (e.tagName === 'INPUT' ? '' : [...e.childNodes].map(n => n.nodeType === Node.TEXT_NODE ? n.textContent :
        n.nodeType === Node.ELEMENT_NODE && n.getAttribute('aria-hidden') !== 'true' ? name(n, seen) : '').join(' ').trim()) ||
      e.getAttribute('title') || e.getAttribute('placeholder') || '';
  };
  const roles = ['button', 'link', 'checkbox', 'radio', 'switch', 'tab', 'menuitem', 'menuitemradio',
    'option', 'gridcell', 'combobox', 'textbox', 'searchbox', 'spinbutton'];
  const typeable = ['textbox', 'searchbox', 'spinbutton', 'combobox'];
  const selector = 'a[href],button,input,textarea,select,summary,[contenteditable="true"],' +
    roles.map(role => '[role="' + role + '"]').join(',');
  const role = e => {
    if (e.tagName === 'SELECT') return 'select';
    const explicit = e.getAttribute('role');
    if (roles.includes(explicit)) return explicit;
    if (e.tagName === 'BUTTON' || e.tagName === 'SUMMARY') return 'button';
    if (e.tagName === 'A') return 'link';
    if (e.tagName === 'TEXTAREA' || e.isContentEditable) return 'textbox';
    if (e.tagName !== 'INPUT') return null;
    if (['checkbox', 'radio'].includes(e.type)) return e.type;
    if (['button', 'submit', 'reset', 'image'].includes(e.type)) return 'button';
    if (e.type === 'search') return 'searchbox';
    if (e.type === 'number') return 'spinbutton';
    if (['text', 'email', 'url', 'tel'].includes(e.type)) return 'textbox';
    return null;
  };
  const hash = text => {
    let a = 0xdeadbeef, b = 0x41c6ce57;
    for (let i = 0; i < text.length; i++) {
      const c = text.charCodeAt(i);
      a = Math.imul(a ^ c, 2654435761);
      b = Math.imul(b ^ c, 1597334677);
    }
    a = Math.imul(a ^ (a >>> 16), 2246822507) ^ Math.imul(b ^ (b >>> 13), 3266489909);
    b = Math.imul(b ^ (b >>> 16), 2246822507) ^ Math.imul(a ^ (a >>> 13), 3266489909);
    return (b >>> 0).toString(16).padStart(8, '0') + (a >>> 0).toString(16).padStart(8, '0');
  };

  const elements = [];
  for (const e of document.querySelectorAll(selector)) {
    const kind = role(e);
    if (!kind || !safe(e) || !visible(e) || disabled(e) || !centre(e)) continue;
    if (kind === 'gridcell' && e.querySelector('button,[role="button"]')) continue;
    const element = {index: identity(e), role: kind, label: name(e) || kind, value: ''};
    if (e.tagName === 'INPUT') element.input = e.type;
    for (const key of ['checked', 'selected', 'expanded']) {
      const value = e.getAttribute('aria-' + key);
      if (value !== null) element[key] = value;
    }
    if (e.type === 'checkbox' || e.type === 'radio') element.checked = String(e.checked);
    if (kind === 'select') {
      element.value = [...e.selectedOptions].map(o => o.label).join(', ');
      element.options = [...e.options].filter(o => !o.selected && !o.disabled && !o.closest('optgroup[disabled]'))
        .map(o => ({label: o.label, value: o.value}));
    } else if ('value' in e) {
      element.value = String(e.value);
    } else if (e.isContentEditable || kind === 'combobox') {
      element.value = e.innerText.trim();
    }
    if (readonly(e)) element.readonly = true;
    elements.push(element);
    if (elements.length === ELEMENT_CEILING) break;
  }

  const words = [], range = document.createRange();
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  let length = 0;
  for (let node = walker.nextNode(); node && length < TEXT_CEILING; node = walker.nextNode()) {
    const value = node.textContent.trim(), parent = node.parentElement;
    if (!value || !parent || parent.closest('script,style,noscript,template') || !visible(parent)) continue;
    range.selectNodeContents(node);
    const r = range.getBoundingClientRect();
    if (r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < innerHeight && r.right > 0 && r.left < innerWidth) {
      words.push(value);
      length += value.length;
    }
  }
  const text = words.join('\n').slice(0, TEXT_CEILING);
  const fields = [...document.querySelectorAll('input,textarea,select')].filter(safe)
    .map(e => [identity(e), e.value, e.checked, e.selectedIndex, e.disabled, e.readOnly]);
  const fingerprint = hash(JSON.stringify([performance.timeOrigin, location.href, scrollX, scrollY, innerWidth,
    innerHeight, document.title, text, elements, fields]));

  if (request.op === 'snapshot') {
    const height = document.documentElement.scrollHeight;
    return {url: location.href, title: document.title, text, fingerprint, elements,
      scroll: {up: scrollY > 0, down: scrollY + innerHeight < height - SCROLL_SLACK_PX}};
  }
  if (request.op === 'fresh') return fingerprint === request.fingerprint;
  if (fingerprint !== request.fingerprint) return {stale: 'the page changed since the snapshot'};
  if (request.op === 'wait') return {};
  if (request.op === 'scroll') {
    scrollBy(0, (request.direction === 'up' ? -1 : 1) * innerHeight * SCROLL_FRACTION);
    return {};
  }
  const target = `element ${request.element}`;
  const e = cache.nodes.get(request.element);
  if (!e?.isConnected) return {stale: `${target} is gone`};
  if (!visible(e)) return {stale: `${target} is not visible`};
  if (disabled(e)) return {stale: `${target} is disabled`};
  const at = centre(e);
  if (!at) return {stale: `${target} has no size on screen`};
  if (!e.contains(document.elementFromPoint(at.x, at.y))) return {stale: `${target} is covered at its centre`};
  if (request.op === 'fill' && (readonly(e) || !typeable.includes(role(e)))) {
    return {error: `${target} is read-only or not a text field`};
  }
  if (request.op !== 'select') return at;
  const offered = e.tagName === 'SELECT' &&
    [...e.options].some(o => o.value === request.value && !o.disabled && !o.closest('optgroup[disabled]'));
  if (!offered) return {stale: `${target} offers no option ${JSON.stringify(request.value)}`};
  e.value = request.value;
  e.dispatchEvent(new Event('input', {bubbles: true}));
  e.dispatchEvent(new Event('change', {bubbles: true}));
  return {};
})
