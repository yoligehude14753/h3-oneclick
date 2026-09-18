const state = { scan: null, decision: null, plan: null, job: null };
const $ = (id) => document.getElementById(id);
const api = async (path, options = {}) => {
  const response = await fetch(path, { headers: { 'content-type': 'application/json' }, ...options });
  const body = await response.json();
  if (!response.ok) throw new Error(body.message || body.error || response.statusText);
  return body;
};
const json = (value) => ({ method: 'POST', body: JSON.stringify(value) });
const steps = () => Number($('stepsSelect').value);
const sourcePolicy = () => ({ allow_same_file_mirror: $('mirrorToggle').checked, allow_compatible_alternative: $('alternativeToggle').checked });
const workload = () => ({ task: $('taskSelect').value, width: 768, height: 432, seconds: Number($('secondsInput').value), audio: true, preferred_steps: steps() });

function setTop(text, tone = 'idle') { $('topStatus').innerHTML = `<span class="status-dot ${tone}"></span>${text}`; }
function escapeHTML(value) { return String(value ?? '').replace(/[&<>"']/g, (c) => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;' }[c])); }
function renderScan(data) {
  state.scan = data;
  const h = data.hardware || {};
  const gpu = h.gpu_name || '未识别 GPU';
  const memory = h.vram_gib ? `${h.vram_gib.toFixed(1)} GiB VRAM` : `${(h.unified_memory_gib || 0).toFixed(1)} GiB unified`;
  $('hardwareMetric').textContent = gpu;
  $('hardwareDetail').textContent = `${h.os}/${h.arch} · ${h.backend} · ${memory} · RAM ${(h.ram_gib || 0).toFixed(1)} GiB`;
  $('softwareMetric').textContent = `${(data.instances || []).length} 个实例`;
  $('softwareDetail').textContent = data.instances?.length ? data.instances.map((x) => `${x.kind} ${x.path}`).join(' · ') : '没有现有 ComfyUI，将使用隔离计划';
  $('recommendBtn').disabled = false;
  setTop('扫描完成', 'ok');
}
function renderDecision(data) {
  state.decision = data;
  $('decisionPill').textContent = data.recommended_profile || 'unsupported';
  const reasons = (data.reason_codes || []).join(' · ');
  $('decisionBox').innerHTML = `<div class="decision-card"><div><span class="card-kicker">EFFECTIVE PROFILE</span><strong>${escapeHTML(data.effective_profile || data.recommended_profile)}</strong><p>${escapeHTML(data.recommended_stack || '没有通过门的 stack')}</p></div><div><span class="card-kicker">PREFERENCE</span><strong>${data.preferred_steps}-step</strong><p>偏好只参与排序，不推导显存。</p></div><div><span class="card-kicker">REASONS</span><p>${escapeHTML(reasons || '—')}</p><p>主源失败时允许：${data.source_policy?.allow_same_file_mirror ? '同文件镜像' : '否'}</p></div></div>`;
  $('alternatives').innerHTML = (data.alternatives || []).map((alt) => `<div class="alt ${alt.status === 'available' ? 'pass' : 'blocked'}"><b>${escapeHTML(alt.profile)}</b><small>${escapeHTML(alt.status)} · ${escapeHTML((alt.reasons || []).join(', ') || '可用')}</small></div>`).join('');
  $('planBtn').disabled = !data.effective_profile;
  setTop('配置栈已计算', 'ok');
}
function renderPlan(data) {
  state.plan = data;
  $('planPill').textContent = data.blocked ? '阻断' : `${data.profile} / ${data.stack_id || 'stack'}`;
  $('planMeta').innerHTML = `<span>${escapeHTML(data.instance_mode)}</span><span>预计 ${Number(data.required_free_gib || 0).toFixed(1)} GiB</span><span>可用 ${Number(data.available_free_gib || 0).toFixed(1)} GiB</span>${data.block_reasons?.length ? `<span class="source-blocked">${escapeHTML(data.block_reasons.join(' · '))}</span>` : ''}`;
  $('sourceRows').innerHTML = (data.assets || []).map((asset) => `<tr><td><code>${escapeHTML(asset.asset_id)}</code></td><td><a href="${escapeHTML(asset.primary.url)}" target="_blank">${escapeHTML(asset.primary.repository_or_share)}</a></td><td>${asset.mirrors?.length ? `<a href="${escapeHTML(asset.mirrors[0].url)}" target="_blank">镜像</a>` : '—'}</td><td><code>${escapeHTML(asset.target_path)}</code></td><td class="${asset.reuse ? 'source-ok' : 'source-mirror'}">${asset.reuse ? '复用' : '待下载'}</td></tr>`).join('') || '<tr><td colspan="5" class="empty">没有资产</td></tr>';
  $('probeBtn').disabled = !data.assets?.length;
  $('installBtn').disabled = !!data.blocked;
  setTop(data.blocked ? '计划被阻断' : '计划已生成', data.blocked ? 'bad' : 'ok');
}
function renderJob(job) {
  state.job = job;
  $('jobPill').textContent = job.state;
  $('jobState').textContent = job.state;
  $('jobMessage').textContent = job.progress?.message || job.error || '—';
  const total = Number(job.progress?.total || 0); const index = Number(job.progress?.index || 0);
  $('progressBar').style.width = total ? `${Math.min(100, index / total * 100)}%` : (job.state.startsWith('READY') ? '100%' : '5%');
  $('sourceUsed').textContent = job.attempts?.find((x) => x.selected)?.source_id || '—';
  $('jobLog').textContent = JSON.stringify({ state: job.state, progress: job.progress, attempts: job.attempts, error: job.error }, null, 2);
  if (!['READY_FOR_BASELINE','READY_FOR_PROFILE','BLOCKED','SOURCE_BLOCKED','PARTIAL','NON_COMFYUI_RUNTIME'].includes(job.state)) setTimeout(() => pollJob(job.id), 900);
  if (['READY_FOR_BASELINE','READY_FOR_PROFILE'].includes(job.state) && !job._opened) {
    job._opened = true;
    setTimeout(async () => {
      try {
        const base = $('targetDir').value;
        const scan = await api('/api/scan', json({ paths: [base, base ? `${base}/ComfyUI` : ''].filter(Boolean), ports: [18188] }));
        renderScan(scan);
        const instance = scan.instances?.[0];
        if (instance) {
          const opened = await api('/api/template/open', json({ instance_id: instance.id, profile_id: state.plan?.profile, open_browser: true }));
          $('jobLog').textContent += `\nTemplate: ${JSON.stringify(opened)}`;
          if (opened.session_id) pollTemplate(opened.session_id);
        }
      } catch (err) { $('jobLog').textContent += `\nTemplate open pending: ${err.message}`; }
    }, 250);
  }
}
async function pollJob(id) { try { renderJob(await api(`/api/install/${encodeURIComponent(id)}`)); } catch (err) { $('jobLog').textContent = err.message; } }
async function pollTemplate(sessionId, attempts = 0) { try { const session = await api(`/api/template/status?session_id=${encodeURIComponent(sessionId)}`); $('jobLog').textContent += `\nTemplate status: ${JSON.stringify(session)}`; if (!['TEMPLATE_READY','TEMPLATE_BLOCKED'].includes(session.status) && attempts < 30) setTimeout(() => pollTemplate(sessionId, attempts + 1), 700); else { $('jobPill').textContent = session.status; $('jobMessage').textContent = session.message || session.status; } } catch (err) { $('jobLog').textContent += `\nTemplate status failed: ${err.message}`; } }

$('scanBtn').onclick = async () => { $('scanBtn').disabled = true; try { renderScan(await api('/api/scan', json({ paths: [], ports: [18188] }))); } catch (err) { setTop(err.message, 'bad'); } finally { $('scanBtn').disabled = false; } };
$('recommendBtn').onclick = async () => { try { const hw = state.scan?.hardware; renderDecision(await api('/api/best-config', json({ hardware: hw, workload: workload(), preference: { preferred_steps: steps(), quality_floor: 'baseline' }, source_policy: sourcePolicy() }))); } catch (err) { setTop(err.message, 'bad'); } };
$('planBtn').onclick = async () => { try { renderPlan(await api('/api/plan', json({ decision: state.decision, profile_id: state.decision?.effective_profile, hardware: state.scan?.hardware, workload: workload(), source_policy: sourcePolicy(), target_dir: $('targetDir').value, bootstrap_runtime: $('bootstrapToggle').checked }))); } catch (err) { setTop(err.message, 'bad'); } };
$('probeBtn').onclick = async () => { if (!state.plan) return; $('probeBtn').disabled = true; const lines = []; try { for (const asset of state.plan.assets || []) { const result = await api('/api/sources/probe', json({ asset_id: asset.asset_id, source_policy: sourcePolicy() })); lines.push(`${asset.asset_id}: ${result.selected_source_role || 'blocked'} / ${result.mapping_status}`); } $('jobLog').textContent = lines.join('\n'); setTop('来源探测完成', 'ok'); } catch (err) { $('jobLog').textContent = err.message; setTop('来源探测有阻断', 'bad'); } finally { $('probeBtn').disabled = false; } };
$('installBtn').onclick = async () => { try { const job = await api('/api/install', json({ plan: state.plan, dry_run: false })); renderJob(job); document.querySelector('[data-step="run"]').classList.add('active'); } catch (err) { setTop(err.message, 'bad'); } };
