import { useEffect, useState } from "react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useGlobalWSStatus } from "@/hooks/useGlobalWSStatus";
import { postWSReconnect } from "@/lib/api";
import type { ChannelId, ChannelSnapshot } from "@/lib/wsConnectionLog";
import { cn } from "@/lib/utils";

const LABELS: Record<ChannelId, string> = {
  relay: "WS",
  ob: "OB",
  user: "USER",
};

function dotClass(display: ChannelSnapshot["display"]) {
  switch (display) {
    case "connected":
      return "bg-success animate-breathe";
    case "reconnecting":
      return "bg-warning animate-pulse";
    case "standby":
    case "unconfigured":
      return "bg-muted-foreground/40";
    default:
      return "bg-destructive";
  }
}

function statusText(ch: ChannelSnapshot) {
  switch (ch.display) {
    case "connected":
      return "已连接";
    case "reconnecting":
      return "重连中";
    case "standby":
      return "待机";
    case "unconfigured":
      return "未配置";
    default:
      return "未连接";
  }
}

function ChannelPill({ ch, onReconnect }: { ch: ChannelSnapshot; onReconnect: () => void }) {
  const [, setNow] = useState(Date.now());
  useEffect(() => {
    if (ch.display !== "reconnecting" && ch.display !== "disconnected") return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [ch.display, ch.nextRetryAt]);

  const countdown =
    ch.nextRetryAt && ch.nextRetryAt > Date.now()
      ? `将在 ${Math.max(0, Math.ceil((ch.nextRetryAt - Date.now()) / 1000))}s 后重试${ch.attempt ? ` (#${ch.attempt})` : ""}`
      : null;

  const pill = (
    <button
      type="button"
      className={cn(
        "flex items-center gap-1 px-2 py-1 rounded-md border text-[10px] font-mono transition",
        ch.display === "connected"
          ? "border-success/30 bg-success/5 text-success"
          : ch.display === "standby" || ch.display === "unconfigured"
            ? "border-border bg-surface/50 text-muted-foreground"
            : "border-warning/40 bg-warning/5 text-warning",
      )}
    >
      <span className={cn("size-1.5 rounded-full shrink-0", dotClass(ch.display))} />
      <span>{LABELS[ch.id]}</span>
      <span className="opacity-80">{statusText(ch)}</span>
    </button>
  );

  if (ch.display === "connected" && ch.id !== "relay") {
    return pill;
  }
  if (ch.display === "standby" || ch.display === "unconfigured") {
    return (
      <Popover>
        <PopoverTrigger asChild>{pill}</PopoverTrigger>
        <PopoverContent align="end" className="w-72 p-3 text-[11px]">
          <p className="text-muted-foreground">
            {ch.id === "ob" ? "无持仓时不需要盘口上游连接。" : "未配置 Polymarket 账户。"}
          </p>
        </PopoverContent>
      </Popover>
    );
  }

  return (
    <Popover>
      <PopoverTrigger asChild>{pill}</PopoverTrigger>
      <PopoverContent align="end" className="w-80 p-3 text-[11px]">
        <p className="font-semibold mb-1">
          {LABELS[ch.id]} — {statusText(ch)}
        </p>
        {countdown && <p className="text-muted-foreground mb-2">{countdown}</p>}
        {ch.lastIssue && <p className="text-warning mb-2 truncate">{ch.lastIssue}</p>}
        <div className="max-h-32 overflow-y-auto space-y-1 font-mono text-[10px] mb-2">
          {ch.events.length === 0 ? (
            <p className="text-muted-foreground">暂无日志</p>
          ) : (
            ch.events.map((ev, i) => (
              <p key={i} className={ev.level === "warn" ? "text-warning" : "text-muted-foreground"}>
                {new Date(ev.at).toLocaleTimeString()} {ev.message}
              </p>
            ))
          )}
        </div>
        <button
          type="button"
          className="w-full h-7 rounded-md border border-border hover:bg-accent text-[11px]"
          onClick={onReconnect}
        >
          立即重连
        </button>
      </PopoverContent>
    </Popover>
  );
}

export function GlobalWSStatusCluster() {
  const { channels, reconnectRelay } = useGlobalWSStatus();

  return (
    <div className="flex items-center gap-1.5">
      {channels.map((ch) => (
        <ChannelPill
          key={ch.id}
          ch={ch}
          onReconnect={() => {
            if (ch.id === "relay") {
              reconnectRelay();
            } else if (ch.id === "ob") {
              void postWSReconnect("orderbook");
            } else {
              void postWSReconnect("user");
            }
          }}
        />
      ))}
    </div>
  );
}
