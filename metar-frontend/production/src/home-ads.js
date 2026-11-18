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
    let index = 0,
      timer,
      closed = false;
    const stop = () => clearTimeout(timer);
    function show() {
      stop();
      if (closed || !slot.isConnected || !items.length) return;
      const item = items[index],
        image = document.createElement("img");
      image.alt = "访问广告网站";
      image.onload = () => {
        if (closed || !slot.isConnected) return;
        link.replaceChildren(image);
        link.href = item.link;
        slot.hidden = false;
        if (items.length > 1) {
          const advance = () => {
            if (closed || !slot.isConnected) return;
            if (document.hidden) {
              timer = setTimeout(advance, 15000);
              return;
            }
            index = (index + 1) % items.length;
            show();
          };
          timer = setTimeout(advance, 15000);
        }
      };
      image.onerror = () => {
        if (closed || !slot.isConnected) return;
        items.splice(index, 1);
        if (!items.length) {
          slot.hidden = true;
          return;
        }
        index %= items.length;
        show();
      };
      image.src = imageURL(item, base);
    }
    close.onclick = () => {
      closed = true;
      stop();
      slot.hidden = true;
      slot.replaceChildren();
    };
    slot.replaceChildren(link, badge, close);
    show();
  }
  const managerHTML = () =>
    `<section data-ad-manager><header class="ad-manager-heading"><div><h1>广告管理</h1><p class="muted">顶部和侧边可同时展示，各位置的广告按发布顺序每 15 秒轮播</p></div><button type="button" class="btn primary" data-new>＋ 新增广告</button></header><div data-create></div><p role="status"></p><section aria-label="广告列表"><h2>广告列表</h2><div data-cards></div></section></section>`;
  async function picture(file) {
    if (!file || file.size > 8 * 1024 * 1024)
      throw Error("请选择不超过 8 MB 的横版图片。");
    const img = await createImageBitmap(file);
    try {
      if (img.width <= img.height) throw Error("请上传宽度大于高度的图片。");
      const canvas = document.createElement("canvas");
      canvas.width = Math.min(img.width, 1280);
      canvas.height = Math.max(
        1,
        Math.round((img.height * canvas.width) / img.width),
      );
      const ctx = canvas.getContext("2d");
      ctx.fillStyle = "white";
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
      const blob = await new Promise((resolve) =>
        canvas.toBlob(resolve, "image/jpeg", 0.82),
      );
      if (!blob || blob.size > 880 * 1024)
        throw Error("图片压缩后仍过大，请缩小后重试。");
      return blob;
    } finally {
      img.close();
    }
  }
  const fields = `<header class="ad-card-head"><h3></h3><span class="ad-state"></span></header>
 <div class="ad-card-body"><div><div class="ad-preview"><img alt="广告图片预览"><span data-placeholder>请选择横版图片</span></div><button type="button" data-retry class="btn small" hidden>重新加载预览</button><label>广告海报（选择新图可替换）<input name="image" type="file" accept="image/jpeg,image/png,image/webp"></label><small class="muted">JPG、PNG 或 WebP，最大 8 MB</small></div>
 <div class="ad-fields"><label>展示状态<select name="enabled"><option value="true">展示中</option><option value="false">暂停展示（保留海报和设置）</option></select></label><label>展示位置<select name="placement"><option value="side">侧边广告 · 4:3</option><option value="top">顶部横幅 · 4:1</option></select></label><label>跳转网址<input name="link" type="url" placeholder="https://" required></label><label>后台备注<textarea name="note" maxlength="300" rows="2" placeholder="例如：合作方名称、投放周期（选填）"></textarea></label><small class="muted">备注仅管理员可见，不会展示在首页。</small></div></div>
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
      f.placement.onchange = fit;
      fit();
      f.link.value = item?.link || "";
      f.enabled.value = String(item?.enabled !== false);
      f.note.value = item?.note || "";
      f.image.required = !item;
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
      f.image.onchange = async () => {
        const token = ++seq;
        blob = null;
        msg.textContent = "";
        const file = f.image.files[0];
        if (!file) {
          pending = false;
          save.disabled = false;
          original();
          return;
        }
        pending = true;
        save.disabled = true;
        try {
          const converted = await picture(file);
          if (seq !== token) return;
          const reader = new FileReader();
          const data = await new Promise((resolve, reject) => {
            reader.onload = () => resolve(reader.result);
            reader.onerror = reject;
            reader.readAsDataURL(converted);
          });
          if (seq !== token) return;
          blob = converted;
          painting(data, false);
        } catch (e) {
          if (seq === token) {
            msg.textContent = e.message || "图片无法读取。";
            f.image.value = "";
            original();
          }
        } finally {
          if (f.image.files[0] === file || !f.image.files.length) {
            pending = false;
            save.disabled = false;
          }
        }
      };
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
