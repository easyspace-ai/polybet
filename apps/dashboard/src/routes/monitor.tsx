import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect, useRef } from "react";
import { TopBar } from "@/components/TopBar";
import { RiskRuntimeLogPanel } from "@/components/RiskRuntimeLogPanel";
import { RefreshCw, AlertTriangle, ExternalLink, EyeOff } from "lucide-react";
import { useMonitorCache, applyMonitorPositionPatch } from "@/hooks/useMonitorCache";
import { monitorCoordinator } from "@/lib/monitor/coordinator";
import {
  postMonitorClosePosition,
  postMonitorCloseAll,
  patchMonitorPosition,
  postMonitorOfficialRefresh,
  postMonitorHidePosition,
  postMonitorTasksClear,
} from "@/lib/api";
import { resolvePolymarketEventUrl } from "@/lib/polymarketLinks";
import { cn } from "@/lib/utils";
import {
  floorCents1,
  linkTrailingStopDraft,
  trailingStopCentsFromHW,
  type TrailingStopEditField,
} from "@/lib/cents";
import { toast } from "sonner";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

export const Route = createFileRoute("/monitor")({ component: MonitorPage });

function fmtUsd(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return "—";
  const sign = n < 0 ? "−" : "";
  const v = Math.abs(n);
  return `${sign}$${v.toFixed(2)}`;
}

function fmtCents(c: number | null | undefined): string {
  if (c == null || !Number.isFinite(c)) return "—";
  return `${floorCents1(c).toFixed(1)}¢`;
}

function truncTitle(s: string, max = 42): string {
  const t = s.trim();
  if (t.length <= max) return t;
  return `${t.slice(0, max - 1)}…`;
}

function bookSubStatusLabel(
  sub: import("@/lib/api").RiskBookSubscriptionStatus | undefined,
  obConnected: boolean,
): { text: string; className: string; title: string } {
  if (!obConnected) {
    return { text: "OB断", className: "text-warning", title: "订单簿上游未连接" };
  }
  if (!sub) {
    return { text: "未知", className: "text-muted-foreground", title: "尚未拉取订阅状态" };
  }
  if (!sub.clientSubscribed) {
    return {
      text: "未订",
      className: "text-danger",
      title: "浏览器 CLOB 行情 WS 未订阅此 token",
    };
  }
  if (!sub.upstreamSubscribed && !sub.clientSubscribed) {
    return {
      text: "OB断",
      className: "text-warning",
      title: "订单簿上游未连接",
    };
  }
  if (sub.stale) {
    const age =
      sub.lastFrameMs && sub.lastFrameMs > 0
        ? `${Math.round((Date.now() - sub.lastFrameMs) / 1000)}s 无帧`
        : "无帧";
    return { text: "过期", className: "text-warning", title: `盘口帧过期（${age}）` };
  }
  return {
    text: "正常",
    className: "text-success",
    title: sub.lastFrameMs
      ? `最近帧 ${new Date(sub.lastFrameMs).toLocaleTimeString("zh-CN")}`
      : "已订阅且活跃",
  };
}

function relAgeShort(iso: string | null): string {
  if (!iso) return "—";
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t)) return "—";
  const sec = Math.floor((Date.now() - t) / 1000);
  if (sec < 0) return "刚刚";
  if (sec < 60) return `${sec}s前`;
  const m = Math.floor(sec / 60);
  if (m < 60) return `${m}m前`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h前`;
  return `${Math.floor(h / 24)}d前`;
}

/** Human-readable lines from server `last_attempt_detail` JSON (FOK / abort snapshot). */
function closeAttemptSummaryLines(raw: string | null | undefined): string[] {
  if (!raw?.trim()) return [];
  try {
    const o = JSON.parse(raw) as Record<string, unknown>;
    const lines: string[] = [];
    const push = (label: string, v: unknown) => {
      if (v === undefined || v === null || v === "") return;
      lines.push(
        `${label}: ${typeof v === "number" && Number.isFinite(v) ? (Math.abs(v) < 1e-6 ? String(v) : String(v)) : String(v)}`,
      );
    };
    push("执行模式", o.executionMode);
    push("对冲 token", o.hedgeTokenId);
    push("对冲预算方式", o.hedgeSizing);
    push("买单 USDC", o.sizeUSDC);
    push("方向", o.side);
    push("worst 配置价", o.worstPriceConfigured);
    push("阶段", o.phase);
    push("时间(UTC)", o.at);
    push("移动止损线¢", o.trailCents);
    push("高水位¢", o.highWaterCents);
    push("止损%", o.stopLossPct);
    push("评估侧bid¢", o.evalBidCents);
    push("评估侧ask¢", o.evalAskCents);
    push("CLOB best bid(0-1)", o.bestBid);
    push("CLOB best ask(0-1)", o.bestAsk);
    push("提交限价(小数)", o.limitPriceDecimal);
    push("提交限价¢", o.limitPriceCents);
    push("下单份额", o.sharesSubmitted);
    push("持仓请求份额", o.positionSharesRequested);
    push("链上条件余额份额", o.onChainBalanceShares);
    push("tick", o.tickSize);
    push("额外tick", o.extraTicks);
    push("orderId", o.orderId);
    push("错误步骤", o.errorStep);
    push("错误", o.err);
    push("中止原因", o.abortReason);
    return lines;
  } catch {
    return [raw];
  }
}

function riskCloseModeBanner(
  meta: {
    riskCloseExecutionMode?: string;
    riskCloseFakWorstPrice?: number;
    riskHedgeBuySizing?: string;
  } | null,
): string {
  const m = (meta?.riskCloseExecutionMode || "fok_sell").trim();
  if (m === "fak_sell") {
    const w = meta?.riskCloseFakWorstPrice;
    const ws = w != null && Number.isFinite(w) ? String(w) : "0.01";
    return `当前全局平仓：FAK 卖出（worst ${ws}）· 部分成交会自动重试`;
  }
  if (m === "hedge_fok_buy") {
    const s = (meta?.riskHedgeBuySizing || "notional").trim();
    const sizing = s === "shares" ? "按份额估算买单" : "按等值美元买单";
    return `当前全局平仓：反向 FOK 买单对冲（${sizing}）· 不卖出原 YES；成功后默认不再监控该仓位`;
  }
  return "当前全局平仓：FOK 卖出（整笔成交或取消）";
}

function trailDraftFor(
  p: { stopLossPct: number; highWaterCents: number; trailingStopCents?: number },
  prev?: { sl: string; hw: string; trigger: string },
) {
  const hw = prev?.hw ?? String(floorCents1(p.highWaterCents));
  const sl = prev?.sl ?? String(p.stopLossPct);
  const hwNum = floorCents1(p.highWaterCents);
  const trigger =
    prev?.trigger ??
    String(
      p.trailingStopCents != null && Number.isFinite(p.trailingStopCents)
        ? floorCents1(p.trailingStopCents)
        : trailingStopCentsFromHW(hwNum, p.stopLossPct),
    );
  return { sl, hw, trigger };
}

function MonitorPage() {
  const {
    positions,
    meta,
    tasks,
    loading,
    error,
    refresh,
    lastRefresh,
    polyOrderbookConnected,
    bookSubByToken,
  } = useMonitorCache();
  const [closingId, setClosingId] = useState<string | null>(null);
  const [closingAll, setClosingAll] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, { sl: string; hw: string; trigger: string }>>(
    {},
  );
  /** Row + field focused — blocks server sync for that field while the input is active. */
  const [trailEditing, setTrailEditing] = useState<{
    rowId: string;
    field: TrailingStopEditField;
  } | null>(null);
  const trailEditingRef = useRef<typeof trailEditing>(null);
  trailEditingRef.current = trailEditing;
  /** Per-field local edits (survive blur) until 应用 succeeds or row is reset. */
  const [trailDirty, setTrailDirty] = useState<
    Record<string, Partial<Record<TrailingStopEditField, boolean>>>
  >({});
  const trailDirtyRef = useRef(trailDirty);
  trailDirtyRef.current = trailDirty;

  const markTrailDirty = (rowId: string, field: TrailingStopEditField) => {
    setTrailDirty((prev) => ({
      ...prev,
      [rowId]: { ...prev[rowId], [field]: true },
    }));
  };

  const clearTrailDirty = (rowId: string) => {
    setTrailDirty((prev) => {
      if (!prev[rowId]) return prev;
      const next = { ...prev };
      delete next[rowId];
      return next;
    });
  };
  const [patchingKey, setPatchingKey] = useState<string | null>(null);
  const [hidingKey, setHidingKey] = useState<string | null>(null);
  const [officialSyncing, setOfficialSyncing] = useState(false);
  const [clearingTasks, setClearingTasks] = useState(false);

  // Init drafts; sync from server unless a field is focused or has uncommitted local edits.
  useEffect(() => {
    setDrafts((prev) => {
      const next = { ...prev };
      let changed = false;
      const editing = trailEditingRef.current;
      const dirtyMap = trailDirtyRef.current;
      for (const p of positions) {
        const sl0 = String(p.stopLossPct);
        const hw0 = String(floorCents1(p.highWaterCents));
        const trigger0 = String(
          p.trailingStopCents != null && Number.isFinite(p.trailingStopCents)
            ? floorCents1(p.trailingStopCents)
            : trailingStopCentsFromHW(floorCents1(p.highWaterCents), p.stopLossPct),
        );
        if (!next[p.id]) {
          next[p.id] = { sl: sl0, hw: hw0, trigger: trigger0 };
          changed = true;
          continue;
        }
        const rowField = editing?.rowId === p.id ? editing.field : null;
        const dirty = dirtyMap[p.id] ?? {};
        const d = { ...next[p.id] };
        let rowChanged = false;
        if (rowField !== "hw" && !dirty.hw && d.hw !== hw0) {
          d.hw = hw0;
          rowChanged = true;
        }
        if (rowField !== "sl" && !dirty.sl && d.sl !== sl0) {
          d.sl = sl0;
          rowChanged = true;
        }
        const recomputeTriggerFromDraft =
          rowField === "hw" || rowField === "sl" || dirty.hw || dirty.sl;
        const preserveTriggerDraft =
          (rowField === "trigger" || dirty.trigger) && !recomputeTriggerFromDraft;
        if (recomputeTriggerFromDraft) {
          const hwNum = parseFloat(d.hw);
          const slNum = parseFloat(d.sl);
          if (
            Number.isFinite(hwNum) &&
            hwNum > 0 &&
            Number.isFinite(slNum) &&
            slNum >= 1 &&
            slNum <= 99
          ) {
            const trigComputed = String(trailingStopCentsFromHW(floorCents1(hwNum), slNum));
            if (d.trigger !== trigComputed) {
              d.trigger = trigComputed;
              rowChanged = true;
            }
          }
        } else if (!preserveTriggerDraft) {
          const hwNum = parseFloat(d.hw);
          const slNum = parseFloat(d.sl);
          const trigComputed =
            p.trailingStopCents != null && Number.isFinite(p.trailingStopCents)
              ? String(floorCents1(p.trailingStopCents))
              : Number.isFinite(hwNum) &&
                  hwNum > 0 &&
                  Number.isFinite(slNum) &&
                  slNum >= 1 &&
                  slNum <= 99
                ? String(trailingStopCentsFromHW(floorCents1(hwNum), slNum))
                : trigger0;
          if (d.trigger !== trigComputed) {
            d.trigger = trigComputed;
            rowChanged = true;
          }
        }
        if (rowChanged) {
          next[p.id] = d;
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [positions, trailEditing, trailDirty]);

  const handleCloseOne = async (id: string) => {
    setClosingId(id);
    try {
      await postMonitorClosePosition(id);
      toast.success("已平仓", { description: "平仓任务已加入队列" });
      refresh();
    } catch (err) {
      toast.error("失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setClosingId(null);
    }
  };

  const handleCloseAll = async () => {
    setClosingAll(true);
    try {
      await postMonitorCloseAll();
      toast.success("已平仓", { description: "已为所有持仓创建平仓任务" });
      refresh();
    } catch (err) {
      toast.error("失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setClosingAll(false);
    }
  };

  const applyRiskControls = async (id: string) => {
    const d = drafts[id];
    if (!d) return;
    const sl = parseFloat(d.sl);
    const hwRaw = parseFloat(d.hw);
    if (!Number.isFinite(sl) || sl < 1 || sl > 99) {
      toast.error("无效", { description: "止损% 须在 1–99 之间" });
      return;
    }
    if (!Number.isFinite(hwRaw) || hwRaw <= 0 || hwRaw > 100) {
      toast.error("无效", { description: "最高水位须在 (0, 100]（¢）" });
      return;
    }
    const hw = floorCents1(hwRaw);
    setPatchingKey(`${id}:risk`);
    try {
      const resp = await patchMonitorPosition(id, { stopLossPct: sl, highWaterCents: hw });
      if (resp.position) {
        applyMonitorPositionPatch(resp.position);
        setDrafts((prev) => ({
          ...prev,
          [id]: trailDraftFor(resp.position),
        }));
        clearTrailDirty(id);
        setTrailEditing((e) => (e?.rowId === id ? null : e));
      }
      toast.success("已更新", { description: `高水位 ${hw}¢ · 止损 ${sl}%` });
    } catch (err) {
      toast.error("失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setPatchingKey(null);
    }
  };

  const handleHideFromMonitor = async (p: { id: string; tokenId: string; sideLabel: string }) => {
    if (!confirm("确定不再监控该仓位？（仍可在账户下通过 DELETE /api/risk/hidden-positions 恢复）"))
      return;
    setHidingKey(p.id);
    try {
      await postMonitorHidePosition({ tokenId: p.tokenId, sideLabel: p.sideLabel });
      toast.success("已隐藏", { description: "该仓位已从持仓监控中移除" });
      refresh();
    } catch (err) {
      toast.error("失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setHidingKey(null);
    }
  };

  const handleClearTaskLog = async () => {
    if (
      !confirm(
        "确定清除任务日志中的已完成记录？将删除状态为成功、失败、已取消的行；进行中的任务（待处理 / 执行中）会保留。",
      )
    ) {
      return;
    }
    setClearingTasks(true);
    try {
      const r = await postMonitorTasksClear();
      toast.success("已清除", { description: `已删除 ${r.deleted} 条记录` });
      refresh();
    } catch (err) {
      toast.error("失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setClearingTasks(false);
    }
  };

  const handleOfficialRefresh = async () => {
    setOfficialSyncing(true);
    try {
      const r = await postMonitorOfficialRefresh();
      if (r.alreadyRunning) {
        toast.info("同步进行中", { description: "已有官方同步任务在后台运行，请稍候" });
      } else {
        toast.success("已提交", { description: "官方持仓同步已在后台进行，完成后会自动更新" });
      }
      refresh();
    } catch (err) {
      toast.error("刷新失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setOfficialSyncing(false);
    }
  };

  return (
    <>
      <TopBar
        title="监控"
        subtitle={
          <>
            {lastRefresh && (
              <>
                <span className="text-muted-foreground">
                  更新 {relAgeShort(lastRefresh.toISOString())}
                </span>
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
              {officialSyncing ? "同步中..." : "刷新"}
            </button>
            <button
              onClick={handleCloseAll}
              disabled={closingAll || positions.length === 0}
              className="h-8 px-3 text-[12px] rounded-md bg-destructive text-destructive-foreground hover:opacity-90 transition flex items-center gap-1.5 font-medium disabled:opacity-50"
            >
              <AlertTriangle className="size-3.5" />
              {closingAll ? "处理中..." : "一键全部平仓"}
            </button>
          </>
        }
      />

      <div className="px-6 pt-4 -mb-2">
        <div className="rounded-lg border border-border/80 bg-muted/30 px-3 py-2 text-[11px] text-muted-foreground leading-snug">
          {riskCloseModeBanner(meta)}
          <span className="text-border mx-1.5">·</span>
          <span className="opacity-90">在「设置 → 止损平仓」修改；单笔无法切换模式。</span>
        </div>
      </div>

      <div className="p-6 pb-28 space-y-6 animate-slide-up">
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
              本系统成交与官网/CLOB 成交会通过用户 WebSocket（及 REST
              兜底）合并到此处；移动止损按「设置 →
              价格区间」的止损%，相对持仓期间的最高水位计算触发价。
            </div>
          </div>
        ) : (
          <>
            <TooltipProvider delayDuration={300}>
              {/* positions table */}
              <section className="surface rounded-xl border border-border overflow-hidden min-w-0">
                <div className="px-4 py-3 border-b border-border flex items-center justify-between gap-2 min-w-0">
                  <h2 className="text-[13px] font-semibold shrink-0">持仓监控</h2>
                  <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-widest truncate">
                    {positions.length} 个市场
                  </span>
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full min-w-[720px] table-fixed text-[11px]">
                    <colgroup>
                      <col className="w-[48px]" />
                      <col className="w-[28%]" />
                      <col className="w-[16%]" />
                      <col className="w-[52px]" />
                      <col className="w-[16%]" />
                      <col className="w-[12%]" />
                      <col className="w-[14%]" />
                    </colgroup>
                    <thead className="text-[9px] uppercase tracking-widest text-muted-foreground bg-background/40">
                      <tr>
                        <th className="px-2 py-2 font-medium text-right align-bottom">ID</th>
                        <th className="px-2 py-2 font-medium text-left align-bottom">市场</th>
                        <th className="px-2 py-2 font-medium text-left align-bottom">盘口</th>
                        <th className="px-2 py-2 font-medium text-center align-bottom">订阅</th>
                        <th className="px-2 py-2 font-medium text-left align-bottom">移动止损</th>
                        <th className="px-2 py-2 font-medium text-right align-bottom">市值</th>
                        <th className="px-2 py-2 font-medium text-right align-bottom w-[120px]">
                          操作
                        </th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border">
                      {positions.map((p) => {
                        const pnlPct =
                          p.pnlUsd != null && p.costUsd > 0 ? (p.pnlUsd / p.costUsd) * 100 : null;
                        const display = (p.displayTitle?.trim() || p.title).trim();
                        const href =
                          resolvePolymarketEventUrl(p.officialUrl, p.polySlug) ?? undefined;
                        const titleShort = truncTitle(display, 38);
                        const bookSub = bookSubStatusLabel(
                          bookSubByToken[p.tokenId] ?? p.bookSub,
                          polyOrderbookConnected || monitorCoordinator.isOrderbookConnected(),
                        );

                        return (
                          <tr key={p.id} className="hover:bg-accent/30 transition-colors align-top">
                            <td className="px-2 py-2.5 text-right font-mono text-[10px] text-muted-foreground tabular-nums">
                              {p.positionSeq != null && p.positionSeq > 0 ? p.positionSeq : "—"}
                            </td>
                            <td className="px-2 py-2.5 min-w-0">
                              <div className="flex items-start gap-2 min-w-0">
                                {p.iconUrl ? (
                                  <img
                                    src={p.iconUrl}
                                    alt=""
                                    className="size-6 rounded object-contain shrink-0 mt-0.5"
                                  />
                                ) : (
                                  <div className="size-6 rounded-md bg-brand/10 border border-brand/20 flex items-center justify-center shrink-0 mt-0.5">
                                    <div className="size-1.5 rounded-sm bg-brand" />
                                  </div>
                                )}
                                <div className="flex flex-col min-w-0 gap-0.5">
                                  <Tooltip>
                                    <TooltipTrigger asChild>
                                      <a
                                        href={href || "#"}
                                        target={href ? "_blank" : undefined}
                                        rel={href ? "noopener noreferrer" : undefined}
                                        className="text-[11px] font-medium text-foreground leading-snug line-clamp-2 hover:text-brand transition-colors flex items-start gap-1 min-w-0 group"
                                      >
                                        <span className="min-w-0 break-words">{titleShort}</span>
                                        {href && (
                                          <ExternalLink className="size-2.5 shrink-0 mt-0.5 text-muted-foreground group-hover:text-brand" />
                                        )}
                                      </a>
                                    </TooltipTrigger>
                                    <TooltipContent
                                      side="bottom"
                                      className="max-w-sm text-left font-normal"
                                    >
                                      <p className="text-[11px]">{display}</p>
                                      {p.sideLabel && (
                                        <p className="text-[10px] text-muted-foreground mt-1">
                                          方向：{p.sideLabel}
                                        </p>
                                      )}
                                    </TooltipContent>
                                  </Tooltip>
                                  <div className="text-[10px] num text-muted-foreground leading-tight">
                                    买价(均价){" "}
                                    <span className="text-foreground">
                                      {fmtCents(p.avgEntryCents)}
                                    </span>
                                    <span className="text-border mx-1">·</span>
                                    当前{" "}
                                    <span className="text-foreground">
                                      {fmtCents(
                                        p.bids?.[0] ? p.bids[0].odds * 100 : p.currentCents,
                                      )}
                                    </span>
                                  </div>
                                  <div className="text-[10px] num text-muted-foreground leading-tight">
                                    份额{" "}
                                    <span className="text-foreground">
                                      {p.sizeShares.toFixed(2)}
                                    </span>
                                    <span className="text-border mx-1">·</span>
                                    成本{" "}
                                    <span className="text-foreground">{fmtUsd(p.costUsd)}</span>
                                    <span className="text-border mx-1">·</span>
                                    可赢利{" "}
                                    <span className="text-success">
                                      {fmtUsd(p.potentialProfitUsd)}
                                    </span>
                                  </div>
                                </div>
                              </div>
                            </td>
                            <td className="px-2 py-2.5">
                              <div className="flex gap-2 text-[9px] leading-tight">
                                <div className="flex-1 min-w-0 space-y-0.5">
                                  <div className="text-muted-foreground uppercase tracking-tighter">
                                    Bid
                                  </div>
                                  {[0, 1, 2, 3].map((i) => (
                                    <div key={i} className="num text-success/90 truncate">
                                      {p.bids?.[i]
                                        ? `${fmtCents(p.bids[i].odds * 100)} $${p.bids[i].size.toFixed(0)}`
                                        : "—"}
                                    </div>
                                  ))}
                                </div>
                                <div className="flex-1 min-w-0 space-y-0.5">
                                  <div className="text-muted-foreground uppercase tracking-tighter">
                                    Ask
                                  </div>
                                  {[0, 1, 2, 3].map((i) => (
                                    <div key={i} className="num text-danger/90 truncate">
                                      {p.asks?.[i]
                                        ? `${fmtCents(p.asks[i].odds * 100)} $${p.asks[i].size.toFixed(0)}`
                                        : "—"}
                                    </div>
                                  ))}
                                </div>
                              </div>
                            </td>
                            <td className="px-1 py-2.5 text-center">
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <span
                                    className={cn(
                                      "text-[9px] font-medium uppercase tracking-tight cursor-default",
                                      bookSub.className,
                                    )}
                                  >
                                    {bookSub.text}
                                  </span>
                                </TooltipTrigger>
                                <TooltipContent side="top" className="text-[10px] max-w-xs">
                                  {bookSub.title}
                                </TooltipContent>
                              </Tooltip>
                            </td>
                            <td className="px-2 py-2.5">
                              <div className="flex flex-col gap-1 min-w-0">
                                <div className="flex items-center gap-1">
                                  <span className="text-[9px] text-muted-foreground shrink-0">
                                    高
                                  </span>
                                  <input
                                    type="number"
                                    step="0.1"
                                    min={0.1}
                                    max={100}
                                    disabled={p.status !== "open"}
                                    value={drafts[p.id]?.hw ?? ""}
                                    onFocus={() => setTrailEditing({ rowId: p.id, field: "hw" })}
                                    onBlur={() =>
                                      setTrailEditing((e) =>
                                        e?.rowId === p.id && e.field === "hw" ? null : e,
                                      )
                                    }
                                    onChange={(e) => {
                                      markTrailDirty(p.id, "hw");
                                      setDrafts((prev) => ({
                                        ...prev,
                                        [p.id]: linkTrailingStopDraft(
                                          trailDraftFor(p, prev[p.id]),
                                          "hw",
                                          e.target.value,
                                        ),
                                      }));
                                    }}
                                    className="min-w-0 flex-1 h-6 px-1 text-[10px] num rounded border border-border bg-background focus:outline-none focus:border-brand"
                                  />
                                  <span className="text-[9px] text-muted-foreground shrink-0">
                                    ¢
                                  </span>
                                </div>
                                <div className="flex items-center gap-1">
                                  <span className="text-[9px] text-muted-foreground shrink-0">
                                    损%
                                  </span>
                                  <input
                                    type="number"
                                    step={1}
                                    min={1}
                                    max={99}
                                    disabled={p.status !== "open"}
                                    value={drafts[p.id]?.sl ?? ""}
                                    onFocus={() => setTrailEditing({ rowId: p.id, field: "sl" })}
                                    onBlur={() =>
                                      setTrailEditing((e) =>
                                        e?.rowId === p.id && e.field === "sl" ? null : e,
                                      )
                                    }
                                    onChange={(e) => {
                                      markTrailDirty(p.id, "sl");
                                      setDrafts((prev) => ({
                                        ...prev,
                                        [p.id]: linkTrailingStopDraft(
                                          trailDraftFor(p, prev[p.id]),
                                          "sl",
                                          e.target.value,
                                        ),
                                      }));
                                    }}
                                    className="min-w-0 flex-1 h-6 px-1 text-[10px] num rounded border border-border bg-background focus:outline-none focus:border-brand"
                                  />
                                </div>
                                <div className="flex items-center gap-1">
                                  <span className="text-[9px] text-warning shrink-0">触发</span>
                                  <input
                                    type="number"
                                    step="0.1"
                                    min={0.1}
                                    max={100}
                                    disabled={p.status !== "open"}
                                    value={drafts[p.id]?.trigger ?? ""}
                                    onFocus={() =>
                                      setTrailEditing({ rowId: p.id, field: "trigger" })
                                    }
                                    onBlur={() => {
                                      const base = trailDraftFor(p, drafts[p.id]);
                                      const linked = linkTrailingStopDraft(
                                        base,
                                        "trigger",
                                        base.trigger,
                                      );
                                      setDrafts((prev) => ({
                                        ...prev,
                                        [p.id]: linked,
                                      }));
                                      setTrailDirty((d) => ({
                                        ...d,
                                        [p.id]: {
                                          ...d[p.id],
                                          trigger: true,
                                          ...(linked.sl !== base.sl ? { sl: true } : {}),
                                        },
                                      }));
                                      setTrailEditing((e) =>
                                        e?.rowId === p.id && e.field === "trigger" ? null : e,
                                      );
                                    }}
                                    onChange={(e) => {
                                      markTrailDirty(p.id, "trigger");
                                      setDrafts((prev) => ({
                                        ...prev,
                                        [p.id]: {
                                          ...trailDraftFor(p, prev[p.id]),
                                          trigger: e.target.value,
                                        },
                                      }));
                                    }}
                                    className="min-w-0 flex-1 h-6 px-1 text-[10px] num rounded border border-warning/40 bg-warning/5 text-warning focus:outline-none focus:border-warning"
                                  />
                                  <span className="text-[9px] text-warning shrink-0">¢</span>
                                </div>
                                <button
                                  type="button"
                                  onClick={() => applyRiskControls(p.id)}
                                  disabled={p.status !== "open" || patchingKey === `${p.id}:risk`}
                                  className="h-6 text-[10px] rounded border border-border hover:bg-accent transition disabled:opacity-50"
                                >
                                  {patchingKey === `${p.id}:risk` ? "…" : "应用"}
                                </button>
                              </div>
                            </td>
                            <td className="px-2 py-2.5 text-right">
                              <div className="num text-muted-foreground text-[10px]">
                                {fmtUsd(p.valueUsd)}
                              </div>
                              {p.pnlUsd != null && (
                                <div
                                  className={cn(
                                    "text-[9px] num mt-0.5",
                                    p.pnlUsd >= 0 ? "text-success" : "text-danger",
                                  )}
                                >
                                  {fmtUsd(p.pnlUsd)}
                                  {pnlPct != null && Number.isFinite(pnlPct) && (
                                    <span>
                                      {" "}
                                      ({pnlPct >= 0 ? "" : "−"}
                                      {Math.abs(pnlPct).toFixed(1)}%)
                                    </span>
                                  )}
                                </div>
                              )}
                            </td>
                            <td className="px-2 py-2.5 text-right">
                              <div className="inline-flex flex-col items-stretch gap-1.5 min-w-[88px]">
                                <button
                                  type="button"
                                  onClick={() => handleHideFromMonitor(p)}
                                  disabled={hidingKey === p.id}
                                  className="h-7 px-2 rounded border border-border text-[10px] text-muted-foreground hover:bg-accent hover:text-foreground transition flex items-center justify-center gap-1 disabled:opacity-50"
                                >
                                  <EyeOff className="size-3 shrink-0" />
                                  {hidingKey === p.id ? "…" : "不再监控"}
                                </button>
                                <button
                                  type="button"
                                  onClick={() => handleCloseOne(p.id)}
                                  disabled={p.status !== "open" || closingId === p.id}
                                  className="h-7 px-2 rounded-md bg-brand text-brand-foreground text-[11px] font-medium hover:opacity-90 transition active:scale-[0.98] disabled:opacity-50"
                                >
                                  {closingId === p.id ? "…" : "卖出"}
                                </button>
                              </div>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              </section>
            </TooltipProvider>

            {/* task queue logs */}
            <section className="space-y-3">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="flex items-center gap-2 min-w-0">
                  <h2 className="text-[13px] font-semibold shrink-0">任务队列</h2>
                  {tasks.length > 0 && (
                    <button
                      type="button"
                      onClick={handleClearTaskLog}
                      disabled={clearingTasks}
                      className="h-7 px-2.5 text-[10px] rounded-md border border-border bg-background hover:bg-accent transition disabled:opacity-50 shrink-0"
                    >
                      {clearingTasks ? "…" : "清除已完成"}
                    </button>
                  )}
                </div>
                <p className="text-[11px] text-muted-foreground max-w-2xl text-right flex-1 min-w-[200px]">
                  止损触发与手动平仓均进入此队列；状态为{" "}
                  <code className="text-warning">FAILED</code> 时自动重试，重试会逐步提高 FOK 卖单的
                  tick 让步（仍单笔 FOK，非挂单拆单）。 市场已结束导致{" "}
                  <code className="text-muted-foreground">aborted:market_ended</code>{" "}
                  后，同一持仓的止损入队会冷却一段时间，避免刷屏。 每条任务可展开查看最近一次提交的{" "}
                  <strong className="text-foreground/90">
                    限价、份额、CLOB 盘口快照与移动止损线
                  </strong>
                  （与服务器日志一致）。
                </p>
              </div>
              {tasks.length === 0 ? (
                <div className="surface-elevated rounded-xl border border-border p-4 text-[11.5px] text-muted-foreground">
                  暂无任务
                </div>
              ) : (
                <div className="surface-elevated rounded-xl border border-border p-4 font-mono text-[11.5px] space-y-2 max-h-[400px] overflow-y-auto scrollbar-thin">
                  {tasks.map((t) => {
                    const detailLines = closeAttemptSummaryLines(t.lastAttemptDetail ?? null);
                    return (
                      <div
                        key={t.id}
                        className="flex flex-col gap-1 py-1.5 hover:bg-accent/30 px-2 -mx-2 rounded transition-colors border-b border-border/40 last:border-0"
                      >
                        <div className="flex items-start gap-3 flex-wrap">
                          <span className="text-muted-foreground shrink-0">
                            {t.updatedAt.slice(5, 16)}
                          </span>
                          <span className="text-brand shrink-0">{t.type}</span>
                          <span
                            className={cn(
                              "shrink-0 uppercase text-[9px]",
                              t.status === "succeeded" && "text-success",
                              t.status === "failed" && "text-danger",
                              t.status === "pending" && "text-warning",
                              t.status === "running" && "text-blue-400",
                              t.status === "cancelled" && "text-muted-foreground",
                            )}
                          >
                            {t.status}
                          </span>
                          {t.positionId && (
                            <span className="text-muted-foreground">
                              pos {t.positionId.slice(0, 8)}…
                            </span>
                          )}
                          <span className="text-muted-foreground">#{t.attempts}</span>
                          {t.reason && (
                            <span className="text-[9px] text-muted-foreground">
                              reason {t.reason}
                            </span>
                          )}
                          {t.lastError && (
                            <span className="text-danger/80 break-all">{t.lastError}</span>
                          )}
                        </div>
                        {detailLines.length > 0 && (
                          <pre className="text-[9.5px] leading-snug text-muted-foreground/95 pl-0 sm:pl-[4.5rem] whitespace-pre-wrap break-all max-h-40 overflow-y-auto scrollbar-thin border-l border-border/60 pl-2 ml-1">
                            {detailLines.join("\n")}
                          </pre>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </section>
          </>
        )}
      </div>
      <RiskRuntimeLogPanel />
    </>
  );
}
