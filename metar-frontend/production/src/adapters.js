/* METAR production adapters. Answer remains the identity and community content source of truth. */
'use strict';
(() => {
  const t = (...args) => window.MetarI18n?.t(...args) ?? args[0];
  const ANSWER_TOKEN_KEY = '_a_ltk_';
  const DEFAULT_TIMEOUT_MS = 10000;

  class AdapterError extends Error {
    constructor(message, options = {}) {
      super(message);
      this.name = 'AdapterError';
      this.status = options.status || 0;
      this.code = options.code || 'unknown';
      this.recoverable = options.recoverable !== false;
      this.details = options.details || null;
    }
  }

  const relativePath = (value, fallback) => {
    if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//') || value.includes('://')) return fallback;
    return value;
  };

  async function loadIdentitySnapshot(loader) {
    try {
      return { state: 'ready', user: await loader(), error: null };
    } catch (error) {
      if (error instanceof AdapterError && error.code === 'unauthorized') {
        return { state: 'ready', user: null, error: null };
      }
      return { state: 'unavailable', user: null, error };
    }
  }

  function routeMatchesNavigation(path, search, target) {
    const [targetPath, targetSearch = ''] = String(target).split('?', 2);
    const query = new URLSearchParams(String(search || '').replace(/^\?/, ''));
    const targetQuery = new URLSearchParams(targetSearch);

    if (targetPath === '/latest') {
      const targetOrder = targetQuery.get('order');
      if (targetOrder) return path === '/latest' && query.get('order') === targetOrder;
      return (path === '/latest' && query.get('order') !== 'unanswered') || path.startsWith('/question/');
    }
    if (targetPath === '/me') return path === '/me';
    if (targetPath === '/topics') return path === '/topics' || path.startsWith('/topic/');
    return path === targetPath || path.startsWith(`${targetPath}/`);
  }

  class AnswerAdapter {
    constructor(config) {
      this.base = relativePath(config.answerApiBase, '/answer/api/v1').replace(/\/$/, '');
      this.timeoutMs = Number.isInteger(config.requestTimeoutMs) ? config.requestTimeoutMs : DEFAULT_TIMEOUT_MS;
      this.contentCache = new Map();
      this.contentEpoch = 0;
    }

    token() {
      try { return window.localStorage.getItem(ANSWER_TOKEN_KEY) || ''; } catch (_) { return ''; }
    }

    async request(path, options = {}) {
      const controller = new AbortController();
      const timeout = window.setTimeout(() => controller.abort(), this.timeoutMs);
      const headers = new Headers(options.headers || {});
      headers.set('Accept', 'application/json');
      headers.set('Accept-Language', window.MetarI18n?.locale() || 'zh-CN');
      const token = this.token();
      if (token) headers.set('Authorization', token.startsWith('Bearer ') ? token : `Bearer ${token}`);
      try {
        const response = await fetch(`${this.base}${path}`, {
          ...options,
          headers,
          credentials: 'same-origin',
          signal: controller.signal,
          redirect: 'error',
        });
        let payload = null;
        try { payload = await response.json(); } catch (_) { /* handled below */ }
        if (!response.ok) {
          const type = payload?.data?.type || '';
          const code = response.status === 401 ? 'unauthorized' : type === 'inactive' ? 'inactive' : `http_${response.status}`;
          throw new AdapterError(payload?.msg || `${t("社区服务返回 ")}${response.status}`, { status: response.status, code, details: payload?.data });
        }
        if (!payload || typeof payload !== 'object' || !Object.prototype.hasOwnProperty.call(payload, 'data')) {
          throw new AdapterError(t("社区服务返回了无法识别的数据"), { code: 'invalid_response' });
        }
        return payload.data;
      } catch (error) {
        if (error instanceof AdapterError) throw error;
        if (error?.name === 'AbortError') throw new AdapterError(t("社区服务响应超时，请稍后重试"), { code: 'timeout' });
        throw new AdapterError(t("暂时无法连接社区服务，请稍后重试"), { code: 'network', details: String(error) });
      } finally {
        window.clearTimeout(timeout);
      }
    }

    getCurrentUser() { return this.request('/user/info'); }

    clearContentCache() { this.contentCache.clear(); this.contentEpoch++; }

    // Only public browsing lists use this bounded, short-lived memory cache.
    // Identity, permissions, binding, notifications and financial reads stay live.
    content(path) {
      if (!/^\/(?:question\/page|tags\/page|search)\?/.test(path)) return this.request(path);
      const scope = `${this.token()}\u0000${window.MetarI18n?.locale() || 'zh-CN'}`;
      if (this.contentScope !== scope) { this.clearContentCache(); this.contentScope = scope; }
      const existing = this.contentCache.get(path);
      if (existing && (existing.pending || existing.expires > Date.now())) return existing.promise;
      const epoch = this.contentEpoch;
      const entry = { pending: true, expires: 0 };
      entry.promise = this.request(path).then(value => {
        entry.pending = false;
        entry.expires = Date.now() + 15000;
        return value;
      }).catch(error => {
        if (epoch === this.contentEpoch && this.contentCache.get(path) === entry) this.contentCache.delete(path);
        throw error;
      });
      this.contentCache.delete(path);
      this.contentCache.set(path, entry);
      if (this.contentCache.size > 24) this.contentCache.delete(this.contentCache.keys().next().value);
      return entry.promise;
    }

    listQuestions({ page = 1, pageSize = 20, order = 'active', tag = '' } = {}) {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize), order });
      if (tag) params.set('tag', tag);
      return this.content(`/question/page?${params}`);
    }

    getQuestion(id) {
      return this.request(`/question/info?id=${encodeURIComponent(id)}`);
    }

    listAnswers(questionId, { page = 1, pageSize = 50 } = {}) {
      const params = new URLSearchParams({ question_id: questionId, page: String(page), page_size: String(pageSize), order: 'default' });
      return this.request(`/answer/page?${params}`);
    }

    listTags({ page = 1, pageSize = 24, order = 'popular' } = {}) {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize), query_cond: order });
      return this.content(`/tags/page?${params}`);
    }

    search(query, { page = 1, size = 30, order = 'relevance' } = {}) {
      const params = new URLSearchParams({ q: query, page: String(page), size: String(size), order });
      return this.content(`/search?${params}`);
    }

    getProfile(username) {
      return this.request(`/personal/user/info?username=${encodeURIComponent(username)}`);
    }

    listPersonalQuestions(username, { page = 1, pageSize = 20 } = {}) {
      const params = new URLSearchParams({ username, page: String(page), page_size: String(pageSize), order: 'newest' });
      return this.request(`/personal/question/page?${params}`);
    }

    listBookmarks(username, { page = 1, pageSize = 20 } = {}) {
      const params = new URLSearchParams({ username, page: String(page), page_size: String(pageSize) });
      return this.request(`/personal/collection/page?${params}`);
    }

    listNotifications({ page = 1, pageSize = 20 } = {}) {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize), type: 'inbox' });
      return this.request(`/notification/page?${params}`);
    }

    async getBindingState() {
      const connectors = await this.request('/connector/user/info');
      const list = Array.isArray(connectors) ? connectors : [];
      const connector = list.find((item) => String(item?.link || '').includes('pulse_user_center')) || null;
      if (!connector) return { status: 'unavailable', connector: null };
      return { status: connector.binding ? 'bound' : 'unbound', connector };
    }
  }

  // Only operation identifiers and the server choice are kept for recovery; balances and
  // reward outcomes always come from the authenticated server.
  class PulseOperation {
    constructor(userId) { this.key = `_metar_pending_pulse:${String(userId)}`; }
    read() {
      try {
        const value = JSON.parse(window.sessionStorage.getItem(this.key) || 'null');
        const valid = item => item && /^[a-f0-9-]{36}$/.test(item.actionId) && item.actionId === item.idempotencyKey
          && ((item.protocolVersion === undefined && item.selection === undefined) || ([2,3].includes(item.protocolVersion) && typeof item.selection === 'string' && item.selection.length > 0 && item.selection.length <= 1024));
        if (value === null) return null;
        if (valid(value) && (!value.actions || ([1, 5].includes(value.actions.length) && value.actions.every(item => valid(item) && item.protocolVersion === value.protocolVersion && (value.protocolVersion === 3 || item.selection === value.selection))
          && value.actions[0].selection === value.selection && value.actions[0].actionId === value.actionId && new Set(value.actions.map(item => item.actionId)).size === value.actions.length))) return value;
        throw new Error('Invalid saved operation');
      } catch (_) { throw new AdapterError('无法读取原抽奖请求，请恢复站点存储后重试', { code: 'storage_unavailable' }); }
    }
    begin(count = 1, choice) {
      const previous = this.read();
      if (previous) return previous;
      if (![1, 5].includes(count)) throw new AdapterError('invalid draw count', { code: 'invalid_request' });
      const selections = choice?.draws?.slice(0, count);
      if (choice?.selection_version !== 3 || !Array.isArray(selections) || selections.length !== count || selections.some(draw => typeof draw.selection !== 'string' || !draw.selection || draw.selection.length > 1024)) throw new AdapterError('refresh current reward rules', {code:'selection_required'});
      const actions = selections.map(draw => { const id = window.crypto.randomUUID(); return {actionId:id,idempotencyKey:id,protocolVersion:3,selection:draw.selection}; });
      const value = count === 1 ? actions[0] : { ...actions[0], actions };
      try { window.sessionStorage.setItem(this.key, JSON.stringify(value)); }
      catch (_) { throw new AdapterError('无法保存本次请求，请允许站点存储后重试', { code: 'storage_unavailable' }); }
      return value;
    }
    clear() {
      try { window.sessionStorage.removeItem(this.key); }
      catch (_) { throw new AdapterError('无法保存本次请求，请允许站点存储后重试', {code:'storage_unavailable'}); }
    }
  }

  const pulseActions = operation => operation?.actions || (operation ? [operation] : []);
  function validPulseResult(result, actionId) {
    return result && typeof result.grant_id === 'string' && result.grant_id.length > 0
      && result.action_id === actionId && Number.isSafeInteger(result.amount) && result.amount >= 0
      && ['community_exp', 'newapi_quota'].includes(result.reward_type);
  }

  // A five-draw session is five ordinary actions, each with its original key.
  // Persist identifiers before the first POST; never persist or invent outcomes.
  class PulseDrawSession {
    constructor(client, store, { wait = ms => new Promise(resolve => window.setTimeout(resolve, ms)) } = {}) {
      this.client = client; this.store = store; this.wait = wait;
    }
    async lookup(action) {
      const history = await this.client.rewards(action.actionId);
      if (!Array.isArray(history?.rewards)) throw new AdapterError('unconfirmed history', {code:'pulse_unavailable'});
      const matches = history.rewards.filter(result => validPulseResult(result, action.actionId));
      if (matches.length > 1) throw new AdapterError('duplicate grant', {code:'action_conflict'});
      // A malformed matching record is not proof that no draw was committed.
      if (!matches.length && history.rewards.length) throw new AdapterError('unconfirmed history', {code:'pulse_unavailable'});
      return matches[0];
    }
    async recover(operation) {
      const results = (await Promise.all(pulseActions(operation).map(action => this.lookup(action)))).filter(Boolean);
      if (new Set(results.map(result => result.grant_id)).size !== results.length) throw new AdapterError('duplicate grant', {code:'action_pending'});
      return results;
    }
    async resolve(action, canContinue) {
      let failure;
      for (let attempt = 0; attempt < 3; attempt++) {
        const active = () => { if (!canContinue()) throw new AdapterError('draw interrupted', {code:'action_interrupted'}); };
        active();
        if (attempt) { await this.wait(attempt === 1 ? 300 : 1000); active(); }
        try {
          // An uncertain POST may have committed. Query before retrying the
          // exact same key and selection; a failed query never means not found.
          if (attempt) {
            const recovered = await this.lookup(action);
            active();
            if (recovered) return recovered;
          }
          active();
          const result = await this.client.act(action);
          if (!validPulseResult(result, action.actionId)) throw new AdapterError('unconfirmed result', {code:'action_pending'});
          return result; // pending settlement is already a confirmed prize.
        } catch (error) {
          if (!['action_pending', 'pulse_unavailable', 'rate_limited'].includes(error.code)) throw error;
          failure = error;
        }
      }
      throw failure;
    }
    async run(operation, { known = [], onResult = () => {}, canContinue = () => true } = {}) {
      const results = [], actions = pulseActions(operation);
      for (const action of actions) {
        let result = known.find(item => validPulseResult(item, action.actionId));
        if (!result) {
          result = await this.resolve(action, canContinue);
        }
        if (results.some(item => item.grant_id === result.grant_id)) throw new AdapterError('duplicate grant', {code:'action_pending'});
        results.push(result);
        onResult([...results], actions.length);
      }
      this.store.clear();
      return results;
    }
  }

  // Presentation only: these thresholds neither choose rewards nor change odds.
  function pulseRewardTier(reward, perUnit) {
    if (reward?.reward_type !== 'newapi_quota' || !Number.isSafeInteger(reward.amount) || reward.amount <= 0
      || !Number.isSafeInteger(perUnit) || perUnit <= 0) return 'white';
    const amount = BigInt(reward.amount), unit = BigInt(perUnit);
    return amount >= unit * 10n ? 'red' : amount >= unit * 2n ? 'gold' : amount * 2n >= unit ? 'purple' : 'blue';
  }

  function formatPulseQuota(amount, perUnit, language = 'zh_CN') {
    const english = language === 'en_US' || language === 'en-US';
    if (!Number.isSafeInteger(amount) || amount < 0) return english ? 'Awaiting verification' : '待核对';
    if (!Number.isSafeInteger(perUnit) || perUnit <= 0) return `${amount} quota`;
    const n = BigInt(amount), d = BigInt(perUnit), scale = 1000000n;
    const tail = ((n % d) * scale / d).toString().padStart(6, '0').replace(/0+$/, '');
    const approximate = (n % d) * scale % d !== 0n ? '≈' : '';
    return `${approximate}${n / d}${tail ? `.${tail}` : ''} ⚡️`;
  }

  // Display-only rational arithmetic. Never feed rounded percentages into a draw.
  function formatPulseRatio(n, d) {
    if (typeof n !== 'bigint' || typeof d !== 'bigint' || n < 0n || d <= 0n) return '—';
    const scale = 10000n, scaled = n * scale / d;
    if (n > 0n && scaled === 0n) return '<0.0001';
    const tail = String(scaled % scale).padStart(4, '0').replace(/0+$/, '');
    return `${n * scale % d ? '≈' : ''}${scaled / scale}${tail ? '.' + tail : ''}`;
  }
  function formatPulseProbability(weight, total) {
    if (![weight, total].every(v => typeof v === 'bigint' || Number.isSafeInteger(v))) return '—';
    const n = BigInt(weight), d = BigInt(total);
    if (n < 0n || n > d || d <= 0n) return '—';
    return formatPulseRatio(n * 100n, d) + '%';
  }
  function pulseRewardStats(rows) {
    const stats = { total: 0n, quotaWeight: 0n, expWeight: 0n, quotaAmount: 0n, expAmount: 0n };
    if (!rows.length) throw new Error('empty reward pool');
    for (const row of rows) {
      if (![row.amount, row.weight].every(v => Number.isSafeInteger(v) && v > 0)) throw new Error('invalid reward');
      const type = row.reward_type || 'newapi_quota';
      if (!['newapi_quota', 'community_exp'].includes(type)) throw new Error('invalid reward type');
      const weight = BigInt(row.weight), amount = BigInt(row.amount);
      stats.total += weight;
      if (type === 'community_exp') { stats.expWeight += weight; stats.expAmount += amount * weight; }
      else { stats.quotaWeight += weight; stats.quotaAmount += amount * weight; }
    }
    if (stats.total > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('reward weight overflow');
    return stats;
  }

  class PulseAdapter {
    constructor(answer) { this.answer = answer; }
    async request(path, options = {}) {
      const controller = new AbortController();
      const timeout = window.setTimeout(() => controller.abort(), this.answer.timeoutMs);
      const headers = new Headers({ Accept: 'application/json' });
      headers.set('Accept-Language', window.MetarI18n?.locale() || 'zh-CN');
      const token = this.answer.token();
      if (token) headers.set('Authorization', token.startsWith('Bearer ') ? token : `Bearer ${token}`);
      if (options.operation) {
        headers.set('Content-Type', 'application/json');
        headers.set('X-Metar-Request', '1');
        headers.set('Idempotency-Key', options.operation.idempotencyKey);
      }
      try {
        const response = await fetch(`/metar/api/pulse/${path}`, {
          method: options.operation ? 'POST' : 'GET', headers, credentials: 'same-origin', redirect: 'error', signal: controller.signal,
          ...(options.operation ? { body: JSON.stringify({ action_id: options.operation.actionId, ...([2,3].includes(options.operation.protocolVersion) ? {protocol_version: options.operation.protocolVersion, selection: options.operation.selection} : {}) }) } : {}),
        });
        let payload;
        try { payload = await response.json(); } catch (_) { throw new AdapterError('暂时无法确认奖励状态，请查询原请求', { code: options.operation ? 'action_pending' : 'invalid_response' }); }
        if (!response.ok) throw new AdapterError('权益服务暂时无法完成请求', { code: payload?.error || `http_${response.status}`, status: response.status });
        if (!payload || typeof payload !== 'object' || Array.isArray(payload)) throw new AdapterError('权益数据格式异常', { code: 'invalid_response' });
        return payload;
      } catch (error) {
        if (error instanceof AdapterError) throw error;
        throw new AdapterError('暂时无法确认奖励状态，请稍后查询原请求', { code: options.operation ? 'action_pending' : 'pulse_unavailable' });
      } finally { window.clearTimeout(timeout); }
    }
    summary() { return this.request('summary'); }
    rules() { return this.request('rules'); }
    rewards(actionId = '') { return this.request(`rewards?${actionId ? `action_id=${encodeURIComponent(actionId)}` : 'limit=50'}`); }
    act(operation) { return this.request('actions', { operation }); }
  }


  const PULSE_ADMIN_SECRET_KEYS = Object.freeze([
    'PULSE_USER_BFF_HMAC_SECRET', 'PULSE_ADMIN_HMAC_SECRET', 'PULSE_FORUM_HMAC_SECRET',
    'PULSE_COMMUNITY_BFF_HMAC_SECRET', 'PULSE_SERVICE_HMAC_SECRET', 'PULSE_ROLLBACK_HMAC_SECRET',
  ].flatMap((key) => [key, `${key}_PREVIOUS`]));

  function isCommunityAdministrator(user) {
    return user?.role_id === 2 && user.mail_status === 1 && user.status === 'normal';
  }

  class PulseAdminAdapter {
    constructor(answer) { this.answer = answer; }
    async request(path, { method = 'GET', body, idempotencyKey } = {}) {
      const controller = new AbortController();
      const timeout = window.setTimeout(() => controller.abort(), this.answer.timeoutMs);
      const headers = new Headers({ Accept: 'application/json', 'Accept-Language': window.MetarI18n?.locale() || 'zh-CN', 'X-Metar-Request': '1' });
      const token = this.answer.token();
      if (token) headers.set('Authorization', token.startsWith('Bearer ') ? token : `Bearer ${token}`);
      if (method !== 'GET') {
        headers.set('Content-Type', 'application/json');
        headers.set('X-Metar-Request', '1');
      }
      if (idempotencyKey) headers.set('Idempotency-Key', idempotencyKey);
      try {
        const response = await fetch(`/metar/api/admin/pulse/${path}`, {
          method, headers, credentials: 'same-origin', redirect: 'error', cache: 'no-store', signal: controller.signal,
          ...(body === undefined ? {} : { body: JSON.stringify(body) }),
        });
        let payload;
        try { payload = await response.json(); } catch (_) {
          throw new AdapterError('配置响应无法确认', { code: method === 'PUT' ? 'settings_pending' : 'invalid_response' });
        }
        if (!response.ok) {
          const code = response.status === 401 ? 'authentication_required' : response.status === 403 ? 'admin_required' : response.status === 409 ? (path === 'periods' ? 'period_conflict' : 'settings_conflict') : response.status >= 500 && method === 'PUT' ? 'settings_pending' : payload?.error || `http_${response.status}`;
          throw new AdapterError('配置请求未完成', { status: response.status, code });
        }
        if (!payload || typeof payload !== 'object' || Array.isArray(payload)) throw new AdapterError('配置响应无法确认', { code: method === 'PUT' ? 'settings_pending' : 'invalid_response' });
        return payload;
      } catch (error) {
        if (error instanceof AdapterError) throw error;
        // Never retain server messages or fetch errors: they may contain submitted secrets.
        throw new AdapterError('配置请求未完成', { code: method === 'PUT' ? 'settings_pending' : 'settings_unavailable' });
      } finally { window.clearTimeout(timeout); }
    }
    async periods() {
      const value = await this.request('periods');
      if (!Array.isArray(value.periods) || value.periods.some((p) => typeof p.key !== 'string' || !Array.isArray(p.rules) || !Number.isSafeInteger(p.ticket_threshold_milli))) throw new AdapterError('invalid periods', {code:'invalid_response'});
      return value;
    }
    async createPeriod(body, idempotencyKey) {
      const value = await this.request('periods', {method:'PUT',body,idempotencyKey});
      if (!Number.isSafeInteger(value.period_id) || value.period_id <= 0 || value.period_key !== body.key || value.status !== 'active') throw new AdapterError('invalid period result', {code:'settings_pending'});
      return value;
    }
    async settings() { return this.validateSettings(await this.request('settings')); }
    validateSettings(value) {
      if (!Number.isSafeInteger(value?.revision) || value.revision < 0 || typeof value.config?.newapi_internal_base_url !== 'string' || !/^(0|[1-9][0-9]*)$/.test(value.config?.quota_per_unit) || typeof value.config?.actions_enabled !== 'boolean' || typeof value.config?.reward_shadow_mode !== 'boolean' || !value.secrets || typeof value.secrets !== 'object') {
        throw new AdapterError('配置响应无法确认', { code: 'invalid_response' });
      }
      // An accidental server extension must never make existing secrets readable by the UI.
      return {
        revision: value.revision, worker_ready: value.worker_ready === true, newapi_target_locked: value.newapi_target_locked === true,
        config: {
          newapi_internal_base_url: value.config.newapi_internal_base_url,
          quota_per_unit: String(value.config.quota_per_unit),
          actions_enabled: value.config.actions_enabled, reward_shadow_mode: value.config.reward_shadow_mode,
        },
        secrets: Object.fromEntries(PULSE_ADMIN_SECRET_KEYS.map((key) => [key, {
          configured: value.secrets[key]?.configured === true,
          source: ['environment', 'database', 'unset'].includes(value.secrets[key]?.source) ? value.secrets[key].source : 'unset',
        }])),
      };
    }
    async save(body, idempotencyKey) {
      if (!idempotencyKey) throw new AdapterError('缺少保存请求编号', { code: 'invalid_request' });
      const result = await this.request('settings', { method: 'PUT', body, idempotencyKey });
      try { return this.validateSettings(result); }
      catch (_) { throw new AdapterError('配置响应无法确认', { code: 'settings_pending' }); }
    }
    async generateSecret() {
      const result = await this.request('secret', { method: 'POST', body: {} });
      if (typeof result.secret !== 'string' || !/^[a-f0-9]{64}$/.test(result.secret)) throw new AdapterError('配置响应无法确认', { code: 'invalid_response' });
      return result.secret;
    }
  }

  class KnowledgeAdapter {
    constructor(config) { this.base = relativePath(config.blogBasePath, '/blog/'); }
    listArticles() {
      return [
        { id: 'home', category: t("知识库"), title: t("元衡技术博客"), description: t("模型评测、成本分析与 API 接入实践。"), href: this.base },
        { id: 'reviews', category: t("模型评测"), title: t("基于真实调用的模型评测"), description: t("说明样本范围、时间窗口与限制，不把单次结果包装成长期结论。"), href: `${this.base}reviews/` },
        { id: 'guides', category: t("接入教程"), title: t("API 接入、鉴权与错误处理"), description: t("整理密钥保管、SDK、失败重试与成本优化实践。"), href: `${this.base}guides/` },
      ];
    }
  }

  window.MetarAdapters = Object.freeze({ AdapterError, AnswerAdapter, PulseAdminAdapter, PULSE_ADMIN_SECRET_KEYS, isCommunityAdministrator, PulseAdapter, PulseOperation, PulseDrawSession, pulseActions, validPulseResult, pulseRewardTier, formatPulseQuota, formatPulseRatio, formatPulseProbability, pulseRewardStats, KnowledgeAdapter, loadIdentitySnapshot, relativePath, routeMatchesNavigation });
})();
