// Development only: exercise the real published SDK and staged Studio adapter.
import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
import path from "node:path";

const base = process.env.AUTOBLOW_TEST_BASE;
const token = process.env.AUTOBLOW_TEST_TOKEN;
const clients = process.env.AUTOBLOW_TEST_CLIENTS;
assert(base && token && clients, "contract harness environment missing");
const originalFetch = globalThis.fetch;
let requests = 0;
globalThis.fetch = async (input, init) => {
  const url = new URL(typeof input === "string" || input instanceof URL ? input : input.url);
  assert.equal(
    url.origin,
    new URL(base).origin,
    "external network request in offline contract test",
  );
  requests++;
  return originalFetch(input, init);
};
const { deviceInitFromCluster } = await import(
  pathToFileURL(path.join(clients, "node_modules/@xsense/autoblow-sdk/dist/deviceInit.js"))
);
const { AutoblowHttpClient, openEventStream } = await import(
  pathToFileURL(path.join(clients, "studio-httpClient.ts"))
);
const { vacuglide } = await deviceInitFromCluster(token, base, "vacuglide");
assert.equal((await vacuglide.getState()).operationalMode, "TARGET_SPEED_PAUSED");
assert.equal((await vacuglide.targetSpeedSet(15)).targetSpeed, 15);
assert.equal((await vacuglide.valveStrokePlusSet(true)).strokePlusValve, true);
assert.equal((await vacuglide.targetSpeedStop()).strokePlusValve, true);
await vacuglide.valveStrokePlusSet(false);
await vacuglide.targetSpeedStop();
assert(Number.isFinite(await vacuglide.estimateLatency(2)));
const studio = new AutoblowHttpClient({
  baseUrl: () => base,
  latencyUrl: () => base,
  token: () => token,
});
assert.equal((await studio.connected()).connected, true);
assert.equal((await studio.info()).deviceType, "vacuglide");
assert.equal((await studio.send({ kind: "speed", value: 12 })).state.targetSpeed, 12);
assert.equal(
  (await studio.send({ kind: "valve", valve: "minus", open: true })).state.strokeMinusValve,
  true,
);
assert.equal(
  (await studio.send({ kind: "motorStop" })).state.operationalMode,
  "TARGET_SPEED_PAUSED",
);
await studio.send({ kind: "valve", valve: "minus", open: false });
await studio.send({ kind: "motorStop" });
const state = (await studio.state()).state;
assert.equal(typeof state.syncScriptToken, "string");
assert.equal(Object.keys(state).length, 9);
let closeStream;
await new Promise((resolve, reject) => {
  const timeout = setTimeout(() => {
    closeStream?.();
    reject(new Error("Studio SSE connection timeout"));
  }, 3000);
  closeStream = openEventStream(base, token, {
    onEvent(event) {
      if (event.event === "connected") {
        clearTimeout(timeout);
        resolve();
      }
    },
    onClose(err) {
      if (err) {
        clearTimeout(timeout);
        reject(err);
      }
    },
  });
});
closeStream();
assert.equal((await vacuglide.localScriptList()).length, 30);
assert.equal((await vacuglide.localScriptSet(29)).localScript, 29);
assert.equal((await vacuglide.localScriptStop()).operationalMode, "LOCAL_SCRIPT_PAUSED");
assert.equal((await vacuglide.localScriptStart()).operationalMode, "LOCAL_SCRIPT_PLAYING");
await vacuglide.targetSpeedStop();
assert.equal((await vacuglide.localScriptStart()).operationalMode, "TARGET_SPEED_PAUSED");
assert.equal((await vacuglide.localScriptStop()).operationalMode, "TARGET_SPEED_PAUSED");
assert.equal((await studio.send({ kind: "script", index: 2 })).state.localScript, 2);
await studio.send({ kind: "motorStop" });
const source = {
  actions: [
    { at: 0, pos: 0 },
    { at: 1000, pos: 90 },
    { at: 2000, pos: 0 },
  ],
};
const uploaded = await vacuglide.syncScriptUploadFunscriptFile(source);
assert.equal(uploaded.operationalMode, "SYNC_SCRIPT_PAUSED");
assert.match(uploaded.syncScriptToken, /^[0-9a-f-]{36}$/);
assert.equal((await vacuglide.syncScriptStart(500)).operationalMode, "SYNC_SCRIPT_PLAYING");
assert.equal((await vacuglide.syncScriptOffset(-100)).syncScriptOffsetTime, -100);
assert.equal((await vacuglide.syncScriptLoop(true)).syncScriptLoop, true);
assert.equal((await vacuglide.syncScriptStop()).operationalMode, "SYNC_SCRIPT_PAUSED");
assert.equal(
  (await vacuglide.syncScriptLoadToken(uploaded.syncScriptToken)).syncScriptToken,
  uploaded.syncScriptToken,
);
assert.equal(
  (await vacuglide.syncScriptUploadCsvFile("at,pos\n0,0\n1000,90\n2000,0\n")).operationalMode,
  "SYNC_SCRIPT_PAUSED",
);
const studioUpload = await studio.syncUpload(JSON.stringify(source), "independent-test");
assert.equal(studioUpload.state.syncScriptToken, studioUpload.token);
assert.equal(
  (await studio.send({ kind: "syncStart", timeMs: 500 })).state.operationalMode,
  "SYNC_SCRIPT_PLAYING",
);
assert.equal(
  (await studio.send({ kind: "syncOffset", offsetMs: 100 })).state.syncScriptOffsetTime,
  100,
);
assert.equal((await studio.send({ kind: "syncStop" })).state.operationalMode, "SYNC_SCRIPT_PAUSED");
await vacuglide.targetSpeedStop();
assert.equal(
  (
    await vacuglide.syncScriptUploadFunscriptUrl(
      process.env.AUTOBLOW_TEST_SCRIPT_SOURCE + "/script",
    )
  ).operationalMode,
  "SYNC_SCRIPT_PAUSED",
);
assert.equal(
  (await vacuglide.syncScriptUploadCsvUrl(process.env.AUTOBLOW_TEST_SCRIPT_SOURCE + "/csv"))
    .operationalMode,
  "SYNC_SCRIPT_PAUSED",
);
await vacuglide.targetSpeedStop();
assert.equal((await vacuglide.getState()).operationalMode, "TARGET_SPEED_PAUSED");
const slotsBefore = await vacuglide.localScriptList();
const swapped = await vacuglide.localScriptSwap(0, 29);
assert.equal(swapped[0], slotsBefore[29]);
assert.equal(swapped[29], slotsBefore[0]);
const moved = await vacuglide.localScriptMove(29, 0);
assert.equal(moved[0], slotsBefore[0]);
assert.equal(moved[1], slotsBefore[29]);
const replaced = await vacuglide.localScriptReplace(1, uploaded.syncScriptToken);
assert.equal(replaced.length, 30);
assert(Number.isInteger(replaced[1]) && replaced[1] > 0 && replaced[1] <= 0xffffffff);
assert(process.env.AUTOBLOW_TEST_CATALOG_UUID, "catalog fixture required");
const catalogReplaced = await vacuglide.localScriptReplace(
  0,
  process.env.AUTOBLOW_TEST_CATALOG_UUID,
);
assert.equal(catalogReplaced[0], 1, "original catalog identity was lost");
assert.equal((await vacuglide.getState()).operationalMode, "LOCAL_SCRIPT_PAUSED");
await vacuglide.targetSpeedStop();
const fractional = await vacuglide.syncScriptUploadFunscriptFile({
  actions: [
    { at: 0.9, pos: 0.9 },
    { at: 1000.9, pos: 3.1 },
    { at: 2000.9, pos: 0.9 },
  ],
});
assert.equal(fractional.operationalMode, "SYNC_SCRIPT_PAUSED");
// SDK 2.6.0 rounds startTimeMs before making its request; direct REST keeps fractions.
assert.equal((await vacuglide.syncScriptStart(0.9)).syncScriptCurrentTime, 1);
assert.equal((await vacuglide.syncScriptOffset(-1.9)).syncScriptOffsetTime, -1);
assert.equal((await vacuglide.targetSpeedSet(15.9)).targetSpeed, 15);
assert.equal((await vacuglide.targetSpeedSet(1e-7)).targetSpeed, 1);
assert.equal((await vacuglide.localScriptSet(1.9)).localScript, 1);
await vacuglide.targetSpeedStop();
console.log(
  JSON.stringify({
    sdk: "2.6.0",
    studioAdapter: true,
    manual: true,
    valves: true,
    clock: true,
    sseHandshake: true,
    requests,
    scriptUploadAndControl: true,
    localListAndControl: true,
    localSlotMutations: true,
    catalogReplacement: true,
    fractionalAndScientificArguments: true,
    scriptURLUpload: true,
    physicalDevice: false,
  }),
);
