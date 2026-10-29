import assert from "node:assert/strict";
import vm from "node:vm";
const base = process.env.ATBLSE_TEST_BASE,
  token = process.env.ATBLSE_TEST_TOKEN;
assert(base && token);
const html = await (await fetch(base + "/")).text();
const nodes = new Map(
  [...html.matchAll(/id="([^"]+)"/g)].map((x) => [
    x[1],
    {
      value: x[1] === "speed" ? "15" : x[1] === "seconds" ? "60" : "",
      hidden: false,
      disabled: false,
      textContent: "",
    },
  ]),
);
const intervals = [];
const context = vm.createContext({
  document: {
    getElementById: (id) => {
      assert(nodes.has(id), `missing${id}`);
      return nodes.get(id);
    },
  },
  sessionStorage: { getItem: () => token, setItem: () => {} },
  location: { hash: "", pathname: "/" },
  history: { replaceState: () => {} },
  URLSearchParams,
  AbortController,
  setTimeout,
  clearTimeout,
  setInterval: (fn, ms) => intervals.push({ fn, ms }),
  fetch: (url, init) => fetch(new URL(url, base), init),
});
vm.runInContext(await (await fetch(base + "/app.js")).text(), context);
const run = (code) => vm.runInContext(code, context),
  node = (id) => nodes.get(id);
for (let i = 0; i < 100 && run("refreshing"); i++) await new Promise((r) => setTimeout(r, 10));
assert.equal(node("login").hidden, true);
assert.equal(node("pair").hidden, false);
assert.equal(node("start").disabled, true);
await node("pair").onclick();
assert.equal(node("pair").hidden, true);
assert.equal(node("start").disabled, false);
await node("start").onclick();
assert(run("epoch"));
assert.match(node("runStatus").textContent, /Сеанс активен/);
await intervals.find((x) => x.ms === 400).fn();
let state = await (
  await fetch(base + "/api/status", { headers: { Authorization: "Bearer " + token } })
).json();
assert.equal(state.active, true);
assert.equal(state.state.targetSpeed, 15);
await node("stop").onclick();
assert.equal(run("epoch"), "");
assert.match(node("runStatus").textContent, /STOP подтверждён/);
state = await (
  await fetch(base + "/api/status", { headers: { Authorization: "Bearer " + token } })
).json();
assert.equal(state.stopConfirmed, true);
assert.equal(state.active, false);
console.log(
  JSON.stringify({
    diagnosticPanel: true,
    auth: true,
    pairing: true,
    start: true,
    keepalive: true,
    confirmedStop: true,
    physicalDevice: false,
  }),
);
