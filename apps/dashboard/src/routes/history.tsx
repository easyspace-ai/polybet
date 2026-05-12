import { createFileRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/app-shell";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useState, useEffect } from 'react';
import { getTrades, postCacheRefresh, type Trade } from '@/lib/api';
import { useBalanceCache } from '@/hooks/useBalanceCache';
import { cn } from '@/lib/utils';
import { formatOdds } from '@/lib/oddsFormat';
import { useOddsFormat } from '@/hooks/useOddsFormat';
import { VenueLogo } from '@/components/venue-logo';

export const Route = createFileRoute("/history")({
  component: HistoryPage,
});

function formatUsd(n: number): string {
  return n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function BalanceCard({
  label,
  amount,
  accent,
  onRefresh,
}: {
  label: string;
  amount: number | null;
  accent: string;
  onRefresh?: () => void;
}) {
  return (
    <div className="flex-1 rounded-md border border-border bg-surface px-3 py-2">
      <div className="flex items-center justify-between">
        <div className={cn('font-mono text-[9px] font-semibold tracking-[0.2em]', accent)}>
          {label}
        </div>
        {onRefresh && (
          <button
            type="button"
            onClick={() => void onRefresh()}
            className="font-mono text-[9px] text-muted-foreground hover:text-foreground"
            title="刷新余额"
          >
            ↻
          </button>
        )}
      </div>
      <div className="mt-1 font-mono text-[18px] font-semibold text-foreground">
        {amount == null ? (
          <span className="text-muted-foreground">不可用</span>
        ) : (
          <>
            <span className="text-muted-foreground">$</span>
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
  if (status === 'filled') return 'text-up';
  if (status === 'failed') return 'text-down';
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

function TradeRow({ t }: { t: Trade }) {
  const [oddsFmt] = useOddsFormat();
  const size = t.executedSize != null ? t.executedSize : t.requestedSize;
  const statusLabel = statusLabelZh(t.status);

  return (
    <div
      className="grid items-center border-b border-border px-2.5 py-2.5"
      style={{ gridTemplateColumns: GRID_COLS, columnGap: 8 }}
    >
      <span className="font-mono text-[10px] text-muted-foreground">{formatDate(t.createdAt)}</span>
      <span className="overflow-hidden text-ellipsis whitespace-nowrap text-[12px] text-foreground">
        {t.marketName}
      </span>
      <span className="overflow-hidden text-ellipsis whitespace-nowrap text-[12px] text-muted-foreground">
        {t.outcomeLabel}
      </span>
      <span className="flex items-center">
        <VenueLogo size={18} />
      </span>
      <span
        className={cn(
          'font-mono text-[10px] font-semibold',
          t.side === 'buy' ? 'text-primary' : 'text-muted-foreground',
        )}
      >
        {sideLabelZh(t.side)}
      </span>
      <span className="text-right font-mono text-[12px] font-semibold text-foreground">
        {size.toFixed(2)}
      </span>
      <span className="text-right font-mono text-[12px] font-semibold text-foreground">
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
          className="font-mono text-[10px] text-primary hover:underline"
        >
          {t.txHash.slice(0, 6)}…↗
        </a>
      ) : (
        <span className="font-mono text-[10px] text-muted-foreground">—</span>
      )}
    </div>
  );
}

function HistoryPage() {
  const [trades, setTrades] = useState<Trade[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const { balance } = useBalanceCache();
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

  const totalPages = Math.max(1, Math.ceil(total / limit));

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <PageHeader
        left={
          <>
            <span className="font-medium">交易历史</span>
            <span className="font-mono text-[11px] bg-surface text-muted-foreground px-2 py-0.5 rounded">
              共 <span className="text-foreground">{total}</span> 笔
            </span>
          </>
        }
        right={
          <div className="flex items-center gap-2 font-mono text-[10px] tracking-wider text-muted-foreground">
            <button
              onClick={() => setPage((p) => p - 1)}
              disabled={page === 1}
              className="px-2 py-1 rounded-md border border-border bg-transparent hover:bg-surface hover:text-foreground disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-muted-foreground"
            >
              <ChevronLeft className="size-3.5" />
            </button>
            <span className="px-1 text-foreground">
              {page}/{totalPages}
            </span>
            <button
              onClick={() => setPage((p) => p + 1)}
              disabled={page >= totalPages}
              className="px-2 py-1 rounded-md border border-border bg-transparent hover:bg-surface hover:text-foreground disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-muted-foreground"
            >
              <ChevronRight className="size-3.5" />
            </button>
          </div>
        }
      />

      <div className="flex-1 min-h-0 overflow-auto p-4">
        <div className="mb-4 flex flex-col gap-3">
          <div className="flex gap-3">
            <BalanceCard
              label="Polymarket（当前）"
              amount={balance?.polymarket ?? null}
              accent="text-primary"
              onRefresh={async () => {
                await postCacheRefresh();
              }}
            />
          </div>
          {(balance?.polymarketAccounts?.length ?? 0) > 0 && (
            <div className="rounded-md border border-border bg-surface px-3 py-2">
              <p className="font-mono text-[9px] font-semibold tracking-[0.2em] text-muted-foreground mb-2">
                各账号 pUSD
              </p>
              <ul className="space-y-1.5">
                {balance!.polymarketAccounts!.map((row) => (
                  <li key={row.id} className="flex justify-between font-mono text-[11px] text-foreground">
                    <span>
                      {row.name}
                      {row.isActive ? <span className="text-primary ml-1">· 当前</span> : null}
                    </span>
                    <span className="text-muted-foreground">
                      {row.polymarket == null ? '—' : `$${formatUsd(row.polymarket)}`}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>

        {error && (
          <div className="mb-3 rounded-sm border border-down/30 bg-down/10 px-3 py-2 font-mono text-[11px] text-down">
            {error}
          </div>
        )}

        <div
          className="grid items-center border-b border-border px-2.5 py-2 font-mono text-[9px] font-semibold tracking-[0.2em] text-muted-foreground"
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
          <div className="px-2.5 py-8 text-center font-mono text-[11px] text-muted-foreground">
            <div className="mx-auto mb-3 size-12 rounded-full border border-dashed border-border grid place-items-center font-mono text-xl">∅</div>
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
