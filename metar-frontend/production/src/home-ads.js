(function (root) {
  "use strict";
  const endpoint = "/metar/ads";
  const writeAd = (client, path, method, body) =>
    client.request(path, { method, headers: { "X-Metar-Request": "1" }, body });
  const imageURL = (item, base = "/answer/api/v1") =>
    base +
    endpoint +
    "/" +
    encodeURIComponent(item.id) +
    "?v=" +
    encodeURIComponent(item.revision || "1");
  async function mountSlots(slots, api) {
    const all = await api.request(endpoint);
    for (const slot of slots) {
      if (!slot?.isConnected) continue;
      const items = all.filter(
        (item) => (item.placement || "side") === slot.dataset.homeAd,
      );
      if (items.length) mountSlot(slot, items, api.base);
    }
  }
  function mountSlot(slot, items, base) {
    const link = document.createElement("a"),
      badge = document.createElement("small"),
      close = document.createElement("button");
    link.target = "_blank";
    link.rel = "sponsored noopener noreferrer";
    badge.textContent = "广告";
    close.type = "button";
    close.textContent = "×";
    close.setAttribute("aria-label", "关闭广告");
    let index = 0, timer, closed = false, generation = 0, current = null;
    let gesture = null, suppressClick = false;
    const stop = () => clearTimeout(timer);
    function move(step) {
      if (closed || items.length < 2) return;
      index = (index + step + items.length) % items.length;
      show(step);
    }
    function arrow(step) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = step < 0 ? 'ad-prev' : 'ad-next';
      button.textContent = step < 0 ? '‹' : '›';
      button.setAttribute('aria-label', step < 0 ? '上一条广告' : '下一条广告');
      button.onclick = () => move(step);
      return button;
    }
    const prev = arrow(-1), next = arrow(1), indicator = document.createElement('span');
    indicator.className = 'ad-position';
    function controls() {
      const n = items.length;
      prev.hidden = next.hidden = indicator.hidden = n < 2;
      indicator.setAttribute('aria-label', '广告 ' + (index + 1) + ' / ' + n);
      if (n > 8) { indicator.textContent = (index + 1) + ' / ' + n; return; }
      indicator.replaceChildren(...items.map((_, i) => {
        const dot = document.createElement('i');
        dot.className = i === index ? 'active' : '';
        return dot;
      }));
    }
    function schedule() {
      stop();
      if (items.length < 2 || closed || !slot.isConnected) return;
      timer = setTimeout(() => {
        if (document.hidden || gesture) schedule(); else move(1);
      }, 15000);
    }
    link.ondragstart = e => e.preventDefault();
    link.onpointerdown = e => {
      if (!e.isPrimary || e.button !== 0) return;
      suppressClick = false;
      if (items.length < 2) return;
      gesture = {id:e.pointerId, x:e.clientX, y:e.clientY};
      link.setPointerCapture(e.pointerId);
      stop();
    };
    link.onpointerup = e => {
      if (!gesture || gesture.id !== e.pointerId) return;
      const dx = e.clientX - gesture.x, dy = e.clientY - gesture.y;
      gesture = null;
      suppressClick = Math.abs(dx) >= 28 && Math.abs(dx) > Math.abs(dy) * 1.25;
      if (suppressClick) move(dx < 0 ? 1 : -1); else schedule();
    };
    link.onpointercancel = link.onlostpointercapture = () => { if (gesture) { gesture = null; schedule(); } };
    link.onclick = e => { if (suppressClick && e.detail !== 0) { e.preventDefault(); e.stopPropagation(); suppressClick = false; } };
    slot.onkeydown = e => {
      if (items.length < 2 || (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight')) return;
      e.preventDefault(); move(e.key === 'ArrowLeft' ? -1 : 1);
    };
    function show(direction) {
      stop();
      if (closed || !slot.isConnected || !items.length) return;
      const turn = ++generation, item = items[index], image = document.createElement('img');
      image.alt = '访问广告网站'; image.draggable = false;
      image.onload = () => {
        if (closed || !slot.isConnected || generation !== turn) return;
        const old = current;
        current = image;
        link.replaceChildren(image);
        if (old && root.matchMedia && !root.matchMedia('(prefers-reduced-motion: reduce)').matches && image.animate) {
          old.className = 'ad-outgoing';old.alt = '';link.append(old);
          const dx = direction < 0 ? '-100%' : '100%';
          const options = {duration:260, easing:'ease-out'};
          image.animate([{transform:'translateX('+dx+')'},{transform:'translateX(0)'}],options);
          const leave = old.animate([{transform:'translateX(0)'},{transform:'translateX('+(direction < 0 ? '100%' : '-100%')+')'}],options);
          leave.onfinish = () => old.remove();
        }
        link.href = item.link;slot.hidden = false;controls();schedule();
      };
      image.onerror = () => {
        if (closed || !slot.isConnected || generation !== turn) return;
        items.splice(index,1);
        if (!items.length) { slot.hidden=true;return; }
        index %= items.length;show(direction);
      };
      image.src = imageURL(item,base);
    }
    close.onclick = () => { closed=true; stop(); slot.hidden=true;slot.replaceChildren(); };
    slot.replaceChildren(link,badge,close,prev,next,indicator);
    show(0);
  }
  const managerHTML = () =>
    `<section data-ad-manager><header class="ad-manager-heading"><div><h1>广告管理</h1><p class="muted">顶部和侧边可同时展示，各位置的广告按发布顺序每 15 秒轮播</p></div><button type="button" class="btn primary" data-new>＋ 新增广告</button></header><div data-create></div><p role="status"></p><section aria-label="广告列表"><h2>广告列表</h2><div data-cards></div></section></section>`;
  // Preview and JPEG share one canvas: the pixels inside the frame are exactly what is saved.
  async function selectCrop(source, placement, form, previous) {
    if (source.size > 8 * 1024 * 1024) throw Error('请选择不超过 8 MB 的图片。');
    const bitmap=await createImageBitmap(source), dialog=document.createElement('dialog');
    try {
      if(!form.isConnected)return null;
      const top=placement==='top', w=top?1600:800,h=top?160:600;
      dialog.className='ad-crop-dialog';dialog.setAttribute('aria-label','选择广告显示范围');
      dialog.innerHTML='<h2>选择显示范围</h2><p></p><canvas tabindex="0" aria-label="拖动图片或按方向键移动"></canvas><label>缩放 <input type="range" min="1" max="4" step="0.01" value="1"></label><footer><button type="button" data-cancel class="btn">取消</button><button type="button" data-apply class="btn primary">使用选区</button></footer>';
      const find=s=>dialog.querySelector(s);find('p').textContent=(top?'顶部横幅 · 10:1':'侧边广告 · 4:3')+'　拖动图片并缩放，仅保留框内内容。';
      const canvas=find('canvas'),range=find('input'),apply=find('[data-apply]');canvas.width=w;canvas.height=h;
      const ctx=canvas.getContext('2d'),scale=Math.max(w/bitmap.width,h/bitmap.height);
      let cx=previous?.cx??bitmap.width/2,cy=previous?.cy??bitmap.height/2,sw,sh,drag=null;
      range.value=String(previous?.zoom??1);
      function draw(){const zoom=Number(range.value);sw=w/(scale*zoom);sh=h/(scale*zoom);cx=Math.max(sw/2,Math.min(bitmap.width-sw/2,cx));cy=Math.max(sh/2,Math.min(bitmap.height-sh/2,cy));ctx.fillStyle='#fff';ctx.fillRect(0,0,w,h);ctx.drawImage(bitmap,cx-sw/2,cy-sh/2,sw,sh,0,0,w,h);}
      draw();range.oninput=draw;
      canvas.onpointerdown=e=>{if(!e.isPrimary)return;canvas.setPointerCapture(e.pointerId);drag=[e.clientX,e.clientY]};canvas.onpointermove=e=>{if(!e.isPrimary||!drag)return;const rect=canvas.getBoundingClientRect();cx-=(e.clientX-drag[0])*sw/rect.width;cy-=(e.clientY-drag[1])*sh/rect.height;drag=[e.clientX,e.clientY];draw()};canvas.onlostpointercapture=()=>{drag=null};
      canvas.onkeydown=e=>{const d={ArrowLeft:[1,0],ArrowRight:[-1,0],ArrowUp:[0,1],ArrowDown:[0,-1]}[e.key];if(d){e.preventDefault();cx+=d[0]*sw*.025;cy+=d[1]*sh*.025;draw()}};
      form.append(dialog);dialog.showModal();
      const accepted=await new Promise(resolve=>{dialog.onclose=()=>resolve(dialog.returnValue==='apply');apply.onclick=()=>dialog.close('apply');find('[data-cancel]').onclick=()=>dialog.close('cancel')});
      if(!accepted)return null;
      const blob=await new Promise(resolve=>canvas.toBlob(resolve,'image/jpeg',.85));if(!blob||blob.size>880*1024)throw Error('选区压缩后仍然过大，请缩小原图后重试。');
      return {blob,cx,cy,zoom:Number(range.value)};
    }finally{dialog.remove();bitmap.close()}
  }
  const fields = `<header class="ad-card-head"><h3></h3><span class="ad-state"></span></header>
 <div class="ad-card-body"><div><div class="ad-preview"><img alt="广告图片预览"><span data-placeholder>请选择横版图片</span></div><button type="button" data-retry class="btn small" hidden>重新加载预览</button><button type="button" data-adjust class="btn small" hidden>调整显示范围</button><label>广告海报（选择新图可替换）<input name="image" type="file" accept="image/jpeg,image/png,image/webp"></label><small class="muted">JPG、PNG 或 WebP，最大 8 MB</small></div>
 <div class="ad-fields"><label>展示状态<select name="enabled"><option value="true">展示中</option><option value="false">暂停展示（保留海报和设置）</option></select></label><label>展示位置<select name="placement"><option value="side">侧边广告 · 4:3</option><option value="top">顶部横幅 · 10:1</option></select></label><label>跳转网址<input name="link" type="url" placeholder="https://" required></label><label>后台备注<textarea name="note" maxlength="300" rows="2" placeholder="例如：合作方名称、投放周期（选填）"></textarea></label><small class="muted">备注仅管理员可见，不会展示在首页。</small></div></div>
 <footer><span role="status"></span><button type="button" data-secondary class="btn small"></button><button type="submit" class="btn primary"></button></footer>`;
  function mountManager(el, api, admin) {
    const cards = el.querySelector("[data-cards]"),
      create = el.querySelector("[data-create]"),
      add = el.querySelector("[data-new]"),
      status = el.querySelector("[role=status]");
    function emptyState() {
      status.textContent = cards.childElementCount ? "" : "暂无广告。";
    }
    function editor(item) {
      const form = document.createElement("form");
      form.className = "ad-editor card";
      form.innerHTML = fields;
      const q = (s) => form.querySelector(s),
        f = form.elements,
        img = q("img"),
        hint = q("[data-placeholder]"),
        retry = q("[data-retry]"),
        msg = q("[role=status]"),
        save = q("[type=submit]"),
        secondary = q("[data-secondary]");
      q("h3").textContent = item ? "广告 #" + item.id : "新增广告";
      q(".ad-state").textContent = item
        ? item.enabled === false
          ? "已暂停"
          : "展示中"
        : "草稿";
      save.textContent = item ? "保存修改" : "发布广告";
      secondary.textContent = item ? "删除广告" : "取消";
      f.placement.value = item?.placement || "side";
      const preview = q(".ad-preview");
      const fit = () => preview.setAttribute("data-format", f.placement.value);

      fit();
      f.link.value = item?.link || "";
      f.enabled.value = String(item?.enabled !== false);
      f.note.value = item?.note || "";

      let seq = 0,
        blob = null,
        pending = false;
      function painting(src, fallback) {
        const generation = ++seq;
        retry.hidden = true;
        hint.hidden = true;
        img.hidden = false;
        img.onerror = async () => {
          if (seq !== generation) return;
          img.onerror = null;
          if (fallback) {
            try {
              const res = await admin.request(
                endpoint + "/" + item.id + "/preview",
              );
              if (seq !== generation) return;
              if (!res.jpeg) throw Error();
              painting("data:image/jpeg;base64," + res.jpeg, false);
              return;
            } catch (_) {}
          }
          if (seq !== generation) return;
          img.hidden = true;
          hint.textContent = "预览加载失败";
          hint.hidden = false;
          retry.hidden = !item;
        };
        img.src = src;
      }
      function original() {
        seq++;
        blob = null;
        if (item) painting(imageURL(item, api.base), true);
        else {
          img.hidden = true;
          hint.hidden = false;
          hint.textContent = "请选择横版图片";
          retry.hidden = true;
        }
      }
      original();
      retry.onclick = () => original();
      const adjust=q('[data-adjust]');let source=null,selection=null,format=f.placement.value;
      adjust.hidden=!item;
      async function choose(file, nextFormat){
        if(pending)return false;pending=true;save.disabled=secondary.disabled=f.image.disabled=f.placement.disabled=adjust.disabled=true;msg.textContent='';
        try{
          const originalFile=file||source||await (async()=>{const r=await fetch(imageURL(item,api.base));if(!r.ok)throw Error('无法读取当前海报，请重新选择图片。');return r.blob()})();
          const result=await selectCrop(originalFile,nextFormat,form,originalFile===source && nextFormat===format?selection:null);if(!result)return false;
          const reader=new FileReader();const data=await new Promise((resolve,reject)=>{reader.onload=()=>resolve(reader.result);reader.onerror=reject;reader.readAsDataURL(result.blob)});
          source=originalFile;selection=result;blob=result.blob;format=nextFormat;painting(data,false);fit();adjust.hidden=false;msg.textContent='选区已确认，保存广告后生效。';return true;
        }catch(e){msg.textContent=e.message||'图片无法读取，请重新选择。';return false}
        finally{pending=false;save.disabled=secondary.disabled=f.image.disabled=f.placement.disabled=adjust.disabled=false;}
      }
      f.image.onchange=async()=>{const file=f.image.files[0];if(file)await choose(file,f.placement.value);f.image.value='';};
      adjust.onclick=()=>choose(null,f.placement.value);
      f.placement.onchange=async()=>{const next=f.placement.value;if(item||source){if(!await choose(null,next))f.placement.value=format;}else format=next;fit()};
      secondary.onclick = async () => {
        if (!item) {
          form.remove();
          add.disabled = false;
          return;
        }
        if (
          !root.confirm(
            "永久删除这条广告？如果只是暂时停播，请取消并选择「暂停展示」。",
          )
        )
          return;
        secondary.disabled = save.disabled = true;
        try {
          await writeAd(admin, endpoint + "/" + item.id, "DELETE");
          form.remove();
          emptyState();
        } catch (e) {
          msg.textContent = e.message;
        } finally {
          secondary.disabled = save.disabled = false;
        }
      };
      form.onsubmit = async (e) => {
        e.preventDefault();
        if (save.disabled || pending) return;
        if (f.placement.value!==format || (!item && !blob)) {msg.textContent="请先选择海报并确认显示范围。";return;}
        const chosen = blob;
        save.disabled =
          secondary.disabled =
          f.image.disabled =
          f.link.disabled =
          f.enabled.disabled =
          f.placement.disabled =
          f.note.disabled =
            true;
        try {
          const url = new URL(f.link.value);
          if (
            !["http:", "https:"].includes(url.protocol) ||
            url.username ||
            url.password
          )
            throw Error("请填写有效的 HTTP 或 HTTPS 网址。");
          const body = new FormData();
          body.append("link", url.href);
          body.append("enabled", f.enabled.value);
          body.append("placement", f.placement.value);
          body.append("note", f.note.value.trim());
          if (chosen) body.append("image", chosen, "banner.jpg");
          else if (!item) throw Error("请选择横版图片。");
          if (item) body.append("revision", item.revision);
          const saved = await writeAd(
            admin,
            item ? endpoint + "/" + item.id : endpoint,
            item ? "PUT" : "POST",
            body,
          );
          const updated = {
            id: saved.id,
            revision: saved.revision,
            link: url.href,
            placement: f.placement.value,
            enabled: f.enabled.value === "true",
            note: f.note.value.trim(),
          };
          const next = editor(updated);
          if (item) form.replaceWith(next);
          else {
            form.remove();
            cards.append(next);
            add.disabled = false;
          }
          status.textContent = item ? "修改已保存。" : "广告已发布。";
        } catch (e) {
          msg.textContent =
            e.status === 409
              ? "这条广告已被修改或下架，请刷新后再编辑。"
              : e.message;
        } finally {
          save.disabled =
            secondary.disabled =
            f.image.disabled =
            f.link.disabled =
            f.enabled.disabled =
            f.placement.disabled =
            f.note.disabled =
              false;
        }
      };
      return form;
    }
    add.onclick = () => {
      add.disabled = true;
      const form = editor(null);
      create.replaceChildren(form);
      form.elements.image.focus();
    };
    async function load() {
      add.disabled = true;
      status.textContent = "正在加载广告…";
      try {
        const rows = await admin.request(endpoint);
        cards.replaceChildren(...rows.map(editor));
        emptyState();
      } catch (e) {
        status.textContent = "广告列表加载失败：" + e.message;
        const retry = document.createElement("button");
        retry.type = "button";
        retry.className = "btn small";
        retry.textContent = "重试";
        retry.onclick = load;
        status.append(retry);
      } finally {
        add.disabled = false;
      }
    }
    load();
  }
  root.MetarAds = { managerHTML, mountSlots, mountManager };
})(window);
