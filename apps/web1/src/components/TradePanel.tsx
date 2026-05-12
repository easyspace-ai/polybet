import { useState, useEffect, useMemo } from 'react';
import { Button } from './ui/button';
import { postTrade, getOrderBook, type OrderBookLevel } from '../lib/api';
import { toast } from './ui/use-toast';
import { cn } from '../lib/utils';
import { formatOdds } from '../lib/oddsFormat';
import { useOddsFormat } from '../hooks/useOddsFormat';
import { usePolyOrderBook } from '../hooks/useOrderBook';
import { VenueLogo } from './VenueLogo';
import { GITHUB_REPO_URL } from '../lib/constants';

const isPublic = import.meta.env.VITE_PUBLIC_MODE === 'true';

// Inline official GitHub mark — lucide-react no longer ships brand icons due to trademark.
function GithubMark({ size = 14 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
    </svg>
  );
}

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

function venueChip(): { text: string; bg: string; border: string } {
  return { text: 'text-tm-poly', bg: 'bg-tm-poly/15', border: 'border-tm-poly' };
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
        <div className="px-4 py-2 border-b border-tm-bd">
          <p className="font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-mut">
            交易
          </p>
          <p className="text-[13px] font-semibold text-tm-tx mt-0.5">{outcomeLabel}</p>
        </div>
      )}

      {/* Order book ladder */}
      <div className="border-b border-tm-bd">
        <div
          className="grid items-center px-4 py-1.5 bg-tm-bg-sunk border-b border-tm-bd font-mono text-[9px] font-semibold tracking-[0.18em] text-tm-tx-mut grid-cols-[28px_1fr_1fr_56px] md:grid-cols-[36px_1fr_1fr_80px] gap-x-2"
        >
          <span>源</span>
          <span>赔率</span>
          <span>量</span>
          <span className="text-right">成交</span>
        </div>
        {bookError ? (
          <p className="px-4 py-3 font-mono text-[11px] text-tm-tx-dim">盘口暂不可用</p>
        ) : book === null ? (
          <p className="px-4 py-3 font-mono text-[11px] text-tm-tx-dim">加载深度中…</p>
        ) : book.length === 0 ? (
          <p className="px-4 py-3 font-mono text-[11px] text-tm-tx-dim">暂无深度数据</p>
        ) : (
          annotated.slice(0, 10).map((a, i) => {
            const chip = venueChip();
            const decimal = formatOdds(a.level.odds, oddsFmt);
            const isFilled = a.status !== 'unfilled';
            // Mobile caps at 5 levels, desktop at 10. Render up to 10 always; rows past
            // the 5th hide on small viewports via `hidden md:grid` so a resize brings
            // them back without re-rendering.
            const hideOnMobile = i >= 5;
            return (
              <div
                key={i}
                className={cn(
                  hideOnMobile ? 'hidden md:grid' : 'grid',
                  'items-center h-9 md:h-6 px-4 border-t border-tm-bd first:border-t-0 grid-cols-[28px_1fr_1fr_56px] md:grid-cols-[36px_1fr_1fr_80px] gap-x-2',
                )}
              >
                <span className="inline-flex items-center justify-center w-8">
                  <VenueLogo size={16} />
                </span>
                <span className={cn('font-mono text-[12px] font-semibold', isFilled ? chip.text : 'text-tm-tx')}>
                  {decimal}
                </span>
                <span className="font-mono text-[11px] text-tm-tx-dim">
                  ${a.level.size.toFixed(0)}
                </span>
                <div className="h-1.5 rounded-full bg-tm-bd overflow-hidden">
                  <div
                    className={cn('h-full rounded-full transition-all', 'bg-tm-poly')}
                    style={{ width: `${(a.fillFraction * 100).toFixed(0)}%` }}
                  />
                </div>
              </div>
            );
          })
        )}
      </div>

      {/* Public read-only mode: replace stake + actions with a GitHub CTA */}
      {isPublic ? (
        <div className="px-4 py-4 border-b border-tm-bd space-y-3">
          <p className="font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-mut">
            只读预览
          </p>
          <p className="text-[12px] text-tm-tx-dim leading-relaxed">
            当前为体育预测市场聚合器的只读视图。若需完整机器人、下单执行与智能路由，请前往
            GitHub 仓库自行部署或二次开发。
          </p>
          <p className="text-[12px] text-tm-tx-dim leading-relaxed">
            问题、建议或缺陷欢迎在 GitHub 提 Issue。
          </p>
          <p className="font-mono text-[10px] text-tm-tx-mut truncate">
            easyspace-ai/polybet
          </p>
          <a
            href={GITHUB_REPO_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center justify-center gap-2 w-full font-mono text-[11px] font-semibold tracking-[0.15em] uppercase rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-el text-tm-tx hover:bg-tm-accent hover:text-tm-bg hover:border-tm-accent transition-colors px-3 py-2.5"
          >
            <GithubMark size={14} />
            <span>在 GitHub 查看 ↗</span>
          </a>
        </div>
      ) : (
      <div className="px-4 py-3 border-b border-tm-bd space-y-3">
        <p className="font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-mut">本金</p>
        <div className="flex items-stretch gap-3">
          <div className="flex items-center bg-tm-bg border border-tm-bd rounded-[var(--tm-rad)] focus-within:border-tm-bd-st px-3 flex-1 min-w-0">
            <input
              type="number"
              min={0}
              placeholder="0"
              value={size}
              onChange={(e) => setSize(e.target.value)}
              className="flex-1 bg-transparent border-0 outline-none text-tm-tx font-mono text-base md:text-[15px] font-semibold text-right py-2 min-w-0 placeholder:text-tm-tx-mut"
            />
            <span className="ml-2 font-mono text-[10px] tracking-widest text-tm-tx-mut">USDC</span>
          </div>
          <div className="flex flex-col justify-center px-2 shrink-0 text-right">
            <span className="font-mono text-[9px] tracking-[0.18em] text-tm-tx-mut leading-none">潜在盈利</span>
            <span className="font-mono text-[15px] font-semibold text-tm-pos leading-tight mt-1">
              {potentialProfit !== null ? `$${potentialProfit.toFixed(2)}` : '—'}
            </span>
          </div>
        </div>

        <Button
          size="sm"
          onClick={handleExecute}
          disabled={!validSize || executing}
          className="w-full font-mono tracking-wider bg-tm-poly text-tm-bg hover:bg-tm-poly/90"
        >
          {executing ? '执行中…' : '下单'}
        </Button>

        <p className="font-mono text-[10px] text-tm-tx-mut">
          {fillSummary
            ? `本金 ${fillSummary.stake.toFixed(0)} · 预计成交 ${fillSummary.maxFill.toFixed(1)} · 均价 ${formatOdds(fillSummary.avg > 0 ? 1 / fillSummary.avg : 0, oddsFmt)}`
            : '输入金额以模拟撮合'}
        </p>
      </div>
      )}
    </div>
  );
}
