import React, { useEffect, useRef, useState } from 'react';
import { Compartment, StateEffect } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { markdownLanguage } from '@codemirror/lang-markdown';
import Storage from '@/utils/storage';
import { LOGGED_TOKEN_STORAGE_KEY } from '@/common/constants';
import { loggedUserInfoStore } from '@/stores';
import { useTranslation } from 'react-i18next';
import { createImporter, downloadAndUpload } from './image-import';

export default function ImageImportEditor({ editor }) {
  const { i18n } = useTranslation();
  const enabled = /^\/(questions(?:\/|$)|posts\/)/.test(location.pathname);
  const userID = loggedUserInfoStore(s => s.user.id);
  const [state, setState] = useState({busy:false, pending:0, failed:0, complete:0});
  const task = useRef<ReturnType<typeof createImporter> | null>(null);
  useEffect(() => {
    if (!enabled) return undefined;
    const importer = createImporter({
      editor, parser:markdownLanguage.parser, origin:location.origin,
      identity:() => Storage.get(LOGGED_TOKEN_STORAGE_KEY) || '',
      importImage:(source, token, signal) => downloadAndUpload(source, token, signal, fetch,
        () => Storage.get(LOGGED_TOKEN_STORAGE_KEY) === token),
      notify:setState,
    });
    task.current = importer;
    const compartment = new Compartment();
    editor.dispatch({effects:StateEffect.appendConfig.of(compartment.of(
      EditorView.updateListener.of(update => { if (update.docChanged) importer.schedule(); })
    ))});
    importer.schedule();
    const form = editor.dom.closest('form');
    const guard = (event) => {
      if (!importer.isBusy()) return;
      const target = event.target instanceof Element ? event.target.closest('button') : null;
      if (event.type === 'submit' || (target && !target.closest('.md-editor-wrap') &&
          (target.type === 'submit' || target.classList.contains('btn-primary')))) {
        event.preventDefault(); event.stopImmediatePropagation();
      }
    };
    form?.addEventListener('submit', guard, true);
    form?.addEventListener('click', guard, true);
    return () => {
      importer.dispose(); task.current = null;
      form?.removeEventListener('submit', guard, true);
      form?.removeEventListener('click', guard, true);
      editor.dispatch({effects:compartment.reconfigure([])});
    };
  }, [editor, userID, enabled]);
  if (!enabled) return null;
  const english = i18n.language === 'en_US';
  return <span className="metar-image-import d-inline-flex align-items-center gap-2 flex-wrap" role="status" aria-live="polite">
    <button type="button" className="btn btn-sm btn-link" disabled={state.busy} onClick={() => task.current?.retry()}>
      {state.failed ? (english ? 'Retry images' : '重试图片') : (english ? 'Import external images' : '导入外链图片')}
    </button>
    {(state.busy || state.failed > 0 || state.complete > 0) && <small>
      {state.busy ? (english ? `Importing images (${state.complete} saved)…` : `正在导入图片（已保存 ${state.complete} 张）…`) : state.failed ? (english ? `${state.failed} failed; original links kept.` : `${state.failed} 张未导入，已保留原地址`) : (english ? `${state.complete} images saved` : `已保存 ${state.complete} 张图片`)}
    </small>}
  </span>;
}
