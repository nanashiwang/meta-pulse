export function installAwardTitles(host = window) {
  const doc = host.document,
    cache = new Map(),
    expiryTimers = new WeakMap();
  let queued = false;
  function paint(slot, data, name) {
    const anchor = slot.parentElement?.querySelector('a[href^="/users/"]');
    if (
      !anchor ||
      decodeURIComponent(new URL(anchor.href).pathname.split("/")[2] || "") !==
        name
    )
      return;
    if (!slot.isConnected) return;
    const title = data?.selected;
    const previous = expiryTimers.get(slot);
    if (previous && previous.expires !== title?.expires_at) {
      host.clearTimeout(previous.timer);
      expiryTimers.delete(slot);
    }
    let node = slot.parentElement.querySelector(":scope > .metar-award-title");
    slot.classList.add("metar-award-reputation");
    if (!title || title.expires_at * 1000 <= Date.now()) {
      node?.remove();
      return;
    }
    if (!node) {
      node = doc.createElement("span");
      slot.after(node);
    }
    const cls =
      "metar-award-title" +
      (title.style === "glow" ? " metar-award-title-glow" : "");
    if (node.className !== cls) node.className = cls;
    if (node.textContent !== title.name) node.textContent = title.name;
    if (!expiryTimers.has(slot)) {
      const timer = host.setTimeout(
        () => {
          expiryTimers.delete(slot);
          paint(slot, data, name);
        },
        Math.min(2147483647, Math.max(0, title.expires_at * 1000 - Date.now())),
      );
      expiryTimers.set(slot, { timer, expires: title.expires_at });
    }
    node.title =
      "头衔有效至 " + new Date(title.expires_at * 1000).toLocaleString();
  }
  function render() {
    queued = false;
    for (const slot of doc.querySelectorAll(
      '.questionDetailPage span[title="Reputation"]',
    )) {
      const anchor = slot.parentElement.querySelector('a[href^="/users/"]');
      if (!anchor) continue;
      const name = decodeURIComponent(
        new URL(anchor.href).pathname.split("/")[2] || "",
      );
      if (!name) continue;
      if (slot.dataset.awardUser !== name) {
        slot.parentElement
          .querySelector(":scope > .metar-award-title")
          ?.remove();
        slot.dataset.awardUser = name;
      }
      let entry = cache.get(name);
      if (entry && Date.now() - entry.at < 30000) {
        if (entry.data) paint(slot, entry.data, name);
        else entry.promise?.then((v) => paint(slot, v, name));
        continue;
      }
      entry = { at: Date.now() };
      entry.promise = host
        .fetch(
          "/answer/api/v1/metar/awards/profile?username=" +
            encodeURIComponent(name),
          {
            credentials: "omit",
            redirect: "error",
            cache: "no-store",
            signal: AbortSignal.timeout(5000),
          },
        )
        .then((r) => (r.ok ? r.json() : null))
        .then((r) => {
          entry.data = r?.data || {};
          return entry.data;
        })
        .catch(() => ({}));
      cache.set(name, entry);
      entry.promise.then((v) => paint(slot, v, name));
    }
    if (cache.size > 200)
      for (const [key, value] of cache)
        if (Date.now() - value.at > 30000) cache.delete(key);
  }
  function schedule() {
    if (!queued) {
      queued = true;
      host.requestAnimationFrame(render);
    }
  }
  new host.MutationObserver(schedule).observe(doc.documentElement, {
    childList: true,
    subtree: true,
  });
  host.setInterval(() => {
    if (!doc.hidden) schedule();
  }, 30000);
  host.addEventListener("focus", schedule);
  render();
}
