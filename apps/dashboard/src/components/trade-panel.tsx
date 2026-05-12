import { useState, useEffect, useMemo } from 'react';
import { Button } from '@/components/ui/button';
import { postTrade, getOrderBook, type OrderBookLevel } from '@/lib/api';
import { toast } from '@/lib/toast';
import { cn } from '@/lib/utils';
import { formatOdds } from '@/lib/oddsFormat';
import { useOddsFormat } from '@/hooks/useOddsFormat';
import { usePolyOrderBook } from '@/hooks/usePolyOrderBook';
import { VenueLogo } from './venue-logo';

interface TradePanelProps {
  outcomeId: string;
  outcomeLabel: string;
  onTradeExecuted?: () => void;
  hideHeader?: boolean;
}

type FillStatus = 'filled' | 'partial' | 'unfilled';

interface AnnotatedLevel {
  level: OrderBookLevel;
  status: FillStatus;
  fillFraction: number;
}

function simulateFill(levels: OrderBookLevel[], size: number): AnnotatedLevel[] {
  let remaining = size;
  return levels.map((level) => {
    if (remaining <= 0) return { level, status: 'unfilled', fillFraction: 0 };
    if (remaining >= level.size) {
      remaining -= level.size;
      return { level, status: 'filled', fillFraction: 1 };
    }
    const fraction = remaining / level.size;
    remaining = 0;
    return { level, status: 'partial', fillFraction: fraction };
  });
}

export function TradePanel({ outcomeId, outcomeLabel, onTradeExecuted, hideHeader }: TradePanelProps) {
  const [oddsFmt] = useOddsFormat();
  const [size, setSize] = useState('');
  const [executing, setExecuting] = useState(false);
  const [restLevels, setRestLevels] = useState<OrderBookLevel[] | null>(null);
  const [polyTokenId, setPolyTokenId] = useState<string | null>(null);
  const [bookError, setBookError] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setRestLevels(null);
    setPolyTokenId(null);
    setBookError(false);
    getOrderBook(outcomeId)
      .then((resp) => {
        if (cancelled) return;
        setRestLevels(resp.levels);
        setPolyTokenId(resp.polyTokenId ?? null);
      })
      .catch(() => { if (!cancelled) setBookError(true); });
    return () => { cancelled = true; };
  }, [outcomeId]);

  const livePoly = usePolyOrderBook(polyTokenId);

  // REST snapshot first; Polymarket WS depth replaces the ladder when it arrives.
  const book: OrderBookLevel[] | null = useMemo(() => {
    if (restLevels === null) return null;
    const polyLevels: OrderBookLevel[] = livePoly
      ? livePoly.map((l) => ({ odds: l.odds, size: l.size, platform: 'polymarket' as const }))
      : restLevels.filter((l) => l.platform === 'polymarket');
    return [...polyLevels].sort((a, b) => a.odds - b.odds);
  }, [restLevels, livePoly]);

  const sizeNum = parseFloat(size);
  const validSize = !isNaN(sizeNum) && sizeNum > 0;

  const annotated: AnnotatedLevel[] = useMemo(
    () => (book && validSize
      ? simulateFill(book, sizeNum)
      : book?.map((level) => ({ level, status: 'unfilled' as FillStatus, fillFraction: 0 })) ?? []),
    [book, sizeNum, validSize],
  );

  // "Stake X · Max fill Y at Z avg"
  const fillSummary = useMemo(() => {
    if (!book || !validSize) return null;
    let filled = 0;
    let oddsSum = 0;
    for (const level of book) {
      if (filled >= sizeNum) break;
      const take = Math.min(level.size, sizeNum - filled);
      if (take <= 0) continue;
      filled += take;
      const decimal = level.odds > 0 ? 1 / level.odds : 0;
      oddsSum += decimal * take;
    }
    const avg = filled > 0 ? oddsSum / filled : 0;
    return { stake: sizeNum, maxFill: filled, avg };
  }, [book, sizeNum, validSize]);

  const potentialProfit = useMemo(() => {
    if (!fillSummary || fillSummary.avg <= 1 || fillSummary.maxFill <= 0) return null;
    return fillSummary.maxFill * (fillSummary.avg - 1);
  }, [fillSummary]);

  async function handleExecute() {
    if (!validSize) return;
    setExecuting(true);
    try {
      const result = await postTrade(outcomeId, 'buy', sizeNum);
      if (result.status === 'filled') {
        toast({
          title: '成交完成',
          description: `${result.trades.length} 笔成交 · ${result.trades.map((t) => t.platform).join('、')}`,
          variant: 'success',
        });
      } else if (result.status === 'partial') {
        const legReasons = result.trades
          .filter((t) => t.status !== 'filled' && t.failureReason)
          .map((t) => `${t.platform}: ${t.failureReason}`)
          .join(' · ');
        toast({
          title: '部分成交',
          description:
            (result.message && result.message.trim()) ||
            legReasons ||
            '部分路由失败 — 请至交易历史查看详情',
          variant: 'default',
        });
      } else {
        const legReasons = result.trades
          .filter((t) => t.status !== 'filled' && t.failureReason)
          .map((t) => `${t.platform}: ${t.failureReason}`)
          .join(' · ');
        const description =
          (result.message && result.message.trim()) ||
          legReasons ||
          '全部路由失败 — 请至交易历史查看';
        toast({
          title: '下单失败',
          description,
          variant: 'destructive',
        });
      }
      setSize('');
      onTradeExecuted?.();
    } catch (err) {
      const msg = err instanceof Error ? err.message : '执行失败';
      toast({ title: '下单失败', description: msg, variant: 'destructive' });
    } finally {
      setExecuting(false);
    }
  }

  return (
    <div className="flex flex-col">
      {!hideHeader && (
        <div className="px-4 py-2 border-b border-border">
          <p className="font-mono text-[10px] font-semibold tracking-[0.2em] text-muted-foreground">
            交易
          </p>
          <p className="text-[13px] font-semibold text-foreground mt-0.5">{outcomeLabel}</p>
        </div>
      )}

      {/* Order book ladder */}
      <div className="border-b border-border">
        <div
          className="grid items-center px-4 py-1.5 bg-sidebar border-b border-border font-mono text-[9px] font-semibold tracking-[0.18em] text-muted-foreground grid-cols-[28px_1fr_1fr_56px] md:grid-cols-[36px_1fr_1fr_80px] gap-x-2"
        >
          <span>源</span>
          <span>赔率</span>
          <span>量</span>
          <span className="text-right">成交</span>
        </div>
        {bookError ? (
          <p className="px-4 py-3 font-mono text-[11px] text-muted-foreground">盘口暂不可用</p>
        ) : book === null ? (
          <p className="px-4 py-3 font-mono text-[11px] text-muted-foreground">加载深度中…</p>
        ) : book.length === 0 ? (
          <p className="px-4 py-3 font-mono text-[11px] text-muted-foreground">暂无深度数据</p>
        ) : (
          annotated.slice(0, 10).map((a, i) => {
            const decimal = formatOdds(a.level.odds, oddsFmt);
            const isFilled = a.status !== 'unfilled';
            const hideOnMobile = i >= 5;
            return (
              <div
                key={i}
                className={cn(
                  hideOnMobile ? 'hidden md:grid' : 'grid',
                  'items-center h-9 md:h-6 px-4 border-t border-border first:border-t-0 grid-cols-[28px_1fr_1fr_56px] md:grid-cols-[36px_1fr_1fr_80px] gap-x-2',
                )}
              >
                <span className="inline-flex items-center justify-center w-8">
                  <VenueLogo size={16} />
                </span>
                <span className={cn('font-mono text-[12px] font-semibold', isFilled ? 'text-primary' : 'text-foreground')}>
                  {decimal}
                </span>
                <span className="font-mono text-[11px] text-muted-foreground">
                  ${a.level.size.toFixed(0)}
                </span>
                <div className="h-1.5 rounded-full bg-border overflow-hidden">
                  <div
                    className={cn('h-full rounded-full transition-all', 'bg-primary')}
                    style={{ width: `${(a.fillFraction * 100).toFixed(0)}%` }}
                  />
                </div>
              </div>
            );
          })
        )}
      </div>

      <div className="px-4 py-3 border-b border-border space-y-3">
        <p className="font-mono text-[10px] font-semibold tracking-[0.2em] text-muted-foreground">本金</p>
        <div className="flex items-stretch gap-3">
          <div className="flex items-center bg-background border border-border rounded-md focus-within:border-ring px-3 flex-1 min-w-0">
            <input
              type="number"
              min={0}
              placeholder="0"
              value={size}
              onChange={(e) => setSize(e.target.value)}
              className="flex-1 bg-transparent border-0 outline-none text-foreground font-mono text-base md:text-[15px] font-semibold text-right py-2 min-w-0 placeholder:text-muted-foreground"
            />
            <span className="ml-2 font-mono text-[10px] tracking-widest text-muted-foreground">USDC</span>
          </div>
          <div className="flex flex-col justify-center px-2 shrink-0 text-right">
            <span className="font-mono text-[9px] tracking-[0.18em] text-muted-foreground leading-none">潜在盈利</span>
            <span className="font-mono text-[15px] font-semibold text-up leading-tight mt-1">
              {potentialProfit !== null ? `$${potentialProfit.toFixed(2)}` : '—'}
            </span>
          </div>
        </div>

        <Button
          size="sm"
          onClick={handleExecute}
          disabled={!validSize || executing}
          className="w-full font-mono tracking-wider bg-primary text-primary-foreground hover:bg-primary/90"
        >
          {executing ? '执行中…' : '下单'}
        </Button>

        <p className="font-mono text-[10px] text-muted-foreground">
          {fillSummary
            ? `本金 ${fillSummary.stake.toFixed(0)} · 预计成交 ${fillSummary.maxFill.toFixed(1)} · 均价 ${formatOdds(fillSummary.avg > 0 ? 1 / fillSummary.avg : 0, oddsFmt)}`
            : '输入金额以模拟撮合'}
        </p>
      </div>
    </div>
  );
}
