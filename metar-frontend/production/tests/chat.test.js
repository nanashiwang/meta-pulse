const test = require("node:test");
const assert = require("node:assert/strict");
const {
  Client,
  mergeMessages,
  messageHTML,
  errorText,
} = require("../src/chat.js");
test("incremental changes replace recalled content and ignore stale responses", () => {
  const original = { seq: 1, version: 1, body: "private" };
  const removed = { seq: 1, version: 4, body: "", removed: true };
  const merged = mergeMessages(
    [original, { seq: 2, version: 2 }],
    [removed, { seq: 3, version: 3 }],
  );
  assert.deepEqual(
    merged.map((m) => m.seq),
    [1, 2, 3],
  );
  assert.equal(merged[0].body, "");
  assert.equal(mergeMessages(merged, [original])[0].removed, true);
});
test("message renderer escapes all untrusted text and does not emit removed bodies", () => {
  const m = {
    seq: 1,
    version: 1,
    sender: { id: "2", display_name: "<img src=x onerror=alert(1)>" },
    body: '<script>alert(1)</script> & "text"',
    created_at: 1,
  };
  const html = messageHTML(m, "3");
  assert.ok(!html.includes("<script>"));
  assert.ok(!html.includes("<img"));
  assert.ok(html.includes("&lt;script&gt;"));
  assert.ok(
    !messageHTML({ ...m, removed: true }, "3").includes("alert(1)&lt;/script"),
  );
});
test("send retries preserve authenticated transport and the supplied key", async () => {
  const calls = [];
  const api = {
    request: async (...args) => {
      calls.push(args);
      if (calls.length === 1) throw new Error("response lost");
      return { seq: 1 };
    },
  };
  const client = new Client(api, api),
    command = { op: "send", room_id: "12", body: "hello" };
  await assert.rejects(client.command(command, "stable-request"));
  await client.command(command, "stable-request");
  assert.deepEqual(calls[0], calls[1]);
  assert.equal(calls[0][1].headers["X-Metar-Request"], "1");
  assert.equal(calls[0][0], "/metar/chat/command");
  assert.equal(calls[0][1].headers["Idempotency-Key"], "stable-request");
});
test("private reads never use the public content cache and query values are encoded", async () => {
  let path;
  const client = new Client({ request: async (p) => ((path = p), {}) });
  await client.get("people", { q: "a&admin=1" });
  assert.equal(path, "/metar/chat/people?q=a%26admin%3D1");
  assert.notEqual(
    errorText({ details: { error: "contact_unavailable" } }),
    errorText({ status: 500 }),
  );
});

test("closed group owners and members can leave without destructive group controls", () => {
  const { View } = require("../src/chat.js");
  for (const user of ["2", "3"]) {
    const view = new View({}, {}, { id: user });
    view.room = {
      id: "12",
      kind: "group",
      title: "closed",
      owner_id: "2",
      closed: true,
      members: [],
    };
    let content;
    view.dialog = (_, body) => {
      content = body;
    };
    view.members();
    assert.ok(content.includes('data-chat="confirm-leave"'));
    assert.ok(!content.includes('data-chat="confirm-close"'));
    assert.ok(!content.includes('data-chat="invite"'));
    view.confirm("leave");
    assert.ok(content.includes("无法再次查看其聊天记录"));
    assert.deepEqual(view.confirmCommand, { op: "leave", room_id: "12" });
  }
});
