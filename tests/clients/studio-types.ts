// Общие типы приложения: устройство, намерения, профиль, безопасность, снимки состояния.

export type OperationalMode =
  | "ONLINE_CONNECTED"
  | "LOCAL_SCRIPT_PLAYING"
  | "LOCAL_SCRIPT_PAUSED"
  | "TARGET_SPEED_PLAYING"
  | "TARGET_SPEED_PAUSED"
  | "SYNC_SCRIPT_PLAYING"
  | "SYNC_SCRIPT_PAUSED"
  | "LOADING_INTERACTIVE"
  | "SETUP"
  | "LOADING_SETUP"
  | "FIRMWARE_UPDATING"
  | "ERROR"
  | "ERROR_MOTOR_STUCK"
  | "ERROR_MOTOR_OVERRUN";

/** Ответ /vacuglide/state и большинства команд. */
export interface DeviceState {
  operationalMode: OperationalMode;
  localScript: number;
  targetSpeed: number;
  strokePlusValve: boolean;
  strokeMinusValve: boolean;
  syncScriptCurrentTime: number;
  syncScriptOffsetTime: number;
  syncScriptToken: string;
  syncScriptLoop: boolean;
}

export interface DeviceInfo {
  firmwareStatus: "UP_TO_DATE" | "UPDATE_AVAILABLE" | "UPDATE_REQUIRED";
  firmwareVersion: number | string;
  firmwareBranch: string;
  hardwareVersion: string;
  mac: string;
  deviceType: string;
}

/** Stroke+ (plus) впускает воздух и ослабляет хватку, Stroke- (minus) откачивает и присасывает сильнее. */
export type Valve = "plus" | "minus";

export type IntentSource =
  "manual" | "game" | "llm" | "partner" | "character" | "autopilot" | "safety" | "system";

export type MotionIntent =
  | { kind: "speed"; value: number; abrupt?: boolean }
  | { kind: "ramp"; to: number; seconds: number }
  | { kind: "waves"; min: number; max: number; periodSec: number }
  | { kind: "edgeHold" }
  | { kind: "edgeReset" }
  | { kind: "valveLoop"; base: number; minusMs: number; plusMs: number; periodSec: number }
  | { kind: "pause"; seconds?: number }
  | { kind: "script"; slot: number }
  /** Синхронизация с видео: устройство само играет загруженный funscript с позиции timeMs. */
  | { kind: "sync"; timeMs: number }
  /** Пауза видео: скрипт на устройстве останавливается без импульса клапана. */
  | { kind: "syncPause" }
  | { kind: "softStop"; seconds: number }
  | { kind: "stop" };

export type MotionMode = "idle" | "trajectory" | "edgeHold" | "script" | "sync" | "stopped";

/** Загруженный на устройство funscript и состояние синхронизации с видео. */
export interface SyncSnapshot {
  /** Pluggable processing and server-side valve overlay. */
  processing?: import("./sync/processing.js").SyncProcessingSnapshot;
  loaded: {
    name: string;
    hash: string;
    durationMs: number;
    actions: number;
    uploadedAt: number;
    /** Ceiling of the actual uploaded copy. Older servers have no certificate. */ speedLimit?: number;
  } | null;
  playing: boolean;
  /** Позиция скрипта на момент снимка, мс. */
  positionMs: number;
  /** Ручная поправка сверх автоматической, мс. */
  offsetMs: number;
  /** Оценка задержки облака в одну сторону, мс; null, пока не замерена. */
  latencyMs: number | null;
  /** Сколько замеров легло в оценку. */
  latencySamples: number;
  /** Замер идёт. */
  measuring: boolean;
}

export interface Profile {
  comfortMin: number;
  comfortMax: number;
  finishMin: number;
  finishMax: number;
  /** Потолок скорости для любых источников. */
  maxSpeed: number;
  /** Сглаживание: не больше stepPct за stepIntervalSec. */
  stepPct: number;
  stepIntervalSec: number;
  /** Скорость после сброса на краю и сколько её держать до нового намерения. */
  resetSpeed: number;
  resetHoldSec: number;
  strokeMinusMs: number;
  /** Комфортный интервал Stroke- из профиля (30–45 с); жёсткий минимум задаёт SafetyConfig. */
  strokeMinusIntervalSec: number;
  strokePlusMs: number;
  receiver: "S" | "M" | "L";
  /** Как плотно ресивер прилегает: свободно, плотно, очень плотно. */
  receiverFit: "loose" | "snug" | "tight";
  /** Аксессуары из каталога (id). */
  accessories: string[];
  /** Свои аксессуары и особенности, свободный текст. */
  customAccessories: string;
  /** Как он обычно пользуется устройством: поза, что любит, что нет. */
  usageNotes: string;
  allowRuin: boolean;
  allowDenial: boolean;
  /** Движок сам делает сброс при 9 всегда и при 8 вне edgeHold. */
  autoResetAt9: boolean;
  autoResetAt8: boolean;
  /** Персонаж может подкручивать кубик, монетку и колоду в играх. */
  allowCheating: boolean;
  /** Показывать пользователю пометку о подкрутке. */
  cheatVisible: boolean;
}

export interface SafetyConfig {
  /** Клапан не открыт дольше этого, мс. Жёсткий потолок. */
  valveMaxOpenMs: number;
  /** Минимальный интервал между открытиями Stroke-, с. */
  strokeMinusMinIntervalSec: number;
  watchdogTimeoutSec: number;
  watchdogDescentSec: number;
  /** Legacy storage compatibility only; not an absolute session limit. */
  sessionMaxMin: number;
  /** Legacy storage compatibility only; no elapsed-time warning. */
  sessionWarnMin: number;
  /** Minutes since the last real browser interaction; legacy duration fields are ignored. */
  idleTimeoutMin: number;
  idleMin: number;
  idleSpeed: number;
  stopWords: string[];
  rateBudgetPerMin: number;
  rateReserve: number;
}

export type SessionPhase = "idle" | "active" | "finishing" | "finished";

export interface SessionOptions {
  maxSpeed: number;
  /** Legacy API/recipe compatibility only; does not limit the session. */
  durationMin: number;
  background: boolean;
  private: boolean;
}
export type ControlOwner = "manual" | "autopilot" | "game" | "character" | "video" | "llm" | null;

export interface SessionInfo {
  phase: SessionPhase;
  startedAt: number | null;
  endedAt: number | null;
  elapsedSec: number;
  /** Server timestamps for diagnostics, not a timer on the main screen. */
  lastUserActivityAt?: number | null;
  idleDeadlineAt?: number | null;
  edges: number;
  warned: boolean;
  finishStyle: "normal" | "ruin" | "denial" | null;
}

export type ArousalSource = "manual" | "partner" | "eom" | "biofeedback";

export interface Arousal {
  value: number | null;
  updatedAt: number | null;
  source: ArousalSource | null;
}

export interface TrajectoryPoint {
  /** Абсолютное время, мс. */
  t: number;
  speed: number;
}

export interface ValveStatus {
  open: boolean;
  openedAt: number | null;
  closeAt: number | null;
  lastOpenedAt: number | null;
}

export interface PlannedValve {
  t: number;
  valve: Valve;
  ms: number;
}

export interface MotionSnapshot {
  mode: MotionMode;
  intent: MotionIntent | null;
  source: IntentSource | null;
  intentAt: number | null;
  /** Последняя скорость, которую движок отправил (принял SafetyGate). */
  currentSpeed: number;
  /** Точка траектории на текущий момент. */
  desiredSpeed: number;
  /** Будущее для таймлайна, прореженное. */
  plan: TrajectoryPoint[];
  plannedValves: PlannedValve[];
  valves: { plus: ValveStatus; minus: ValveStatus };
  lastResetAt: number | null;
  edgeHoldTarget: number | null;
}

export interface RateInfo {
  budgetPerMin: number;
  tokens: number;
  reserve: number;
  serverLimit: number | null;
  serverRemaining: number | null;
  backoffUntil: number | null;
  sentLastMin: number;
  pending: number;
}

export interface ConnectionInfo {
  events?: {
    status: "connecting" | "live" | "stale" | "closed";
    lastEventAt: number | null;
    reconnects: number;
  };
  mode: "sim" | "device";
  hasToken: boolean;
  cluster: string | null;
  online: boolean;
  lastError: string | null;
  lastOkAt: number | null;
  info: DeviceInfo | null;
}

export interface HistorySample {
  t: number;
  speed: number;
  arousal: number | null;
  plus: boolean;
  minus: boolean;
}

export interface LogEntry {
  t: number;
  level: "info" | "warn" | "error";
  msg: string;
  source?: string;
}

export interface SlotConfig {
  /** 0–29, как в API. В UI показывается index+1. */
  index: number;
  name: string;
}

export interface SimConfig {
  online: boolean;
  latencyMs: number;
  jitterMs: number;
  budgetPerMin: number;
}

export interface SimSnapshot {
  online: boolean;
  mode: OperationalMode;
  speed: number;
  plus: boolean;
  minus: boolean;
  /** Условный уровень вакуума 0–100 для визуализации. */
  vacuum: number;
  requestsLastMin: number;
  script: number | null;
}

export interface Settings {
  background: { enabled: boolean };
  autopilot: import("./autopilot.js").AutopilotSettings;
  profile: Profile;
  safety: SafetyConfig;
  connection: { mode: "sim" | "device" };
  slots: SlotConfig[];
  sim: SimConfig;
  ageConfirmed: boolean;
  llm: import("./llm/types.js").LlmConfig;
  chat: { personaId: string };
  character: { activeId: string | null; applyReactions: boolean };
  tts: import("./history.js").TtsConfig;
  /** Рекорды игр: id игры → лучший результат (раунды выносливости). */
  games: { records: Record<string, number> };
}

export interface SafetyFlags {
  /** Confirmation from the command API, never physical motor telemetry. */
  stopConfirmation: "pending" | "confirmed" | "failed";
  stopConfirmedAt: number | null;
  unsafe: boolean;
  watchdogTripped: boolean;
  idlePrompted: boolean;
  lastStopReason: string | null;
  lastStopAt: number | null;
}

export interface AppSnapshot {
  biofeedback?: import("./biofeedback.js").BioSnapshot;
  sessionOptions: SessionOptions;
  controlOwner: ControlOwner;
  backgroundActive: boolean;
  autopilot: import("./autopilot.js").AutopilotSnapshot;
  t: number;
  game: import("./games/types.js").GameSnapshot | null;
  character: import("./characters/types.js").CharacterSnapshot;
  connection: ConnectionInfo;
  device: DeviceState | null;
  motion: MotionSnapshot;
  arousal: Arousal;
  session: SessionInfo;
  rate: RateInfo;
  sim: SimSnapshot | null;
  safety: SafetyFlags;
  settings: Settings;
  sync: SyncSnapshot;
  clients: number;
}
