import React from "react";
import { Moon, Sun, Zap, Wifi, WifiOff } from "lucide-react";
import { useTheme } from "@/lib/theme";
import { useWsStatus } from "@/hooks/useWsStatus";

interface Props {
  title: string;
  subtitle?: React.ReactNode;
  actions?: React.ReactNode;
}

function WsIndicator() {
  const { dashConnected } = useWsStatus();
  return (
    <div className="flex items-center gap-1.5 px-2 py-1 rounded-md border border-border bg-surface/50">
      {dashConnected ? (
        <Wifi className="size-3 text-success" />
      ) : (
        <WifiOff className="size-3 text-destructive" />
      )}
      <span className={`text-[10px] font-mono ${dashConnected ? "text-success" : "text-destructive"}`}>
        {dashConnected ? "已连接" : "断开"}
      </span>
      <Zap className="size-3 text-warning ml-0.5" />
    </div>
  );
}

export function TopBar({ title, subtitle, actions }: Props) {
  const { theme, toggle } = useTheme();
  return (
    <header
      className="h-14 px-6 flex items-center justify-between border-b border-border bg-background/80 backdrop-blur-md sticky top-0 z-30"
      style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
    >
      <div className="flex items-center gap-4" style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}>
        <h1 className="text-[14px] font-semibold tracking-tight">{title}</h1>
        {subtitle && (
          <>
            <div className="h-3 w-px bg-border" />
            <div className="text-[11px] text-muted-foreground font-mono flex items-center gap-3">{subtitle}</div>
          </>
        )}
      </div>
      <div className="flex items-center gap-3" style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}>
        <WsIndicator />
        {actions}
        <button
          onClick={toggle}
          aria-label="切换主题"
          className="size-8 rounded-md border border-border bg-surface hover:bg-accent transition-all flex items-center justify-center"
        >
          {theme === "dark" ? <Sun className="size-3.5" /> : <Moon className="size-3.5" />}
        </button>
      </div>
    </header>
  );
}
