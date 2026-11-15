/* Personal browser preferences only; Answer remains the tag/content source. */
(function(root,factory){const api=factory();if(typeof module==='object'&&module.exports)module.exports=api;else root.MetarTagShortcuts=api;})(typeof window==='undefined'?globalThis:window,function(){
 'use strict';
 const defaults=['gossip','wiki','cloud-asset','feeds','reading','resource','original','feedback'];
 const limit=12;
 function clean(values){return Array.isArray(values)?[...new Set(values.filter(v=>typeof v==='string'&&v.length>0&&v.length<=100))].slice(0,limit):null;}
 function items(catalog){const seen=new Set();return [...(catalog.categories||[]),...(catalog.tags||[])].filter(v=>v&&typeof v.slug_name==='string'&&v.slug_name&&!seen.has(v.slug_name)&&seen.add(v.slug_name));}
 function initial(list){const known=new Set(list.map(v=>v.slug_name));const picked=defaults.filter(v=>known.has(v));return picked.length?picked:list.slice(0,limit).map(v=>v.slug_name);}
 function href(slug,order='active'){const base=slug?'/topic/'+encodeURIComponent(slug):'/latest';return base+'?'+new URLSearchParams({order:['active','newest','hot','score','unanswered'].includes(order)?order:'active'});}
 function preferences(storage,owner){const key='metar-tag-shortcuts-v1:'+encodeURIComponent(owner||'guest');return {read(){try{return clean(JSON.parse(storage.getItem(key)||'null'));}catch(_){return null;}},write(values){const next=clean(values);if(next===null)throw Error('invalid preference');storage.setItem(key,JSON.stringify(next));return next;}};}
 async function mount(node,{catalog,allTags,owner,selected='',order='active',english=false,host=window}){
  const t=(zh,en)=>english?en:zh;const doc=node.ownerDocument;let disposed=false,list=[],pinned=[],draft=[],editor=null,full=false,editing=false;const saved=preferences({getItem:k=>host.localStorage.getItem(k),setItem:(k,v)=>host.localStorage.setItem(k,v)},owner);
  const el=(tag,text,cls)=>{const n=doc.createElement(tag);if(text!==undefined)n.textContent=text;if(cls)n.className=cls;return n;};
  const button=(text,fn,cls='tag-shortcut-control')=>{const b=el('button',text,cls);b.type='button';b.onclick=fn;return b;};
  const name=slug=>{const item=list.find(v=>v.slug_name===slug);return item?.display_name||slug;};
  let notice='';
  async function ensureFull(){if(full)return;const extra=await allTags();if(disposed||!node.isConnected)return;list=items({categories:list,tags:extra});full=true;}
  function render(){
   if(disposed||!node.isConnected)return;
   node.replaceChildren();node.className='tag-shortcuts';
   const head=el('div',undefined,'tag-shortcuts-head');head.append(el('strong',t('我的常用标签','My shortcuts')),button(t('自定义','Customize'),async()=>{editing=!editing;draft=[...pinned];notice='';render();if(editing){try{await ensureFull();render();}catch(_){notice=t('标签目录暂时无法完整加载，请稍后重试。','The full tag list could not be loaded. Please retry.');render();}}}));node.append(head);
   const nav=el('nav',undefined,'tag-shortcuts-grid');nav.setAttribute('aria-label',t('按标签筛选帖子','Filter posts by tag'));
   const link=slug=>{const a=el('a',slug?name(slug):t('全部','All'),'tag-shortcut');a.href=href(slug,order);a.setAttribute('data-router','');if(selected===slug)a.setAttribute('aria-current','page');return a;};
   nav.append(link(''));pinned.forEach(slug=>nav.append(link(slug)));
   nav.append(button(t('更多标签','More tags'),async()=>{const existing=node.querySelector('[data-shortcut-more]');if(existing){existing.remove();return;}const more=el('div',undefined,'tag-shortcuts-more');more.dataset.shortcutMore='';node.append(more);more.append(el('p',t('加载标签…','Loading tags…')));try{await ensureFull();if(!more.isConnected)return;more.replaceChildren();const search=el('input');search.type='search';search.placeholder=t('搜索标签','Search tags');search.setAttribute('aria-label',search.placeholder);const results=el('div',undefined,'tag-shortcuts-grid');const update=()=>{const value=search.value.trim().toLowerCase();results.replaceChildren();list.filter(v=>(v.display_name+' '+v.slug_name).toLowerCase().includes(value)).slice(0,80).forEach(v=>results.append(link(v.slug_name)));if(!results.childNodes.length)results.append(el('p',t('没有匹配标签','No matching tags')));};search.oninput=update;more.append(search,results);update();}catch(_){more.replaceChildren(el('p',t('标签加载失败，请重新打开重试。','Could not load tags. Close and reopen to retry.')));}}));node.append(nav);
   if(selected&&!pinned.includes(selected))node.append(el('p',t('当前筛选：','Current filter: ')+name(selected),'tag-shortcuts-hint'));
   if(!editing)return;
   editor=el('section',undefined,'tag-shortcuts-editor');editor.setAttribute('aria-label',t('自定义常用标签','Customize shortcuts'));node.append(editor);
   editor.append(el('p',t('最多 12 个，仅保存在本浏览器；左右箭头调整顺序。','Up to 12 shortcuts, saved in this browser. Use arrows to reorder.')));
   const chosen=el('div',undefined,'tag-shortcuts-chosen');draft.forEach((slug,i)=>{const row=el('div',undefined,'tag-shortcuts-chip');row.append(el('span',name(slug)));for(const [dir,label] of [[-1,'←'],[1,'→']]){const move=button(label,()=>{const to=i+dir;[draft[i],draft[to]]=[draft[to],draft[i]];render();});move.disabled=i+dir<0||i+dir>=draft.length;move.setAttribute('aria-label',name(slug)+' '+t(dir<0?'左移':'右移',dir<0?'Move left':'Move right'));row.append(move);}row.append(button('×',()=>{draft=draft.filter(v=>v!==slug);render();}));chosen.append(row);});editor.append(chosen);
   const search=el('input');search.type='search';search.placeholder=t('搜索并添加标签','Find a tag to add');search.setAttribute('aria-label',search.placeholder);editor.append(search);const available=el('div',undefined,'tag-shortcuts-available-tags');editor.append(available);
   function renderAvailable(){available.replaceChildren();list.filter(v=>!draft.includes(v.slug_name)&&(v.display_name+' '+v.slug_name).toLowerCase().includes(search.value.trim().toLowerCase())).slice(0,60).forEach(v=>{const b=button('＋ '+(v.display_name||v.slug_name),()=>{if(draft.length>=limit)return;draft.push(v.slug_name);render();});b.disabled=draft.length>=limit;available.append(b);});}search.oninput=renderAvailable;renderAvailable();
   const actions=el('div',undefined,'tag-shortcuts-actions');actions.append(button(t('保存','Save'),()=>{try{pinned=saved.write(draft);editing=false;notice='';render();}catch(_){notice=t('保存失败，请允许站点存储后重试。','Save failed. Allow site storage and retry.');render();}},'btn primary'),button(t('取消','Cancel'),()=>{editing=false;render();},'btn'),button(t('恢复默认','Reset'),()=>{draft=initial(list);render();}));editor.append(actions);const status=el('p',notice||draft.length+' / '+limit,'tag-shortcuts-hint');status.setAttribute('role','status');editor.append(status);
  }
  try{const data=await catalog();if(!node.isConnected)return()=>{};list=items(data);const stored=saved.read();
   pinned=stored===null?initial(list):stored;
   if(stored!==null&&pinned.some(slug=>!list.some(item=>item.slug_name===slug))){
    try{await ensureFull();}catch(_){/* keep the saved slugs; known names render as slugs until the catalog recovers */}
   }
   render();}catch(_){if(node.isConnected)node.replaceChildren(el('p',t('标签暂时无法加载，可继续浏览帖子。','Tags are unavailable. You can still browse posts.')));}
  return ()=>{disposed=true;};
 }
 return {clean,items,initial,href,preferences,mount};
});
