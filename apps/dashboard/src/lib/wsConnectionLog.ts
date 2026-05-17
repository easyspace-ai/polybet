export type ChannelId = "relay" | "ob" | "user";
export type ChannelDisplay = "connected" | "disconnected" | "reconnecting" | "standby" | "unconfigured";

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

export function setOBRequired(required: boolean, connected: boolean) {
  const ob = snapshots.ob;
  const before = captureFingerprints();
  ob.required = required;
  if (!required) {
    ob.display = "standby";
    ob.nextRetryAt = undefined;
  } else if (connected) {
    ob.display = "connected";
    ob.nextRetryAt = undefined;
  } else if (ob.nextRetryAt && ob.nextRetryAt > Date.now()) {
    ob.display = "reconnecting";
  } else {
    ob.display = "disconnected";
  }
  notifyIfChanged(before);
}

export function setUSERRequired(required: boolean, connected: boolean) {
  const user = snapshots.user;
  const before = captureFingerprints();
  user.required = required;
  if (!required) {
    user.display = "unconfigured";
    notifyIfChanged(before);
    return;
  }
  if (connected) {
    user.display = "connected";
    user.nextRetryAt = undefined;
  } else if (user.nextRetryAt && user.nextRetryAt > Date.now()) {
    user.display = "reconnecting";
  } else {
    user.display = "disconnected";
  }
  notifyIfChanged(before);
}
