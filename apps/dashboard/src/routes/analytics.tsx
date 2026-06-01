import { createFileRoute } from "@tanstack/react-router";
import { useCallback, useEffect, useMemo, useState } from "react";
import { BarChart3, ChevronLeft, ChevronRight, CloudDownload, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { TopBar } from "@/components/TopBar";
import { PolymarketTitleLink } from "@/components/PolymarketTitleLink";
import { cn } from "@/lib/utils";
import {
  loadAnalyticsDateRange,
  saveAnalyticsDateRange,
} from "@/lib/analyticsDateRange";
import {
  getAnalyticsDaily,
  getAnalyticsTrades,
  syncAnalyticsFull,
  type AnalyticsDailyRow,
  type AnalyticsTradeRow,
} from "@/lib/api";

export const Route = createFileRoute("/analytics")({ component: AnalyticsPage });

const PAGE_SIZE = 20;

function fmtUsd(n: number | null | undefined): string {
  return (n ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function fmtPct(n: number | null | undefined): string {
  return `${(n ?? 0).toFixed(1)}%`;
}

function num(raw: unknown): number {
  const n = Number(raw);
  return Number.isFinite(n) ? n : 0;
}

/** Accept camelCase or legacy PascalCase daily rows from the API. */
function normalizeDailyRow(raw: Record<string, unknown>): AnalyticsDailyRow {
  return {
    date: String(raw.date ?? raw.Date ?? ""),
    totalInvestedUsd: num(raw.totalInvestedUsd ?? raw.TotalInvestedUSD),
    tradeCount: num(raw.tradeCount ?? raw.TradeCount),
    winCount: num(raw.winCount ?? raw.WinCount),
    winRate: num(raw.winRate ?? raw.WinRate),
    profitUsd: num(raw.profitUsd ?? raw.ProfitUSD),
    profitAmountRate: num(raw.profitAmountRate ?? raw.ProfitAmountRate),
  };
}

export default function AnalyticsPage() {
  const [dateRange, setDateRange] = useState(loadAnalyticsDateRange);
  const from = dateRange.from;
  const to = dateRange.to;
  const setFrom = (value: string) => setDateRange((prev) => ({ ...prev, from: value }));
  const setTo = (value: string) => setDateRange((prev) => ({ ...prev, to: value }));
  const [result, setResult] = useState<"all" | "win" | "loss">("all");
  const [page, setPage] = useState(1);
  const [daily, setDaily] = useState<AnalyticsDailyRow[]>([]);
  const [trades, setTrades] = useState<AnalyticsTradeRow[]>([]);
  const [tradeTotal, setTradeTotal] = useState(0);
  const [totals, setTotals] = useState({
    totalInvestedUsd: 0,
    totalProfitUsd: 0,
    returnPct: 0,
    tradeCount: 0,
  });
  const [loading, setLoading] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setPage(1);
  }, [from, to, result]);

  useEffect(() => {
    saveAnalyticsDateRange({ from, to });
  }, [from, to]);

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const offset = (page - 1) * PAGE_SIZE;
      const [d, t] = await Promise.all([
        getAnalyticsDaily(from, to),
        getAnalyticsTrades({ from, to, result, limit: PAGE_SIZE, offset }),
      ]);
      const rows = Array.isArray(d.rows) ? d.rows : [];
      setDaily(rows.map((r) => normalizeDailyRow(r as unknown as Record<string, unknown>)));
      setTrades(Array.isArray(t.rows) ? t.rows : []);
      setTradeTotal(typeof t.total === "number" ? t.total : (t.totals?.tradeCount ?? 0));
      setTotals(t.totals ?? { totalInvestedUsd: 0, totalProfitUsd: 0, returnPct: 0, tradeCount: 0 });
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载统计失败");
      toast.error("加载失败");
    } finally {
      setLoading(false);
    }
  }, [from, to, result, page]);

  useEffect(() => {
    void fetchData();
  }, [fetchData]);

  const handleFullSync = useCallback(async () => {
    setSyncing(true);
    setError(null);
    try {
      const res = await syncAnalyticsFull();
      const s = res.stats ?? { fetched: 0, created: 0, updated: 0, skipped: 0, resolutions: 0 };
      toast.success(
        `全量同步完成：拉取 ${s.fetched} 条，新建 ${s.created}，更新 ${s.updated}，决议 ${s.resolutions}`,
      );
      setPage(1);
      await fetchData();
    } catch (err) {
      const msg = err instanceof Error ? err.message : "全量同步失败";
      setError(msg);
      toast.error(msg);
    } finally {
      setSyncing(false);
    }
  }, [fetchData]);

  const periodSummary = useMemo(() => {
    let invested = 0;
    let profit = 0;
    let count = 0;
    let wins = 0;
    for (const r of daily) {
      invested += r.totalInvestedUsd ?? 0;
      profit += r.profitUsd ?? 0;
      count += r.tradeCount ?? 0;
      wins += r.winCount ?? 0;
    }
    return {
      totalInvestedUsd: invested,
      profitUsd: profit,
      tradeCount: count,
      winRate: count > 0 ? (wins / count) * 100 : 0,
      profitAmountRate: invested > 0 ? (profit / invested) * 100 : 0,
    };
  }, [daily]);

  const totalPages = Math.max(1, Math.ceil(tradeTotal / PAGE_SIZE));

  return (
    <div className="flex flex-col h-full min-h-0">
      <TopBar
        title="数据统计"
        subtitle="按 Polymarket 官方市场结算日（美东）归集已平仓持仓"
        icon={BarChart3}
        actions={
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => void handleFullSync()}
              disabled={syncing || loading}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-[12px] border border-brand/40 bg-brand/10 text-brand hover:bg-brand/15 disabled:opacity-50"
            >
              <CloudDownload className={cn("size-3.5", syncing && "animate-pulse")} />
              {syncing ? "同步中…" : "全量同步"}
            </button>
            <button
              type="button"
              onClick={() => void fetchData()}
              disabled={loading || syncing}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-[12px] border border-border hover:bg-accent/40 disabled:opacity-50"
            >
              <RefreshCw className={cn("size-3.5", loading && "animate-spin")} />
              刷新
            </button>
          </div>
        }
      />

      <div className="flex-1 min-h-0 overflow-auto p-4 space-y-4">
        <div className="flex flex-wrap items-end gap-3">
          <label className="text-[11px] text-muted-foreground">
            自
            <input
              type="date"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
              className="ml-1 h-8 px-2 rounded border border-border bg-background text-[12px]"
            />
          </label>
          <label className="text-[11px] text-muted-foreground">
            至
            <input
              type="date"
              value={to}
              onChange={(e) => setTo(e.target.value)}
              className="ml-1 h-8 px-2 rounded border border-border bg-background text-[12px]"
            />
          </label>
          <div className="flex gap-1">
            {(["all", "win", "loss"] as const).map((r) => (
              <button
                key={r}
                type="button"
                onClick={() => setResult(r)}
                className={cn(
                  "px-2.5 py-1 rounded text-[11px] border",
                  result === r
                    ? "bg-brand/15 border-brand/40 text-brand"
                    : "border-border text-muted-foreground hover:bg-accent/30",
                )}
              >
                {r === "all" ? "全部" : r === "win" ? "盈利" : "亏损"}
              </button>
            ))}
          </div>
          <span className="text-[10px] text-muted-foreground pb-1">
            区间 {from} ~ {to}
          </span>
        </div>

        {error && (
          <div className="p-3 rounded-md border border-destructive/30 bg-destructive/10 text-destructive text-[12px]">
            {error}
          </div>
        )}

        <section className="grid grid-cols-2 md:grid-cols-5 gap-3">
          <StatCard label="区间总投入" value={`$${fmtUsd(periodSummary.totalInvestedUsd)}`} />
          <StatCard label="区间笔数" value={String(periodSummary.tradeCount)} />
          <StatCard label="收益笔数率" value={fmtPct(periodSummary.winRate)} />
          <StatCard
            label="区间收益"
            value={`$${fmtUsd(periodSummary.profitUsd)}`}
            positive={periodSummary.profitUsd >= 0}
          />
          <StatCard label="收益金额率" value={fmtPct(periodSummary.profitAmountRate)} />
        </section>

        <section className="rounded-xl border border-border overflow-hidden">
          <div className="px-4 py-2.5 border-b border-border bg-muted/30 text-[13px] font-semibold">
            按结算日汇总
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-[12px]">
              <thead>
                <tr className="text-muted-foreground border-b border-border">
                  {["结算日", "总投入", "笔数", "收益笔数率", "收益金额", "收益金额率"].map((h) => (
                    <th key={h} className="px-3 py-2 text-left font-medium">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {daily.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="px-3 py-6 text-center text-muted-foreground">
                      {loading ? "加载中…" : "暂无已结算记录"}
                    </td>
                  </tr>
                ) : (
                  daily.map((r) => (
                    <tr key={r.date || `row-${r.tradeCount}-${r.totalInvestedUsd}`} className="hover:bg-accent/20">
                      <td className="px-3 py-2 tabular-nums">{r.date || "—"}</td>
                      <td className="px-3 py-2 tabular-nums">${fmtUsd(r.totalInvestedUsd)}</td>
                      <td className="px-3 py-2 tabular-nums">{r.tradeCount}</td>
                      <td className="px-3 py-2 tabular-nums">{fmtPct(r.winRate)}</td>
                      <td
                        className={cn(
                          "px-3 py-2 tabular-nums",
                          r.profitUsd >= 0 ? "text-success" : "text-destructive",
                        )}
                      >
                        ${fmtUsd(r.profitUsd)}
                      </td>
                      <td className="px-3 py-2 tabular-nums">{fmtPct(r.profitAmountRate)}</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </section>

        <section className="rounded-xl border border-border overflow-hidden">
          <div className="px-4 py-2.5 border-b border-border bg-muted/30 flex items-center justify-between gap-3">
            <span className="text-[13px] font-semibold">单笔明细</span>
            <span className="text-[11px] text-muted-foreground text-right">
              合计 {totals.tradeCount} 笔 · 投入 ${fmtUsd(totals.totalInvestedUsd)} · 收益 $
              {fmtUsd(totals.totalProfitUsd)} · 总收益率 {fmtPct(totals.returnPct)}
            </span>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-[12px]">
              <thead>
                <tr className="text-muted-foreground border-b border-border">
                  {["赛事", "投入", "收益", "收益率", "结算日", "状态"].map((h) => (
                    <th key={h} className="px-3 py-2 text-left font-medium">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {trades.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="px-3 py-6 text-center text-muted-foreground">
                      {loading ? "加载中…" : "暂无明细"}
                    </td>
                  </tr>
                ) : (
                  trades.map((r) => (
                    <tr key={r.positionId} className="hover:bg-accent/20">
                      <td className="px-3 py-2 max-w-[240px]">
                        <PolymarketTitleLink title={r.title} titleClassName="text-[12px]" />
                      </td>
                      <td className="px-3 py-2 tabular-nums">${fmtUsd(r.investedUsd)}</td>
                      <td
                        className={cn(
                          "px-3 py-2 tabular-nums font-medium",
                          r.profitUsd >= 0 ? "text-success" : "text-destructive",
                        )}
                      >
                        {r.profitUsd >= 0 ? "+" : ""}${fmtUsd(r.profitUsd)}
                      </td>
                      <td className="px-3 py-2 tabular-nums">{fmtPct(r.returnPct)}</td>
                      <td className="px-3 py-2 tabular-nums">{r.settlementDate || "—"}</td>
                      <td className="px-3 py-2 text-[11px] text-muted-foreground">
                        {r.pendingOfficial ? "待官方结算" : "已结算"}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
          {tradeTotal > PAGE_SIZE && (
            <div className="px-4 py-2.5 border-t border-border flex items-center justify-between text-[11px]">
              <span className="text-muted-foreground">
                第 {page} / {totalPages} 页 · 共 {tradeTotal} 笔
              </span>
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  disabled={page <= 1 || loading}
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                  className="inline-flex items-center gap-1 px-2 py-1 rounded border border-border hover:bg-accent/30 disabled:opacity-40"
                >
                  <ChevronLeft className="size-3.5" />
                  上一页
                </button>
                <button
                  type="button"
                  disabled={page >= totalPages || loading}
                  onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                  className="inline-flex items-center gap-1 px-2 py-1 rounded border border-border hover:bg-accent/30 disabled:opacity-40"
                >
                  下一页
                  <ChevronRight className="size-3.5" />
                </button>
              </div>
            </div>
          )}
        </section>
      </div>
    </div>
  );
}

function StatCard({
  label,
  value,
  positive,
}: {
  label: string;
  value: string;
  positive?: boolean;
}) {
  return (
    <div className="rounded-xl border border-border bg-surface px-3 py-3">
      <p className="text-[10px] text-muted-foreground uppercase tracking-wide">{label}</p>
      <p
        className={cn(
          "mt-1 text-[18px] font-semibold tabular-nums",
          positive === true && "text-success",
          positive === false && "text-destructive",
        )}
      >
        {value}
      </p>
    </div>
  );
}
