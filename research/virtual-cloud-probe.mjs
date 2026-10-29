// Explicitly approved cloud protocol capture with the real device UNPLUGGED.
// This virtual client is evidence collection, not a firmware behavior oracle.
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL, fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
const plan = JSON.parse(
  fs.readFileSync(new URL("./virtual-probe-plan.json", import.meta.url), "utf8"),
);
if (process.argv.includes("--execute") && process.argv.includes("--dry-run"))
  throw new Error("Choose execution or dry-run, not both");
const allowed = new Set([
  "GET /vacuglide/info",
  "GET /vacuglide/state",
  "GET /vacuglide/local-script/loaded",
  "PUT /vacuglide/target-speed",
  "PUT /vacuglide/target-speed/stop",
  "PUT /vacuglide/valve/stroke-plus",
  "PUT /vacuglide/valve/stroke-minus",
  "PUT /vacuglide/local-script",
  "PUT /vacuglide/local-script/start",
  "PUT /vacuglide/local-script/stop",
  "PUT /vacuglide/sync-script/upload-funscript",
  "PUT /vacuglide/sync-script/start",
  "PUT /vacuglide/sync-script/stop",
  "PUT /vacuglide/sync-script/offset",
  "PUT /vacuglide/sync-script/loop",
]);
if (plan.some((step) => !allowed.has(step.method + " " + step.path)))
  throw new Error("Unapproved operation in probe plan");
if (!process.argv.includes("--execute")) {
  console.log(
    JSON.stringify(
      {
        dryRun: true,
        cloudWrites: false,
        requires: "explicit human approval and real device unplugged",
        operations: plan.map(({ method, path }) => ({ method, path })),
      },
      null,
      2,
    ),
  );
  process.exit(0);
}
if (process.env.VACUGLIDE_PROBE_POWER_OFF !== "CONFIRMED")
  throw new Error("Human confirmation that the real device is unplugged is required");
const host = process.env.VACUGLIDE_PROBE_CLUSTER || "eu-central-1.autoblowapi.com";
if (
  host !== "latency.autoblowapi.com" &&
  ![
    "eu-central-1",
    "eu-west-2",
    "us-east-1",
    "us-east-2",
    "us-west-1",
    "us-west-2",
    "ca-central-1",
    "ap-southeast-2",
  ].some((x) => host === x + ".autoblowapi.com")
)
  throw new Error("Unexpected cloud endpoint");
const tokenFile = process.env.VACUGLIDE_PROBE_TOKEN_FILE;
const headersFile = process.env.VACUGLIDE_PROBE_HEADERS_FILE;
const mac = (process.env.VACUGLIDE_PROBE_MAC || "").replaceAll(":", "").toLowerCase();
const output = process.env.VACUGLIDE_PROBE_OUTPUT;
const deps = process.env.VACUGLIDE_PROBE_DEPENDENCIES;
if (
  !tokenFile ||
  !headersFile ||
  !output ||
  !deps ||
  !path.isAbsolute(output) ||
  !/^[a-f0-9]{12}$/.test(mac)
)
  throw new Error("Missing private probe configuration");
const token = JSON.parse(fs.readFileSync(tokenFile, "utf8")).deviceToken;
if (typeof token !== "string" || token.length < 5) throw new Error("Device token unavailable");
const captured = JSON.parse(fs.readFileSync(headersFile, "utf8"));
const deviceHeaderNames = [
  "x-device-mac",
  "x-device-token",
  "x-hardware-version",
  "x-firmware-version",
  "x-firmware-branch",
  "x-private-key",
  "user-agent",
];
if (
  deviceHeaderNames.some(
    (name) =>
      typeof captured[name] !== "string" || !captured[name] || /[\r\n]/.test(captured[name]),
  )
)
  throw new Error("Captured device headers unavailable");
if (
  captured["x-device-token"] !== token ||
  captured["x-device-mac"].replaceAll(":", "").toLowerCase() !== mac
)
  throw new Error("Captured device identity mismatch");
const deviceHeaders = Object.fromEntries(deviceHeaderNames.map((name) => [name, captured[name]]));
const { default: WebSocket } = await import(pathToFileURL(path.join(deps, "ws/index.js")));
const ac = new AbortController();
const deadline = setTimeout(() => ac.abort(new Error("Probe lifetime exceeded")), 90000);
let ws,
  bytes = 0,
  closed = false;
const state = { mode: 50, speed: 0, plus: 0, minus: 0, offset: 0, loop: 0, scriptToken: "" };
function redact(value) {
  if (typeof value === "string") {
    for (const [secret, label] of [
      [token, "[device-token]"],
      [mac, "[device-mac]"],
      [captured["x-private-key"], "[private-key]"],
    ]) {
      if (value === secret) value = label;
      else if (secret.length >= 8) value = value.replaceAll(secret, label);
    }
    return value;
  }
  if (Array.isArray(value)) return value.map(redact);
  if (value && typeof value === "object")
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, redact(item)]));
  return value;
}
function record(kind, data) {
  let line = JSON.stringify(redact({ at: new Date().toISOString(), kind, data })) + "\n";
  bytes += Buffer.byteLength(line);
  if (bytes > 131072) {
    ac.abort(new Error("Capture bound reached"));
    return;
  }
  fs.appendFileSync(path.join(output, "capture.ndjson"), line, { mode: 0o600 });
}
async function request(step) {
  if (closed) throw new Error("Virtual device disconnected; no more API calls");
  for (const field of ["mode", "speed", "plus", "minus", "offset", "loop"])
    if (field in step) state[field] = step[field];
  const headers = { "x-device-token": token };
  let body;
  if (step.file) {
    body = new FormData();
    body.append(
      "file",
      new Blob([JSON.stringify(step.file)], { type: "application/json" }),
      "synthetic.funscript",
    );
  } else if (step.body) {
    headers["content-type"] = "application/json";
    body = JSON.stringify(step.body);
  }
  const signal = AbortSignal.any([ac.signal, AbortSignal.timeout(6000)]);
  const r = await fetch("https://" + host + step.path, {
    method: step.method,
    headers,
    body,
    signal,
  });
  const text = await r.text();
  record("http-response", {
    method: step.method,
    path: step.path,
    status: r.status,
    body: text.slice(0, 4096),
  });
  return { status: r.status, body: JSON.parse(text) };
}
try {
  const check = await fetch("https://" + host + "/vacuglide/connected", {
    headers: { "x-device-token": token },
    signal: ac.signal,
  });
  if (!check.ok) throw new Error("Cannot verify cloud connection baseline");
  const baseline = await check.json();
  if (baseline.connected !== false) throw new Error("Real device is online; probe refused");
  fs.mkdirSync(output, { recursive: false, mode: 0o700 });
  record("baseline", {
    connected: false,
    host,
    planSha256: createHash("sha256").update(JSON.stringify(plan)).digest("hex"),
    replies: "synthetic states; never firmware acceptance",
  });
  ws = new WebSocket("wss://" + host + "/device-backend/ws", {
    headers: deviceHeaders,
    handshakeTimeout: 10000,
  });
  ac.signal.addEventListener("abort", () => ws.terminate(), { once: true });
  ws.on("close", () => {
    closed = true;
  });
  ws.on("message", (data) => {
    const text = data.toString();
    record("official-server-command", text.slice(0, 8192));
    const parts = text.split("/");
    const id = parts.at(-1);
    if (
      !/^[a-f0-9-]{36}$/.test(id) ||
      parts[1] === "connected-ok" ||
      /firmware/i.test(parts[1] || "")
    )
      return;
    if (/sync.*load/i.test(parts[1] || "")) {
      const candidate = parts.slice(2, -1).find((x) => /^[a-f0-9-]{36}$/.test(x));
      if (candidate) state.scriptToken = candidate;
    }
    const reply = `/state/${state.mode}/0/${state.speed}/${state.plus}/${state.minus}/0/${state.offset}/${state.loop}/${state.scriptToken}/${id}`;
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(reply);
      record("synthetic-client-reply", reply);
    }
  });
  await new Promise((resolve, reject) => {
    ws.once("open", resolve);
    ws.once("error", reject);
  });
  await new Promise((resolve) => setTimeout(resolve, 1200));
  for (const step of plan) {
    if (ac.signal.aborted || closed) break;
    try {
      await request(step);
    } catch (error) {
      record("operation-error", { path: step.path, error: error.name });
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
} finally {
  clearTimeout(deadline);
  if (ws) {
    ws.close();
    await new Promise((resolve) => setTimeout(resolve, 1200));
    if (ws.readyState !== WebSocket.CLOSED) ws.terminate();
  }
}
console.log(
  JSON.stringify({ captureSaved: true, bytes, physicalDevice: false, firmwareUpdated: false }),
);
