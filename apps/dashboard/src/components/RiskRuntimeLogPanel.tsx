import { useCallback, useEffect, useMemo, useState } from "react";
import { ChevronUp, ChevronDown, Terminal, Trash2 } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { getRiskRuntimeLogs, type RiskRuntimeLogEnvelope } from "@/lib/api";
import { riskWsBus } from "@/lib/wsBus";

const MAX_LINES = 500;

/** Newest-first: `seq` is authoritative; `ts` breaks ties. */
function sortLogsDesc(entries: RiskRuntimeLogEnvelope[]): RiskRuntimeLogEnvelope[] {
  return [...entries].sort((a, b) => {
    if (b.seq !== a.seq) return b.seq - a.seq;
    return new Date(b.ts).getTime() - new Date(a.ts).getTime();
  });
}

function clampLogsDesc(entries: RiskRuntimeLogEnvelope[]): RiskRuntimeLogEnvelope[] {
  return sortLogsDesc(entries).slice(0, MAX_LINES);
}

/**
 * Wall-clock in the user's local timezone. `ts` must be RFC3339 / ISO8601;
 * `new Date(ts)` interprets Z and offsets correctly — do not add manual offsets.
 * If the server emits a wrong offset or a naive string without TZ, the instant
 * shown will still match what the string parses to in JS (may drift from server intent).
 */
function formatLogTsLocal(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

type Chip = "position" | "transport" | "stop" | "book";

function matchesChip(entry: RiskRuntimeLogEnvelope, chip: Chip): boolean {
  const { category, type: ty } = entry;
  switch (chip) {
    case "position":
      return category === "position";
    case "transport":
      return category === "transport" || category === "market_sub";
    case "book":
      return category === "market_data";
    case "stop":
      return (
        category === "position" &&
        (ty.includes("stop") ||
          ty.includes("close_queued") ||
          ty.includes("close_all") ||
          ty.includes("close_failed") ||
          ty.includes("closed"))
      );
    default:
      return true;
  }
}

function severityClass(sev: string): string {
  switch (sev) {
    case "error":
      return "text-danger";
    case "warn":
      return "text-warning";
    case "debug":
      return "text-muted-foreground/80";
    default:
      return "text-foreground/90";
  }
}

export function RiskRuntimeLogPanel() {
  const [open, setOpen] = useState(false);
  const [logs, setLogs] = useState<RiskRuntimeLogEnvelope[]>([]);
  const [chips, setChips] = useState<Set<Chip>>(() => new Set(["position", "transport", "stop", "book"]));

  const toggleChip = useCallback((c: Chip) => {
    setChips((prev) => {
      const next = new Set(prev);
      if (next.has(c)) next.delete(c);
      else next.add(c);
      return next;
    });
  }, []);

  const filtered = useMemo(() => {
    if (chips.size === 0) return logs;
    return logs.filter((e) => {
      for (const c of chips) {
        if (matchesChip(e, c)) return true;
      }
      return false;
    });
  }, [logs, chips]);

  const clearDisplay = useCallback(() => {
    setLogs([]);
  }, []);

  useEffect(() => {
    return riskWsBus.onRuntimeLog((msg) => {
      if (msg.type === "risk_runtime_log_snapshot") {
        setLogs(clampLogsDesc(msg.data));
        return;
      }
      setLogs((prev) => clampLogsDesc([msg.data, ...prev]));
    });
  }, []);

  useEffect(() => {
    if (!open) return;
    void getRiskRuntimeLogs(200).then((r) => {
      const incoming = r.logs || [];
      if (incoming.length === 0) return;
      setLogs((prev) => {
        const bySeq = new Map<number, RiskRuntimeLogEnvelope>();
        for (const e of incoming) bySeq.set(e.seq, e);
        for (const e of prev) bySeq.set(e.seq, e);
        return clampLogsDesc(Array.from(bySeq.values()));
      });
    });
  }, [open]);

  const chipBtn = (id: Chip, label: string) => (
    <button
      type="button"
      key={id}
      onClick={() => toggleChip(id)}
      className={cn(
        "h-6 px-2 rounded-full text-[10px] font-medium border transition",
        chips.has(id)
          ? "border-brand/50 bg-brand/15 text-foreground"
          : "border-border text-muted-foreground opacity-60 hover:opacity-100",
      )}
    >
      {label}
    </button>
  );

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <div className="fixed bottom-0 left-[160px] right-0 z-40 border-t border-border bg-background/95 backdrop-blur-md shadow-[0_-8px_24px_rgba(0,0,0,0.12)]">
        <CollapsibleTrigger asChild>
          <button
            type="button"
            className="w-full h-9 flex items-center justify-between px-4 text-[12px] font-medium text-muted-foreground hover:text-foreground hover:bg-accent/40 transition"
          >
            <span className="flex items-center gap-2">
              <Terminal className="size-3.5" />
              运行日志
              <span className="text-[10px] font-mono text-muted-foreground/80">({filtered.length})</span>
            </span>
            {open ? <ChevronDown className="size-4" /> : <ChevronUp className="size-4" />}
          </button>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="px-4 pb-2 pt-1 border-t border-border/60 flex flex-wrap gap-1.5 items-center justify-between gap-y-2">
            <div className="flex flex-wrap gap-1.5 items-center min-w-0">
              <span className="text-[10px] text-muted-foreground uppercase tracking-wider mr-1">筛选</span>
              {chipBtn("position", "仓位")}
              {chipBtn("transport", "连接")}
              {chipBtn("stop", "止损/平仓")}
              {chipBtn("book", "盘口摘要")}
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <button
                type="button"
                onClick={clearDisplay}
                disabled={logs.length === 0}
                className="h-6 px-2 rounded-md border border-border text-[10px] text-muted-foreground hover:bg-accent hover:text-foreground transition flex items-center gap-1 disabled:opacity-40 disabled:pointer-events-none"
                title="仅清空本页显示；重连或刷新后可能再次出现服务端缓冲中的记录"
              >
                <Trash2 className="size-3" />
                清空显示
              </button>
            </div>
          </div>
          <p className="px-4 pb-1 text-[9px] text-muted-foreground/90 leading-snug">
            时间按本机时区显示（ISO/RFC3339 解析为本地时刻）。若服务端 <code className="font-mono">ts</code>{" "}
            偏移或时区标注错误，显示可能与服务器意图不符。仅清空显示不影响服务端环形缓冲。
          </p>
          <TooltipProvider delayDuration={250}>
            <div className="h-[260px] overflow-y-auto scrollbar-thin text-[10px] leading-relaxed px-4 pb-4">
              {filtered.length === 0 ? (
                <p className="text-muted-foreground text-center py-8">暂无匹配事件</p>
              ) : (
                <div className="overflow-x-auto min-w-0 -mx-1 px-1">
                  <table className="w-full min-w-[760px] table-fixed border-collapse">
                    <colgroup>
                      <col className="min-w-[148px]" style={{ width: "22%" }} />
                      <col className="min-w-[56px]" style={{ width: "9%" }} />
                      <col className="min-w-[120px]" style={{ width: "26%" }} />
                      <col className="min-w-[200px]" style={{ width: "43%" }} />
                    </colgroup>
                    <thead className="sticky top-0 z-[1] bg-background/95 backdrop-blur-sm border-b border-border text-[9px] uppercase tracking-wider text-muted-foreground">
                      <tr>
                        <th className="py-1.5 pr-2 text-left font-medium align-bottom">时间</th>
                        <th className="py-1.5 px-1 text-left font-medium align-bottom">级别</th>
                        <th className="py-1.5 px-1 text-left font-medium align-bottom">类型</th>
                        <th className="py-1.5 pl-2 text-left font-medium align-bottom">详情</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border/50">
                      {filtered.map((e) => (
                        <tr key={e.seq} className="align-top hover:bg-accent/20 transition-colors">
                          <td className="py-1.5 pr-2 text-muted-foreground whitespace-nowrap tabular-nums">
                            {formatLogTsLocal(e.ts)}
                          </td>
                          <td className={cn("py-1.5 px-1 uppercase font-medium whitespace-nowrap", severityClass(e.severity))}>
                            {e.severity}
                          </td>
                          <td className="py-1.5 px-1 min-w-0 max-w-0">
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <span className="block truncate text-brand/90 cursor-default">{e.type}</span>
                              </TooltipTrigger>
                              <TooltipContent side="top" className="max-w-md break-all font-mono text-[11px]">
                                {e.type}
                              </TooltipContent>
                            </Tooltip>
                          </td>
                          <td className="py-1.5 pl-2 min-w-0 max-w-0">
                            <pre className="font-mono text-[9px] text-muted-foreground/95 whitespace-pre-wrap break-all max-h-20 overflow-y-auto overflow-x-auto rounded border border-border/40 bg-muted/20 px-1.5 py-1">
                              {JSON.stringify(e.detail)}
                            </pre>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </TooltipProvider>
        </CollapsibleContent>
      </div>
    </Collapsible>
  );
}
