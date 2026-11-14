/* Same-origin Answer views inside the personal workspace. No copied forms or credentials. */
(function(root,factory){const api=factory();if(typeof module==='object'&&module.exports)module.exports=api;else root.MetarPersonalWorkspace=api;})(typeof window==='undefined'?globalThis:window,function(){
 'use strict';
 const tabs=new Set(['profile','notify','account','interface']);
  function nativeTarget(path,query=new URLSearchParams()){
  if(path==='/me/settings')return '/users/settings/'+(tabs.has(query.get('tab'))?query.get('tab'):'profile');
  if(path==='/me/notifications')return '/users/notifications/'+(query.get('view')==='achievement'?'achievement':'inbox');
  if(path==='/me/badges'){
   const id=query.get('badge');if(id&&!/^[a-zA-Z0-9]+$/.test(id))return null;
   const params=new URLSearchParams();for(const key of ['username','page'])if(query.get(key))params.set(key,query.get(key));
   return '/badges'+(id?'/'+id:'')+(params.size?'?'+params:'');
  }
  return null;
 }
 function workspaceURL(value,origin){
  try{const u=new URL(value,origin);if(u.origin!==origin)return null;
   const setting=u.pathname.match(/^\/users\/settings\/([a-zA-Z0-9_-]+)\/?$/);
   if(setting&&tabs.has(setting[1]))return '/me/settings?tab='+setting[1];
   const notice=u.pathname.match(/^\/users\/notifications\/(inbox|achievement)\/?$/);
   if(notice)return '/me/notifications?view='+notice[1];
   const badge=u.pathname.match(/^\/badges(?:\/([a-zA-Z0-9]+))?\/?$/);
   if(badge){const p=new URLSearchParams();if(badge[1])p.set('badge',badge[1]);for(const key of ['username','page'])if(u.searchParams.get(key))p.set(key,u.searchParams.get(key));return '/me/badges'+(p.size?'?'+p:'');}
  }catch(_){}return null;
 }
 function isSave(method,value,origin){
  try{return method==='PUT'&&new URL(value,origin).origin===origin&&new Set(['/answer/api/v1/user/info','/answer/api/v1/user/interface','/answer/api/v1/user/notification/config','/answer/api/v1/user/password']).has(new URL(value,origin).pathname);}catch(_){return false;}
 }
 class View{
  constructor(root,{target,onRoute,onNavigate,onExit,onSaved,t,host=window}){
   this.host=host;this.root=root;this.frame=root.querySelector('iframe');this.status=root.querySelector('[data-native-status]');this.options={target,onRoute,onNavigate,onExit,onSaved,t};this.cleanups=[];this.revision=0;this.savedRevision=0;this.path=target;this.disposed=false;
   this.frame.addEventListener('load',()=>this.loaded());
   this.timer=host.setTimeout(()=>this.fail(),15000);
   this.before=e=>{if(this.dirty()){e.preventDefault();e.returnValue='';}};host.addEventListener('beforeunload',this.before);
   this.frame.src=target;
  }
  dirty(){return this.revision!==this.savedRevision;}
  confirmLeave(){return !this.dirty()||this.host.confirm(this.options.t('设置尚未保存，确定离开？'));}
  fail(){if(this.disposed)return;this.status.hidden=false;this.status.textContent=this.options.t('设置页面加载较慢，可使用下方链接打开原页面。');}
  clear(){for(const cleanup of this.cleanups.splice(0))cleanup();}
  loaded(){
   if(this.disposed)return;this.clear();this.host.clearTimeout(this.timer);
   let child,doc,url;
   try{child=this.frame.contentWindow;doc=child.document;url=new URL(child.location.href);if(url.origin!==this.host.location.origin)throw Error('origin');}catch(_){this.fail();return;}
   if(!workspaceURL(url.href,this.host.location.origin)){this.frame.hidden=true;this.options.onExit(url.pathname+url.search+url.hash);return;}
   this.path=url.pathname+url.search;this.revision=0;this.savedRevision=0;
   doc.documentElement.classList.add('metar-personal-content');
   const style=doc.createElement('link');style.rel='stylesheet';style.href='/metar-assets/personal-workspace.css';doc.head.append(style);
   const resize=()=>{if(!this.frame.isConnected)return;const content=doc.querySelector('#main')||doc.querySelector('main')||doc.querySelector('#root');const height=Math.max(560,Math.ceil(content?.getBoundingClientRect().bottom||doc.body.scrollHeight)+24);this.frame.style.height=Math.min(height,20000)+'px';};
   style.onload=()=>{resize();this.frame.hidden=false;this.status.hidden=true;};style.onerror=()=>this.fail();
   const observer=new child.ResizeObserver(resize);observer.observe(doc.body);this.cleanups.push(()=>observer.disconnect());
   const input=e=>{if(e.target.closest('form')&&!e.target.matches('input[type=search]'))this.revision++;};doc.addEventListener('input',input,true);doc.addEventListener('change',input,true);this.cleanups.push(()=>{doc.removeEventListener('input',input,true);doc.removeEventListener('change',input,true);});
   // Observe successful native saves, never inspect or copy request bodies/credentials.
   const proto=child.XMLHttpRequest.prototype,open=proto.open,send=proto.send;const pending=new WeakMap(),self=this;
   proto.open=function(method,path,...rest){pending.set(this,{save:isSave(method,path,self.host.location.origin)});return open.call(this,method,path,...rest);};
   proto.send=function(...args){const meta=pending.get(this);if(meta?.save){const revision=self.revision;this.addEventListener('load',()=>{try{const body=this.responseType==='json'?this.response:JSON.parse(this.responseText);if(this.status>=200&&this.status<300&&body?.code===200){if(self.revision===revision)self.savedRevision=revision;self.options.onSaved?.();}}catch(_){}},{once:true});}return send.apply(this,args);};
   this.cleanups.push(()=>{proto.open=open;proto.send=send;});
   let approvedTarget=null;
   const originalPush=child.history.pushState.bind(child.history),originalReplace=child.history.replaceState.bind(child.history);
   const update=(state,title,target)=>{
    const next=new URL(target||child.location.href,child.location.href),route=workspaceURL(next.href,self.host.location.origin);
    if(!route){if(self.confirmLeave())self.options.onExit(next.href);return;}
    if(next.pathname!==child.location.pathname&&approvedTarget!==next.href&&!self.confirmLeave())return;
    approvedTarget=null;
    originalReplace(state,title,target);self.path=next.pathname+next.search;self.savedRevision=self.revision;
    self.options.onRoute(route);resize();
   };
   child.history.pushState=update;child.history.replaceState=update;
   this.cleanups.push(()=>{child.history.pushState=originalPush;child.history.replaceState=originalReplace;});
   child.addEventListener('beforeunload',this.before);this.cleanups.push(()=>child.removeEventListener('beforeunload',this.before));
   const click=e=>{if(e.defaultPrevented||e.button!==0||e.ctrlKey||e.metaKey||e.altKey||e.shiftKey)return;const anchor=e.target.closest?.('a[href]');if(!anchor||anchor.hasAttribute('download')||(anchor.target&&anchor.target!=='_self')||anchor.getAttribute('href').startsWith('#'))return;
    const next=new URL(anchor.href);const mapped=workspaceURL(next.href,self.host.location.origin);if(mapped){if(mapped.startsWith('/me/badges')){e.preventDefault();e.stopImmediatePropagation();if(self.confirmLeave())self.options.onNavigate(mapped);return;}if(next.pathname!==child.location.pathname){if(!self.confirmLeave()){e.preventDefault();e.stopImmediatePropagation();return;}approvedTarget=next.href;}return;}
    e.preventDefault();e.stopImmediatePropagation();if(self.confirmLeave())self.options.onExit(next.href);
   };doc.addEventListener('click',click,true);this.cleanups.push(()=>doc.removeEventListener('click',click,true));
   this.options.onRoute(workspaceURL(url.href,this.host.location.origin),true);
  }
  dispose(){this.disposed=true;this.clear();this.host.clearTimeout(this.timer);this.host.removeEventListener('beforeunload',this.before);}
 }
 return {nativeTarget,workspaceURL,isSave,View};
});
