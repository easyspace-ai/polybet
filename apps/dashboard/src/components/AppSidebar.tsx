import React from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { Activity, LineChart, Radio, History, Wallet, Settings, Loader2, Copy } from "lucide-react";
import { useBalanceCache } from "@/hooks/useBalanceCache";
import { useAccounts } from "@/hooks/useAccounts";
import { useMonitorCache } from "@/hooks/useMonitorCache";
import { abbreviateAddress } from "@/lib/formatAddress";
import { toast } from "sonner";

const items = [
  { title: "监控", url: "/monitor", icon: Radio },
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
  const { positions } = useMonitorCache();

  const accountList = accounts ?? [];
  const activeAccount = accountList.find((a) => a.isActive);
  const accountName = activeAccount?.name ?? accountList[0]?.name ?? "—";
  const funderAddress = activeAccount?.funderAddress?.trim() ?? "";
  const activeAccountBal = balance?.polymarketAccounts?.find((a) => a.isActive)?.polymarket;
  const totalBalance = balance?.polymarket ?? activeAccountBal ?? null;

  const positionTotalUsd = positions.reduce((sum, p) => {
    const v = p.valueUsd;
    return sum + (v != null && Number.isFinite(v) ? v : 0);
  }, 0);
  const positionCount = positions.length;

  const copyAddr = async () => {
    if (!funderAddress) return;
    try {
      await navigator.clipboard.writeText(funderAddress);
      toast.success("已复制代理地址");
    } catch {
      toast.error("复制失败");
    }
  };

  return (
    <aside className="w-[160px] shrink-0 h-full flex flex-col bg-sidebar border-r border-sidebar-border">
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

      <div className="px-3 py-3 border-t border-sidebar-border space-y-2">
        <div className="px-3 py-2 rounded-md bg-sidebar-accent/40 border border-sidebar-border/50">
          <p className="text-[10px] text-muted-foreground">持仓总额</p>
          <p className="text-[14px] font-semibold text-sidebar-foreground tabular-nums mt-0.5">
            ${formatUsd(positionTotalUsd)}
          </p>
          <p className="text-[10px] text-muted-foreground mt-1">
            {positionCount} 个持仓
          </p>
        </div>
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
          {funderAddress && (
            <button
              type="button"
              onClick={copyAddr}
              title={funderAddress}
              className="mt-1.5 flex items-center gap-1 text-[10px] font-mono text-muted-foreground hover:text-foreground transition w-full"
            >
              <span className="truncate">{abbreviateAddress(funderAddress)}</span>
              <Copy className="size-2.5 shrink-0 opacity-70" />
            </button>
          )}
        </div>
      </div>
    </aside>
  );
}
