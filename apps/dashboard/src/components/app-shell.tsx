import { Link, useRouterState } from "@tanstack/react-router";
import { Activity, BarChart3, Shield, History, Wallet, FileText, Settings, Zap } from "lucide-react";
import type { ReactNode } from "react";

const nav = [
  { to: "/", label: "市场", icon: BarChart3 },
  { to: "/risk", label: "风控", icon: Shield },
  { to: "/history", label: "历史", icon: History },
  { to: "/accounts", label: "账号", icon: Wallet },
  { to: "/logs", label: "日志", icon: FileText },
  { to: "/settings", label: "设置", icon: Settings },
] as const;

export function AppShell({ children }: { children: ReactNode }) {
  const path = useRouterState({ select: (s) => s.location.pathname });

  return (
    <div className="flex h-screen w-full overflow-hidden bg-background text-foreground">
      <aside className="flex w-56 flex-none flex-col border-r border-sidebar-border bg-sidebar">
        <div className="flex items-center gap-3 px-5 py-5 border-b border-sidebar-border">
          <div className="grid size-9 place-items-center rounded-md bg-primary/10 ring-1 ring-primary/30">
            <Activity className="size-5 text-primary" strokeWidth={2.5} />
          </div>
          <div className="leading-tight">
            <div className="font-bold tracking-tight">PolyBet</div>
            <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">AI Terminal</div>
          </div>
        </div>

        <nav className="flex-1 px-2 py-4 space-y-0.5">
          {nav.map((item) => {
            const active = item.to === "/" ? path === "/" : path.startsWith(item.to);
            const Icon = item.icon;
            return (
              <Link
                key={item.to}
                to={item.to}
                className={[
                  "group flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors relative",
                  active
                    ? "bg-sidebar-accent text-foreground"
                    : "text-muted-foreground hover:text-foreground hover:bg-sidebar-accent/50",
                ].join(" ")}
              >
                {active && (
                  <span className="absolute left-0 top-1.5 bottom-1.5 w-0.5 rounded-full bg-primary" />
                )}
                <Icon className={["size-4", active ? "text-primary" : ""].join(" ")} />
                <span className="font-medium">{item.label}</span>
              </Link>
            );
          })}
        </nav>

        <div className="m-3 rounded-md border border-sidebar-border bg-surface px-3 py-2.5">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className="relative flex size-2">
                <span className="absolute inset-0 rounded-full bg-primary animate-flicker" />
                <span className="absolute inset-0 rounded-full bg-primary/40" />
              </span>
              <div className="leading-tight">
                <div className="text-xs font-medium">WebSocket</div>
                <div className="font-mono text-[10px] text-primary">已连接</div>
              </div>
            </div>
            <Zap className="size-3.5 text-warning" />
          </div>
        </div>
      </aside>

      <main className="flex-1 flex flex-col min-w-0 overflow-hidden">{children}</main>
    </div>
  );
}

export function PageHeader({
  left,
  right,
}: {
  left: ReactNode;
  right?: ReactNode;
}) {
  return (
    <header className="h-14 flex-none border-b border-border bg-background/60 backdrop-blur flex items-center px-6 justify-between">
      <div className="flex items-center gap-4 text-sm min-w-0">{left}</div>
      <div className="flex items-center gap-3">{right}</div>
    </header>
  );
}

export function StatusDot({ tone = "up" }: { tone?: "up" | "down" | "warning" | "muted" }) {
  const map: Record<string, string> = {
    up: "bg-up",
    down: "bg-down",
    warning: "bg-warning",
    muted: "bg-muted-foreground",
  };
  return (
    <span className="relative flex size-2">
      <span className={`absolute inset-0 rounded-full ${map[tone]} opacity-60 animate-flicker`} />
      <span className={`absolute inset-0 rounded-full ${map[tone]}`} />
    </span>
  );
}
