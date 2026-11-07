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
  function card(item, privateMode = false) {
    const e = item.entry;
    return `<article class="card market-card"><div class="market-card-flags"><span class="badge">${label(uses,e.use_case)}</span>${e.kind === 'idea' ? `<span class="badge">${label(stages,e.idea_stage)}</span>` : ''}${item.featured ? `<span class="badge green">${t('精选','Featured')}</span>` : ''}${privateMode ? `<span class="badge">${label(states,item.status)}</span>` : ''}</div><h2>${privateMode && !item.has_published ? esc(item.title) : link(`/marketplace/item/${item.id}`,esc(item.title),'market-title')}</h2><p class="market-summary">${esc(e.summary)}</p><p class="muted market-stack">${esc(e.tech_stack)}</p><div class="market-card-author">${esc(item.author?.display_name || item.author?.username)}${e.relation === 'recommendation' ? ` · ${t('推荐收录 · 原作者：','Recommended · by ')}${esc(e.original_author)}` : ''}</div>${item.review_note && privateMode ? `<p class="market-review-note">${esc(item.review_note)}</p>` : ''}<div class="market-card-actions">${privateMode ? '' : e.kind === 'project' ? `${e.demo_url ? `<span class="badge${item.demo_status === 'available' ? ' green' : ''}">${item.demo_status !== 'available' ? t('演示暂不可用','Demo unavailable') : item.embed_allowed ? t('可站内体验','Try here') : t('外站演示','External demo')}</span>` : ''}${e.github_url ? '<span class="badge">GitHub</span>' : ''}${e.pricing ? `<span class="badge">${label(prices,e.pricing)}</span>` : ''}` : `<span class="muted">${item.answer_count || 0} ${t('条回答','answers')}</span>`}${privateMode ? link(`/marketplace/publish?id=${item.id}&page=${item._page || 1}`,t('查看 / 维护','View / manage'),'btn small') : link(`/marketplace/item/${item.id}`,t(e.kind === 'idea' ? '参与讨论' : '查看项目',e.kind === 'idea' ? 'Discuss' : 'View project'),'textlink')}</div></article>`;
  }
  const errorText = error => {
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
      const canEmbed=e.kind==='project' && item.embed_allowed && item.demo_status==='available' && safeURL(e.demo_url);
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
      let posts=[];
      try {posts=(await this.answer.listPersonalQuestions(this.user.username,{pageSize:50})).list || [];} catch(_) { /* A pasted native post URL is an independent recovery path. */ }
      const frozen=item?.status==='pending_review';
      const field=(name,zh,en,max=255,required=false)=>`<label><span>${t(zh,en)}</span><input name="${name}" value="${esc(e[name])}" maxlength="${max}"${required?' required':''}></label>`;
      return this.header(t('发布到 Vibe Coding 集市','Share with the marketplace'),t('先发布社区介绍，再补充收录信息。讨论和收藏都留在同一个帖子。','Publish a community introduction, then add listing details. Discussions and bookmarks stay with the original post.'))+this.recovery()+`<section class="card card-pad market-publish-intro"><h2>${t('1. 准备介绍与讨论帖','1. Prepare your introduction')}</h2><p>${t('使用下面的模板在社区发布介绍，完成后回到这里，选择你的帖子或粘贴帖子地址。','Use the template below to publish an introduction. Then return here and select your post or paste its URL.')}</p><a class="btn" data-market-template href="${esc(introductionURL(e.kind))}" target="_blank" rel="noopener">${t('用模板发布介绍帖','Create introduction from template')} ↗</a></section><form class="card card-pad market-form mt24" data-market-editor><h2>${t('2. 填写收录信息','2. Add listing details')}</h2>${item?`<p class="market-review-note">${label(states,item.status)}${item.review_note?` · ${esc(item.review_note)}`:''}</p>`:''}${item?.has_published?`<p>${t('更新链接或归属需重新审核；审核期间保留已上线版本。','Link and ownership changes require review. The approved version stays live during review.')}</p>`:''}<fieldset${frozen?' disabled':''}><div class="market-form-grid"><label><span>${t('类型','Type')}</span><select name="kind"${item?' disabled':''}>${options({project:['项目','Project'],idea:['想法讨论','Idea']},e.kind)}</select>${item?`<input type="hidden" name="kind" value="${e.kind}">`:''}</label><label><span>${t('用途','Use case')}</span><select name="use_case">${options(uses,e.use_case)}</select></label><label class="market-wide"><span>${t('介绍帖地址或数字 ID（仅本人帖子）','Your introduction post URL or numeric ID')}</span><input name="question" value="${esc(e.question_id?postURL(e.question_id):this.params.get('question')||'')}" required list="market-own-posts"${item?' readonly':''}><datalist id="market-own-posts">${posts.map(p=>`<option value="/questions/${esc(p.id)}">${esc(p.title)}</option>`).join('')}</datalist></label><label class="market-wide"><span>${t('一句话说明（5–280 字）','Short description (5–280 characters)')}</span><textarea name="summary" maxlength="280" minlength="5" required rows="3">${esc(e.summary)}</textarea></label><label><span>${t('作者关系','Attribution')}</span><select name="relation">${options({self:['本人 / 团队作品','My / our work'],recommendation:['推荐收录他人作品','Recommend someone else’s work']},e.relation)}</select></label>${field('original_author','原作者（推荐收录必填）','Original author (required for recommendations)',120)}<div class="market-wide market-form-grid" data-market-project${e.kind==='idea'?' hidden':''}>${field('github_url','GitHub 仓库地址','GitHub repository URL',512)}${field('demo_url','HTTPS 演示地址','HTTPS demo URL',512)}${field('tech_stack','技术栈 / 开发工具','Tech stack / tools')}${field('license','许可证（不清楚可留空）','License (leave blank if unknown)',64)}<label><span>${t('费用','Pricing')}</span><select name="pricing">${options(prices,e.pricing,t('请选择（有演示必填）','Select (required for demos)'))}</select></label>${field('source_idea_id','来源想法的集市 ID（可选）','Source idea marketplace ID (optional)',20)}<label class="market-wide"><span>${t('访问与 AI 费用：登录要求、是否用 AI、由谁付费（有演示必填）','Access & AI costs: login, AI use and who pays (required for demos)')}</span><textarea name="access_note" maxlength="300" rows="2">${esc(e.access_note)}</textarea></label><label class="market-wide"><span>${t('输入数据去向：发送给谁、是否保存（有演示必填）','Data: recipients and retention (required for demos)')}</span><textarea name="data_note" maxlength="500" rows="2">${esc(e.data_note)}</textarea></label><label class="market-wide market-check"><input type="checkbox" name="embed_requested"${e.embed_requested?' checked':''}><span>${t('我已获得嵌入许可，申请站内体验（需审核）','I have permission to embed this demo and request an on-site preview (subject to review)')}</span></label></div><label data-market-idea${e.kind==='idea'?'':' hidden'}><span>${t('想法进度','Idea stage')}</span><select name="idea_stage">${options(stages,e.idea_stage || 'feedback')}</select></label></div><div class="market-main-actions">${!item?.has_published?`<button class="btn" type="submit" name="op" value="save_draft" formnovalidate>${t('保存草稿','Save draft')}</button>`:''}<button class="btn primary" type="submit" name="op" value="submit">${t(item?.has_published?'提交更新审核':'提交审核',item?.has_published?'Submit update for review':'Submit for review')}</button></div></fieldset>${item?`<button class="btn" type="button" data-market-archive>${t('下架 / 撤回投稿','Archive / withdraw')}</button>`:''}<p role="status" data-market-status></p></form>`;
    }
    async queue() {
      const status=this.params.get('status') || 'pending_review';
      const data=await api.adminList(this.admin,{status:status==='all'?'':status,page:this.params.get('page') || 1});this.items=data.items;
      return this.header(t('集市审核','Marketplace review'),t('核对项目来源、用途、费用、数据去向和作者的嵌入许可。','Check attribution, purpose, pricing, data notes and permission to embed.'))+this.recovery()+`<form class="market-filters" data-market-filter><label><span>${t('状态','Status')}</span><select name="status">${options({pending_review:states.pending_review,published:states.published,changes_requested:states.changes_requested,archived:states.archived,all:['全部','All']},status)}</select></label><button class="btn">${t('筛选','Filter')}</button></form>${data.items.map(i=>{const e=i.entry;return `<form class="card card-pad market-review mt24" data-market-review="${i.id}"><h2>${esc(i.title)} <span class="badge">${label(states,i.status)}</span></h2><p>${esc(e.summary)}</p><p>${esc(i.author.display_name || i.author.username)} · ${e.relation==='recommendation'?esc(e.original_author):t('本人 / 团队作品','My / our work')} · ${label(uses,e.use_case)}</p><div class="market-main-actions"><a class="btn" href="${postURL(e.question_id)}" target="_blank" rel="noopener">${t('查看原帖','Read original post')} ↗</a>${external(e.github_url,'GitHub',true)}${external(e.demo_url,t('检查演示','Check demo'))}</div><dl><dt>${t('费用与访问','Pricing & access')}</dt><dd>${label(prices,e.pricing)} · ${esc(e.access_note)}</dd><dt>${t('数据去向','Data handling')}</dt><dd>${esc(e.data_note)}</dd><dt>${t('技术栈 / 许可证','Stack / license')}</dt><dd>${esc(e.tech_stack)} / ${esc(e.license)}</dd><dt>${t('来源想法 ID','Source idea ID')}</dt><dd>${esc(e.source_idea_id || '—')}</dd></dl><div class="market-form-grid"><label><span>${t('决定','Decision')}</span><select name="decision">${options(i.status==='pending_review'?{publish:['通过并发布','Approve & publish'],changes:['退回修改','Request changes'],archive:['下架','Archive']}:{availability:['更新体验 / 精选状态','Update demo / featured status'],archive:['下架','Archive']},'')}</select></label><label><span>${t('演示状态','Demo status')}</span><select name="demo_status">${options({available:['可用','Available'],unavailable:['不可用','Unavailable']},i.demo_status)}</select></label><label class="market-check"><input type="checkbox" name="embed_allowed"${i.embed_allowed?' checked':''}${!e.embed_requested?' disabled':''}><span>${t('允许站内嵌入（需确认作者许可）','Allow embed (verify author permission)')}</span></label><label class="market-check"><input type="checkbox" name="featured"${i.featured?' checked':''}><span>${t('精选','Featured')}</span></label><label class="market-wide"><span>${t('审核说明（作者可见，至少 2 字）','Review note (visible to author, at least 2 characters)')}</span><textarea name="reason" required minlength="2" maxlength="500">${esc(i.review_note)}</textarea></label></div><button class="btn primary" type="submit">${t('确认审核决定','Save review decision')}</button><p role="status" data-market-status></p></form>`;}).join('')}${!data.items.length?`<section class="card card-pad">${t('当前没有待处理内容。','No entries to review.')}</section>`:''}${this.pager(data)}`;
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
        catch(error){if(current()){if(status)status.textContent=errorText(error);else root.alert(errorText(error));}}
        finally {this.busy=false;if(current()){buttons.forEach(b=>b.disabled=false);if(this.pending){let retry=container.querySelector('[data-market-retry]');if(!retry){retry=root.document.createElement('button');retry.type='button';retry.className='btn';retry.dataset.marketRetry='';retry.textContent=t('重试原操作','Retry original operation');(status?.parentElement || container).append(retry);retry.onclick=()=>run(container,()=>this.retry());}container.querySelectorAll('[data-market-editor] button[type="submit"],[data-market-review] button[type="submit"],[data-market-archive]').forEach(b=>b.disabled=true);}}}
      };
      container.querySelector('[data-market-retry]')?.addEventListener('click',()=>run(container,()=>this.retry()));
      container.querySelector('[data-market-filter]')?.addEventListener('submit',event=>{event.preventDefault();const p=new URLSearchParams(new root.FormData(event.currentTarget));root.location.assign(`${this.path}?${p}`);});
      const editor=container.querySelector('[data-market-editor]');
      if(editor) {
        const kind=editor.querySelector('select[name="kind"]');
        const toggle=()=>{const idea=kind.value==='idea';editor.querySelector('[data-market-project]').hidden=idea;editor.querySelector('[data-market-idea]').hidden=!idea;container.querySelector('[data-market-template]').href=introductionURL(kind.value);};
        kind.onchange=toggle;
        editor.addEventListener('submit',event=>{
          event.preventDefault();if(this.pending)return;
          const f=new root.FormData(editor),idea=f.get('kind')==='idea';
          let question=String(f.get('question') || '').trim();
          const match=question.match(/^(?:https:\/\/[^/]+)?\/questions\/(\d+)(?:\/[^?#]*)?(?:[?#].*)?$/);if(match)question=match[1];
          const entry={question_id:question,kind:f.get('kind'),summary:f.get('summary'),use_case:f.get('use_case'),relation:f.get('relation'),original_author:f.get('original_author'),github_url:idea?'':f.get('github_url'),demo_url:idea?'':f.get('demo_url'),tech_stack:idea?'':f.get('tech_stack'),license:idea?'':f.get('license'),pricing:idea?'':f.get('pricing'),access_note:idea?'':f.get('access_note'),data_note:idea?'':f.get('data_note'),embed_requested:!idea && f.has('embed_requested'),source_idea_id:idea?'':f.get('source_idea_id'),idea_stage:idea?f.get('idea_stage'):''};
          run(editor,()=>this.command({op:event.submitter?.value || 'submit',id:this.item?.id || '',version:this.item?.version || 0,entry}));
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
  const exports={...api,View,safeURL,card};
  if(typeof module==='object' && module.exports)module.exports=exports;
  else root.MetarMarketplace=Object.freeze(exports);
})(typeof window==='undefined'?globalThis:window);
