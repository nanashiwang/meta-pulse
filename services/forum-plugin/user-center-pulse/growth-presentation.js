/* Public, non-monetary presentation shared by Answer and the METAR shell. */
(function (root) {
  "use strict";
  function username(href, origin) {
    try {
      const u = new URL(href, origin);
      if (u.origin !== origin) return "";
      const m = /^\/users\/([^/]+)\/?$/.exec(u.pathname);
      if (
        !m ||
        /^(login|register|logout|settings|notifications|account-recovery|password-reset|activate|email-verification)$/.test(
          m[1],
        )
      )
        return "";
      return decodeURIComponent(m[1]);
    } catch (_) {
      return "";
    }
  }
  function install(host = window) {
    if (host.__metarGrowthPresentation) return;
    host.__metarGrowthPresentation = true;
    const doc = host.document,
      cache = new Map();
    let scheduled = false;
    const english = () => doc.documentElement.lang.startsWith("en");
    function paint(anchor, value, name) {
      if (
        !anchor.isConnected ||
        username(anchor.href, host.location.origin) !== name
      )
        return;
      const level = value?.level;
      if (
        !Number.isInteger(level?.number) ||
        level.number < 0 ||
        level.number > 8
      )
        return;
      const img = anchor.querySelector("img");
      if (img) img.dataset.metarAppearance = value.appearance;
      let badge = anchor.querySelector(".metar-exp-badge");
      if (
        img &&
        ![...anchor.childNodes]
          .filter((n) => n !== badge)
          .map((n) => n.textContent)
          .join("")
          .trim()
      ) {
        badge?.remove();
        return;
      }
      if (!badge) {
        badge = doc.createElement("span");
        badge.className = "metar-exp-badge";
        anchor.append(badge);
      }
      const text = "Lv." + level.number;
      if (badge.textContent !== text) badge.textContent = text;
      badge.title =
        (english() ? "Community level · " : "社区等级 · ") +
        (english()
          ? [
              "New friend",
              "New arrival",
              "Active member",
              "Helpful member",
              "Familiar face",
              "Experienced member",
              "Community builder",
              "Long-term contributor",
              "Community companion",
            ][level.number]
          : level.name);
      if (anchor.matches(".h3")) {
        const profile = anchor.closest(".flex-md-row");
        if (profile) profile.dataset.metarAppearance = value.appearance;
      }
    }
    function render() {
      scheduled = false;
      const links = [...doc.querySelectorAll('a[href*="/users/"]')].filter(
        (a) =>
          !a.closest(
            "#header,.topbar,nav,.nav,.nav-link,[role=tablist],.sidebar,#sideNav",
          ),
      );
      const requested = new Set();
      for (const a of links) {
        const name = username(a.href, host.location.origin);
        if (!name) continue;
        const found = cache.get(name);
        if (found && Date.now() - found.at < 60000) {
          if (found.value) paint(a, found.value, name);
          continue;
        }
        if (requested.size >= 30 && !requested.has(name)) continue;
        requested.add(name);
        if (found?.pending) {
          found.pending.then((v) => paint(a, v, name));
          continue;
        }
        const item = { at: Date.now(), value: null };
        item.pending = host
          .fetch(
            "/answer/api/v1/metar/experience/profile?username=" +
              encodeURIComponent(name),
            {
              credentials: "same-origin",
              redirect: "error",
              signal: AbortSignal.timeout(5000),
            },
          )
          .then((r) => (r.ok ? r.json() : null))
          .then((r) => {
            item.value = r?.data;
            return item.value;
          })
          .catch(() => null)
          .finally(() => {
            item.pending = null;
          });
        cache.set(name, item);
        item.pending.then((v) => paint(a, v, name));
      }
      if (cache.size > 300)
        for (const [name, v] of cache)
          if (!v.pending && Date.now() - v.at > 60000) cache.delete(name);
    }
    new host.MutationObserver(() => {
      if (!scheduled) {
        scheduled = true;
        host.requestAnimationFrame(render);
      }
    }).observe(doc.documentElement, { childList: true, subtree: true });
    render();
  }
  const api = { install, username };
  if (typeof module === "object" && module.exports) module.exports = api;
  else {
    root.MetarGrowthPresentation = api;
    if (root.document) install(root);
  }
})(typeof window === "undefined" ? globalThis : window);
