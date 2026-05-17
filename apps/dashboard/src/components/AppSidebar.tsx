import React from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { Activity, LineChart, Shield, History, Wallet, Settings, Loader2 } from "lucide-react";
import { useBalanceCache } from "@/hooks/useBalanceCache";
import { useAccounts } from "@/hooks/useAccounts";

const items = [
  { title: "风控", url: "/risk", icon: Shield },
  { title: "市场", url: "/market", icon: LineChart },
  { title: "历史", url: "/history", icon: History },
  { title: "账号", url: "/accounts", icon: Wallet },
  { title: "设置", url: "/settings", icon: Settings },
];

function formatUsd(n: number): string {
  return n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

export function AppSidebar() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const { balance, loading: balanceLoading } = useBalanceCache();
  const { accounts } = useAccounts();

  const accountList = accounts ?? [];
  const activeAccount = accountList.find((a) => a.isActive);
  const accountName = activeAccount?.name ?? accountList[0]?.name ?? "—";
  const activeAccountBal = balance?.polymarketAccounts?.find((a) => a.isActive)?.polymarket;
  const totalBalance = balance?.polymarket ?? activeAccountBal ?? null;

  return (
    <aside className="w-[160px] shrink-0 h-screen sticky top-0 flex flex-col bg-sidebar border-r border-sidebar-border">
      <div
        className="h-14 pl-20 pr-5 flex items-center border-b border-sidebar-border"
        style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
      >
        <div className="size-9 rounded-lg bg-brand/10 border border-brand/30 flex items-center justify-center">
          <Activity className="size-4 text-brand" />
        </div>
      </div>

      <nav className="flex-1 px-3 py-2 flex flex-col gap-0.5">
        {items.map((item) => {
          const active = pathname === item.url || pathname.startsWith(`${item.url}/`);
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

      <div className="px-3 py-3 border-t border-sidebar-border">
        <div className="px-3 py-2 rounded-md bg-sidebar-accent/40 border border-sidebar-border/50">
          <p className="text-[11px] text-sidebar-foreground truncate">{accountName}</p>
          <div className="mt-1 h-5 flex items-center">
            {balanceLoading && totalBalance == null ? (
              <Loader2 className="size-4 text-brand animate-spin" />
            ) : (
              <span className="text-[13px] font-semibold text-sidebar-foreground tabular-nums">
                {totalBalance != null ? `$${formatUsd(totalBalance)}` : "—"}
              </span>
            )}
          </div>
        </div>
      </div>
    </aside>
  );
}
