(request => {
  const ELEMENT_CEILING = 250;
  const TEXT_CEILING = 6000;
  const SCROLL_FRACTION = 0.8;
  const SCROLL_SLACK_PX = 2;
  const SETTLE_FRAMES = 2;
  if (!document.body) return {stale: request.op === 'snapshot' ? 'no body' : 'detached'};
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
  const unnamed = 'style,script,noscript,template';
  const name = (e, seen = new Set()) => {
    if (!e || seen.has(e)) return '';
    seen.add(e);
    const referenced = (e.getAttribute('aria-labelledby') || '').split(/\s+/)
      .map(id => name(document.getElementById(id), seen)).filter(Boolean).join(' ');
    return referenced || e.getAttribute('aria-label') ||
      [...(e.labels || [])].map(label => name(label, seen)).filter(Boolean).join(' ') ||
      (['button', 'submit', 'reset'].includes(e.type) ? e.value : '') || e.getAttribute('alt') ||
      (e.tagName === 'INPUT' ? '' : [...e.childNodes].map(n => n.nodeType === Node.TEXT_NODE ? n.textContent :
        n.nodeType === Node.ELEMENT_NODE && !n.matches(unnamed) && n.getAttribute('aria-hidden') !== 'true' ? name(n, seen) : '')
        .join(' ').trim()) ||
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
  const contexts = new Map();
  const guard = e => {
    const context = e.closest('form,dialog,[role="dialog"],tr,[role="row"]') || e.parentElement;
    if (!contexts.has(context)) contexts.set(context, (context?.innerText ?? '').slice(0, TEXT_CEILING));
    return hash(JSON.stringify([role(e), name(e), e.value ?? null, e.checked ?? null, e.selectedIndex ?? null, readonly(e),
      ...['expanded', 'checked', 'selected'].map(key => e.getAttribute('aria-' + key)), e.getAttribute('href'), contexts.get(context)]));
  };

  if (request.op === 'settle') {
    const e = cache.nodes.get(request.element);
    const listed = () => {
      const ids = (e.getAttribute('aria-controls') || e.getAttribute('aria-owns') || '').split(/\s+/).filter(Boolean);
      const roots = ids.length ? ids.map(id => document.getElementById(id)).filter(Boolean) : [document];
      return roots.some(root => [...root.querySelectorAll('[role="option"]')].some(o => visible(o) && centre(o)));
    };
    return new Promise(resolve => {
      let frames = 0;
      setTimeout(resolve, request.wait, {});
      const tick = () => ++frames >= SETTLE_FRAMES && (!request.combobox || !e?.isConnected || listed()) ?
        resolve({}) : requestAnimationFrame(tick);
      requestAnimationFrame(tick);
    });
  }

  if (request.op === 'snapshot') {
    const elements = [], guards = {}, names = {};
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
      if (typeable.includes(kind)) {
        names[element.index] = ['placeholder', 'name', 'aria-label'].map(key => e.getAttribute(key)).filter(Boolean);
      }
      guards[element.index] = guard(e);
      elements.push(element);
      if (elements.length === ELEMENT_CEILING) break;
    }

    const words = [], range = document.createRange();
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    let length = 0;
    for (let node = walker.nextNode(); node && length < TEXT_CEILING; node = walker.nextNode()) {
      const value = node.textContent.trim(), parent = node.parentElement;
      if (!value || !parent || parent.closest(unnamed) || !visible(parent)) continue;
      range.selectNodeContents(node);
      const r = range.getBoundingClientRect();
      if (r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < innerHeight && r.right > 0 && r.left < innerWidth) {
        words.push(value);
        length += value.length;
      }
    }
    const text = words.join('\n').slice(0, TEXT_CEILING);
    const height = document.documentElement.scrollHeight;
    return {url: location.href, title: document.title, text, elements, guards, names,
      fingerprint: hash(JSON.stringify([location.href, document.title, text, elements])),
      scroll: {up: scrollY > 0, down: scrollY + innerHeight < height - SCROLL_SLACK_PX}};
  }

  if (request.op === 'wait') return {};
  if (request.op === 'scroll') {
    scrollBy(0, (request.direction === 'up' ? -1 : 1) * innerHeight * SCROLL_FRACTION);
    return {};
  }
  const e = cache.nodes.get(request.element);
  if (!e?.isConnected) return {stale: 'detached'};
  if (!visible(e)) return {stale: 'hidden'};
  if (disabled(e) || guard(e) !== request.guard) return {stale: 'changed'};
  const at = centre(e);
  if (!at) return {stale: 'no size'};
  if (!e.contains(document.elementFromPoint(at.x, at.y))) return {stale: 'covered'};
  if (request.op === 'fill' && (readonly(e) || !typeable.includes(role(e)))) {
    return {error: `element ${request.element} is read-only or not a text field`};
  }
  if (request.op !== 'select') return {...at, combobox: role(e) === 'combobox'};
  const offered = e.tagName === 'SELECT' &&
    [...e.options].some(o => o.value === request.value && !o.disabled && !o.closest('optgroup[disabled]'));
  if (!offered) return {stale: 'changed'};
  e.value = request.value;
  e.dispatchEvent(new Event('input', {bubbles: true}));
  e.dispatchEvent(new Event('change', {bubbles: true}));
  return {};
})
