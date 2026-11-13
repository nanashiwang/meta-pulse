(function(root,factory){
 const api=factory();
 if(typeof module==='object'&&module.exports)module.exports=api;
 if(root.document){root.MetarReturnNavigation=api;api.install(root);}
})(typeof window==='undefined'?globalThis:window,function(){
 'use strict';
 const stateKey='metarReturnV2', pendingKey='_metar_return_pending_v2';
 const area=url=>/^\/admin(?:\/|$)/.test(url.split('?')[0])?'/admin':url;
 function local(value,origin){
  if(typeof value!=='string'||!value||/[\\\u0000-\u0020]/.test(value))return null;
  try{const u=new URL(value,origin);
   if(u.origin!==origin||/^\/(?:answer|api|metar\/api)(?:\/|$)/.test(u.pathname)||/^\/users\/(?:login|logout|register|auth-landing|confirm-email|account-activation|password-reset|account-recovery|activate|email-verification)(?:\/|$)/.test(u.pathname))return null;
   return u.pathname+u.search;
  }catch(_){return null;}
 }
 function create(host){
  let current=null;
  const safe=url=>local(url,host.location.origin);
  function restore(value,url){
   const target=safe(url);
   if(!target){current=null;return;}
   const valid=value&&value.url===target&&Number.isSafeInteger(value.step)&&value.step>=0&&Array.isArray(value.trail)&&value.trail.length<=50&&value.trail.every((e,i,all)=>e&&safe(e.url)===e.url&&Number.isSafeInteger(e.step)&&e.step>=0&&e.step<value.step&&(!i||e.step>all[i-1].step));
   current=valid?{url:target,step:value.step,trail:value.trail.map(e=>({...e}))}:{url:target,step:0,trail:[]};
  }
  function advance(url,replace=false){
   const target=safe(url);
   if(!target){current=null;return;}
   if(!current){restore(null,target);return;}
   const trail=current.trail.slice();
   if(!replace&&area(current.url)!==area(target))trail.push({url:current.url,step:current.step});
   current={url:target,step:current.step+(replace?0:1),trail:trail.slice(-50)};
  }
  function destination(_path,fallback){return current?.trail.at(-1)?.url||fallback;}
  function distance(){const prev=current?.trail.at(-1);return prev?current.step-prev.step:0;}
  function snapshot(){return current?{...current,trail:current.trail.map(e=>({...e}))}:null;}
  // Intent only: no history changes until a navigation actually commits.
  let intent=null;
  function remember(from,to){intent=safe(from)&&safe(to)?{from:safe(from),to:safe(to)}:null;}
  return {restore,advance,destination,distance,snapshot,remember,intent:()=>intent,clearIntent:()=>{intent=null;}};
 }
 function install(host){
  if(host.__metarReturnNavigation)return host.__metarReturnNavigation;
  const nav=create(host),history=host.history,doc=host.document;
  if(!history?.replaceState)return nav;
  host.__metarReturnNavigation=nav;
  const originalPush=history.pushState.bind(history),originalReplace=history.replaceState.bind(history);
  const url=()=>host.location.pathname+host.location.search;
  const write=()=>originalReplace({...history.state,[stateKey]:nav.snapshot()},'',host.location.href);
  try{host.sessionStorage.removeItem('_metar_return_origins_v1');}catch(_){}
  let pending;
  try{pending=JSON.parse(host.sessionStorage.getItem(pendingKey)||'null');host.sessionStorage.removeItem(pendingKey);}catch(_){}
  nav.restore(history.state?.[stateKey],url());
  if(!history.state?.[stateKey]&&pending&&Date.now()-pending.at>=0&&Date.now()-pending.at<30000&&pending.to===url()&&local(doc.referrer,host.location.origin)===pending.from){
   nav.restore(pending.state,pending.from);nav.advance(url());
  }
  write();
  history.pushState=function(state,title,target){
   const before=url();originalPush(state,title,target);
   if(nav.snapshot()?.url!==before)nav.restore(null,before);
   nav.advance(url());nav.clearIntent();write();
  };
  history.replaceState=function(state,title,target){
   originalReplace(state,title,target);nav.advance(url(),true);write();
  };
  host.addEventListener('popstate',()=>{nav.clearIntent();nav.restore(history.state?.[stateKey],url());write();});
  host.addEventListener('pageshow',event=>{if(event.persisted){nav.clearIntent();nav.restore(history.state?.[stateKey],url());write();}});
  doc.addEventListener('click',event=>{
   nav.clearIntent();
   if(event.defaultPrevented||event.button!==0||event.ctrlKey||event.metaKey||event.shiftKey||event.altKey)return;
   const a=event.target.closest?.('a[href]');
   if(!a||a.hasAttribute('download')||(a.target&&a.target!=='_self'))return;
   nav.remember(host.location.href,a.getAttribute('href'));
  },true);
  host.addEventListener('pagehide',()=>{
   const intent=nav.intent();
   if(!intent||intent.from!==url()||intent.from===intent.to)return;
   try{host.sessionStorage.setItem(pendingKey,JSON.stringify({...intent,state:nav.snapshot(),at:Date.now()}));}catch(_){}
  });
  nav.back=fallback=>{
   nav.clearIntent();
   const count=nav.distance();
   if(count>0){history.go(-count);return;}
   host.location.replace(local(fallback,host.location.origin)||'/latest');
  };
  return nav;
 }
 return {create,install};
});
