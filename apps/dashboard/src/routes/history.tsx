import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect, useCallback, useMemo } from "react";
import { RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { TopBar } from "@/components/TopBar";
import { RecordFiltersBar } from "@/components/RecordFiltersBar";
import { PolymarketTitleLink } from "@/components/PolymarketTitleLink";
import { cn } from "@/lib/utils";
import { getStopLossHistory, getTradeHistory, postStopLossHistoryClear } from "@/lib/api";
import { runUnifiedMarketsRefresh } from "@/lib/unifiedRefresh";
import type { StopLossHistoryTask, OfficialTrade } from "@/lib/api";
import { useConfig } from "@/hooks/useConfig";
import {
  DEFAULT_EVENT_CLASSIFICATION_TAGS,
  parseEventClassificationTags,
} from "@/lib/eventClassification";
import {
  DEFAULT_RECORD_FILTERS,
  type RecordFilterState,
  recordTimestampInRange,
  matchesLeagueTag,
  matchesMarketQuery,
  sortByUpdatedDesc,
} from "@/lib/recordFilters";
import { formatHistoryTimestamp } from "@/lib/kickoffTime";

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

function applyRecordFilters<T extends { title?: string; league?: string; updatedAt: string; createdAt: string }>(
  rows: T[],
  filters: RecordFilterState,
): T[] {
  return rows.filter((row) => {
    if (!matchesLeagueTag(row.league, undefined, filters.league)) return false;
    if (!recordTimestampInRange(row.updatedAt || row.createdAt, filters)) return false;
    if (!matchesMarketQuery(row.title, filters.marketQuery)) return false;
    return true;
  });
}

export default function HistoryPage() {
  const [activeTab, setActiveTab] = useState<'stop_loss' | 'trades'>('stop_loss');
  const [stopLossTasks, setStopLossTasks] = useState<StopLossHistoryTask[]>([]);
  const [officialTrades, setOfficialTrades] = useState<OfficialTrade[]>([]);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [clearingStopLoss, setClearingStopLoss] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filters, setFilters] = useState<RecordFilterState>(DEFAULT_RECORD_FILTERS);
  const { rows: configRows } = useConfig();

  const configuredTags = useMemo(() => {
    const raw = configRows.find((r) => r.key === 'eventClassificationTags')?.value ?? '';
    const tags = parseEventClassificationTags(raw);
    return tags.length > 0 ? tags : DEFAULT_EVENT_CLASSIFICATION_TAGS;
  }, [configRows]);

  const fetchHistoryRecords = useCallback(async (syncOfficial = false) => {
    const [sl, tr] = await Promise.all([
      getStopLossHistory(200, syncOfficial),
      getTradeHistory(50, syncOfficial),
    ]);
    setStopLossTasks(sortByUpdatedDesc(Array.isArray(sl.tasks) ? sl.tasks : []));
    setOfficialTrades(Array.isArray(tr.trades) ? tr.trades : []);
  }, []);

  const fetchData = useCallback(async (opts?: { initial?: boolean; unifiedRefresh?: boolean }) => {
    const initial = opts?.initial ?? false;
    const unified = opts?.unifiedRefresh ?? false;
    if (unified) {
      setRefreshing(true);
      setStopLossTasks([]);
      setOfficialTrades([]);
    } else if (initial) {
      setLoading(true);
    }
    setError(null);
    try {
      if (unified) {
        await runUnifiedMarketsRefresh();
      }
      await fetchHistoryRecords(unified);
      if (unified) {
        toast.success('已刷新', { description: '市场缓存已重建，历史记录已更新' });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载历史失败');
      if (unified) {
        toast.error('刷新失败', { description: err instanceof Error ? err.message : '请稍后重试' });
      }
    } finally {
      if (unified) {
        setRefreshing(false);
      } else if (initial) {
        setLoading(false);
      }
    }
  }, [fetchHistoryRecords]);

  const handleUnifiedRefresh = () => void fetchData({ unifiedRefresh: true });

  const handleClearStopLossHistory = async () => {
    if (!window.confirm('确定清空所有止损触发记录？此操作不可恢复。')) {
      return;
    }
    setClearingStopLoss(true);
    try {
      const r = await postStopLossHistoryClear();
      setStopLossTasks([]);
      toast.success('已清空止损记录', { description: `已删除 ${r.deleted} 条` });
    } catch (err) {
      toast.error('清空失败', { description: err instanceof Error ? err.message : '请稍后重试' });
    } finally {
      setClearingStopLoss(false);
    }
  };

  useEffect(() => {
    void fetchData({ initial: true });
  }, [fetchData]);

  const marketSuggestions = useMemo(() => {
    const titles = new Set<string>();
    for (const t of stopLossTasks) {
      if (t.title?.trim()) titles.add(t.title.trim());
    }
    return [...titles].sort();
  }, [stopLossTasks]);

  const filteredStopLoss = useMemo(
    () => applyRecordFilters(stopLossTasks, filters),
    [stopLossTasks, filters],
  );

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      <TopBar
        title="历史记录"
        subtitle={
          <span className="flex items-center gap-3">
            <span>止损 {filteredStopLoss.length}/{stopLossTasks.length} 条</span>
            <span className="text-border">·</span>
            <span>成交记录 {officialTrades.length} 条</span>
          </span>
        }
        actions={
          <button
            onClick={handleUnifiedRefresh}
            disabled={loading || refreshing}
            className="h-8 px-3 text-[12px] rounded-md border border-border bg-surface hover:bg-accent transition flex items-center gap-1.5 disabled:opacity-50"
          >
            <RefreshCw className={cn("size-3.5", refreshing && "animate-spin")} />
            {refreshing ? '刷新中...' : '刷新'}
          </button>
        }
      />

      <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden p-6 space-y-4 animate-slide-up scrollbar-thin">
        {error && (
          <div className="p-4 rounded-md border border-destructive/30 bg-destructive/10 text-destructive text-[12px]">
            {error}
          </div>
        )}

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

        {activeTab === 'stop_loss' && (
          <RecordFiltersBar
            filters={filters}
            onChange={setFilters}
            configuredTags={configuredTags}
            marketSuggestions={marketSuggestions}
          />
        )}

        {loading || refreshing ? (
          <div className="text-center py-12 text-muted-foreground">
            <RefreshCw className="size-6 mx-auto mb-3 animate-spin opacity-60" />
            <p className="text-[12px]">{refreshing ? '正在同步市场并加载记录…' : '加载中...'}</p>
          </div>
        ) : activeTab === 'stop_loss' ? (
          <section className="surface rounded-xl border border-border overflow-hidden">
            <div className="px-5 py-3.5 border-b border-border flex items-center justify-between gap-3">
              <h2 className="text-[13px] font-semibold">止损触发记录</h2>
              <div className="flex items-center gap-2">
                <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-widest">
                  {filteredStopLoss.length} 条
                </span>
                <button
                  type="button"
                  onClick={() => void handleClearStopLossHistory()}
                  disabled={clearingStopLoss || stopLossTasks.length === 0}
                  className="h-7 px-2.5 text-[11px] rounded-md border border-destructive/40 bg-destructive/5 text-destructive hover:bg-destructive/10 transition flex items-center gap-1 disabled:opacity-50"
                >
                  <Trash2 className={cn("size-3", clearingStopLoss && "animate-pulse")} />
                  {clearingStopLoss ? '清空中' : '清空止损记录'}
                </button>
              </div>
            </div>
            {filteredStopLoss.length === 0 ? (
              <div className="p-8 text-center text-muted-foreground text-[12px]">
                {stopLossTasks.length === 0 ? '暂无止损触发记录' : '没有符合筛选条件的记录'}
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
                  {filteredStopLoss.map((t) => (
                    <tr key={t.id} className="hover:bg-accent/30 transition-colors">
                      <td className="px-4 py-3 text-foreground/90 tabular-nums">
                        {formatHistoryTimestamp(t.updatedAt || t.createdAt)}
                      </td>
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
                    <div className="shrink-0 flex flex-col items-center gap-0.5 w-10">
                      <div className={cn(
                        "size-7 rounded-full flex items-center justify-center border",
                        "bg-muted border-border text-muted-foreground"
                      )}>
                        {isBuy ? (
                          <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><line x1="6" y1="2" x2="6" y2="10"/><line x1="2" y1="6" x2="10" y2="6"/></svg>
                        ) : (
                          <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><line x1="2" y1="6" x2="10" y2="6"/></svg>
                        )}
                      </div>
                      <span className="text-[10px] text-muted-foreground">{isBuy ? '买入' : '卖出'}</span>
                    </div>

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
    </div>
  );
}
