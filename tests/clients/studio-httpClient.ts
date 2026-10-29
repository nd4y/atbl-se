import {
  DeviceError,
  type DeviceClient,
  type DeviceCommand,
  type DeviceInfo,
  type DeviceResult,
  type DeviceState,
  type RateHeaders,
  type SyncUploadResult,
} from "./studio-device-types.ts";

export interface HttpClientOptions {
  /** Базовый URL кластера (без завершающего слэша) или null, если кластер ещё не известен. */
  baseUrl: () => string | null;
  /** URL для /vacuglide/connected (latency-эндпоинт или сам симулятор). */
  latencyUrl: () => string;
  token: () => string | null;
  timeoutMs?: number;
  fetchImpl?: typeof fetch;
}

function parseRateHeaders(h: Headers): RateHeaders | undefined {
  const out: RateHeaders = {};
  let any = false;
  h.forEach((value, key) => {
    const k = key.toLowerCase();
    const m = /^(?:x-)?ratelimit-(limit|remaining|reset)$/.exec(k);
    if (!m) return;
    const n = Number(value);
    if (!Number.isFinite(n)) return;
    any = true;
    if (m[1] === "limit") out.limit = n;
    else if (m[1] === "remaining") out.remaining = n;
    else out.reset = n;
  });
  return any ? out : undefined;
}

/**
 * HTTP-клиент Autoblow API по документации:
 * заголовок x-device-token, JSON-тело только у PUT с параметрами, 502 = устройство офлайн, 429 = лимит.
 */
export class AutoblowHttpClient implements DeviceClient {
  private fetchImpl: typeof fetch;
  private timeoutMs: number;

  constructor(private opts: HttpClientOptions) {
    this.fetchImpl = opts.fetchImpl ?? fetch;
    this.timeoutMs = opts.timeoutMs ?? 10_000;
  }

  async connected(): Promise<{ connected: boolean; cluster?: string }> {
    const data = await this.request<{ connected: boolean; cluster?: string }>(
      "GET",
      "/vacuglide/connected",
      undefined,
      this.opts.latencyUrl(),
    );
    return data.body;
  }

  async info(): Promise<DeviceInfo> {
    return (await this.request<DeviceInfo>("GET", "/vacuglide/info")).body;
  }

  async state(): Promise<DeviceResult> {
    const r = await this.request<DeviceState>("GET", "/vacuglide/state");
    return { state: r.body, ...(r.rate ? { rate: r.rate } : {}) };
  }

  async send(cmd: DeviceCommand): Promise<DeviceResult> {
    let r: { body: DeviceState; rate?: RateHeaders };
    switch (cmd.kind) {
      case "speed":
        r = await this.request<DeviceState>("PUT", "/vacuglide/target-speed", {
          targetSpeed: cmd.value,
        });
        break;
      case "motorStop":
        r = await this.request<DeviceState>("PUT", "/vacuglide/target-speed/stop");
        break;
      case "valve":
        r = await this.request<DeviceState>(
          "PUT",
          cmd.valve === "plus" ? "/vacuglide/valve/stroke-plus" : "/vacuglide/valve/stroke-minus",
          { valveState: cmd.open },
        );
        break;
      case "script":
        r = await this.request<DeviceState>("PUT", "/vacuglide/local-script", {
          localScriptIndex: cmd.index,
        });
        break;
      case "syncStart":
        r = await this.request<DeviceState>("PUT", "/vacuglide/sync-script/start", {
          startTimeMs: cmd.timeMs,
        });
        break;
      case "syncStop":
        r = await this.request<DeviceState>("PUT", "/vacuglide/sync-script/stop");
        break;
      case "syncOffset":
        r = await this.request<DeviceState>("PUT", "/vacuglide/sync-script/offset", {
          offsetTimeMs: cmd.offsetMs,
        });
        break;
    }
    return { state: r.body, ...(r.rate ? { rate: r.rate } : {}) };
  }

  /** Загрузка funscript: multipart/form-data с полем file, ответ содержит syncScriptToken. Таймаут по документации от минуты. */
  async syncUpload(funscriptJson: string, name: string): Promise<SyncUploadResult> {
    const form = new FormData();
    const fileName = /\.funscript$/i.test(name) ? name : `${name || "script"}.funscript`;
    form.append("file", new Blob([funscriptJson], { type: "application/json" }), fileName);
    const r = await this.request<DeviceState>(
      "PUT",
      "/vacuglide/sync-script/upload-funscript",
      form,
      undefined,
      120_000,
    );
    const token = typeof r.body.syncScriptToken === "string" ? r.body.syncScriptToken : "";
    if (!token) throw new DeviceError("http", "upload returned no script token");
    return { state: r.body, token, ...(r.rate ? { rate: r.rate } : {}) };
  }

  private async request<T>(
    method: "GET" | "PUT",
    path: string,
    body?: unknown,
    baseOverride?: string,
    timeoutMs?: number,
  ): Promise<{ body: T; rate?: RateHeaders }> {
    const token = this.opts.token();
    if (!token) throw new DeviceError("noToken", "device token is not set");
    const base = baseOverride ?? this.opts.baseUrl();
    if (!base) throw new DeviceError("offline", "cluster unknown");
    const headers: Record<string, string> = { "x-device-token": token };
    let payload: string | FormData | undefined;
    if (body instanceof FormData) {
      // Границу multipart выставляет fetch сам.
      payload = body;
    } else if (body !== undefined) {
      headers["content-type"] = "application/json";
      payload = JSON.stringify(body);
    }
    const ac = new AbortController();
    const timer = setTimeout(() => ac.abort(), timeoutMs ?? this.timeoutMs);
    let result: { body: T; rate?: RateHeaders };
    try {
      const res = await this.fetchImpl(`${base}${path}`, {
        method,
        headers,
        body: payload,
        signal: ac.signal,
      });
      const rate = parseRateHeaders(res.headers);
      if (res.status === 429)
        throw new DeviceError("rateLimited", "rate limit exceeded", 429, rate);
      if (res.status === 502) throw new DeviceError("offline", "device is offline", 502, rate);
      const text = await res.text();
      if (!res.ok)
        throw new DeviceError("http", `HTTP ${res.status} ${text.slice(0, 200)}`, res.status, rate);
      let parsed: T;
      try {
        parsed = (text ? JSON.parse(text) : {}) as T;
      } catch {
        throw new DeviceError("http", "invalid JSON response", res.status, rate);
      }
      result = rate ? { body: parsed, rate } : { body: parsed };
    } catch (e) {
      if (e instanceof DeviceError) throw e;
      throw new DeviceError("network", e instanceof Error ? e.message : String(e));
    } finally {
      // The deadline covers receiving the body as well as response headers.
      clearTimeout(timer);
    }
    return result;
  }
}

export interface SseEvent {
  event: string;
  data: string;
}

/**
 * Подписка на /events/stream. Токен по документации идёт только в query, поэтому стрим открывает
 * только сервер, и URL никогда не логируется. Возвращает функцию отмены.
 */
export interface EventStreamOptions {
  connectTimeoutMs?: number;
  inactivityTimeoutMs?: number;
}

export function openEventStream(
  baseUrl: string,
  token: string,
  handlers: { onEvent(e: SseEvent): void; onClose(err: Error | null): void; onActivity?(): void },
  fetchImpl: typeof fetch = fetch,
  options: EventStreamOptions = {},
): () => void {
  const ac = new AbortController();
  let deadlineError: Error | null = null;
  let deadline = setTimeout(() => {
    deadlineError = new Error("SSE connect deadline");
    ac.abort();
  }, options.connectTimeoutMs ?? 10_000);
  const alive = () => {
    clearTimeout(deadline);
    handlers.onActivity?.();
    deadline = setTimeout(() => {
      deadlineError = new Error("SSE inactivity deadline");
      ac.abort();
    }, options.inactivityTimeoutMs ?? 45_000);
  };
  void (async () => {
    try {
      const res = await fetchImpl(
        `${baseUrl}/events/stream?deviceToken=${encodeURIComponent(token)}`,
        {
          headers: { accept: "text/event-stream" },
          signal: ac.signal,
        },
      );
      if (!res.ok || !res.body) {
        handlers.onClose(new Error(`SSE HTTP ${res.status}`));
        return;
      }
      alive();
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = "";
      let event = "message";
      let data: string[] = [];
      let dataLength = 0;
      try {
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          alive();
          buf += decoder.decode(value, { stream: true });
          if (buf.length > 65_536) throw new Error("SSE frame too large");
          let idx: number;
          while ((idx = buf.indexOf("\n")) >= 0) {
            const line = buf.slice(0, idx).replace(/\r$/, "");
            buf = buf.slice(idx + 1);
            if (line === "") {
              if (data.length) handlers.onEvent({ event, data: data.join("\n") });
              event = "message";
              data = [];
              dataLength = 0;
            } else if (line.startsWith("event:")) event = line.slice(6).trim();
            else if (line.startsWith("data:")) {
              const value = line.slice(5).trim();
              dataLength += value.length + 1;
              if (dataLength > 65_536) throw new Error("SSE frame too large");
              data.push(value);
            }
          }
        }
      } finally {
        await reader.cancel().catch(() => {});
        reader.releaseLock();
      }
      handlers.onClose(null);
    } catch (e) {
      if (ac.signal.aborted) handlers.onClose(deadlineError);
      else handlers.onClose(e instanceof Error ? e : new Error(String(e)));
    } finally {
      clearTimeout(deadline);
    }
  })();
  return () => {
    clearTimeout(deadline);
    ac.abort();
  };
}
