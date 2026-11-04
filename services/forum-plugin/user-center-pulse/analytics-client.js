/* Optional first-party analytics. No token copies, URLs, content or account IDs. */
(function(root,factory){
 const api=factory();
 if(typeof module==='object'&&module.exports) module.exports=api;
 if(root?.document){root.MetarAnalytics=api;api.install(root);}
})(typeof window==='undefined'?globalThis:window,function(){
 'use strict';
 function contentPath(path){return /^\/(?:questions|question)\/[a-zA-Z0-9]+(?:\/|$)/.test(path)&&!/^\/questions\/(?:ask|edit|write)(?:\/|$)/.test(path);}
 function measurable(path){return !/^\/(?:admin|answer|api|metar\/api|pulse|chat|settings|me)(?:\/|$)/.test(path);}
 function source(referrer,origin){try{const u=new URL(referrer);return u.origin===origin?'':u.hostname.toLowerCase().replace(/[^a-z0-9.-]/g,'').slice(0,128);}catch(_){return '';}}
 function install(host){
  if(host.__metarAnalytics||host.navigator?.doNotTrack==='1'||host.navigator?.globalPrivacyControl)return;
  host.__metarAnalytics=true;
  let key='',seconds=0,read=false,last=Date.now(),interaction=last,lastSent=0,busy=false,retryAt=0;
  const doc=host.document;
  const token=()=>{try{return host.localStorage.getItem('_a_ltk_')||'';}catch(_){return '';}};
  for(const event of ['pointerdown','keydown','scroll','touchstart'])host.addEventListener(event,()=>{interaction=Date.now();},{passive:true});
  async function send(kind,auth){
   if(busy||Date.now()<retryAt)return false;
   busy=true;const sentKey=key;const controller=new AbortController();const timeout=host.setTimeout(()=>controller.abort(),3000);
   try{
    const headers={'Content-Type':'application/json','X-Metar-Request':'1'};if(auth)headers.Authorization=auth.startsWith('Bearer ')?auth:`Bearer ${auth}`;
    const res=await host.fetch('/answer/api/v1/metar/analytics/'+(auth?'visit':'anonymous'),{method:'POST',headers,credentials:'same-origin',redirect:'error',signal:controller.signal,body:JSON.stringify({kind,source:source(doc.referrer,host.location.origin)})});
    if(!res.ok){retryAt=Date.now()+60000;return false;}if(key===sentKey)lastSent=Date.now();return key===sentKey;
   }catch(_){retryAt=Date.now()+60000;return false;}finally{host.clearTimeout(timeout);busy=false;}
  }
  async function tick(){
   const now=Date.now(),elapsed=Math.min(2,Math.max(0,(now-last)/1000));last=now;
   const path=host.location.pathname,auth=token(),next=path+'|'+auth+'|'+Math.floor((now+28800000)/86400000);
   if(next!==key){key=next;seconds=0;read=false;lastSent=0;interaction=now;}
   if(!measurable(path)||doc.visibilityState!=='visible'||!doc.hasFocus())return;
   if(!lastSent){await send('visit',auth);return;}
   if(contentPath(path)&&now-interaction<60000)seconds+=elapsed;
   if(!read&&seconds>=30){const sentKey=key;if(await send('read',auth)&&key===sentKey)read=true;}
   else if(now-lastSent>=60000)await send('visit',auth);
  }
  host.setInterval(tick,1000);
  doc.addEventListener('visibilitychange',()=>{last=Date.now();if(doc.visibilityState==='visible')interaction=last;});
 }
 return {contentPath,measurable,source,install};
});
