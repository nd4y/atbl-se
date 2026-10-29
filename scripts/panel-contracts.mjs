import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
const source = fs.readFileSync(new URL("../core/web/app.js", import.meta.url), "utf8");
const ready = {
  connected: true,
  ready: true,
  paired: true,
  generation: 1,
  maxSpeed: 15,
  active: false,
  stopping: false,
  stopConfirmed: true,
  firmware: "1.03",
  mac: "test",
  state: { operationalMode: "TARGET_SPEED_PAUSED", targetSpeed: 0 },
};
const deferred = () => {
  let resolve;
  return { promise: new Promise((r) => (resolve = r)), resolve };
};
async function panel(handler) {
  const nodes = new Map(),
    timers = [];
  const $ = (id) => {
    if (!nodes.has(id))
      nodes.set(id, {
        value: id === "speed" ? "15" : id === "seconds" ? "60" : "",
        textContent: "",
        hidden: false,
        disabled: false,
      });
    return nodes.get(id);
  };
  const context = vm.createContext({
    document: { getElementById: $ },
    sessionStorage: { getItem: () => "test-key", setItem: () => {} },
    history: { replaceState: () => {} },
    location: { hash: "", pathname: "/" },
    URLSearchParams,
    AbortController,
    setTimeout,
    clearTimeout,
    setInterval: (fn, ms) => timers.push({ fn, ms }),
    fetch: async (url, opts) => {
      const r = await handler(url, opts);
      return { ok: r.code === 200, status: r.code, json: async () => r.body };
    },
  });
  vm.runInContext(source, context);
  await new Promise(setImmediate);
  return { $, context, timers, eval: (code) => vm.runInContext(code, context) };
}
let checks = 0;
{
  const start = deferred();
  const p = await panel(async (url) =>
    url.endsWith("/start")
      ? start.promise
      : { code: 200, body: url.endsWith("/status") ? ready : { stopped: true } },
  );
  const pending = p.$("start").onclick();
  await new Promise(setImmediate);
  await p.eval("refresh()");
  assert.equal(p.$("start").disabled, true, "poll must not reenable a pending START");
  checks++;
  await p.$("stop").onclick();
  start.resolve({ code: 200, body: { controlEpoch: "late-start" } });
  await pending;
  assert.equal(p.eval("epoch"), "", "late START response must not resurrect renewal after STOP");
  assert.match(p.$("runStatus").textContent, /STOP/);
  checks++;
}
{
  let unauthorized = false;
  const p = await panel(async () =>
    unauthorized
      ? { code: 401, body: { error: "local authorization required" } }
      : { code: 200, body: ready },
  );
  assert.equal(p.$("login").hidden, true);
  unauthorized = true;
  await p.eval("refresh()");
  assert.equal(p.$("login").hidden, false, "expired credentials must allow sign-in again");
  assert.equal(p.$("start").disabled, true);
  checks++;
}
{
  let delayed = null;
  const p = await panel(async (url) =>
    url.endsWith("/status") && delayed
      ? delayed.promise
      : { code: 200, body: url.endsWith("/status") ? ready : { stopped: true } },
  );
  delayed = deferred();
  const poll = p.eval("refresh()");
  await new Promise(setImmediate);
  await p.$("stop").onclick();
  delayed.resolve({ code: 200, body: { ...ready, active: true, compatibilityControl: true } });
  await poll;
  assert.match(
    p.$("runStatus").textContent,
    /STOP/,
    "stale poll must not replace newer STOP acknowledgement",
  );
  checks++;
}
{
  let status = { ...ready, active: true, compatibilityControl: true };
  const p = await panel(async () => ({ code: 200, body: status }));
  assert.match(p.$("runStatus").textContent, /внешнее приложение/);
  status = { ...ready };
  await p.eval("refresh()");
  assert.equal(p.$("runStatus").textContent, "");
  status = { ...ready, active: true, compatibilityControl: true };
  await p.eval("refresh()");
  status = { connected: false };
  await p.eval("refresh()");
  assert.match(p.$("runStatus").textContent, /не подтверждена/);
  checks++;
}
{
  const oldReply = deferred(),
    newReply = deferred();
  let state = { ...ready },
    starts = 0,
    renewals = 0;
  const p = await panel(async (url) => {
    if (url.endsWith("/start")) {
      state = { ...ready, active: true };
      return { code: 200, body: { controlEpoch: "epoch-" + ++starts } };
    }
    if (url.endsWith("/stop")) {
      state = { ...ready };
      return { code: 200, body: { stopped: true } };
    }
    if (url.endsWith("/keepalive")) return ++renewals === 1 ? oldReply.promise : newReply.promise;
    return { code: 200, body: state };
  });
  await p.$("start").onclick();
  const tick = p.timers.find((x) => x.ms === 400).fn;
  const old = tick();
  let next;
  try {
    await p.$("stop").onclick();
    await p.$("start").onclick();
    next = tick();
    await new Promise(setImmediate);
    assert.equal(renewals, 2, "hung renewal from previous session blocks new lease");
    oldReply.resolve({ code: 401, body: { error: "old request unauthorized" } });
    await old;
    assert.equal(p.eval("epoch"), "epoch-2", "old renewal clears the new session");
    assert.equal(p.eval("renewing"), "epoch-2", "old finalizer clears newer pending renewal");
    checks++;
  } finally {
    oldReply.resolve({ code: 200, body: {} });
    newReply.resolve({ code: 200, body: {} });
    await Promise.allSettled([old, next]);
  }
}
console.log(JSON.stringify({ panelRaceAndAuthChecks: checks, physicalDevice: false }));
