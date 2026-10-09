/* Private text chat. Messages and drafts stay in memory; Answer owns identity. */
(function (root, factory) {
  if (typeof module === "object" && module.exports) module.exports = factory();
  else root.MetarChat = factory();
})(typeof window === "undefined" ? globalThis : window, () => {
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
  const t = (zh, en) =>
    typeof window !== "undefined" && window.MetarI18n?.getLanguage() === "en_US"
      ? en
      : zh;
  const name = (p) => p?.display_name || p?.username || t("社区成员", "Member");
  const title = (r) => (r.kind === "direct" ? name(r.peer) : r.title);
  const id = () => crypto.randomUUID();
  function mergeMessages(current, incoming) {
    const map = new Map(current.map((m) => [m.seq, m]));
    for (const m of incoming) {
      if (
        !map.has(m.seq) ||
        Number(m.version) >= Number(map.get(m.seq).version)
      )
        map.set(m.seq, m);
    }
    return [...map.values()].sort((a, b) => a.seq - b.seq);
  }
  function messageHTML(m, user) {
    return `<article class="chat-message ${m.sender.id === user ? "mine" : ""}" data-message="${m.seq}"><div class="chat-message-meta"><strong>${esc(name(m.sender))}</strong><time>${esc(new Date(m.created_at * 1000).toLocaleString())}</time></div><div class="chat-bubble ${m.removed ? "removed" : ""}">${m.removed ? t("消息已撤回或移除", "Message recalled or removed") : esc(m.body)}</div>${!m.removed ? `<div class="chat-message-actions">${m.sender.id === user && Date.now() / 1000 - m.created_at < 120 ? `<button type="button" data-chat="recall" data-seq="${m.seq}">${t("撤回", "Recall")}</button>` : m.sender.id !== user ? `<button type="button" data-chat="report" data-seq="${m.seq}">${t("举报", "Report")}</button>` : ""}</div>` : ""}</article>`;
  }
  function errorText(e) {
    const code = e?.details?.error || e?.code;
    return (
      {
        forbidden: t(
          "你已不在此会话中，或没有操作权限。",
          "You are not a member or lack permission.",
        ),
        account_unavailable: t(
          "账号暂不可用，请重新登录并检查激活状态。",
          "Account unavailable. Sign in and check activation.",
        ),
        contact_unavailable: t(
          "暂时无法联系此成员。",
          "This member cannot be contacted.",
        ),
        rate_limited: t(
          "操作过于频繁或已达聊天限额，请稍后重试。",
          "Chat limit reached. Please try again later.",
        ),
        conflict: t(
          "状态已变化，请刷新后重试；群主退出前请先转让或解散群组。",
          "State changed. Reload; owners must transfer ownership or close the group before leaving.",
        ),
        invalid_request: t(
          "请检查内容、群名称或所选成员。",
          "Check the message, group name or selected members.",
        ),
        unauthorized: t(
          "登录已过期，请重新登录。",
          "Your session expired. Sign in again.",
        ),
      }[code] ||
      t(
        "连接暂时中断，消息未确认。可使用原请求重试。",
        "Connection interrupted; delivery is unconfirmed. Retry the original request.",
      )
    );
  }
  class Client {
    constructor(api, admin) {
      this.api = api;
      this.admin = admin;
    }
    get(op, params = {}) {
      const query = new URLSearchParams(params);
      return this.api.request(
        "/metar/chat/" + op + (query.size ? "?" + query : ""),
      );
    }
    command(command, key = id(), moderation = false) {
      return (moderation ? this.admin : this.api).request(
        "/metar/chat/" + (moderation ? "reports" : "command"),
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Metar-Request": "1",
            "Idempotency-Key": key,
          },
          body: JSON.stringify(command),
        },
      );
    }
    reports(before = "") {
      return this.admin.request(
        "/metar/chat/reports" +
          (before ? "?before=" + encodeURIComponent(before) : ""),
      );
    }
  }
  class View {
    constructor(api, admin, user) {
      this.client = new Client(api, admin);
      this.user = String(user.id);
      this.rooms = [];
      this.messages = [];
      this.drafts = new Map();
      this.pending = new Map();
      this.epoch = 0;
      this.selection = 0;
      this.failures = 0;
      this.busy = false;
    }
    shell() {
      return `<section class="chat-workspace" data-chat-root><div class="chat-toolbar"><h1>${t("聊天", "Chat")}</h1><div class="flex wrap"><button class="btn small" type="button" data-chat="new-direct">${t("发起私聊", "New message")}</button><button class="btn small" type="button" data-chat="new-group">${t("创建群组", "Create group")}</button><button class="btn ghost small" type="button" data-chat="blocks">${t("屏蔽名单", "Blocked members")}</button></div></div><p class="chat-notice" data-chat-notice role="status"></p><div class="chat-layout"><aside class="chat-conversations" aria-label="${t("会话列表", "Conversations")}"><div data-chat-list></div><button type="button" class="btn small" data-chat="more-rooms" hidden>${t("更多会话", "More conversations")}</button></aside><div class="chat-pane" data-chat-pane><div class="chat-welcome"><span class="chat-welcome-icon" aria-hidden="true">☏</span><h2>${t("聊一聊，让交流继续", "Keep the conversation going")}</h2><p>${t("选择会话，或发起私聊和创建群组。", "Choose a conversation, start a private message or create a group.")}</p><p class="muted">${t("群邀请需对方确认。文字消息仅会话成员可见；举报时管理员可查看被举报消息。", "Group invitations require acceptance. Messages are visible to members; moderators can review reported messages.")}</p></div></div></div><dialog class="chat-dialog" data-chat-dialog aria-labelledby="chat-dialog-title"></dialog></section>`;
    }
    mount(root, requested = "") {
      this.root = root;
      this.room = null;
      this.messages = [];
      this.cursor = 0;
      this.polling = false;
      this.reading = false;
      this.busy = false;
      this.epoch++;
      this.disposed = false;
      this.clickHandler = (e) => this.click(e);
      this.submitHandler = (e) => this.submit(e);
      this.inputHandler = (e) => {
        if (e.target.matches('[name="message"]')) {
          this.drafts.set(this.room.id, e.target.value);
          this.updateCounter();
        }
      };
      root.addEventListener("click", this.clickHandler);
      root.addEventListener("submit", this.submitHandler);
      root.addEventListener("input", this.inputHandler);
      this.keyHandler = (e) => {
        if (
          e.target.matches('[name="message"]') &&
          e.key === "Enter" &&
          (e.ctrlKey || e.metaKey) &&
          !e.isComposing
        ) {
          e.preventDefault();
          this.send();
        }
      };
      root.addEventListener("keydown", this.keyHandler);
      this.visibility = () => {
        clearTimeout(this.timer);
        if (!document.hidden) this.tick();
      };
      document.addEventListener("visibilitychange", this.visibility);
      this.unload = (e) => {
        if (
          [...this.drafts.values()].some((v) => v.trim()) ||
          this.pending.size
        ) {
          e.preventDefault();
          e.returnValue = "";
        }
      };
      window.addEventListener("beforeunload", this.unload);
      this.initial(requested);
    }
    valid(epoch = this.epoch) {
      return !this.disposed && this.root?.isConnected && epoch === this.epoch;
    }
    dispose() {
      this.disposed = true;
      this.epoch++;
      this.selection++;
      clearTimeout(this.timer);
      if (this.root) {
        this.root.removeEventListener("click", this.clickHandler);
        this.root.removeEventListener("submit", this.submitHandler);
        this.root.removeEventListener("input", this.inputHandler);
        this.root.removeEventListener("keydown", this.keyHandler);
        this.root.querySelector("dialog")?.close();
      }
      document.removeEventListener("visibilitychange", this.visibility);
      window.removeEventListener("beforeunload", this.unload);
    }
    notice(text) {
      if (this.valid())
        this.root.querySelector("[data-chat-notice]").textContent = text;
    }
    async initial(requested) {
      const epoch = this.epoch;
      try {
        await this.inbox();
        if (!this.valid(epoch)) return;
        if (requested) await this.open(requested);
        else this.notice("");
      } catch (e) {
        this.notice(errorText(e));
      } finally {
        if (this.valid(epoch)) this.schedule();
      }
    }
    schedule() {
      clearTimeout(this.timer);
      if (this.valid() && !document.hidden)
        this.timer = setTimeout(
          () => this.tick(),
          Math.min(30000, 3000 * 2 ** this.failures),
        );
    }
    async inbox(before = "") {
      const epoch = this.epoch;
      const data = await this.client.get("inbox", before ? { before } : {});
      if (!this.valid(epoch)) return;
      if (!before && data.next) {
        const rest = await this.client.get("inbox", { before: data.next });
        if (!this.valid(epoch)) return;
        data.rooms.push(...rest.rooms);
        data.next = rest.next;
      }
      this.rooms = before
        ? [
            ...this.rooms,
            ...data.rooms.filter((r) => !this.rooms.some((v) => v.id === r.id)),
          ]
        : data.rooms;
      this.next = data.next;
      this.list();
      this.badge(data);
    }
    badge(data) {
      document.querySelectorAll("[data-chat-unread]").forEach((n) => {
        const count = Number(data.unread) + Number(data.invites);
        n.textContent = count > 99 ? "99+" : String(count);
        n.hidden = !count;
      });
    }
    list() {
      const node = this.root.querySelector("[data-chat-list]");
      const rooms = [...this.rooms].sort(
        (a, b) => Number(b.updated_at) - Number(a.updated_at),
      );
      const html = rooms.length
        ? rooms
            .map(
              (r) =>
                `<div class="chat-room ${this.room?.id === r.id ? "active" : ""}"><button class="chat-room-open" type="button" data-chat="open" data-room="${esc(r.id)}"><span class="chat-room-avatar" aria-hidden="true">${r.kind === "group" ? "#" : esc(title(r).slice(0, 1))}</span><span class="chat-room-copy"><strong>${esc(title(r))}</strong><small>${r.state === "invited" ? t("邀请你加入群组", "Group invitation") : r.closed ? t("群组已解散", "Group closed") : r.kind === "group" ? t("群聊", "Group") : t("私聊", "Private message")}</small></span>${r.unread ? `<span class="chat-count">${r.unread > 99 ? "99+" : r.unread}</span>` : ""}</button>${r.state === "invited" ? `<div class="chat-invite-actions"><button class="btn small primary" data-chat="accept" data-room="${esc(r.id)}">${t("接受", "Accept")}</button><button class="btn small" data-chat="decline" data-room="${esc(r.id)}">${t("拒绝", "Decline")}</button></div>` : ""}</div>`,
            )
            .join("")
        : `<p class="chat-empty">${t("还没有会话，从右上角发起交流。", "No conversations yet. Start one above.")}</p>`;
      if (node.innerHTML !== html) node.innerHTML = html;
      this.root.querySelector('[data-chat="more-rooms"]').hidden = !this.next;
    }
    async open(roomID) {
      if (!/^\d+$/.test(roomID)) return;
      const listed = this.rooms.find((r) => r.id === roomID);
      if (listed?.state === "invited") {
        this.notice(t("请先接受群邀请。", "Accept the invitation first."));
        return;
      }
      const selection = ++this.selection,
        epoch = this.epoch;
      this.notice(t("正在读取消息…", "Loading messages…"));
      try {
        const data = await this.client.get("messages", { room_id: roomID });
        if (!this.valid(epoch) || selection !== this.selection) return;
        this.room = data.room;
        this.messages = data.messages;
        this.cursor = data.cursor;
        this.more = data.has_more;
        this.renderPane();
        this.list();
        this.notice("");
        history.replaceState(
          history.state,
          "",
          "/chat?room=" + encodeURIComponent(roomID),
        );
        await this.markRead();
      } catch (e) {
        if (this.valid(epoch) && selection === this.selection) {
          this.notice(errorText(e));
          this.room = null;
          this.root.querySelector("[data-chat-pane]").innerHTML =
            `<p class="chat-empty">${esc(errorText(e))}</p>`;
        }
      }
    }
    renderPane() {
      const r = this.room;
      this.root.classList.add("chat-has-room");
      const pane = this.root.querySelector("[data-chat-pane]");
      pane.innerHTML = `<div class="chat-room-header"><button class="btn ghost small chat-back" type="button" data-chat="back" aria-label="${t("返回会话列表", "Back to conversations")}">‹</button><div class="chat-heading"><h2 data-chat-title>${esc(title(r))}</h2><small data-chat-subtitle>${r.kind === "group" ? t("群聊", "Group") : t("私聊", "Private message")}</small></div><button class="btn small" type="button" data-chat="members">${r.kind === "group" ? t("群组管理", "Group details") : t("会话设置", "Conversation settings")}</button></div><div class="chat-message-scroll" tabindex="0" role="log" aria-label="${t("聊天记录", "Messages")}" aria-live="polite"><button class="btn small chat-older" type="button" data-chat="older" ${this.more ? "" : "hidden"}>${t("更早消息", "Earlier messages")}</button><div data-chat-messages></div></div><form data-chat-form="send" class="chat-composer"><label class="visually-hidden" for="chat-message">${t("输入消息", "Message")}</label><textarea id="chat-message" name="message" rows="2" maxlength="4000" placeholder="${t("输入消息，Ctrl / ⌘ + Enter 发送", "Write a message. Ctrl / ⌘ + Enter to send")}"></textarea><div class="chat-compose-actions"><small data-chat-counter>0 / 4000</small><button type="submit" class="btn primary" data-chat-send>${t("发送", "Send")}</button></div><p class="chat-send-state" data-chat-send-state role="status"></p></form>`;
      pane.querySelector("textarea").value = this.drafts.get(r.id) || "";
      this.paintMessages(true);
      this.updateCounter();
      this.updateComposer();
      pane
        .querySelector(".chat-message-scroll")
        .addEventListener("scroll", () => {
          if (this.atBottom()) this.markRead().catch(() => {});
        });
    }
    atBottom() {
      const list = this.root?.querySelector(".chat-message-scroll");
      return (
        list &&
        list.getClientRects().length > 0 &&
        list.scrollHeight - list.clientHeight - list.scrollTop < 48
      );
    }
    paintMessages(force = false, keepPosition = false) {
      const scroll = this.root.querySelector(".chat-message-scroll"),
        stick = !keepPosition && (force || this.atBottom());
      const node = this.root.querySelector("[data-chat-messages]");
      const html = this.messages.length
        ? this.messages.map((m) => messageHTML(m, this.user)).join("")
        : `<p class="chat-empty">${t("还没有消息，打个招呼吧。", "No messages yet. Say hello.")}</p>`;
      if (node.innerHTML !== html) node.innerHTML = html;
      this.root.querySelector('[data-chat="older"]').hidden = !this.more;
      if (stick) scroll.scrollTop = scroll.scrollHeight;
    }
    updateCounter() {
      const input = this.root.querySelector('[name="message"]');
      if (input)
        this.root.querySelector("[data-chat-counter]").textContent =
          `${[...input.value].length} / 4000`;
    }
    updateComposer() {
      if (!this.room) return;
      const pending = this.pending.get(this.room.id);
      const input = this.root.querySelector('[name="message"]'),
        button = this.root.querySelector("[data-chat-send]");
      if (!input) return;
      input.disabled = this.room.closed;
      input.readOnly = !!pending;
      button.disabled = this.room.closed || !!pending?.sending;
      button.textContent = pending
        ? pending.sending
          ? t("发送中…", "Sending…")
          : t("重试发送", "Retry send")
        : t("发送", "Send");
      this.root.querySelector("[data-chat-send-state]").textContent = this.room
        .closed
        ? t(
            "群组已解散，可继续查看历史消息。",
            "This group is closed. History remains available.",
          )
        : pending?.error
          ? errorText(pending.error)
          : "";
    }
    async markRead() {
      if (
        !this.room ||
        !this.valid() ||
        document.hidden ||
        !this.atBottom() ||
        this.reading
      )
        return;
      const room = this.room,
        seq = this.messages.at(-1)?.seq || 0;
      if (seq <= room.read_seq) return;
      this.reading = true;
      try {
        await this.client.command({ op: "read", room_id: room.id, seq });
        if (this.room === room) room.read_seq = Math.max(room.read_seq, seq);
      } finally {
        this.reading = false;
      }
    }
    async tick() {
      if (!this.valid() || document.hidden || this.polling) return;
      this.polling = true;
      const epoch = this.epoch,
        selection = this.selection;
      try {
        if (this.room) {
          const data = await this.client.get("messages", {
            room_id: this.room.id,
            version: String(this.cursor),
          });
          if (!this.valid(epoch) || selection !== this.selection) return;
          this.room = data.room;
          this.cursor = data.cursor;
          this.messages = mergeMessages(this.messages, data.messages);
          if (this.messages.length > 300 && this.atBottom()) {
            this.messages = this.messages.slice(-300);
            this.more = true;
          }
          this.paintMessages();
          this.root.querySelector("[data-chat-title]").textContent = title(
            this.room,
          );
          this.updateComposer();
          await this.markRead();
        }
        await this.inbox();
        if (this.valid(epoch)) {
          this.failures = 0;
          this.notice("");
        }
      } catch (e) {
        if (this.valid(epoch)) {
          this.failures = Math.min(4, this.failures + 1);
          this.notice(errorText(e));
          if (e.status === 401 || e.status === 403) {
            this.room = null;
            this.messages = [];
            this.root.querySelector("dialog")?.close();
            if (
              e.status === 401 ||
              e.details?.error === "account_unavailable"
            ) {
              this.rooms = [];
              this.next = "";
              this.drafts.clear();
              this.pending.clear();
              this.list();
              this.badge({ unread: 0, invites: 0 });
            }
            this.root.querySelector("[data-chat-pane]").innerHTML =
              `<p class="chat-empty">${esc(errorText(e))}</p>`;
          }
        }
      } finally {
        this.polling = false;
        if (this.valid(epoch)) this.schedule();
      }
    }
    async send() {
      if (!this.room || !this.valid()) return;
      const room = this.room,
        epoch = this.epoch;
      let pending = this.pending.get(room.id);
      if (pending?.sending) return;
      if (!pending) {
        const body = this.root.querySelector('[name="message"]').value.trim();
        if (!body) return;
        pending = {
          key: id(),
          command: { op: "send", room_id: room.id, body },
        };
        this.pending.set(room.id, pending);
      }
      pending.sending = true;
      pending.error = null;
      this.updateComposer();
      try {
        await this.client.command(pending.command, pending.key);
        this.pending.delete(room.id);
        this.drafts.delete(room.id);
        if (!this.valid(epoch)) return;
        if (this.room?.id === room.id) {
          this.root.querySelector('[name="message"]').value = "";
          this.updateCounter();
          await this.tick();
          this.root.querySelector('[name="message"]')?.focus();
        }
      } catch (e) {
        pending.error = e;
        if (e.status >= 400 && e.status < 500) {
          this.pending.delete(room.id);
          if (this.valid(epoch)) this.notice(errorText(e));
        }
      } finally {
        pending.sending = false;
        if (this.valid(epoch) && this.room?.id === room.id)
          this.updateComposer();
      }
    }
    dialog(heading, body) {
      const d = this.root.querySelector("dialog");
      d.innerHTML = `<div class="chat-dialog-header"><h2 id="chat-dialog-title">${esc(heading)}</h2><button class="icon-btn" type="button" data-chat="close-dialog" aria-label="${t("关闭", "Close")}">×</button></div>${body}<p class="chat-dialog-status" role="status"></p>`;
      if (!d.open) d.showModal();
      d.querySelector("input,textarea,button")?.focus();
    }
    newConversation(group = false, invite = false) {
      this.mode = invite ? "invite" : group ? "group" : "direct";
      this.selected = new Map();
      this.dialog(
        invite
          ? t("邀请成员", "Invite members")
          : group
            ? t("创建群组", "Create group")
            : t("发起私聊", "New message"),
        `${group ? `<label>${t("群名称", "Group name")}<input name="group-title" maxlength="80" required></label>` : ""}<form data-chat-form="people" class="chat-people-search"><label class="visually-hidden" for="chat-people">${t("按用户名查找", "Find by username")}</label><input id="chat-people" name="query" minlength="2" maxlength="50" required placeholder="${t("输入至少两个字符的用户名", "At least two characters of a username")}"><button class="btn" type="submit">${t("查找", "Find")}</button></form><div data-chat-selected></div><div data-chat-people></div>${group ? `<button type="button" class="btn primary" data-chat="create-group">${t("创建并发送邀请", "Create and invite")}</button>` : ""}<p class="muted chat-dialog-help">${t("群成员上限 50 人，受邀者确认后加入；新成员只能查看加入后的消息。", "Up to 50 members. Invitees join after accepting and see messages from that point.")}</p>`,
      );
    }
    async people(form) {
      const query = new FormData(form).get("query").trim();
      const d = this.root.querySelector("dialog"),
        epoch = this.epoch;
      const results = await this.client.get("people", { q: query });
      if (!this.valid(epoch) || !d.open) return;
      d.querySelector("[data-chat-people]").innerHTML = results.length
        ? results
            .map(
              (p) =>
                `<button type="button" class="chat-person" data-chat="select-person" data-person="${esc(p.id)}" data-name="${esc(name(p))}"><strong>${esc(name(p))}</strong><span>@${esc(p.username)}</span><span>${this.mode === "direct" ? t("私聊", "Message") : t("选择", "Select")}</span></button>`,
            )
            .join("")
        : `<p class="chat-empty">${t("没有找到可联系的成员。", "No available members found.")}</p>`;
    }
    members() {
      const r = this.room;
      if (!r) return;
      const owner = r.owner_id === this.user;
      this.dialog(
        r.kind === "direct"
          ? t("会话设置", "Conversation settings")
          : t("群组管理", "Group details"),
        r.kind === "direct"
          ? `<p>${esc(title(r))}</p><button class="btn" type="button" data-chat="block" data-person="${esc(r.peer.id)}">${t("屏蔽此成员", "Block member")}</button><p class="muted">${t("屏蔽后双方不能继续私聊或互发群邀请；已共同加入的群组不受影响。", "Blocks stop private messages and invitations in both directions. Existing shared groups are unaffected.")}</p>`
          : `${owner && !r.closed ? `<form data-chat-form="rename"><label>${t("群名称", "Group name")}<input name="title" value="${esc(r.title)}" maxlength="80" required></label><button class="btn small">${t("保存群名称", "Save group name")}</button></form><button class="btn small" type="button" data-chat="invite">${t("邀请成员", "Invite members")}</button>` : ""}<ul class="chat-member-list">${r.members.map((m) => `<li><span><strong>${esc(name(m))}</strong><small>@${esc(m.username)} ${m.id === r.owner_id ? t("群主", "Owner") : m.state === "invited" ? t("待接受", "Invited") : ""}</small></span>${owner && !r.closed && m.id !== this.user ? `<span><button class="btn small" data-chat="confirm-remove" data-person="${esc(m.id)}">${t("移出", "Remove")}</button>${m.state === "active" ? `<button class="btn small" data-chat="confirm-transfer" data-person="${esc(m.id)}">${t("转让", "Make owner")}</button>` : ""}</span>` : ""}</li>`).join("")}</ul>${!r.closed ? `<button class="btn" type="button" data-chat="${owner ? "confirm-close" : "confirm-leave"}">${owner ? t("解散群组", "Close group") : t("退出群组", "Leave group")}</button>` : ""}`,
      );
    }
    confirm(op, target = "") {
      this.confirmCommand = {
        op,
        room_id: this.room.id,
        ...(target ? { target } : {}),
      };
      const labels = {
        remove: t("移出成员", "Remove member"),
        transfer: t("转让群主", "Transfer ownership"),
        close: t("解散群组", "Close group"),
        leave: t("退出群组", "Leave group"),
      };
      this.dialog(
        labels[op],
        `<p>${op === "close" ? t("解散后不能再发消息，现有成员仍可查看历史记录。", "Members can still read history, but no one can send new messages.") : op === "transfer" ? t("转让后，你将成为普通成员。", "You will become an ordinary member.") : t("离开后将无法查看群消息，重新受邀加入后只能看到新的消息。", "After leaving, message access ends; rejoining starts a new history boundary.")}</p><button class="btn primary" type="button" data-chat="confirmed">${t("确认", "Confirm")}</button>`,
      );
    }
    async action(command, { close = true } = {}) {
      const epoch = this.epoch;
      const key =
        this.actionPending &&
        JSON.stringify(this.actionPending.command) === JSON.stringify(command)
          ? this.actionPending.key
          : id();
      this.actionPending = { key, command };
      const result = await this.client.command(command, key);
      this.actionPending = null;
      if (!this.valid(epoch)) return result;
      if (close) this.root.querySelector("dialog").close();
      await this.inbox();
      if (!this.valid(epoch)) return result;
      if (["leave", "decline"].includes(command.op)) {
        if (this.room?.id === command.room_id) {
          this.room = null;
          this.root.classList.remove("chat-has-room");
          this.root.querySelector("[data-chat-pane]").innerHTML = "";
        }
      } else if (result.room_id) await this.open(result.room_id);
      return result;
    }
    async click(event) {
      const button = event.target.closest("[data-chat]");
      if (!button || !this.root.contains(button) || this.busy) return;
      const op = button.dataset.chat;
      if (op === "open") {
        await this.open(button.dataset.room);
        return;
      }
      if (op === "close-dialog") {
        this.root.querySelector("dialog").close();
        return;
      }
      if (op === "new-direct" || op === "new-group") {
        this.newConversation(op === "new-group");
        return;
      }
      if (op === "back") {
        this.root.classList.remove("chat-has-room");
        return;
      }
      if (op === "members") {
        this.members();
        return;
      }
      if (op === "invite") {
        this.newConversation(false, true);
        return;
      }
      if (op.startsWith("confirm-")) {
        this.confirm(op.slice(8), button.dataset.person);
        return;
      }
      if (op === "report") {
        this.reportSeq = Number(button.dataset.seq);
        this.dialog(
          t("举报消息", "Report message"),
          `<p>${t("管理员将看到这条消息及举报原因，不会获得整个会话。", "Moderators will see this message and your reason, not the whole conversation.")}</p><form data-chat-form="report"><label>${t("举报原因", "Reason")}<textarea name="reason" maxlength="500" required></textarea></label><button class="btn primary">${t("提交举报", "Submit report")}</button></form>`,
        );
        return;
      }
      this.busy = true;
      button.disabled = true;
      const epoch = this.epoch;
      try {
        if (op === "accept" || op === "decline")
          await this.action({ op, room_id: button.dataset.room });
        if (op === "more-rooms") await this.inbox(this.next);
        if (op === "older") {
          const scroll = this.root.querySelector(".chat-message-scroll"),
            height = scroll.scrollHeight,
            top = scroll.scrollTop,
            selected = this.selection;
          const data = await this.client.get("messages", {
            room_id: this.room.id,
            before: String(this.messages[0]?.seq || 0),
          });
          if (this.valid(epoch) && selected === this.selection) {
            this.messages = mergeMessages(data.messages, this.messages);
            this.more = data.has_more;
            this.paintMessages(false, true);
            scroll.scrollTop = top + scroll.scrollHeight - height;
          }
        }
        if (op === "select-person") {
          if (this.mode === "group") {
            this.selected.set(button.dataset.person, button.dataset.name);
            this.root.querySelector("[data-chat-selected]").innerHTML = [
              ...this.selected,
            ]
              .map(
                ([p, n]) =>
                  `<button class="btn small" type="button" data-chat="unselect" data-person="${esc(p)}">${esc(n)} ×</button>`,
              )
              .join("");
          } else
            await this.action({
              op: this.mode === "invite" ? "invite" : "direct",
              ...(this.mode === "invite" ? { room_id: this.room.id } : {}),
              target: button.dataset.person,
            });
        }
        if (op === "unselect") {
          this.selected.delete(button.dataset.person);
          button.remove();
        }
        if (op === "create-group") {
          const groupTitle = this.root
            .querySelector('[name="group-title"]')
            .value.trim();
          if (!groupTitle) throw { code: "invalid_request" };
          await this.action({
            op: "group",
            title: groupTitle,
            members: [...this.selected.keys()],
          });
        }
        if (op === "recall")
          await this.action({
            op: "recall",
            room_id: this.room.id,
            seq: Number(button.dataset.seq),
          });
        if (op === "confirmed") await this.action(this.confirmCommand);
        if (op === "block" || op === "unblock") {
          await this.action({ op, target: button.dataset.person });
          this.notice(
            op === "block"
              ? t(
                  "已屏蔽，可在屏蔽名单中解除。",
                  "Blocked. You can unblock them in the blocked list.",
                )
              : t("已解除屏蔽。", "Member unblocked."),
          );
        }
        if (op === "blocks") {
          const members = await this.client.get("blocks");
          if (this.valid(epoch))
            this.dialog(
              t("屏蔽名单", "Blocked members"),
              members.length
                ? members
                    .map(
                      (p) =>
                        `<div class="chat-person"><span>${esc(name(p))} @${esc(p.username)}</span><button class="btn small" data-chat="unblock" data-person="${esc(p.id)}">${t("解除屏蔽", "Unblock")}</button></div>`,
                    )
                    .join("")
                : `<p>${t("暂无屏蔽成员。", "No blocked members.")}</p>`,
            );
        }
      } catch (e) {
        if (this.valid(epoch)) {
          const d = this.root.querySelector("dialog");
          if (d.open)
            d.querySelector('[role="status"]').textContent = errorText(e);
          else this.notice(errorText(e));
        }
      } finally {
        this.busy = false;
        if (button.isConnected) button.disabled = false;
      }
    }
    async submit(event) {
      const form = event.target.closest("[data-chat-form]");
      if (!form) return;
      event.preventDefault();
      const op = form.dataset.chatForm;
      if (op === "send") {
        await this.send();
        return;
      }
      if (this.busy) return;
      this.busy = true;
      const epoch = this.epoch;
      const submit = form.querySelector(
        'button[type="submit"],button:not([type])',
      );
      if (submit) submit.disabled = true;
      try {
        if (op === "people") await this.people(form);
        if (op === "rename")
          await this.action({
            op: "rename",
            room_id: this.room.id,
            title: new FormData(form).get("title").trim(),
          });
        if (op === "report") {
          await this.action({
            op: "report",
            room_id: this.room.id,
            seq: this.reportSeq,
            reason: new FormData(form).get("reason").trim(),
          });
          if (this.valid(epoch))
            this.notice(
              t(
                "举报已提交，管理员将审核这条消息。",
                "Report submitted for moderator review.",
              ),
            );
        }
      } catch (e) {
        if (this.valid(epoch))
          this.root.querySelector(".chat-dialog-status").textContent =
            errorText(e);
      } finally {
        this.busy = false;
        if (submit?.isConnected) submit.disabled = false;
      }
    }
  }
  class Moderation {
    constructor(api, admin) {
      this.client = new Client(api, admin);
    }
    async page(before = "") {
      const reports = await this.client.reports(before);
      return `<section class="card card-pad chat-moderation"><h1>${t("聊天举报", "Chat reports")}</h1><p class="muted">${t("仅显示成员主动举报的消息，不提供私人会话浏览。", "Only reported messages are shown; private inbox browsing is unavailable.")}</p>${reports.length ? reports.map((r) => `<form data-chat-review="${esc(r.id)}"><div class="chat-message-meta">#${esc(r.id)} · ${esc(new Date(r.created_at * 1000).toLocaleString())}</div><blockquote>${esc(r.body)}</blockquote><p>${t("举报原因：", "Reason: ")}${esc(r.reason)}</p><label>${t("处理说明", "Resolution")}<input name="reason" maxlength="500" required></label><div class="flex wrap"><button name="decision" value="remove" class="btn">${t("移除消息", "Remove message")}</button><button name="decision" value="dismiss" class="btn">${t("驳回举报", "Dismiss report")}</button></div><p role="status"></p></form>`).join("") : `<p>${t("暂无待处理举报。", "No pending reports.")}</p>`}<nav class="flex wrap">${before ? `<a class="btn small" href="/admin/chat" data-router>${t("最新举报", "Latest reports")}</a>` : ""}${reports.length === 50 ? `<a class="btn small" href="/admin/chat?before=${encodeURIComponent(reports.at(-1).id)}" data-router>${t("更早举报", "Earlier reports")}</a>` : ""}</nav></section>`;
    }
    async submit(form, decision) {
      const reason = new FormData(form).get("reason").trim();
      const command = {
        op: "resolve_report",
        report_id: form.dataset.chatReview,
        body: decision,
        reason,
      };
      const fp = JSON.stringify(command);
      if (form.dataset.command !== fp) {
        form.dataset.command = fp;
        form.dataset.key = id();
      }
      const result = await this.client.command(command, form.dataset.key, true);
      form.innerHTML = `<p role="status">${t("已处理", "Resolved")}</p>`;
      return result;
    }
  }
  return { Client, View, Moderation, mergeMessages, messageHTML, errorText };
});
