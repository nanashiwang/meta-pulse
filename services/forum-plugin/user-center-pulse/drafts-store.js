/* Browser-only drafts. Native Answer and METAR share this exact implementation. */
(function (root, factory) {
  const api = factory();
  root.MetarDrafts = api;
  if (typeof module === "object" && module.exports) module.exports = api;
})(typeof window === "undefined" ? globalThis : window, function () {
  "use strict";
  const keys = { _a_dq_: "question", _a_da_: "answer" },
    marker = "|_a_t_s_|";
  const validID = (id) =>
    typeof id === "string" && /^[a-zA-Z0-9_-]{8,80}$/.test(id);
  function create(
    storage,
    owner,
    { now = Date.now, uuid = () => crypto.randomUUID() } = {},
  ) {
    if (!owner) throw Error("draft_owner_required");
    const prefix = `metar-draft-v1:${encodeURIComponent(owner)}:`;
    function read(id) {
      if (!validID(id)) return null;
      const raw = storage.getItem(prefix + id);
      if (!raw) return null;
      try {
        const v = JSON.parse(raw);
        if (
          v.id !== id ||
          !["question", "answer"].includes(v.kind) ||
          !Number.isFinite(v.updated) ||
          !Number.isSafeInteger(v.revision) || v.revision < 1 ||
          (v.kind === "question" && (typeof v.data?.title !== "string" || !Array.isArray(v.data?.tags))) ||
          (v.kind === "answer" && !/^[0-9]+$/.test(v.data?.questionId || "")) ||
          !v.data ||
          typeof v.data.content !== "string" ||
          JSON.stringify(v.data).length > 65536
        )
          return null;
        return v;
      } catch (_) {
        return null;
      }
    }
    function list() {
      const result = [];
      for (let i = 0; i < storage.length; i++) {
        const key = storage.key(i);
        if (key?.startsWith(prefix)) {
          const v = read(key.slice(prefix.length));
          if (v) result.push(v);
        }
      }
      return result.sort((a, b) => b.updated - a.updated);
    }
    function save(kind, data, id, expected) {
      if (
        !["question", "answer"].includes(kind) ||
        !data ||
        typeof data.content !== "string" ||
        JSON.stringify(data).length > 65536
      )
        throw Error("invalid_draft");
      if (kind === "answer" && !/^[0-9]+$/.test(data.questionId || ""))
        throw Error("invalid_draft");
      const previous = id ? read(id) : null;
      if (expected !== undefined && (previous?.revision || 0) !== expected)
        throw Error("draft_conflict");
      if (!previous && list().length >= 30) throw Error("draft_limit");
      id = id || uuid();
      if (!validID(id)) throw Error("invalid_draft");
      const value = {
        id,
        kind,
        data,
        updated: now(),
        revision: (previous?.revision || 0) + 1,
      };
      storage.setItem(prefix + id, JSON.stringify(value));
      return value;
    }
    function remove(id, expected) {
      const v = read(id);
      if (expected !== undefined && v?.revision !== expected)
        throw Error("draft_conflict");
      if (v) storage.removeItem(prefix + id);
    }
    function legacy() {
      const out = [];
      for (const [key, kind] of Object.entries(keys)) {
        const raw = storage.getItem(key);
        if (!raw) continue;
        const n = raw.indexOf(marker);
        if (n < 0 || Number(raw.slice(0, n)) <= now()) continue;
        try {
          const data = JSON.parse(raw.slice(n + marker.length));
          if (data && typeof data.content === "string")
            out.push({ key, kind, data });
        } catch (_) {}
      }
      return out;
    }
    function importLegacy() {
      for (const v of legacy()) {
        save(v.kind, v.data);
        storage.removeItem(v.key);
      }
    }
    return { read, list, save, remove, legacy, importLegacy };
  }
  function destination(v) {
    return v.kind === "answer"
      ? `/questions/${encodeURIComponent(v.data.questionId)}?metar_draft=${encodeURIComponent(v.id)}`
      : `/questions/ask?metar_draft=${encodeURIComponent(v.id)}`;
  }
  // Patch the host's storage adapter, never Storage.prototype or Answer source.
  function install(adapter, host) {
    const original = {
      get: adapter.get.bind(adapter),
      set: adapter.set.bind(adapter),
      remove: adapter.remove.bind(adapter),
    };
    let owner = "",
      library = null;
    const slots = {};
    function context(key) {
      const user = host.getUser();
      const next = user?.id ? String(user.id) : "";
      if (next !== owner) {
        owner = next;
        library = null;
        library = owner ? create(host.storage, owner) : null;
        for (const k of Object.keys(slots)) delete slots[k];
      }
      if (!library || user.mail_status !== 1 || user.status !== "normal")
        return null;
      const url = new URL(host.url());
      const q = url.pathname.match(/^\/questions\/(\d+)/)?.[1];
      const kind = keys[key];
      const scope =
        (kind === "answer" ? `answer:${q || ""}` : "question") +
        ":" +
        (url.searchParams.get("metar_draft") || "") +
        ":" +
        url.searchParams.toString();
      if (!slots[scope]) {
        const selected = url.searchParams.get("metar_draft");
        let v = selected && selected !== "new" ? library.read(selected) : null;
        if (
          v &&
          (v.kind !== kind || (kind === "answer" && v.data.questionId !== q))
        )
          v = null;
        if (
          !selected &&
          !url.searchParams.has("prefill") &&
          !url.searchParams.has("tags")
        )
          v = library
            .list()
            .find(
              (v) =>
                v.kind === kind &&
                (kind !== "answer" || v.data.questionId === q),
            );
        slots[scope] = { id: v?.id, revision: v?.revision || 0 };
      }
      return { library, slot: slots[scope], kind };
    }
    adapter.get = function (key) {
      if (!keys[key]) return original.get(key);
      try {
        const c = context(key);
        return c?.slot.id ? c.library.read(c.slot.id)?.data : null;
      } catch (e) {
        host.error(e);
        return null;
      }
    };
    adapter.set = function (key, data, time) {
      if (!keys[key]) return original.set(key, data, time);
      try {
        const c = context(key);
        if (!c) return;
        const v = c.library.save(c.kind, data, c.slot.id, c.slot.revision);
        c.slot.id = v.id;
        c.slot.revision = v.revision;
      } catch (e) {
        host.error(e);
      }
    };
    adapter.remove = function (key, cb) {
      if (!keys[key]) return original.remove(key, cb);
      try {
        const c = context(key);
        if (c?.slot.id) c.library.remove(c.slot.id, c.slot.revision);
        if (c) {
          c.slot.id = null;
          c.slot.revision = 0;
        }
        cb?.();
      } catch (e) {
        host.error(e);
      }
    };
    return () => Object.assign(adapter, original);
  }
  return { create, destination, install };
});
