const test = require("node:test"),
  assert = require("node:assert/strict"),
  vm = require("node:vm"),
  fs = require("node:fs");
const code = fs.readFileSync(require.resolve("../src/home-ads.js"), "utf8");
function fixture(items, broken = []) {
  let ticks = 0,
    calls = 0;
  const timers = new Map();
  const node = () => ({
    isConnected: true,
    hidden: true,
    setAttribute() {},
    replaceChildren(...children) {
      this.children = children;
    },
  });
  const slot = (pos) => Object.assign(node(), { dataset: { homeAd: pos } });
  const window = {};
  const document = {
    createElement(tag) {
      const n = node();
      if (tag === "img")
        Object.defineProperty(n, "src", {
          set(url) {
            this.url = url;
            broken.some((id) => url.includes("/" + id + "?"))
              ? this.onerror()
              : this.onload();
          },
        });
      return n;
    },
  };
  vm.runInNewContext(code, {
    window,
    document,
    setTimeout(fn, ms) {
      assert.equal(ms, 15000);
      timers.set(++ticks, fn);
      return ticks;
    },
    clearTimeout(id) {
      timers.delete(id);
    },
  });
  const mount = (slots) =>
    window.MetarAds.mountSlots(slots, {
      request: async () => {
        calls++;
        return items;
      },
    });
  return {
    slot,
    mount,
    timers,
    calls: () => calls,
    tick() {
      const f = [...timers.values()];
      timers.clear();
      f.forEach((fn) => fn());
    },
  };
}
const entry = (id, placement) => ({
  id,
  placement,
  link: "https://example.org/" + id,
});
test("two independent placements rotate, close only one, reload restores it", async () => {
  const h = fixture([
    entry(1, "top"),
    entry(2, "side"),
    entry(3, "top"),
    entry(4, "side"),
  ]);
  const top = h.slot("top"),
    side = h.slot("side");
  await h.mount([top, side]);
  assert.equal(h.calls(), 1);
  assert.equal(h.timers.size, 2);
  assert.equal(top.children[0].href, entry(1).link);
  assert.equal(side.children[0].href, entry(2).link);
  h.tick();
  assert.equal(top.children[0].href, entry(3).link);
  assert.equal(side.children[0].href, entry(4).link);
  top.children[2].onclick();
  assert.equal(top.hidden, true);
  assert.equal(h.timers.size, 1);
  h.tick();
  assert.equal(side.children[0].href, entry(2).link);
  const fresh = h.slot("top");
  await h.mount([fresh]);
  assert.equal(fresh.hidden, false);
});
test("old ads default to side; skip broken images without cycling forever or displaying empty space", async () => {
  const h = fixture([entry(1, "top"), entry(2), entry(3, "side")], [1, 2]);
  const top = h.slot("top"),
    side = h.slot("side");
  await h.mount([top, side]);
  assert.equal(top.hidden, true);
  assert.equal(side.hidden, false);
  assert.equal(side.children[0].href, entry(3).link);
  assert.equal(h.timers.size, 0);
});
test("published cards preserve originals, save notes privately, replace pictures and remove", async () => {
  const node = () => {
    const entries = {};
    return {
      children: [],
      setAttribute(k, v) {
        this[k] = v;
      },
      querySelector(k) {
        return (entries[k] ??= node());
      },
      get childElementCount() {
        return this.children.length;
      },
      replaceChildren(...v) {
        this.children = v;
      },
      append(v) {
        this.children.push(v);
      },
      replaceWith(v) {
        this.next = v;
      },
      remove() {
        this.removed = true;
      },
      focus() {},
    };
  };
  const el = node(),
    calls = [];
  const item = {
    id: "4",
    revision: "2",
    link: "https://example.org/",
    note: "旧备注",
  };
  const document = {
    createElement(tag) {
      const n = node();
      if (tag === "form")
        n.elements = {
          image: { files: [], focus() {} },
          note: {},
          link: {},
          placement: {},
        };
      if (tag === "canvas")
        Object.assign(n, {
          getContext: () => ({ fillRect() {}, drawImage() {} }),
          toBlob: (cb) => cb(new Blob(["JPEG"])),
        });
      return n;
    },
  };
  const window = { confirm: () => true };
  const admin = {
    request: async (p, o) => {
      if (!o) return p.endsWith("/preview") ? { jpeg: "abc" } : [item];
      calls.push([p, o]);
      return { id: "4", revision: "3" };
    },
  };
  vm.runInNewContext(code, {
    window,
    document,
    URL,
    FormData,
    FileReader: class {
      readAsDataURL() {
        this.result = "data:image/jpeg;base64,YQ==";
        this.onload();
      }
    },
    createImageBitmap: async () => ({ width: 200, height: 100, close() {} }),
  });
  window.MetarAds.mountManager(el, { base: "/answer/api/v1" }, admin);
  await new Promise(setImmediate);
  const card = el.querySelector("[data-cards]").children[0];
  assert.equal(card.elements.note.value, "旧备注");
  const img = card.querySelector("img");
  assert.match(img.src, /4\?v=2$/);
  await img.onerror();
  assert.equal(img.src, "data:image/jpeg;base64,abc");
  card.elements.placement.value = "top";
  card.elements.note.value = "新备注";
  card.elements.link.value = "https://new.example/";
  await card.onsubmit({ preventDefault() {} });
  assert.equal(calls[0][1].body.has("image"), false);
  assert.equal(calls[0][1].body.get("placement"), "top");
  assert.equal(card.next.elements.placement.value, "top");
  assert.equal(calls[0][1].body.get("note"), "新备注");
  assert.equal(calls[0][1].body.get("revision"), "2");
  assert.match(card.next.querySelector("img").src, /4\?v=3$/);
  const edit = card.next;
  edit.elements.image.files = [{ size: 10 }];
  await edit.elements.image.onchange();
  await edit.onsubmit({ preventDefault() {} });
  assert.ok(calls[1][1].body.has("image"));
  await edit.next.querySelector("[data-secondary]").onclick();
  el.querySelector("[data-new]").onclick();
  const draft = el.querySelector("[data-create]").children[0];
  draft.elements.link.value = item.link;
  draft.elements.image.files = [{ size: 10 }];
  await draft.elements.image.onchange();
  await draft.onsubmit({ preventDefault() {} });
  assert.deepEqual(
    calls.map(([p, o]) => [p, o.method, o.headers["X-Metar-Request"]]),
    [
      ["/metar/ads/4", "PUT", "1"],
      ["/metar/ads/4", "PUT", "1"],
      ["/metar/ads/4", "DELETE", "1"],
      ["/metar/ads", "POST", "1"],
    ],
  );
});
