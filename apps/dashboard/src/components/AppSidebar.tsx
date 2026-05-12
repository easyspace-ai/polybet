import { Link, useRouterState } from "@tanstack/react-router";
import { Activity, LineChart, Shield, History, Wallet, FileText, Settings, Zap } from "lucide-react";

const items = [
  { title: "市场", url: "/", icon: LineChart },
  { title: "风控", url: "/risk", icon: Shield },
  { title: "历史", url: "/history", icon: History },
  { title: "账号", url: "/accounts", icon: Wallet },
  { title: "日志", url: "/logs", icon: FileText },
  { title: "设置", url: "/settings", icon: Settings },
];

export function AppSidebar() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  return (
    <aside className="w-[160px] shrink-0 h-screen sticky top-0 flex flex-col bg-sidebar border-r border-sidebar-border">
      <div className="px-5 py-5 flex items-center gap-3">
        <div className="size-9 rounded-lg bg-brand/10 border border-brand/30 flex items-center justify-center">
          <Activity className="size-4 text-brand" />
        </div>
        <div className="flex flex-col leading-tight">
          <span className="text-[15px] font-semibold tracking-tight">PolyBet</span>
          <span className="text-[10px] text-muted-foreground font-mono uppercase tracking-widest">AI Terminal</span>
        </div>
      </div>

      <nav className="flex-1 px-3 py-2 flex flex-col gap-0.5">
        {items.map((item) => {
          const active = item.url === "/" ? pathname === "/" : pathname.startsWith(item.url);
          const Icon = item.icon;
          return (
            <Link
              key={item.url}
              to={item.url}
              className={[
                "group relative flex items-center gap-3 px-3 py-2 rounded-md text-[13px] transition-all duration-200",
                active
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-sidebar-accent/50",
              ].join(" ")}
            >
              {active && (
                <span className="absolute left-0 top-1/2 -translate-y-1/2 h-5 w-[3px] rounded-r-full bg-brand shadow-[0_0_10px_color-mix(in_oklab,var(--color-brand)_60%,transparent)]" />
              )}
              <Icon className={`size-4 shrink-0 transition-colors ${active ? "text-brand" : ""}`} />
              <span className="font-medium">{item.title}</span>
            </Link>
          );
        })}
      </nav>

      <div className="px-3 py-3 mx-3 mb-4 rounded-md border border-sidebar-border bg-sidebar-accent/40">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="size-1.5 rounded-full bg-success animate-breathe" />
            <span className="text-[11px] font-medium">WebSocket</span>
          </div>
          <Zap className="size-3.5 text-warning" />
        </div>
        <div className="mt-1 text-[10px] text-muted-foreground font-mono">已连接 · 14ms</div>
      </div>
    </aside>
  );
}
