"use strict";
const $ = (id) => document.getElementById(id);
let token = sessionStorage.getItem("localToken") || "";
let epoch = "",
  status = null,
  renewing = "",
  refreshing = false,
  hadSession = false;
let actionSequence = 0,
  pendingAction = false;
const incoming = new URLSearchParams(location.hash.slice(1)).get("token");
if (incoming) {
  token = incoming;
  sessionStorage.setItem("localToken", token);
  history.replaceState(null, "", location.pathname);
}
async function api(path, body) {
  const requestToken = token;
  const requestEpoch = body && body.controlEpoch;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 10000);
  try {
    const r = await fetch("/api/" + path, {
      method: body === undefined ? "GET" : "POST",
      headers: { Authorization: "Bearer " + requestToken, "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: "no-store",
      signal: controller.signal,
    });
    const data = await r.json();
    if (r.status === 401 && requestToken === token && (!requestEpoch || requestEpoch === epoch)) {
      $("login").hidden = false;
      epoch = "";
    }
    if (!r.ok) throw new Error(data.error || "Ошибка запроса");
    return data;
  } finally {
    clearTimeout(timer);
  }
}
function error(e) {
  $("error").textContent = e.message;
}
async function refresh() {
  if (!token || refreshing) return;
  refreshing = true;
  const sequence = actionSequence;
  try {
    const next = await api("status");
    if (sequence !== actionSequence) return;
    const wasExternal = status && status.compatibilityControl;
    status = next;
    $("login").hidden = true;
    $("connection").textContent = status.connected
      ? status.ready
        ? "Устройство подключено"
        : "Подключение устройства..."
      : "Ожидание VacuGlide";
    $("details").textContent = status.connected
      ? "Прошивка " + status.firmware + " • " + status.mac
      : "";
    $("pair").hidden = !status.connected || status.paired;
    $("start").disabled =
      pendingAction || !status.ready || !status.paired || status.active || status.stopping;
    $("speed").max = status.maxSpeed;
    $("speedLabel").textContent = $("speed").value + "%";
    $("state").textContent = status.state
      ? status.state.operationalMode + " · заданная скорость " + status.state.targetSpeed + "%"
      : "";
    if (status.compatibilityControl)
      $("runStatus").textContent =
        "Управляет внешнее приложение. Его сеанс не использует таймер панели.";
    else if (wasExternal)
      $("runStatus").textContent = status.connected
        ? ""
        : "Связь потеряна. Остановка не подтверждена.";
    if (!status.active && hadSession) {
      epoch = "";
      if (status.stopConfirmed) {
        hadSession = false;
        $("runStatus").textContent = "STOP подтверждён устройством";
      } else {
        $("runStatus").textContent = status.connected
          ? "Ожидание подтверждения STOP..."
          : "Связь потеряна. Остановка не подтверждена.";
      }
    }
  } catch (e) {
    error(e);
    $("start").disabled = true;
    if (hadSession) $("runStatus").textContent = "Связь с сервером потеряна. Проверьте устройство.";
  } finally {
    refreshing = false;
  }
}
$("loginButton").onclick = () => {
  token = $("token").value.trim();
  sessionStorage.setItem("localToken", token);
  refresh();
};
$("speed").oninput = () => {
  $("speedLabel").textContent = $("speed").value + "%";
};
$("pair").onclick = async () => {
  try {
    await api("pair", { mac: status.mac });
    await refresh();
  } catch (e) {
    error(e);
  }
};
$("start").onclick = async () => {
  if (pendingAction || !status || !status.ready || !status.paired) return;
  const sequence = ++actionSequence;
  pendingAction = true;
  $("start").disabled = true;
  $("error").textContent = "";
  try {
    const r = await api("start", {
      speed: Number($("speed").value),
      seconds: Number($("seconds").value),
      generation: status.generation,
    });
    if (sequence !== actionSequence) return;
    epoch = r.controlEpoch;
    hadSession = true;
    $("runStatus").textContent = "Сеанс активен. Оставьте эту страницу открытой.";
  } catch (e) {
    if (sequence === actionSequence) error(e);
  } finally {
    if (sequence === actionSequence) pendingAction = false;
  }
  if (sequence !== actionSequence) return;
  await refresh();
};
$("stop").onclick = async () => {
  const sequence = ++actionSequence;
  pendingAction = true;
  $("start").disabled = true;
  epoch = "";
  try {
    await api("stop", {});
    if (sequence !== actionSequence) return;
    hadSession = false;
    $("runStatus").textContent = "STOP подтверждён устройством";
  } catch (e) {
    if (sequence === actionSequence) {
      error(e);
      $("runStatus").textContent = "STOP не подтверждён. Проверьте устройство.";
    }
  } finally {
    if (sequence === actionSequence) pendingAction = false;
  }
  if (sequence === actionSequence) await refresh();
};
setInterval(async () => {
  if (!epoch || renewing === epoch) return;
  const sentEpoch = epoch;
  renewing = sentEpoch;
  try {
    await api("keepalive", { controlEpoch: sentEpoch });
  } catch (e) {
    if (epoch === sentEpoch) {
      epoch = "";
      error(e);
    }
  } finally {
    if (renewing === sentEpoch) renewing = "";
  }
}, 400);
setInterval(refresh, 1000);
refresh();
