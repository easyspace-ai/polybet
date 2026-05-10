import { useCallback, useEffect, useState } from 'react';
import {
  getRiskPositions,
  getRiskTasks,
  patchRiskPosition,
  postRiskCloseAll,
  postRiskClosePosition,
  type RiskPositionRow,
  type RiskPositionsMeta,
  type RiskTaskRow,
} from '../lib/api';
import { toast } from '../components/ui/use-toast';
import { cn } from '../lib/utils';

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

export function RiskControl() {
  const [positions, setPositions] = useState<RiskPositionRow[]>([]);
  const [meta, setMeta] = useState<RiskPositionsMeta | null>(null);
  const [tasks, setTasks] = useState<RiskTaskRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [closingId, setClosingId] = useState<string | null>(null);
  const [closingAll, setClosingAll] = useState(false);
  /** Draft inputs keyed by position id — reset when `positions` refresh. */
  const [drafts, setDrafts] = useState<Record<string, { sl: string; hw: string }>>({});
  const [patchingKey, setPatchingKey] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [p, t] = await Promise.all([getRiskPositions(), getRiskTasks(50)]);
      setPositions(p.positions);
      setMeta(p.meta ?? null);
      setTasks(t.tasks);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
    const id = setInterval(() => void load(), 5000);
    return () => clearInterval(id);
  }, [load]);

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
      await load();
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
      await load();
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
      await load();
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
      await load();
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
    <div className="flex h-full min-h-0 flex-col bg-tm-bg">
      <div className="shrink-0 border-b border-tm-bd bg-tm-bg px-4 py-2">
        <div className="flex items-center justify-between gap-3">
        <span className="font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-dim">
          风控
        </span>
        <button
          type="button"
          disabled={closingAll || positions.length === 0}
          onClick={() => void onCloseAll()}
          className={cn(
            'rounded-sm px-2.5 py-1 font-mono text-[10px] font-bold tracking-wider transition-colors',
            closingAll || positions.length === 0
              ? 'bg-tm-bg-sunk text-tm-tx-mut cursor-not-allowed'
              : 'bg-tm-neg/90 text-white hover:bg-tm-neg',
          )}
        >
          {closingAll ? '…' : '一键全部平仓'}
        </button>
        </div>
        {meta && (
          <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 font-mono text-[9px] text-tm-tx-mut">
            <span>
              用户 WS：
              {meta.userWsConnected ? (
                <span className="text-tm-pos">已连接</span>
              ) : meta.userWsConnecting ? (
                <span className="text-amber-400">连接中…</span>
              ) : (
                <span className="text-tm-neg">未连接</span>
              )}
            </span>
            <span
              className={
                meta.outboundProxyConfigured
                  ? 'text-tm-tx'
                  : 'text-amber-500'
              }
              title="与 REST 相同：HTTP_PLATFORM_PROXY_URL 或 设置 → 代理"
            >
              出站 WSS：
              {meta.outboundProxyConfigured ? (
                <span className="text-tm-pos">已配置（CONNECT 隧道）</span>
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
            <span className="text-tm-tx-dim">
              风控最小份额 ≥ {meta.minOpenRiskShares ?? 1}（设置 → 通用 <span className="text-tm-tx">minOpenRiskShares</span>）
            </span>
            {meta.userWsLastIssue && (
              <span
                className="text-tm-neg w-full break-all"
                title={meta.userWsLastIssue}
              >
                WS 提示：{meta.userWsLastIssue}
                {/failed to connect|1006/i.test(meta.userWsLastIssue) &&
                  !meta.outboundProxyConfigured && (
                    <span className="block mt-0.5 text-tm-tx-dim font-normal">
                      当前为直连 Polymarket；若网络受限，请在环境变量
                      <code className="text-tm-tx"> HTTP_PLATFORM_PROXY_URL </code>
                      或 设置 → 代理 中填写支持 CONNECT 到
                      <code className="text-tm-tx"> ws-subscriptions-clob.polymarket.com:443 </code>
                      的 HTTP(S) 代理。
                    </span>
                  )}
                {/failed to connect|1006/i.test(meta.userWsLastIssue) &&
                  meta.outboundProxyConfigured && (
                    <span className="block mt-0.5 text-tm-tx-dim font-normal">
                      已走代理仍连不上：确认代理允许 CONNECT 到上述主机 443、超时足够；REST 仍会定期同步成交。
                    </span>
                  )}
              </span>
            )}
            <span className="text-tm-tx-dim max-w-[420px]">
              持仓与成交以用户通道为主；WS 不可用时定期拉取成交兜底；并与 CLOB 条件代币余额对账。各 token 订阅读取盘口用于移动止损。
            </span>
          </div>
        )}
      </div>

      <div className="flex-1 min-h-0 overflow-auto p-4">
        {error && (
          <div className="mb-3 rounded-sm border border-tm-neg/30 bg-tm-neg/10 px-3 py-2 font-mono text-[11px] text-tm-neg">
            {error}
          </div>
        )}
        {loading && (
          <div className="font-mono text-[11px] text-tm-tx-mut">加载中…</div>
        )}

        {!loading && positions.length === 0 && !error && (
          <div className="font-mono text-[11px] text-tm-tx-mut max-w-xl space-y-2">
            <p>
              暂无持仓。本系统成交与官网/CLOB 成交会通过用户 WebSocket（及 REST 兜底）合并到此处；移动止损按「设置 → 价格区间」的
              <span className="text-tm-tx">止损%</span>，相对持仓期间的
              <span className="text-tm-tx">最高水位</span>（YES 最优买价曾到过的最高价）计算触发价：
              <span className="text-tm-tx-dim"> 触发价 = 最高水位 × (1 − 止损% / 100)</span>
              （例如最高 80¢、止损 20% → 跌至 64¢ 以下触发）。当前价由市场频道订单簿推送驱动；平仓使用 FOK 卖单，相对最优买价向下按 tick 放宽（见设置 → 通用 → polymarketFokSellExtraTicks）；平仓失败会短间隔连重试。
            </p>
            <p className="text-tm-tx-dim text-[10px]">
              有持仓时，表格展示：均价、当前最优买价、份额、成本、盈亏、
              <span className="text-tm-tx">最高水位</span>、止损比例、
              <span className="text-tm-tx">移动止损触发价</span>。
            </p>
          </div>
        )}

        {positions.length > 0 && (
          <div className="overflow-x-auto rounded-sm border border-tm-bd">
            <table className="w-full min-w-[1120px] border-collapse font-mono text-[10px]">
              <thead>
                <tr className="border-b border-tm-bd bg-tm-bg-el text-tm-tx-mut text-left">
                  <th className="px-2 py-2 font-semibold">盘口</th>
                  <th className="px-2 py-2 font-semibold w-[72px]">来源</th>
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
                  return (
                    <tr key={row.id} className="border-b border-tm-bd/80 hover:bg-tm-bg-el/40">
                      <td className="px-2 py-2 align-top">
                        <div className="text-tm-tx font-semibold leading-snug max-w-[220px]">{row.title}</div>
                        <div className="mt-0.5 text-tm-tx-dim">{row.sideLabel}</div>
                        {row.status === 'closing' && (
                          <span className="mt-1 inline-block text-[9px] text-amber-400">平仓中…</span>
                        )}
                      </td>
                      <td className="px-2 py-2 text-tm-tx-dim whitespace-nowrap align-top">
                        {sourceLabel(row.source)}
                      </td>
                      <td className="px-2 py-2 text-tm-tx whitespace-nowrap">
                        {fmtCents(row.avgEntryCents)}
                        <span className="text-tm-tx-mut mx-0.5">→</span>
                        {fmtCents(row.currentCents)}
                      </td>
                      <td className="px-2 py-2 text-tm-tx">{row.sizeShares.toFixed(2)}</td>
                      <td className="px-2 py-2 text-tm-tx">{fmtUsd(row.costUsd)}</td>
                      <td className="px-2 py-2 text-tm-pos">{fmtUsd(row.potentialProfitUsd)}</td>
                      <td className="px-2 py-2">
                        <div className="text-tm-tx">{fmtUsd(row.valueUsd)}</div>
                        {row.pnlUsd != null && (
                          <div
                            className={cn(
                              'text-[9px] mt-0.5',
                              row.pnlUsd >= 0 ? 'text-tm-pos' : 'text-tm-neg',
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
                            className="w-full rounded-sm border border-tm-bd bg-tm-bg px-1 py-0.5 text-[10px] text-tm-poly disabled:opacity-40"
                          />
                          <button
                            type="button"
                            disabled={
                              row.status !== 'open' || patchingKey === `${row.id}:hw` || patchingKey === `${row.id}:sl`
                            }
                            onClick={() => void applyHighWater(row.id)}
                            className="rounded-sm bg-tm-bg-sunk px-1 py-0.5 text-[9px] font-bold text-tm-tx hover:bg-tm-bd disabled:cursor-not-allowed disabled:opacity-40"
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
                              className="min-w-0 flex-1 rounded-sm border border-tm-bd bg-tm-bg px-1 py-0.5 text-[10px] text-tm-tx disabled:opacity-40"
                            />
                            <span className="shrink-0 text-[9px] text-tm-tx-mut">%</span>
                          </div>
                          <button
                            type="button"
                            disabled={
                              row.status !== 'open' || patchingKey === `${row.id}:sl` || patchingKey === `${row.id}:hw`
                            }
                            onClick={() => void applyStopLossPct(row.id)}
                            className="rounded-sm bg-tm-bg-sunk px-1 py-0.5 text-[9px] font-bold text-tm-tx hover:bg-tm-bd disabled:cursor-not-allowed disabled:opacity-40"
                          >
                            {patchingKey === `${row.id}:sl` ? '…' : '应用'}
                          </button>
                        </div>
                      </td>
                      <td className="px-2 py-2 text-tm-neg">{fmtCents(row.trailingStopCents)}</td>
                      <td className="px-2 py-2">
                        <button
                          type="button"
                          disabled={row.status !== 'open' || closingId === row.id}
                          onClick={() => void onCloseOne(row.id)}
                          className={cn(
                            'w-full rounded-sm py-1 text-[10px] font-bold',
                            row.status !== 'open' || closingId === row.id
                              ? 'bg-tm-bg-sunk text-tm-tx-mut cursor-not-allowed'
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
          <div className="font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-dim mb-2">
            任务队列
          </div>
          <p className="font-mono text-[9px] text-tm-tx-mut mb-2 max-w-2xl">
            止损触发与手动平仓均进入此队列；状态为 failed 时自动重试——平仓任务前几轮为短间隔（应对深度/滑点/FOK
            未成交），其后退避拉长。卖单价格 = max(tick, 最优买价 − polymarketFokSellExtraTicks×tick)，数值在设置 → 通用。
          </p>
          {tasks.length === 0 ? (
            <div className="font-mono text-[10px] text-tm-tx-mut">暂无任务</div>
          ) : (
            <ul className="space-y-1.5 max-h-[280px] overflow-y-auto rounded-sm border border-tm-bd bg-tm-bg-el p-2">
              {tasks.map((t) => (
                <li
                  key={t.id}
                  className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 font-mono text-[10px] text-tm-tx border-b border-tm-bd/50 pb-1.5 last:border-0"
                >
                  <span className="text-tm-tx-dim shrink-0">{t.updatedAt.slice(5, 16)}</span>
                  <span className="font-semibold text-tm-accent">{t.type}</span>
                  <span
                    className={cn(
                      'uppercase text-[9px]',
                      t.status === 'succeeded' && 'text-tm-pos',
                      t.status === 'failed' && 'text-tm-neg',
                      t.status === 'pending' && 'text-amber-400',
                      t.status === 'running' && 'text-sky-400',
                      t.status === 'cancelled' && 'text-tm-tx-mut',
                    )}
                  >
                    {t.status}
                  </span>
                  {t.positionId && (
                    <span className="text-tm-tx-mut truncate max-w-[120px]" title={t.positionId}>
                      pos {t.positionId.slice(0, 8)}…
                    </span>
                  )}
                  <span className="text-tm-tx-mut">#{t.attempts}</span>
                  {t.lastError && (
                    <span className="text-tm-neg w-full break-all text-[9px]">{t.lastError}</span>
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
