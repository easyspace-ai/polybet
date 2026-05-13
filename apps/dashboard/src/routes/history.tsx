import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { TopBar } from "@/components/TopBar";
import { ChevronLeft, ChevronRight, Plus, Minus, ExternalLink } from "lucide-react";
import { useTrades } from "@/hooks/useTrades";
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

function sideLabelZh(side: string): string {
  if (side === 'buy') return '买入';
  if (side === 'sell') return '卖出';
  return side.toUpperCase();
}

function tradeValue(t: { side: string; executedSize: number | null; requestedSize: number; fillOdds: number | null; requestedOdds: number; status: string }): number | null {
  if (t.status === 'failed') return null;
  const size = t.executedSize ?? t.requestedSize;
  const odds = t.fillOdds ?? t.requestedOdds;
  if (odds == null || !isFinite(odds)) return null;
  const val = size * odds;
  if (t.side === 'buy') return -val;
  return val;
}

function MarketIcon({ name }: { name: string }) {
  const initial = name.charAt(0).toUpperCase();
  return (
    <div className="size-9 rounded-lg bg-gradient-to-br from-brand/20 to-brand/5 border border-brand/10 flex items-center justify-center text-brand font-bold text-[13px] shrink-0">
      {initial}
    </div>
  );
}

function HistoryPage() {
  const [page, setPage] = useState(1);
  const { trades, total, loading, wsConnected: tradesWsConnected, lastRefresh: tradesLastRefresh } = useTrades(page, 20);
  const limit = 20;
  const totalPages = Math.max(1, Math.ceil(total / limit));

  return (
    <>
      <TopBar
        title="交易历史"
        subtitle={
          <span className="flex items-center gap-3">
            <span className="flex items-center gap-1.5">
              <span className={`size-1.5 rounded-full ${tradesWsConnected ? 'bg-success' : 'bg-warning'}`} />
              {tradesWsConnected ? '实时' : '离线'}
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

      <div className="p-6 space-y-3 animate-slide-up">
        {loading ? (
          <div className="surface rounded-xl border border-border p-10 text-center text-muted-foreground text-[12px]">
            加载中...
          </div>
        ) : trades.length === 0 ? (
          <div className="surface rounded-xl border border-border p-10 text-center">
            <div className="inline-flex flex-col items-center gap-3 text-muted-foreground">
              <div className="size-10 rounded-full border border-dashed border-border flex items-center justify-center">
                <div className="size-2 rounded-full bg-muted-foreground/40" />
              </div>
              <p className="text-[12px]">暂无成交记录</p>
            </div>
          </div>
        ) : (
          trades.map((t) => {
            const size = t.executedSize ?? t.requestedSize;
            const val = tradeValue(t);
            const isBuy = t.side === 'buy';
            const isSell = t.side === 'sell';
            const marketUrl = t.officialUrl || `https://polymarket.com/search?q=${encodeURIComponent(t.marketName)}`;

            return (
              <div
                key={t.id}
                className="surface rounded-xl border border-border p-4 flex items-center gap-4 hover:border-brand/20 transition-colors"
              >
                {/* Left: action icon */}
                <div className="shrink-0 flex flex-col items-center gap-1 w-12">
                  <div
                    className={cn(
                      "size-8 rounded-full flex items-center justify-center border",
                      isBuy
                        ? "bg-muted border-border text-muted-foreground"
                        : isSell
                          ? "bg-muted border-border text-muted-foreground"
                          : "bg-success/10 border-success/20 text-success"
                    )}
                  >
                    {isBuy ? <Plus className="size-4" /> : isSell ? <Minus className="size-4" /> : <span className="text-xs">✓</span>}
                  </div>
                  <span className="text-[11px] text-muted-foreground">{sideLabelZh(t.side)}</span>
                </div>

                {/* Middle: market info */}
                <div className="flex-1 min-w-0">
                  <a
                    href={marketUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-2 group"
                  >
                    <MarketIcon name={t.marketName} />
                    <span className="text-[13px] font-medium truncate group-hover:text-brand transition-colors">
                      {t.marketName}
                    </span>
                    <ExternalLink className="size-3 text-muted-foreground shrink-0 opacity-0 group-hover:opacity-100 transition-opacity" />
                  </a>
                  <div className="mt-1.5 flex items-center gap-2">
                    <span
                      className={cn(
                        "text-[11px] px-1.5 py-0.5 rounded font-medium",
                        isBuy
                          ? "bg-brand/10 text-brand"
                          : "bg-destructive/10 text-destructive"
                      )}
                    >
                      {t.outcomeLabel}
                    </span>
                    <span className="text-[11px] text-muted-foreground">
                      {size.toFixed(2)} 份额
                    </span>
                    {t.status === 'failed' && (
                      <span className="text-[11px] text-destructive font-medium">失败</span>
                    )}
                  </div>
                </div>

                {/* Right: value + time */}
                <div className="shrink-0 text-right">
                  <p
                    className={cn(
                      "text-[13px] font-semibold tabular-nums",
                      val == null
                        ? "text-muted-foreground"
                        : val >= 0
                          ? "text-success"
                          : "text-destructive"
                    )}
                  >
                    {val == null ? '—' : `${val >= 0 ? '+' : ''}$${formatUsd(Math.abs(val))}`}
                  </p>
                  <p className="text-[11px] text-muted-foreground mt-0.5 tabular-nums">
                    {formatDate(t.createdAt)}
                  </p>
                </div>
              </div>
            );
          })
        )}
      </div>
    </>
  );
}
