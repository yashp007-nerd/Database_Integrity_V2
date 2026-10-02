/* ============================================================
   app.js  –  RowGuard Vibe Secure Ledger  –  Frontend Logic
   ============================================================ */

const API = '/student';
let editingId = null;
let activeTab = 'ledger'; // 'ledger' | 'trash'

// ── Toast ─────────────────────────────────────────────────────────────────────

function showToast(message, type = 'success') {
  const toast = document.getElementById('toast');
  const icon  = document.getElementById('toastIcon');
  const msg   = document.getElementById('toastMsg');

  const cfg = {
    success: { icon: '✅', border: 'border-emerald-500/30' },
    error:   { icon: '❌', border: 'border-red-500/30'     },
    warn:    { icon: '⚠️',  border: 'border-amber-500/30'  },
    info:    { icon: 'ℹ️',  border: 'border-indigo-500/30' },
  }[type] || { icon: 'ℹ️', border: '' };

  icon.textContent  = cfg.icon;
  msg.textContent   = message;
  toast.className   = `fixed top-6 right-6 z-50 glass rounded-xl px-5 py-3 flex items-center gap-3 shadow-2xl max-w-sm ${cfg.border}`;
  toast.classList.remove('hidden');
  toast.style.opacity = '0';
  toast.style.transform = 'translateY(-8px)';
  setTimeout(() => { toast.style.opacity = '1'; toast.style.transform = 'translateY(0)'; }, 10);

  clearTimeout(toast._timer);
  toast._timer = setTimeout(() => {
    toast.style.opacity = '0';
    toast.style.transform = 'translateY(-8px)';
    setTimeout(() => toast.classList.add('hidden'), 300);
  }, 3500);
}

// ── API helpers ───────────────────────────────────────────────────────────────

async function apiFetch(url, options = {}) {
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

// ── Tab switching ─────────────────────────────────────────────────────────────

function switchTab(tab) {
  activeTab = tab;
  const ledgerBtn = document.getElementById('tabLedger');
  const trashBtn  = document.getElementById('tabTrash');
  const ledgerView = document.getElementById('ledgerView');
  const trashView  = document.getElementById('trashView');

  if (tab === 'ledger') {
    ledgerBtn.classList.add('tab-active');
    trashBtn.classList.remove('tab-active');
    ledgerView.classList.remove('hidden');
    trashView.classList.add('hidden');
    loadStudents();
  } else {
    trashBtn.classList.add('tab-active');
    ledgerBtn.classList.remove('tab-active');
    trashView.classList.remove('hidden');
    ledgerView.classList.add('hidden');
    loadTrash();
  }
}

// ── Load & Render Active Students ─────────────────────────────────────────────

async function loadStudents() {
  const tbody = document.getElementById('tableBody');
  tbody.innerHTML = `<tr><td colspan="7" class="px-6 py-12 text-center text-slate-600">
    <span class="spinner"></span>
  </td></tr>`;

  try {
    const students = await apiFetch(`${API}/all`);
    document.getElementById('statTotal').textContent   = students.length;
    document.getElementById('statSecure').textContent  = '—';
    document.getElementById('statAnomalies').textContent = '—';
    renderTable(students);
  } catch (e) {
    tbody.innerHTML = `<tr><td colspan="7" class="px-6 py-12 text-center text-red-400 text-sm">
      Failed to load records: ${e.message}
    </td></tr>`;
    showToast(e.message, 'error');
  }
}

function renderTable(students) {
  const tbody = document.getElementById('tableBody');
  if (!students || students.length === 0) {
    tbody.innerHTML = `<tr><td colspan="7" class="px-6 py-12 text-center text-slate-600 text-sm">
      No records yet. Add your first student above.
    </td></tr>`;
    return;
  }

  tbody.innerHTML = students.map((s, i) => `
    <tr class="trow border-b border-white/5 row-animate" style="animation-delay:${i * 30}ms">
      <td class="px-6 py-4 text-slate-400 mono text-xs">#${s.id}</td>
      <td class="px-6 py-4 font-medium text-white">${escHtml(s.student_name)}</td>
      <td class="px-6 py-4">
        <span class="bg-indigo-500/20 text-indigo-300 border border-indigo-500/30 text-xs px-2.5 py-1 rounded-lg font-mono">${escHtml(s.subject_code)}</span>
      </td>
      <td class="px-6 py-4">
        <span class="font-bold ${marksColor(s.marks)}">${s.marks}</span>
        <span class="text-slate-600 text-xs">/100</span>
      </td>
      <td class="px-6 py-4">
        <span class="hash-pill mono" title="${escHtml(s.hash_footprint)}" onclick="copyHash('${escHtml(s.hash_footprint)}')">
          ${s.hash_footprint.slice(0, 10)}…
        </span>
      </td>
      <td class="px-6 py-4 text-xs text-slate-500">${formatDate(s.created_at)}</td>
      <td class="px-6 py-4">
        <div class="flex gap-2">
          <button onclick="startEdit(${s.id},'${escJs(s.student_name)}','${escJs(s.subject_code)}',${s.marks})"
            class="btn-edit text-xs font-medium px-3 py-1.5 rounded-lg">Edit</button>
          <button onclick="deleteStudent(${s.id})"
            class="btn-danger text-xs font-medium px-3 py-1.5 rounded-lg">🗑 Trash</button>
        </div>
      </td>
    </tr>
  `).join('');
}

// ── Load & Render Trash ───────────────────────────────────────────────────────

async function loadTrash() {
  const tbody = document.getElementById('trashBody');
  tbody.innerHTML = `<tr><td colspan="6" class="px-6 py-12 text-center text-slate-600">
    <span class="spinner"></span>
  </td></tr>`;

  try {
    const students = await apiFetch(`${API}/trash`);
    document.getElementById('statTrashed').textContent = students.length;
    renderTrash(students);
  } catch (e) {
    tbody.innerHTML = `<tr><td colspan="6" class="px-6 py-12 text-center text-red-400 text-sm">
      Failed to load trash: ${e.message}
    </td></tr>`;
    showToast(e.message, 'error');
  }
}

function renderTrash(students) {
  const tbody = document.getElementById('trashBody');
  if (!students || students.length === 0) {
    tbody.innerHTML = `<tr><td colspan="6" class="px-6 py-12 text-center text-slate-600 text-sm">
      Trash is empty. Deleted records will appear here.
    </td></tr>`;
    return;
  }

  tbody.innerHTML = students.map((s, i) => `
    <tr class="trow border-b border-white/5 row-animate" style="animation-delay:${i * 30}ms">
      <td class="px-6 py-4 text-slate-400 mono text-xs">#${s.id}</td>
      <td class="px-6 py-4 font-medium text-slate-400 line-through">${escHtml(s.student_name)}</td>
      <td class="px-6 py-4">
        <span class="bg-slate-700/40 text-slate-400 border border-slate-600/30 text-xs px-2.5 py-1 rounded-lg font-mono">${escHtml(s.subject_code)}</span>
      </td>
      <td class="px-6 py-4 text-slate-500">${s.marks}<span class="text-slate-700 text-xs">/100</span></td>
      <td class="px-6 py-4 text-xs text-amber-500/80">${formatDate(s.deleted_at)}</td>
      <td class="px-6 py-4">
        <button onclick="restoreStudent(${s.id})"
          class="btn-restore text-xs font-medium px-3 py-1.5 rounded-lg">↩ Restore</button>
      </td>
    </tr>
  `).join('');
}

// ── Form: Create / Update ─────────────────────────────────────────────────────

async function handleSubmit(e) {
  e.preventDefault();
  const btn   = document.getElementById('submitBtn');
  const name  = document.getElementById('studentName').value.trim();
  const code  = document.getElementById('subjectCode').value.trim();
  const marks = parseInt(document.getElementById('marks').value, 10);

  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span> Saving…';

  try {
    const payload = { student_name: name, subject_code: code, marks };

    if (editingId) {
      await apiFetch(`${API}/update/${editingId}`, { method: 'PATCH', body: JSON.stringify(payload) });
      showToast(`Record #${editingId} updated successfully.`, 'success');
      cancelEdit();
    } else {
      await apiFetch(`${API}/create`, { method: 'POST', body: JSON.stringify(payload) });
      showToast('New record created and hash footprint generated.', 'success');
      document.getElementById('studentForm').reset();
    }
    loadStudents();
  } catch (err) {
    showToast(err.message, 'error');
  } finally {
    btn.disabled = false;
    btn.textContent = 'Save Record';
  }
}

function startEdit(id, name, code, marks) {
  editingId = id;
  document.getElementById('studentName').value = name;
  document.getElementById('subjectCode').value  = code;
  document.getElementById('marks').value        = marks;
  document.getElementById('formTitle').textContent = `✏️ Editing Record #${id}`;
  document.getElementById('cancelBtn').classList.remove('hidden');
  document.getElementById('studentName').focus();
  document.getElementById('studentForm').scrollIntoView({ behavior: 'smooth', block: 'start' });
}

function cancelEdit() {
  editingId = null;
  document.getElementById('studentForm').reset();
  document.getElementById('formTitle').textContent = '➕ Add Student Record';
  document.getElementById('cancelBtn').classList.add('hidden');
}

// ── Delete (soft) ─────────────────────────────────────────────────────────────

async function deleteStudent(id) {
  if (!confirm(`Move record #${id} to trash? You can restore it later.`)) return;
  try {
    await apiFetch(`${API}/delete/${id}`, { method: 'DELETE' });
    showToast(`Record #${id} moved to trash.`, 'warn');
    loadStudents();
  } catch (err) {
    showToast(err.message, 'error');
  }
}

// ── Restore from trash ────────────────────────────────────────────────────────

async function restoreStudent(id) {
  try {
    await apiFetch(`${API}/restore/${id}`, { method: 'POST' });
    showToast(`Record #${id} restored to the ledger.`, 'success');
    loadTrash();
    // Refresh stat counter for trash
    document.getElementById('statTrashed').textContent = '…';
  } catch (err) {
    showToast(err.message, 'error');
  }
}

// ── Integrity Audit ───────────────────────────────────────────────────────────

async function runAudit() {
  const btn = document.getElementById('auditBtn');
  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span> Auditing…';

  try {
    const report = await apiFetch(`${API}/validate`, { method: 'POST' });

    // Show top-level ledger banner
    displayLedgerBanner(report.ledger_integrity, report.ledger_detail);

    // Show per-row results
    displayAudit(report.results || []);

    const results   = report.results || [];
    const secure    = results.filter(r => r.status === 'SECURE_INTEGRITY_MAINTAINED').length;
    const anomalies = results.length - secure;
    const ledgerOK  = report.ledger_integrity === 'LEDGER_INTACT';

    document.getElementById('statSecure').textContent    = secure;
    document.getElementById('statAnomalies').textContent = anomalies + (ledgerOK ? 0 : 1);
    document.getElementById('statTotal').textContent     = results.length;

    const allClear = anomalies === 0 && ledgerOK;
    showToast(
      allClear
        ? `Audit complete: ${secure} rows secure, ledger intact.`
        : `⚠️ Audit flagged issues — ${anomalies} row anomalies${ledgerOK ? '' : ' + ledger mismatch'}.`,
      allClear ? 'success' : 'warn'
    );
  } catch (err) {
    showToast('Audit failed: ' + err.message, 'error');
  } finally {
    btn.disabled = false;
    btn.innerHTML = '<span>🔍</span> Run Integrity Audit';
  }
}

function displayLedgerBanner(status, detail) {
  const banner = document.getElementById('ledgerBanner');
  const isOK   = status === 'LEDGER_INTACT';

  banner.className = `glass rounded-xl px-5 py-4 flex items-start gap-4 mb-4 ring-1 ${
    isOK ? 'ring-emerald-500/30' : 'ring-red-500/40'
  }`;
  banner.innerHTML = `
    <span class="text-2xl mt-0.5">${isOK ? '🔗' : '💀'}</span>
    <div>
      <p class="font-semibold text-sm ${isOK ? 'text-emerald-400' : 'text-red-400'}">
        ${isOK ? 'Ledger Intact — No Missing Rows' : 'CRITICAL: Ledger Tampered — Rows Missing'}
      </p>
      <p class="text-xs text-slate-400 mt-1">${escHtml(detail)}</p>
    </div>`;
  banner.classList.remove('hidden');
}

function displayAudit(results) {
  const panel     = document.getElementById('auditPanel');
  const container = document.getElementById('auditResults');

  if (!results || results.length === 0) {
    container.innerHTML = `<p class="text-slate-500 text-sm">No active records to audit.</p>`;
    panel.classList.remove('hidden');
    return;
  }

  container.innerHTML = results.map(r => {
    const cfg = statusConfig(r.status);
    return `
      <div class="glass rounded-xl p-4 flex flex-col sm:flex-row sm:items-start gap-4 ${cfg.ring}">
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-3 flex-wrap">
            <span class="font-semibold text-white text-sm">${escHtml(r.student_name)}</span>
            <span class="text-xs text-slate-500 mono">#${r.id}</span>
            <span class="text-xs text-slate-500">${escHtml(r.subject_code)} · ${r.marks}/100</span>
          </div>
          <p class="detail-text mt-2">${escHtml(r.detail)}</p>
          <div class="mt-2 flex gap-2 flex-wrap">
            <span class="hash-pill mono" title="${escHtml(r.stored_hash)}">stored: ${r.stored_hash.slice(0,12)}…</span>
            <span class="hash-pill mono" title="${escHtml(r.recomputed_hash)}">recomputed: ${r.recomputed_hash.slice(0,12)}…</span>
          </div>
        </div>
        <div class="shrink-0">
          <span class="text-xs font-bold px-3 py-1.5 rounded-full ${cfg.badge}">${cfg.label}</span>
        </div>
      </div>`;
  }).join('');

  panel.classList.remove('hidden');
  panel.scrollIntoView({ behavior: 'smooth', block: 'start' });
}

function statusConfig(status) {
  switch (status) {
    case 'SECURE_INTEGRITY_MAINTAINED':
      return { label: '✅ SECURE', badge: 'badge-secure', ring: 'ring-1 ring-emerald-500/20' };
    case 'TAMPERED_VIA_SQLI':
      return { label: '🚨 SQLI TAMPERED', badge: 'badge-sqli', ring: 'ring-1 ring-amber-500/20' };
    case 'TAMPERED_UNAUTHORIZED':
      return { label: '⛔ UNAUTHORIZED TAMPER', badge: 'badge-tampered', ring: 'ring-1 ring-red-500/20' };
    case 'TAMPERED_ROW_DELETED':
      return { label: '💀 ROW DELETED', badge: 'badge-tampered', ring: 'ring-1 ring-red-500/30' };
    default:
      return { label: status, badge: '', ring: '' };
  }
}

// ── Utilities ─────────────────────────────────────────────────────────────────

function marksColor(m) {
  if (m >= 75) return 'text-emerald-400';
  if (m >= 50) return 'text-amber-400';
  return 'text-red-400';
}

function formatDate(iso) {
  if (!iso) return '—';
  const d = new Date(iso);
  return d.toLocaleDateString('en-IN', { day: '2-digit', month: 'short', year: 'numeric' });
}

function escHtml(str) {
  return String(str ?? '')
    .replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')
    .replace(/"/g,'&quot;').replace(/'/g,'&#39;');
}

function escJs(str) {
  return String(str ?? '').replace(/\\/g,'\\\\').replace(/'/g,"\\'");
}

async function copyHash(hash) {
  try {
    await navigator.clipboard.writeText(hash);
    showToast('Full hash copied to clipboard.', 'info');
  } catch { showToast('Could not copy — try manually.', 'warn'); }
}

// ── Bootstrap ─────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
  loadStudents();
});
