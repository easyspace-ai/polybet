import {
  getMonitorPositions,
  postMonitorPositionsSync,
  type RiskPositionRow,
} from "@/lib/api";

const LOG_PREFIX = "[Monitor] avg-entry";

/** How often the fallback loop scans open positions. */
export const AVG_ENTRY_FALLBACK_TICK_MS = 3_000;
/** Per-position minimum interval between sync attempts. */
export const AVG_ENTRY_MIN_INTERVAL_MS = 3_000;
export const AVG_ENTRY_MAX_BACKOFF_MS = 30_000;
/** Brief pause after sync so server risk cache can rebuild. */
const POST_SYNC_SETTLE_MS = 450;

type PositionBackoff = {
  lastAttemptMs: number;
  backoffMs: number;
};

const activeIds = new Set<string>();
const backoffById = new Map<string, PositionBackoff>();
let fallbackTimer: ReturnType<typeof setInterval> | null = null;
let fallbackRefCount = 0;
let fallbackHidden = false;
let fallbackDeps: AvgEntryFallbackDeps | null = null;
let tickInflight = false;
let syncInflight: Promise<void> | null = null;

export function positionNeedsAvgEntryBackfill(pos: RiskPositionRow): boolean {
  if (pos.status !== "open") return false;
  if (pos.sizeShares <= 0) return false;
  return pos.avgEntryCents <= 0 || pos.costUsd <= 0;
}

function positionHasValidAvgEntry(pos: RiskPositionRow): boolean {
  return pos.avgEntryCents > 0;
}

export type AvgEntryFallbackDeps = {
  getOpenPositions: () => RiskPositionRow[];
  onPositionUpdated: (row: RiskPositionRow) => void;
};

function clearPositionState(positionId: string) {
  activeIds.delete(positionId);
  backoffById.delete(positionId);
}

function syncPositionsOnce(): Promise<void> {
  if (syncInflight) return syncInflight;
  syncInflight = postMonitorPositionsSync()
    .then(() => new Promise<void>((resolve) => setTimeout(resolve, POST_SYNC_SETTLE_MS)))
    .catch((err) => {
      const msg = err instanceof Error ? err.message : String(err);
      console.warn(`${LOG_PREFIX} positions sync failed: ${msg}`);
    })
    .finally(() => {
      syncInflight = null;
    });
  return syncInflight;
}

async function runFallbackTick(): Promise<void> {
  if (fallbackHidden || !fallbackDeps || tickInflight) return;

  const open = fallbackDeps.getOpenPositions();
  const need = open.filter(positionNeedsAvgEntryBackfill);
  for (const id of activeIds) {
    if (!need.some((p) => p.id === id)) clearPositionState(id);
  }
  if (need.length === 0) return;

  const now = Date.now();
  const due = need.filter((pos) => {
    const meta = backoffById.get(pos.id);
    if (!meta) return true;
    const gap = Math.max(AVG_ENTRY_MIN_INTERVAL_MS, meta.backoffMs);
    return now - meta.lastAttemptMs >= gap;
  });
  if (due.length === 0) return;

  tickInflight = true;
  try {
    await syncPositionsOnce();
    const resp = await getMonitorPositions();
    const rows = resp.positions ?? [];
    const byId = new Map(rows.map((row) => [row.id, row]));

    for (const pos of due) {
      const meta = backoffById.get(pos.id) ?? {
        lastAttemptMs: 0,
        backoffMs: AVG_ENTRY_MIN_INTERVAL_MS,
      };
      meta.lastAttemptMs = now;

      const fresh = byId.get(pos.id);
      if (!fresh || fresh.status !== "open") {
        clearPositionState(pos.id);
        continue;
      }
      if (positionHasValidAvgEntry(fresh)) {
        fallbackDeps.onPositionUpdated(fresh);
        console.info(
          `${LOG_PREFIX} backfilled #${fresh.positionSeq ?? fresh.id.slice(0, 8)} avg=${fresh.avgEntryCents.toFixed(1)}¢ cost=$${fresh.costUsd.toFixed(2)}`,
        );
        clearPositionState(pos.id);
        continue;
      }

      meta.backoffMs = Math.min(meta.backoffMs * 2, AVG_ENTRY_MAX_BACKOFF_MS);
      backoffById.set(pos.id, meta);
    }
  } finally {
    tickInflight = false;
  }
}

/** Track open positions missing avg entry / cost and poll Polymarket sync until filled. */
export function reconcileAvgEntryFallback(deps: AvgEntryFallbackDeps, positions: RiskPositionRow[]) {
  fallbackDeps = deps;
  for (const pos of positions) {
    if (positionNeedsAvgEntryBackfill(pos)) {
      activeIds.add(pos.id);
      if (!backoffById.has(pos.id)) {
        backoffById.set(pos.id, {
          lastAttemptMs: 0,
          backoffMs: AVG_ENTRY_MIN_INTERVAL_MS,
        });
      }
    } else {
      clearPositionState(pos.id);
    }
  }
}

export function startAvgEntryFallbackPoller(deps: AvgEntryFallbackDeps): () => void {
  fallbackDeps = deps;
  fallbackRefCount += 1;
  if (fallbackTimer) {
    return () => stopAvgEntryFallbackPoller();
  }
  fallbackTimer = setInterval(() => {
    void runFallbackTick();
  }, AVG_ENTRY_FALLBACK_TICK_MS);
  void runFallbackTick();
  return () => stopAvgEntryFallbackPoller();
}

function stopAvgEntryFallbackPoller() {
  fallbackRefCount = Math.max(0, fallbackRefCount - 1);
  if (fallbackRefCount > 0 || !fallbackTimer) return;
  clearInterval(fallbackTimer);
  fallbackTimer = null;
}

export function setAvgEntryFallbackHidden(hidden: boolean) {
  fallbackHidden = hidden;
  if (!hidden) void runFallbackTick();
}
