const API = 'http://127.0.0.1:17654';
const KEY = 'b7a9f4e6b8634a9bbf95c7a84c2d3e71';

function supported(value) {
  try { const u = new URL(value); return u.protocol === 'http:' || u.protocol === 'https:'; }
  catch { return false; }
}

async function cookieHeader(url) {
  try {
    const cookies = await chrome.cookies.getAll({ url });
    return cookies.map(c => `${c.name}=${c.value}`).join('; ');
  } catch { return ''; }
}

async function sendBatch(message) {
  const urls = [...new Set(Array.isArray(message.urls) ? message.urls : [])].filter(supported);
  const downloads = [];
  for (const url of urls) {
    const headers = {};
    const cookie = await cookieHeader(url);
    if (cookie) headers.Cookie = cookie;
    if (message.pageUrl) headers.Referer = message.pageUrl;
    if (message.userAgent) headers['User-Agent'] = message.userAgent;
    const request = { url, headers };
    if (message.engine) request.engine = message.engine;
    downloads.push(request);
  }
  const response = await fetch(`${API}/api/v1/downloads`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-BoltDM-Key': KEY },
    body: JSON.stringify({ downloads })
  });
  if (!response.ok) throw new Error(`BoltDM returned HTTP ${response.status}: ${await response.text()}`);
  return response.json();
}

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message?.type === 'BOLTDM_DOWNLOAD_BATCH') {
    sendBatch(message).then(r => sendResponse({ ok: true, requested: message.urls?.length || 0, started: r.accepted, failed: r.errors || [] }))
      .catch(e => sendResponse({ ok: false, requested: message.urls?.length || 0, started: 0, failed: [{ error: e.message }] }));
    return true;
  }
  if (message?.type === 'BOLTDM_HEALTH') {
    fetch(`${API}/api/v1/health`).then(r => r.json()).then(r => sendResponse({ ok: !!r.ok })).catch(() => sendResponse({ ok: false }));
    return true;
  }
});
