import { useEffect, useRef, useCallback } from 'react';
import { wsBus, type PositionUpdateMessage } from '@/lib/wsBus';
import type { RiskPositionRow } from '@/lib/api';
import { useSoundSettings } from './useSoundSettings';

interface DesktopAPI {
  playSound?: (soundName: string) => Promise<{ ok: true } | { ok: false; error: string }>;
}

function isDesktopApp(): boolean {
  const api = (window as unknown as { desktopAPI?: DesktopAPI }).desktopAPI;
  return typeof window !== 'undefined' && typeof api?.playSound === 'function';
}

function getDesktopAPI(): DesktopAPI | undefined {
  return (window as unknown as { desktopAPI?: DesktopAPI }).desktopAPI;
}

function comparePositions(
  oldPositions: RiskPositionRow[],
  newPositions: RiskPositionRow[]
): { opened: string[]; closed: string[]; stoppedOut: string[] } {
  const oldIds = new Set(oldPositions.map(p => p.id));
  const newIds = new Set(newPositions.map(p => p.id));
  const oldMap = new Map(oldPositions.map(p => [p.id, p]));

  const opened: string[] = [];
  const closed: string[] = [];
  const stoppedOut: string[] = [];

  for (const id of newIds) {
    if (!oldIds.has(id)) {
      opened.push(id);
    }
  }

  for (const id of oldIds) {
    if (!newIds.has(id)) {
      const oldPos = oldMap.get(id);
      if (oldPos && oldPos.pnlUsd !== null && oldPos.pnlUsd < 0) {
        stoppedOut.push(id);
      } else {
        closed.push(id);
      }
    }
  }

  return { opened, closed, stoppedOut };
}

export function useSoundNotifications() {
  const previousPositionsRef = useRef<RiskPositionRow[]>([]);
  const hasEverConnectedRef = useRef(false);
  const wasConnectedRef = useRef(false);
  const { settings } = useSoundSettings();

  const playSound = useCallback(async (soundName: 'buy' | 'sell' | 'alert') => {
    if (!isDesktopApp()) return;
    if (!settings.enabled) return;
    if (soundName === 'buy' && !settings.buyEnabled) return;
    if (soundName === 'sell' && !settings.sellEnabled) return;
    if (soundName === 'alert' && !settings.alertEnabled) return;

    try {
      const api = getDesktopAPI();
      await api?.playSound!(soundName);
    } catch (e) {
      console.error('[sound] Failed to play:', soundName, e);
    }
  }, [settings]);

  useEffect(() => {
    if (!isDesktopApp()) return;

    const unsubStatus = wsBus.onStatus((connected) => {
      if (hasEverConnectedRef.current && !connected) {
        console.warn('[sound] WebSocket disconnected, playing alert sound');
        playSound('alert');
      }
      if (connected) {
        hasEverConnectedRef.current = true;
      }
      wasConnectedRef.current = connected;
    });

    const unsubPosition = wsBus.onPositionUpdate((msg: PositionUpdateMessage) => {
      const newPositions = msg.data as RiskPositionRow[];
      const oldPositions = previousPositionsRef.current;

      if (oldPositions.length > 0) {
        const { opened, closed, stoppedOut } = comparePositions(oldPositions, newPositions);

        if (opened.length > 0) {
          playSound('buy');
        }
        if (closed.length > 0 || stoppedOut.length > 0) {
          playSound('sell');
        }
      }

      previousPositionsRef.current = newPositions;
    });

    return () => {
      unsubStatus();
      unsubPosition();
    };
  }, [playSound]);
}
