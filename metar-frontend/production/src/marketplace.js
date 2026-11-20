/* Reviewed discovery metadata; Answer owns accounts, posts and interactions. */
(function(root) {
  'use strict';
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const t = (zh, en) => root.MetarI18n?.getLanguage() === 'en_US' ? en : zh;
  const uid = () => root.crypto.randomUUID();
  const query = values => new URLSearchParams(Object.entries(values || {}).filter(([,v]) => v !== '' && v != null)).toString();
  const safeURL = (value, github = false) => {
    try {
      const u = new URL(value);
      if (u.protocol !== 'https:' || u.username || u.password || u.search || u.hash) return '';
      if (github && (u.hostname !== 'github.com' || !/^\/[\w.-]+\/[\w.-]+$/.test(u.pathname))) return '';
      return u.href;
    } catch (_) { return ''; }
  };
  const link = (path, label, cls = 'btn') => `<a data-router class="${cls}" href="${esc(path)}">${label}</a>`;
  const external = (url, label, github = false) => {
    const href = safeURL(url, github);
    return href ? `<a class="btn" href="${esc(href)}" target="_blank" rel="noopener noreferrer">${label} ↗</a>` : '';
  };
  const postURL = id => /^\d+$/.test(String(id)) ? `/questions/${id}` : '/questions';
  const uses = {productivity:['效率工具','Productivity'],content:['内容创作','Content'],development:['开发工具','Development'],education:['学习教育','Education'],experiment:['趣味实验','Experiments'],other:['其他','Other']};
  const states = {draft:['草稿','Draft'],pending_review:['待审核','In review'],published:['已发布','Published'],changes_requested:['需修改','Changes requested'],archived:['已下架','Archived']};
  const stages = {feedback:['征集反馈','Seeking feedback'],building:['开发中','Building'],launched:['已有作品','Launched'],paused:['暂停','Paused']};
  const prices = {free:['免费','Free'],freemium:['有限免费','Freemium'],paid:['收费','Paid']};
  const label = (map, key) => map[key] ? t(...map[key]) : key || '—';
  const options = (map, selected, blank = '') => (blank ? `<option value="">${blank}</option>` : '') + Object.entries(map).map(([v,l]) => `<option value="${v}"${v === selected ? ' selected' : ''}>${t(...l)}</option>`).join('');
  const send = (answer, admin, value, key) => answer.request(`/metar/marketplace/${admin ? 'admin/' : ''}command`, {method:'POST', headers:{'Content-Type':'application/json','Idempotency-Key':key,'X-Metar-Request':'1'},body:JSON.stringify(value)});
  const api = {
    list:(a,v) => a.request(`/metar/marketplace/items?${query(v)}`),
    detail:(a,id) => a.request(`/metar/marketplace/items/${encodeURIComponent(id)}`),
    mine:(a,v) => a.request(`/metar/marketplace/mine?${query(v)}`),
    adminList:(a,v) => a.request(`/metar/marketplace/admin/items?${query(v)}`),
    command:(a,v,k=uid()) => send(a,false,v,k),
    review:(a,v,k=uid()) => send(a,true,v,k),
  };
  function introductionURL(kind) {
    const idea=kind==='idea';
    const template=idea ? t('## 为谁解决什么问题\n\n## 希望怎样使用\n\n## 输入与输出示例\n\n## 希望得到的帮助','## Who is it for and what problem does it solve?\n\n## How should it work?\n\n## Input and output examples\n\n## Feedback wanted') : t('## 这个项目做什么\n\n## 如何使用\n\n## 费用、AI 依赖与限制\n\n## 操作示例','## What does this project do?\n\n## How to use it\n\n## Pricing, AI dependencies and limits\n\n## Examples');
    const prefill=new URLSearchParams({prefill:encodeURIComponent(`---\ntitle: ${JSON.stringify(t(idea?'我的 Vibe Coding 想法':'我的 Vibe Coding 项目',idea?'My Vibe Coding idea':'My Vibe Coding project'))}\n---\n${template}`),tags:'vibe-coding'});
    return `/questions/ask?${prefill}`;
  }
  const farm={display_mode:'play',cover_url:'https://metarhub.com/games/token-farm/token-farm-4x3.jpg',demo_url:'https://metarhub.com/games/token-farm/',kind:'project',use_case:'experiment',relation:'self',pricing:'free',summary:'在 64 格机房培养六系 AI 角色，一键训练、收获，观察满血、自部署和豆包变异。',access_note:'免费，需要论坛登录并绑定元衡账号；游戏不调用付费模型。',data_note:'正式机房进度由服务器保存，绑定账号的昨日消费额度用于发种。'};
  const coverSrc=v=>v===farm.cover_url?v:/^\/(?:uploads|upload|answer\/api\/v1\/file)\/[a-zA-Z0-9_/-]+[.](?:png|jpe?g|webp)$/.test(v||'')?v:'';
  const coverMedia=(e,title)=>coverSrc(e.cover_url)?`<img loading="lazy" referrerpolicy="no-referrer" src="${esc(coverSrc(e.cover_url))}" alt="${esc(title)}">`:'';
  // Decode and normalise on-device. Never pass arbitrary URL input to the upload service.
  async function coverJPEG(file) {
    if(!file || !['image/jpeg','image/png','image/webp'].includes(file.type) || file.size<=0 || file.size>8*1024*1024) throw Error(t('请选择不超过 8 MB 的 JPG、PNG 或 WebP 图片。','Select a JPG, PNG or WebP up to 8 MB.'));
    const img=await root.createImageBitmap(file);
    try {
      const w=img.width,h=img.height;
      if(!w||!h||w*h>32000000)throw Error(t('图片尺寸无效或超过 3200 万像素。','Invalid image or larger than 32 megapixels.'));
      const width=Math.min(w,h*4/3),height=width*3/4,canvas=root.document.createElement('canvas');canvas.width=1200;canvas.height=900;
      const ctx=canvas.getContext('2d');ctx.fillStyle='#fff';ctx.fillRect(0,0,1200,900);ctx.drawImage(img,(w-width)/2,(h-height)/2,width,height,0,0,1200,900);
      const blob=await new Promise(resolve=>canvas.toBlob(resolve,'image/jpeg',0.88));if(!blob)throw Error(t('无法生成封面，请换一张图片。','Could not generate cover.'));return blob;
    } finally {img.close();}
  }
  function uploadedCover(value,origin){if(typeof value!=='string'||!value) return '';try{const u=new URL(value,origin);if(u.origin!==origin||u.username||u.password||u.search||u.hash)return '';return coverSrc(u.pathname);}catch(_){return '';}}
  function card(item, privateMode = false) {
    const e = item.entry;
    if (!privateMode && e.kind==='project' && e.display_mode==='play' && item.has_published && item.status==='published') {
      const image=coverMedia(e,item.title),play=item.demo_status==='available'&&safeURL(e.demo_url);
      const face=image+'<span>'+ (play?t('直接游玩 ↗','Play now ↗'):t('暂不可玩','Unavailable'))+'</span>';
      return '<article class="card market-card market-game"><div class="market-game-face">'+(play?'<a href="'+esc(play)+'" target="_blank" rel="noopener noreferrer" aria-label="'+esc(t('直接游玩：','Play: ')+item.title)+'">'+face+'</a>':face)+'</div><div class="market-game-body"><small>'+label(uses,e.use_case)+' · '+label(prices,e.pricing)+'</small><h2>'+esc(item.title)+'</h2><p>'+esc(e.summary)+'</p>'+link('/marketplace/item/'+item.id,t('介绍与讨论','Details & discussion'),'textlink')+'</div></article>';
    }
    return `<article class="card market-card"><div class="market-card-flags"><span class="badge">${label(uses,e.use_case)}</span>${e.kind === 'idea' ? `<span class="badge">${label(stages,e.idea_stage)}</span>` : ''}${item.featured ? `<span class="badge green">${t('精选','Featured')}</span>` : ''}${privateMode ? `<span class="badge">${label(states,item.status)}</span>` : ''}</div><h2>${privateMode && !item.has_published ? esc(item.title) : link(`/marketplace/item/${item.id}`,esc(item.title),'market-title')}</h2><p class="market-summary">${esc(e.summary)}</p><p class="muted market-stack">${esc(e.tech_stack)}</p><div class="market-card-author">${esc(item.author?.display_name || item.author?.username)}${e.relation === 'recommendation' ? ` · ${t('推荐收录 · 原作者：','Recommended · by ')}${esc(e.original_author)}` : ''}</div>${item.review_note && privateMode ? `<p class="market-review-note">${esc(item.review_note)}</p>` : ''}<div class="market-card-actions">${privateMode ? '' : e.kind === 'project' ? `${e.demo_url ? `<span class="badge${item.demo_status === 'available' ? ' green' : ''}">${item.demo_status !== 'available' ? t('演示暂不可用','Demo unavailable') : item.embed_allowed ? t('可站内体验','Try here') : t('外站演示','External demo')}</span>` : ''}${e.github_url ? '<span class="badge">GitHub</span>' : ''}${e.pricing ? `<span class="badge">${label(prices,e.pricing)}</span>` : ''}` : `<span class="muted">${item.answer_count || 0} ${t('条回答','answers')}</span>`}${privateMode ? link(`/marketplace/publish?id=${item.id}&page=${item._page || 1}`,t('查看 / 维护','View / manage'),'btn small') : link(`/marketplace/item/${item.id}`,t(e.kind === 'idea' ? '参与讨论' : '查看项目',e.kind === 'idea' ? 'Discuss' : 'View project'),'textlink')}</div></article>`;
  }
  function problem(field,zh,en){throw Object.assign(Error(t(zh,en)),{field});}
  function postRef(raw,origin){const text=String(raw||'').trim();if(/^[1-9]\d{0,19}$/.test(text))return text;
    try{const u=new URL(text,origin);const m=u.pathname.match(/^\/questions?\/([A-Za-z0-9]+)(?:\/|$)/);if(u.origin===origin&&!u.username&&!u.password&&m&&m[1]!=='ask')return m[1];}catch(_){}
    problem('question','请选择本人已发布的介绍帖，或粘贴该帖的网址。游戏入口、集市页和帖子标题都不是介绍帖地址。','Select your published introduction or paste its forum post URL, not the game URL or post title.');}
  function checkEntry(e,submit){
    for(const key of Object.keys(e))if(typeof e[key]==='string')e[key]=e[key].trim();
    if(e.display_mode==='play'&&['https://metarhub.com/games/token-farm',farm.demo_url].includes(e.demo_url))e.demo_url=farm.demo_url;
    if(e.demo_url && !(e.display_mode==='play'&&e.demo_url===farm.demo_url)){
      const s=safeURL(e.demo_url);if(!s||/[?#\\]/.test(e.demo_url))problem('demo_url','演示地址须为完整的 HTTPS 游戏首页，不能有登录信息、? 参数或 # 片段。','Use the HTTPS demo homepage without credentials, query or fragment.');
      const u=new URL(s);if(/(^|\.)(metarhub\.com|metar\.uk)$/.test(u.hostname))problem('demo_url','Token 农场请选择「游玩卡」，演示地址填 https://metarhub.com/games/token-farm/ ，不要填封面或介绍帖链接。','For Token Farm select a play card and use the official game homepage.');
      if((u.port&&u.port!=='443')||!u.hostname.includes('.')||/(^|\.)(localhost|local|internal|lan|test|invalid|home\.arpa)$/.test(u.hostname)||/[\[\]:]/.test(u.hostname)||/\.\d+$/.test(u.hostname))problem('demo_url','演示地址必须是公开 HTTPS 域名，不接受 IP、内网或自定义端口。','Provide a public HTTPS hostname.');}
    if(e.github_url&&!safeURL(e.github_url,true))problem('github_url','GitHub 栏请填 https://github.com/作者/仓库；有演示地址可留空。','Enter a GitHub repository URL or leave it blank if you have a demo.');
    if(e.source_idea_id&&!/^[1-9]\d{0,19}$/.test(e.source_idea_id))problem('source_idea_id','来源想法 ID 只填已有集市想法的数字 ID；没有关联请留空。','Use a numeric marketplace idea ID or leave it blank.');
    if(!submit)return;
    if(Array.from(e.summary).length<5)problem('summary','一句话说明至少需要 5 个字。','Add a description of at least five characters.');
    if(e.relation==='recommendation'&&!e.original_author)problem('original_author','推荐收录他人作品时需要填写原作者。','Name the original author for a recommendation.');
    if(e.kind==='project'&&!e.demo_url&&(e.display_mode==='play'||!e.github_url))problem('demo_url','请填写项目入口：游玩卡必须提供 HTTPS 演示地址，介绍卡也可以只填 GitHub。','Add the demo URL; introduction cards may use GitHub instead.');
    if(e.demo_url){if(!e.pricing)problem('pricing','请选择费用，例如「免费」。','Select a pricing option.');if(!e.access_note)problem('access_note','请填写访问与 AI 费用说明，例如是否需要登录、是否额外收费。','Describe login requirements and AI costs.');if(!e.data_note)problem('data_note','请填写输入数据去向，例如保存在哪里、是否发送给第三方。','Describe where user data goes.');}
  }
  async function resolvePost(answer,ref,user,submit){let post;try{post=await answer.getQuestion(ref);}catch(err){if(err.status&&![400,404].includes(err.status))throw err;problem('question','未找到可读取的介绍帖，请先打开帖子确认已经发布。','Could not read that introduction post.');}
    if(!/^[1-9]\d{0,19}$/.test(post?.id||''))problem('question','没有找到该介绍帖的数字 ID，请从「我的帖子」中选择。','Select an introduction from your posts.');
    if(String(post?.user_info?.id)!==String(user.id))problem('question','这篇介绍帖不是当前论坛账号发布的，请选择自己的帖子。','Select a post authored by the signed-in account.');
    if(submit&&Number(post.show)!==1)problem('question','这篇介绍帖尚未公开，请先确认帖子已发布并公开可见。','The introduction must be public.');return String(post.id);}
  const errorText = error => {
    if(error?.field)return error.message;
    if(error?.code === 'storage_unavailable') return t('请允许浏览器站点存储后重试，以便在网络中断时恢复投稿。','Allow site storage before submitting so your request can be recovered after a network failure.');
    if (error?.status === 409 || error?.code === 'conflict') return t('版本已变化或该帖子已收录，请刷新我的发布后再操作。','The version changed or this post is already listed. Refresh your submissions.');
    if (error?.status === 400) return t('请检查必填项、帖子归属以及链接格式。演示需使用公开 HTTPS 地址，不能包含登录凭证、查询参数或片段。','Check required fields, post ownership and links. Use public HTTPS URLs without credentials, queries or fragments.');
    if (error?.status === 403 || error?.status === 401) return t('当前账号无权操作，请确认登录、邮箱激活及帖子归属。','Check your login, activation, post ownership and permissions.');
    if (error?.status === 429) return t('操作过于频繁，请稍后重试。','Too many requests. Try again shortly.');
    return t('暂时无法确认操作结果。请重试原操作，系统会恢复同一结果。','Unable to confirm the result. Retry the original operation to recover it.');
  };
  class View {
    constructor(answer, admin) { this.answer=answer; this.admin=admin; this.generation=0; }
    dispose() { this.generation++; this.root=null; }
    async page(path, user, params) {
      this.dispose(); this.path=path; this.user=user; this.params=params; this.pending=null;
      this.session=this.answer.token?.();
      this.storageKey=user ? `metar-market-pending-v1:${user.id}` : null;
      if (this.storageKey) {
        let raw=null;
        try {raw=root.sessionStorage.getItem(this.storageKey);} catch(_) {this.storageUnavailable=true;}
        if(raw) {
          const pending=JSON.parse(raw);
          if (!pending || typeof pending.key !== 'string' || !pending.command || typeof pending.admin !== 'boolean') throw Error('invalid saved marketplace request');
          this.pending=pending;
        }
      }
      if(path === '/marketplace/publish') return this.editor();
      if(path === '/admin/marketplace') return this.queue();
      if(path === '/marketplace/mine') return this.mine();
      if(path.startsWith('/marketplace/item/')) return this.detail(path.slice('/marketplace/item/'.length));
      return this.list();
    }
    header(title, description = '') {
      return `<div class="page-title"><div><h1>${title}</h1><p class="muted">${description}</p></div><div class="market-header-actions">${link('/marketplace',t('逛集市','Explore'))}${link('/marketplace/mine',t('我的发布','My submissions'))}${link('/marketplace/publish',t('发布项目 / 想法','Share a project / idea'),'btn primary')}</div></div>`;
    }
    recovery() { return this.pending ? `<section class="card card-pad market-recovery"><p role="status">${t('有一个操作尚未确认。重试会使用原请求，避免重复发布。','A previous operation is unconfirmed. Retry it to avoid duplicates.')}</p><button class="btn primary" data-market-retry>${t('重试原操作','Retry original operation')}</button></section>` : ''; }
    pager(data) {
      const page=data.page, total=Math.ceil(data.count/data.page_size);
      const url=n => {const p=new URLSearchParams(this.params);p.set('page',n);return `${this.path}?${p}`;};
      return total>1 ? `<nav class="market-pagination" aria-label="${t('分页','Pagination')}">${page>1 ? link(url(page-1),t('上一页','Previous')) : ''}<span>${page} / ${total}</span>${page<total ? link(url(page+1),t('下一页','Next')) : ''}</nav>` : '';
    }
    async list() {
      const kind=this.params.get('kind') === 'idea' ? 'idea' : 'project';
      const values={kind,q:this.params.get('q') || '',use_case:this.params.get('use_case') || '',capability:kind==='project' ? this.params.get('capability') || '' : '',order:this.params.get('order') || 'featured',page:this.params.get('page') || 1};
      const data=await api.list(this.answer,values);
      return this.header(t('Vibe Coding 集市','Vibe Coding Marketplace'),t('分享用 AI 做出的作品，让好想法遇到一起动手的人。','Discover AI-built projects and turn ideas into shared experiments.')) + `<nav class="discussion-tabs"><a data-router class="discussion-tab ${kind==='project'?'active':''}" href="/marketplace?kind=project">${t('项目','Projects')}</a><a data-router class="discussion-tab ${kind==='idea'?'active':''}" href="/marketplace?kind=idea">${t('想法讨论','Ideas & discussions')}</a></nav><form class="market-filters" data-market-filter><input type="hidden" name="kind" value="${kind}"><label><span>${t('搜索','Search')}</span><input type="search" name="q" value="${esc(values.q)}" maxlength="80" placeholder="${t('项目、用途、技术栈…','Project, use case, stack…')}"></label><label><span>${t('用途','Use case')}</span><select name="use_case">${options(uses,values.use_case,t('全部用途','All uses'))}</select></label>${kind==='project'?`<label><span>${t('体验方式','Access')}</span><select name="capability">${options({demo:['在线演示','Online demo'],github:['GitHub 源码','GitHub repository']},values.capability,t('全部方式','All access'))}</select></label>`:''}<label><span>${t('排序','Sort')}</span><select name="order">${options({featured:['精选优先','Featured first'],newest:['最新发布','Newest']},values.order)}</select></label><button class="btn primary">${t('筛选','Filter')}</button></form><p class="muted market-count">${data.count} ${t('个结果','results')}</p><div class="market-grid">${data.items.map(i=>card(i)).join('')}</div>${!data.items.length ? `<section class="card card-pad market-empty"><h2>${t('期待你的第一个作品或想法','Share the first project or idea')}</h2><p>${t('这里暂时没有符合条件的内容。发布你的作品，或换个筛选条件。','No matching entries yet. Share your work or adjust the filters.')}</p>${link('/marketplace/publish',t('发布到集市','Share with the marketplace'),'btn primary')}</section>`:''}${this.pager(data)}`;
    }
    async detail(id) {
      const data=await api.detail(this.answer,id); const item=data.item,e=item.entry;
      const canEmbed=e.kind==='project' && e.display_mode!=='play' && item.embed_allowed && item.demo_status==='available' && safeURL(e.demo_url);
      return this.header(esc(item.title),esc(e.summary))+`<section class="card card-pad market-detail"><div class="market-card-flags"><span class="badge">${label(uses,e.use_case)}</span><span>${esc(item.author.display_name || item.author.username)}</span>${e.relation==='recommendation'?`<span>${t('推荐收录 · 原作者：','Recommended · by ')}${esc(e.original_author)}</span>`:''}${e.idea_stage?`<span class="badge">${label(stages,e.idea_stage)}</span>`:''}</div><div class="market-main-actions">${canEmbed?`<button class="btn primary" data-market-run>${t('站内体验','Try here')}</button>`:''}${e.demo_url && item.demo_status==='available'?external(e.demo_url,t('打开外站演示','Open demo')):''}${external(e.github_url,t('前往 GitHub','View on GitHub'),true)}<a class="btn" href="${postURL(e.question_id)}">${t('查看介绍 · 讨论 · 收藏','Read · Discuss · Bookmark')}</a>${e.kind==='idea'?link(`/marketplace/publish?kind=project&source=${item.id}`,t('实现这个想法','Build this idea')):''}</div>${e.demo_url?`<div class="market-disclosure"><h2>${t('体验前了解','Before you try')}</h2><dl><dt>${t('费用','Pricing')}</dt><dd>${label(prices,e.pricing)}</dd><dt>${t('访问与 AI 费用说明','Access & AI costs')}</dt><dd>${esc(e.access_note)}</dd><dt>${t('输入数据去向','Where your data goes')}</dt><dd>${esc(e.data_note)}</dd></dl><p>${t('应用由作者或第三方提供。请先阅读以上说明，再决定是否输入数据或使用自己的 API Key。','The app is provided by its author or a third party. Read these notes before entering data or using your API key.')}</p>${item.demo_status!=='available'?`<p role="status">${t('演示暂不可用，仍可查看源码与参与讨论。','The demo is unavailable. You can still read the repository and discuss the project.')}</p>`:''}</div>`:''}<dl><dt>${t('技术栈','Tech stack')}</dt><dd>${esc(e.tech_stack || '—')}</dd><dt>${t('许可证（投稿者声明）','License (as declared)')}</dt><dd>${esc(e.license || t('未声明；不代表可自由使用','Unspecified; check usage rights'))}</dd></dl><section data-market-player hidden class="market-player"><div class="between wrap"><h2>${t('在线体验','Live preview')}</h2><button class="btn" data-market-stop>${t('关闭体验','Close preview')}</button></div><p role="status" data-market-frame-status>${t('正在载入。若无法显示，可使用外站演示。','Loading. If the preview does not work, open the external demo.')}</p><div data-market-frame></div>${external(e.demo_url,t('无法显示？打开外站演示','Preview unavailable? Open demo'))}</section></section>${data.source ? `<section class="card card-pad mt24"><h2>${t('来自这个想法','Inspired by')}</h2>${link(`/marketplace/item/${data.source.id}`,esc(data.source.title),'textlink')}</section>`:''}${data.projects.length?`<section class="mt24"><h2>${t('这个想法诞生的作品','Projects built from this idea')}</h2><div class="market-grid">${data.projects.map(i=>card(i)).join('')}</div></section>`:''}`;
    }
    async mine() {
      const data=await api.mine(this.answer,{page:this.params.get('page') || 1});this.items=data.items;
      return this.header(t('我的集市发布','My submissions'),t('跟进审核结果、更新项目信息和管理上线状态。','Track reviews and maintain your projects.'))+this.recovery()+`<div class="market-grid">${data.items.map(i=>card({...i,_page:data.page},true)).join('')}</div>${!data.items.length?`<section class="card card-pad">${t('你还没有投稿，先分享一个项目或想法吧。','You have no submissions yet. Share a project or an idea.')}</section>`:''}${this.pager(data)}`;
    }
    async editor() {
      const id=this.params.get('id');let item=null;
      if(id){const data=await api.mine(this.answer,{page:this.params.get('page') || 1});item=data.items.find(i=>i.id===id);if(!item)throw Error(t('未找到本页发布，请从我的发布进入。','Entry not found. Open it from My submissions.'));}
      this.item=item;
      const e=item?.entry || {kind:this.params.get('kind')==='idea'?'idea':'project',use_case:'productivity',relation:'self',idea_stage:'feedback',source_idea_id:this.params.get('source') || ''};
      if(!item && this.params.get('preset')==='token-farm')Object.assign(e,farm);else if(!item && this.params.get('display')==='play')e.display_mode='play';
      let posts=[];
      try {posts=(await this.answer.listPersonalQuestions(this.user.username,{pageSize:50})).list || [];} catch(_) { /* A pasted native post URL is an independent recovery path. */ }
      const frozen=item?.status==='pending_review';
      const field=(name,zh,en,max=255,required=false)=>`<label><span>${t(zh,en)}</span><input name="${name}" value="${esc(e[name])}" maxlength="${max}"${required?' required':''}></label>`;
      return this.header(t('发布到 Vibe Coding 集市','Share with the marketplace'),t('先发布社区介绍，再补充收录信息。讨论和收藏都留在同一个帖子。','Publish a community introduction, then add listing details. Discussions and bookmarks stay with the original post.'))+this.recovery()+`<section class="card card-pad market-publish-intro"><h2>${t('1. 准备介绍与讨论帖','1. Prepare your introduction')}</h2><p>${t('使用下面的模板在社区发布介绍，完成后回到这里，选择你的帖子或粘贴帖子地址。','Use the template below to publish an introduction. Then return here and select your post or paste its URL.')}</p><a class="btn" data-market-template href="${esc(introductionURL(e.kind))}" target="_blank" rel="noopener">${t('用模板发布介绍帖','Create introduction from template')} ↗</a></section><form class="card card-pad market-form mt24" data-market-editor><h2>${t('2. 填写收录信息','2. Add listing details')}</h2>${item?`<p class="market-review-note">${label(states,item.status)}${item.review_note?` · ${esc(item.review_note)}`:''}</p>`:''}${item?.has_published?`<p>${t('更新链接或归属需重新审核；审核期间保留已上线版本。','Link and ownership changes require review. The approved version stays live during review.')}</p>`:''}<fieldset${frozen?' disabled':''}><div class="market-form-grid"><label><span>${t('类型','Type')}</span><select name="kind"${item?' disabled':''}>${options({project:['项目','Project'],idea:['想法讨论','Idea']},e.kind)}</select>${item?`<input type="hidden" name="kind" value="${e.kind}">`:''}</label><label><span>${t('用途','Use case')}</span><select name="use_case">${options(uses,e.use_case)}</select></label><label class="market-wide"><span>${t('介绍帖地址或数字 ID（仅本人帖子）','Your introduction post URL or numeric ID')}</span><input name="question" value="${esc(e.question_id?postURL(e.question_id):this.params.get('question')||'')}" required list="market-own-posts"${item?' readonly':''}><datalist id="market-own-posts">${posts.map(p=>`<option value="/questions/${esc(p.id)}">${esc(p.title)}</option>`).join('')}</datalist></label><label class="market-wide"><span>${t('一句话说明（5–280 字）','Short description (5–280 characters)')}</span><textarea name="summary" maxlength="280" minlength="5" required rows="3">${esc(e.summary)}</textarea></label><label><span>${t('作者关系','Attribution')}</span><select name="relation">${options({self:['本人 / 团队作品','My / our work'],recommendation:['推荐收录他人作品','Recommend someone else’s work']},e.relation)}</select></label>${field('original_author','原作者（推荐收录必填）','Original author (required for recommendations)',120)}<div class="market-wide market-form-grid" data-market-project${e.kind==='idea'?' hidden':''}><label><span>${t('卡片样式','Card style')}</span><select name="display_mode">${options({intro:['介绍卡：查看项目 / 仓库','Introduction / repository'],play:['游玩卡：点击封面直接玩','Cover / play directly']},e.display_mode||'intro')}</select></label><div class="market-wide" data-cover-fields${e.display_mode==='play'?'':' hidden'}><label><span>${t('上传游玩封面','Upload play cover')}</span><input type="file" accept="image/jpeg,image/png,image/webp" data-cover-file aria-describedby="market-cover-help"></label><input type="hidden" name="cover_url" value="${esc(e.cover_url || '')}"><small id="market-cover-help">${t('选择 JPG、PNG 或 WebP，最多 8 MB。自动居中裁成 4:3，建议 1200 × 900。','Select a JPG, PNG or WebP up to 8 MB. Cropped to 4:3; 1200 × 900 recommended.')}</small><div class="market-game-face market-game-preview" data-cover-preview${coverSrc(e.cover_url)?'':' hidden'}>${coverMedia(e,t('封面预览','Cover preview'))}</div><p role="status" data-cover-status></p></div>${field('github_url','GitHub 仓库地址','GitHub repository URL',512)}${field('demo_url','HTTPS 演示地址','HTTPS demo URL',512)}${field('tech_stack','技术栈 / 开发工具','Tech stack / tools')}${field('license','许可证（不清楚可留空）','License (leave blank if unknown)',64)}<label><span>${t('费用','Pricing')}</span><select name="pricing">${options(prices,e.pricing,t('请选择（有演示必填）','Select (required for demos)'))}</select></label>${field('source_idea_id','来源想法的集市 ID（可选）','Source idea marketplace ID (optional)',20)}<label class="market-wide"><span>${t('访问与 AI 费用：登录要求、是否用 AI、由谁付费（有演示必填）','Access & AI costs: login, AI use and who pays (required for demos)')}</span><textarea name="access_note" maxlength="300" rows="2">${esc(e.access_note)}</textarea></label><label class="market-wide"><span>${t('输入数据去向：发送给谁、是否保存（有演示必填）','Data: recipients and retention (required for demos)')}</span><textarea name="data_note" maxlength="500" rows="2">${esc(e.data_note)}</textarea></label><label class="market-wide market-check"><input type="checkbox" name="embed_requested"${e.embed_requested?' checked':''}><span>${t('我已获得嵌入许可，申请站内体验（需审核）','I have permission to embed this demo and request an on-site preview (subject to review)')}</span></label></div><label data-market-idea${e.kind==='idea'?'':' hidden'}><span>${t('想法进度','Idea stage')}</span><select name="idea_stage">${options(stages,e.idea_stage || 'feedback')}</select></label></div><div class="market-main-actions">${!item?.has_published?`<button class="btn" type="submit" name="op" value="save_draft" formnovalidate>${t('保存草稿','Save draft')}</button>`:''}<button class="btn primary" type="submit" name="op" value="submit">${t(item?.has_published?'提交更新审核':'提交审核',item?.has_published?'Submit update for review':'Submit for review')}</button></div></fieldset>${item?`<button class="btn" type="button" data-market-archive>${t('下架 / 撤回投稿','Archive / withdraw')}</button>`:''}<p role="status" data-market-status></p></form>`;
    }
    async queue() {
      const status=this.params.get('status') || 'pending_review';
      const data=await api.adminList(this.admin,{status:status==='all'?'':status,page:this.params.get('page') || 1});this.items=data.items;
      return this.header(t('集市审核','Marketplace review'),t('核对项目来源、用途、费用、数据去向和作者的嵌入许可。','Check attribution, purpose, pricing, data notes and permission to embed.'))+this.recovery()+`<section class="card card-pad mt24"><h2>${t('后台添加作品','Add a listing')}</h2><p>${t('添加后仍需提交审核。旧版作品默认使用介绍卡。','New entries require review; existing entries keep their introduction cards.')}</p><div class="market-main-actions">${link('/marketplace/publish',t('添加介绍卡','Add introduction card'))}${link('/marketplace/publish?display=play',t('添加游玩卡','Add playable card'))}${link('/marketplace/publish?preset=token-farm',t('添加 Token 农场（预填）','Add Token Farm preset'),'btn primary')}</div></section><form class="market-filters" data-market-filter><label><span>${t('状态','Status')}</span><select name="status">${options({pending_review:states.pending_review,published:states.published,changes_requested:states.changes_requested,archived:states.archived,all:['全部','All']},status)}</select></label><button class="btn">${t('筛选','Filter')}</button></form>${data.items.map(i=>{const e=i.entry;return `<form class="card card-pad market-review mt24" data-market-review="${i.id}"><h2>${esc(i.title)} <span class="badge">${label(states,i.status)}</span></h2><p><strong>${e.display_mode==='play'?t('直接游玩 · 4:3 封面','Playable · 4:3 cover'):t('介绍卡','Introduction')}</strong></p>${e.display_mode==='play'?'<div class="market-game-face market-game-preview">'+coverMedia(e,i.title)+'</div>':''}<p>${esc(e.summary)}</p><p>${esc(i.author.display_name || i.author.username)} · ${e.relation==='recommendation'?esc(e.original_author):t('本人 / 团队作品','My / our work')} · ${label(uses,e.use_case)}</p><div class="market-main-actions"><a class="btn" href="${postURL(e.question_id)}" target="_blank" rel="noopener">${t('查看原帖','Read original post')} ↗</a>${external(e.github_url,'GitHub',true)}${external(e.demo_url,t('检查演示','Check demo'))}</div><dl><dt>${t('费用与访问','Pricing & access')}</dt><dd>${label(prices,e.pricing)} · ${esc(e.access_note)}</dd><dt>${t('数据去向','Data handling')}</dt><dd>${esc(e.data_note)}</dd><dt>${t('技术栈 / 许可证','Stack / license')}</dt><dd>${esc(e.tech_stack)} / ${esc(e.license)}</dd><dt>${t('来源想法 ID','Source idea ID')}</dt><dd>${esc(e.source_idea_id || '—')}</dd></dl><div class="market-form-grid"><label><span>${t('决定','Decision')}</span><select name="decision">${options(i.status==='pending_review'?{publish:['通过并发布','Approve & publish'],changes:['退回修改','Request changes'],archive:['下架','Archive']}:{availability:['更新体验 / 精选状态','Update demo / featured status'],archive:['下架','Archive']},'')}</select></label><label><span>${t('演示状态','Demo status')}</span><select name="demo_status">${options({available:['可用','Available'],unavailable:['不可用','Unavailable']},i.demo_status)}</select></label><label class="market-check"><input type="checkbox" name="embed_allowed"${i.embed_allowed?' checked':''}${!e.embed_requested?' disabled':''}><span>${t('允许站内嵌入（需确认作者许可）','Allow embed (verify author permission)')}</span></label><label class="market-check"><input type="checkbox" name="featured"${i.featured?' checked':''}><span>${t('精选','Featured')}</span></label><label class="market-wide"><span>${t('审核说明（作者可见，至少 2 字）','Review note (visible to author, at least 2 characters)')}</span><textarea name="reason" required minlength="2" maxlength="500">${esc(i.review_note)}</textarea></label></div><button class="btn primary" type="submit">${t('确认审核决定','Save review decision')}</button><p role="status" data-market-status></p></form>`;}).join('')}${!data.items.length?`<section class="card card-pad">${t('当前没有待处理内容。','No entries to review.')}</section>`:''}${this.pager(data)}`;
    }
    async command(command, admin = false) {
      if(this.pending)throw Error('pending');
      if(!this.storageKey || this.storageUnavailable)throw Object.assign(Error('storage unavailable'),{code:'storage_unavailable'});
      this.pending={key:uid(),command,admin};
      // Persist before sending. A failed response replays the same command/key,
      // including after reload or a confirmed mutation followed by a failed read.
      try {root.sessionStorage.setItem(this.storageKey,JSON.stringify(this.pending));}
      catch(error){this.pending=null;throw error;}
      return this.retry();
    }
    async retry() {
      const p=this.pending;if(!p)throw Error('missing operation');
      if(this.answer.token && this.answer.token() !== this.session)throw Object.assign(Error('session changed'),{status:401});
      try {
        const result=await send(p.admin?this.admin:this.answer,p.admin,p.command,p.key);
        root.sessionStorage.removeItem(this.storageKey);this.pending=null;
        return result;
      } catch(error) {
        if([400,401,403,404,409,429].includes(error.status)) {root.sessionStorage.removeItem(this.storageKey);this.pending=null;}
        throw error;
      }
    }
    mount(container) {
      this.root=container;const generation=this.generation;
      const current=()=>this.root===container && container.isConnected && generation===this.generation;
      const reload=()=>this.path === '/marketplace/publish' ? root.location.assign('/marketplace/mine') : root.location.reload();
      const run=async(form,action)=>{
        if(this.busy)return;this.busy=true;
        const buttons=[...container.querySelectorAll('button')];buttons.forEach(b=>b.disabled=true);
        const status=form.querySelector('[data-market-status]') || container.querySelector('[data-market-status]');
        try {await action();if(current())reload();}
        catch(error){if(current()){if(status)status.textContent=errorText(error);else root.alert(errorText(error));if(error.field){const control=form.elements?.namedItem(error.field);if(control){control.focus();control.setCustomValidity(error.message);control.reportValidity();control.setCustomValidity('');}} }}
        finally {this.busy=false;if(current()){buttons.forEach(b=>b.disabled=false);if(this.pending){let retry=container.querySelector('[data-market-retry]');if(!retry){retry=root.document.createElement('button');retry.type='button';retry.className='btn';retry.dataset.marketRetry='';retry.textContent=t('重试原操作','Retry original operation');(status?.parentElement || container).append(retry);retry.onclick=()=>run(container,()=>this.retry());}container.querySelectorAll('[data-market-editor] button[type="submit"],[data-market-review] button[type="submit"],[data-market-archive]').forEach(b=>b.disabled=true);}}}
      };
      container.querySelector('[data-market-retry]')?.addEventListener('click',()=>run(container,()=>this.retry()));
      container.querySelector('[data-market-filter]')?.addEventListener('submit',event=>{event.preventDefault();const p=new URLSearchParams(new root.FormData(event.currentTarget));root.location.assign(`${this.path}?${p}`);});
      const editor=container.querySelector('[data-market-editor]');
      if(editor) {
        const kind=editor.querySelector('select[name="kind"]');
        const mode=editor.querySelector('[name="display_mode"]'),coverBox=editor.querySelector('[data-cover-fields]'),embed=editor.querySelector('[name="embed_requested"]');
        const toggle=()=>{const idea=kind.value==='idea',play=!idea&&mode.value==='play';coverBox.hidden=!play;embed.disabled=play; if(play)embed.checked=false;editor.querySelector('[data-market-project]').hidden=idea;editor.querySelector('[data-market-idea]').hidden=!idea;container.querySelector('[data-market-template]').href=introductionURL(kind.value);};
        kind.onchange=toggle;mode.onchange=toggle;toggle();
        const picker=editor.querySelector('[data-cover-file]'),saved=editor.querySelector('[name="cover_url"]'),preview=editor.querySelector('[data-cover-preview]'),feedback=editor.querySelector('[data-cover-status]');
        let uploading=false;
        picker.addEventListener('change',async()=>{
          const file=picker.files?.[0];if(!file||uploading||this.busy||this.pending)return;
          const submitters=[...editor.querySelectorAll('button[type="submit"]')];uploading=true;picker.disabled=true;submitters.forEach(b=>b.disabled=true);
          feedback.textContent=t('正在处理并上传封面…','Preparing and uploading cover…');
          try {
            const blob=await coverJPEG(file);
            if(!current()||!this.session||this.answer.token()!==this.session)throw Error(t('登录状态已改变，请刷新后重试。','Session changed. Reload and retry.'));
            const body=new root.FormData();body.append('source','post');body.append('file',blob,'market-cover.jpg');
            const result=await this.answer.request('/file',{method:'POST',body});const url=uploadedCover(result,root.location.origin);
            if(!url)throw Error(t('未获得可用的论坛封面，请重试。','Upload did not return a usable forum cover.'));
            if(!current()||this.answer.token()!==this.session)return;
            saved.value=url;preview.innerHTML=coverMedia({cover_url:url},t('封面预览','Cover preview'));preview.hidden=false;feedback.textContent=t('封面已上传，提交审核后生效。','Cover uploaded. Submit for review.');
          }catch(error){if(current())feedback.textContent=t('上传失败；原封面不变。','Upload failed; previous cover kept. ')+(error.message||'');}
          finally{uploading=false;if(current()){picker.value='';picker.disabled=false;submitters.forEach(b=>b.disabled=!!this.pending);}}
        });
        editor.addEventListener('submit',event=>{
          event.preventDefault();if(this.pending||uploading)return;
          const f=new root.FormData(editor),idea=f.get('kind')==='idea';
          if(!idea && f.get('display_mode')==='play' && (event.submitter?.value||'submit')==='submit' && !coverSrc(saved.value)){feedback.textContent=t('请先上传游玩封面。','Please upload a cover first.');picker.focus();return;}
          const question=String(f.get('question')||'');
          const entry={question_id:question,display_mode:!idea&&f.get('display_mode')==='play'?'play':'',cover_url:!idea&&f.get('display_mode')==='play'?String(f.get('cover_url')||'').trim():'',kind:f.get('kind'),summary:f.get('summary'),use_case:f.get('use_case'),relation:f.get('relation'),original_author:f.get('original_author'),github_url:idea?'':f.get('github_url'),demo_url:idea?'':f.get('demo_url'),tech_stack:idea?'':f.get('tech_stack'),license:idea?'':f.get('license'),pricing:idea?'':f.get('pricing'),access_note:idea?'':f.get('access_note'),data_note:idea?'':f.get('data_note'),embed_requested:!idea && f.has('embed_requested'),source_idea_id:idea?'':f.get('source_idea_id'),idea_stage:idea?f.get('idea_stage'):''};
          const op=event.submitter?.value||'submit';run(editor,async()=>{const ref=postRef(question,root.location.origin);checkEntry(entry,op==='submit');entry.question_id=await resolvePost(this.answer,ref,this.user,op==='submit');if(!current())return;return this.command({op,id:this.item?.id||'',version:this.item?.version||0,entry});});
        });
        editor.querySelector('[data-market-archive]')?.addEventListener('click',()=>{if(root.confirm(t('确认从集市下架？原社区帖子仍保留。','Archive this listing? The original post will remain.')))run(editor,()=>this.command({op:'archive',id:this.item.id,version:this.item.version}));});
      }
      container.querySelectorAll('[data-market-review]').forEach(form=>form.addEventListener('submit',event=>{
        event.preventDefault();const item=this.items.find(i=>i.id===form.dataset.marketReview),f=new root.FormData(form);
        run(form,()=>this.command({op:'review',id:item.id,version:item.version,decision:f.get('decision'),reason:f.get('reason'),embed_allowed:f.has('embed_allowed'),demo_status:f.get('demo_status'),featured:f.has('featured')},true));
      }));
      container.querySelector('[data-market-run]')?.addEventListener('click',async event=>{
        // Fetch the reviewed live entry again at the moment of use. No session
        // token, balance authorization or application key enters the frame URL.
        const button=event.currentTarget;button.disabled=true;
        try {
          const data=await api.detail(this.answer,this.path.split('/').pop()),item=data.item;
          if(!current())return;
          if(!item.embed_allowed || item.demo_status!=='available' || !safeURL(item.entry.demo_url))throw Error('preview unavailable');
          const player=container.querySelector('[data-market-player]'),slot=container.querySelector('[data-market-frame]');
          const frame=root.document.createElement('iframe');frame.title=item.title;frame.setAttribute('sandbox','allow-scripts allow-forms');frame.referrerPolicy='no-referrer';frame.setAttribute('allow',"camera 'none'; microphone 'none'; geolocation 'none'; payment 'none'; clipboard-write 'none'");frame.src=safeURL(item.entry.demo_url);slot.replaceChildren(frame);player.hidden=false;player.scrollIntoView({behavior:'smooth',block:'start'});
          // Cross-origin frame events cannot prove successful execution. Keep
          // the external fallback and neutral loading message visible.
        }catch(_){if(current())root.alert(t('体验暂不可用，请刷新页面或使用外站演示。','Preview unavailable. Refresh or open the external demo.'));}
        finally{if(current())button.disabled=false;}
      });
      container.querySelector('[data-market-stop]')?.addEventListener('click',()=>{container.querySelector('[data-market-frame]').replaceChildren();container.querySelector('[data-market-player]').hidden=true;});
    }
  }
  const exports={...api,View,safeURL,card,uploadedCover,postRef,checkEntry,resolvePost};
  if(typeof module==='object' && module.exports)module.exports=exports;
  else root.MetarMarketplace=Object.freeze(exports);
})(typeof window==='undefined'?globalThis:window);
