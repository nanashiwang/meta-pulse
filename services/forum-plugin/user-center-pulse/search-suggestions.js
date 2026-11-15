/* Public title suggestions only. Never sends a user token or stores browsing history. */
(function(root,factory){const api=factory();if(typeof module==='object'&&module.exports)module.exports=api;root.MetarSearchSuggestions=api;})(typeof window==='undefined'?globalThis:window,function(){
 'use strict';
 function titles(payload){if(!Array.isArray(payload?.data?.list))return [];return [...new Set(payload.data.list.filter(v=>v&&typeof v.title==='string'&&v.title.trim()&&v.title.length<=500).map(v=>v.title.trim()))].slice(0,12);}
 function choose(list,current,random=Math.random){const pool=list.filter(x=>x!==current);return pool.length?pool[Math.min(pool.length-1,Math.floor(random()*pool.length))]:list[0]||'';}
 function install(host=window,{navigate=value=>host.location.assign(value)}={}){
  if(host.__metarSearchSuggestions)return host.__metarSearchSuggestions;
  const doc=host.document;let input=null,list=[],controller=null,pending=false,lastFetch=0,disposed=false,scheduled=false;
  function detect(){const next=doc.querySelector('.topbar .searchbox input[type="search"],#header > .w-100:first-child form input[type="search"]');if(next!==input){input=next;if(input){input.dataset.searchDefault=input.placeholder;input.dataset.metarSuggestion='';rotate();}}}
  function rotate(){if(!input?.isConnected||input.value||doc.activeElement===input||doc.hidden)return;const value=choose(list,input.dataset.metarSuggestion);if(value){input.dataset.metarSuggestion=value;input.placeholder=value;}}
  async function load(){if(pending||doc.hidden||Date.now()-lastFetch<60000)return;pending=true;lastFetch=Date.now();controller=new AbortController();const timer=host.setTimeout(()=>controller.abort(),6000);
   try{const response=await host.fetch('/answer/api/v1/question/page?page=1&page_size=12&order=hot',{credentials:'omit',redirect:'error',headers:{Accept:'application/json'},signal:controller.signal});if(!response.ok)throw Error('unavailable');const data=titles(await response.json());if(!disposed){list=data;detect();rotate();}}
   catch(_){/* Keep normal search functional when suggestions are unavailable. */}
   finally{host.clearTimeout(timer);pending=false;}
  }
  function submit(event){if(!input||event.target!==input.form||input.value.trim()||!input.dataset.metarSuggestion)return;event.preventDefault();event.stopImmediatePropagation();navigate('/search?'+new URLSearchParams({q:input.dataset.metarSuggestion}));}
  function click(event){if(event.button!==0||event.ctrlKey||event.metaKey||event.shiftKey||event.altKey||!input||input.value.trim()||!input.dataset.metarSuggestion)return;const wrap=event.target.closest?.('#header .search-wrap');if(wrap&&wrap.closest('form')===input.form){event.preventDefault();event.stopImmediatePropagation();navigate('/search?'+new URLSearchParams({q:input.dataset.metarSuggestion}));}}
  function schedule(){if(scheduled)return;scheduled=true;host.requestAnimationFrame(()=>{scheduled=false;if(!disposed)detect();});}
  const observer=new host.MutationObserver(schedule);observer.observe(doc.documentElement,{childList:true,subtree:true});
  doc.addEventListener('submit',submit,true);doc.addEventListener('click',click,true);
  const focus=()=>{detect();void load();};host.addEventListener('focus',focus);
  const timer=host.setInterval(()=>{detect();rotate();},8000);
  const api={dispose(){disposed=true;controller?.abort();observer.disconnect();host.clearInterval(timer);doc.removeEventListener('submit',submit,true);doc.removeEventListener('click',click,true);host.removeEventListener('focus',focus);if(input?.isConnected){input.placeholder=input.dataset.searchDefault||'';delete input.dataset.metarSuggestion;}delete host.__metarSearchSuggestions;}};
  host.__metarSearchSuggestions=api;detect();void load();return api;
 }
 return {titles,choose,install};
});
