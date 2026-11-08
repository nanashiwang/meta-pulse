/* Model leaderboard admin (/admin/models). Requests go to the forum plugin's
   admin BFF, which re-checks the Answer admin role and signs the call to the
   Intel service. Documents are validated before import, and an import keeps
   one idempotency key until it is confirmed, so a retry never writes twice. */
(function(root) {
  'use strict';
  const BASE = '/answer/admin/api/metar/intel/';
  const MAX_BYTES = 1 << 20;
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const en = () => root.MetarI18n?.getLanguage() === 'en_US';
  const t = (zh, enText) => en() ? enText : zh;
  const time = v => { const d = new Date(v); return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString(en() ? 'en-US' : 'zh-CN', {timeZone: 'Asia/Shanghai', hour12: false}); };
  const bytes = text => new TextEncoder().encode(text).length;

  class AdminError extends Error {
    constructor(code, status, problems) { super(code); this.code = code; this.status = status; this.problems = problems || []; }
  }

  function errorText(error) {
    switch (error?.code) {
      case 'authentication_required': return t('登录已失效，请重新登录。', 'Your session has expired. Sign in again.');
      case 'forbidden': return t('需要当前有效的管理员账号。', 'An active administrator account is required.');
      case 'invalid_document': return t('文件未通过校验，请按下面的问题修改。', 'The document failed validation. Fix the problems below.');
      case 'idempotency_conflict': return t('这次导入与之前未确认的请求内容不同。请放弃原操作后重新校验。', 'This import differs from the earlier unconfirmed request. Discard it and validate again.');
      case 'document_too_large': return t('文件超过 1 MiB。请拆分，或在服务器上用 metar-intel-tool 导入。', 'The document exceeds 1 MiB. Split it or import it on the server with metar-intel-tool.');
      case 'invalid_json': return t('不是合法的 JSON。', 'This is not valid JSON.');
      case 'pending': return t('结果尚未确认。重试会沿用原请求，不会重复导入。', 'The result is unconfirmed. Retrying reuses the original request and will not import twice.');
      default: return t('模型榜服务暂时不可用，或尚未配置管理连接。', 'The model leaderboard service is unavailable or its admin connection is not configured.');
    }
  }

  function defaultFetch(answer) {
    return async (op, {method = 'GET', body, key} = {}) => {
      const headers = {Accept: 'application/json', 'X-Metar-Request': '1'};
      const token = answer?.token?.();
      if (token) headers.Authorization = token.startsWith('Bearer ') ? token : `Bearer ${token}`;
      if (body !== undefined) headers['Content-Type'] = 'application/json';
      if (key) headers['Idempotency-Key'] = key;
      let response;
      try {
        response = await root.fetch(BASE + op, {method, headers, body, credentials: 'same-origin', cache: 'no-store', redirect: 'error'});
      } catch (_) { throw new AdminError(method === 'POST' && key ? 'pending' : 'unavailable', 0); }
      let payload = null;
      try { payload = await response.json(); } catch (_) { /* handled below */ }
      if (!response.ok || !payload || typeof payload !== 'object') {
        const code = response.status === 413 ? 'document_too_large' : payload?.error || (key && response.status >= 500 ? 'pending' : 'unavailable');
        throw new AdminError(code, response.status, Array.isArray(payload?.problems) ? payload.problems.slice(0, 200).map(String) : []);
      }
      return payload;
    };
  }

  const PENDING = 'metar-intel-import-pending-v1:';

  class View {
    constructor(answer, fetcher) {
      this.call = fetcher || defaultFetch(answer);
      this.storage = (() => { try { return root.sessionStorage; } catch (_) { return null; } })();
      this.generation = 0;
    }
    dispose() { this.generation++; this.root = null; }

    async page(user) {
      this.dispose();
      this.user = user;
      this.text = '';
      this.validated = null; // {text, summary, key}
      this.problems = [];
      this.message = '';
      this.messageKind = '';
      this.pending = this.readPending();
      try {
        this.overview = await this.call('overview');
        this.loadError = null;
      } catch (error) {
        this.overview = null;
        this.loadError = error;
      }
      return this.render();
    }

    pendingKey() { return this.user?.id ? PENDING + this.user.id : null; }
    readPending() {
      const k = this.pendingKey();
      if (!k || !this.storage) return null;
      try {
        const v = JSON.parse(this.storage.getItem(k) || 'null');
        return v && typeof v.key === 'string' && typeof v.text === 'string' ? v : null;
      } catch (_) { return null; }
    }
    writePending(v) {
      const k = this.pendingKey();
      if (!k || !this.storage) return;
      try { v ? this.storage.setItem(k, JSON.stringify(v)) : this.storage.removeItem(k); } catch (_) { /* in-memory only */ }
    }

    setText(text) {
      this.text = text;
      if (this.validated && this.validated.text !== text) this.validated = null;
    }

    async validate() {
      this.problems = [];
      this.message = '';
      const text = this.text;
      if (bytes(text) > MAX_BYTES) throw new AdminError('document_too_large', 413);
      try { JSON.parse(text); } catch (_) { throw new AdminError('invalid_json', 0); }
      const res = await this.call('validate', {method: 'POST', body: text});
      this.validated = {text, summary: res.summary || {}, key: root.crypto.randomUUID()};
      return this.validated;
    }

    // Import only the exact text that passed validation, under its key. The
    // pending record survives reloads until the server confirms the result.
    async importValidated() {
      const job = this.pending || (this.validated && this.validated.text === this.text ? {key: this.validated.key, text: this.validated.text} : null);
      if (!job) throw new AdminError('invalid_document', 0, [t('请先校验当前内容。', 'Validate the current content first.')]);
      this.pending = job;
      this.writePending(job);
      const res = await this.call('import', {method: 'POST', body: job.text, key: job.key});
      this.pending = null;
      this.writePending(null);
      this.validated = null;
      this.lastImport = res;
      return res;
    }

    discardPending() { this.pending = null; this.writePending(null); }

    async recompute() { return this.call('recompute', {method: 'POST'}); }
    async exportDocument() { return this.call('export'); }
    async refresh() { this.overview = await this.call('overview'); this.loadError = null; }

    render() {
      const head = `<div class="page-title intel-admin-head"><div><h1>${t('模型榜管理', 'Model leaderboard admin')}</h1><p class="muted">${t('导入与修正模型、价格、评测和速度数据，所有导入记录操作人。', 'Import and correct models, prices, benchmarks and speeds. Every import records who ran it.')}</p></div><div class="intel-side-actions"><a data-router class="btn" href="/models">${t('查看榜单', 'View leaderboard')}</a></div></div>`;
      if (!this.overview) return `${head}<section class="card card-pad intel-state" role="alert"><h2>${t('无法读取模型榜状态', 'Unable to load the leaderboard status')}</h2><p class="muted">${esc(errorText(this.loadError))}</p></section>`;
      const o = this.overview, c = o.counts || {};
      const stat = (label, v) => `<div class="card intel-stat"><span class="muted">${label}</span><strong>${Number(v || 0).toLocaleString()}</strong></div>`;
      const snap = o.snapshot ? `${t('当前榜单：', 'Current board: ')}${esc(o.snapshot.rows)} ${t('个模型', 'models')} · ${t('方法 ', 'method ')}${esc(o.snapshot.method_version)} · ${t('计算于 ', 'computed ')}${time(o.snapshot.computed_at)}` : t('尚未生成榜单。导入数据后会自动计算。', 'No board yet. It is computed automatically after an import.');
      const summary = this.validated ? `<div class="intel-admin-ok" role="status"><strong>${t('校验通过', 'Valid')}</strong> · ${Object.entries(this.validated.summary).map(([k, v]) => `${esc(k)} ${esc(v)}`).join(' · ')}</div>` : '';
      const problems = this.problems.length ? `<div class="intel-admin-problems" role="alert"><strong>${t('需要修改：', 'Needs fixing:')}</strong><ul>${this.problems.map(p => `<li><code>${esc(p)}</code></li>`).join('')}</ul></div>` : '';
      const message = this.message ? `<p class="intel-admin-message ${this.messageKind}" role="status">${esc(this.message)}</p>` : '';
      const pending = this.pending ? `<div class="card card-pad intel-admin-pending" role="status"><p>${esc(errorText({code: 'pending'}))}</p><div class="intel-side-actions mt16"><button class="btn primary small" data-intel-admin="retry">${t('重试原操作', 'Retry original operation')}</button><button class="btn small" data-intel-admin="discard">${t('放弃', 'Discard')}</button></div></div>` : '';
      const runs = (o.imports || []).map(r => `<tr><td>${esc(r.id)}</td><td>${time(r.created_at)}</td><td><code>${esc(r.source)}</code></td><td>${esc(r.actor_id || t('命令行', 'CLI'))}</td><td class="intel-num">${esc(r.catalog_rows)}</td><td class="intel-num">${esc(r.observations_added)}</td><td class="intel-num">${esc(r.observations_skipped)}</td></tr>`).join('') || `<tr><td colspan="7" class="intel-empty-row">${t('还没有导入记录', 'No imports yet')}</td></tr>`;
      const canImport = !!this.pending || (this.validated && this.validated.text === this.text);
      return `${head}
        <section class="intel-stats">${stat(t('厂商', 'Providers'), c.providers)}${stat(t('模型', 'Models'), c.models)}${stat(t('评测', 'Benchmarks'), c.benchmarks)}${stat(t('售卖条目', 'Offerings'), c.offerings)}${stat(t('价格记录', 'Price records'), c.prices)}${stat(t('评测结果', 'Results'), c.benchmark_results)}</section>
        <section class="card card-pad mt16"><div class="intel-section-head"><p class="muted">${snap}</p><div class="intel-side-actions"><button class="btn small" data-intel-admin="recompute">${t('立即重算', 'Recompute now')}</button><button class="btn small" data-intel-admin="export">${t('导出当前数据', 'Export current data')}</button></div></div></section>
        ${pending}
        <section class="card card-pad mt16 intel-admin-import"><h2>${t('导入数据', 'Import data')}</h2>
          <p class="muted">${t('上传或粘贴导入文件（JSON，最大 1 MiB）。先校验，通过后再导入。导出文件可直接修改后重新导入；修改前请先改 source.slug。', 'Upload or paste an import document (JSON, up to 1 MiB). Validate first, then import. An export can be edited and imported again; change source.slug first.')}</p>
          <label class="intel-admin-file">${t('选择文件', 'Choose file')}<input type="file" accept="application/json,.json" data-intel-admin-file></label>
          <textarea data-intel-admin-text rows="14" spellcheck="false" aria-label="${t('导入文件内容', 'Import document')}" placeholder='{"source":{"slug":"manual-2026-11"},"models":[...]}'>${esc(this.text)}</textarea>
          ${summary}${problems}${message}
          <div class="intel-side-actions mt16"><button class="btn" data-intel-admin="validate">${t('校验', 'Validate')}</button><button class="btn primary" data-intel-admin="import"${canImport ? '' : ' disabled'}>${t('导入', 'Import')}</button></div>
        </section>
        <section class="card card-pad mt16"><h2>${t('最近导入', 'Recent imports')}</h2><div class="intel-table-wrap"><table class="intel-table"><thead><tr><th scope="col">#</th><th scope="col">${t('时间', 'Time')}</th><th scope="col">${t('来源', 'Source')}</th><th scope="col">${t('操作人 ID', 'Operator ID')}</th><th scope="col" class="intel-num">${t('目录', 'Catalog')}</th><th scope="col" class="intel-num">${t('新增', 'Added')}</th><th scope="col" class="intel-num">${t('已存在', 'Existing')}</th></tr></thead><tbody>${runs}</tbody></table></div></section>`;
    }

    mount(container) {
      this.root = container;
      const generation = this.generation;
      const live = () => this.root === container && container.isConnected && generation === this.generation;
      const rerender = () => { if (live()) container.innerHTML = this.render(); };
      const run = async (action, after) => {
        if (this.busy) return;
        this.busy = true;
        container.querySelectorAll('button').forEach(b => { b.disabled = true; });
        try { await after(await action()); }
        catch (error) {
          this.message = errorText(error);
          this.messageKind = 'danger';
          this.problems = error?.problems || [];
        } finally {
          this.busy = false;
          rerender();
        }
      };
      container.addEventListener('input', event => {
        if (event.target.matches('[data-intel-admin-text]')) {
          const before = !!this.validated;
          this.setText(event.target.value);
          if (before && !this.validated) {
            const btn = container.querySelector('[data-intel-admin="import"]');
            if (btn && !this.pending) btn.disabled = true;
          }
        }
      });
      container.addEventListener('change', async event => {
        const input = event.target;
        if (!input.matches('[data-intel-admin-file]') || !input.files?.[0]) return;
        const file = input.files[0];
        if (file.size > MAX_BYTES) { this.message = errorText({code: 'document_too_large'}); this.messageKind = 'danger'; rerender(); return; }
        this.setText(await file.text());
        this.problems = [];
        this.message = '';
        rerender();
      });
      container.addEventListener('click', event => {
        const button = event.target.closest('[data-intel-admin]');
        if (!button || !container.contains(button)) return;
        const op = button.dataset.intelAdmin;
        if (op === 'validate') run(() => this.validate(), () => { this.message = ''; });
        if (op === 'import' || op === 'retry') run(() => this.importValidated(), async res => {
          const s = res.stats || {};
          this.text = '';
          this.message = (s.replayed ? t('这次导入此前已完成，未重复写入。', 'This import had already completed; nothing was written again.') : t(`导入完成：目录 ${s.catalog_rows} 行，新增 ${s.observations_added} 条记录，${s.observations_skipped} 条已存在。`, `Imported: ${s.catalog_rows} catalog rows, ${s.observations_added} new records, ${s.observations_skipped} already present.`)) + (res.snapshot?.stored ? t(' 榜单已更新。', ' The board was updated.') : t(' 榜单无变化。', ' The board is unchanged.'));
          this.messageKind = 'green';
          await this.refresh().catch(() => {});
        });
        if (op === 'discard') { this.discardPending(); rerender(); }
        if (op === 'recompute') run(() => this.recompute(), async res => {
          this.message = res.stored ? t(`已生成新榜单（${res.rows} 个模型）。`, `New board computed (${res.rows} models).`) : t('数据无变化，榜单保持不变。', 'No data changed; the board is the same.');
          this.messageKind = 'green';
          await this.refresh().catch(() => {});
        });
        if (op === 'export') run(() => this.exportDocument(), doc => {
          const blob = new Blob([JSON.stringify(doc, null, 2) + '\n'], {type: 'application/json'});
          const url = root.URL.createObjectURL(blob);
          const a = root.document.createElement('a');
          a.href = url;
          a.download = `metar-models-${new Date().toISOString().slice(0, 10)}.json`;
          root.document.body.append(a);
          a.click();
          a.remove();
          setTimeout(() => root.URL.revokeObjectURL(url), 1000);
          this.message = t('已导出当前数据。', 'Current data exported.');
          this.messageKind = 'green';
        });
      });
    }
  }

  const api = {View, AdminError, errorText, MAX_BYTES};
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.MetarModelsAdmin = api;
})(typeof window === 'undefined' ? globalThis : window);
