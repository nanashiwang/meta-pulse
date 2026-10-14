(function (root) {
  "use strict";
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
  const t = (zh, en) => (root.MetarI18n.getLanguage() === "en_US" ? en : zh);
  const stamp = (n) =>
    esc(new Date(n * 1000).toLocaleString(root.MetarI18n.locale()));
  const text = (v, fallback) => esc(v || fallback);
  class View {
    constructor(api, admin) {
      this.api = api;
      this.admin = admin;
      this.pending = null;
      this.generation = 0;
    }
    dispose() {
      this.generation++;
      this.root = null;
    }
    async page(path, user, query) {
      this.dispose();
      this.path = path;
      this.user = user;
      this.pageNo = Number(query.get("page") || 1);
      this.past = query.get("past") === "1";
      this.pending = null;
      this.pendingKey = user ? `metar-event-pending-v1:${user.id}` : null;
      if (this.pendingKey && path !== "/me/drafts") {
        try {
          const p = JSON.parse(
            root.sessionStorage.getItem(this.pendingKey) || "null",
          );
          if (
            p &&
            typeof p.key === "string" &&
            ["create", "edit", "cancel", "join", "leave"].includes(
              p.command?.op,
            )
          )
            this.pending = p;
        } catch (_) {}
      }
      if (path === "/me/drafts") return this.drafts();
      const result = await this.api.request(
        `/metar/events${user ? "/mine" : ""}?page=${this.pageNo}${this.past ? "&past=1" : ""}`,
      );
      this.events = result.items || [];
      this.more = result.more;
      return this.eventsHTML();
    }
    drafts() {
      try {
        this.library = root.MetarDrafts.create(
          root.localStorage,
          String(this.user.id),
        );
        this.items = this.library.list();
      } catch (_) {
        return `<section class="card card-pad" role="alert">${t("浏览器存储不可用，无法读取草稿。", "Browser storage is unavailable.")}</section>`;
      }
      return `<div class="between wrap"><h1 class="member-title">${t("我的草稿", "My drafts")}</h1><a class="btn primary" href="/questions/ask?metar_draft=new">${t("新建草稿", "New draft")}</a></div><p class="prod-page-note">${t("仅保存在当前浏览器，按社区账号隔离，最多 30 份。清理浏览器数据会删除草稿，请自行备份重要正文。", "Saved only in this browser, separately for each account, up to 30 drafts. Clearing browser data removes them; back up important text.")}</p>${this.library.legacy().length ? `<section class="card card-pad"><p>${t("此浏览器存在旧版未归属草稿；仅在确认属于自己时导入。", "This browser has legacy drafts without an owner. Import only if they are yours.")}</p><button class="btn" data-member="import">${t("确认属于我并导入", "Confirm ownership and import")}</button></section>` : ""}<div class="stack mt16">${this.items.length ? this.items.map((v) => `<article class="card card-pad"><div class="between wrap"><h2>${text(v.data.title, v.kind === "answer" ? t("回答草稿", "Answer draft") : t("未命名话题", "Untitled topic"))}</h2><small>${esc(new Date(v.updated).toLocaleString())}</small></div><p class="muted member-preview">${esc(v.data.content.slice(0, 160))}</p><div class="flex wrap"><a class="btn primary" href="${root.MetarDrafts.destination(v)}">${t("继续编辑", "Continue editing")}</a><button class="btn ghost" data-member="delete" data-id="${v.id}">${t("删除", "Delete")}</button></div></article>`).join("") : `<section class="card card-pad">${t("还没有草稿。在发布页编辑后会自动保存到这里。", "No drafts yet. Writing in the composer saves here automatically.")}</section>`}</div><p role="status" data-member-status></p>`;
    }
    eventsHTML() {
      const admin = this.path === "/admin/events",
        now = Date.now() / 1000;
      const form = admin
        ? `<details class="card card-pad" data-event-editor><summary>${t("发布或编辑活动", "Publish or edit event")}</summary><form data-event-form class="member-event-form"><input type="hidden" name="id"><input type="hidden" name="version" value="0"><label>${t("活动标题", "Title")}<input name="title" maxlength="100" required></label><label>${t("活动介绍与报名说明", "Description and registration details")}<textarea name="body" rows="5" maxlength="12000" required></textarea></label><label>${t("地点或线上参与方式", "Location or online participation")}<input name="location" maxlength="300"></label><div class="form-grid">${[
            ["starts_at", "开始时间", "Starts"],
            ["ends_at", "结束时间", "Ends"],
            ["deadline", "报名截止", "Registration deadline"],
          ]
            .map(
              ([key, zh, en]) =>
                `<label>${t(zh, en)}<input name="${key}" type="datetime-local" required></label>`,
            )
            .join(
              "",
            )}<label>${t("人数上限", "Capacity")}<input name="capacity" type="number" min="1" max="100000" value="100" required></label></div><p class="prod-page-note">${t("时间按当前设备时区填写。发布后公开；本活动不收费、不自动发放 Pulse 奖励。", "Times use your device timezone. Publishing is public; events are free and do not automatically grant Pulse rewards.")}</p><button class="btn primary" type="submit">${t("保存并发布", "Save and publish")}</button><button class="btn ghost" type="reset">${t("重置为新活动", "Reset to new event")}</button></form></details>`
        : "";
      return `<div class="between wrap"><h1 class="member-title">${t(admin ? "活动管理" : "近期活动", admin ? "Manage events" : "Community events")}</h1>${!admin && root.MetarAdapters.isCommunityAdministrator(this.user) ? '<a class="btn" data-router href="/admin/events">' + t("管理活动", "Manage events") + "</a>" : ""}</div><nav class="flex mt16"><a class="btn ${!this.past ? "primary" : ""}" data-router href="${this.path}">${t("近期", "Upcoming")}</a><a class="btn ${this.past ? "primary" : ""}" data-router href="${this.path}?past=1">${t("往期", "Past")}</a></nav>${form}<p role="status" data-member-status>${this.pending ? t("有一个操作尚未确认，请先重试原操作。", "An earlier operation is unconfirmed. Retry it first.") + ` <button class="btn" data-member="retry">${t("重试原操作", "Retry original operation")}</button>` : ""}</p><div class="stack mt16">${
        this.events.length
          ? this.events
              .map((v) => {
                const closed = v.status === "cancelled",
                  deadline = now >= v.deadline;
                return `<article class="card card-pad"><div class="between wrap"><h2>${esc(v.title)}</h2><span class="badge">${closed ? t("已取消", "Cancelled") : now >= v.ends_at ? t("已结束", "Ended") : now >= v.starts_at ? t("进行中", "In progress") : t("即将开始", "Upcoming")}</span></div><p class="member-preview">${esc(v.body)}</p><dl class="info-pairs"><dt>${t("活动时间", "Schedule")}</dt><dd>${stamp(v.starts_at)} — ${stamp(v.ends_at)}</dd><dt>${t("报名截止", "Deadline")}</dt><dd>${stamp(v.deadline)}</dd><dt>${t("参与方式", "Location")}</dt><dd>${text(v.location, t("详见活动说明", "See description"))}</dd><dt>${t("报名人数", "Registrations")}</dt><dd>${v.registered_count} / ${v.capacity}</dd></dl><div class="flex wrap mt16">${admin ? `<button class="btn" data-member="edit" data-id="${v.id}" ${closed || now >= v.starts_at ? "disabled" : ""}>${t("编辑", "Edit")}</button><button class="btn" data-member="roster" data-id="${v.id}">${t("报名名单", "Registration list")}</button><button class="btn ghost" data-member="cancel" data-id="${v.id}" ${closed ? "disabled" : ""}>${t("取消活动", "Cancel event")}</button>` : this.user ? `<button class="btn ${v.joined ? "" : "primary"}" data-member="${v.joined ? "leave" : "join"}" data-id="${v.id}" ${!v.joined && (closed || deadline || v.registered_count >= v.capacity) ? "disabled" : ""}>${v.joined ? t("已报名 · 取消报名", "Registered · Withdraw") : closed ? t("活动已取消", "Event cancelled") : deadline ? t("报名已截止", "Registration closed") : v.registered_count >= v.capacity ? t("名额已满", "Full") : t("报名参加", "Register")}</button>` : `<a class="btn" href="/users/login">${t("登录后报名", "Sign in to register")}</a>`}</div></article>`;
              })
              .join("")
          : `<section class="card card-pad">${t(this.past ? "暂无往期活动。" : "暂时没有已发布的活动。", this.past ? "No past events." : "No published events yet.")}</section>`
      }</div><nav class="flex mt16">${this.pageNo > 1 ? `<a class="btn" data-router href="${this.path}?page=${this.pageNo - 1}${this.past ? "&past=1" : ""}">${t("上一页", "Previous")}</a>` : ""}${this.more ? `<a class="btn" data-router href="${this.path}?page=${this.pageNo + 1}${this.past ? "&past=1" : ""}">${t("下一页", "Next")}</a>` : ""}</nav><dialog data-event-roster><h2>${t("报名名单", "Registration list")}</h2><div data-roster-list></div><div class="flex"><button class="btn" data-member="roster-prev">${t("上一页", "Previous")}</button><button class="btn" data-member="roster-next">${t("下一页", "Next")}</button><button class="btn" data-member="close">${t("关闭", "Close")}</button></div></dialog>`;
    }
    mount(node) {
      this.root = node;
      node.onclick = (e) => {
        const b = e.target.closest("[data-member]");
        if (b)
          this.action(b.dataset.member, b.dataset.id).catch((err) =>
            this.error(err),
          );
      };
      this.mountFormOnly(node);
    }
    error(e) {
      const n = this.root?.querySelector("[data-member-status]");
      if (!n) return;
      const code = e?.details?.error || e?.code;
      const messages = {
        event_full: t("名额已满。", "This event is full."),
        event_closed: t(
          "活动已取消或报名已截止。",
          "Event cancelled or registration closed.",
        ),
        conflict: t(
          "内容已变化，请刷新后再编辑。",
          "Content changed. Refresh before editing.",
        ),
        forbidden: t(
          "账号或权限已变化，请重新登录核对。",
          "Account or permissions changed. Sign in again.",
        ),
        invalid_request: t(
          "请检查内容、时间顺序和人数上限。",
          "Check the content, time order and capacity.",
        ),
      };
      n.textContent =
        messages[code] ||
        (e?.message === "draft_conflict"
          ? t(
              "草稿已在其他页面修改，请刷新后再操作。",
              "Draft changed in another tab. Refresh before continuing.",
            )
          : e?.message === "draft_limit"
            ? t(
                "最多保存 30 份草稿，请先清理。",
                "At most 30 drafts can be stored. Delete older drafts first.",
              )
            : null) ||
        t(
          "操作未确认，请重试原操作。",
          "Operation not confirmed. Retry the original request.",
        );
      if (e?.message === "refresh_required") {
        n.textContent = t("操作已完成，列表刷新失败。请刷新列表后继续。", "Operation completed, but the list could not refresh. Reload it before continuing.");
        const b = document.createElement("button"); b.className="btn"; b.dataset.member="refresh"; b.textContent=t("刷新列表", "Reload list"); n.append(b);
      }
      if (this.pending) {
        const b = document.createElement("button");
        b.className = "btn";
        b.dataset.member = "retry";
        b.textContent = t("重试原操作", "Retry original operation");
        n.append(b);
      }
    }
    async refresh() {
      const gen = this.generation;
      const result = await this.api.request(
        `/metar/events${this.user ? "/mine" : ""}?page=${this.pageNo}${this.past ? "&past=1" : ""}`,
      );
      if (gen !== this.generation || !this.root) return;
      this.needsRefresh = false;
      this.events = result.items;
      this.more = result.more;
      const node = this.root;
      node.innerHTML = this.eventsHTML();
      this.mountFormOnly(node);
    }
    mountFormOnly(node) {
      node
        .querySelector("[data-event-form]")
        ?.addEventListener("submit", (e) => {
          e.preventDefault();
          const f = new FormData(e.target),
            event = {};
          for (const k of ["title", "body", "location", "id"])
            event[k] = f.get(k);
          for (const k of ["starts_at", "ends_at", "deadline"])
            event[k] = Math.floor(new Date(f.get(k)).getTime() / 1000);
          event.capacity = Number(f.get("capacity"));
          event.version = Number(f.get("version"));
          this.command({ op: event.id ? "edit" : "create", event }, true).catch(
            (err) => this.error(err),
          );
        });
    }
    async command(command, admin) {
      if (this.busy) return;
      if (this.needsRefresh) throw Error("refresh_required");
      if (this.pending && command !== this.pending.command)
        throw Error("pending");
      this.pending ||= { command, admin, key: crypto.randomUUID() };
      root.sessionStorage.setItem(
        this.pendingKey,
        JSON.stringify(this.pending),
      );
      const p = this.pending,
        gen = this.generation;
      this.busy = true;
      let committed = false;
      try {
        await (p.admin ? this.admin : this.api).request(
          "/metar/events/command",
          {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "X-Metar-Request": "1",
              "Idempotency-Key": p.key,
            },
            body: JSON.stringify(p.command),
          },
        );
        committed = true;
        this.needsRefresh = true;
        root.sessionStorage.removeItem(this.pendingKey);
        this.pending = null;
        if (gen === this.generation) await this.refresh();
      } catch (e) {
        if (committed) throw Error("refresh_required");
        if (e?.status >= 400 && e?.status < 500) {
          root.sessionStorage.removeItem(this.pendingKey);
          this.pending = null;
        }
        throw e;
      } finally {
        this.busy = false;
      }
    }
    async action(op, id) {
      if (op === "refresh") { await this.refresh(); return; }
      if (op === "delete") {
        const v = this.items.find((v) => v.id === id);
        if (
          v &&
          confirm(
            t(
              "删除这份草稿？此操作无法撤销。",
              "Delete this draft? This cannot be undone.",
            ),
          )
        ) {
          this.library.remove(id, v.revision);
          this.root.innerHTML = this.drafts();
        }
        return;
      }
      if (op === "import") {
        if (
          confirm(
            t(
              "确认这些旧草稿属于你，并导入当前账号？",
              "Confirm these legacy drafts are yours and import them?",
            ),
          )
        ) {
          this.library.importLegacy();
          this.root.innerHTML = this.drafts();
        }
        return;
      }
      if (op === "close") {
        this.root.querySelector("dialog").close();
        return;
      }
      if (op === "retry") {
        if (this.pending)
          await this.command(this.pending.command, this.pending.admin);
        return;
      }
      if (op === "roster" || op === "roster-next" || op === "roster-prev") {
        this.rosterID = id || this.rosterID;
        this.rosterPage =
          op === "roster"
            ? 1
            : Math.max(1, this.rosterPage + (op === "roster-next" ? 1 : -1));
        const gen = this.generation;
        const people = await this.admin.request(
          `/metar/events/roster?id=${this.rosterID}&page=${this.rosterPage}`,
        );
        if (gen !== this.generation || !this.root) return;
        const d = this.root.querySelector("dialog");
        d.querySelector("[data-roster-list]").innerHTML = people.length
          ? people
              .map(
                (p) =>
                  `<p>${esc(p.display_name)} (@${esc(p.username)}) · ${stamp(p.joined_at)}</p>`,
              )
              .join("")
          : t("暂无报名。", "No registrations.");
        d.querySelector('[data-member="roster-prev"]').disabled =
          this.rosterPage === 1;
        d.querySelector('[data-member="roster-next"]').disabled =
          people.length < 100;
        if (!d.open) d.showModal();
        return;
      }
      const v = this.events.find((v) => v.id === id);
      if (!v) return;
      if (op === "edit") {
        const d = this.root.querySelector("[data-event-editor]");
        d.open = true;
        const f = d.querySelector("form");
        for (const k of [
          "id",
          "version",
          "title",
          "body",
          "location",
          "capacity",
        ])
          f.elements[k].value = v[k];
        for (const k of ["starts_at", "ends_at", "deadline"]) {
          const dt = new Date(v[k] * 1000);
          f.elements[k].value = new Date(dt - dt.getTimezoneOffset() * 60000)
            .toISOString()
            .slice(0, 16);
        }
        f.scrollIntoView();
        return;
      }
      if (
        op === "cancel" &&
        !confirm(
          t(
            "取消活动并关闭报名？已报名成员将看到取消状态。",
            "Cancel the event and close registration? Members will see its cancelled status.",
          ),
        )
      )
        return;
      if (
        op === "leave" &&
        !confirm(
          t(
            "取消本次报名？释放的名额可能被其他人报名。",
            "Withdraw? Another member may take the released place.",
          ),
        )
      )
        return;
      await this.command(
        { op, event: { id, version: v.version } },
        op === "cancel",
      );
    }
  }
  root.MetarMemberPages = { View };
})(window);
