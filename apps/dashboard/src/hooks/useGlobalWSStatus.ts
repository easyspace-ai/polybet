import { useSyncExternalStore } from "react";
import { riskWsBus, wsBus, type PolyStatusMessage } from "@/lib/wsBus";
import {
  type ChannelId,
  type ChannelSnapshot,
  applyUpstreamPolyStatus,
  clearUpstreamConnectedState,
  getAllChannelSnapshots,
  getChannelSnapshot,
  getUpstreamConnectedState,
  markUpstreamReconnecting,
  subscribeWSConnectionLog,
} from "@/lib/wsConnectionLog";
import { getOpenRiskPositionCount } from "@/hooks/useRiskControlCache";
import { getWSConfig } from "@/hooks/useWSConfig";
import { getWSStatus, postWSReconnect } from "@/lib/api";

const DISCOVERY_BASE_MS = 1000;
const DISCOVERY_MAX_MS = 10_000;
const RECONNECT_MIN_INTERVAL_MS = 5_000;
const RECONNECT_BURST_COUNT = 5;
const RECONNECT_BURST_INTERVAL_MS = 750;
const RECONNECT_COOLDOWN_MS = 3_000;
const CHANNEL_IDS: ChannelId[] = ["relay", "ob", "user"];

let wsStatusInflight: Promise<void> | null = null;
let wsStatusAbortController: AbortController | null = null;
let discoveryTimer: ReturnType<typeof setTimeout> | null = null;
let burstTimer: ReturnType<typeof setTimeout> | null = null;
let burstRemaining = 0;
let discoveryAttempt = 0;
let discoveryActive = false;
const reconnectInflight = new Set<"orderbook" | "user">();
const reconnectLastAt: Partial<Record<"orderbook" | "user", number>> = {};
const reconnectCooldownUntil: Partial<Record<"orderbook" | "user", number>> = {};

function channelNeedsDiscovery(ch: ChannelSnapshot): boolean {
  if (ch.id === "relay") {
    return ch.display === "disconnected" || ch.display === "reconnecting";
  }
  if (ch.id === "ob") {
    if (!ch.required) return false;
    return ch.display === "disconnected" || ch.display === "reconnecting";
  }
  if (ch.display === "unconfigured") return false;
  return ch.display === "disconnected" || ch.display === "reconnecting";
}

function anyChannelNeedsDiscovery(): boolean {
  return CHANNEL_IDS.some((id) => channelNeedsDiscovery(getChannelSnapshot(id)));
}

function discoveryBackoffMs(): number {
  const exp = DISCOVERY_BASE_MS * 2 ** Math.max(0, discoveryAttempt - 1);
  return Math.min(exp, DISCOVERY_MAX_MS);
}

function clearDiscoveryTimer() {
  if (discoveryTimer) {
    clearTimeout(discoveryTimer);
    discoveryTimer = null;
  }
}

function resetDiscoveryBackoff() {
  discoveryAttempt = 0;
}

function clearBurstPoll() {
  if (burstTimer) {
    clearTimeout(burstTimer);
    burstTimer = null;
  }
  burstRemaining = 0;
}

function cancelInflightStatusPoll() {
  if (wsStatusAbortController) {
    wsStatusAbortController.abort();
    wsStatusAbortController = null;
  }
  wsStatusInflight = null;
}

function scheduleBurstPollTick() {
  if (burstRemaining <= 0) {
    clearBurstPoll();
    syncDiscoveryLoop();
    return;
  }
  burstRemaining -= 1;
  burstTimer = setTimeout(() => {
    burstTimer = null;
    void pollWSStatusOnce().then(() => {
      if (!anyChannelNeedsDiscovery()) {
        clearBurstPoll();
        syncDiscoveryLoop();
        return;
      }
      scheduleBurstPollTick();
    });
  }, RECONNECT_BURST_INTERVAL_MS);
}

function startReconnectBurstPoll() {
  clearBurstPoll();
  burstRemaining = RECONNECT_BURST_COUNT;
  resetDiscoveryBackoff();
  void pollWSStatusOnce().then(() => {
    if (!anyChannelNeedsDiscovery()) {
      clearBurstPoll();
      syncDiscoveryLoop();
      return;
    }
    scheduleBurstPollTick();
  });
}

function scheduleDiscoveryPoll(immediate = false) {
  if (!discoveryActive) return;
  clearDiscoveryTimer();
  discoveryTimer = setTimeout(
    () => {
      void runDiscoveryPoll();
    },
    immediate ? 0 : discoveryBackoffMs(),
  );
}

async function runDiscoveryPoll() {
  await pollWSStatusOnce();
  if (!discoveryActive) return;
  if (anyChannelNeedsDiscovery()) {
    discoveryAttempt += 1;
    scheduleDiscoveryPoll();
  } else {
    resetDiscoveryBackoff();
  }
}

function syncDiscoveryLoop() {
  if (!discoveryActive) return;
  if (anyChannelNeedsDiscovery()) {
    if (!discoveryTimer) scheduleDiscoveryPoll(discoveryAttempt < 3);
  } else {
    clearDiscoveryTimer();
    resetDiscoveryBackoff();
    clearBurstPoll();
  }
}

function refreshWSStatus(opts?: { resetBackoff?: boolean }) {
  if (opts?.resetBackoff) resetDiscoveryBackoff();
  void pollWSStatusOnce().then(() => syncDiscoveryLoop());
}

async function requestUpstreamReconnect(
  channel: "orderbook" | "user",
  opts?: { nextRetryAt?: number; force?: boolean },
) {
  const now = Date.now();
  if (!opts?.force) {
    if (opts?.nextRetryAt && opts.nextRetryAt > now) return;
    const last = reconnectLastAt[channel] ?? 0;
    if (now - last < RECONNECT_MIN_INTERVAL_MS) return;
    const cooldownEnd = reconnectCooldownUntil[channel] ?? 0;
    if (now < cooldownEnd) return;
  }
  if (reconnectInflight.has(channel)) return;

  reconnectInflight.add(channel);
  reconnectLastAt[channel] = now;
  try {
    cancelInflightStatusPoll();

    const alreadyConnected =
      channel === "orderbook"
        ? getUpstreamConnectedState("ob") === true
        : getUpstreamConnectedState("user") === true;
    if (alreadyConnected) {
      syncDiscoveryLoop();
      return;
    }

    markUpstreamReconnecting(channel === "orderbook" ? "ob" : "user");
    const res = await postWSReconnect(channel);
    if (res.ok || res.accepted) {
      reconnectCooldownUntil[channel] = Date.now() + RECONNECT_COOLDOWN_MS;
      void pollWSStatusOnce().then(() => startReconnectBurstPoll());
    } else {
      refreshWSStatus({ resetBackoff: true });
    }
  } catch {
    refreshWSStatus({ resetBackoff: true });
  } finally {
    reconnectInflight.delete(channel);
  }
}

async function pollWSStatusOnce() {
  if (wsStatusInflight) return wsStatusInflight;
  const cfg = getWSConfig();
  wsStatusAbortController = new AbortController();
  wsStatusInflight = (async () => {
    try {
      const st = await getWSStatus({ signal: wsStatusAbortController!.signal });
      applyUpstreamPolyStatus(
        {
          polyOrderbookConnected: st.polyOrderbookConnected,
          polyUserConnected: st.polyUserConnected,
          polyOrderbookConnecting: st.polyOrderbookConnecting,
          polyUserConnecting: st.polyUserConnecting,
          orderbookNextRetryAt: st.orderbookNextRetryAt,
          orderbookReconnectAttempt: st.orderbookReconnectAttempt,
          userNextRetryAt: st.userNextRetryAt,
          userReconnectAttempt: st.userReconnectAttempt,
          userWsLastIssue: st.userWsLastIssue,
          wsEvents: st.wsEvents,
        },
        {
          fromRest: true,
          openPositionsCount: st.openPositionsCount ?? getOpenRiskPositionCount(),
        },
      );
      if (!cfg.wsAutoRequestUpstreamReconnect) return;
      const openN = st.openPositionsCount ?? getOpenRiskPositionCount();
      const obRequired = openN > 0;
      if (obRequired && st.polyOrderbookConnected === false) {
        void requestUpstreamReconnect("orderbook", { nextRetryAt: st.orderbookNextRetryAt });
      }
      if (st.polyUserConnected === false) {
        void requestUpstreamReconnect("user", { nextRetryAt: st.userNextRetryAt });
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === "AbortError") {
        return;
      }
      // REST poll failed (network down, 500, timeout).  Downgrade upstream
      // state so the UI does not falsely claim upstream is healthy.
      applyUpstreamPolyStatus(
        { polyOrderbookConnected: false, polyUserConnected: false },
        { fromRest: true },
      );
      // Back off instead of resetting — prevents retry storms when the
      // backend is down or overloaded.
      discoveryAttempt += 1;
    } finally {
      syncDiscoveryLoop();
    }
  })().finally(() => {
    wsStatusInflight = null;
    wsStatusAbortController = null;
  });
  return wsStatusInflight;
}

function onPolyStatus(_msg: PolyStatusMessage) {
  syncDiscoveryLoop();
}

function startWSDiscovery() {
  if (discoveryActive) return;
  discoveryActive = true;
  refreshWSStatus({ resetBackoff: true });
}

let bootstrapInstalled = false;
function installWSStatusBootstrap() {
  if (bootstrapInstalled || typeof window === "undefined") return;
  bootstrapInstalled = true;

  riskWsBus.onPolyStatus(onPolyStatus);
  wsBus.onPolyStatus(onPolyStatus);

  const onDashWSStatusChange = (connected: boolean) => {
    if (connected) {
      refreshWSStatus({ resetBackoff: true });
    } else {
      // Dashboard WS is our realtime source — when it drops, upstream state
      // becomes unknown.  Downgrade so the UI does not falsely show "connected".
      clearUpstreamConnectedState();
      syncDiscoveryLoop();
    }
  };
  wsBus.onStatusChange(onDashWSStatusChange);
  riskWsBus.onStatusChange(onDashWSStatusChange);

  subscribeWSConnectionLog(syncDiscoveryLoop);

  let pollHidden = document.visibilityState === "hidden";
  const onVisibility = () => {
    const visible = document.visibilityState === "visible";
    pollHidden = !visible;
    if (visible) refreshWSStatus({ resetBackoff: true });
  };
  const onFocus = () => refreshWSStatus({ resetBackoff: true });
  document.addEventListener("visibilitychange", onVisibility);
  window.addEventListener("focus", onFocus);

  const cfg = getWSConfig();
  const steadyPollMs = cfg.wsRiskPollIntervalSec * 1000;

  // Steady state polling
  setInterval(() => {
    if (pollHidden) return;
    void pollWSStatusOnce();
  }, steadyPollMs);

  // Startup burst: poll more aggressively for the first 90s so we catch
  // sidecar initialization / upstream reconnect quickly without waiting
  // for the discovery backoff to grow.
  const BOOTSTRAP_BURST_MS = 3_000;
  const BOOTSTRAP_BURST_DURATION_MS = 90_000;
  let bootstrapTick = 0;
  const bootstrapBurst = setInterval(() => {
    if (pollHidden) return;
    void pollWSStatusOnce();
    bootstrapTick += BOOTSTRAP_BURST_MS;
    if (bootstrapTick >= BOOTSTRAP_BURST_DURATION_MS) {
      clearInterval(bootstrapBurst);
    }
  }, BOOTSTRAP_BURST_MS);

  startWSDiscovery();
}

installWSStatusBootstrap();

export function useGlobalWSStatus() {
  const snapshots = useSyncExternalStore(
    subscribeWSConnectionLog,
    getAllChannelSnapshots,
    getAllChannelSnapshots,
  );

  return {
    channels: snapshots,
    reconnectRelay: () => riskWsBus.reconnect(true),
    reconnectUpstream: (channel: "orderbook" | "user") =>
      requestUpstreamReconnect(channel, { force: true }),
  };
}
