export function installContentAwards(options, host = window) {
  if (host.__metarContentAwards) return;
  host.__metarContentAwards = true;
  const doc = host.document;
  let node = null,
    key = "",
    epoch = 0,
    scheduled = false;
  const text = (zh, en) => (options.getLanguage?.() === "en_US" ? en : zh);
  const make = (tag, value) => {
    const n = doc.createElement(tag);
    if (value) n.textContent = value;
    return n;
  };
  const identity = () => String(options.getUser?.()?.id || "");
  async function api(path, command, requestKey) {
    const headers = { Accept: "application/json" };
    const token = options.getToken?.();
    if (token)
      headers.Authorization = token.startsWith("Bearer ")
        ? token
        : "Bearer " + token;
    if (command) {
      headers["Content-Type"] = "application/json";
      headers["X-Metar-Request"] = "1";
      headers["Idempotency-Key"] = requestKey;
    }
    const controller = new AbortController(),
      timer = host.setTimeout(() => controller.abort(), 15000);
    try {
      const response = await host.fetch("/answer/api/v1/metar/awards" + path, {
        method: command ? "POST" : "GET",
        headers,
        credentials: "same-origin",
        redirect: "error",
        signal: controller.signal,
        ...(command ? { body: JSON.stringify(command) } : {}),
      });
      const body = await response.json();
      if (!response.ok)
        throw Object.assign(Error(body.error || "unavailable"), {
          status: response.status,
        });
      return body.data;
    } finally {
      host.clearTimeout(timer);
    }
  }
  function schedule() {
    if (scheduled) return;
    scheduled = true;
    host.requestAnimationFrame(mount);
  }
  async function mount() {
    scheduled = false;
    const match = host.location.pathname.match(
      /^\/questions\/([a-zA-Z0-9]+)(?:\/|$)/,
    );
    const article = doc
      .querySelector(".questionDetailPage .page-main h1")
      ?.parentElement?.querySelector("article.fmt");
    const user = options.getUser?.();
    const id = match?.[1];
    const next = [
      id,
      identity(),
      user?.status,
      user?.mail_status,
      options.getLanguage?.(),
    ].join(":");
    if (!article || !id || ["ask", "add", "edit", "linked"].includes(id)) {
      epoch++;
      node?.remove();
      node = null;
      key = "";
      return;
    }
    if (next === key && node?.isConnected) return;
    key = next;
    const generation = ++epoch;
    node?.remove();
    node = make("section");
    node.className = "metar-award-nomination";
    article.after(node);
    const current = node;
    const title = make("strong", text("优秀内容评选", "Content awards"));
    const info = make(
      "p",
      text(
        "有价值、引发有效讨论的帖子可以提名优秀帖子奖；作者采纳的回答进入优秀回应评选。",
        "Nominate valuable posts with meaningful discussion. Author-accepted answers qualify for response review.",
      ),
    );
    const link = make(
      "a",
      text("查看评选规则与获奖名单", "Rules and award winners"),
    );
    link.href = "/awards";
    const guidance = make('p', text(
      '每帖最多 10 人推荐。帖子获优秀帖子周奖后，推荐者平分 20 闪，每人至少 3 闪，并获得一次「伯乐」勋章；同帖月奖不重复发。',
      'Up to 10 recommendations per post. If the post wins a weekly award, recommenders split 20 credits with a 3-credit minimum each and earn a Talent Scout medal. Monthly awards do not pay twice.'
    ));
    current.append(title, info, guidance, link);
    if (!user?.id) {
      const login = make("a", text("登录后提名", "Sign in to nominate"));
      login.href = "/users/login";
      current.append(login);
      return;
    }
    const button = make("button", text("优秀稿件", "Nominate post"));
    button.type = "button";
    button.disabled = true;
    const status = make("p");
    status.setAttribute("role", "status");
    current.append(button, status);
    const live = () =>
      generation === epoch &&
      identity() === String(user.id) &&
      current.isConnected;
    const showError = (e) => {
      if (!live()) return;
      const messages = {
        recommendation_limit: ["该帖已有 10 人推荐。", "This post already has 10 recommenders."],
        nomination_limit: [
          "今日或本周提名次数已用完。",
          "Daily or weekly nomination limit reached.",
        ],
        period_closed: [
          "本帖提名时间已结束。",
          "The nomination window has closed.",
        ],
        forbidden: [
          "需正常且已验证的账号，不能提名自己的帖子。",
          "A verified active account is required. Self-nomination is not allowed.",
        ],
        not_found: ["帖子暂不可见。", "This post is not available."],
      };
      status.textContent = messages[e.message]
        ? text(...messages[e.message])
        : text("暂时无法确认，请重试。", "Could not confirm. Please retry.");
    };
    async function load() {
      try {
        const result = await api(
          "/status?question_id=" + encodeURIComponent(id),
        );
        if (!live()) return;
        button.disabled = !result.can_nominate;
        button.textContent = !result.nominated && result.recommendation_count >= 10
          ? text("推荐已满", "Recommendations full")
          : result.nominated
          ? text("已提名", "Nominated")
          : text("优秀稿件", "Nominate post");
        status.textContent =
          text(
            "今日剩余 " +
              result.daily_remaining +
              " 次，本周剩余 " +
              result.weekly_remaining +
              " 次；截止：",
            "Remaining today: " +
              result.daily_remaining +
              ", this week: " +
              result.weekly_remaining +
              ". Closes: ",
          ) +
          new Date(result.closes_at * 1000).toLocaleString("zh-CN", {
            timeZone: "Asia/Shanghai",
          }) +
          " (UTC+8)";
      } catch (e) {
        showError(e);
        const retry = make("button", text("重新加载", "Reload"));
        retry.type = "button";
        retry.onclick = () => {
          retry.remove();
          load();
        };
        if (live()) current.append(retry);
      }
    }
    let requestKey = null;
    button.onclick = async () => {
      if (!live()) return;
      button.disabled = true;
      try {
        requestKey ||= host.crypto.randomUUID();
        await api("/command", { op: "nominate", question_id: id }, requestKey);
        requestKey = null;
        await load();
      } catch (e) {
        showError(e);
        if (e.status >= 400 && e.status < 500) {
          requestKey = null;
          await load();
        } else if (live()) {
          button.disabled = false;
          button.textContent = text("重试原提名", "Retry nomination");
        }
      }
    };
    await load();
  }
  new host.MutationObserver((records) => {
    if (records.some((r) => !node?.contains(r.target))) schedule();
  }).observe(doc.documentElement, { childList: true, subtree: true });
  host.addEventListener("popstate", schedule);
  options.subscribeUser?.(schedule);
  options.onLanguageChanged?.(schedule);
  schedule();
}
