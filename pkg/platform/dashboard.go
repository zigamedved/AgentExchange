package platform

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AgentExchange — Dashboard</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: #0d1117; color: #c9d1d9; font-family: -apple-system, 'Segoe UI', Helvetica, Arial, sans-serif; font-size: 13px; }

  header { background: #161b22; border-bottom: 1px solid #30363d; padding: 14px 24px; display: flex; align-items: center; gap: 16px; }
  header h1 { font-size: 15px; color: #f0f6fc; font-weight: 600; letter-spacing: -0.01em; }
  header h1 em { font-style: normal; color: #79c0ff; }
  .conn-badge { font-size: 10px; padding: 2px 8px; border-radius: 12px; font-weight: 500; }
  .conn-badge.live { background: #238636; color: #fff; }
  .conn-badge.offline { background: #6e7681; color: #fff; }
  .header-stats { display: flex; gap: 20px; margin-left: auto; font-size: 12px; color: #8b949e; }
  .header-stats span { color: #f0f6fc; font-weight: 500; }
  .org-info { font-size: 11px; color: #8b949e; border-left: 1px solid #30363d; padding-left: 16px; margin-left: 8px; }
  .org-info .org-name { color: #d2a8ff; font-weight: 500; }
  .org-info .credits { color: #3fb950; }

  .grid { display: grid; grid-template-columns: 340px 1fr; gap: 1px; background: #30363d; height: calc(100vh - 53px); }
  .panel { background: #0d1117; padding: 16px; overflow-y: auto; }
  .panel-title { font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em; color: #8b949e; margin-bottom: 12px; border-bottom: 1px solid #21262d; padding-bottom: 8px; display: flex; align-items: center; justify-content: space-between; }
  .panel-title .count { background: #21262d; color: #c9d1d9; font-size: 10px; padding: 1px 6px; border-radius: 10px; }

  /* Agent cards */
  .agent-card { background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 12px; margin-bottom: 8px; transition: border-color 0.15s; }
  .agent-card:hover { border-color: #484f58; }
  .agent-header { display: flex; align-items: center; gap: 8px; }
  .agent-card .name { color: #79c0ff; font-weight: 600; font-size: 13px; }
  .vis-badge { font-size: 9px; padding: 1px 5px; border-radius: 3px; font-weight: 500; text-transform: uppercase; letter-spacing: 0.04em; }
  .vis-badge.public { background: #0d419d; color: #79c0ff; }
  .vis-badge.private { background: #3d1f00; color: #d29922; }
  .agent-card .desc { color: #8b949e; font-size: 11px; margin-top: 4px; line-height: 1.4; }
  .agent-card .org { color: #8b949e; font-size: 11px; margin-top: 4px; }
  .agent-card .skills { margin-top: 8px; display: flex; flex-wrap: wrap; gap: 4px; }
  .skill-tag { background: #0d419d33; color: #79c0ff; font-size: 10px; padding: 2px 6px; border-radius: 4px; border: 1px solid #0d419d55; }
  .agent-card .price { color: #3fb950; font-size: 11px; margin-top: 6px; font-weight: 500; }
  .agent-card .price.free { color: #8b949e; }

  /* Call feed */
  .call-row { padding: 8px 10px; border-bottom: 1px solid #21262d; display: grid; grid-template-columns: 90px 1fr 100px 60px 60px 50px; gap: 8px; align-items: center; animation: fadeIn 0.2s ease; font-size: 12px; }
  .call-row:hover { background: #161b22; }
  @keyframes fadeIn { from { opacity: 0; transform: translateY(-3px); } to { opacity: 1; transform: none; } }
  .call-row .time { color: #8b949e; font-family: 'SF Mono', 'Consolas', monospace; font-size: 11px; }
  .call-row .route { color: #c9d1d9; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .call-row .route .caller { color: #d2a8ff; }
  .call-row .route .arrow { color: #484f58; margin: 0 4px; }
  .call-row .route .target { color: #79c0ff; }
  .call-row .method { color: #d2a8ff; font-family: 'SF Mono', 'Consolas', monospace; font-size: 10px; }
  .call-row .cost { color: #3fb950; text-align: right; font-family: 'SF Mono', 'Consolas', monospace; font-size: 11px; }
  .call-row .cost.free { color: #484f58; }
  .call-row .latency { color: #f0f6fc; text-align: right; font-family: 'SF Mono', 'Consolas', monospace; font-size: 11px; }
  .call-row .status { text-align: center; }
  .status-dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; }
  .status-dot.success { background: #3fb950; }
  .status-dot.error { background: #f85149; }
  .status-dot.in_flight { background: #d29922; animation: pulse 1s infinite; }
  @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.4; } }
  .col-headers { display: grid; grid-template-columns: 90px 1fr 100px 60px 60px 50px; gap: 8px; padding: 6px 10px; color: #8b949e; font-size: 10px; text-transform: uppercase; letter-spacing: 0.04em; border-bottom: 1px solid #30363d; }

  /* Spend section */
  .spend-section { margin-top: 20px; }
  .spend-row { display: flex; justify-content: space-between; padding: 6px 0; border-bottom: 1px solid #21262d; }
  .spend-row .org-name { color: #c9d1d9; }
  .spend-row .amount { color: #3fb950; font-weight: 600; font-family: 'SF Mono', 'Consolas', monospace; }
  .empty { color: #484f58; padding: 24px; text-align: center; font-style: italic; font-size: 12px; }
</style>
</head>
<body>
<header>
  <h1><em>AX</em> AgentExchange</h1>
  <div id="conn-badge" class="conn-badge offline">connecting</div>
  <div class="header-stats">
    <div>Agents <span id="agent-count">0</span></div>
    <div>Calls <span id="call-count">0</span></div>
    <div id="spend-header" style="display:none">Spend <span id="total-spend">$0.00</span></div>
  </div>
  <div id="org-bar" class="org-info" style="display:none"></div>
</header>
<div class="grid">
  <div class="panel">
    <div class="panel-title">Agents <span class="count" id="agent-count-badge">0</span></div>
    <div id="agents-list"><div class="empty">No agents registered yet</div></div>
  </div>
  <div class="panel">
    <div class="panel-title">Live Call Feed <span class="count" id="call-count-badge">0</span></div>
    <div class="col-headers">
      <div>Time</div><div>Route</div><div>Method</div><div>Cost</div><div>Latency</div><div></div>
    </div>
    <div id="calls-list"></div>
    <div id="spend-section" class="spend-section" style="display:none">
      <div class="panel-title">Spend by Organization</div>
      <div id="spend-list"></div>
    </div>
  </div>
</div>
<script>
// Auth
let API_KEY = new URLSearchParams(window.location.search).get('api_key')
  || localStorage.getItem('ax_api_key');
if (!API_KEY) {
  API_KEY = prompt('Enter your AX API key:');
  if (API_KEY) localStorage.setItem('ax_api_key', API_KEY);
}
if (!API_KEY) {
  document.body.innerHTML = '<div style="padding:40px;color:#f85149;font-family:monospace">No API key. Reload or add ?api_key=KEY to the URL.</div>';
  throw new Error('no api key');
}

let callCount = 0;
let hasSpend = false;
const agents = {};
const spendByOrg = {};

// Fetch org info for the header
fetch('/platform/v1/orgs/me', { headers: { 'Authorization': 'Bearer ' + API_KEY } })
  .then(r => r.json())
  .then(org => {
    const bar = document.getElementById('org-bar');
    let html = '<span class="org-name">' + org.name + '</span>';
    if (org.credits !== undefined && org.credits > 0) {
      html += ' &middot; <span class="credits">' + org.credits.toFixed(2) + ' credits</span>';
    }
    bar.innerHTML = html;
    bar.style.display = '';
  }).catch(() => {});

function fmtTime(d) {
  return new Date(d).toLocaleTimeString('en-US', {hour12: false, hour:'2-digit', minute:'2-digit', second:'2-digit'});
}

function renderAgents() {
  const list = document.getElementById('agents-list');
  const entries = Object.values(agents);
  document.getElementById('agent-count').textContent = entries.length;
  document.getElementById('agent-count-badge').textContent = entries.length;
  if (!entries.length) { list.innerHTML = '<div class="empty">No agents registered yet</div>'; return; }
  list.innerHTML = entries.map(a => {
    const vis = a.visibility || 'public';
    const visBadge = '<span class="vis-badge ' + vis + '">' + vis + '</span>';
    const skills = (a.agent_card?.skills || []).map(s =>
      '<span class="skill-tag">' + (s.id || s.name || '') + '</span>'
    ).join('');
    const desc = a.agent_card?.description
      ? '<div class="desc">' + a.agent_card.description + '</div>' : '';
    const price = a.agent_card?.['x-ax-pricing'];
    let priceStr = '<div class="price free">Free</div>';
    if (price && price.per_call_usd > 0) {
      priceStr = '<div class="price">$' + price.per_call_usd.toFixed(4) + ' / call</div>';
    }
    return '<div class="agent-card">' +
      '<div class="agent-header"><span class="name">' + a.name + '</span>' + visBadge + '</div>' +
      '<div class="org">' + (a.organization || '') + '</div>' +
      desc +
      '<div class="skills">' + skills + '</div>' +
      priceStr +
      '</div>';
  }).join('');
}

function renderCall(rec) {
  const list = document.getElementById('calls-list');
  if (list.querySelector('.empty')) list.innerHTML = '';
  const dot = '<span class="status-dot ' + rec.status + '"></span>';
  const latency = rec.latency_ms ? rec.latency_ms + 'ms' : '';
  const price = rec.price_usd > 0 ? '$' + rec.price_usd.toFixed(4) : '';
  const priceClass = rec.price_usd > 0 ? 'cost' : 'cost free';
  const row = document.createElement('div');
  row.className = 'call-row';
  row.id = 'call-' + rec.id;
  row.innerHTML =
    '<div class="time">' + fmtTime(rec.started_at) + '</div>' +
    '<div class="route"><span class="caller">' + (rec.caller_org || '') + '</span><span class="arrow">&rarr;</span><span class="target">' + (rec.agent_name || '') + '</span></div>' +
    '<div class="method">' + (rec.method || '').replace('a2a_', '') + '</div>' +
    '<div class="' + priceClass + '">' + price + '</div>' +
    '<div class="latency">' + latency + '</div>' +
    '<div class="status">' + dot + '</div>';
  list.insertBefore(row, list.firstChild);
  callCount++;
  document.getElementById('call-count').textContent = callCount;
  document.getElementById('call-count-badge').textContent = callCount;
}

function updateCall(rec) {
  const row = document.getElementById('call-' + rec.id);
  if (!row) { renderCall(rec); return; }
  const dot = row.querySelector('.status-dot');
  if (dot) dot.className = 'status-dot ' + rec.status;
  row.querySelector('.latency').textContent = rec.latency_ms ? rec.latency_ms + 'ms' : '';
  const costEl = row.querySelector('.cost');
  if (costEl && rec.price_usd > 0) {
    costEl.textContent = '$' + rec.price_usd.toFixed(4);
    costEl.className = 'cost';
  }
}

function renderSpend() {
  const entries = Object.entries(spendByOrg).filter(([,v]) => v > 0);
  if (!entries.length) return;

  hasSpend = true;
  document.getElementById('spend-section').style.display = '';
  document.getElementById('spend-header').style.display = '';

  const list = document.getElementById('spend-list');
  list.innerHTML = entries.sort((a,b) => b[1]-a[1]).map(([org, amt]) =>
    '<div class="spend-row"><div class="org-name">' + org + '</div>' +
    '<div class="amount">$' + amt.toFixed(4) + '</div></div>'
  ).join('');
  const total = entries.reduce((s, [,v]) => s + v, 0);
  document.getElementById('total-spend').textContent = '$' + total.toFixed(4);
}

const evtSource = new EventSource('/platform/v1/events?api_key=' + API_KEY);

evtSource.onopen = () => {
  const b = document.getElementById('conn-badge');
  b.textContent = 'live';
  b.className = 'conn-badge live';
};

evtSource.onerror = () => {
  const b = document.getElementById('conn-badge');
  b.textContent = 'disconnected';
  b.className = 'conn-badge offline';
};

evtSource.onmessage = (e) => {
  const event = JSON.parse(e.data);

  if (event.kind === 'init') {
    (event.agents || []).forEach(a => { agents[a.id] = a; });
    renderAgents();
    (event.calls || []).reverse().forEach(rec => renderCall(rec));
    if (event.spend) {
      Object.assign(spendByOrg, event.spend);
      renderSpend();
    }
    return;
  }

  if (event.kind === 'agent.registered') {
    const d = event.data;
    agents[d.agent_id] = {
      id: d.agent_id,
      name: d.name,
      organization: d.org,
      visibility: d.visibility || 'public',
      agent_card: { skills: d.skills, description: d.description, 'x-ax-pricing': d.pricing }
    };
    renderAgents();
  }

  if (event.kind === 'agent.deregistered') {
    delete agents[event.data.agent_id];
    renderAgents();
  }

  if (event.kind === 'call.started') {
    renderCall(event.data);
  }

  if (event.kind === 'call.completed' || event.kind === 'call.failed') {
    updateCall(event.data);
    if (event.data.caller_org && event.data.price_usd) {
      spendByOrg[event.data.caller_org] = (spendByOrg[event.data.caller_org] || 0) + event.data.price_usd;
      renderSpend();
    }
  }
};
</script>
</body>
</html>`
