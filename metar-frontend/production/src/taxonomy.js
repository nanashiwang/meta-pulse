/* Categories reuse Answer's administrator-managed recommended tags. */
'use strict';
(() => {
  const t = (...args) => window.MetarI18n?.t(...args) ?? args[0];
  const categories = [
    ['develop', '开发实践', '开发、部署、性能优化与问题排查。'],
    ['domestic', '国产生态', '国产模型、软件与服务的使用实践。'],
    ['resource', '工具资源', '分享有用的工具、项目与学习资源。'],
    ['cloud-asset', '资料分享', '分享有权公开的网盘资料与资源索引。', 'resource'],
    ['wiki', '教程共建', '共同完善教程、文档与经验总结。'],
    ['job', '招聘协作', '招聘、求职与项目协作。'],
    ['reading', '阅读思考', '阅读、写作与学习交流。'],
    ['news', '科技动态', '技术、模型与行业新动态。'],
    ['feeds', '好文收藏', '值得保存的网络文章与见闻。'],
    ['welfare', '优惠活动', '分享公开优惠与社区福利。'],
    ['gossip', '日常交流', '日常交流、想法与有趣的发现。'],
    ['square', '灵感交流', '跨领域交流与寻找同好。'],
    ['feedback', '站务建议', '社区建议、问题反馈与类别标签申请。'],
  ].map(([slug_name, display_name, description, parent]) => Object.freeze({slug_name, display_name, description, parent}));
  const tags = [
    ['ai', 'AI 交流', '模型、智能体与 AI 应用。'],
    ['announcement', '社区公告', '社区正式通知。'],
    ['original', '原创分享', '由作者独立创作的内容。'],
    ['qa', '问题求助', '简明的问题与解答。'],
    ['lottery', '有奖活动', '社区活动交流；不代表 Pulse 奖励资格。'],
    ['featured', '精选内容', '值得反复阅读的优质内容。'],
    ['megathread', '专题汇总', '同类信息的集中整理与持续讨论。'],
  ].map(([slug_name, display_name, description]) => Object.freeze({slug_name, display_name, description}));
  const legacyNames = Object.freeze({'develop':'开发调优','domestic':'国产替代','resource':'资源荟萃','cloud-asset':'网盘资源','wiki':'文档共建','job':'非我莫属','reading':'读书成诗','news':'前沿快讯','feeds':'网络记忆','welfare':'福利羊毛','gossip':'搞七捻三','square':'虫洞广场','feedback':'运营反馈','ai':'人工智能','announcement':'公告','original':'原创','qa':'快问快答','lottery':'抽奖','featured':'精华神帖','megathread':'集中帖'});
  const seeds = Object.freeze([...categories, ...tags]);
  const seedFor = slug => seeds.find(item => item.slug_name === slug);
  const name = tag => tag.display_name || tag.slug_name;
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;'}[c]));
  const topicURL = slug => `/topic/${encodeURIComponent(slug)}`;
  const askURL = (path, slug) => `${path}?${new URLSearchParams({tags:slug})}`;

  function recommended(site) {
    if (!site?.site_write || !Array.isArray(site.site_write.recommend_tags)) throw new Error(t('暂时无法读取类别设置，请重试。'));
    return site.site_write.recommend_tags.filter(item => typeof item?.slug_name === 'string' && item.slug_name);
  }

  function requestURL(path, kind, english = false, feedback = '') {
    const category = kind === 'category';
    const title = english ? `[${category ? 'Category' : 'Tag'} request] Name` : `【${category ? '类别' : '标签'}申请】请填写名称`;
    const body = english
      ? '## Proposed name\n\n## Scope and purpose\n\n## Why existing categories or tags do not fit\n\n## Example topics\n\nPlease include public topic links or example titles. An administrator will review this request and reply here.'
      : '## 建议名称\n\n## 适用内容与用途\n\n## 现有类别或标签为何不能满足\n\n## 示例话题\n\n请提供公开话题链接或示例标题。管理员审核后会在本帖回复处理结果。';
    const source = `---\ntitle: ${JSON.stringify(title)}\n---\n${body}`;
    // Answer 1.7.1 applies decodeURIComponent after URLSearchParams decoding.
    const params = new URLSearchParams({prefill: encodeURIComponent(source)});
    if (feedback) params.set('tags', feedback);
    return `${path}?${params}`;
  }

  function requests(askPath, categoryList, english) {
    const feedback = categoryList.find(item => item.slug_name === 'feedback' || item.display_name === '运营反馈');
    return `<nav class="taxonomy-requests" aria-label="${t('申请新类别或标签')}"><a class="btn small" href="${esc(requestURL(askPath,'category',english,feedback?.slug_name))}">${t('申请新类别')}</a><a class="btn small" href="${esc(requestURL(askPath,'tag',english,feedback?.slug_name))}">${t('申请新标签')}</a>${feedback ? `<a data-router href="${topicURL(feedback.slug_name)}" class="btn small">${t('查看站务建议')}</a>` : ''}</nav>`;
  }

  const catalogs = new WeakMap();
  const scope = answer => JSON.stringify([answer.token?.() || '', window.MetarI18n?.locale(), answer.contentEpoch]);
  const publicTag = tag => ({slug_name:tag.slug_name,display_name:name(tag)});
  const validTags = list => Array.isArray(list) ? list.filter(tag=>typeof tag?.slug_name === 'string' && tag.slug_name).map(publicTag) : [];
  const color = slug => [...String(slug)].reduce((hash,char)=>(hash * 31 + char.charCodeAt(0)) >>> 0, 0) % 8;
  const marker = slug => `<span class="taxonomy-dot taxonomy-color-${color(slug)}" aria-hidden="true"></span>`;

  // Only a public projection lives here; session/locale/epoch changes discard it.
  function catalog(answer) {
    const key = scope(answer), previous = catalogs.get(answer);
    if (previous?.key === key && (previous.pending || previous.expires > Date.now())) return previous.promise;
    const entry = {key,pending:true,expires:0};
    entry.promise = Promise.allSettled([
      answer.request('/siteinfo').then(site=>recommended(site).map(publicTag)),
      answer.request(`/tags?${new URLSearchParams({tags:tags.map(tag=>tag.slug_name).join(',')})}`).then(validTags),
      answer.request('/tags/page?page=1&page_size=48&query_cond=popular').then(result=>validTags(result?.list)),
    ]).then(([categoryResult,commonResult,popularResult])=>{
      const categoryList = categoryResult.status === 'fulfilled' ? categoryResult.value : [];
      const categoryNames = new Set(categoryList.map(tag=>tag.slug_name));
      const common = commonResult.status === 'fulfilled' ? commonResult.value : [];
      const popular = popularResult.status === 'fulfilled' ? popularResult.value : [];
      const ordered = [...tags.map(seed=>common.find(tag=>tag.slug_name===seed.slug_name)).filter(Boolean),...popular];
      const tagList = [...new Map(ordered.filter(tag=>!categoryNames.has(tag.slug_name)).map(tag=>[tag.slug_name,tag])).values()];
      entry.pending = false;
      const failed = [categoryResult,commonResult,popularResult].some(result=>result.status === 'rejected');
      entry.expires = failed ? 0 : Date.now()+15000;
      return {categories:categoryList,tags:tagList,categoriesUnavailable:categoryResult.status==='rejected',tagsUnavailable:commonResult.status==='rejected' && popularResult.status==='rejected'};
    });
    catalogs.set(answer,entry);
    return entry.promise;
  }

  function navigation() {
    return `<nav class="taxonomy-nav" aria-label="${t('类别与标签')}">${[['categories','类别','▦'],['tags','标签','#']].map(([kind,label,icon])=>`<button type="button" class="nav-item taxonomy-trigger" data-action="taxonomy-open" data-kind="${kind}" aria-haspopup="dialog" aria-controls="taxonomy-drawer" aria-expanded="false"><span class="taxonomy-trigger-icon" aria-hidden="true">${icon}</span><span>${t(label)}</span><span class="taxonomy-chevron" aria-hidden="true">›</span></button>`).join('')}</nav>`;
  }

  function drawer(kind) {
    const label = kind === 'categories' ? '类别' : '标签';
    return `<div class="taxonomy-drawer-header"><h2 id="taxonomy-drawer-title">${t(label)}</h2><button type="button" class="icon-btn" data-action="taxonomy-close" aria-label="${t('关闭目录')}">×</button></div><label class="taxonomy-drawer-search"><span class="visually-hidden">${t('筛选目录')}</span><input type="search" data-taxonomy-search placeholder="${t('输入名称筛选…')}" aria-label="${t('筛选目录')}" autocomplete="off" autofocus></label><div class="taxonomy-drawer-body" data-taxonomy-options><p class="muted" role="status">${t('正在加载…')}</p></div><div class="taxonomy-drawer-footer" data-taxonomy-footer><a data-router class="textlink" href="${kind==='categories'?'/topics':'/topics?view=tags'}">${t(kind==='categories'?'浏览全部类别':'浏览全部标签')}</a></div>`;
  }

  function drawerItems(data, kind, query = '', current = '') {
    const list = data[kind] || [], filter = query.trim().toLocaleLowerCase();
    const matches = list.filter(tag=>`${name(tag)} ${tag.slug_name}`.toLocaleLowerCase().includes(filter));
    const unavailable = kind==='categories' ? data.categoriesUnavailable : data.tagsUnavailable;
    if (unavailable && !list.length) return `<p class="muted" role="status">${t('暂时无法加载')}</p><button type="button" class="btn small" data-action="taxonomy-retry">${t('重新加载')}</button>`;
    if (!matches.length) return `<p class="muted" role="status">${t(filter?'没有匹配的项目':'暂无可用项目')}</p>`;
    return `<nav aria-label="${t(kind==='categories'?'类别':'标签')}">${matches.map(tag=>`<a data-router class="taxonomy-option${tag.slug_name===current?' active':''}" href="${topicURL(tag.slug_name)}"${tag.slug_name===current?' aria-current="page"':''}>${kind==='categories'?marker(tag.slug_name):'<span class="taxonomy-hash" aria-hidden="true">#</span>'}<span>${esc(name(tag))}</span></a>`).join('')}</nav>`;
  }

  function drawerFooter(data, kind, askPath, english) {
    const feedback = data.categories.find(item=>item.slug_name==='feedback');
    return `<a data-router class="textlink" href="${kind==='categories'?'/topics':'/topics?view=tags'}">${t(kind==='categories'?'浏览全部类别':'浏览全部标签')}</a><a class="textlink" href="${esc(requestURL(askPath,kind==='categories'?'category':'tag',english,feedback?.slug_name))}">${t(kind==='categories'?'申请新类别':'申请新标签')}</a>`;
  }

  function filters(data, current = '') {
    const categorySelected = data.categories.some(tag=>tag.slug_name===current);
    const tagList = [...data.tags];
    if (current && !categorySelected && !tagList.some(tag=>tag.slug_name===current)) tagList.unshift({slug_name:current});
    const select = (kind,label,list,selected,unavailable) => `<label class="taxonomy-select"><span class="visually-hidden">${t(label)}</span><select data-action="taxonomy-filter" data-kind="${kind}" aria-label="${t(label)}"><option value="">${t(kind==='categories'?'全部类别':'全部标签')}</option>${list.map(tag=>`<option value="${esc(tag.slug_name)}"${selected===tag.slug_name?' selected':''}>${esc(name(tag))}</option>`).join('')}${unavailable?`<option disabled>${t('暂时无法加载')}</option>`:''}<option value="@directory">${t(kind==='categories'?'浏览全部类别':'浏览全部标签')}</option></select></label>`;
    return select('categories','按类别浏览',data.categories,categorySelected?current:'',data.categoriesUnavailable) + select('tags','按标签浏览',tagList,categorySelected?'':current,data.tagsUnavailable);
  }

  function categoryRow(tag, preview = false) {
    return `<article class="taxonomy-category">${marker(tag.slug_name)}<div class="taxonomy-row-content"><h2>${preview ? esc(t(name(tag))) : `<a data-router href="${topicURL(tag.slug_name)}">${esc(name(tag))}</a>`}</h2><p>${esc(preview ? t(tag.description) : tag.description || t('浏览这个类别中的讨论。'))}</p></div>${preview ? `<span class="badge">${t('待管理员启用')}</span>` : ''}</article>`;
  }

  async function directory(answer, query, askPath, english) {
    const view = query.get('view') === 'tags' ? 'tags' : 'categories';
    const page = Math.max(1, parseInt(query.get('page'), 10) || 1);
    const site = await answer.request('/siteinfo');
    const categoryList = recommended(site);
    const toolbar = `<div class="taxonomy-toolbar"><nav class="discussion-tabs" aria-label="${t('类别与标签')}"><a data-router class="discussion-tab ${view==='categories'?'active':''}" href="/topics"${view==='categories'?' aria-current="page"':''}>${t('类别')}</a><a data-router class="discussion-tab ${view==='tags'?'active':''}" href="/topics?view=tags"${view==='tags'?' aria-current="page"':''}>${t('全部标签')}</a></nav>${requests(askPath,categoryList,english)}</div>`;
    if (view === 'categories') {
      // Site settings expose names only; descriptions must come from current tags.
      const detailed = await Promise.all(categoryList.map(async tag => {
        try {
          const current = await answer.request(`/tag?name=${encodeURIComponent(tag.slug_name)}`);
          if (current?.slug_name === tag.slug_name && current.status === 'available') return {...tag,description:current.description};
        } catch (_) { /* A missing description must not hide a configured category. */ }
        return tag;
      }));
      return toolbar + (!categoryList.length ? `<p class="muted">${t('起步类别待管理员启用，已有帖子和标签仍可正常浏览。')}</p>` : '') + `<div class="taxonomy-list">${(categoryList.length ? detailed : categories).map(tag=>categoryRow(tag,!categoryList.length)).join('')}</div>`;
    }
    const result = await answer.listTags({page,pageSize:48,order:'name'});
    const list = Array.isArray(result?.list) ? result.list : [];
    const categoryNames = new Set(categoryList.map(item=>item.slug_name));
    const totalPages = Math.max(1,Math.ceil((Number(result?.count)||0)/48));
    const cards = list.map(tag => `<article class="taxonomy-tag">${categoryNames.has(tag.slug_name)?marker(tag.slug_name):'<span class="taxonomy-hash" aria-hidden="true">#</span>'}<div class="taxonomy-row-content"><h2><a data-router href="${topicURL(tag.slug_name)}">${esc(name(tag))}</a>${categoryNames.has(tag.slug_name)?`<span class="badge">${t('类别')}</span>`:''}</h2><p>${esc(tag.description || t('该话题暂未添加介绍。'))}</p></div><small class="taxonomy-count">${esc(window.MetarI18n.countLabel(tag.question_count,'questions'))}</small></article>`).join('');
    const preview = !list.length && !Number(result?.count) ? `<p class="muted">${t('常用标签待管理员启用。')}</p><div class="taxonomy-seed-tags">${tags.map(tag=>`<span class="badge">${esc(t(tag.display_name))}</span>`).join('')}</div>` : '';
    return toolbar + preview + `<div class="taxonomy-list">${cards}</div>` + (totalPages>1 ? `<nav class="prod-pager" aria-label="${t('标签分页')}">${page>1?`<a data-router class="btn small" href="/topics?view=tags&page=${page-1}">${t('上一页')}</a>`:''}<span>${t('第 {page} / {total} 页',{page,total:totalPages})}</span>${page<totalPages?`<a data-router class="btn small" href="/topics?view=tags&page=${page+1}">${t('下一页')}</a>`:''}</nav>` : '');
  }

  async function renameDefaults(answer, progress = () => {}) {
    const user = await answer.getCurrentUser();
    if (!window.MetarAdapters.isCommunityAdministrator(user)) throw new Error(t('需要管理员权限'));
    let changed = false;
    try {
      for (const item of seeds) {
        const existing = await answer.request(`/tag?name=${encodeURIComponent(item.slug_name)}`);
        if (existing?.slug_name !== item.slug_name || existing.main_tag_slug_name || existing.status !== 'available') throw new Error(t('已有同名标签状态不兼容，请先在原生标签管理中核对。'));
        if (existing.display_name !== legacyNames[item.slug_name]) continue;
        if (!existing.tag_id || typeof existing.original_text !== 'string') throw new Error(t('标签原文不完整，请刷新后重试。'));
        progress(item.display_name);
        changed = true;
        await answer.request('/tag',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({tag_id:existing.tag_id,slug_name:existing.slug_name,display_name:item.display_name,original_text:existing.original_text,edit_summary:'统一 METAR 起步目录名称'})});
        const saved = await answer.request(`/tag?name=${encodeURIComponent(item.slug_name)}`);
        if (saved?.tag_id !== existing.tag_id || saved.slug_name !== existing.slug_name || saved.display_name !== item.display_name || saved.original_text !== existing.original_text || saved.status !== 'available' || saved.main_tag_slug_name) throw new Error(t('保存结果尚未核实，请重新读取后继续。'));
      }
    } finally { if (changed) answer.clearContentCache(); }
  }

  // Native settings have no compare-and-swap endpoint. Refuse detected drift and
  // read back every write; no automatic retries after uncertain responses.
  async function initialize(answer, admin, progress = () => {}) {
    const user = await answer.getCurrentUser();
    if (!window.MetarAdapters.isCommunityAdministrator(user)) throw new Error(t('需要管理员权限'));
    const before = await admin.request('/siteinfo/write');
    if (!before || !Array.isArray(before.recommend_tags) || !Array.isArray(before.reserved_tags)) throw new Error(t('暂时无法读取类别设置，请重试。'));
    const reserved = new Set(before.reserved_tags.map(item=>item.slug_name));
    if (categories.some(item=>reserved.has(item.slug_name))) throw new Error(t('起步类别与已有保留标签冲突，请先在撰写设置中核对。'));
    for (const item of seeds) {
      progress(item.display_name);
      let existing;
      try { existing = await answer.request(`/tag?name=${encodeURIComponent(item.slug_name)}`); }
      catch (error) { if (error.status !== 404) throw error; }
      if (existing) {
        if (existing.slug_name !== item.slug_name || existing.main_tag_slug_name || existing.status !== 'available') throw new Error(t('已有同名标签状态不兼容，请先在原生标签管理中核对。'));
        continue;
      }
      await answer.request('/tag',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({slug_name:item.slug_name,display_name:item.display_name,original_text:item.description})});
      const saved = await answer.request(`/tag?name=${encodeURIComponent(item.slug_name)}`);
      if (saved?.slug_name !== item.slug_name || saved.status !== 'available' || saved.main_tag_slug_name) throw new Error(t('保存结果尚未核实，请重新读取后继续。'));
    }
    const latest = await admin.request('/siteinfo/write');
    if (JSON.stringify(latest) !== JSON.stringify(before)) throw new Error(t('撰写设置已被其他管理员修改，请刷新后重试。'));
    const merged = new Map(before.recommend_tags.map(item=>[item.slug_name,item]));
    categories.forEach(item=>{ if (!merged.has(item.slug_name)) merged.set(item.slug_name,{slug_name:item.slug_name,display_name:item.display_name}); });
    const next = {...before,recommend_tags:[...merged.values()]};
    if (JSON.stringify(next) !== JSON.stringify(before)) await admin.request('/siteinfo/write',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(next)});
    const saved = await admin.request('/siteinfo/write');
    const actual = new Set(saved?.recommend_tags?.map(item=>item.slug_name));
    const settings = value => JSON.stringify({...value,recommend_tags:undefined});
    if ([...merged.keys()].some(slug=>!actual.has(slug)) || actual.size !== merged.size || settings(saved)!==settings(before)) throw new Error(t('保存结果尚未核实，请重新读取后继续。'));
    answer.clearContentCache();
    await renameDefaults(answer, progress);
  }

  function adminPage() {
    return `<section class="card card-pad stack"><div class="flex wrap"><a class="btn" data-router href="/topics">${t('查看类别与标签')}</a><a class="btn" href="/admin/write">${t('打开撰写设置')}</a><a class="btn" href="/tags">${t('管理原生标签')}</a></div><p>${t('同步会补齐缺失类别和标签，并将旧版默认名称更新为下列名称；保留自定义名称、介绍、帖子归属与撰写设置。')}</p><div class="taxonomy-seed-tags">${categories.map(item=>`<span class="badge green">${esc(t(item.display_name))}</span>`).join('')}</div><div class="taxonomy-seed-tags">${tags.map(item=>`<span class="badge">${esc(t(item.display_name))}</span>`).join('')}</div><form data-form="taxonomy-init"><label class="taxonomy-confirm"><input type="checkbox" required name="confirmed">${t('我已核对起步目录，并确认没有其他管理员同时修改标签或撰写设置。')}</label><button type="submit" class="btn primary">${t('同步类别与标签')}</button><p class="muted" role="status" data-taxonomy-status></p></form><details><summary>${t('处理新增申请')}</summary><p>${t('在申请帖回复审核结果。通过后在原生标签页创建标签；新增类别还需加入撰写设置的推荐标签。公告等管理专用标签可在撰写设置中设为保留标签。')}</p><p>${t('类别用于整理内容，可与其他标签一起选择。资料分享单独筛选，不自动合并到工具资源；申请和标签本身不授予 Pulse 权益。')}</p></details></section>`;
  }

  window.MetarTaxonomy = Object.freeze({categories:Object.freeze(categories),tags:Object.freeze(tags),seedFor,scope,catalog,navigation,drawer,drawerItems,drawerFooter,filters,topicURL,askURL,requestURL,recommended,directory,initialize,renameDefaults,adminPage});
})();
