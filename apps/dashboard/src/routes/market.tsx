import { createFileRoute } from "@tanstack/react-router";
import React, { useState, useMemo, useEffect } from "react";
import { Search, Circle, Zap, RefreshCw, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { TopBar } from "@/components/TopBar";
import { useMarketList } from "@/hooks/useMarketList";
import { useBalanceCache } from "@/hooks/useBalanceCache";
import { usePolyOrderBook } from "@/hooks/useOrderBook";
import { useConfig } from "@/hooks/useConfig";
import { groupMarkets, formatDateHeader, localDateKey, isAmericanSport, get1X2, getSpreadMLTotal, type MatchGroup, type OutcomeRow } from "@/lib/marketUtils";
import { DEFAULT_EVENT_CLASSIFICATION_TAGS, parseEventClassificationTags } from "@/lib/eventClassification";
import { getOrderBook, postTrade, type Market, type OrderBookLevel } from "@/lib/api";
import { refreshMonitorData } from "@/hooks/useMonitorCache";
import { polymarketEventUrl } from "@/lib/polymarketLinks";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/market")({ component: MarketsPage });

function formatKickoff(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString('zh-CN', { hour: 'numeric', minute: '2-digit', timeZone: 'America/New_York' });
}

function useFetchAge(lastFetch: Date | null): string {
  const [, setTick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setTick(x => x + 1), 1000);
    return () => clearInterval(id);
  }, []);
  if (!lastFetch) return '—';
  const s = Math.floor((Date.now() - lastFetch.getTime()) / 1000);
  if (s < 60) return `${s}秒`;
  if (s < 3600) return `${Math.floor(s / 60)}分`;
  return `${Math.floor(s / 3600)}小时`;
}

const CLOSED_MARKET_STATUSES = new Set([
  'closed',
  'resolved',
  'settled',
  'final',
  'finalized',
  'inactive',
  'cancelled',
  'canceled',
  'expired',
  'archived',
]);

function isOpenMarket(market: Market): boolean {
  return !CLOSED_MARKET_STATUSES.has(market.status.toLowerCase());
}

function isSettledLikePrice(price: number | null | undefined): boolean {
  return price != null && Number.isFinite(price) && (price <= 0.01 || price >= 0.99);
}

function isTradableMatchGroup(group: MatchGroup): boolean {
  const primaryOutcomes = isAmericanSport(group)
    ? (() => {
        const { mlHome, mlAway } = getSpreadMLTotal(group);
        return [mlHome, mlAway];
      })()
    : (() => {
        const { home, draw, away } = get1X2(group);
        return [home, draw, away];
      })();
  const prices = primaryOutcomes
    .map((outcome) => outcome?.polymarket?.impliedOdds ?? null)
    .filter((price): price is number => price != null && Number.isFinite(price));

  if (prices.length === 0) return false;
  return !prices.some(isSettledLikePrice);
}

function OddsCell({ price, label, active, onClick }: { price: number | null; label: string; active: boolean; onClick?: () => void }) {
  if (price == null) {
    return (
      <div className="w-[92px] h-[58px] rounded-md border border-border flex items-center justify-center text-muted-foreground">
        —
      </div>
    );
  }
  return (
    <button
      onClick={onClick}
      className={`w-[92px] h-[58px] rounded-md border flex flex-col items-center justify-center transition-all duration-200 ${
        active
          ? "border-brand bg-brand/10 text-brand shadow-[0_0_0_1px_var(--color-brand),0_8px_24px_-12px_color-mix(in_oklab,var(--color-brand)_70%,transparent)]"
          : "border-border bg-surface hover:border-brand/40 hover:bg-brand/5"
      }`}
    >
      <span className={`text-[10px] font-mono truncate max-w-[80px] ${active ? "text-brand/80" : "text-muted-foreground"}`}>
        {label}
      </span>
      <span className={`text-[15px] font-semibold num ${active ? "text-brand" : ""}`}>
        {(price * 100).toFixed(1)}¢
      </span>
    </button>
  );
}

interface SelectedBet {
  outcomeId: string;
  label: string;
  matchName: string;
}

function MatchRow({ group, selectedOutcomeId, onSelect }: { group: MatchGroup; selectedOutcomeId: string | null; onSelect: (selection: SelectedBet) => void }) {
  const { home, away } = get1X2(group);
  const { mlHome, mlAway } = getSpreadMLTotal(group);
  const american = isAmericanSport(group);

  const [team1, team2] = group.name.split(' vs ').map((s) => s.trim());
  
  const homeOutcome = american ? mlHome : home;
  const awayOutcome = american ? mlAway : away;

  const eventUrl = polymarketEventUrl(group.polySlug);

  return (
    <div className="grid grid-cols-[80px_1fr_auto_auto] items-center gap-4 px-5 py-4 hover:bg-accent/30 transition-colors">
      <div className="text-[12px] font-mono text-muted-foreground">{formatKickoff(group.startTime)}</div>
      <a
        href={eventUrl ?? '#'}
        target={eventUrl ? '_blank' : undefined}
        rel={eventUrl ? 'noopener noreferrer' : undefined}
        className="flex items-center gap-3 min-w-0 group"
        onClick={eventUrl ? undefined : (e) => e.preventDefault()}
      >
        {group.iconUrl ? (
          <img src={group.iconUrl} alt="" className="size-7 rounded object-contain shrink-0" />
        ) : (
          <div className="size-7 rounded-md bg-brand/10 border border-brand/20 flex items-center justify-center shrink-0">
            <span className="text-[10px] font-bold text-brand">{team1.charAt(0)}</span>
          </div>
        )}
        <div className="flex flex-col leading-tight min-w-0">
          <span className="text-[13.5px] font-semibold truncate group-hover:text-brand transition-colors">{team1}</span>
          <span className="text-[13.5px] font-semibold text-muted-foreground/80 mt-0.5 truncate">{team2}</span>
        </div>
        {eventUrl && (
          <ExternalLink className="size-3 text-muted-foreground shrink-0 opacity-0 group-hover:opacity-100 transition-opacity" />
        )}
      </a>
      <div className="text-[10.5px] font-mono text-muted-foreground tracking-wide">—</div>
      <div className="flex items-center gap-2">
        <OddsCell 
          price={homeOutcome?.polymarket?.impliedOdds ?? null} 
          label={homeOutcome?.label ?? team1}
          active={selectedOutcomeId === homeOutcome?.outcomeId}
          onClick={() => homeOutcome && onSelect({ outcomeId: homeOutcome.outcomeId, label: homeOutcome.label, matchName: group.name })}
        />
        <OddsCell 
          price={awayOutcome?.polymarket?.impliedOdds ?? null} 
          label={awayOutcome?.label ?? team2}
          active={selectedOutcomeId === awayOutcome?.outcomeId}
          onClick={() => awayOutcome && onSelect({ outcomeId: awayOutcome.outcomeId, label: awayOutcome.label, matchName: group.name })}
        />
      </div>
    </div>
  );
}

function formatUsd(n: number): string {
  return n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function formatCents(price: number | null | undefined): string {
  if (price == null || !Number.isFinite(price)) return '—';
  return `${(price * 100).toFixed(1)}¢`;
}

function getActiveBalance(balance: ReturnType<typeof useBalanceCache>["balance"]): number | null {
  const active = balance?.polymarketAccounts?.find((a) => a.isActive);
  return active?.polymarket ?? balance?.polymarket ?? null;
}

function simulateBuyFill(levels: OrderBookLevel[], stake: number) {
  let remaining = stake;
  let spent = 0;
  let shares = 0;

  const fills = levels.map((level) => {
    const available = Math.max(0, level.size);
    const take = Math.min(available, remaining);
    remaining -= take;
    spent += take;
    if (level.odds > 0) shares += take / level.odds;
    return {
      level,
      fillFraction: available > 0 ? take / available : 0,
    };
  });

  const avgPrice = shares > 0 ? spent / shares : null;
  return {
    fills,
    spent,
    shares,
    avgPrice,
    potentialProfit: shares > 0 ? shares - spent : null,
  };
}

function VenueMark() {
  return (
    <span className="inline-flex h-7 w-7 items-center justify-center bg-blue-600 text-white">
      <svg viewBox="0 0 24 24" className="h-5 w-5" aria-hidden="true">
        <path
          d="M4 5.5 20 2v20L4 18.5v-4L16 12 4 9.5v-4Z"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinejoin="round"
        />
      </svg>
    </span>
  );
}

function TradeSidebar({
  selection,
  selectedOutcome,
  onClear,
}: {
  selection: SelectedBet | null;
  selectedOutcome: OutcomeRow | null;
  onClear: () => void;
}) {
  const { balance, loading: balanceLoading, refresh: refreshBalance } = useBalanceCache();
  const activeBalance = getActiveBalance(balance);
  const [amount, setAmount] = useState('');
  const [restBook, setRestBook] = useState<OrderBookLevel[] | null>(null);
  const [polyTokenId, setPolyTokenId] = useState<string | null>(null);
  const [bookError, setBookError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const liveBook = usePolyOrderBook(polyTokenId);

  useEffect(() => {
    setAmount('');
    setRestBook(null);
    setPolyTokenId(null);
    setBookError(null);
    if (!selection) return;

    let cancelled = false;
    getOrderBook(selection.outcomeId)
      .then((res) => {
        if (cancelled) return;
        setRestBook([...res.levels].filter((l) => l.platform === 'polymarket').sort((a, b) => a.odds - b.odds));
        setPolyTokenId(res.polyTokenId ?? null);
      })
      .catch((err) => {
        if (cancelled) return;
        setBookError(err instanceof Error ? err.message : '盘口暂不可用');
        setRestBook([]);
      });
    return () => {
      cancelled = true;
    };
  }, [selection?.outcomeId]);

  const book = useMemo<OrderBookLevel[] | null>(() => {
    if (restBook === null) return null;
    if (liveBook && liveBook.asks) {
      return liveBook.asks.map((level) => ({ ...level, platform: 'polymarket' as const })).sort((a, b) => a.odds - b.odds);
    }
    return restBook;
  }, [liveBook, restBook]);

  const amountNum = Number.parseFloat(amount);
  const validAmount = Number.isFinite(amountNum) && amountNum > 0;
  const bestPrice = book?.[0]?.odds ?? selectedOutcome?.polymarket?.impliedOdds ?? null;
  const fill = useMemo(
    () => simulateBuyFill(book ?? [], validAmount ? amountNum : 0),
    [book, amountNum, validAmount],
  );
  const estimatePrice = fill.avgPrice ?? bestPrice;
  const estimateShares = fill.shares || (estimatePrice && validAmount ? amountNum / estimatePrice : 0);
  const estimateProfit = fill.potentialProfit ?? (estimateShares && validAmount ? estimateShares - amountNum : null);
  const insufficientBalance = activeBalance != null && validAmount && amountNum > activeBalance;
  const unfilledPct = validAmount && amountNum > 0
    ? Math.max(0, ((amountNum - fill.spent) / amountNum) * 100)
    : null;
  const potentialProfitLabel = estimateProfit == null ? '—' : `+$${formatUsd(estimateProfit)}`;

  const setByBalance = (fraction: number) => {
    if (activeBalance == null || activeBalance <= 0) {
      toast.error('暂无可用余额');
      return;
    }
    setAmount((activeBalance * fraction).toFixed(2));
  };

  const handleSubmit = async () => {
    if (!selection || !validAmount || insufficientBalance) return;
    setSubmitting(true);
    try {
      const result = await postTrade(selection.outcomeId, 'buy', amountNum);
      if (result.status === 'filled') {
        toast.success('下单成功', { description: `${selection.label} · $${formatUsd(amountNum)}` });
      } else if (result.status === 'partial') {
        toast.warning('部分成交', { description: result.message || '请在交易历史查看详情' });
      } else {
        toast.error('下单失败', { description: result.message || '未成交，请稍后重试' });
      }
      setAmount('');
      refreshBalance();
      if (result.status === 'filled' || result.status === 'partial') {
        refreshMonitorData();
      }
    } catch (err) {
      toast.error('下单失败', { description: err instanceof Error ? err.message : '请求错误' });
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <aside className="w-[360px] max-w-[38vw] min-w-[320px] shrink-0 flex flex-col bg-sidebar/40 overflow-hidden">
     
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-thin px-6 py-5 space-y-5">
        {selection ? (
          <>
            {/* <div className="rounded-lg border border-brand/30 bg-brand/5 p-4 animate-slide-up">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <span className="block text-[13px] font-semibold truncate">{selection.label}</span>
                  <span className="mt-1 block text-[10.5px] text-muted-foreground font-mono truncate">{selection.matchName}</span>
                </div>
                <button
                  onClick={onClear}
                  className="shrink-0 text-[11px] text-muted-foreground hover:text-danger transition"
                >
                  移除
                </button>
              </div>
              <div className="mt-3 text-[10.5px] text-muted-foreground font-mono uppercase tracking-widest">YES</div>
              <div className="mt-3 flex items-baseline gap-2 min-w-0">
                <span className="text-3xl font-semibold num text-brand tracking-tight">{formatCents(bestPrice)}</span>
                <span className="text-[11px] text-muted-foreground font-mono truncate">
                  → {bestPrice ? `${(100 / bestPrice).toFixed(1)}%` : '—'}
                </span>
              </div>
            </div> */}

            <section className="border-y border-border -mx-6">
              <div className="grid grid-cols-[44px_1fr_1fr_84px] items-center border-b border-border px-6 py-2 text-[10px] font-mono font-semibold text-muted-foreground">
                <span>源</span>
                <span>赔率</span>
                <span>量</span>
                <span className="text-right">成交</span>
              </div>
              {bookError ? (
                <div className="px-6 py-3 text-[12px] text-danger">{bookError}</div>
              ) : book === null ? (
                <div className="px-6 py-3 text-[12px] text-muted-foreground">加载盘口...</div>
              ) : book.length === 0 ? (
                <div className="px-6 py-3 text-[12px] text-muted-foreground">暂无盘口深度</div>
              ) : (
                fill.fills.slice(0, 5).map(({ level, fillFraction }, index) => (
                  <div
                    key={`${level.odds}-${index}`}
                    className="grid h-8 grid-cols-[44px_1fr_1fr_84px] items-center border-b border-border px-6 last:border-b-0"
                  >
                    <VenueMark />
                    <span className="num text-[13px] font-semibold">{(level.odds * 100).toFixed(1)}%</span>
                    <span className="num text-[12px] text-muted-foreground">${level.size.toFixed(0)}</span>
                    <span className="h-1.5 rounded-full bg-border overflow-hidden">
                      <span
                        className="block h-full rounded-full bg-brand transition-all"
                        style={{ width: `${Math.round(fillFraction * 100)}%` }}
                      />
                    </span>
                  </div>
                ))
              )}
            </section>

            <section className="space-y-4 min-w-0">
              <div className="space-y-2">
                <div className="flex items-center justify-between gap-3 text-[11px] text-muted-foreground">
                  <label className="font-semibold text-foreground/80">下注金额 <span className="font-mono tracking-[0.18em] text-muted-foreground">USDC</span></label>
                  <span className="font-mono truncate">
                    {balanceLoading ? '余额加载中' : `可用 ${activeBalance == null ? '—' : `$${formatUsd(activeBalance)}`}`}
                  </span>
                </div>

                <div className="relative min-w-0">
                  <input
                    value={amount}
                    onChange={(e) => setAmount(e.target.value)}
                    inputMode="decimal"
                    className="w-full h-[68px] min-w-0 rounded-md border border-border bg-surface px-4 pr-[150px] text-left text-[26px] num tracking-tight focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand transition"
                    placeholder="0.00"
                  />
                  <div className="absolute right-3 top-1/2 flex -translate-y-1/2 items-center gap-1.5">
                    <button onClick={() => setByBalance(0.25)} className="h-8 rounded-md bg-muted px-2.5 text-[11px] font-mono text-muted-foreground hover:text-foreground transition">25%</button>
                    <button onClick={() => setByBalance(0.5)} className="h-8 rounded-md bg-muted px-2.5 text-[11px] font-mono text-muted-foreground hover:text-foreground transition">50%</button>
                    <button onClick={() => setByBalance(1)} className="h-8 rounded-md bg-muted px-2.5 text-[11px] font-mono text-muted-foreground hover:text-foreground transition">MAX</button>
                  </div>
                </div>

                {insufficientBalance && (
                  <div className="text-[11px] text-danger">余额不足，请降低金额或切换账号。</div>
                )}
              </div>

              <dl className="space-y-2.5 text-[12px]">
                <Row label="平均成交价" value={formatCents(estimatePrice)} />
                <Row label="预计份额" value={estimateShares ? estimateShares.toFixed(2) : '—'} />
                <Row label="最大盈利" value={potentialProfitLabel} className="text-success" />
                <Row label="未成交估算" value={unfilledPct == null || !book?.length ? '—' : `${unfilledPct.toFixed(1)}%`} />
              </dl>

              <button
                disabled={!selection || !validAmount || submitting || insufficientBalance}
                onClick={handleSubmit}
                className="w-full h-14 rounded-md bg-brand text-brand-foreground text-[15px] font-semibold tracking-tight hover:opacity-90 transition-all disabled:opacity-40 disabled:cursor-not-allowed shadow-[0_16px_36px_-16px_color-mix(in_oklab,var(--color-brand)_70%,transparent)]"
              >
                {submitting ? '下单中...' : '确认下单'}
              </button>
            </section>
          </>
        ) : (
          <div className="text-[12px] text-muted-foreground text-center py-10 border border-dashed border-border rounded-lg">
            点击赔率以加入投注单
          </div>
        )}
      </div>

    </aside>
  );
}

function MarketsPage() {
  const { markets, loading, error, lastUpdate, wsConnected, refresh } = useMarketList();
  const { rows: configRows } = useConfig();
  const configuredTags = useMemo(() => {
    const raw = configRows.find(r => r.key === 'eventClassificationTags')?.value ?? '';
    const tags = parseEventClassificationTags(raw);
    return tags.length > 0 ? tags : DEFAULT_EVENT_CLASSIFICATION_TAGS;
  }, [configRows]);

  const [activeLeague, setActiveLeague] = useState(configuredTags[0] || 'nba');

  useEffect(() => {
    if (configuredTags.length > 0 && !configuredTags.includes(activeLeague)) {
      setActiveLeague(configuredTags[0]);
    }
  }, [configuredTags, activeLeague]);

  const [selection, setSelection] = useState<SelectedBet | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const allGroups = useMemo(() => {
    return groupMarkets(markets.filter(isOpenMarket)).filter(isTradableMatchGroup);
  }, [markets]);

  const filteredGroups = useMemo(() => {
    return allGroups.filter(g => g.league.toLowerCase() === activeLeague.toLowerCase());
  }, [allGroups, activeLeague]);

  const groupsByDate = useMemo(() => {
    const byDate = new Map<string, MatchGroup[]>();
    for (const g of filteredGroups) {
      const dk = localDateKey(g.startTime);
      if (!byDate.has(dk)) byDate.set(dk, []);
      byDate.get(dk)!.push(g);
    }
    for (const list of byDate.values()) {
      list.sort((a, b) => new Date(a.startTime).getTime() - new Date(b.startTime).getTime());
    }
    return byDate;
  }, [filteredGroups]);

  const leagues = useMemo(() => {
    const counts = new Map<string, number>();
    for (const g of allGroups) {
      const league = g.league.toLowerCase();
      counts.set(league, (counts.get(league) || 0) + 1);
    }
    return configuredTags.map(tag => ({
      name: tag,
      count: counts.get(tag.toLowerCase()) || 0,
    }));
  }, [allGroups, configuredTags]);

  const selectedOutcome = useMemo(
    () => allGroups.flatMap(g => g.outcomes ?? []).find(o => o.outcomeId === selection?.outcomeId) ?? null,
    [allGroups, selection?.outcomeId],
  );

  const fetchAge = useFetchAge(lastUpdate);

  const handleRefresh = async () => {
    setRefreshing(true);
    try {
      await refresh();
      toast.success('市场已刷新', { description: '已从官方获取最新数据' });
    } catch (err) {
      toast.error('刷新失败', { description: err instanceof Error ? err.message : '请稍后重试' });
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <React.Fragment>
      <TopBar
        title="市场"
        subtitle={
          <>
            <span className="flex items-center gap-1.5">
              <span className={cn("size-1.5 rounded-full", wsConnected ? "bg-success animate-breathe" : "bg-warning")} />
              WS {wsConnected ? "已连接" : "未连接"}
            </span>
            <span className="text-border">·</span>
            <span>实时</span>
            <span className="text-border">·</span>
            <span>推送 {fetchAge}</span>
          </>
        }
        actions={
          <>
            <button
              onClick={handleRefresh}
              disabled={refreshing}
              className="h-8 px-3 text-[12px] rounded-md border border-border bg-surface hover:bg-accent transition flex items-center gap-1.5 disabled:opacity-50"
            >
              <RefreshCw className={cn("size-3.5", refreshing && "animate-spin")} />
              {refreshing ? '刷新中' : '刷新'}
            </button>
            <span className="flex items-center gap-1.5 px-2.5 py-1 rounded-md border border-success/30 bg-success/10 text-success text-[10.5px] font-mono">
              <Circle className="size-1.5 fill-success text-success animate-breathe" />
              POLY
            </span>
          </>
        }
      />

      <div className="flex flex-1 min-h-0 overflow-hidden">
        <section className="flex-1 min-w-0 flex flex-col border-r border-border">
          <div className="flex flex-1 min-h-0">
          {/* League list */}
          <aside className="w-[240px] shrink-0 border-r border-border flex flex-col">
            <div className="p-4">
              <div className="relative">
                <Search className="size-3.5 absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
                <input
                  placeholder="搜索联赛"
                  className="w-full h-9 pl-9 pr-3 text-[12px] rounded-md border border-border bg-surface focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand transition"
                />
              </div>
            </div>
            <div className="px-4 pb-2 text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
              联赛
            </div>
            <nav className="px-2 flex-1 overflow-y-auto scrollbar-thin">
              {leagues.length === 0 ? (
                <div className="px-3 py-2.5 text-[12px] text-muted-foreground">
                  {loading ? '加载中...' : '暂无市场'}
                </div>
              ) : (
                leagues.map((l) => {
                  const active = l.name.toLowerCase() === activeLeague.toLowerCase();
                  return (
                    <button
                      key={l.name}
                      onClick={() => setActiveLeague(l.name.toLowerCase())}
                      className={[
                        "w-full flex items-center justify-between px-3 py-2.5 rounded-md text-[13px] transition-all duration-200",
                        active
                          ? "bg-brand/10 text-brand border border-brand/30 shadow-[inset_0_0_0_1px_color-mix(in_oklab,var(--color-brand)_15%,transparent)]"
                          : "text-muted-foreground hover:text-foreground hover:bg-accent/60 border border-transparent",
                      ].join(" ")}
                    >
                      <span className="font-medium">{l.name}</span>
                      <span className={`text-[10.5px] font-mono ${active ? "text-brand" : "text-muted-foreground"}`}>
                        {l.count}
                      </span>
                    </button>
                  );
                })
              )}
            </nav>
            <div className={cn(
              "m-3 p-3 rounded-md border flex items-center justify-between",
              wsConnected 
                ? "border-success/30 bg-success/10" 
                : "border-warning/30 bg-warning/10"
            )}>
              <div>
                <div className="flex items-center gap-2 text-[11.5px] font-medium">
                  <Circle className={cn("size-1.5 animate-breathe", wsConnected ? "fill-success text-success" : "fill-warning text-warning")} />
                  WebSocket
                </div>
                <div className="text-[10px] text-muted-foreground font-mono mt-0.5">
                  {wsConnected ? '已连接' : '未连接'}
                </div>
              </div>
              <Zap className={cn("size-4", wsConnected ? "text-success" : "text-warning")} />
            </div>
          </aside>

          {/* Matches */}
          <div className="flex-1 min-w-0 overflow-y-auto scrollbar-thin px-8 py-6 animate-slide-up">
            {error && (
              <div className="mb-4 p-4 rounded-md border border-destructive/30 bg-destructive/10 text-destructive text-[12px] flex items-center gap-2">
                <span className="font-semibold">错误:</span> {error}
              </div>
            )}
            
            {groupsByDate.size === 0 && !loading && (
              <div className="text-center py-12 text-muted-foreground">
                <p className="text-[12px]">
                  {markets.length === 0
                    ? '暂无市场数据 — 同步任务可能仍在运行。'
                    : `暂无「${activeLeague.toUpperCase()}」相关市场。`}
                </p>
              </div>
            )}

            {Array.from(groupsByDate.entries()).map(([dateKey, dateGroups]) => (
              <div key={dateKey} className="mb-8">
                <div className="flex items-center gap-3 mb-3">
                  <h2 className="text-[14px] font-semibold tracking-tight">{formatDateHeader(dateKey)}</h2>
                  <span className="text-[11px] text-muted-foreground font-mono">{dateGroups.length} 场</span>
                  <div className="flex-1 h-px bg-border" />
                  <span className="text-[10.5px] uppercase tracking-[0.18em] text-muted-foreground">独赢</span>
                </div>
                <div className="rounded-xl border border-border surface divide-y divide-border overflow-hidden">
                  {dateGroups.map((group) => (
                    <MatchRow
                      key={`${group.league}-${group.name}-${group.startTime}`}
                      group={group}
                      selectedOutcomeId={selection?.outcomeId ?? null}
                      onSelect={setSelection}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
      </section>

      <TradeSidebar selection={selection} selectedOutcome={selectedOutcome} onClear={() => setSelection(null)} />
    </div>
    </React.Fragment>
  );
}

function Row({ label, value, className = "" }: { label: string; value: string; className?: string }) {
  return (
    <div className="flex items-center justify-between gap-3 min-w-0">
      <dt className="text-muted-foreground shrink-0">{label}</dt>
      <dd className={`num font-medium text-right truncate ${className}`}>{value}</dd>
    </div>
  );
}
