import i18n from '@/i18n/init';
import { loggedUserInfoStore, userCenterStore } from '@/stores';
import zhCN from '@i18n/zh_CN.yaml';
import enUS from '@i18n/en_US.yaml';
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';

import { installLanguageSwitcher } from './language-switcher';
import './language-switcher.css';
import { installNavigationBridge } from './navigation-bridge';
import { installNativeShell } from './native-shell';
import { installLocalRegistration } from './local-registration';
import './theme-tokens.css';
import './native-shell.css';

installLocalRegistration(userCenterStore);
installNavigationBridge();
installNativeShell({
  getLanguage: () => i18n.language,
  onLanguageChanged: (handler) => i18n.on('languageChanged', handler),
});

// Use the host's own translator, locale data and in-memory preference. No
// user profile/session is written and no new-api/Pulse request is involved.
i18n.addResourceBundle('zh_CN', 'translation', zhCN.ui, true, false);
i18n.addResourceBundle('en_US', 'translation', enUS.ui, true, false);
installLanguageSwitcher({
  getLanguage: () => i18n.language,
  changeLanguage: (language: string) => i18n.changeLanguage(language),
  onLanguageChanged: (handler) => {
    i18n.on('languageChanged', handler);
    return () => i18n.off('languageChanged', handler);
  },
  getUserLanguage: () => loggedUserInfoStore.getState().user.language,
  setUserLanguage: (language: string) => loggedUserInfoStore.setState((state) => ({
    user: { ...state.user, language },
  })),
  subscribeUser: (handler) => loggedUserInfoStore.subscribe(handler),
  setDateLocale: (language: string) => dayjs.locale(language === 'en_US' ? 'en' : 'zh-cn'),
});

// The enabled plugin's module is loaded once by Answer's plugin registry.
// No renderer slot is required; avoid importing pluginKit during its own init.
export default {
  info: { slug_name: 'pulse_user_center', type: 'render' },
  component: () => null,
};
