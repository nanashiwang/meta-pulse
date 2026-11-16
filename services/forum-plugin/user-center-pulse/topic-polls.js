/* Topic polls are optional and do not grant reputation, EXP or tickets. */
export function isPollComposerPath(path) {
  return /^\/questions\/(?:ask|add)\/?$/.test(path);
}

export function validatePoll(config) {
  const title = String(config.title || "").trim(),
    options = (config.options || []).map((v) => String(v).trim());
  const max = Number(config.max_choices),
    days = Number(config.days),
    text = (v) => v && !/[\u0000-\u001f\u007f]/.test(v);
  if (
    !text(title) ||
    Array.from(title).length > 160 ||
    options.length < 2 ||
    options.length > 10 ||
    options.some((v) => !text(v) || Array.from(v).length > 100) ||
    new Set(options.map((v) => v.toLowerCase())).size !== options.length ||
    !Number.isInteger(max) ||
    max < 1 ||
    max > options.length ||
    ![1, 3, 7, 30].includes(days)
  )
    throw Error("invalid_request");
  return { title, options, max_choices: max, days };
}
export function canInspectPollVoters(user) {
  return (
    Boolean(user?.id) &&
    user.status === "normal" &&
    user.mail_status === 1 &&
    user.role_id === 2
  );
}
export function voterChoiceLabels(choices, options) {
  return (Array.isArray(choices) ? choices : [])
    .filter((i) => Number.isInteger(i) && i >= 0 && i < options.length)
    .map((i) => options[i]);
}
export function resultPercent(count, total) {
  return total > 0 ? Math.max(0, Math.min(100, (count * 100) / total)) : 0;
}
export function installTopicPolls(options, host = window) {
  if (host.MetarPollComposer) return host.MetarPollComposer;
  const doc = host.document;
  let composer = null,
    panel = null,
    question = "",
    epoch = 0,
    scheduled = false,
    expiry = 0,
    scope = "",
    recovery = null;
  const en = () =>
    options.getLanguage?.() === "en_US" ||
    doc.documentElement.lang.startsWith("en");
  const t = (zh, english) => (en() ? english : zh);
  const user = () => String(options.getUser?.()?.id || "");
  const token = () => options.getToken?.() || "";
  const el = (tag, text, cls) => {
    const n = doc.createElement(tag);
    if (text !== undefined) n.textContent = text;
    if (cls) n.className = cls;
    return n;
  };
  const key = (kind) => "metar-poll-v1:" + user() + ":" + kind;
  function read(kind) {
    try {
      return JSON.parse(host.localStorage.getItem(key(kind)) || "null");
    } catch (_) {
      return null;
    }
  }
  function save(kind, data) {
    host.localStorage.setItem(key(kind), JSON.stringify(data));
  }
  function remove(kind) {
    try {
      host.localStorage.removeItem(key(kind));
    } catch (_) {}
  }
  const message = (code) =>
    ({
      invalid_request: t(
        "请填写题目、2～10 个不同选项，并检查可选数量。",
        "Enter a title, 2–10 unique options and a valid selection limit.",
      ),
      forbidden: t(
        "仅已验证且正常的账号可操作，只有作者能创建或结束投票。",
        "A verified active account is required; only the author can create or close a poll.",
      ),
      authentication_required: t(
        "请先登录社区账号。",
        "Sign in to participate.",
      ),
      conflict: t(
        "投票已变化，请刷新后重试。",
        "The poll changed. Refresh before trying again.",
      ),
      poll_closed: t("投票已结束，不能再修改选择。", "This poll has ended."),
      not_found: t(
        "帖子暂不可见或投票不存在。",
        "The post or poll is not available.",
      ),
      pending_request: t(
        "上一次操作尚未确认，请先重试原请求。",
        "A previous request is unconfirmed. Retry it first.",
      ),
      storage: t(
        "无法保存恢复记录，请允许站点存储后重试。",
        "Enable site storage so this request can be recovered.",
      ),
      changed_user: t(
        "账号已切换，请重新打开帖子。",
        "Your account changed. Reopen this post.",
      ),
    })[code] ||
    t(
      "请求未确认，请重试原请求，不要重复发布帖子。",
      "Request not confirmed. Retry the same request; do not republish the post.",
    );
  async function api(path, body, keyValue) {
    const controller = new AbortController(),
      timer = host.setTimeout(() => controller.abort(), 10000),
      headers = { Accept: "application/json" };
    const auth = token();
    if (auth)
      headers.Authorization = auth.startsWith("Bearer ")
        ? auth
        : "Bearer " + auth;
    if (body) {
      headers["Content-Type"] = "application/json";
      headers["X-Metar-Request"] = "1";
      headers["Idempotency-Key"] = keyValue;
    }
    try {
      const r = await host.fetch("/answer/api/v1/metar/polls" + path, {
        method: body ? "POST" : "GET",
        headers,
        credentials: "same-origin",
        redirect: "error",
        signal: controller.signal,
        ...(body ? { body: JSON.stringify(body) } : {}),
      });
      const payload = await r.json();
      if (!r.ok)
        throw Object.assign(Error(payload.error || "poll_unavailable"), {
          definite: [400, 401, 403, 404, 409].includes(r.status),
        });
      if (!payload || !Object.hasOwn(payload, "data"))
        throw Error("poll_unavailable");
      return payload.data;
    } finally {
      host.clearTimeout(timer);
    }
  }
  function form(container, initial, submit) {
    const box = el("div", undefined, "metar-poll-editor");
    const title = el("input");
    title.type = "text";
    title.maxLength = 160;
    title.value = initial?.title || "";
    title.placeholder = t(
      "例如：你最常用哪个 AI 工具？",
      "Which AI tool do you use most?",
    );
    function field(label, input) {
      const l = el("label", label, "metar-poll-field");
      l.append(input);
      box.append(l);
    }
    field(t("投票题目", "Poll question"), title);
    const optionBox = el("div", undefined, "metar-poll-option-editor");
    box.append(optionBox);
    let inputs = [];
    const limit = el("select");
    for (let i = 1; i <= 10; i++) {
      const o = el(
        "option",
        i === 1
          ? t("单选", "Single choice")
          : t("最多选 " + i + " 项", "Up to " + i + " choices"),
      );
      o.value = String(i);
      limit.append(o);
    }
    limit.value = String(initial?.max_choices || 1);
    const duration = el("select");
    for (const d of [1, 3, 7, 30]) {
      const o = el("option", t(d + " 天", d + " days"));
      o.value = String(d);
      duration.append(o);
    }
    duration.value = String(initial?.days || 7);
    function add(value = "") {
      if (inputs.length >= 10) return;
      const row = el("div", undefined, "metar-poll-option-input");
      const input = el("input");
      input.type = "text";
      input.maxLength = 100;
      input.value = value;
      input.placeholder = t("选项", "Option");
      input.setAttribute("aria-label", t("投票选项", "Poll option"));
      const del = el("button", t("删除", "Remove"));
      del.type = "button";
      del.onclick = () => {
        if (inputs.length <= 2) return;
        inputs = inputs.filter((x) => x !== input);
        row.remove();
        changed();
      };
      row.append(input, del);
      optionBox.append(row);
      inputs.push(input);
    }
    (initial?.options || ["", ""]).slice(0, 10).forEach(add);
    const addButton = el("button", t("＋ 添加选项", "＋ Add option"));
    addButton.type = "button";
    addButton.onclick = () => {
      add();
      changed();
    };
    box.append(addButton);
    field(t("投票方式", "Selection limit"), limit);
    field(t("持续时间", "Duration"), duration);
    box.append(
      el(
        "p",
        t(
          "发布后题目与选项固定；截止前可改票，投票后或结束后可看结果。",
          "Question and options are fixed after publication. Change your vote before closing; results appear after voting or closing.",
        ),
        "metar-poll-muted",
      ),
    );
    const status = el("p", "", "metar-poll-error");
    status.setAttribute("role", "status");
    box.append(status);
    const values = () => ({
      title: title.value,
      options: inputs.map((i) => i.value),
      max_choices: Number(limit.value),
      days: Number(duration.value),
    });
    function changed() {
      if (!submit && user()) {
        try {
          save("draft", {
            enabled: composer?.enabled.checked !== false,
            ...values(),
          });
          status.textContent = "";
        } catch (_) {
          status.textContent = message("storage");
        }
      }
    }
    box.addEventListener("input", changed);
    box.addEventListener("change", changed);
    if (submit) {
      const button = el(
        "button",
        t("发布投票", "Publish poll"),
        "metar-poll-primary",
      );
      button.type = "button";
      button.onclick = async () => {
        try {
          const config = validatePoll(values());
          button.disabled = true;
          await submit(config);
          status.textContent = "";
        } catch (e) {
          status.textContent = message(e.message);
        } finally {
          button.disabled = false;
        }
      };
      box.append(button);
    }
    container.append(box);
    return { values, status, box };
  }
  function mountComposer() {
    if (!isPollComposerPath(host.location.pathname) || !user()) {
      composer?.node.remove();
      composer = null;
      return;
    }
    const input = doc.querySelector("input#title"),
      native = input?.closest("form");
    if (!native) return;
    if (composer?.node.isConnected) return;
    const saved = read("draft"),
      node = el("details", undefined, "metar-poll-composer");
    node.open = !!saved?.enabled;
    node.append(el("summary", t("添加投票（可选）", "Add a poll (optional)")));
    const enabled = el("input");
    enabled.type = "checkbox";
    enabled.checked = !!saved?.enabled;
    const label = el(
      "label",
      t("在这篇帖子中附带投票", "Include a poll in this post"),
      "metar-poll-enable",
    );
    label.prepend(enabled);
    node.append(label);
    const editor = form(node, saved, null);
    editor.box.hidden = !enabled.checked;
    enabled.onchange = () => {
      editor.box.hidden = !enabled.checked;
      try {
        save("draft", { enabled: enabled.checked, ...editor.values() });
        editor.status.textContent = "";
      } catch (_) {
        editor.status.textContent = message("storage");
      }
    };
    let actions = native.querySelector('button[type="submit"]');
    while (actions && actions.parentElement !== native)
      actions = actions.parentElement;
    if (actions) native.insertBefore(node, actions);
    else native.append(node);
    composer = { node, enabled, ...editor };
  }
  const hooks = {
    prepare() {
      if (!composer?.enabled.checked) return null;
      try {
        if (!user()) throw Error("authentication_required");
        const config = validatePoll(composer.values());
        const value = { user: user(), config, key: host.crypto.randomUUID() };
        save("draft", { enabled: true, ...config });
        composer.status.textContent = "";
        return value;
      } catch (e) {
        composer.status.textContent = message(
          e.message === "invalid_request" ? e.message : "storage",
        );
        composer.node.open = true;
        composer.node.scrollIntoView({ block: "center" });
        return false;
      }
    },
    async attach(id, draft) {
      if (!draft) return;
      try {
        if (draft.user !== user()) throw Error("changed_user");
        const body = {
          op: "create",
          question_id: String(id),
          config: draft.config,
          version: 0,
        };
        recovery = {
          question_id: String(id),
          body,
          key: draft.key,
          user: draft.user,
        };
        save("pending-create", recovery);
        await api("/command", body, draft.key);
        if (draft.user === user()) {
          remove("pending-create");
          remove("draft");
          recovery = null;
        }
      } catch (_) {
        /* Detail page offers explicit recovery of this same request. */
      }
    },
  };
  host.MetarPollComposer = hooks;
  async function command(body) {
    if (!user()) throw Error("authentication_required");
    const owner = user(),
      id = question;
    let pending = read("command:" + id);
    if (pending && JSON.stringify(pending.body) !== JSON.stringify(body))
      throw Error("pending_request");
    if (!pending) {
      pending = { body, key: host.crypto.randomUUID() };
      try {
        save("command:" + id, pending);
      } catch (_) {
        throw Error("storage");
      }
    }
    try {
      await api("/command", pending.body, pending.key);
      if (owner !== user()) throw Error("changed_user");
      remove("command:" + id);
      if (id !== question) throw Error("changed_page");
      return true;
    } catch (e) {
      if (e.definite && owner === user()) remove("command:" + id);
      throw e;
    }
  }
  function button(label, action, primary = false) {
    const b = el("button", label, primary ? "metar-poll-primary" : "");
    b.type = "button";
    b.onclick = action;
    return b;
  }
  async function refresh() {
    const current = ++epoch,
      owner = user(),
      id = question,
      node = panel;
    if (!id || !node?.isConnected) return;
    try {
      const data = await api(
        (owner ? "/mine" : "") + "?question_id=" + encodeURIComponent(id),
      );
      if (current !== epoch || owner !== user() || !node.isConnected) return;
      render(data);
    } catch (e) {
      if (current !== epoch || !node.isConnected) return;
      node.replaceChildren(
        el("p", message(e.message), "metar-poll-muted"),
        button(t("重试投票加载", "Retry poll"), refresh),
      );
    }
  }
  function render(data) {
    if (!panel?.isConnected) return;
    host.clearTimeout(expiry);
    panel.replaceChildren();
    panel.className = "metar-topic-poll";
    const pending =
      read("pending-create") || (recovery?.user === user() ? recovery : null);
    const matchingPending =
      pending?.body?.config &&
      (pending.question_id === question ||
        pending.question_id === data.question_id);
    if (!data.exists) {
      if (!data.can_create || !matchingPending) {
        panel.hidden = true;
        return;
      }
      panel.hidden = false;
      const notice = el(
        "p",
        t(
          "帖子已发布，投票尚未确认保存。请重试，无需再次发帖。",
          "Your post is published. Poll creation was not confirmed. Retry without posting again.",
        ),
        "metar-poll-error",
      );
      panel.append(
        notice,
        button(t("重试保存原投票", "Retry original poll"), async () => {
          const owner = user();
          try {
            await api("/command", pending.body, pending.key);
            if (owner !== user()) return;
            remove("pending-create");
            remove("draft");
            recovery = null;
            await refresh();
          } catch (e) {
            notice.textContent = message(e.message);
          }
        }),
      );
      return;
    }
    panel.hidden = false;
    if (matchingPending) {
      remove("pending-create");
      remove("draft");
    }
    const head = el("div", undefined, "metar-poll-heading");
    head.append(
      el("strong", t("社区投票", "Community poll")),
      el(
        "span",
        data.ended
          ? data.early_closed
            ? t("已提前结束", "Closed early")
            : t("已结束", "Closed")
          : t("进行中", "Open"),
        "metar-poll-status",
      ),
    );
    panel.append(head, el("h2", data.title));
    panel.append(
      el(
        "p",
        (data.max_choices === 1
          ? t("单选", "Single choice")
          : t(
              "最多选 " + data.max_choices + " 项",
              "Choose up to " + data.max_choices,
            )) +
          " · " +
          t("截止 ", "Closes ") +
          new Date(data.closes_at * 1000).toLocaleString(
            en() ? "en-US" : "zh-CN",
          ),
        "metar-poll-muted",
      ),
    );
    const inputBox = el("div", undefined, "metar-poll-options");
    panel.append(inputBox);
    let editing = !data.results_visible;
    const inputs = [];
    data.options.forEach((label, i) => {
      const row = el("label", undefined, "metar-poll-option");
      const input = el("input");
      input.type = data.max_choices === 1 ? "radio" : "checkbox";
      input.name = "metar-topic-poll";
      input.value = String(i);
      input.checked = data.selected.includes(i);
      input.disabled = !data.can_vote || !editing;
      inputs.push(input);
      row.append(input, el("span", label));
      if (data.results_visible) {
        const count = data.counts[i] || 0,
          percent = resultPercent(count, data.total);
        row.append(
          el(
            "span",
            count + " · " + percent.toFixed(1) + "%",
            "metar-poll-count",
          ),
        );
        const track = el("div", undefined, "metar-poll-track"),
          fill = el("div");
        fill.style.width = percent + "%";
        track.append(fill);
        row.append(track);
      }
      inputBox.append(row);
    });
    const status = el("p", "", "metar-poll-error");
    status.setAttribute("role", "status");
    panel.append(status);
    const actions = el("div", undefined, "metar-poll-actions");
    panel.append(actions);
    if (data.can_vote) {
      const submit = button(
        t("提交投票", "Submit vote"),
        async () => {
          const choices = inputs
            .filter((i) => i.checked)
            .map((i) => Number(i.value));
          if (!choices.length || choices.length > data.max_choices) {
            status.textContent = message("invalid_request");
            return;
          }
          submit.disabled = true;
          try {
            await command({
              op: "vote",
              question_id: question,
              choices,
              version: data.version,
            });
            await refresh();
          } catch (e) {
            status.textContent = message(e.message);
            submit.disabled = false;
            if (e.message === "conflict" || e.message === "poll_closed")
              actions.append(button(t("刷新投票", "Refresh poll"), refresh));
          }
        },
        true,
      );
      submit.hidden = !editing;
      actions.append(submit);
      if (data.results_visible)
        actions.append(
          button(t("修改选择", "Change vote"), () => {
            editing = true;
            inputs.forEach((i) => (i.disabled = false));
            submit.hidden = false;
          }),
        );
    } else if (!data.ended && !user()) {
      const login = el(
        "a",
        t("登录后投票", "Sign in to vote"),
        "metar-poll-login",
      );
      login.href = "/users/login";
      actions.append(login);
    }
    if (data.can_manage && !data.ended)
      actions.append(
        button(t("提前结束", "Close early"), async () => {
          if (
            !host.confirm(
              t(
                "结束后不可恢复，确定结束投票？",
                "Closing is permanent. End this poll?",
              ),
            )
          )
            return;
          try {
            await command({ op: "close", question_id: question, version: 0 });
            await refresh();
          } catch (e) {
            status.textContent = message(e.message);
          }
        }),
      );
    actions.append(button(t("刷新", "Refresh"), refresh));
    panel.append(
      el(
        "p",
        data.results_visible
          ? t(
              "共 " +
                data.total +
                " 人参与" +
                (data.max_choices > 1
                  ? " · 比例按参与人数计算，合计可能超过100%。"
                  : ""),
              data.total +
                " participants" +
                (data.max_choices > 1
                  ? " · Multiple-choice percentages may total over 100%."
                  : ""),
            )
          : t(
              "投票后或截止后查看汇总结果。",
              "Aggregate results appear after voting or closing.",
            ),
        "metar-poll-muted",
      ),
    );

    panel.append(
      el(
        "p",
        t(
          "个人选择不公开，管理员可查看投票明细。",
          "Individual choices are private to other members; administrators can inspect votes.",
        ),
        "metar-poll-muted",
      ),
    );
    if (canInspectPollVoters(options.getUser?.())) {
      const details = el("section", undefined, "metar-poll-voters");
      details.hidden = true;
      const toggle = button(
        t("查看投票明细（管理员）", "View voters (admin)"),
        () => {
          details.hidden = !details.hidden;
          if (!details.hidden) void loadVoters("");
          else {
            serial++;
            details.replaceChildren();
          }
        },
      );
      actions.append(toggle);
      panel.append(details);
      let serial = 0;
      async function loadVoters(after) {
        const request = ++serial,
          actor = user(),
          currentQuestion = question,
          currentEpoch = epoch;
        details.replaceChildren(
          el("p", t("正在加载投票明细…", "Loading votes…")),
        );
        try {
          const query = new URLSearchParams({ question_id: currentQuestion });
          if (after) query.set("after", after);
          const page = await api("/voters?" + query);
          if (
            request !== serial ||
            epoch !== currentEpoch ||
            user() !== actor ||
            question !== currentQuestion ||
            !details.isConnected ||
            details.hidden ||
            !canInspectPollVoters(options.getUser?.())
          )
            return;
          details.replaceChildren(
            el(
              "h4",
              t(
                "投票明细 · 仅管理员可见",
                "Voter details · Administrators only",
              ),
            ),
          );
          const list = el("ul", undefined, "metar-poll-voter-list");
          for (const voter of page.items || []) {
            const row = el("li");
            const name =
              voter.display_name ||
              voter.username ||
              t("已删除账号", "Deleted account");
            row.append(el("strong", name + " · ID " + voter.user_id));
            row.append(
              el(
                "p",
                voterChoiceLabels(voter.choices, data.options).join("、"),
              ),
            );
            row.append(
              el(
                "small",
                new Date(voter.updated_at * 1000).toLocaleString(
                  en() ? "en-US" : "zh-CN",
                ),
              ),
            );
            list.append(row);
          }
          details.append(list);
          if (!page.items?.length)
            details.append(el("p", t("暂无投票记录。", "No votes yet.")));
          const controls = el("div", undefined, "metar-poll-actions");
          if (after)
            controls.append(
              button(t("回到第一页", "First page"), () => loadVoters("")),
            );
          if (page.next_cursor)
            controls.append(
              button(t("下一页", "Next page"), () =>
                loadVoters(page.next_cursor),
              ),
            );
          controls.append(
            button(t("刷新明细", "Refresh details"), () => loadVoters("")),
          );
          details.append(controls);
        } catch (error) {
          if (
            request !== serial ||
            epoch !== currentEpoch ||
            user() !== actor ||
            !details.isConnected ||
            details.hidden
          )
            return;
          details.replaceChildren(
            el(
              "p",
              error.message === "forbidden"
                ? t(
                    "仅管理员可查看投票明细。",
                    "Only administrators can inspect votes.",
                  )
                : t(
                    "投票明细加载失败，请重试。",
                    "Could not load votes. Please retry.",
                  ),
            ),
          );
          details.append(button(t("重试", "Retry"), () => loadVoters(after)));
        }
      }
    }
    const outstanding = read("command:" + question);
    if (outstanding) {
      status.textContent = t(
        "有一次操作尚未确认，请先重试原请求。",
        "A request is unconfirmed. Retry it first.",
      );
      actions.prepend(
        button(t("重试原请求", "Retry pending request"), async () => {
          try {
            await command(outstanding.body);
            await refresh();
          } catch (e) {
            status.textContent = message(e.message);
          }
        }),
      );
    }
    if (!data.ended) {
      const delay = Math.max(
        1000,
        Math.min(2147483647, data.closes_at * 1000 - Date.now() + 100),
      );
      expiry = host.setTimeout(refresh, delay);
    }
  }
  function mount() {
    scheduled = false;
    const identity =
      user() +
      ":" +
      canInspectPollVoters(options.getUser?.()) +
      ":" +
      (options.getLanguage?.() || doc.documentElement.lang);
    if (identity !== scope) {
      scope = identity;
      epoch++;
      composer?.node.remove();
      composer = null;
      panel?.remove();
      panel = null;
      question = "";
      host.clearTimeout(expiry);
    }
    mountComposer();
    const match = host.location.pathname.match(
      /^\/questions\/([a-zA-Z0-9]+)(?:\/|$)/,
    );
    const heading = doc.querySelector(".questionDetailPage .page-main h1"),
      article = heading?.parentElement?.querySelector("article.fmt");
    if (
      !match ||
      !article ||
      ["ask", "add", "edit", "linked"].includes(match[1])
    ) {
      epoch++;
      panel?.remove();
      panel = null;
      question = "";
      host.clearTimeout(expiry);
      return;
    }
    if (question === match[1] && panel?.isConnected) return;
    epoch++;
    panel?.remove();
    question = match[1];
    panel = el("section", undefined, "metar-topic-poll");
    panel.setAttribute("aria-label", t("帖子投票", "Topic poll"));
    article.after(panel);
    void refresh();
  }
  function schedule() {
    if (!scheduled) {
      scheduled = true;
      host.requestAnimationFrame(mount);
    }
  }
  new host.MutationObserver((records) => {
    if (
      records.some(
        (r) => !panel?.contains(r.target) && !composer?.node.contains(r.target),
      )
    )
      schedule();
  }).observe(doc.documentElement, { childList: true, subtree: true });
  host.addEventListener("popstate", schedule);
  host.addEventListener("pageshow", schedule);
  host.addEventListener("storage", (e) => {
    if (e.key === "_a_ltk_" || e.key === null) schedule();
  });
  options.subscribeUser?.(schedule);
  options.onLanguageChanged?.(schedule);
  schedule();
  return hooks;
}
