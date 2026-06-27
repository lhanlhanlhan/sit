// SIT Manager GUI — shared helpers. No framework, no build step.
// Config & session live in localStorage:
//   sit_api   = REST base, e.g. https://mgr.example:8443  (no trailing slash, no /api/v1)
//   sit_token = admin bearer token
//   sit_user  = admin username (for display)

const SIT = {
  get apiBase() { return (localStorage.getItem('sit_api') || '').replace(/\/+$/, ''); },
  set apiBase(v) { localStorage.setItem('sit_api', (v || '').replace(/\/+$/, '')); },
  get token() { return localStorage.getItem('sit_token') || ''; },
  set token(v) { v ? localStorage.setItem('sit_token', v) : localStorage.removeItem('sit_token'); },
  get user() { return localStorage.getItem('sit_user') || ''; },
  set user(v) { v ? localStorage.setItem('sit_user', v) : localStorage.removeItem('sit_user'); },
};

// api performs a fetch against /api/v1 with the bearer token attached.
// Returns parsed JSON (or null for 204). Throws {status, code, message} on error.
async function api(method, path, body) {
  if (!SIT.apiBase) throw { status: 0, code: 'no_api', message: '未配置 API 地址,请先登录' };
  const opts = { method, headers: {} };
  if (SIT.token) opts.headers['Authorization'] = 'Bearer ' + SIT.token;
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  let resp;
  try {
    resp = await fetch(SIT.apiBase + '/api/v1' + path, opts);
  } catch (e) {
    throw { status: 0, code: 'network', message: '网络错误(可能是跨域/反代未配置):' + e.message };
  }
  if (resp.status === 401) {
    // Token invalid/expired -> back to login (unless we are already logging in).
    if (!path.startsWith('/auth/login')) {
      SIT.token = '';
      if (!location.pathname.endsWith('login.html')) location.href = 'login.html';
    }
    throw { status: 401, code: 'unauthorized', message: '未授权或登录已过期' };
  }
  if (resp.status === 204) return null;
  let data = null;
  try { data = await resp.json(); } catch (_) { /* empty body */ }
  if (!resp.ok) {
    const err = (data && data.error) || {};
    throw { status: resp.status, code: err.code || 'error', message: err.message || ('HTTP ' + resp.status) };
  }
  return data;
}

// requireLogin redirects to login.html when there is no token.
function requireLogin() {
  if (!SIT.token || !SIT.apiBase) { location.href = 'login.html'; return false; }
  return true;
}

// renderHeader injects the shared nav bar into #app-header.
function renderHeader(active) {
  const el = document.getElementById('app-header');
  if (!el) return;
  const links = [
    ['nodes.html', '节点'],
    ['enroll.html', '接入'],
  ];
  el.innerHTML =
    '<span class="brand">SIT Manager</span>' +
    '<nav>' + links.map(([href, label]) =>
      `<a href="${href}" class="${active === href ? 'active' : ''}">${label}</a>`).join('') +
    '</nav>' +
    '<span class="spacer"></span>' +
    `<span class="who">${escapeHtml(SIT.user || '')} @ ${escapeHtml(SIT.apiBase)}</span>` +
    '<button class="secondary" onclick="logout()">退出</button>';
}

async function logout() {
  try { await api('POST', '/auth/logout'); } catch (_) { /* ignore */ }
  SIT.token = '';
  location.href = 'login.html';
}

// ---- formatting helpers ----

// fmtTime renders a unix-millis timestamp as a local datetime, or "—" for 0/empty.
function fmtTime(ms) {
  if (!ms) return '—';
  const d = new Date(Number(ms));
  if (isNaN(d.getTime())) return '—';
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

// fmtAgo renders "x秒/分钟/小时前" relative to now.
function fmtAgo(ms) {
  if (!ms) return '—';
  const diff = Date.now() - Number(ms);
  if (diff < 0) return fmtTime(ms);
  const s = Math.floor(diff / 1000);
  if (s < 60) return s + ' 秒前';
  const m = Math.floor(s / 60);
  if (m < 60) return m + ' 分钟前';
  const h = Math.floor(m / 60);
  if (h < 24) return h + ' 小时前';
  return Math.floor(h / 24) + ' 天前';
}

function statusBadge(status) {
  const cls = status === 'online' ? 'online' : 'offline';
  return `<span class="badge ${cls}">${status === 'online' ? '在线' : '离线'}</span>`;
}

function escapeHtml(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

// qs reads a query-string param from the current URL.
function qs(name) { return new URLSearchParams(location.search).get(name) || ''; }

// showError / showOk write a status line into an element by id.
function showError(id, msg) { const e = document.getElementById(id); if (e) { e.className = 'error'; e.textContent = msg; } }
function showOk(id, msg) { const e = document.getElementById(id); if (e) { e.className = 'ok'; e.textContent = msg; } }
function clearMsg(id) { const e = document.getElementById(id); if (e) { e.textContent = ''; } }
