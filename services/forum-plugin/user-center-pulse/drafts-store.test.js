import test from "node:test";
import assert from "node:assert/strict";
import "./drafts-store.js";
const { create, install, destination } = globalThis.MetarDrafts;
function storage() {
  const m = new Map();
  return {
    getItem: (k) => m.get(k) || null,
    setItem: (k, v) => m.set(k, v),
    removeItem: (k) => m.delete(k),
    get length() {
      return m.size;
    },
    key: (i) => [...m.keys()][i],
  };
}
const data = {
  title: "<script>题目",
  content: "draft",
  tags: [],
  answer_content: "",
};
test("drafts isolate users, support multiple questions and detect stale editor writes", () => {
  const disk = storage(),
    a = create(disk, "alice"),
    b = create(disk, "bob");
  const first = a.save("question", data),
    second = a.save("question", { ...data, title: "second" });
  assert.equal(a.list().length, 2);
  assert.equal(b.list().length, 0);
  assert.equal(b.read(first.id), null);
  a.save("question", { ...data, content: "new" }, first.id, 1);
  assert.throws(() => a.save("question", data, first.id, 1), /conflict/);
  assert.throws(() => a.remove(first.id, 1), /conflict/);
  a.remove(second.id, 1);
  assert.equal(a.list().length, 1);
  assert.ok(destination(first).startsWith("/questions/ask?metar_draft="));
});
test("native resume saves into the selected draft and publishing removes only that draft", () => {
  const disk = storage(),
    lib = create(disk, "alice"),
    first = lib.save("question", data),
    other = lib.save("question", { ...data, title: "other" });
  const adapter = { get: () => 42, set: () => {}, remove: (_k, cb) => cb?.() };
  let url = "https://test" + destination(first),
    user = { id: "alice", username: "alice", status: "normal", mail_status: 1 };
  install(adapter, {
    storage: disk,
    getUser: () => user,
    url: () => url,
    error: (e) => {
      throw e;
    },
  });
  assert.equal(adapter.get("_a_dq_").title, data.title);
  adapter.set("_a_dq_", { ...data, content: "edited" });
  assert.equal(lib.read(first.id).data.content, "edited");
  adapter.remove("_a_dq_");
  assert.equal(lib.read(first.id), null);
  assert.equal(lib.read(other.id).data.title, "other");
  url = "https://test/questions/ask?metar_draft=new";
  assert.equal(adapter.get("_a_dq_"), null);
  adapter.set("_a_dq_", data);
  assert.equal(lib.list().length, 2);
  user = { id: "bob", username: "bob", status: "normal", mail_status: 1 };
  assert.equal(adapter.get("_a_dq_"), null);
  assert.equal(adapter.get("unrelated"), 42);
});
test("answers resume only on their question; legacy drafts require explicit import", () => {
  const disk = storage();
  disk.setItem(
    "_a_dq_",
    `${Date.now() + 60000}|_a_t_s_|${JSON.stringify(data)}`,
  );
  const lib = create(disk, "alice");
  assert.equal(lib.list().length, 0);
  assert.equal(lib.legacy().length, 1);
  lib.importLegacy();
  assert.equal(lib.list().length, 1);
  assert.equal(disk.getItem("_a_dq_"), null);
  const answer = lib.save("answer", { content: "answer", questionId: "42" });
  let url = "https://test/questions/43?metar_draft=" + answer.id;
  const adapter = { get: () => null, set: () => {}, remove: () => {} };
  install(adapter, {
    storage: disk,
    getUser: () => ({
      id: "alice",
      username: "alice",
      mail_status: 1,
      status: "normal",
    }),
    url: () => url,
    error: (e) => {
      throw e;
    },
  });
  assert.equal(adapter.get("_a_da_"), null);
  url = "https://test" + destination(answer);
  assert.equal(adapter.get("_a_da_").questionId, "42");
});
test("storage failures and corrupt records do not invent a saved draft", () => {
  const disk = storage(),
    lib = create(disk, "alice");
  disk.setItem("metar-draft-v1:alice:invalid-id", "broken");
  assert.equal(lib.list().length, 0);
  assert.throws(
    () =>
      create(
        {
          getItem() {
            throw Error("blocked");
          },
        },
        "alice",
      ).read("valid-id1"),
    /blocked/,
  );
  assert.throws(
    () => lib.save("answer", { content: "x", questionId: "../../admin" }),
    /invalid/,
  );
});

test("prefilled new questions never overwrite a previous draft and account renames keep stable ownership", () => {
  const disk = storage(),
    lib = create(disk, "42");
  const saved = lib.save("question", data);
  const adapter = { get: () => null, set: () => {}, remove: () => {} };
  const user = {
    id: "42",
    username: "renamed",
    mail_status: 1,
    status: "normal",
  };
  install(adapter, {
    storage: disk,
    getUser: () => user,
    url: () => "https://test/questions/ask?prefill=anything",
    error: (e) => {
      throw e;
    },
  });
  assert.equal(adapter.get("_a_dq_"), null);
  adapter.set("_a_dq_", { ...data, title: "prefilled" });
  assert.equal(lib.list().length, 2);
  assert.equal(lib.read(saved.id).data.title, data.title);
});

test('blocked storage during account change cannot reuse the previous account library', () => {
  const disk = storage();
  create(disk, 'alice').save('question', data);
  let user = {id:'alice', username:'alice', status:'normal', mail_status:1}, blocked=false;
  const adapter={get:()=>null,set:()=>{},remove:()=>{}};
  install(adapter,{get storage(){if(blocked)throw Error('blocked');return disk},getUser:()=>user,url:()=> 'https://test/questions/ask',error:()=>{}});
  assert.equal(adapter.get('_a_dq_').content,'draft');
  user={...user,id:'bob',username:'bob'};blocked=true;
  assert.equal(adapter.get('_a_dq_'),null);
  assert.equal(adapter.get('_a_dq_'),null);
});
