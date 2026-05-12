import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { TopBar } from "@/components/TopBar";
import { RefreshCw, AlertTriangle } from "lucide-react";
import { useRiskControlCache } from "@/hooks/useRiskControlCache";
import { postRiskClosePosition, postRiskCloseAll, patchRiskPosition, postRiskOfficialRefresh } from "@/lib/api";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

export const Route = createFileRoute("/risk")({ component: RiskPage });

function fmtUsd(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return '—';
  const sign = n < 0 ? '−' : '';
  const v = Math.abs(n);
  return `${sign}$${v.toFixed(2)}`;
}

function fmtCents(c: number | null | undefined): string {
  if (c == null || !Number.isFinite(c)) return '—';
  return `${c.toFixed(1)}¢`;
}

function relAgeShort(iso: string | null): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t)) return '—';
  const sec = Math.floor((Date.now() - t) / 1000);
  if (sec < 0) return '刚刚';
  if (sec < 60) return `${sec}s前`;
  const m = Math.floor(sec / 60);
  if (m < 60) return `${m}m前`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h前`;
  return `${Math.floor(h / 24)}d前`;
}

function RiskPage() {
  const { positions, meta, tasks, loading, error, refresh, wsConnected, lastRefresh } = useRiskControlCache();
  const [closingId, setClosingId] = useState<string | null>(null);
  const [closingAll, setClosingAll] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, { sl: string; hw: string }>>({});
  const [patchingKey, setPatchingKey] = useState<string | null>(null);
  const [officialSyncing, setOfficialSyncing] = useState(false);

  const handleCloseOne = async (id: string) => {
    setClosingId(id);
    try {
      await postRiskClosePosition(id);
      toast.success('已平仓', { description: '平仓任务已加入队列' });
      refresh();
    } catch (err) {
      toast.error('失败', { description: err instanceof Error ? err.message : '未知错误' });
    } finally {
      setClosingId(null);
    }
  };

  const handleCloseAll = async () => {
    setClosingAll(true);
    try {
      await postRiskCloseAll();
      toast.success('已平仓', { description: '已为所有持仓创建平仓任务' });
      refresh();
    } catch (err) {
      toast.error('失败', { description: err instanceof Error ? err.message : '未知错误' });
    } finally {
      setClosingAll(false);
    }
  };

  const applyStopLossPct = async (id: string) => {
    const d = drafts[id];
    if (!d) return;
    const n = parseFloat(d.sl);
    if (!Number.isFinite(n) || n < 1 || n > 99) {
      toast.error('无效', { description: '止损% 须在 1–99 之间' });
      return;
    }
    setPatchingKey(`${id}:sl`);
    try {
      await patchRiskPosition(id, { stopLossPct: n });
      toast.success('已更新', { description: `止损% = ${n}` });
      refresh();
    } catch (err) {
      toast.error('失败', { description: err instanceof Error ? err.message : '未知错误' });
    } finally {
      setPatchingKey(null);
    }
  };

  const applyHighWater = async (id: string) => {
    const d = drafts[id];
    if (!d) return;
    const n = parseFloat(d.hw);
    if (!Number.isFinite(n) || n <= 0 || n > 100) {
      toast.error('无效', { description: '最高水位须在 (0, 100]（¢）' });
      return;
    }
    setPatchingKey(`${id}:hw`);
    try {
      await patchRiskPosition(id, { highWaterCents: n });
      toast.success('已更新', { description: `最高水位 = ${n}¢` });
      refresh();
    } catch (err) {
      toast.error('失败', { description: err instanceof Error ? err.message : '未知错误' });
    } finally {
      setPatchingKey(null);
    }
  };

  const handleOfficialRefresh = async () => {
    setOfficialSyncing(true);
    try {
      const r = await postRiskOfficialRefresh();
      if (r.syncError) {
        toast.warning('缓存已更新', { description: `官方持仓同步未完全成功：${r.syncError}` });
      } else {
        toast.success('已刷新', { description: '已从官方 CLOB 同步持仓与链上余额' });
      }
      refresh();
    } catch (err) {
      toast.error('刷新失败', { description: err instanceof Error ? err.message : '未知错误' });
    } finally {
      setOfficialSyncing(false);
    }
  };

  return (
    <>
      <TopBar
        title="风控"
        subtitle={
          <>
            <span className="flex items-center gap-1.5">
              <span className={`size-1.5 rounded-full ${wsConnected ? 'bg-success animate-breathe' : 'bg-warning'}`} />
              WS {wsConnected ? '已连接' : '未连接'}
            </span>
            <span className="text-border">·</span>
            {lastRefresh && (
              <>
                <span className="text-muted-foreground">更新 {relAgeShort(lastRefresh.toISOString())}</span>
                <span className="text-border">·</span>
              </>
            )}
            <span>minOpenRiskShares ≥ {meta?.minOpenRiskShares ?? 1}</span>
            {meta?.userWsLastIssue && (
              <span className="text-warning text-[10px] ml-2" title={meta.userWsLastIssue}>
                ({meta.userWsLastIssue.slice(0, 30)}...)
              </span>
            )}
          </>
        }
        actions={
          <>
            <button 
              onClick={handleOfficialRefresh}
              disabled={officialSyncing}
              className="h-8 px-3 text-[12px] rounded-md border border-border bg-surface hover:bg-accent transition flex items-center gap-1.5 disabled:opacity-50"
            >
              <RefreshCw className={cn("size-3.5", officialSyncing && "animate-spin")} /> 
              {officialSyncing ? '同步中...' : '刷新'}
            </button>
            <button 
              onClick={handleCloseAll}
              disabled={closingAll || positions.length === 0}
              className="h-8 px-3 text-[12px] rounded-md bg-destructive text-destructive-foreground hover:opacity-90 transition flex items-center gap-1.5 font-medium disabled:opacity-50"
            >
              <AlertTriangle className="size-3.5" /> 
              {closingAll ? '处理中...' : '一键全部平仓'}
            </button>
          </>
        }
      />

      <div className="p-6 space-y-6 animate-slide-up">
        {error && (
          <div className="p-4 rounded-md border border-destructive/30 bg-destructive/10 text-destructive text-[12px]">
            {error}
          </div>
        )}

        {loading ? (
          <div className="text-center py-12 text-muted-foreground">加载中...</div>
        ) : positions.length === 0 ? (
          <div className="surface rounded-xl border border-border p-8 text-center">
            <div className="text-[14px] text-muted-foreground mb-2">暂无持仓</div>
            <div className="text-[12px] text-muted-foreground max-w-xl mx-auto">
              本系统成交与官网/CLOB 成交会通过用户 WebSocket（及 REST 兜底）合并到此处；移动止损按「设置 → 价格区间」的止损%，相对持仓期间的最高水位计算触发价。
            </div>
          </div>
        ) : (
          <>
            {/* positions table */}
            <section className="surface rounded-xl border border-border overflow-hidden">
              <div className="px-5 py-3.5 border-b border-border flex items-center justify-between">
                <h2 className="text-[13px] font-semibold">持仓监控</h2>
                <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-widest">{positions.length} 个市场</span>
              </div>
              <table className="w-full text-[12px]">
                <thead className="text-[10px] uppercase tracking-widest text-muted-foreground bg-background/40">
                  <tr>
                    {["市场", "均价 → 当前", "份额", "成本", "可赢利", "市值", "最高水位", "止损 %", "移动止损价", ""].map((h) => (
                      <th key={h} className="px-4 py-2.5 font-medium text-left">{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {positions.map((p) => {
                    const pnlPct = p.pnlUsd != null && p.costUsd > 0 ? (p.pnlUsd / p.costUsd) * 100 : null;
                    const display = (p.displayTitle?.trim() || p.title).trim();
                    
                    if (!drafts[p.id]) {
                      setDrafts(prev => ({ ...prev, [p.id]: { sl: String(p.stopLossPct), hw: String(p.highWaterCents) } }));
                    }
                    
                    return (
                      <tr key={p.id} className="hover:bg-accent/30 transition-colors">
                        <td className="px-4 py-4">
                          <div className="flex items-center gap-2.5">
                            <div className="size-7 rounded-md bg-brand/10 border border-brand/20 flex items-center justify-center">
                              <div className="size-2 rounded-sm bg-brand" />
                            </div>
                            <div className="flex flex-col">
                              <span className="font-mono text-[11px] text-brand">{display}</span>
                              <span className="mt-0.5 text-[10px] num text-muted-foreground bg-accent rounded px-1.5 py-0.5 w-fit">{fmtCents(p.avgEntryCents)}</span>
                            </div>
                          </div>
                        </td>
                        <td className="px-4 py-4 num">{fmtCents(p.avgEntryCents)} <span className="text-muted-foreground">→ {fmtCents(p.currentCents)}</span></td>
                        <td className="px-4 py-4 num">{p.sizeShares.toFixed(2)}</td>
                        <td className="px-4 py-4 num">{fmtUsd(p.costUsd)}</td>
                        <td className="px-4 py-4 num text-success">{fmtUsd(p.potentialProfitUsd)}</td>
                        <td className="px-4 py-4">
                          <div className="num text-muted-foreground">{fmtUsd(p.valueUsd)}</div>
                          {p.pnlUsd != null && (
                            <div className={cn("text-[9px] mt-0.5", p.pnlUsd >= 0 ? "text-success" : "text-danger")}>
                              {fmtUsd(p.pnlUsd)}
                              {pnlPct != null && Number.isFinite(pnlPct) && (
                                ` (${pnlPct >= 0 ? '' : '−'}${Math.abs(pnlPct).toFixed(1)}%)`
                              )}
                            </div>
                          )}
                        </td>
                        <td className="px-4 py-4">
                          <div className="flex flex-col gap-1">
                            <input
                              type="number"
                              step="0.1"
                              min={0.1}
                              max={100}
                              disabled={p.status !== 'open'}
                              value={drafts[p.id]?.hw ?? ''}
                              onChange={(e) => setDrafts(prev => ({
                                ...prev,
                                [p.id]: { ...prev[p.id], hw: e.target.value }
                              }))}
                              className="w-20 h-7 px-2 text-[11.5px] num rounded border border-border bg-background focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/30 transition"
                            />
                            <button
                              onClick={() => applyHighWater(p.id)}
                              disabled={p.status !== 'open' || patchingKey === `${p.id}:hw`}
                              className="h-6 text-[10px] rounded border border-border hover:bg-accent transition disabled:opacity-50"
                            >
                              {patchingKey === `${p.id}:hw` ? '...' : '应用'}
                            </button>
                          </div>
                        </td>
                        <td className="px-4 py-4">
                          <div className="flex flex-col gap-1">
                            <input
                              type="number"
                              step={1}
                              min={1}
                              max={99}
                              disabled={p.status !== 'open'}
                              value={drafts[p.id]?.sl ?? ''}
                              onChange={(e) => setDrafts(prev => ({
                                ...prev,
                                [p.id]: { ...prev[p.id], sl: e.target.value }
                              }))}
                              className="w-16 h-7 px-2 text-[11.5px] num rounded border border-border bg-background focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/30 transition"
                            />
                            <button
                              onClick={() => applyStopLossPct(p.id)}
                              disabled={p.status !== 'open' || patchingKey === `${p.id}:sl`}
                              className="h-6 text-[10px] rounded border border-border hover:bg-accent transition disabled:opacity-50"
                            >
                              {patchingKey === `${p.id}:sl` ? '...' : '应用'}
                            </button>
                          </div>
                        </td>
                        <td className="px-4 py-4 num text-warning">{fmtCents(p.trailingStopCents)}</td>
                        <td className="px-4 py-4">
                          <button 
                            onClick={() => handleCloseOne(p.id)}
                            disabled={p.status !== 'open' || closingId === p.id}
                            className="px-3 py-1.5 rounded-md bg-brand text-brand-foreground text-[11.5px] font-medium hover:opacity-90 transition active:scale-95 disabled:opacity-50"
                          >
                            {closingId === p.id ? '...' : '卖出'}
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </section>

            {/* task queue logs */}
            <section className="space-y-3">
              <div className="flex items-baseline justify-between">
                <h2 className="text-[13px] font-semibold">任务队列</h2>
                <p className="text-[11px] text-muted-foreground max-w-2xl text-right">
                  止损触发与手动平仓均进入此队列；状态为 <code className="text-warning">FAILED</code> 时自动重试。
                </p>
              </div>
              {tasks.length === 0 ? (
                <div className="surface-elevated rounded-xl border border-border p-4 text-[11.5px] text-muted-foreground">
                  暂无任务
                </div>
              ) : (
                <div className="surface-elevated rounded-xl border border-border p-4 font-mono text-[11.5px] space-y-2 max-h-[400px] overflow-y-auto scrollbar-thin">
                  {tasks.map((t) => (
                    <div key={t.id} className="flex items-start gap-3 py-1 hover:bg-accent/30 px-2 -mx-2 rounded transition-colors">
                      <span className="text-muted-foreground shrink-0">{t.updatedAt.slice(5, 16)}</span>
                      <span className="text-brand shrink-0">{t.type}</span>
                      <span className={cn(
                        "shrink-0 uppercase text-[9px]",
                        t.status === 'succeeded' && "text-success",
                        t.status === 'failed' && "text-danger",
                        t.status === 'pending' && "text-warning",
                        t.status === 'running' && "text-blue-400",
                      )}>
                        {t.status}
                      </span>
                      {t.positionId && (
                        <span className="text-muted-foreground">pos {t.positionId.slice(0, 8)}…</span>
                      )}
                      <span className="text-muted-foreground">#{t.attempts}</span>
                      {t.lastError && <span className="text-danger/80">{t.lastError}</span>}
                    </div>
                  ))}
                </div>
              )}
            </section>
          </>
        )}
      </div>
    </>
  );
}