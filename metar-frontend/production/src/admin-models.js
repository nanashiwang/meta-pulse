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
      case 'invalid_api_key': return t('API key 无效或已被吊销，请到 llm-stats.com/developer 检查后重新填写。', 'The API key is invalid or revoked. Check it at llm-stats.com/developer and enter it again.');
      case 'verify_unavailable': return t('暂时无法连接 LLM Stats 验证这个 key，未保存，请稍后重试。', 'LLM Stats could not be reached to verify the key; nothing was saved. Try again shortly.');
      case 'managed_by_env': return t('该密钥由服务器环境变量配置，需在服务器上修改。', 'This key is set in the server environment and must be changed there.');
      case 'not_configured': return t('尚未配置可用的 API key。', 'No usable API key is configured.');
      case 'empty_key': return t('请先粘贴 API key。', 'Paste an API key first.');
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
    dispose() { this.generation++; this.root = null; clearTimeout(this.sourceTimer); }

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
      this.sourceMessage = '';
      this.sourceKind = '';
      await this.refreshSources().catch(() => {});
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

    // Data sources. The API key is read from the password field at the
    // moment of saving and never kept in page state, storage or responses.
    async refreshSources() {
      try { this.sources = await this.call('sources'); this.sourcesError = null; }
      catch (error) { this.sources = null; this.sourcesError = error; throw error; }
    }
    async saveSourceKey(key) {
      key = String(key || '').trim();
      if (!key) throw new AdminError('empty_key', 0);
      this.sources = await this.call('sources/llmstats', {method: 'PUT', body: JSON.stringify({api_key: key})});
    }
    async setSourceEnabled(enabled) { this.sources = await this.call('sources/llmstats', {method: 'PUT', body: JSON.stringify({enabled: !!enabled})}); }
    async clearSourceKey() { this.sources = await this.call('sources/llmstats', {method: 'PUT', body: JSON.stringify({clear: true})}); }
    async requestSourceSync() { this.sources = await this.call('sources/llmstats/sync', {method: 'POST'}); }

    sourcesSection() {
      const title = `<h2>${t('数据源 · LLM Stats', 'Data source · LLM Stats')}</h2>`;
      if (!this.sources) return `<section class="card card-pad mt16 intel-admin-sources">${title}<p class="muted">${esc(errorText(this.sourcesError))}</p></section>`;
      const s = this.sources.llm_stats || {};
      const status = s.origin === 'env' ? t(`已配置（服务器环境变量，末四位 ${s.key_hint || '—'}）`, `Configured in the server environment (…${s.key_hint || '—'})`)
        : !s.configured ? t('未配置', 'Not configured')
        : s.key_stale ? t('已保存的 key 无法解密（管理密钥已更换），请重新填写', 'The saved key can no longer be decrypted (admin secret rotated); enter it again')
        : !s.enabled ? t(`已停用（末四位 ${s.key_hint || '—'}）`, `Disabled (…${s.key_hint || '—'})`)
        : t(`已启用（后台保存，末四位 ${s.key_hint || '—'}）`, `Enabled (saved here, …${s.key_hint || '—'})`);
      const ok = s.configured && s.enabled && !s.key_stale;
      const last = s.last_attempt_at ? `<p class="muted">${t('上次同步：', 'Last sync: ')}${time(s.last_attempt_at)} · ${s.last_status === 'ok' ? `<span class="green">${t('成功', 'succeeded')}</span>` : `<span class="danger">${t('失败', 'failed')}</span>`} · ${esc(s.last_requests)} ${t('次请求', 'requests')}${s.last_success_at && s.last_status !== 'ok' ? ` · ${t('最近成功：', 'last success: ')}${time(s.last_success_at)}` : ''}</p>${s.last_error ? `<p class="danger intel-admin-source-error"><code>${esc(s.last_error)}</code></p>` : ''}${s.last_report ? `<details class="mt8"><summary>${t('同步摘要', 'Sync summary')}</summary><code class="intel-admin-report">${esc(s.last_report)}</code></details>` : ''}`
        : `<p class="muted">${t('尚未同步。', 'Not synced yet.')}</p>`;
      const requested = s.sync_requested_at ? `<p class="green" role="status">${t('已请求立即同步，约 30 秒内开始，完成后此处自动刷新。', 'Sync requested; it starts within about 30 seconds and this panel refreshes when done.')}</p>` : '';
      const form = s.can_edit ? `<div class="intel-admin-key-row mt16"><input type="password" data-intel-source-key autocomplete="off" spellcheck="false" maxlength="220" placeholder="sk_ze_…" aria-label="${t('LLM Stats API key', 'LLM Stats API key')}"><button class="btn primary small" data-intel-source="save">${s.configured ? t('替换并验证', 'Replace and verify') : t('保存并验证', 'Save and verify')}</button></div>` : '';
      const actions = [
        ok ? `<button class="btn small" data-intel-source="sync"${s.sync_requested_at ? ' disabled' : ''}>${t('立即同步', 'Sync now')}</button>` : '',
        s.can_edit && s.configured && !s.key_stale ? `<button class="btn small" data-intel-source="${s.enabled ? 'disable' : 'enable'}">${s.enabled ? t('停用', 'Disable') : t('启用', 'Enable')}</button>` : '',
        s.can_edit && s.configured ? `<button class="btn small" data-intel-source="clear">${t('清除 key', 'Remove key')}</button>` : '',
      ].join('');
      const msg = this.sourceMessage ? `<p class="intel-admin-message ${this.sourceKind}" role="status">${esc(this.sourceMessage)}</p>` : '';
      return `<section class="card card-pad mt16 intel-admin-sources">${title}
        <p><strong>${esc(status)}</strong></p>
        <p class="muted">${t(`每 ${s.interval_hours || 24} 小时自动同步模型、评测、价格与速度；保存新 key 后会立即同步一次。获取方式：在 `, `Models, benchmarks, prices and speeds sync every ${s.interval_hours || 24} hours, and once right after a new key is saved. Get a key: sign in at `)}<a href="https://llm-stats.com/developer" target="_blank" rel="noopener noreferrer">llm-stats.com/developer</a>${t(' 登录后创建 API key（sk_ze_…）。key 加密保存，任何页面都不会显示完整 key。', ' and create an API key (sk_ze_…). The key is stored encrypted and is never shown in full.')}</p>
        ${last}${requested}${form}${actions ? `<div class="intel-side-actions mt16">${actions}</div>` : ''}${msg}
        <p class="muted intel-admin-note mt16">${t('使用该数据的页面会自动显示「Data by LLM Stats」来源链接。LLM Stats 免费计划条款写明不得再发布，公开展示前请与对方确认授权。', 'Pages using this data show a "Data by LLM Stats" credit automatically. LLM Stats\'s free plan terms prohibit redistribution; confirm permission before showing it publicly.')}</p>
      </section>`;
    }

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
        ${this.sourcesSection()}
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
      // Refresh the source panel while a requested sync is pending (max 10 min).
      let polls = 0;
      const watch = () => {
        clearTimeout(this.sourceTimer);
        if (!live() || !this.sources?.llm_stats?.sync_requested_at || polls++ > 60) return;
        this.sourceTimer = setTimeout(async () => {
          if (!live()) return;
          try { await this.refreshSources(); if (!this.sources?.llm_stats?.sync_requested_at) await this.refresh().catch(() => {}); } catch (_) { /* keep last view */ }
          if (live() && !this.busy) rerender();
          watch();
        }, 10000);
      };
      watch();
      const runSource = async action => {
        if (this.busy) return;
        this.busy = true;
        const input = container.querySelector('[data-intel-source-key]');
        container.querySelectorAll('button').forEach(b => { b.disabled = true; });
        try {
          await action(input);
          this.sourceKind = 'green';
        } catch (error) {
          this.sourceMessage = errorText(error);
          this.sourceKind = 'danger';
        } finally {
          if (input) input.value = '';
          this.busy = false;
          rerender();
          watch();
        }
      };
      container.addEventListener('click', event => {
        const sourceButton = event.target.closest('[data-intel-source]');
        if (sourceButton && container.contains(sourceButton)) {
          const op = sourceButton.dataset.intelSource;
          if (op === 'save') runSource(async input => { await this.saveSourceKey(input?.value); this.sourceMessage = t('key 验证通过并已加密保存，正在发起首次同步。', 'Key verified and stored encrypted; the first sync has been requested.'); });
          if (op === 'sync') runSource(async () => { await this.requestSourceSync(); this.sourceMessage = t('已请求同步。', 'Sync requested.'); });
          if (op === 'enable' || op === 'disable') runSource(async () => { await this.setSourceEnabled(op === 'enable'); this.sourceMessage = op === 'enable' ? t('已启用。', 'Enabled.') : t('已停用，不再自动同步。', 'Disabled; automatic sync stopped.'); });
          if (op === 'clear' && root.confirm(t('确定清除已保存的 API key？清除后停止自动同步，已有数据保留。', 'Remove the saved API key? Automatic sync stops; existing data stays.'))) runSource(async () => { await this.clearSourceKey(); this.sourceMessage = t('已清除 key。', 'Key removed.'); });
          return;
        }
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
