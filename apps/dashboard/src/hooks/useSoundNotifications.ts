import { useEffect, useRef, useCallback, useState } from 'react';
import { wsBus, type PositionUpdateMessage } from '@/lib/wsBus';
import type { RiskPositionRow } from '@/lib/api';
import { useSoundSettings } from './useSoundSettings';
import { toast } from 'sonner';

interface DesktopAPI {
  playSound?: (soundName: string) => Promise<{ ok: true } | { ok: false; error: string }>;
}

const SOUND_ASSETS: Record<string, string> = {
  buy: '/sounds/buy.mp3',
  sell: '/sounds/sell.mp3',
  alert: '/sounds/alert.mp3',
};

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
    if (!oldIds.has(id)) opened.push(id);
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

async function playSoundViaWebAudio(soundName: keyof typeof SOUND_ASSETS): Promise<void> {
  const url = SOUND_ASSETS[soundName];
  if (!url) throw new Error(`Unknown sound: ${soundName}`);

  const ctx = new (window.AudioContext || (window as any).webkitAudioContext)();
  try {
    const res = await fetch(url);
    const arrayBuf = await res.arrayBuffer();
    const audioBuf = await ctx.decodeAudioData(arrayBuf);
    const src = ctx.createBufferSource();
    src.buffer = audioBuf;
    src.connect(ctx.destination);
    src.start(0);
  } finally {
    // Close context after playback to avoid resource leaks
    setTimeout(() => ctx.close(), 2000);
  }
}

export function useSoundNotifications() {
  const previousPositionsRef = useRef<RiskPositionRow[]>([]);
  const hasEverConnectedRef = useRef(false);
  const { settings } = useSoundSettings();
  const [webAudioReady, setWebAudioReady] = useState(false);

  const playSound = useCallback(async (soundName: 'buy' | 'sell' | 'alert') => {
    if (!settings.enabled) return;
    if (soundName === 'buy' && !settings.buyEnabled) return;
    if (soundName === 'sell' && !settings.sellEnabled) return;
    if (soundName === 'alert' && !settings.alertEnabled) return;

    // Prefer desktop Electron API
    const api = getDesktopAPI();
    if (api?.playSound) {
      try {
        await api.playSound(soundName);
        return;
      } catch (e) {
        console.error('[sound] desktopAPI.playSound failed, falling back:', e);
      }
    }

    // Fallback to Web Audio API
    if (typeof window !== 'undefined' && typeof AudioContext !== 'undefined') {
      try {
        await playSoundViaWebAudio(soundName);
      } catch (e) {
        console.error('[sound] Web Audio playback failed:', soundName, e);
      }
    }
  }, [settings]);

  useEffect(() => {
    // Warm up AudioContext on mount so first playback isn't delayed
    if (typeof window !== 'undefined' && typeof AudioContext !== 'undefined') {
      const ctx = new (window.AudioContext || (window as any).webkitAudioContext)();
      ctx.resume().then(() => {
        setWebAudioReady(true);
        // Keep a minimal oscillator connected to keep the context alive
        const osc = ctx.createOscillator();
        osc.frequency.value = 1;
        osc.connect(ctx.destination);
        osc.start();
        // Stop it after a short time, context stays alive
        setTimeout(() => {
          try { osc.stop(); } catch {}
          try { ctx.close(); } catch {}
          setWebAudioReady(false);
        }, 1000);
      }).catch(() => {});
    }
  }, []);

  useEffect(() => {
    const unsubStatus = wsBus.onStatus((connected) => {
      if (hasEverConnectedRef.current && !connected) {
        console.warn('[sound] WebSocket disconnected, playing alert sound');
        playSound('alert');
      }
      if (connected) {
        hasEverConnectedRef.current = true;
      }
    });

    const unsubPosition = wsBus.onPositionUpdate((msg: PositionUpdateMessage) => {
      const newPositions = msg.data as RiskPositionRow[];
      const oldPositions = previousPositionsRef.current;

      if (oldPositions.length > 0) {
        const { opened, closed, stoppedOut } = comparePositions(oldPositions, newPositions);
        if (opened.length > 0) playSound('buy');
        if (closed.length > 0 || stoppedOut.length > 0) playSound('sell');
      }

      previousPositionsRef.current = newPositions;
    });

    return () => {
      unsubStatus();
      unsubPosition();
    };
  }, [playSound]);
}