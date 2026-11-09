// Framework-independent lifecycle so one challenge can authorize only one submit.
export function createCaptchaController({ captchaKey, request, present, report }) {
  let epoch = 0, busy = false, disposed = false, dismiss = () => {};
  let callback, token = '', captchaID = '', required = false;
  const clear = () => { dismiss(); dismiss = () => {}; token = ''; };
  async function challenge() {
    const current = ++epoch;
    clear(); busy = true;
    try {
      const response = await request('/answer/api/v1/captcha/config');
      if (disposed || current !== epoch) return;
      const config = response?.data?.config;
      if (response?.data?.slug_name !== 'metar_turnstile' || !config?.key || config.action !== 'answer_captcha') throw new Error('configuration');
      dismiss = present(config, {
        verified(value) {
          if (disposed || current !== epoch || !value || !callback) return;
          token = value;
          const submit = callback;
          // The callback synchronously obtains the token through getCaptcha.
          // Invalidate widget callbacks before handing control to the host form.
          ++epoch; busy = false; dismiss(); dismiss = () => {};
          try { Promise.resolve(submit()).catch(() => { token = ''; report(); }); }
          catch { report(); }
          finally { token = ''; }
        },
        failed() { if (current === epoch) { token = ''; report(); } },
        cancelled() { close(); },
      });
    } catch {
      if (!disposed && current === epoch) { busy = false; report(); }
    }
  }
  function close() { ++epoch; clear(); callback = undefined; busy = false; return Promise.resolve(); }
  return {
    close,
    async check(submit) {
      if (busy || disposed) return;
      busy = true; callback = submit;
      const current = ++epoch;
      try {
        const response = await request(`/answer/api/v1/user/action/record?action=${encodeURIComponent(captchaKey)}`);
        if (disposed || current !== epoch) return;
        const data = response?.data;
        if (typeof data?.verify !== 'boolean') throw new Error('strategy');
        required = data.verify; captchaID = data.captcha_id || '';
        // Email includes registration and must never be silently downgraded.
        if (captchaKey === 'email' && !required) throw new Error('captcha disabled');
        if (required) { await challenge(); return; }
        busy = false; return submit();
      } catch { if (!disposed && current === epoch) { busy = false; report(); } }
    },
    getCaptcha() {
      const result = { verify: required, captcha_id: captchaID, captcha_code: token };
      token = ''; return result;
    },
    resolveCaptchaReq(req) {
      const data = this.getCaptcha();
      if (data.verify) { req.captcha_id = data.captcha_id; req.captcha_code = data.captcha_code; }
    },
    handleCaptchaError(errors = []) {
      const error = errors.find((item) => item.error_field === 'captcha_code');
      token = '';
      if (error && callback) { required = true; void challenge(); }
      else void close();
      return error;
    },
    dispose() { disposed = true; void close(); },
  };
}
