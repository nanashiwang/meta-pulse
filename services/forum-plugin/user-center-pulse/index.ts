import './drafts-store.js';
import storageExpires from '@/utils/storageWithExpires';
import './growth-presentation.js';
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
import { installAccountMenu } from './account-menu';

installAccountMenu({
  getUser: () => loggedUserInfoStore.getState().user,
  getLanguage: () => i18n.language,
  onLanguageChanged: (handler) => i18n.on('languageChanged', handler),
  subscribeUser: (handler) => loggedUserInfoStore.subscribe(handler),
});
import { installPasswordVisibility } from './password-visibility';
import './password-visibility.css';

installPasswordVisibility({
  getLanguage: () => i18n.language,
  onLanguageChanged: (handler) => i18n.on('languageChanged', handler),
});

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

// Account-scoped drafts retain Answer's native editor and publish cleanup.
(window as any).MetarDrafts.install(storageExpires, {
  getUser: () => loggedUserInfoStore.getState().user,
  get storage() { return window.localStorage; },
  url: () => window.location.href,
  error: () => {
    let notice = document.getElementById('metar-draft-error');
    if (!notice) {
      notice = document.createElement('div');
      notice.id = 'metar-draft-error'; notice.setAttribute('role', 'alert');
      notice.className = 'alert alert-warning'; document.body.prepend(notice);
    }
    notice.textContent = i18n.language === 'en_US' ? 'Draft could not be saved. Keep this page open and copy your text before leaving; check storage space or edits in another tab.' : '草稿未能保存。请保留本页并复制正文，再检查存储空间或其他标签页的编辑。';
  },
});
