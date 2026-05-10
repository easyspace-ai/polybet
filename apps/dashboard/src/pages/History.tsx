import { useState, useEffect } from 'react';
import { getTrades, getBalances, type Trade, type BalanceSummary } from '../lib/api';
import { cn } from '../lib/utils';
import { formatOdds } from '../lib/oddsFormat';
import { useOddsFormat } from '../hooks/useOddsFormat';
import { VenueLogo } from '../components/VenueLogo';

function formatUsd(n: number): string {
  return n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function BalanceCard({
  label,
  amount,
  accent,
}: {
  label: string;
  amount: number | null;
  accent: string;
}) {
  return (
    <div className="flex-1 rounded-sm border border-tm-bd bg-tm-bg-el px-3 py-2.5">
      <div className={cn('font-mono text-[9px] font-semibold tracking-[0.2em]', accent)}>
        {label}
      </div>
      <div className="mt-1 font-mono text-[18px] font-semibold text-tm-tx">
        {amount == null ? (
          <span className="text-tm-tx-mut">不可用</span>
        ) : (
          <>
            <span className="text-tm-tx-dim">$</span>
            {formatUsd(amount)}
          </>
        )}
      </div>
    </div>
  );
}

const POLYGONSCAN = 'https://polygonscan.com/tx/';
const GRID_COLS = '110px 1.4fr 1fr 44px 48px 60px 56px 72px 72px';

function formatDate(iso: string): string {
  const d = new Date(iso);
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  const hh = String(d.getHours()).padStart(2, '0');
  const mi = String(d.getMinutes()).padStart(2, '0');
  return `${mm}/${dd} ${hh}:${mi}`;
}

function statusColor(status: string): string {
  if (status === 'filled') return 'text-tm-pos';
  if (status === 'failed') return 'text-tm-neg';
  return 'text-tm-warn';
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

function TradeRow({ t }: { t: Trade }) {
  const [oddsFmt] = useOddsFormat();
  const size = t.executedSize != null ? t.executedSize : t.requestedSize;
  const statusLabel = statusLabelZh(t.status);

  return (
    <div
      className="grid items-center border-b border-tm-bd px-2.5 py-2.5"
      style={{ gridTemplateColumns: GRID_COLS, columnGap: 8 }}
    >
      <span className="font-mono text-[10px] text-tm-tx-dim">{formatDate(t.createdAt)}</span>
      <span className="overflow-hidden text-ellipsis whitespace-nowrap text-[12px] text-tm-tx">
        {t.marketName}
      </span>
      <span className="overflow-hidden text-ellipsis whitespace-nowrap text-[12px] text-tm-tx-dim">
        {t.outcomeLabel}
      </span>
      <span className="flex items-center">
        <VenueLogo size={18} />
      </span>
      <span
        className={cn(
          'font-mono text-[10px] font-semibold',
          t.side === 'buy' ? 'text-tm-accent' : 'text-tm-tx-dim',
        )}
      >
        {sideLabelZh(t.side)}
      </span>
      <span className="text-right font-mono text-[12px] font-semibold text-tm-tx">
        {size.toFixed(2)}
      </span>
      <span className="text-right font-mono text-[12px] font-semibold text-tm-tx">
        {formatOdds(t.fillOdds, oddsFmt)}
      </span>
      <span
        className={cn('font-mono text-[10px] font-semibold tracking-wider', statusColor(t.status))}
        title={t.failureReason ?? undefined}
      >
        ● {statusLabel}
      </span>
      {t.txHash ? (
        <a
          href={`${POLYGONSCAN}${t.txHash}`}
          target="_blank"
          rel="noopener noreferrer"
          className="font-mono text-[10px] text-tm-poly hover:underline"
        >
          {t.txHash.slice(0, 6)}…↗
        </a>
      ) : (
        <span className="font-mono text-[10px] text-tm-tx-mut">—</span>
      )}
    </div>
  );
}

export function History() {
  const [trades, setTrades] = useState<Trade[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [balances, setBalances] = useState<BalanceSummary | null>(null);
  const limit = 20;

  useEffect(() => {
    setLoading(true);
    getTrades(page, limit)
      .then((res) => {
        setTrades(res.trades);
        setTotal(res.total);
        setError(null);
      })
      .catch((err) => setError(err instanceof Error ? err.message : '加载成交记录失败'))
      .finally(() => setLoading(false));
  }, [page]);

  useEffect(() => {
    getBalances()
      .then(setBalances)
      .catch(() => setBalances({ polymarket: null, polymarketAccounts: [] }));
  }, []);

  const totalPages = Math.max(1, Math.ceil(total / limit));

  return (
    <div className="flex h-full min-h-0 flex-col bg-tm-bg">
      <div className="h-10 shrink-0 flex items-center gap-4 px-4 bg-tm-bg border-b border-tm-bd">
        <span className="font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-dim">
          交易历史
        </span>
        <span className="font-mono text-[10px] px-1.5 py-0.5 rounded-sm bg-tm-bg-el border border-tm-bd text-tm-tx-dim">
          共 {total} 笔
        </span>

        <div className="ml-auto flex items-center gap-2 font-mono text-[10px] tracking-wider text-tm-tx-mut">
          <button
            onClick={() => setPage((p) => p - 1)}
            disabled={page === 1}
            className="px-2 py-1 rounded-sm border border-tm-bd bg-transparent hover:bg-tm-bg-el hover:text-tm-tx disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-tm-tx-mut"
          >
            ‹ 上一页
          </button>
          <span className="px-1 text-tm-tx-dim">
            {page}/{totalPages}
          </span>
          <button
            onClick={() => setPage((p) => p + 1)}
            disabled={page >= totalPages}
            className="px-2 py-1 rounded-sm border border-tm-bd bg-transparent hover:bg-tm-bg-el hover:text-tm-tx disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-tm-tx-mut"
          >
            下一页 ›
          </button>
        </div>
      </div>

      <div className="flex-1 min-h-0 overflow-auto p-4">
        <div className="mb-4 flex flex-col gap-3">
          <div className="flex gap-3">
            <BalanceCard
              label="Polymarket（当前）"
              amount={balances?.polymarket ?? null}
              accent="text-tm-poly"
            />
          </div>
          {(balances?.polymarketAccounts?.length ?? 0) > 0 && (
            <div className="rounded-sm border border-tm-bd bg-tm-bg-el px-3 py-2">
              <p className="font-mono text-[9px] font-semibold tracking-[0.2em] text-tm-tx-mut mb-2">
                各账号 pUSD
              </p>
              <ul className="space-y-1.5">
                {balances!.polymarketAccounts!.map((row) => (
                  <li key={row.id} className="flex justify-between font-mono text-[11px] text-tm-tx">
                    <span>
                      {row.name}
                      {row.isActive ? <span className="text-tm-poly ml-1">· 当前</span> : null}
                    </span>
                    <span className="text-tm-tx-dim">
                      {row.polymarket == null ? '—' : `$${formatUsd(row.polymarket)}`}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>

        {error && (
          <div className="mb-3 rounded-sm border border-tm-neg/30 bg-tm-neg/10 px-3 py-2 font-mono text-[11px] text-tm-neg">
            {error}
          </div>
        )}

        <div
          className="grid items-center border-b border-tm-bd-st px-2.5 py-2 font-mono text-[9px] font-semibold tracking-[0.2em] text-tm-tx-mut"
          style={{ gridTemplateColumns: GRID_COLS, columnGap: 8 }}
        >
          <span>时间</span>
          <span>市场</span>
          <span>选项</span>
          <span>源</span>
          <span>方向</span>
          <span className="text-right">金额</span>
          <span className="text-right">成交盘</span>
          <span>状态</span>
          <span>链上</span>
        </div>

        {!loading && !error && trades.length === 0 && (
          <div className="px-2.5 py-8 text-center font-mono text-[11px] text-tm-tx-mut">
            暂无成交记录
          </div>
        )}

        {trades.map((t) => (
          <TradeRow key={t.id} t={t} />
        ))}
      </div>
    </div>
  );
}
