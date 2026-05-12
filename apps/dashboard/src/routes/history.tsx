import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { TopBar } from "@/components/TopBar";
import { ChevronLeft, ChevronRight, RefreshCw } from "lucide-react";
import { useTrades } from "@/hooks/useTrades";
import { useBalanceCache } from "@/hooks/useBalanceCache";
import { formatOdds, type OddsFormat } from "@/lib/oddsFormat";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/history")({ component: HistoryPage });

function formatUsd(n: number): string {
  return n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  const hh = String(d.getHours()).padStart(2, '0');
  const mi = String(d.getMinutes()).padStart(2, '0');
  return `${mm}/${dd} ${hh}:${mi}`;
}

function statusColor(status: string): string {
  if (status === 'filled') return 'text-success';
  if (status === 'failed') return 'text-danger';
  return 'text-warning';
}

function statusLabelZh(status: string): string {
  if (status === 'filled') return '已成交';
  if (status === 'failed') return '失败';
  if (status === 'partial') return '部分';
  return status;
}

function sideLabelZh(side: string): string {
  if (side === 'buy') return '买入';
  if (side === 'sell') return '卖出';
  return side.toUpperCase();
}

function HistoryPage() {
  const [page, setPage] = useState(1);
  const { trades, total, loading, wsConnected: tradesWsConnected, lastRefresh: tradesLastRefresh } = useTrades(page, 20);
  const { balance, refresh: refreshBalance, wsConnected: balanceWsConnected } = useBalanceCache();
  const limit = 20;
  const totalPages = Math.max(1, Math.ceil(total / limit));

  const format: OddsFormat = 'percent';

  return (
    <>
      <TopBar
        title="交易历史"
        subtitle={
          <span className="flex items-center gap-3">
            <span className="flex items-center gap-1.5">
              <span className={`size-1.5 rounded-full ${balanceWsConnected ? 'bg-success' : 'bg-warning'}`} />
              {balanceWsConnected ? '实时' : '离线'}
            </span>
            <span className="text-border">·</span>
            <span>共 {total} 笔成交</span>
          </span>
        }
        actions={
          <div className="flex items-center gap-2 text-[11.5px] font-mono">
            <button 
              onClick={() => setPage(p => p - 1)} 
              disabled={page === 1}
              className="size-7 rounded border border-border hover:bg-accent transition flex items-center justify-center disabled:opacity-50"
            >
              <ChevronLeft className="size-3.5" />
            </button>
            <span className="text-muted-foreground">{page} / {totalPages}</span>
            <button 
              onClick={() => setPage(p => p + 1)} 
              disabled={page >= totalPages}
              className="size-7 rounded border border-border hover:bg-accent transition flex items-center justify-center disabled:opacity-50"
            >
              <ChevronRight className="size-3.5" />
            </button>
          </div>
        }
      />

      <div className="p-6 space-y-5 animate-slide-up">
        <section className="surface rounded-xl border border-border p-5 relative overflow-hidden">
          <div className="absolute -right-12 -top-12 size-48 rounded-full bg-brand/10 blur-3xl pointer-events-none" />
          <div className="flex items-center justify-between">
            <div>
              <p className="text-[10px] font-mono uppercase tracking-widest text-brand">Polymarket · 当前账号</p>
              <p className="mt-2 text-3xl font-semibold num tracking-tight">
                {balance?.polymarket != null ? `$${formatUsd(balance.polymarket)}` : '—'}
              </p>
            </div>
            <button 
              onClick={() => refreshBalance()}
              className="size-8 rounded-md border border-border bg-surface-elevated hover:bg-accent transition flex items-center justify-center"
            >
              <RefreshCw className="size-3.5" />
            </button>
          </div>
        </section>

        {(balance?.polymarketAccounts?.length ?? 0) > 0 && (
          <section className="surface rounded-xl border border-border">
            <div className="px-5 py-3 border-b border-border flex items-center justify-between">
              <p className="text-[10px] font-mono uppercase tracking-widest text-muted-foreground">各账号 pUSD</p>
            </div>
            {balance!.polymarketAccounts!.map((row) => (
              <div key={row.id} className="px-5 py-4 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-[12.5px]">{row.name}</span>
                  {row.isActive && (
                    <>
                      <span className="text-muted-foreground text-[11px]">·</span>
                      <span className="text-brand text-[11px]">当前</span>
                    </>
                  )}
                </div>
                <span className="num text-[14px] font-medium">
                  {row.polymarket == null ? '—' : `$${formatUsd(row.polymarket)}`}
                </span>
              </div>
            ))}
          </section>
        )}

        <section className="surface rounded-xl border border-border overflow-hidden">
          <table className="w-full text-[12px]">
            <thead className="text-[10px] uppercase tracking-widest text-muted-foreground bg-background/40">
              <tr>
                {["时间", "市场", "选项", "源", "方向", "金额", "成交盘", "状态", "链上"].map((h) => (
                  <th key={h} className="px-5 py-3 font-medium text-left">{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={9} className="px-5 py-16 text-center text-muted-foreground">
                    加载中...
                  </td>
                </tr>
              ) : trades.length === 0 ? (
                <tr>
                  <td colSpan={9} className="px-5 py-16 text-center">
                    <div className="inline-flex flex-col items-center gap-3 text-muted-foreground">
                      <div className="size-10 rounded-full border border-dashed border-border flex items-center justify-center">
                        <div className="size-2 rounded-full bg-muted-foreground/40" />
                      </div>
                      <p className="text-[12px]">暂无成交记录</p>
                    </div>
                  </td>
                </tr>
              ) : (
                trades.map((t) => {
                  const size = t.executedSize != null ? t.executedSize : t.requestedSize;
                  return (
                    <tr key={t.id} className="border-b border-border/50 hover:bg-accent/30">
                      <td className="px-5 py-3 font-mono text-[10px] text-muted-foreground">{formatDate(t.createdAt)}</td>
                      <td className="px-5 py-3 text-[12px] truncate max-w-[200px]">{t.marketName}</td>
                      <td className="px-5 py-3 text-[12px] text-muted-foreground truncate max-w-[100px]">{t.outcomeLabel}</td>
                      <td className="px-5 py-3 text-[10px] text-muted-foreground">POLY</td>
                      <td className={cn("px-5 py-3 font-mono text-[10px] font-semibold", t.side === 'buy' ? "text-brand" : "text-muted-foreground")}>
                        {sideLabelZh(t.side)}
                      </td>
                      <td className="px-5 py-3 text-right font-mono text-[12px] font-semibold">{size.toFixed(2)}</td>
                      <td className="px-5 py-3 text-right font-mono text-[12px] font-semibold">{formatOdds(t.fillOdds, format)}</td>
                      <td className={cn("px-5 py-3 font-mono text-[10px] font-semibold tracking-wider", statusColor(t.status))}>
                        ● {statusLabelZh(t.status)}
                      </td>
                      <td className="px-5 py-3">
                        {t.txHash ? (
                          <a
                            href={`https://polygonscan.com/tx/${t.txHash}`}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="font-mono text-[10px] text-brand hover:underline"
                          >
                            {t.txHash.slice(0, 6)}…↗
                          </a>
                        ) : (
                          <span className="font-mono text-[10px] text-muted-foreground">—</span>
                        )}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </section>
      </div>
    </>
  );
}