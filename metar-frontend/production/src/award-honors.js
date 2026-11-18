(function (root) {
  const esc = (v) =>
    String(v ?? "").replace(
      /[&<>"']/g,
      (c) =>
        ({
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          '"': "&quot;",
          "'": "&#39;",
        })[c],
    );
  const translate = (zh, en) => root.MetarI18n?.locale().startsWith('en') ? en : zh;
  function medalModels(rows, unavailable) {
    if (rows !== null && !Array.isArray(rows)) throw new Error('invalid badge data');
    const types = [
      {kind:'post',name:'编辑者',en:'Editor',icon:'bi-pencil-fill',condition:translate('获得周度或月度优秀帖子奖','Win a weekly or monthly Outstanding Post award')},
      {kind:'recommendation',name:'伯乐',en:'Talent Scout',icon:'bi-stars',condition:translate('推荐的帖子获得优秀帖子周奖，每篇累计一次','Earn once per recommended post that wins a weekly Outstanding Post award')},
      {kind:'response',name:'学者',en:'Scholar',icon:'bi-check-circle-fill',condition:translate('获得周度或月度优秀回应奖','Win a weekly or monthly Outstanding Response award')},
    ];
    return types.map(type => {
      const badge = rows?.find(b => b.kind === type.kind || (!b.kind && b.name === type.name));
      const count = badge?.count ?? 0;
      if (!Number.isSafeInteger(count) || count < 0) throw new Error('invalid badge count');
      const state = unavailable ? translate('获得状态暂不可用','Award status unavailable') : rows === null ? translate('正在读取获得状态…','Loading award status…') : count > 0 ? translate('已获得','Earned') + ' × ' + count : translate('尚未获得','Not earned yet');
      return { name:translate(type.name,type.en), icon:type.icon, condition:type.condition, count, state };
    });
  }
  function badges(rows, unavailable = false) {
    return '<div class="award-medal-catalog">' + medalModels(rows, unavailable).map(({name,condition,count,state}) => {
      return '<article class="award-medal-card' + (count > 0 ? ' is-earned' : '') + '"><svg class="award-medal-icon" viewBox="0 0 64 76" aria-hidden="true"><path d="M17 2h12l9 22-12 7z" fill="#c08320"/><path d="M35 2h12L38 31l-12-7z" fill="#f1bf45"/><circle cx="32" cy="45" r="27" fill="#e6b644" stroke="#ac7616" stroke-width="2"/><circle cx="32" cy="45" r="21" fill="#ffe5a0" stroke="#bb8729"/><path d="m32 30 4.4 9 10 1.5-7.2 7 1.7 10-8.9-4.7-8.9 4.7 1.7-10-7.2-7 10-1.5z" fill="#b57b13"/></svg><div><strong>' + esc(name) + (count > 0 ? ' × ' + count : '') + '</strong><span class="award-medal-tier">' + translate('永久金色勋章','Permanent gold medal') + '</span><p>' + esc(condition) + '</p><small>' + esc(state) + '</small></div></article>';
    }).join('') + '</div>';
  }
  function nativeBadgeCards(rows, unavailable = false) {
    return medalModels(rows, unavailable).map(({name,icon,condition,count,state}) =>
      '<div class="mb-4 col-lg-3 col-md-4 col-sm-6"><a class="card text-center badge-card h-100" href="/awards?view=honors"><div class="card-body">' +
      (count > 0 ? '<span class="badge bg-success metar-award-badge-earned">' + esc(state) + '</span>' : '') +
      '<i class="br ' + icon + ' lh-1 gold metar-award-badge-icon" aria-hidden="true"></i><h6 class="mb-0 mt-3">' + esc(name) +
      '</h6><p class="small text-secondary mt-2 mb-0">' + esc(condition) + '</p>' +
      (count > 0 ? '' : '<p class="small text-secondary mt-2 mb-0">' + esc(state) + '</p>') + '</div></a></div>'
    ).join('');
  }
  function mountBadgeGroup(doc, api, current) {
    const group = doc.createElement('div');
    group.className = 'mb-4';
    group.dataset.metarAwardBadgeGroup = '';
    const heading = doc.createElement('h5');
    heading.className = 'mb-4';
    heading.textContent = translate('评选金色勋章','Gold award medals');
    const grid = doc.createElement('div');
    grid.className = 'row';
    grid.innerHTML = nativeBadgeCards(null);
    group.append(heading, grid);
    let active = true;
    const insert = () => {
      if (!active || !current()) return;
      const container = doc.querySelector('.pt-4.mb-5 > h3.mb-4')?.parentElement;
      if (container && container.lastElementChild !== group) container.append(group);
    };
    const observer = new doc.defaultView.MutationObserver(insert);
    observer.observe(doc.body, {childList:true,subtree:true});
    insert();
    api.request('/metar/awards/honors').then(data => {
      if (!active || !current()) return;
      if (!Array.isArray(data?.badges)) throw new Error('invalid badge data');
      grid.innerHTML = nativeBadgeCards(data.badges);
    }).catch(() => {
      if (active && current()) grid.innerHTML = nativeBadgeCards(null, true);
    });
    return () => { active = false; observer.disconnect(); group.remove(); };
  }
  class View {
    constructor(api) {
      this.api = api;
      this.epoch = 0;
    }
    dispose() {
      this.epoch++;
      this.root = null;
    }
    async page(query) {
      this.pageNo = Math.max(1, Number(query.get("page") || 1));
      this.data = await this.api.request(
        "/metar/awards/honors?page=" + this.pageNo,
      );
      return this.html();
    }
    html() {
      const d = this.data;
      return (
        '<section class="card card-pad"><h1>我的头衔与勋章</h1><p>头衔与勋章在公布入选时获得，与手动发放闪分开处理。</p><form data-honor-form><label>帖子等级旁展示的头衔<select name="title"><option value="">不展示</option>' +
        d.titles
          .map(
            (v) =>
              '<option value="' +
              esc(v.key) +
              '" ' +
              (d.selection === v.key ? "selected" : "") +
              ">" +
              esc(v.name) +
              " · " +
              (v.style === "glow" ? "月度光效" : "每周普通") +
              " · " +
              esc(new Date(v.expires_at * 1000).toLocaleString()) +
              " 到期</option>",
          )
          .join("") +
        '</select></label><button class="btn primary mt8">保存展示选择</button><p data-honor-status role="status"></p></form><h2 class="mt24">永久金色勋章</h2>' +
        badges(d.badges) +
        '<h2 class="mt24">获得记录</h2>' +
        d.records
          .map(
            (r) =>
              '<article class="card card-pad mt8"><strong>' +
              esc(r.period.key) +
              " · " +
              (r.period.type === "month" ? "月度" : "每周") +
              (r.period.kind === "post" ? "优秀帖子奖" : "优秀回应奖") +
              "</strong><p>" +
              esc(r.reason) +
              '</p><a href="/questions/' +
              esc(r.question_id) +
              (r.answer_id !== "0" ? "/" + esc(r.answer_id) : "") +
              '" target="_blank" rel="noopener">查看获奖内容 ↗</a></article>',
          )
          .join("") +
        (d.recommendations?.length ? '<h2 class="mt24">' + translate('推荐获奖记录','Recommendation rewards') + '</h2>' + d.recommendations.map(r => {
          const title = r.title ? '<a href="/questions/' + esc(r.question_id) + '">' + esc(r.title) + '</a>' : translate('帖子暂不可见','Post unavailable');
          return '<article class="card card-pad mt8">' + title + '<p>' + esc(r.period_key) + ' · ' + esc((r.cents / 100).toFixed(2)) + ' ⚡️ · ' + (r.paid_at ? translate('已手动发放','Manually delivered') : translate('待手动发放','Pending manual delivery')) + '</p></article>';
        }).join('') : '') +
        '<div class="flex mt16">' +
        (this.pageNo > 1
          ? '<a href="/awards?view=honors&page=' +
            (this.pageNo - 1) +
            '">上一页</a>'
          : "") +
        (d.more
          ? '<a href="/awards?view=honors&page=' +
            (this.pageNo + 1) +
            '">下一页</a>'
          : "") +
        '</div><a href="/awards">返回获奖名单</a></section>'
      );
    }
    mount(node) {
      this.root = node;
      const form = node.querySelector("[data-honor-form]");
      form.onsubmit = async (e) => {
        e.preventDefault();
        const button = form.querySelector("button"),
          status = form.querySelector("[data-honor-status]"),
          epoch = this.epoch;
        button.disabled = true;
        try {
          await this.api.request("/metar/awards/command", {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "X-Metar-Request": "1",
              "Idempotency-Key": root.crypto.randomUUID(),
            },
            body: JSON.stringify({
              op: "title",
              title_key: form.elements.title.value,
            }),
          });
          if (epoch === this.epoch)
            status.textContent = "已保存，帖子中会展示所选有效头衔。";
        } catch (_) {
          if (epoch === this.epoch)
            status.textContent = "保存失败，头衔可能已过期，请刷新后重试。";
        } finally {
          button.disabled = false;
        }
      };
    }
  }
  root.MetarAwardHonors = { View, badges, mountBadgeGroup };
})(window);
