// Reuse Answer anchors and their handlers; decorate only their appearance.
const paths = {
  "trophy": "<path d=\"M8 3h8v6a4 4 0 0 1-8 0V3Zm0 2H4v2a4 4 0 0 0 4 4m8-6h4v2a4 4 0 0 1-4 4m-4 2v5m-4 3h8m-6-3h4l2 3H8Z\"/>",
  "language": "<path d=\"m5 8 6 6m-7 0 6-6 2-5M2 5h12M7 2v3m7 17 4-10 4 10m-6.8-3h5.6\"/>",
  "search": "<circle cx=\"10.8\" cy=\"10.8\" r=\"6.6\"/><path d=\"m16 16 4.2 4.2\"/>",
  "chat": "<path d=\"M20 11.5a8 8 0 0 1-8 8H4l1.6-4A8 8 0 1 1 20 11.5Z\"/><path d=\"M8 10h8m-8 4h5\"/>",
  "bell": "<path d=\"M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4\"/>",
  "shield": "<path d=\"m12 2 9 4v6c0 5-9 10-9 10S3 17 3 12V6z\"/><path d=\"m8 11 3 3 5-5\"/>",
  "sun": "<circle cx=\"12\" cy=\"12\" r=\"4\"/><path d=\"M12 1v2m0 18v2M1 12h2m18 0h2M4 4l2 2m12 12 2 2M4 20l2-2M18 6l2-2\"/>",
  "moon": "<path d=\"M21 13A9 9 0 0 1 11 3a9 9 0 1 0 10 10Z\"/>",
  "target": "<circle cx=\"12\" cy=\"12\" r=\"9\"/><circle cx=\"12\" cy=\"12\" r=\"5\"/><circle cx=\"12\" cy=\"12\" r=\"1\"/>"
};
export function headerAccess(user) {
 const active=Boolean(user?.username)&&user.mail_status===1&&user.status==='normal';
 return {chat:active,review:active&&user.role_id===2};
}
export function headerIcon(name) {
 return '<svg class="metar-header-icon" viewBox="0 0 24 24" aria-hidden="true">'+(paths[name]||paths.target)+'</svg>';
}
export function decorateNativeHeader(doc, {english=false,dark=false,chat=false,review=false}={}) {
 const row=doc.querySelector('#header > .w-100');if(!row)return;
 const icon=(node,name)=>{if(!node)return;let decoration=node.querySelector(':scope > .metar-header-decoration');if(!decoration){decoration=doc.createElement('span');decoration.className='metar-header-decoration';decoration.setAttribute('aria-hidden','true');node.append(decoration);}if(decoration.dataset.icon!==name){decoration.innerHTML=headerIcon(name);decoration.dataset.icon=name;}if(!node.classList.contains('metar-header-action'))node.classList.add('metar-header-action');};
 icon(row.querySelector('a[href="/users"]'),'target');
 icon(row.querySelector('a[href="/users/notifications/inbox"]'),'bell');
 icon(row.querySelector('a[href="/users/notifications/achievement"]'),'trophy');
 icon(row.querySelector('a[href="/review"]'),'shield');
 const add=(cls,href,show,label,name)=>{let link=row.querySelector('.'+cls);if(!show){link?.remove();return;}if(!link){link=doc.createElement('a');link.className=cls;link.href=href;row.append(link);}if(link.getAttribute('aria-label')!==label){link.setAttribute('aria-label',label);link.title=label;}icon(link,name);};
 add('metar-header-chat','/chat',chat,english?'Chat':'聊天','chat');
 // The role comes from Answer's current user store, never from DOM inference.
 add('metar-header-review','/review',(review||Boolean(doc.querySelector('#sideNav a[href="/review"]')))&&!row.querySelector('.navbar-nav a[href="/review"]:not(.metar-header-review)'),english?'Review':'审查','shield');
 const input=row.querySelector('form input[type="search"]');if(input){const placeholder=english?'Search questions and answers…':'搜索真实问题与回答…';if(!input.dataset.metarSuggestion&&input.placeholder!==placeholder)input.placeholder=placeholder;if(input.getAttribute('aria-label')!==placeholder)input.setAttribute('aria-label',placeholder);const form=input.closest('form');const search=form.querySelector('.search-wrap');if(search&&!search.querySelector('.metar-header-icon')){const decoration=doc.createElement('span');decoration.className='metar-header-decoration';decoration.innerHTML=headerIcon('search');search.append(decoration);}if(!form.querySelector('.metar-search-shortcut')){const key=doc.createElement('kbd');key.className='metar-search-shortcut';key.textContent='⌘ K';form.append(key);}}
}
