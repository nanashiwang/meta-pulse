(function(root,factory){
 const api=factory();
 if(typeof module==='object'&&module.exports)module.exports=api;
 if(root.document){root.MetarReturnNavigation=api;api.install(root);}
})(typeof window==='undefined'?globalThis:window,function(){
 const key='_metar_return_origins_v1';
 const area=path=>/^\/admin(?:\/|$)/.test(path)?'/admin':path;
 function local(value,origin){
  try {const u=new URL(value,origin);
   if(u.origin!==origin||!value||/[\\]/.test(value)||/^\/(?:answer|api)(?:\/|$)/.test(u.pathname)||/^\/users\/(?:login|logout|register|auth-landing|confirm-email|account-activation|password-reset)/.test(u.pathname))return null;
   return u.pathname+u.search;
  }catch(_){return null;}
 }
 function create(host){
  const read=()=>{try{return JSON.parse(host.sessionStorage.getItem(key)||'{}')}catch(_){return {}}};
  function remember(from,to){
   const source=local(from,host.location.origin),destination=local(to,host.location.origin);
   if(!source||!destination)return;
   const sourceArea=area(new URL(source,host.location.origin).pathname),destArea=area(new URL(destination,host.location.origin).pathname);
   if(sourceArea===destArea)return;
   const entries=read();entries[destArea]=source;
   try{host.sessionStorage.setItem(key,JSON.stringify(Object.fromEntries(Object.entries(entries).slice(-50))))}catch(_){}
  }
  function destination(path,fallback){
   const saved=local(read()[area(path)],host.location.origin);
   return saved&&area(new URL(saved,host.location.origin).pathname)!==area(path)?saved:fallback;
  }
  return {remember,destination};
 }
 function install(host){
  if(host.__metarReturnNavigation)return host.__metarReturnNavigation;
  const controller=create(host);host.__metarReturnNavigation=controller;
  host.document.addEventListener('click',event=>{
   if(event.defaultPrevented||event.button!==0||event.ctrlKey||event.metaKey||event.shiftKey||event.altKey)return;
   const a=event.target.closest?.('a[href]');
   if(!a||a.hasAttribute('download')||(a.target&&a.target!=='_self'))return;
   controller.remember(host.location.href,a.getAttribute('href'));
  },true);
  return controller;
 }
 return {create,install};
});
