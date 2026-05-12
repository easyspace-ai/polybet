import { createFileRoute } from "@tanstack/react-router";
import { PageHeader, StatusDot } from "@/components/app-shell";
import { RotateCw, Trash2 } from "lucide-react";
import { useEffect, useState, useRef } from "react";
import { getLogs, getErrorLogs, clearLogs, getStatus, restartServer } from "@/lib/api";
import { cn } from "@/lib/utils";

declare global {
  interface Window {
    desktopAPI?: {
      restartPolybetSidecar: () => Promise<{ ok: true } | { ok: false; error: string }>;
    };
  }
}

const isElectron =
  typeof navigator !== "undefined" && navigator.userAgent.includes("Electron");

export const Route = createFileRoute("/logs")({
  component: LogsPage,
});

function LogsPage() {
  const [logs, setLogs] = useState<{ time: string; level: string; category: string; message: string }[]>([]);
  const [errors, setErrors] = useState<{ time: string; level: string; category: string; message: string }[]>([]);
  const [activeTab, setActiveTab] = useState<"all" | "errors">("all");
  const [status, setStatus] = useState<{ initStatus?: { complete: boolean }; wsClients: number; serverTime: string } | null>(null);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const logContainerRef = useRef<HTMLDivElement>(null);

  const fetchLogs = async () => {
    try {
      if (activeTab === "errors") {
        const data = await getErrorLogs();
        setErrors(data.logs || []);
      } else {
        const data = await getLogs();
        setLogs(data.logs || []);
      }
    } catch (err) {
      console.error("Failed to fetch logs:", err);
    }
  };

  const fetchStatus = async () => {
    try {
      const data = await getStatus();
      setStatus(data);
    } catch (err) {
      console.error("Failed to fetch status:", err);
    }
  };

  const handleClearLogs = async () => {
    if (!confirm("确定要清空所有日志吗？")) return;
    try {
      await clearLogs();
      setLogs([]);
      setErrors([]);
    } catch (err) {
      console.error("Failed to clear logs:", err);
    }
  };

  useEffect(() => {
    fetchLogs();
    fetchStatus();
    const interval = setInterval(() => {
      fetchLogs();
      fetchStatus();
    }, 3000);
    return () => clearInterval(interval);
  }, [activeTab]);

  useEffect(() => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = 0;
    }
  }, [logs, errors]);

  const handleRestart = async () => {
    if (!confirm("确定要重启服务端吗？")) return;
    setIsRefreshing(true);
    try {
      if (isElectron && window.desktopAPI?.restartPolybetSidecar) {
        const result = await window.desktopAPI.restartPolybetSidecar();
        if (!result.ok) {
          alert("重启失败: " + result.error);
          setIsRefreshing(false);
          return;
        }
      } else {
        await restartServer();
      }
    } catch {
      setIsRefreshing(false);
    }
  };

  const currentLogs = activeTab === "errors" ? [...errors].reverse() : [...logs].reverse();

  const getLevelColor = (level: string) => {
    switch (level) {
      case "error":
        return "text-down";
      case "warn":
        return "text-warning";
      case "info":
        return "text-info";
      default:
        return "text-muted-foreground";
    }
  };

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <PageHeader
        left={
          <>
            <div className="flex items-center gap-2">
              <StatusDot tone={status?.initStatus?.complete ? "up" : "warning"} />
              <span className="font-mono text-xs text-muted-foreground">服务:</span>
              <span className={cn("font-medium", status?.initStatus?.complete ? "text-up" : "text-warning")}>
                {status?.initStatus?.complete ? "运行中" : "初始化中"}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <StatusDot tone={status?.wsClients ? "up" : "muted"} />
              <span className="font-mono text-xs text-muted-foreground">WebSocket:</span>
              <span className="font-mono">{status?.wsClients || 0} 连接</span>
            </div>
            <span className="font-mono text-[11px] text-muted-foreground">{status?.serverTime}</span>
          </>
        }
        right={
          <button
            onClick={handleRestart}
            disabled={isRefreshing}
            className="flex items-center gap-1.5 rounded bg-down/15 text-down border border-down/40 px-3 py-1.5 text-xs font-bold uppercase tracking-wider hover:bg-down/25 disabled:opacity-50"
          >
            <RotateCw className={cn("size-3.5", isRefreshing && "animate-spin")} />
            {isRefreshing ? "重启中..." : "重启服务"}
          </button>
        }
      />

      <div className="flex-1 flex flex-col min-h-0">
        <div className="border-b border-border px-6 flex items-center justify-between">
          <div className="flex">
            <button
              onClick={() => setActiveTab("all")}
              className={cn(
                "px-4 py-3 text-xs font-semibold border-b-2 transition-colors",
                activeTab === "all"
                  ? "border-primary text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground"
              )}
            >
              所有日志 <span className="font-mono text-muted-foreground">({logs.length})</span>
            </button>
            <button
              onClick={() => setActiveTab("errors")}
              className={cn(
                "px-4 py-3 text-xs font-semibold border-b-2 transition-colors",
                activeTab === "errors"
                  ? "border-down text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground"
              )}
            >
              报错日志 <span className="font-mono">({errors.length})</span>
            </button>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={fetchLogs}
              className="text-xs text-muted-foreground hover:text-foreground px-2 py-1 transition-colors"
            >
              刷新
            </button>
            <button
              onClick={handleClearLogs}
              className="text-xs text-muted-foreground hover:text-down px-2 py-1 inline-flex items-center gap-1 transition-colors"
            >
              <Trash2 className="size-3" /> 清空
            </button>
          </div>
        </div>

        <div
          ref={logContainerRef}
          className="flex-1 overflow-y-auto bg-background font-mono text-[11px]"
        >
          {currentLogs.length === 0 ? (
            <div className="text-muted-foreground text-center py-8">暂无日志</div>
          ) : (
            currentLogs.map((log, i) => (
              <div
                key={i}
                className="px-6 py-1.5 hover:bg-surface/40 flex gap-3 border-b border-border/30"
              >
                <span className="text-muted-foreground tabular-nums shrink-0">{log.time}</span>
                <span className={cn("shrink-0", getLevelColor(log.level))}>[{log.level}]</span>
                <span className="shrink-0 text-info">{log.category}</span>
                <span className="text-foreground/80 break-all">{log.message}</span>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
