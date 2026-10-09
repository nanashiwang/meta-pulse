/* Categories reuse Answer's administrator-managed recommended tags. */
'use strict';
(() => {
  const t = (...args) => window.MetarI18n?.t(...args) ?? args[0];
  const categories = [
    ['develop', '开发调优', '开发、部署、性能优化与问题排查。'],
    ['domestic', '国产替代', '国产模型、软件与服务的使用实践。'],
    ['resource', '资源荟萃', '分享有用的工具、项目与学习资源。'],
    ['cloud-asset', '网盘资源', '分享有权公开的网盘资料与资源索引。', 'resource'],
    ['wiki', '文档共建', '共同完善教程、文档与经验总结。'],
    ['job', '非我莫属', '招聘、求职与项目协作。'],
    ['reading', '读书成诗', '阅读、写作与学习交流。'],
    ['news', '前沿快讯', '技术、模型与行业新动态。'],
    ['feeds', '网络记忆', '值得保存的网络文章与见闻。'],
    ['welfare', '福利羊毛', '分享公开优惠与社区福利。'],
    ['gossip', '搞七捻三', '日常交流、想法与有趣的发现。'],
    ['square', '虫洞广场', '跨领域交流与寻找同好。'],
    ['feedback', '运营反馈', '社区建议、问题反馈与类别标签申请。'],
  ].map(([slug_name, display_name, description, parent]) => Object.freeze({slug_name, display_name, description, parent}));
  const tags = [
    ['ai', '人工智能', '模型、智能体与 AI 应用。'],
    ['announcement', '公告', '社区正式通知。'],
    ['original', '原创', '由作者独立创作的内容。'],
    ['qa', '快问快答', '简明的问题与解答。'],
    ['lottery', '抽奖', '社区活动交流；不代表 Pulse 奖励资格。'],
    ['featured', '精华神帖', '值得反复阅读的优质内容。'],
    ['megathread', '集中帖', '同类信息的集中整理与持续讨论。'],
  ].map(([slug_name, display_name, description]) => Object.freeze({slug_name, display_name, description}));
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
    return `<nav class="taxonomy-requests" aria-label="${t('申请新类别或标签')}"><a class="btn small" href="${esc(requestURL(askPath,'category',english,feedback?.slug_name))}">${t('申请新类别')}</a><a class="btn small" href="${esc(requestURL(askPath,'tag',english,feedback?.slug_name))}">${t('申请新标签')}</a>${feedback ? `<a data-router href="${topicURL(feedback.slug_name)}" class="btn small">${t('查看运营反馈')}</a>` : ''}</nav>`;
  }

  function categoryCard(tag, preview = false) {
    const seed = seedFor(tag.slug_name), parent = seedFor(seed?.parent);
    return `<article class="card taxonomy-category"><div class="taxonomy-category-heading"><span class="taxonomy-dot"></span><h2>${preview ? esc(t(name(tag))) : `<a data-router href="${topicURL(tag.slug_name)}">${esc(name(tag))}</a>`}</h2>${parent ? `<small class="muted">${esc(t(parent.display_name))}</small>` : ''}</div><p>${esc(preview ? t(tag.description) : tag.description || t('浏览这个类别中的讨论。'))}</p>${preview ? `<span class="badge">${t('待管理员启用')}</span>` : `<a data-router class="textlink" href="${topicURL(tag.slug_name)}">${t('浏览讨论')}</a>`}</article>`;
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
      return toolbar + (!categoryList.length ? `<p class="muted">${t('起步类别待管理员启用，已有帖子和标签仍可正常浏览。')}</p>` : '') + `<div class="taxonomy-grid">${(categoryList.length ? detailed : categories).map(tag=>categoryCard(tag,!categoryList.length)).join('')}</div>`;
    }
    const result = await answer.listTags({page,pageSize:48,order:'name'});
    const list = Array.isArray(result?.list) ? result.list : [];
    const categoryNames = new Set(categoryList.map(item=>item.slug_name));
    const totalPages = Math.max(1,Math.ceil((Number(result?.count)||0)/48));
    const cards = list.map(tag => `<article class="card taxonomy-tag"><div class="between wrap"><h2><a data-router href="${topicURL(tag.slug_name)}">${esc(name(tag))}</a></h2>${categoryNames.has(tag.slug_name)?`<span class="badge green">${t('类别')}</span>`:''}</div><p>${esc(tag.description || t('该话题暂未添加介绍。'))}</p><small class="muted">${esc(window.MetarI18n.countLabel(tag.question_count,'questions'))}</small></article>`).join('');
    const preview = !list.length && !Number(result?.count) ? `<p class="muted">${t('常用标签待管理员启用。')}</p><div class="taxonomy-seed-tags">${tags.map(tag=>`<span class="badge">${esc(t(tag.display_name))}</span>`).join('')}</div>` : '';
    return toolbar + preview + `<div class="taxonomy-grid">${cards}</div>` + (totalPages>1 ? `<nav class="prod-pager" aria-label="${t('标签分页')}">${page>1?`<a data-router class="btn small" href="/topics?view=tags&page=${page-1}">${t('上一页')}</a>`:''}<span>${t('第 {page} / {total} 页',{page,total:totalPages})}</span>${page<totalPages?`<a data-router class="btn small" href="/topics?view=tags&page=${page+1}">${t('下一页')}</a>`:''}</nav>` : '');
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
  }

  function adminPage() {
    return `<section class="card card-pad stack"><div class="flex wrap"><a class="btn" data-router href="/topics">${t('查看类别与标签')}</a><a class="btn" href="/admin/write">${t('打开撰写设置')}</a><a class="btn" href="/tags">${t('管理原生标签')}</a></div><p>${t('初始化会补齐下列类别和常用标签，保留已有标签、介绍与撰写设置。已有推荐标签也作为类别显示。')}</p><div class="taxonomy-seed-tags">${categories.map(item=>`<span class="badge green">${esc(t(item.display_name))}</span>`).join('')}</div><div class="taxonomy-seed-tags">${tags.map(item=>`<span class="badge">${esc(t(item.display_name))}</span>`).join('')}</div><form data-form="taxonomy-init"><label class="taxonomy-confirm"><input type="checkbox" required name="confirmed">${t('我已核对起步目录，并确认没有其他管理员同时修改撰写设置。')}</label><button type="submit" class="btn primary">${t('初始化类别与标签')}</button><p class="muted" role="status" data-taxonomy-status></p></form><details><summary>${t('处理新增申请')}</summary><p>${t('在申请帖回复审核结果。通过后在原生标签页创建标签；新增类别还需加入撰写设置的推荐标签。公告等管理专用标签可在撰写设置中设为保留标签。')}</p><p>${t('类别用于整理内容，可与其他标签一起选择。网盘资源单独筛选，不自动合并到资源荟萃；申请和标签本身不授予 Pulse 权益。')}</p></details></section>`;
  }

  window.MetarTaxonomy = Object.freeze({categories:Object.freeze(categories),tags:Object.freeze(tags),seedFor,topicURL,askURL,requestURL,recommended,directory,initialize,adminPage});
})();
