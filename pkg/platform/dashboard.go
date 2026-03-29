package platform

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>FAXP Platform — Live Dashboard</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: #0d1117; color: #c9d1d9; font-family: 'SF Mono', 'Consolas', monospace; font-size: 13px; }
  header { background: #161b22; border-bottom: 1px solid #30363d; padding: 16px 24px; display: flex; align-items: center; gap: 16px; }
  header h1 { font-size: 16px; color: #f0f6fc; font-weight: 600; }
  .badge { background: #238636; color: #fff; font-size: 10px; padding: 2px 8px; border-radius: 12px; }
  .badge.offline { background: #6e7681; }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 1px; background: #30363d; height: calc(100vh - 57px); }
  .panel { background: #0d1117; padding: 16px; overflow-y: auto; }
  .panel h2 { font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em; color: #8b949e; margin-bottom: 12px; border-bottom: 1px solid #21262d; padding-bottom: 8px; }
  .agent-card { background: #161b22; border: 1px solid #30363d; border-radius: 6px; padding: 12px; margin-bottom: 8px; }
  .agent-card .name { color: #79c0ff; font-weight: 600; }
  .agent-card .org { color: #8b949e; font-size: 11px; margin-top: 2px; }
  .agent-card .skills { margin-top: 8px; display: flex; flex-wrap: wrap; gap: 4px; }
  .skill-tag { background: #0d419d; color: #79c0ff; font-size: 10px; padding: 2px 6px; border-radius: 4px; }
  .agent-card .price { color: #3fb950; font-size: 11px; margin-top: 6px; }
  .call-row { padding: 8px 10px; border-bottom: 1px solid #21262d; display: grid; grid-template-columns: 120px 1fr 80px 70px 60px; gap: 8px; align-items: center; animation: fadeIn 0.3s ease; }
  .call-row:hover { background: #161b22; }
  @keyframes fadeIn { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: none; } }
  .call-row .time { color: #8b949e; }
  .call-row .route { color: #c9d1d9; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .call-row .route span { color: #79c0ff; }
  .call-row .method { color: #d2a8ff; font-size: 11px; }
  .call-row .latency { color: #f0f6fc; text-align: right; }
  .call-row .status { text-align: right; }
  .status-dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; }
  .status-dot.success { background: #3fb950; }
  .status-dot.error { background: #f85149; }
  .status-dot.in_flight { background: #d29922; animation: pulse 1s infinite; }
  @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.4; } }
  .spend-row { display: flex; justify-content: space-between; padding: 6px 0; border-bottom: 1px solid #21262d; }
  .spend-row .org-name { color: #c9d1d9; }
  .spend-row .amount { color: #3fb950; font-weight: 600; }
  .empty { color: #8b949e; padding: 24px; text-align: center; font-style: italic; }
  .col-headers { display: grid; grid-template-columns: 120px 1fr 80px 70px 60px; gap: 8px; padding: 6px 10px; color: #8b949e; font-size: 10px; text-transform: uppercase; border-bottom: 1px solid #30363d; }
  .status-bar { display: flex; gap: 20px; margin-left: auto; font-size: 11px; color: #8b949e; }
  .status-bar span { color: #f0f6fc; }
</style>
</head>
<body>
<header>
  <h1>⬡ FAXP Platform</h1>
  <div id="conn-badge" class="badge offline">connecting</div>
  <div class="status-bar">
    <div>Agents: <span id="agent-count">0</span></div>
    <div>Calls: <span id="call-count">0</span></div>
    <div>Total spend: $<span id="total-spend">0.0000</span></div>
  </div>
</header>
<div class="grid">
  <div class="panel">
    <h2>Registered Agents</h2>
    <div id="agents-list"><div class="empty">No agents registered yet.</div></div>
  </div>
  <div class="panel">
    <h2>Live Call Feed</h2>
    <div class="col-headers">
      <div>Time</div><div>Route</div><div>Method</div><div>Latency</div><div>Status</div>
    </div>
    <div id="calls-list"></div>
    <h2 style="margin-top:20px">Spend by Organization</h2>
    <div id="spend-list"><div class="empty">No calls yet.</div></div>
  </div>
</div>
<script>
const API_KEY = 'faxp_admin_demo';
let callCount = 0;
let totalSpend = 0;
const agents = {};
const spendByOrg = {};

function fmtTime(d) {
  return new Date(d).toLocaleTimeString('en-US', {hour12: false, hour:'2-digit', minute:'2-digit', second:'2-digit'});
}

function renderAgents() {
  const list = document.getElementById('agents-list');
  const entries = Object.values(agents);
  if (!entries.length) { list.innerHTML = '<div class="empty">No agents registered yet.</div>'; return; }
  list.innerHTML = entries.map(a => {
    const skills = (a.agent_card?.skills || []).map(s =>
      '<span class="skill-tag">' + s.id + '</span>'
    ).join('');
    const price = a.agent_card?.['x-faxp-pricing'];
    const priceStr = price ? '<div class="price">$' + price.per_call_usd.toFixed(4) + ' / call</div>' : '';
    return '<div class="agent-card"><div class="name">' + a.name + '</div>' +
      '<div class="org">' + (a.organization || '') + '</div>' +
      '<div class="skills">' + skills + '</div>' + priceStr + '</div>';
  }).join('');
  document.getElementById('agent-count').textContent = entries.length;
}

function renderCall(rec) {
  const list = document.getElementById('calls-list');
  if (list.querySelector('.empty')) list.innerHTML = '';
  const dot = '<span class="status-dot ' + rec.status + '"></span>';
  const latency = rec.latency_ms ? rec.latency_ms + 'ms' : '—';
  const row = document.createElement('div');
  row.className = 'call-row';
  row.id = 'call-' + rec.id;
  row.innerHTML =
    '<div class="time">' + fmtTime(rec.started_at) + '</div>' +
    '<div class="route"><span>' + (rec.caller_org || '') + '</span> → ' + (rec.agent_name || '') + '</div>' +
    '<div class="method">' + (rec.method || '') + '</div>' +
    '<div class="latency">' + latency + '</div>' +
    '<div class="status">' + dot + '</div>';
  list.insertBefore(row, list.firstChild);
  callCount++;
  document.getElementById('call-count').textContent = callCount;
}

function updateCall(rec) {
  const row = document.getElementById('call-' + rec.id);
  if (!row) { renderCall(rec); return; }
  const dot = row.querySelector('.status-dot');
  if (dot) { dot.className = 'status-dot ' + rec.status; }
  row.querySelector('.latency').textContent = rec.latency_ms ? rec.latency_ms + 'ms' : '—';
}

function renderSpend() {
  const list = document.getElementById('spend-list');
  const entries = Object.entries(spendByOrg);
  if (!entries.length) { list.innerHTML = '<div class="empty">No calls yet.</div>'; return; }
  list.innerHTML = entries.sort((a,b) => b[1]-a[1]).map(([org, amt]) =>
    '<div class="spend-row"><div class="org-name">' + org + '</div>' +
    '<div class="amount">$' + amt.toFixed(4) + '</div></div>'
  ).join('');
  const total = entries.reduce((s, [,v]) => s + v, 0);
  document.getElementById('total-spend').textContent = total.toFixed(4);
}

const evtSource = new EventSource('/platform/v1/events?api_key=' + API_KEY);

evtSource.onopen = () => {
  document.getElementById('conn-badge').textContent = 'live';
  document.getElementById('conn-badge').classList.remove('offline');
};

evtSource.onerror = () => {
  document.getElementById('conn-badge').textContent = 'disconnected';
  document.getElementById('conn-badge').classList.add('offline');
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
    agents[d.agent_id] = { id: d.agent_id, name: d.name, organization: d.org, agent_card: { skills: d.skills } };
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
