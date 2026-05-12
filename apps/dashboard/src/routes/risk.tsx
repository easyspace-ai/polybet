import { createFileRoute } from "@tanstack/react-router";
import { PageHeader, StatusDot } from "@/components/app-shell";
import { RefreshCw } from "lucide-react";
import { useEffect, useState } from 'react';
import {
  patchRiskPosition,
  postRiskCloseAll,
  postRiskClosePosition,
  postRiskOfficialRefresh,
} from '@/lib/api';
import { useRiskControlCache } from '@/hooks/useRiskControlCache';
import { toast } from '@/lib/toast';
import { cn } from '@/lib/utils';

export const Route = createFileRoute("/risk")({
  component: RiskPage,
});

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

function sourceLabel(source: string | undefined): string {
  if (source === 'polymarket_clob') return '官网/CLOB';
  if (source === 'bot') return '本系统';
  return source ?? '—';
}

function sportEmoji(sport: string | undefined): string {
  const s = (sport ?? '').toLowerCase();
  if (!s) return '📊';
  if (s.includes('nba') || s.includes('basket') || s.includes('wnba') || s.includes('ncaa')) return '🏀';
  if (s.includes('nfl') || s.includes('football')) return '🏈';
  if (s.includes('nhl') || s.includes('hockey')) return '🏒';
  if (s.includes('mlb') || s.includes('baseball')) return '⚾';
  if (s.includes('soccer') || s.includes('epl') || s.includes('mls') || s.includes('fifa')) return '⚽';
  if (s.includes('mma') || s.includes('ufc')) return '🥊';
  if (s.includes('tennis')) return '🎾';
  if (s.includes('golf')) return '⛳';
  if (s.includes('crypto') || s.includes('btc') || s.includes('eth')) return '◆';
  return '📊';
}

function RiskPage() {
  const { positions, meta, tasks, loading, refresh } = useRiskControlCache();
  const [error, setError] = useState<string | null>(null);
  const [closingId, setClosingId] = useState<string | null>(null);
  const [closingAll, setClosingAll] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, { sl: string; hw: string }>>({});
  const [patchingKey, setPatchingKey] = useState<string | null>(null);
  const [officialSyncing, setOfficialSyncing] = useState(false);

  useEffect(() => {
    const next: Record<string, { sl: string; hw: string }> = {};
    for (const p of positions) {
      next[p.id] = { sl: String(p.stopLossPct), hw: String(p.highWaterCents) };
    }
    setDrafts(next);
  }, [positions]);

  async function onCloseOne(id: string) {
    setClosingId(id);
    try {
      await postRiskClosePosition(id);
      toast({ title: '已排队', description: '平仓任务已加入队列（失败会自动重试）', variant: 'success' });
      refresh();
    } catch (e) {
      toast({
        title: '失败',
        description: e instanceof Error ? e.message : '未知错误',
        variant: 'destructive',
      });
    } finally {
      setClosingId(null);
    }
  }

  async function applyStopLossPct(id: string) {
    const d = drafts[id];
    if (!d) return;
    const n = parseFloat(d.sl);
    if (!Number.isFinite(n) || n < 1 || n > 99) {
      toast({ title: '无效', description: '止损% 须在 1–99 之间', variant: 'destructive' });
      return;
    }
    setPatchingKey(`${id}:sl`);
    try {
      await patchRiskPosition(id, { stopLossPct: n });
      toast({ title: '已更新', description: `止损% = ${n}`, variant: 'success' });
      refresh();
    } catch (e) {
      toast({
        title: '失败',
        description: e instanceof Error ? e.message : '未知错误',
        variant: 'destructive',
      });
    } finally {
      setPatchingKey(null);
    }
  }

  async function applyHighWater(id: string) {
    const d = drafts[id];
    if (!d) return;
    const n = parseFloat(d.hw);
    if (!Number.isFinite(n) || n <= 0 || n > 100) {
      toast({ title: '无效', description: '最高水位须在 (0, 100]（¢）', variant: 'destructive' });
      return;
    }
    setPatchingKey(`${id}:hw`);
    try {
      await patchRiskPosition(id, { highWaterCents: n });
      toast({ title: '已更新', description: `最高水位 = ${n}¢`, variant: 'success' });
      refresh();
    } catch (e) {
      toast({
        title: '失败',
        description: e instanceof Error ? e.message : '未知错误',
        variant: 'destructive',
      });
    } finally {
      setPatchingKey(null);
    }
  }

  async function onCloseAll() {
    setClosingAll(true);
    try {
      await postRiskCloseAll();
      toast({ title: '已排队', description: '已为所有持仓创建平仓任务', variant: 'success' });
      refresh();
    } catch (e) {
      toast({
        title: '失败',
        description: e instanceof Error ? e.message : '未知错误',
        variant: 'destructive',
      });
    } finally {
      setClosingAll(false);
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="shrink-0 border-b border-border bg-sidebar/80 backdrop-blur-md px-4 py-2.5 z-10">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <span className="relative flex h-[6px] w-[6px]">
              <span className="absolute inline-flex h-full w-full rounded-full bg-warning opacity-60 animate-flicker" />
              <span className="relative inline-flex rounded-full h-[6px] w-[6px] bg-warning" style={{ boxShadow: '0 0 6px hsl(38 94% 52% / 0.4)' }} />
            </span>
            <span className="font-mono text-[10px] font-semibold tracking-[0.15em] text-muted-foreground uppercase">
              风控
            </span>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={officialSyncing}
              onClick={async () => {
                setOfficialSyncing(true);
                try {
                  const r = await postRiskOfficialRefresh();
                  if (r.syncError) {
                    toast({
                      title: '缓存已更新',
                      description: `官方持仓同步未完全成功：${r.syncError}`,
                      variant: 'destructive',
                    });
                  } else {
                    toast({
                      title: '已刷新',
                      description: '已从官方 CLOB 同步持仓与链上余额，并更新缓存',
                      variant: 'success',
                    });
                  }
                  refresh();
                } catch (e) {
                  toast({
                    title: '刷新失败',
                    description: e instanceof Error ? e.message : '未知错误',
                    variant: 'destructive',
                  });
                } finally {
                  setOfficialSyncing(false);
                }
              }}
              className={cn(
                'flex items-center gap-1.5 px-2.5 py-1 rounded-md font-mono text-[10px] font-medium',
                'text-muted-foreground hover:text-foreground hover:bg-surface border border-transparent hover:border-border',
                'transition-all duration-150',
                officialSyncing && 'opacity-50 cursor-wait',
              )}
              title="从官方 CLOB 拉取持仓与余额，并更新服务端缓存"
            >
              <RefreshCw size={10} className={cn(officialSyncing && 'animate-spin')} />
              {officialSyncing ? '同步中…' : '刷新'}
            </button>
            <button
              type="button"
              disabled={closingAll || positions.length === 0}
              onClick={() => void onCloseAll()}
              className={cn(
                'px-3 py-1 rounded-md text-[10px] font-semibold tracking-wide transition-all duration-150',
                closingAll || positions.length === 0
                  ? 'bg-surface text-muted-foreground cursor-not-allowed border border-border'
                  : 'bg-down/90 text-white hover:bg-down',
              )}
              style={closingAll || positions.length === 0 ? {} : { boxShadow: '0 0 8px hsl(0 78% 58% / 0.2)' }}
            >
              {closingAll ? '处理中…' : '一键全部平仓'}
            </button>
          </div>
        </div>
        {meta && (
          <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 font-mono text-[9px] text-muted-foreground">
            <span>
              用户 WS：
              {meta.userWsConnected ? (
                <span className="text-up">已连接</span>
              ) : meta.userWsConnecting ? (
                <span className="text-warning">连接中…</span>
              ) : (
                <span className="text-down">未连接</span>
              )}
            </span>
            <span
              className={
                meta.outboundProxyConfigured
                  ? 'text-foreground'
                  : 'text-warning'
              }
              title="与 REST 相同：HTTP_PLATFORM_PROXY_URL 或 设置 → 代理"
            >
              出站 WSS：
              {meta.outboundProxyConfigured ? (
                <span className="text-up">已配置（CONNECT 隧道）</span>
              ) : (
                <span>未配置（直连）</span>
              )}
            </span>
            <span title={meta.userWsLastMessageAt ?? ''}>
              最近推送 {relAgeShort(meta.userWsLastMessageAt)}
            </span>
            <span title={meta.restTradesSyncLastAt ?? ''}>
              REST 成交同步 {relAgeShort(meta.restTradesSyncLastAt)}
            </span>
            <span className="text-muted-foreground">
              风控最小份额 ≥ {meta.minOpenRiskShares ?? 1}（设置 → 通用 <span className="text-foreground">minOpenRiskShares</span>）
            </span>
            {meta.userWsLastIssue && (
              <span
                className="text-down w-full break-all"
                title={meta.userWsLastIssue}
              >
                WS 提示：{meta.userWsLastIssue}
                {/failed to connect|1006/i.test(meta.userWsLastIssue) &&
                  !meta.outboundProxyConfigured && (
                    <span className="block mt-0.5 text-muted-foreground font-normal">
                      当前为直连 Polymarket；若网络受限，请在环境变量
                      <code className="text-foreground"> HTTP_PLATFORM_PROXY_URL </code>
                      或 设置 → 代理 中填写支持 CONNECT 到
                      <code className="text-foreground"> ws-subscriptions-clob.polymarket.com:443 </code>
                      的 HTTP(S) 代理。
                    </span>
                  )}
                {/failed to connect|1006/i.test(meta.userWsLastIssue) &&
                  meta.outboundProxyConfigured && (
                    <span className="block mt-0.5 text-muted-foreground font-normal">
                      已走代理仍连不上：确认代理允许 CONNECT 到上述主机 443、超时足够；REST 仍会定期同步成交。
                    </span>
                  )}
              </span>
            )}
          </div>
        )}
      </div>

      <div className="flex-1 min-h-0 overflow-auto p-4">
        {error && (
          <div className="mb-3 rounded-sm border border-down/30 bg-down/10 px-3 py-2 font-mono text-[11px] text-down">
            {error}
          </div>
        )}
        {loading && (
          <div className="font-mono text-[11px] text-muted-foreground">加载中…</div>
        )}

        {!loading && positions.length === 0 && !error && (
          <div className="font-mono text-[11px] text-muted-foreground max-w-xl space-y-2">
            <p>
              暂无持仓。本系统成交与官网/CLOB 成交会通过用户 WebSocket（及 REST 兜底）合并到此处；移动止损按「设置 → 价格区间」的
              <span className="text-foreground">止损%</span>，相对持仓期间的
              <span className="text-foreground">最高水位</span>（YES 最优买价曾到过的最高价）计算触发价：
              <span className="text-muted-foreground"> 触发价 = 最高水位 × (1 − 止损% / 100)</span>
              （例如最高 80¢、止损 20% → 跌至 64¢ 以下触发）。当前价由市场频道订单簿推送驱动；平仓使用 FOK 卖单，相对最优买价向下按 tick 放宽（见设置 → 通用 → polymarketFokSellExtraTicks）；平仓失败会短间隔连重试。
            </p>
            <p className="text-muted-foreground text-[10px]">
              有持仓时，表格展示：均价、当前最优买价、份额、成本、盈亏、
              <span className="text-foreground">最高水位</span>、止损比例、
              <span className="text-foreground">移动止损触发价</span>。
            </p>
          </div>
        )}

        {positions.length > 0 && (
          <div className="overflow-x-auto rounded-sm border border-border">
            <table className="w-full min-w-[1040px] border-collapse font-mono text-[10px]">
              <thead>
                <tr className="border-b border-border bg-surface text-muted-foreground text-left">
                  <th className="px-2 py-2 font-semibold min-w-[260px]">市场</th>
                  <th className="px-2 py-2 font-semibold">均价 → 当前</th>
                  <th className="px-2 py-2 font-semibold">份额</th>
                  <th className="px-2 py-2 font-semibold">成本</th>
                  <th className="px-2 py-2 font-semibold">可赢利</th>
                  <th className="px-2 py-2 font-semibold">市值</th>
                  <th className="px-2 py-2 font-semibold">最高水位</th>
                  <th className="px-2 py-2 font-semibold">止损%</th>
                  <th className="px-2 py-2 font-semibold">移动止损价</th>
                  <th className="px-2 py-2 font-semibold w-[72px]" />
                </tr>
              </thead>
              <tbody>
                {positions.map((row) => {
                  const pnlPct =
                    row.pnlUsd != null && row.costUsd > 0
                      ? (row.pnlUsd / row.costUsd) * 100
                      : null;
                  const display = (row.displayTitle?.trim() || row.title).trim();
                  const polyHref =
                    (row.officialUrl?.trim() || row.officialSearchUrl?.trim() || '') || null;
                  const artImg = row.imageUrl?.trim() || '';
                  const artIcon = row.iconUrl?.trim() || '';
                  const artUrls =
                    artImg && artIcon && artImg !== artIcon
                      ? [artImg, artIcon]
                      : [artImg || artIcon].filter(Boolean);
                  return (
                    <tr key={row.id} className="border-b border-border/80 hover:bg-surface/40">
                      <td className="px-2 py-2 align-top">
                        <div className="flex gap-2.5 min-w-0 max-w-[360px]">
                          <div
                            className={cn(
                              'relative flex h-10 shrink-0 items-center justify-center overflow-hidden rounded-md border border-border bg-surface text-[17px] leading-none',
                              artUrls.length > 1 ? 'w-[42px] gap-px' : 'w-10',
                            )}
                            title={row.sport ? `运动: ${row.sport}` : '市场'}
                          >
                            {artUrls.length === 0 && (
                              <span
                                className="absolute inset-0 flex items-center justify-center"
                                aria-hidden
                              >
                                {sportEmoji(row.sport)}
                              </span>
                            )}
                            {artUrls.length > 0 && (
                              <div className="relative z-10 flex h-full w-full min-w-0">
                                {artUrls.map((src) => (
                                  <img
                                    key={src}
                                    src={src}
                                    alt=""
                                    className="h-full min-w-0 flex-1 object-cover"
                                    loading="lazy"
                                    referrerPolicy="no-referrer"
                                    onError={(e) => {
                                      e.currentTarget.remove();
                                    }}
                                  />
                                ))}
                              </div>
                            )}
                          </div>
                          <div className="min-w-0 flex-1">
                            <div className="min-w-0">
                              {polyHref ? (
                                <a
                                  href={polyHref}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-[11px] font-semibold leading-snug text-primary underline decoration-border/80 underline-offset-2 hover:text-sky-400 hover:decoration-sky-400/60"
                                  title="在 Polymarket 打开"
                                  onClick={(e) => e.stopPropagation()}
                                >
                                  {display}
                                </a>
                              ) : (
                                <div className="text-[11px] font-semibold leading-snug text-foreground">{display}</div>
                              )}
                            </div>
                            <div className="mt-1 inline-flex max-w-full flex-wrap items-baseline gap-x-1 gap-y-0 rounded-md border border-down/25 bg-down/10 px-1.5 py-0.5 text-[9px] font-medium text-down">
                              <span>{row.sideLabel}</span>
                              <span className="text-muted-foreground">{fmtCents(row.avgEntryCents)}</span>
                              <span>
                                {row.sizeShares.toFixed(1)} 份额
                              </span>
                            </div>
                            {!(polyHref && row.source === 'polymarket_clob') && (
                              <div className="mt-1 text-[9px] text-muted-foreground">{sourceLabel(row.source)}</div>
                            )}
                            {row.status === 'closing' && (
                              <span className="mt-1 inline-block text-[9px] text-warning">平仓中…</span>
                            )}
                          </div>
                        </div>
                      </td>
                      <td className="px-2 py-2 text-foreground whitespace-nowrap">
                        {fmtCents(row.avgEntryCents)}
                        <span className="text-muted-foreground mx-0.5">→</span>
                        {fmtCents(row.currentCents)}
                      </td>
                      <td className="px-2 py-2 text-foreground">{row.sizeShares.toFixed(2)}</td>
                      <td className="px-2 py-2 text-foreground">{fmtUsd(row.costUsd)}</td>
                      <td className="px-2 py-2 text-up">{fmtUsd(row.potentialProfitUsd)}</td>
                      <td className="px-2 py-2">
                        <div className="text-foreground">{fmtUsd(row.valueUsd)}</div>
                        {row.pnlUsd != null && (
                          <div
                            className={cn(
                              'text-[9px] mt-0.5',
                              row.pnlUsd >= 0 ? 'text-up' : 'text-down',
                            )}
                          >
                            {fmtUsd(row.pnlUsd)}
                            {pnlPct != null && Number.isFinite(pnlPct)
                              ? ` (${pnlPct >= 0 ? '' : '−'}${Math.abs(pnlPct).toFixed(1)}%)`
                              : ''}
                          </div>
                        )}
                      </td>
                      <td className="px-2 py-2 align-top">
                        <div className="flex flex-col gap-1 max-w-[100px]">
                          <input
                            type="number"
                            step="0.1"
                            min={0.1}
                            max={100}
                            disabled={row.status !== 'open'}
                            value={drafts[row.id]?.hw ?? ''}
                            onChange={(e) =>
                              setDrafts((prev) => {
                                const cur = prev[row.id] ?? {
                                  sl: String(row.stopLossPct),
                                  hw: String(row.highWaterCents),
                                };
                                return { ...prev, [row.id]: { ...cur, hw: e.target.value } };
                              })
                            }
                            className="w-full rounded-sm border border-border bg-background px-1 py-0.5 text-[10px] text-primary disabled:opacity-40"
                          />
                          <button
                            type="button"
                            disabled={
                              row.status !== 'open' || patchingKey === `${row.id}:hw` || patchingKey === `${row.id}:sl`
                            }
                            onClick={() => void applyHighWater(row.id)}
                            className="rounded-sm bg-sidebar px-1 py-0.5 text-[9px] font-bold text-foreground hover:bg-border disabled:cursor-not-allowed disabled:opacity-40"
                          >
                            {patchingKey === `${row.id}:hw` ? '…' : '应用'}
                          </button>
                        </div>
                      </td>
                      <td className="px-2 py-2 align-top">
                        <div className="flex flex-col gap-1 max-w-[88px]">
                          <div className="flex items-center gap-0.5">
                            <input
                              type="number"
                              step={1}
                              min={1}
                              max={99}
                              disabled={row.status !== 'open'}
                              value={drafts[row.id]?.sl ?? ''}
                              onChange={(e) =>
                                setDrafts((prev) => {
                                  const cur = prev[row.id] ?? {
                                    sl: String(row.stopLossPct),
                                    hw: String(row.highWaterCents),
                                  };
                                  return { ...prev, [row.id]: { ...cur, sl: e.target.value } };
                                })
                              }
                              className="min-w-0 flex-1 rounded-sm border border-border bg-background px-1 py-0.5 text-[10px] text-foreground disabled:opacity-40"
                            />
                            <span className="shrink-0 text-[9px] text-muted-foreground">%</span>
                          </div>
                          <button
                            type="button"
                            disabled={
                              row.status !== 'open' || patchingKey === `${row.id}:sl` || patchingKey === `${row.id}:hw`
                            }
                            onClick={() => void applyStopLossPct(row.id)}
                            className="rounded-sm bg-sidebar px-1 py-0.5 text-[9px] font-bold text-foreground hover:bg-border disabled:cursor-not-allowed disabled:opacity-40"
                          >
                            {patchingKey === `${row.id}:sl` ? '…' : '应用'}
                          </button>
                        </div>
                      </td>
                      <td className="px-2 py-2 text-down">{fmtCents(row.trailingStopCents)}</td>
                      <td className="px-2 py-2">
                        <button
                          type="button"
                          disabled={row.status !== 'open' || closingId === row.id}
                          onClick={() => void onCloseOne(row.id)}
                          className={cn(
                            'w-full rounded-sm py-1 text-[10px] font-bold',
                            row.status !== 'open' || closingId === row.id
                              ? 'bg-sidebar text-muted-foreground cursor-not-allowed'
                              : 'bg-sky-600 text-white hover:bg-sky-500',
                          )}
                        >
                          {closingId === row.id ? '…' : '卖出'}
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        <div className="mt-6">
          <div className="font-mono text-[10px] font-semibold tracking-[0.2em] text-muted-foreground mb-2">
            任务队列
          </div>
          <p className="font-mono text-[9px] text-muted-foreground mb-2 max-w-2xl">
            止损触发与手动平仓均进入此队列；状态为 failed 时自动重试——平仓任务前几轮为短间隔（应对深度/滑点/FOK
            未成交），其后退避拉长。卖单价格 = max(tick, 最优买价 − polymarketFokSellExtraTicks×tick)，数值在设置 → 通用。
          </p>
          {tasks.length === 0 ? (
            <div className="font-mono text-[10px] text-muted-foreground">暂无任务</div>
          ) : (
            <ul className="space-y-1.5 max-h-[280px] overflow-y-auto rounded-sm border border-border bg-surface p-2">
              {tasks.map((t) => (
                <li
                  key={t.id}
                  className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 font-mono text-[10px] text-foreground border-b border-border/50 pb-1.5 last:border-0"
                >
                  <span className="text-muted-foreground shrink-0">{t.updatedAt.slice(5, 16)}</span>
                  <span className="font-semibold text-primary">{t.type}</span>
                  <span
                    className={cn(
                      'uppercase text-[9px]',
                      t.status === 'succeeded' && 'text-up',
                      t.status === 'failed' && 'text-down',
                      t.status === 'pending' && 'text-warning',
                      t.status === 'running' && 'text-info',
                      t.status === 'cancelled' && 'text-muted-foreground',
                    )}
                  >
                    {t.status}
                  </span>
                  {t.positionId && (
                    <span className="text-muted-foreground truncate max-w-[120px]" title={t.positionId}>
                      pos {t.positionId.slice(0, 8)}…
                    </span>
                  )}
                  <span className="text-muted-foreground">#{t.attempts}</span>
                  {t.lastError && (
                    <span className="text-down w-full break-all text-[9px]">{t.lastError}</span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
