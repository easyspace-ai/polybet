import { createFileRoute } from "@tanstack/react-router";
import { TopBar } from "@/components/TopBar";
import { useState, useEffect, useCallback } from "react";
import { RefreshCw, Trash2 } from "lucide-react";
import { getLogs, getLogErrors, postLogClear, type LogEntry } from "@/lib/api";

export const Route = createFileRoute("/logs")({ component: LogsPage });

const levelColors: Record<string, string> = {
  error: "text-destructive",
  warn: "text-warning",
  info: "text-brand",
};

const categoryColors: Record<string, string> = {
  交易: "text-success",
  风控: "text-warning",
  WebSocket: "text-info",
  市场同步: "text-brand",
};

function LogsPage() {
  const [tab, setTab] = useState<"all" | "err">("all");
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(false);

  const fetchLogs = useCallback(async () => {
    setLoading(true);
    try {
      const data = tab === "all" ? await getLogs() : await getLogErrors();
      setLogs(data.logs || []);
    } catch {
      setLogs([]);
    } finally {
      setLoading(false);
    }
  }, [tab]);

  useEffect(() => {
    fetchLogs();
  }, [fetchLogs]);

  const handleClear = async () => {
    if (!confirm("确定要清空所有日志吗？")) return;
    try {
      await postLogClear();
      setLogs([]);
    } catch {
      // ignore
    }
  };

  const handleRefresh = () => {
    fetchLogs();
  };

  return (
    <>
      <TopBar
        title="日志"
        subtitle={
          <>
            <span className="flex items-center gap-1.5"><span className="size-1.5 rounded-full bg-success animate-breathe" />服务运行中</span>
            <span className="text-border">·</span>
            <span>WebSocket: 1 连接</span>
            <span className="text-border">·</span>
            <span>{new Date().toLocaleString("zh-CN")}</span>
          </>
        }
        actions={
          <button onClick={handleClear} className="h-8 px-3 text-[12px] rounded-md bg-destructive text-destructive-foreground hover:opacity-90 transition flex items-center gap-1.5 font-medium">
            <Trash2 className="size-3.5" /> 清空
          </button>
        }
      />

      <div className="p-6 space-y-4 animate-slide-up">
        <div className="flex items-center justify-between">
          <div className="flex gap-1 border-b border-border w-full">
            {[
              { id: "all", label: `所有日志 (${logs.length})` },
              { id: "err", label: `报错日志 (${logs.filter(l => l.level === "error" || l.level === "warn").length})` },
            ].map((t) => (
              <button
                key={t.id}
                onClick={() => setTab(t.id as "all" | "err")}
                className={`px-4 py-2 text-[12px] -mb-px border-b-2 transition ${tab === t.id ? "border-brand text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"}`}
              >
                {t.label}
              </button>
            ))}
            <div className="ml-auto flex items-center gap-2 pb-1">
              <button onClick={handleRefresh} className="h-7 px-2.5 text-[11px] rounded border border-border hover:bg-accent transition flex items-center gap-1">
                <RefreshCw className={`size-3 ${loading ? "animate-spin" : ""}`} /> 刷新
              </button>
            </div>
          </div>
        </div>

        <div className="surface-elevated rounded-xl border border-border p-4 font-mono text-[11.5px] max-h-[70vh] overflow-y-auto scrollbar-thin">
          {loading ? (
            <p className="text-center text-muted-foreground py-12">加载中...</p>
          ) : logs.length === 0 ? (
            <p className="text-center text-muted-foreground py-12">暂无日志</p>
          ) : (
            logs.map((l, i) => (
              <div key={i} className="flex items-start gap-3 py-1 px-2 -mx-2 rounded hover:bg-accent/30 transition-colors">
                <span className="text-muted-foreground shrink-0">{l.time}</span>
                <span className={`shrink-0 ${levelColors[l.level] || "text-foreground/70"}`}>[{l.level}]</span>
                <span className={`shrink-0 ${categoryColors[l.category] || "text-warning"}`}>[{l.category}]</span>
                <span className="text-foreground/70">{l.message}</span>
              </div>
            ))
          )}
        </div>
      </div>
    </>
  );
}
