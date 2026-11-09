import { useEffect, useRef } from 'react';
import { createCaptchaController } from './controller';
import { presentWidget, reportError } from './widget';

function useCaptcha({ captchaKey, commonProps }) {
  const controller = useRef<ReturnType<typeof createCaptchaController> | null>(null);
  const options = useRef(commonProps);
  options.current = commonProps;
  function create() {
    return createCaptchaController({
      captchaKey,
      request: async (path) => {
        const response = await fetch(path, { credentials: 'same-origin', headers: options.current?.headers, signal: AbortSignal.timeout(10000) });
        if (!response.ok) throw new Error('captcha request failed');
        const body = await response.json();
        if (body.code !== 200) throw new Error('captcha request failed');
        return body;
      },
      present: presentWidget,
      report: reportError,
    });
  }
  if (!controller.current) controller.current = create();
  useEffect(() => {
    // Recreate after StrictMode's setup/cleanup replay as well as action changes.
    controller.current = create();
    return () => controller.current?.dispose();
  }, [captchaKey]);
  return {
    check: (submit) => controller.current!.check(submit),
    getCaptcha: () => controller.current!.getCaptcha(),
    resolveCaptchaReq: (req) => controller.current!.resolveCaptchaReq(req),
    handleCaptchaError: (errors) => controller.current!.handleCaptchaError(errors),
    close: () => controller.current!.close(),
  };
}
export default {
  info: { type: 'captcha', slug_name: 'metar_turnstile' },
  component: () => null,
  hooks: { useCaptcha },
};
