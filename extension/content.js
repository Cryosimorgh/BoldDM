(() => {
  if (window.__boltDMSimilarDownloadCollectorLoaded) return;
  window.__boltDMSimilarDownloadCollectorLoaded = true;

  const INTERACTIVE_SELECTOR = [
    'a[href]',
    'button',
    '[role="button"]',
    'input[type="button"]',
    'input[type="submit"]',
    '[onclick]',
    '[data-href]',
    '[data-url]',
    '[data-download]',
    '[data-download-url]',
    '[data-link]'
  ].join(',');

  const URL_ATTRS = [
    'href',
    'data-href',
    'data-url',
    'data-download',
    'data-download-url',
    'data-link',
    'data-src',
    'formaction'
  ];

  const state = {
    active: false,
    hovered: null,
    hoveredRestore: null,
    matchRestores: new Map(),
    panelHost: null,
    matches: [],
    urls: []
  };

  chrome.runtime.onMessage.addListener((message) => {
    if (message?.type === 'BOLTDM_START_PICKER') startPicker();
  });

  function startPicker() {
    cleanup();
    state.active = true;
    document.addEventListener('pointermove', onPointerMove, true);
    document.addEventListener('click', onPickClick, true);
    document.addEventListener('keydown', onKeyDown, true);
    showInstruction();
  }

  function onKeyDown(event) {
    if (event.key === 'Escape') cleanup();
  }

  function onPointerMove(event) {
    if (!state.active || isCollectorUi(event.target)) return;
    const target = normalizeTarget(event.target);
    if (target === state.hovered) return;
    restoreHover();
    state.hovered = target;
    if (!target) return;

    state.hoveredRestore = {
      outline: target.style.outline,
      outlineOffset: target.style.outlineOffset,
      cursor: target.style.cursor
    };
    target.style.setProperty('outline', '2px solid #d7b477', 'important');
    target.style.setProperty('outline-offset', '2px', 'important');
    target.style.setProperty('cursor', 'crosshair', 'important');
  }

  function onPickClick(event) {
    if (!state.active || isCollectorUi(event.target)) return;

    event.preventDefault();
    event.stopPropagation();
    event.stopImmediatePropagation();

    const sample = normalizeTarget(event.target);
    if (!sample) return;

    stopPickerListeners();
    restoreHover();

    const result = findMatches(sample);
    state.matches = result.matches;
    state.urls = result.urls;
    highlightMatches(result.matches);
    showResults(sample, result);
  }

  function normalizeTarget(node) {
    if (!(node instanceof Element)) return null;
    return node.closest(INTERACTIVE_SELECTOR) || node;
  }

  function stableClasses(element) {
    return [...element.classList].filter((name) => {
      if (!name || name.length > 80) return false;
      return !/^(active|selected|hover|focus|focused|disabled|loading|open|closed|current|pressed)$/i.test(name);
    });
  }

  function attrKeySet(element) {
    return new Set(
      [...element.attributes]
        .map((attr) => attr.name)
        .filter((name) => name.startsWith('data-') || ['role', 'type', 'name', 'download', 'aria-label'].includes(name))
    );
  }

  function jaccard(a, b) {
    const A = new Set(a);
    const B = new Set(b);
    if (A.size === 0 && B.size === 0) return 1;
    const union = new Set([...A, ...B]);
    let intersection = 0;
    for (const value of A) if (B.has(value)) intersection += 1;
    return union.size ? intersection / union.size : 0;
  }

  function normalizedText(element) {
    return (element.innerText || element.textContent || element.getAttribute('aria-label') || '')
      .replace(/\s+/g, ' ')
      .trim()
      .toLowerCase()
      .slice(0, 120);
  }

  function dimensionsSimilarity(a, b) {
    const ar = a.getBoundingClientRect();
    const br = b.getBoundingClientRect();
    if (!ar.width || !ar.height || !br.width || !br.height) return 0.5;
    const widthRatio = Math.min(ar.width, br.width) / Math.max(ar.width, br.width);
    const heightRatio = Math.min(ar.height, br.height) / Math.max(ar.height, br.height);
    return (widthRatio + heightRatio) / 2;
  }

  function similarity(sample, candidate) {
    if (!(candidate instanceof Element)) return 0;

    let points = 0;
    let weight = 0;
    const add = (w, score) => {
      weight += w;
      points += w * score;
    };

    add(0.18, sample.tagName === candidate.tagName ? 1 : 0);

    const sampleClasses = stableClasses(sample);
    const candidateClasses = stableClasses(candidate);
    if (sampleClasses.length || candidateClasses.length) {
      add(0.30, jaccard(sampleClasses, candidateClasses));
    }

    const sampleRole = sample.getAttribute('role') || '';
    const candidateRole = candidate.getAttribute('role') || '';
    const sampleType = sample.getAttribute('type') || '';
    const candidateType = candidate.getAttribute('type') || '';
    if (sampleRole || sampleType) {
      add(0.08, (sampleRole === candidateRole && sampleType === candidateType) ? 1 : 0.35);
    }

    add(0.12, jaccard(attrKeySet(sample), attrKeySet(candidate)));

    const sp = sample.parentElement;
    const cp = candidate.parentElement;
    if (sp && cp) {
      let parentScore = sp.tagName === cp.tagName ? 0.45 : 0;
      parentScore += 0.55 * jaccard(stableClasses(sp), stableClasses(cp));
      add(0.14, parentScore);
    }

    const sampleText = normalizedText(sample);
    const candidateText = normalizedText(candidate);
    if (sampleText) {
      const exact = sampleText === candidateText;
      const bothDownloadish = /download|save|get|direct/i.test(sampleText) && /download|save|get|direct/i.test(candidateText);
      add(0.07, exact ? 1 : (bothDownloadish ? 0.72 : 0.15));
    }

    add(0.07, dimensionsSimilarity(sample, candidate));

    const sampleHasUrl = extractUrls(sample).length > 0;
    const candidateHasUrl = extractUrls(candidate).length > 0;
    add(0.10, sampleHasUrl === candidateHasUrl ? 1 : 0);

    return weight ? points / weight : 0;
  }

  function buildCandidatePool(sample) {
    const candidates = new Set();
    for (const element of document.getElementsByTagName(sample.tagName)) candidates.add(element);
    for (const className of stableClasses(sample).slice(0, 4)) {
      try {
        for (const element of document.querySelectorAll(`.${CSS.escape(className)}`)) candidates.add(element);
      } catch {}
    }
    for (const element of document.querySelectorAll(INTERACTIVE_SELECTOR)) candidates.add(element);
    return [...candidates];
  }

  function findMatches(sample) {
    const pool = buildCandidatePool(sample);
    const scored = [];
    for (const candidate of pool) {
      if (!candidate.isConnected || isCollectorUi(candidate)) continue;
      const urls = extractUrls(candidate);
      if (urls.length === 0) continue;
      const score = candidate === sample ? 1 : similarity(sample, candidate);
      if (score >= 0.62) scored.push({ element: candidate, score, urls });
    }
    scored.sort((a, b) => b.score - a.score);
    if (scored.length <= 1) {
      const sampleClasses = stableClasses(sample);
      if (sampleClasses.length) {
        for (const candidate of pool) {
          if (candidate === sample || !candidate.isConnected || isCollectorUi(candidate)) continue;
          const sameClasses = jaccard(sampleClasses, stableClasses(candidate)) >= 0.80;
          if (!sameClasses) continue;
          const urls = extractUrls(candidate);
          if (!urls.length) continue;
          if (!scored.some((entry) => entry.element === candidate)) {
            scored.push({ element: candidate, score: similarity(sample, candidate), urls });
          }
        }
      }
    }
    const urlSet = new Set();
    for (const entry of scored) for (const url of entry.urls) urlSet.add(url);
    return { matches: scored, urls: [...urlSet] };
  }

  function extractUrls(element) {
    const found = new Set();
    const inspect = (node) => {
      if (!(node instanceof Element)) return;
      for (const attr of URL_ATTRS) {
        const value = node.getAttribute(attr);
        if (value) addResolved(found, value);
      }
      if (node instanceof HTMLAnchorElement && node.href) addResolved(found, node.href);
      if ('formAction' in node && node.formAction) addResolved(found, node.formAction);
      const onclick = node.getAttribute('onclick');
      if (onclick) extractUrlsFromScript(onclick, found);
    };
    inspect(element);
    const closestAnchor = element.closest('a[href]');
    if (closestAnchor && closestAnchor !== element) inspect(closestAnchor);
    for (const child of element.querySelectorAll('a[href], [data-href], [data-url], [data-download-url], [formaction]')) inspect(child);
    return [...found];
  }

  function extractUrlsFromScript(script, found) {
    const patterns = [
      /(?:window\.)?open\s*\(\s*['"]([^'"]+)['"]/gi,
      /(?:window\.)?location(?:\.href)?\s*=\s*['"]([^'"]+)['"]/gi,
      /['"]((?:https?:\/\/|ftp:\/\/|\/|\.\/|\.\.\/)[^'"]+)['"]/gi
    ];
    for (const pattern of patterns) {
      let match;
      while ((match = pattern.exec(script)) !== null) addResolved(found, match[1]);
    }
  }

  function addResolved(set, raw) {
    const value = String(raw).trim();
    if (!value || value === '#' || /^javascript:/i.test(value) || /^mailto:/i.test(value)) return;
    try {
      const url = new URL(value, document.baseURI);
      if (['http:', 'https:', 'ftp:'].includes(url.protocol)) set.add(url.href);
    } catch {}
  }

  function highlightMatches(matches) {
    clearMatchHighlights();
    for (const entry of matches) {
      const element = entry.element;
      state.matchRestores.set(element, { outline: element.style.outline, outlineOffset: element.style.outlineOffset, backgroundColor: element.style.backgroundColor });
      element.style.setProperty('outline', '2px solid #c9a46a', 'important');
      element.style.setProperty('outline-offset', '2px', 'important');
    }
  }

  function clearMatchHighlights() {
    for (const [element, restore] of state.matchRestores) {
      if (!element?.isConnected) continue;
      element.style.outline = restore.outline;
      element.style.outlineOffset = restore.outlineOffset;
      element.style.backgroundColor = restore.backgroundColor;
    }
    state.matchRestores.clear();
  }

  function restoreHover() {
    if (state.hovered && state.hoveredRestore && state.hovered.isConnected) {
      state.hovered.style.outline = state.hoveredRestore.outline;
      state.hovered.style.outlineOffset = state.hoveredRestore.outlineOffset;
      state.hovered.style.cursor = state.hoveredRestore.cursor;
    }
    state.hovered = null;
    state.hoveredRestore = null;
  }

  function showInstruction() {
    const { shadow } = makePanel();
    const box = document.createElement('div');
    box.className = 'box';
    const title = document.createElement('strong');
    title.textContent = 'Select one download button';
    const detail = document.createElement('span');
    detail.textContent = 'Hover to inspect. Click the example button. Esc cancels.';
    box.append(title, detail);
    shadow.append(box);
  }

  function showResults(sample, result) {
    const { shadow } = makePanel();
    shadow.textContent = '';
    const box = document.createElement('div');
    box.className = 'box';
    const title = document.createElement('strong');
    title.textContent = `Found ${result.matches.length} matching button${result.matches.length === 1 ? '' : 's'}`;
    const detail = document.createElement('span');
    detail.textContent = `${result.urls.length} unique download URL${result.urls.length === 1 ? '' : 's'} found.`;
    const sampleInfo = document.createElement('span');
    sampleInfo.className = 'muted';
    sampleInfo.textContent = `Example: <${sample.tagName.toLowerCase()}> ${normalizedText(sample).slice(0, 48) || '(no text)'}`;
    const actions = document.createElement('div');
    actions.className = 'actions';
    const send = button('Send to BoltDM', 'primary');
    send.disabled = result.urls.length === 0;
    send.addEventListener('click', () => submitBatch(send, detail));
    const copy = button('Copy URLs');
    copy.disabled = result.urls.length === 0;
    copy.addEventListener('click', async () => {
      try { await navigator.clipboard.writeText(result.urls.join('\n')); detail.textContent = `Copied ${result.urls.length} URLs.`; }
      catch { detail.textContent = 'Clipboard access was blocked by this page/browser.'; }
    });
    const repick = button('Select again');
    repick.addEventListener('click', startPicker);
    const cancel = button('Cancel');
    cancel.addEventListener('click', cleanup);
    actions.append(send, copy, repick, cancel);
    const details = document.createElement('details');
    const summary = document.createElement('summary');
    summary.textContent = 'Preview URLs';
    const list = document.createElement('div');
    list.className = 'url-list';
    for (const url of result.urls.slice(0, 25)) {
      const line = document.createElement('div');
      line.textContent = url;
      list.append(line);
    }
    if (result.urls.length > 25) {
      const more = document.createElement('div');
      more.textContent = `…and ${result.urls.length - 25} more`;
      list.append(more);
    }
    details.append(summary, list);
    box.append(title, detail, sampleInfo, actions, details);
    shadow.append(box);
  }

  async function submitBatch(sendButton, detailNode) {
    sendButton.disabled = true;
    sendButton.textContent = 'Sending…';
    detailNode.textContent = `Sending ${state.urls.length} URLs to BoltDM…`;
    try {
      const response = await chrome.runtime.sendMessage({ type: 'BOLTDM_DOWNLOAD_BATCH', urls: state.urls, pageUrl: location.href, userAgent: navigator.userAgent });
      if (!response) throw new Error('No response from extension service worker.');
      if (response.failed?.length) {
        detailNode.textContent = `Sent ${response.started}/${response.requested}. ${response.failed.length} failed.`;
        sendButton.disabled = false;
        sendButton.textContent = 'Retry';
      } else {
        detailNode.textContent = `Sent ${response.started} download${response.started === 1 ? '' : 's'}.`;
        sendButton.textContent = 'Sent';
        setTimeout(cleanup, 1800);
      }
    } catch (error) {
      detailNode.textContent = error?.message || String(error);
      sendButton.disabled = false;
      sendButton.textContent = 'Retry';
    }
  }

  function button(text, className = '') {
    const el = document.createElement('button');
    el.type = 'button';
    el.textContent = text;
    if (className) el.className = className;
    return el;
  }

  function makePanel() {
    if (state.panelHost?.isConnected) state.panelHost.remove();
    const host = document.createElement('div');
    host.dataset.boltDMCollectorUi = '1';
    host.style.setProperty('position', 'fixed', 'important');
    host.style.setProperty('top', '16px', 'important');
    host.style.setProperty('right', '16px', 'important');
    host.style.setProperty('z-index', '2147483647', 'important');
    host.style.setProperty('width', 'min(430px, calc(100vw - 32px))', 'important');
    const shadow = host.attachShadow({ mode: 'open' });
    const style = document.createElement('style');
    style.textContent = `
      :host { all: initial; }
      * { box-sizing: border-box; }
      .box { display:flex; flex-direction:column; gap:9px; padding:16px; border:1px solid #343138; border-radius:13px; background:rgba(13,13,16,.97); color:#f2efe8; box-shadow:0 24px 70px rgba(0,0,0,.52); backdrop-filter:blur(18px); font:12px/1.5 "Segoe UI Variable","Segoe UI",system-ui,sans-serif; }
      .box:before { content:"BOLTDM  /  BROWSER COMPANION"; color:#75644b; font-size:8px; letter-spacing:.16em; margin-bottom:1px; }
      strong { font-size:14px; font-weight:600; letter-spacing:.01em; }
      span { color:#aaa6ab; }
      .muted { color:#6f6b72; font-size:10px; }
      .actions { display:flex; flex-wrap:wrap; gap:6px; margin-top:4px; }
      button { height:32px; border:1px solid #333137; border-radius:8px; padding:0 10px; background:#121216; color:#d7d3cd; cursor:pointer; font:11px "Segoe UI Variable","Segoe UI",system-ui,sans-serif; }
      button:hover { border-color:#4b474f; background:#17171b; }
      button.primary { background:#c9a46a; border-color:#c9a46a; color:#17120b; font-weight:700; }
      button.primary:hover { background:#dfbf8b; border-color:#dfbf8b; }
      button:disabled { opacity:.4; cursor:default; }
      details { margin-top:3px; border-top:1px solid #232126; padding-top:9px; }
      summary { cursor:pointer; user-select:none; color:#88848b; font-size:10px; }
      .url-list { margin-top:9px; max-height:180px; overflow:auto; padding:9px; border:1px solid #252329; border-radius:8px; background:#09090b; color:#9e9aa0; font:10px/1.55 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; word-break:break-all; }
      .url-list > div + div { margin-top:5px; }
    `;
    shadow.append(style);
    document.documentElement.append(host);
    state.panelHost = host;
    return { host, shadow };
  }

  function isCollectorUi(node) { return node instanceof Node && state.panelHost && (node === state.panelHost || state.panelHost.contains(node)); }
  function stopPickerListeners() {
    state.active = false;
    document.removeEventListener('pointermove', onPointerMove, true);
    document.removeEventListener('click', onPickClick, true);
    document.removeEventListener('keydown', onKeyDown, true);
  }
  function cleanup() {
    stopPickerListeners();
    restoreHover();
    clearMatchHighlights();
    if (state.panelHost?.isConnected) state.panelHost.remove();
    state.panelHost = null;
    state.matches = [];
    state.urls = [];
  }
})();
