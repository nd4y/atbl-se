import type { DeviceInfo, DeviceState, Valve } from "./studio-types.ts";
export type { DeviceInfo, DeviceState, Valve } from "./studio-types.ts";

export type DeviceCommand =
  | { kind: "speed"; value: number }
  | { kind: "motorStop" }
  | { kind: "valve"; valve: Valve; open: boolean }
  | { kind: "script"; index: number }
  /** Запуск загруженного sync-скрипта с позиции (мс). */
  | {
      kind: "syncStart";
      timeMs: number;
      expectedToken?: string;
      durationMs?: number;
    }
  | { kind: "syncStop" }
  | { kind: "syncOffset"; offsetMs: number };

export interface SyncUploadResult {
  state: DeviceState;
  token: string;
  rate?: RateHeaders;
}

export interface RateHeaders {
  limit?: number;
  remaining?: number;
  reset?: number;
}

export interface DeviceResult {
  state: DeviceState;
  rate?: RateHeaders;
  /** Время от отправки команды до ответа, мс (проставляет шина). */
  rttMs?: number;
}

export type DeviceErrorKind = "offline" | "rateLimited" | "network" | "http" | "noToken";

export class DeviceError extends Error {
  readonly kind: DeviceErrorKind;
  readonly status: number | undefined;
  readonly rate: RateHeaders | undefined;

  constructor(kind: DeviceErrorKind, message: string, status?: number, rate?: RateHeaders) {
    super(message);
    this.name = "DeviceError";
    this.kind = kind;
    this.status = status;
    this.rate = rate;
  }
}

/** Единственный интерфейс к устройству. Реализуется HTTP-клиентом; в тестах подменяется моком. */
export interface DeviceClient {
  connected(): Promise<{ connected: boolean; cluster?: string }>;
  info(): Promise<DeviceInfo>;
  state(): Promise<DeviceResult>;
  send(cmd: DeviceCommand): Promise<DeviceResult>;
  /** Загрузка funscript (текст JSON) на устройство; ответ содержит токен скрипта. Не через шину: редкая и долгая операция. */
  syncUpload(funscriptJson: string, name: string): Promise<SyncUploadResult>;
}

export function commandChannel(cmd: DeviceCommand): string {
  switch (cmd.kind) {
    case "speed":
      return "speed";
    case "motorStop":
      return "stop";
    case "valve":
      return `valve:${cmd.valve}`;
    case "script":
      return "script";
    case "syncStart":
    case "syncStop":
      // Старт и стоп синхронизации схлопываются друг с другом: важно только последнее.
      return "sync";
    case "syncOffset":
      return "syncOffset";
  }
}

/** Порядок отправки: сначала остановка и закрытие клапанов. */
export function commandPriority(cmd: DeviceCommand): number {
  switch (cmd.kind) {
    case "motorStop":
      return 0;
    case "syncStop":
      return 1;
    case "valve":
      return cmd.open ? 2 : 1;
    case "script":
    case "syncStart":
      return 3;
    case "speed":
    case "syncOffset":
      return 4;
  }
}
