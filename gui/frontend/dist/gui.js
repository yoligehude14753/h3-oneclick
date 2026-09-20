/* H3 OneClick Control Deck — drives the embedded /api/* engine. */
const $ = (id) => document.getElementById(id);
const state = { scan: null, decision: null, plan: null, job: null };

const api = async (path, body) => {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body || {}),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.message || data.error || res.statusText);
  return data;
};
const apiGet = async (path) => {
  const res = await fetch(path);
  const data = await res.json();
  if (!res.ok) throw new Error(data.message || data.error || res.statusText);
  return data;
};
const esc = (v) => String(v ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const log = (t) => { $('logBox').textContent += ( $('logBox').textContent === '等待任务。' ? '' : '\n' ) + t; };
const gib = (v) => `${Number(v || 0).toFixed(1)} GiB`;

function setStatus(text, tone = '') {
  $('topStatus').textContent = text;
  $('topLed').className = 'led ' + tone;
}
function setStep(n, cls) {
  const el = $('st' + n);
  el.classList.remove('active', 'done');
  if (cls) el.classList.add(cls);
}
function light(id) { $(id).classList.remove('dim'); $(id).classList.add('lit'); }
function pill(id, text, cls) { const p = $(id); p.textContent = text; p.className = 'pill ' + (cls || ''); }

/* ---------- boot ---------- */
(async () => {
  try {
    if (window.go?.main?.App?.Info) {
      const i = await window.go.main.App.Info();
      $('sysVersion').textContent = 'v' + i.version;
      $('sysPlatform').textContent = i.platform;
      $('sysGo').textContent = i.go;
      $('sysStateDir').textContent = i.state_dir.split('/').slice(-2).join('/');
      $('sysStateDir').title = i.state_dir;
      return;
    }
  } catch (_) { /* fall through to http */ }
  try {
    const h = await apiGet('/api/health');
    $('sysVersion').textContent = 'v' + (h.version || '?');
    $('sysPlatform').textContent = h.runtime || '';
    $('sysGo').textContent = h.runtime || '';
    $('sysStateDir').textContent = 'server mode';
  } catch (_) {}
})();

/* ---------- 01 scan ---------- */
const SCAN_PORTS = [8188, 18188, 18189];
$('scanBtn').onclick = async () => {
  const btn = $('scanBtn'); btn.disabled = true;
  setStatus('SCANNING', 'warn');
  try {
    const d = await api('/api/scan', { paths: [], ports: SCAN_PORTS });
    state.scan = d;
    renderScan(d);
    setStep(1, 'done'); setStep(2, 'active'); light('p2');
    $('recBtn').disabled = false;
    setStatus('SCANNED', 'on');
  } catch (e) { setStatus('SCAN FAILED', 'bad'); }
  btn.disabled = false;
};

function renderScan(d) {
  const h = d.hardware || {};
  const unified = h.unified_memory_gib || 0;
  $('scanEmpty').classList.add('hidden');
  $('hwGrid').classList.remove('hidden');
  $('gpuName').textContent = h.gpu_name || '未识别 GPU';
  const memLabel = h.vram_gib ? `${gib(h.vram_gib)} VRAM` : (unified ? `${gib(unified)} unified` : '—');
  $('gpuMeta').textContent = `${h.os}/${h.arch} · ${(h.backend || '—').toUpperCase()} · CC ${h.compute_capability || '—'} · ${memLabel}`;
  $('gpuCards').innerHTML = (h.gpus && h.gpus.length ? h.gpus : [{ name: h.gpu_name, vram_gib: h.vram_gib, vram_free_gib: h.vram_free_gib, backend: h.backend }])
    .map((g, i) => {
      const total = g.vram_gib || unified || 0, free = g.vram_free_gib || unified || 0;
      const usedPct = total ? Math.max(0, Math.min(100, (total - free) / total * 100)) : 0;
      return `<div class="gpu-card"><div class="g-top"><span>GPU${i} · ${esc(g.backend || '')}</span><b>${gib(free)} free / ${gib(total)}</b></div>
        <div class="bar"><i style="width:${(100 - usedPct).toFixed(1)}%"></i></div>
        <div class="g-top" style="margin:6px 0 0"><span>${esc(g.name || '')}</span><span>${usedPct.toFixed(0)}% used</span></div></div>`;
    }).join('');
  const ram = h.ram_gib || 0;
  $('ramText').textContent = gib(ram);
  $('ramBar').style.width = ram ? '100%' : '0';
  $('diskText').textContent = gib(h.disk_free_gib);
  $('diskBar').style.width = h.disk_free_gib ? '100%' : '0';
  $('drvText').textContent = h.driver || '—';
  const ff = h.ffmpeg && h.ffmpeg !== '';
  $('ffText').textContent = ff ? 'OK' : 'MISSING';
  $('ffText').className = ff ? 'ok' : 'bad';

  const inst = d.instances || [];
  $('instBox').classList.remove('hidden');
  $('instList').innerHTML = inst.length ? inst.map((x) => `
    <div class="inst">
      <span class="led ${x.running ? 'on' : ''}"></span>
      <code>${esc(x.path)}</code>
      <span class="tag">${esc(x.kind)}</span>
      <span class="tag ${x.compatibility === 'repairable' ? 'ok' : 'warn'}">${esc(x.compatibility)}</span>
      <span class="tag">${x.running ? 'RUNNING :' + x.port : 'stopped'}</span>
    </div>`).join('')
    : `<div class="inst"><code>未发现现有 ComfyUI — 将创建隔离实例</code></div>`;
}

/* ---------- 02 decide ---------- */
$('stepsSeg').addEventListener('click', (e) => {
  if (e.target.tagName !== 'BUTTON') return;
  [...$('stepsSeg').children].forEach((b) => b.classList.toggle('on', b === e.target));
});
const steps = () => Number(document.querySelector('#stepsSeg .on').dataset.v);
const policy = () => ({ allow_same_file_mirror: $('mirrorCk').checked, allow_compatible_alternative: $('altCk').checked });
const workload = () => ({ task: $('taskSel').value, width: 768, height: 432, seconds: Number($('secIn').value), audio: true, preferred_steps: steps() });

$('recBtn').onclick = async () => {
  setStatus('RESOLVING', 'warn');
  try {
    const d = await api('/api/best-config', {
      hardware: state.scan?.hardware,
      workload: workload(),
      preference: { preferred_steps: steps(), quality_floor: 'baseline' },
      source_policy: policy(),
    });
    state.decision = d;
    renderDecision(d);
    setStep(2, 'done'); setStep(3, 'active'); light('p3');
    $('planBtn').disabled = !d.effective_profile;
    setStatus('STACK READY', 'on');
  } catch (e) { setStatus('RESOLVE FAILED', 'bad'); }
};

function renderDecision(d) {
  pill('decPill', d.recommended_profile || 'UNSUPPORTED', d.recommended_profile ? 'ok' : 'bad');
  $('decBox').classList.remove('hidden');
  $('decProfile').textContent = d.effective_profile || '—';
  $('decStack').textContent = d.recommended_stack || '没有通过门控的 stack';
  $('decReasons').innerHTML = (d.reason_codes || []).map((r) =>
    `<span class="chip ${/pass|found|pending/.test(r) ? 'pass' : 'block'}">${esc(r)}</span>`).join('') || '—';
  $('altRow').innerHTML = (d.alternatives || []).map((a) =>
    `<div class="alt ${a.status === 'available' ? 'pass' : 'blocked'} ${a.profile === d.effective_profile ? 'pick' : ''}">
      <b>${esc(a.profile)}</b><small>${esc(a.status)} · ${esc((a.reasons || []).join(', ') || '可用')}</small></div>`).join('');
}

/* ---------- 03 plan ---------- */
$('planBtn').onclick = async () => {
  setStatus('PLANNING', 'warn');
  try {
    const d = await api('/api/plan', {
      decision: state.decision,
      profile_id: state.decision?.effective_profile,
      hardware: state.scan?.hardware,
      workload: workload(),
      source_policy: policy(),
      target_dir: $('dirIn').value,
      bootstrap_runtime: $('bootCk').checked,
    });
    state.plan = d;
    renderPlan(d);
    setStep(3, 'done'); setStep(4, 'active'); light('p4');
    $('installBtn').disabled = !!d.blocked;
    $('probeBtn').disabled = !(d.assets || []).length;
    setStatus(d.blocked ? 'PLAN BLOCKED' : 'PLAN READY', d.blocked ? 'bad' : 'on');
  } catch (e) { setStatus('PLAN FAILED', 'bad'); }
};

function renderPlan(d) {
  pill('planPill', d.blocked ? 'BLOCKED' : `${d.profile} · ${d.stack_id || ''}`, d.blocked ? 'bad' : 'ok');
  const m = $('planMeta');
  m.classList.remove('hidden');
  m.innerHTML = `<span class="ok">${esc(d.instance_mode)}</span>
    <span>需求 ${gib(d.required_free_gib)}</span><span>可用 ${gib(d.available_free_gib)}</span>
    ${(d.warnings || []).map((w) => `<span>${esc(w)}</span>`).join('')}
    ${(d.block_reasons || []).map((w) => `<span class="bad">${esc(w)}</span>`).join('')}`;
  $('assetTbl').classList.remove('hidden');
  $('assetRows').innerHTML = (d.assets || []).map((a) => `<tr>
    <td><code>${esc(a.asset_id)}</code></td>
    <td><a href="${esc(a.primary?.url || '#')}" target="_blank">${esc(a.primary?.repository_or_share || '—')}</a></td>
    <td>${(a.mirrors || []).length ? '<a href="' + esc(a.mirrors[0].url) + '" target="_blank">mirror ×' + a.mirrors.length + '</a>' : '—'}</td>
    <td><code>${esc(a.target_path)}</code></td>
    <td class="${a.reuse ? 'st-reuse' : 'st-dl'}">${a.reuse ? '复用' : '待下载'}</td></tr>`).join('');
}

$('probeBtn').onclick = async () => {
  const btn = $('probeBtn'); btn.disabled = true;
  log('== source probe ==');
  for (const a of state.plan?.assets || []) {
    try {
      const r = await api('/api/sources/probe', { asset_id: a.asset_id, source_policy: policy() });
      log(`${a.asset_id}: ${r.selected_source_role || 'blocked'} / ${r.mapping_status}`);
    } catch (e) { log(`${a.asset_id}: ERROR ${e.message}`); }
  }
  setStatus('PROBE DONE', 'on');
  btn.disabled = false;
};

/* ---------- 04 run ---------- */
const TERMINAL = ['READY_FOR_BASELINE', 'READY_FOR_PROFILE', 'BLOCKED', 'SOURCE_BLOCKED', 'PARTIAL', 'NON_COMFYUI_RUNTIME'];
const STATE_PCT = { PLANNING: 6, QUEUED: 8, SOURCE_PROBING: 25, PROBING: 25, DOWNLOADING: 55, WRITING: 78, VERIFYING: 88, READY_FOR_BASELINE: 100, READY_FOR_PROFILE: 100, BLOCKED: 100, SOURCE_BLOCKED: 100, PARTIAL: 100 };

$('installBtn').onclick = async () => {
  $('installBtn').disabled = true;
  setStatus('INSTALLING', 'warn');
  log(`== install ${state.plan?.profile} (${state.plan?.instance_mode}) ==`);
  try {
    const job = await api('/api/install', { plan: state.plan, dry_run: false });
    renderJob(job);
    pollJob(job.id);
  } catch (e) { setStatus('INSTALL FAILED', 'bad'); $('installBtn').disabled = false; }
};

async function pollJob(id) {
  try {
    const job = await apiGet(`/api/install/${encodeURIComponent(id)}`);
    renderJob(job);
    if (!TERMINAL.includes(job.state)) setTimeout(() => pollJob(id), 900);
  } catch (e) { log(`poll error: ${e.message}`); }
}

function renderJob(j) {
  state.job = j;
  pill('jobPill', j.state, TERMINAL.includes(j.state) ? (j.state.startsWith('READY') ? 'ok' : 'bad') : 'warn');
  $('jobState').textContent = j.state;
  $('jobMsg').textContent = j.progress?.message || j.error || '—';
  const sel = (j.attempts || []).find((x) => x.selected);
  $('jobSource').textContent = sel ? sel.source_id : '—';
  const pct = STATE_PCT[j.state] ?? Math.min(96, (j.progress?.index || 0) / (j.progress?.total || 1) * 100);
  $('progFill').style.width = pct + '%';
  $('progText').textContent = `${j.state} · ${j.progress?.index || 0}/${j.progress?.total || 0}`;
  if (j.attempts?.length) {
    log(j.attempts.map((a) =>
      `${a.asset_id} :: ${a.source_id} ${a.http_status || a.error_code || ''} ${a.elapsed_ms}ms ${a.selected ? '✓' : '✗'}`).join('\n'));
  }
  if (j.state.startsWith('READY')) {
    setStatus('DEPLOYED', 'on');
    setStep(4, 'done');
    $('launchBtn').disabled = false;
    $('tplBtn').disabled = false;
    log(`receipt: ${j.id}`);
  } else if (TERMINAL.includes(j.state)) {
    setStatus(j.state, 'bad');
    $('installBtn').disabled = false;
  }
}

/* ---------- launch ---------- */
$('launchBtn').onclick = async () => {
  try {
    const r = await api('/api/launch', { instance_id: state.plan?.instance_id, open_browser: true });
    log(`launch: ${JSON.stringify(r).slice(0, 300)}`);
    setStatus('COMFYUI UP', 'on');
  } catch (e) { log(`launch failed: ${e.message}`); setStatus('LAUNCH FAILED', 'bad'); }
};

$('tplBtn').onclick = async () => {
  try {
    const s = await api('/api/template/open', {
      instance_id: state.plan?.instance_id,
      profile_id: state.plan?.profile,
      open_browser: true,
    });
    log(`template: ${s.status} · ${s.workflow_path || ''} · nodes=${s.node_count ?? '?'}`);
    if (s.launch_url) log(`url: ${s.launch_url}`);
    if (s.session_id) {
      for (let i = 0; i < 12; i++) {
        await new Promise((r) => setTimeout(r, 1000));
        const t = await apiGet(`/api/template/status?session_id=${encodeURIComponent(s.session_id)}`);
        if (t.status !== s.status) log(`template status: ${t.status} ${t.message || ''}`);
        if (['TEMPLATE_READY', 'TEMPLATE_BLOCKED'].includes(t.status)) break;
        if (i === 11 && t.status === 'TEMPLATE_SAVED') log('template: 已写入 ComfyUI userdata（浏览器回执在桌面壳内不可达，属预期）');
      }
    }
    setStatus('TEMPLATE OPEN', 'on');
  } catch (e) { log(`template failed: ${e.message}`); }
};
