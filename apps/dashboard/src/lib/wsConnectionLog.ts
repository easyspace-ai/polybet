export type ChannelId = "relay" | "ob" | "user";
export type ChannelDisplay =
  | "connected"
  | "disconnected"
  | "reconnecting"
  | "standby"
  | "unconfigured";

export interface WSEventEntry {
  at: number;
  level: "info" | "warn";
  message: string;
}

export interface ChannelSnapshot {
  id: ChannelId;
  display: ChannelDisplay;
  required: boolean;
  nextRetryAt?: number;
  attempt?: number;
  lastIssue?: string;
  events: WSEventEntry[];
}

const MAX_EVENTS = 20;
const snapshots: Record<ChannelId, ChannelSnapshot> = {
  relay: { id: "relay", display: "disconnected", required: true, events: [] },
  ob: { id: "ob", display: "standby", required: false, events: [] },
  user: { id: "user", display: "disconnected", required: true, events: [] },
};

const listeners = new Set<() => void>();
const CHANNEL_IDS: ChannelId[] = ["relay", "ob", "user"];

/** Last authoritative upstream connected flags (from REST or full poly_status). */
let upstreamObConnected: boolean | null = null;
let upstreamUserConnected: boolean | null = null;

export function getUpstreamConnectedState(id: "ob" | "user"): boolean | null {
  return id === "ob" ? upstreamObConnected : upstreamUserConnected;
}

function cloneChannelSnapshot(id: ChannelId): ChannelSnapshot {
  const ch = snapshots[id];
  return { ...ch, events: [...ch.events] };
}

/** Stable reference for useSyncExternalStore — only replaced when channel state changes. */
let allSnapshotsCache: ChannelSnapshot[] = CHANNEL_IDS.map(cloneChannelSnapshot);

function rebuildAllSnapshotsCache(): void {
  allSnapshotsCache = CHANNEL_IDS.map(cloneChannelSnapshot);
}

function snapshotFingerprint(ch: ChannelSnapshot): string {
  return JSON.stringify({
    display: ch.display,
    required: ch.required,
    nextRetryAt: ch.nextRetryAt,
    attempt: ch.attempt,
    lastIssue: ch.lastIssue,
    eventHead: ch.events[0]?.message ?? "",
  });
}

function notifyIfChanged(before: Record<ChannelId, string>) {
  let changed = false;
  for (const id of ["relay", "ob", "user"] as ChannelId[]) {
    if (before[id] !== snapshotFingerprint(snapshots[id])) {
      changed = true;
      break;
    }
  }
  if (changed) {
    rebuildAllSnapshotsCache();
    listeners.forEach((fn) => fn());
  }
}

function captureFingerprints(): Record<ChannelId, string> {
  return {
    relay: snapshotFingerprint(snapshots.relay),
    ob: snapshotFingerprint(snapshots.ob),
    user: snapshotFingerprint(snapshots.user),
  };
}

export function subscribeWSConnectionLog(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function getChannelSnapshot(id: ChannelId): ChannelSnapshot {
  return cloneChannelSnapshot(id);
}

/** Must return a cached reference between store updates (React useSyncExternalStore). */
export function getAllChannelSnapshots(): ChannelSnapshot[] {
  return allSnapshotsCache;
}

export function appendWSLog(id: ChannelId, level: "info" | "warn", message: string) {
  const ch = snapshots[id];
  const before = captureFingerprints();
  ch.events = [{ at: Date.now(), level, message }, ...ch.events].slice(0, MAX_EVENTS);
  notifyIfChanged(before);
}

export function setRelayState(
  display: ChannelDisplay,
  opts?: { nextRetryAt?: number; attempt?: number },
) {
  const ch = snapshots.relay;
  const before = captureFingerprints();
  ch.display = display;
  ch.nextRetryAt = opts?.nextRetryAt;
  ch.attempt = opts?.attempt;
  notifyIfChanged(before);
}

export function setUpstreamFromPolyStatus(msg: {
  polyOrderbookConnected?: boolean;
  polyUserConnected?: boolean;
  orderbookNextRetryAt?: number;
  orderbookReconnectAttempt?: number;
  userNextRetryAt?: number;
  userReconnectAttempt?: number;
  userWsLastIssue?: string;
  wsEvents?: { channel?: string; at?: string; level?: string; message?: string }[];
}) {
  const before = captureFingerprints();
  const ob = snapshots.ob;
  const user = snapshots.user;

  if (msg.orderbookReconnectAttempt != null) ob.attempt = msg.orderbookReconnectAttempt;
  if (msg.orderbookNextRetryAt != null) ob.nextRetryAt = msg.orderbookNextRetryAt;
  if (msg.userReconnectAttempt != null) user.attempt = msg.userReconnectAttempt;
  if (msg.userNextRetryAt != null) user.nextRetryAt = msg.userNextRetryAt;
  if (msg.userWsLastIssue) user.lastIssue = msg.userWsLastIssue;

  if (Array.isArray(msg.wsEvents) && msg.wsEvents.length > 0) {
    for (const ev of msg.wsEvents) {
      const ch = ev.channel === "orderbook" ? ob : ev.channel === "user" ? user : null;
      if (!ch) continue;
      const at = ev.at ? Date.parse(ev.at) : Date.now();
      ch.events = [
        {
          at: Number.isFinite(at) ? at : Date.now(),
          level: ev.level === "warn" ? "warn" : "info",
          message: ev.message ?? "",
        },
        ...ch.events,
      ].slice(0, MAX_EVENTS);
    }
  }

  notifyIfChanged(before);
}

function upstreamDisplay(
  connected: boolean,
  connecting: boolean | undefined,
  nextRetryAt: number | undefined,
): ChannelDisplay {
  if (connected) return "connected";
  if (connecting || (nextRetryAt != null && nextRetryAt > Date.now())) return "reconnecting";
  return "disconnected";
}

function forceUpstreamConnected(id: "ob" | "user") {
  const ch = snapshots[id];
  if (id === "ob" && !ch.required) return;
  const before = captureFingerprints();
  ch.display = "connected";
  ch.nextRetryAt = undefined;
  notifyIfChanged(before);
}

export function markUpstreamReconnecting(id: "ob" | "user") {
  const ch = snapshots[id];
  if (id === "ob" && !ch.required) return;
  // REST/full status already confirmed upstream — never downgrade to reconnecting.
  if (id === "ob" && upstreamObConnected === true) {
    forceUpstreamConnected("ob");
    return;
  }
  if (id === "user" && upstreamUserConnected === true) {
    forceUpstreamConnected("user");
    return;
  }
  const before = captureFingerprints();
  ch.display = "reconnecting";
  notifyIfChanged(before);
}

/** Clear authoritative upstream flags so stale REST data is not trusted after
 *  the dashboard WebSocket (our realtime source) disconnects. */
export function clearUpstreamConnectedState() {
  const before = captureFingerprints();
  upstreamObConnected = null;
  upstreamUserConnected = null;
  notifyIfChanged(before);
}

export function setOBRequired(required: boolean, connected: boolean, connecting?: boolean) {
  const ob = snapshots.ob;
  const before = captureFingerprints();
  ob.required = required;
  upstreamObConnected = connected;
  if (!required) {
    ob.display = "standby";
    ob.nextRetryAt = undefined;
  } else if (connected) {
    ob.display = "connected";
    ob.nextRetryAt = undefined;
  } else {
    ob.display = upstreamDisplay(connected, connecting, ob.nextRetryAt);
  }
  notifyIfChanged(before);
}

export function setUSERRequired(required: boolean, connected: boolean, connecting?: boolean) {
  const user = snapshots.user;
  const before = captureFingerprints();
  user.required = required;
  upstreamUserConnected = connected;
  if (!required) {
    user.display = "unconfigured";
    notifyIfChanged(before);
    return;
  }
  if (connected) {
    user.display = "connected";
    user.nextRetryAt = undefined;
  } else {
    user.display = upstreamDisplay(connected, connecting, user.nextRetryAt);
  }
  notifyIfChanged(before);
}

export type UpstreamPolyStatusInput = {
  polyOrderbookConnected?: boolean;
  polyUserConnected?: boolean;
  polyOrderbookConnecting?: boolean;
  polyUserConnecting?: boolean;
  orderbookNextRetryAt?: number;
  orderbookReconnectAttempt?: number;
  userNextRetryAt?: number;
  userReconnectAttempt?: number;
  userWsLastIssue?: string;
  wsEvents?: { channel?: string; at?: string; level?: string; message?: string }[];
};

/**
 * Apply upstream OB/USER display from poly_status (WS or REST).
 * Must run at wsBus dispatch time — not only inside React hooks — so the
 * initial risk WS poly_status is not dropped before useGlobalWSStatus mounts.
 */
export function applyUpstreamPolyStatus(
  msg: UpstreamPolyStatusInput,
  opts?: { fromRest?: boolean; openPositionsCount?: number },
) {
  setUpstreamFromPolyStatus(msg);
  const fromRest = opts?.fromRest === true;
  const hasObState =
    fromRest ||
    msg.polyOrderbookConnected !== undefined ||
    msg.polyOrderbookConnecting !== undefined;
  const hasUserState =
    fromRest || msg.polyUserConnected !== undefined || msg.polyUserConnecting !== undefined;
  if (!hasObState && !hasUserState) return;

  let obRequired =
    opts?.openPositionsCount != null
      ? opts.openPositionsCount > 0
      : snapshots.ob.required;
  if (msg.polyOrderbookConnected === true) obRequired = true;

  if (hasObState) {
    const obConnected =
      fromRest || msg.polyOrderbookConnected !== undefined
        ? msg.polyOrderbookConnected === true
        : upstreamObConnected === true;
    const obConnecting = msg.polyOrderbookConnecting === true && !obConnected;
    setOBRequired(obRequired, obConnected, obConnecting);
  }

  if (hasUserState) {
    const userConnected =
      fromRest || msg.polyUserConnected !== undefined
        ? msg.polyUserConnected === true
        : upstreamUserConnected === true;
    const userConnecting = msg.polyUserConnecting === true && !userConnected;
    setUSERRequired(true, userConnected, userConnecting);
  }
}
