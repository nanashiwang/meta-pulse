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

    if (targetPath === '/questions') {
      const targetOrder = targetQuery.get('order');
      if (targetOrder) return path === '/questions' && query.get('order') === targetOrder;
      return (path === '/questions' && query.get('order') !== 'unanswered') || path.startsWith('/question/');
    }
    if (targetPath === '/topics') return path === '/topics' || path.startsWith('/topic/');
    return path === targetPath || path.startsWith(`${targetPath}/`);
  }

  class AnswerAdapter {
    constructor(config) {
      this.base = relativePath(config.answerApiBase, '/answer/api/v1').replace(/\/$/, '');
      this.timeoutMs = Number.isInteger(config.requestTimeoutMs) ? config.requestTimeoutMs : DEFAULT_TIMEOUT_MS;
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

    listQuestions({ page = 1, pageSize = 20, order = 'active', tag = '' } = {}) {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize), order });
      if (tag) params.set('tag', tag);
      return this.request(`/question/page?${params}`);
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
      return this.request(`/tags/page?${params}`);
    }

    search(query, { page = 1, size = 30, order = 'relevance' } = {}) {
      const params = new URLSearchParams({ q: query, page: String(page), size: String(size), order });
      return this.request(`/search?${params}`);
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

  // Only operation identifiers are kept for response-loss recovery; balances and
  // reward outcomes always come from the authenticated server.
  class PulseOperation {
    constructor(userId) { this.key = `_metar_pending_pulse:${String(userId)}`; }
    read() {
      try {
        const value = JSON.parse(window.sessionStorage.getItem(this.key) || 'null');
        if (value && /^[a-f0-9-]{36}$/.test(value.actionId) && value.actionId === value.idempotencyKey) return value;
      } catch (_) { /* Missing storage never creates an operation. */ }
      return null;
    }
    begin() {
      const previous = this.read();
      if (previous) return previous;
      const id = window.crypto.randomUUID();
      const value = { actionId: id, idempotencyKey: id };
      try { window.sessionStorage.setItem(this.key, JSON.stringify(value)); }
      catch (_) { throw new AdapterError('无法保存本次请求，请允许站点存储后重试', { code: 'storage_unavailable' }); }
      return value;
    }
    clear() { window.sessionStorage.removeItem(this.key); }
  }

  function formatPulseQuota(amount, perUnit) {
    if (!Number.isSafeInteger(amount) || amount < 0) return '待核对';
    if (!Number.isSafeInteger(perUnit) || perUnit <= 0) return `${amount} quota`;
    const n = BigInt(amount), d = BigInt(perUnit), scale = 1000000n;
    const tail = ((n % d) * scale / d).toString().padStart(6, '0').replace(/0+$/, '');
    const approximate = (n % d) * scale % d !== 0n ? '≈' : '';
    return `${approximate}${n / d}${tail ? `.${tail}` : ''} API 额度`;
  }

  class PulseAdapter {
    constructor(answer) { this.answer = answer; }
    async request(path, options = {}) {
      const controller = new AbortController();
      const timeout = window.setTimeout(() => controller.abort(), this.answer.timeoutMs);
      const headers = new Headers({ Accept: 'application/json' });
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
          ...(options.operation ? { body: JSON.stringify({ action_id: options.operation.actionId }) } : {}),
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

  window.MetarAdapters = Object.freeze({ AdapterError, AnswerAdapter, PulseAdapter, PulseOperation, formatPulseQuota, KnowledgeAdapter, loadIdentitySnapshot, relativePath, routeMatchesNavigation });
})();
