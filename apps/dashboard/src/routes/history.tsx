import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect } from "react";
import { TopBar } from "@/components/TopBar";
import { PolymarketTitleLink } from "@/components/PolymarketTitleLink";
import { cn } from "@/lib/utils";
import { getStopLossHistory, getTradeHistory } from "@/lib/api";
import type { StopLossHistoryTask, OfficialTrade } from "@/lib/api";

export const Route = createFileRoute("/history")({ component: HistoryPage });

function relTime(iso: string): string {
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t)) return '';
  const sec = Math.floor((Date.now() - t) / 1000);
  if (sec < 60) return '刚刚';
  const m = Math.floor(sec / 60);
  if (m < 60) return `${m}分前`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}时前`;
  const d = Math.floor(h / 24);
  if (d < 30) return `${d}天前`;
  return `${Math.floor(d / 30)}月前`;
}

function fmtUsd(n: number): string {
  return n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

export default function HistoryPage() {
  const [activeTab, setActiveTab] = useState<'stop_loss' | 'trades'>('stop_loss');
  const [stopLossTasks, setStopLossTasks] = useState<StopLossHistoryTask[]>([]);
  const [officialTrades, setOfficialTrades] = useState<OfficialTrade[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchData = async () => {
    setLoading(true);
    setError(null);
    try {
      const [sl, tr] = await Promise.all([
        getStopLossHistory(50),
        getTradeHistory(50),
      ]);
      setStopLossTasks(Array.isArray(sl.tasks) ? sl.tasks : []);
      setOfficialTrades(Array.isArray(tr.trades) ? tr.trades : []);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载历史失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  return (
    <>
      <TopBar
        title="历史记录"
        subtitle={
          <span className="flex items-center gap-3">
            <span>止损记录 {stopLossTasks.length} 条</span>
            <span className="text-border">·</span>
            <span>成交记录 {officialTrades.length} 条</span>
          </span>
        }
        actions={
          <button
            onClick={fetchData}
            disabled={loading}
            className="h-8 px-3 text-[12px] rounded-md border border-border bg-surface hover:bg-accent transition flex items-center gap-1.5 disabled:opacity-50"
          >
            {loading ? '加载中...' : '刷新'}
          </button>
        }
      />

      <div className="p-6 space-y-4 animate-slide-up">
        {error && (
          <div className="p-4 rounded-md border border-destructive/30 bg-destructive/10 text-destructive text-[12px]">
            {error}
          </div>
        )}

        {/* Sub-tabs */}
        <div className="flex items-center gap-1">
          <button
            onClick={() => setActiveTab('stop_loss')}
            className={cn(
              "px-3 py-1.5 text-[11.5px] font-medium rounded-md transition",
              activeTab === 'stop_loss'
                ? "bg-brand text-brand-foreground"
                : "text-muted-foreground hover:bg-accent"
            )}
          >
            止损触发记录
          </button>
          <button
            onClick={() => setActiveTab('trades')}
            className={cn(
              "px-3 py-1.5 text-[11.5px] font-medium rounded-md transition",
              activeTab === 'trades'
                ? "bg-brand text-brand-foreground"
                : "text-muted-foreground hover:bg-accent"
            )}
          >
            官方成交记录
          </button>
        </div>

        {loading ? (
          <div className="text-center py-12 text-muted-foreground">加载中...</div>
        ) : activeTab === 'stop_loss' ? (
          <section className="surface rounded-xl border border-border overflow-hidden">
            <div className="px-5 py-3.5 border-b border-border flex items-center justify-between">
              <h2 className="text-[13px] font-semibold">止损触发记录</h2>
              <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-widest">{stopLossTasks.length} 条</span>
            </div>
            {stopLossTasks.length === 0 ? (
              <div className="p-8 text-center text-muted-foreground text-[12px]">
                暂无止损触发记录
              </div>
            ) : (
              <table className="w-full text-[12px]">
                <thead className="text-[10px] uppercase tracking-widest text-muted-foreground bg-background/40">
                  <tr>
                    {["时间", "市场", "持仓 ID", "状态", "尝试次数", ""].map((h) => (
                      <th key={h} className="px-4 py-2.5 font-medium text-left">{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {stopLossTasks.map((t) => (
                    <tr key={t.id} className="hover:bg-accent/30 transition-colors">
                      <td className="px-4 py-3 text-muted-foreground">{t.updatedAt.slice(0, 16).replace('T', ' ')}</td>
                      <td className="px-4 py-3 max-w-[220px]">
                        {t.title ? (
                          <PolymarketTitleLink
                            title={t.title}
                            officialUrl={t.officialUrl}
                            titleClassName="text-[12px] font-medium"
                          />
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                      <td className="px-4 py-3 font-mono text-muted-foreground">
                        {t.positionId ? `${t.positionId.slice(0, 12)}…` : '—'}
                      </td>
                      <td className="px-4 py-3">
                        <span className={cn(
                          "text-[10px] px-1.5 py-0.5 rounded font-medium",
                          t.status === 'succeeded' && "bg-success/10 text-success",
                          t.status === 'failed' && "bg-destructive/10 text-destructive",
                          t.status === 'pending' && "bg-warning/10 text-warning",
                        )}>
                          {t.status}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">{t.attempts}</td>
                      <td className="px-4 py-3">
                        {t.lastError && (
                          <span className="text-destructive text-[11px]" title={t.lastError}>
                            {t.lastError.length > 30 ? `${t.lastError.slice(0, 30)}…` : t.lastError}
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
        ) : (
          <section className="space-y-0 divide-y divide-border surface rounded-xl border border-border overflow-hidden">
            {officialTrades.length === 0 ? (
              <div className="p-8 text-center text-muted-foreground text-[12px]">
                暂无官方成交记录
              </div>
            ) : (
              officialTrades.map((t) => {
                const isBuy = t.side === 'buy';
                const value = t.size * t.price;

                return (
                  <div key={t.id} className="flex items-center gap-4 px-4 py-3 hover:bg-accent/20 transition-colors">
                    {/* Left: side icon */}
                    <div className="shrink-0 flex flex-col items-center gap-0.5 w-10">
                      <div className={cn(
                        "size-7 rounded-full flex items-center justify-center border",
                        isBuy
                          ? "bg-muted border-border text-muted-foreground"
                          : "bg-muted border-border text-muted-foreground"
                      )}>
                        {isBuy ? (
                          <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><line x1="6" y1="2" x2="6" y2="10"/><line x1="2" y1="6" x2="10" y2="6"/></svg>
                        ) : (
                          <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><line x1="2" y1="6" x2="10" y2="6"/></svg>
                        )}
                      </div>
                      <span className="text-[10px] text-muted-foreground">{isBuy ? '买入' : '卖出'}</span>
                    </div>

                    {/* Middle: market info */}
                    <div className="flex-1 min-w-0 flex items-center gap-3">
                      {t.icon ? (
                        <img src={t.icon} alt="" className="size-9 rounded-lg object-contain bg-gradient-to-br from-brand/20 to-brand/5 border border-brand/10 shrink-0" />
                      ) : (
                        <div className="size-9 rounded-lg bg-gradient-to-br from-brand/20 to-brand/5 border border-brand/10 flex items-center justify-center text-brand font-bold text-[13px] shrink-0">
                          {t.title.charAt(0).toUpperCase()}
                        </div>
                      )}
                      <div className="min-w-0">
                        <PolymarketTitleLink
                          title={t.title}
                          officialUrl={t.officialUrl}
                          polySlug={t.polySlug}
                          titleClassName="text-[13px] font-medium"
                        />
                        <div className="mt-0.5 flex items-center gap-2">
                          <span className={cn(
                            "text-[11px] px-1.5 py-0.5 rounded font-medium",
                            isBuy ? "bg-brand/10 text-brand" : "bg-destructive/10 text-destructive"
                          )}>
                            {t.outcome} {t.priceCents.toFixed(0)}¢
                          </span>
                          <span className="text-[11px] text-muted-foreground">{t.size.toFixed(2)} 份额</span>
                        </div>
                      </div>
                    </div>

                    {/* Right: value + time */}
                    <div className="shrink-0 text-right">
                      <p className={cn(
                        "text-[13px] font-semibold tabular-nums",
                        isBuy ? "text-foreground" : "text-success"
                      )}>
                        {isBuy ? '-' : '+'}{`$${fmtUsd(value)}`}
                      </p>
                      <p className="text-[11px] text-muted-foreground mt-0.5 tabular-nums">
                        {relTime(t.timestamp)}
                      </p>
                    </div>
                  </div>
                );
              })
            )}
          </section>
        )}
      </div>
    </>
  );
}
