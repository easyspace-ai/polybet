import { useEffect, useRef, useCallback, useState } from "react";
import { wsBus, type PositionUpdateMessage } from "@/lib/wsBus";
import type { RiskPositionRow } from "@/lib/api";
import { useSoundSettings } from "./useSoundSettings";
import { toast } from "sonner";

interface DesktopAPI {
  playSound?: (soundName: string) => Promise<{ ok: true } | { ok: false; error: string }>;
}

const SOUND_ASSETS: Record<string, string> = {
  buy: "/sounds/buy.mp3",
  sell: "/sounds/sell.mp3",
  alert: "/sounds/alert.mp3",
};

function getDesktopAPI(): DesktopAPI | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { desktopAPI?: DesktopAPI }).desktopAPI;
}

function hasAudioContext(): boolean {
  return (
    typeof window !== "undefined" &&
    (typeof window.AudioContext !== "undefined" ||
      typeof (window as unknown as { webkitAudioContext?: unknown }).webkitAudioContext !==
        "undefined")
  );
}

type AudioContextType = { new (): AudioContext };

function createAudioContext(): AudioContext | null {
  const AC =
    (window as unknown as { AudioContext: AudioContextType }).AudioContext ||
    (window as unknown as { webkitAudioContext: AudioContextType }).webkitAudioContext;
  return AC ? new AC() : null;
}

function comparePositions(
  oldPositions: RiskPositionRow[],
  newPositions: RiskPositionRow[],
): { opened: string[]; closed: string[]; stoppedOut: string[] } {
  const oldIds = new Set(oldPositions.map((p) => p.id));
  const newIds = new Set(newPositions.map((p) => p.id));
  const oldMap = new Map(oldPositions.map((p) => [p.id, p]));

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

  const ctx = createAudioContext();
  if (!ctx) throw new Error("AudioContext not supported");

  try {
    const res = await fetch(url);
    const arrayBuf = await res.arrayBuffer();
    const audioBuf = await ctx.decodeAudioData(arrayBuf);
    const src = ctx.createBufferSource();
    src.buffer = audioBuf;
    src.connect(ctx.destination);
    src.start(0);
  } finally {
    setTimeout(() => ctx.close(), 2000);
  }
}

export function useSoundNotifications() {
  const previousPositionsRef = useRef<RiskPositionRow[]>([]);
  const hasEverConnectedRef = useRef(false);
  const { settings } = useSoundSettings();
  const [, setWebAudioReady] = useState(false);
  const polyWarnedRef = useRef<{ orderbook?: boolean; user?: boolean; hasWarned?: boolean }>({});

  const playSound = useCallback(
    async (soundName: "buy" | "sell" | "alert") => {
      if (!settings.enabled) return;
      if (soundName === "buy" && !settings.buyEnabled) return;
      if (soundName === "sell" && !settings.sellEnabled) return;
      if (soundName === "alert" && !settings.alertEnabled) return;

      // Prefer desktop Electron API
      const api = getDesktopAPI();
      if (api?.playSound) {
        try {
          await api.playSound(soundName);
          return;
        } catch (e) {
          console.error("[sound] desktopAPI.playSound failed, falling back:", e);
        }
      }

      // Fallback to Web Audio API
      if (hasAudioContext()) {
        try {
          await playSoundViaWebAudio(soundName);
        } catch (e) {
          console.error("[sound] Web Audio playback failed:", soundName, e);
        }
      }
    },
    [settings],
  );

  useEffect(() => {
    // Warm up AudioContext on mount so first playback isn't delayed
    if (hasAudioContext()) {
      const ctx = createAudioContext();
      if (ctx) {
        ctx.resume().then(
          () => {
            setWebAudioReady(true);
            // Keep a minimal oscillator to keep the context alive briefly
            const osc = ctx.createOscillator();
            osc.frequency.value = 1;
            osc.connect(ctx.destination);
            osc.start();
            setTimeout(() => {
              try {
                osc.stop();
              } catch {
                /* noop */
              }
              try {
                ctx.close();
              } catch {
                /* noop */
              }
              setWebAudioReady(false);
            }, 1000);
          },
          () => {
            /* context resume failed, ignore */
          },
        );
      }
    }
  }, []);

  useEffect(() => {
    const unsubStatus = wsBus.onStatus((connected) => {
      if (hasEverConnectedRef.current && !connected) {
        console.warn("[sound] WebSocket disconnected, playing alert sound");
        playSound("alert");
      }
      if (connected) {
        hasEverConnectedRef.current = true;
      }
    });

    const unsubPolyStatus = wsBus.onPolyStatus((msg) => {
      const lostOrderbook = msg.polyOrderbookConnected === false && !polyWarnedRef.current.orderbook;
      const lostUser = msg.polyUserConnected === false && !polyWarnedRef.current.user;
      if (lostOrderbook) {
        polyWarnedRef.current.orderbook = true;
        polyWarnedRef.current.hasWarned = true;
        playSound("alert");
        toast.error("Polymarket 盘口连接断开", { duration: 5000 });
      }
      if (lostUser) {
        polyWarnedRef.current.user = true;
        polyWarnedRef.current.hasWarned = true;
        playSound("alert");
        toast.error("Polymarket 用户连接断开", { duration: 5000 });
      }
      const orderbookOk = msg.polyOrderbookConnected !== false;
      const userOk = msg.polyUserConnected !== false;
      if (orderbookOk) polyWarnedRef.current.orderbook = false;
      if (userOk) polyWarnedRef.current.user = false;
      if (polyWarnedRef.current.hasWarned && orderbookOk && userOk) {
        polyWarnedRef.current.hasWarned = false;
        toast.success("Polymarket 连接已恢复", { duration: 3000 });
      }
    });

    const unsubPosition = wsBus.onPositionUpdate((msg: PositionUpdateMessage) => {
      const newPositions = msg.data as RiskPositionRow[];
      const oldPositions = previousPositionsRef.current;

      if (oldPositions.length > 0) {
        const { opened, closed, stoppedOut } = comparePositions(oldPositions, newPositions);
        if (opened.length > 0) playSound("buy");
        if (closed.length > 0 || stoppedOut.length > 0) playSound("sell");
      }

      previousPositionsRef.current = newPositions;
    });

    return () => {
      unsubStatus();
      unsubPolyStatus();
      unsubPosition();
    };
  }, [playSound]);
}
