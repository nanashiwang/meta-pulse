(function(root){
 'use strict';
 const endpoint='/metar/ads', key='metar.homeAds.closed.v1';
 const imageURL=id=>'/answer/api/v1/metar/ads/'+encodeURIComponent(id);
 let closed=false, timer;
 async function mountSlot(slot,api){
   try{if(closed || root.sessionStorage.getItem(key))return;}catch(_){}
   const items=await api.request(endpoint);
   if(!slot.isConnected || closed || !items.length)return;
   clearInterval(timer);
   const link=document.createElement('a'), image=document.createElement('img'), badge=document.createElement('small'), close=document.createElement('button');
   link.target='_blank';link.rel='sponsored noopener noreferrer';image.alt='访问广告网站';link.append(image);badge.textContent='广告';close.type='button';close.textContent='×';close.setAttribute('aria-label','关闭广告');
   let index=0;
   const show=()=>{link.href=items[index].link;image.src=imageURL(items[index].id);};
   close.onclick=()=>{closed=true;try{root.sessionStorage.setItem(key,'1');}catch(_){} clearInterval(timer);slot.hidden=true;slot.replaceChildren();};
   slot.replaceChildren(link,badge,close);show();slot.hidden=false;
   if(items.length>1)timer=setInterval(()=>{if(!slot.isConnected){clearInterval(timer);return;}if(document.hidden)return;index=(index+1)%items.length;show();},15000);
 }
 const managerHTML=()=>'<section data-ad-manager class="card card-pad"><h1>广告管理</h1><p>首页右侧栏 · 多张广告每 15 秒切换，单张固定展示。</p><form><label>横版图片（JPG、PNG、WebP，最大 8 MB）<input name="image" type="file" accept="image/jpeg,image/png,image/webp" required></label><label>跳转链接<input name="link" type="url" placeholder="https://" required></label><button class="btn primary">发布广告</button></form><p role="status"></p><h2>已发布广告</h2><ul></ul></section>';
 async function picture(file){
   if(!file || file.size>8*1024*1024)throw Error('请选择不超过 8 MB 的横版图片。');
   const img=await createImageBitmap(file);try{
     if(img.width<=img.height)throw Error('请上传宽度大于高度的图片。');
     const canvas=document.createElement('canvas');canvas.width=Math.min(img.width,1280);canvas.height=Math.max(1,Math.round(img.height*canvas.width/img.width));const ctx=canvas.getContext('2d');ctx.fillStyle='white';ctx.fillRect(0,0,canvas.width,canvas.height);ctx.drawImage(img,0,0,canvas.width,canvas.height);
     const blob=await new Promise(resolve=>canvas.toBlob(resolve,'image/jpeg',0.82));if(!blob||blob.size>880*1024)throw Error('图片压缩后仍过大，请缩小后重试。');return blob;
   }finally{img.close();}
 }
 function mountManager(el,api,admin){
   const form=el.querySelector('form'),msg=el.querySelector('[role=status]'),list=el.querySelector('ul');let rev=0;
   async function refresh(){const seq=++rev;const entries=await api.request(endpoint);if(seq!==rev)return;list.replaceChildren();if(!entries.length)list.textContent='暂无广告，首页不占位。';for(const item of entries){
     const row=document.createElement('li'),img=document.createElement('img'),dest=document.createElement('span'),del=document.createElement('button');img.src=imageURL(item.id);img.alt='广告缩略图';dest.textContent=item.link;del.textContent='下架';del.className='btn small';del.onclick=async()=>{if(!root.confirm('下架这条广告？'))return;del.disabled=true;try{await admin.request(endpoint+'/'+item.id,{method:'DELETE'});msg.textContent='广告已下架。';await refresh();}catch(e){msg.textContent=e.message;del.disabled=false;}};row.append(img,dest,del);list.append(row);
   }}
   form.onsubmit=async e=>{e.preventDefault();const submit=form.querySelector('button');if(submit.disabled)return;submit.disabled=true;try{const url=new URL(form.elements.link.value);if(!['http:','https:'].includes(url.protocol)||url.username||url.password)throw Error('请填写有效的 HTTP 或 HTTPS 链接。');const body=new FormData();body.append('link',url.href);body.append('image',await picture(form.elements.image.files[0]),'banner.jpg');await admin.request(endpoint,{method:'POST',body});form.reset();msg.textContent='广告已发布。';await refresh();}catch(e){msg.textContent=e.message;}finally{submit.disabled=false;}};
   refresh().catch(e=>{msg.textContent=e.message;});
 }
 root.MetarAds={managerHTML,mountSlot,mountManager};
})(window);
