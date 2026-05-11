import { useEffect, useState, useRef } from "react";

declare global {
  interface Window {
    desktopAPI?: {
      restartPolybetSidecar: () => Promise<{ ok: true } | { ok: false; error: string }>;
    };
  }
}

const isElectron =
  typeof navigator !== "undefined" && navigator.userAgent.includes("Electron");

interface LogEntry {
  time: string;
  level: string;
  category: string;
  message: string;
}

interface Status {
  initStatus?: {
    complete: boolean;
  };
  wsClients: number;
  serverTime: string;
}

export function LogPage() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [errors, setErrors] = useState<LogEntry[]>([]);
  const [activeTab, setActiveTab] = useState<"all" | "errors">("all");
  const [status, setStatus] = useState<Status | null>(null);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const logContainerRef = useRef<HTMLDivElement>(null);

  const fetchLogs = async () => {
    try {
      const endpoint = activeTab === "errors" ? "/api/logs/errors" : "/api/logs";
      const res = await fetch(endpoint);
      const data = await res.json();
      if (activeTab === "errors") {
        setErrors(data.logs || []);
      } else {
        setLogs(data.logs || []);
      }
    } catch (err) {
      console.error("Failed to fetch logs:", err);
    }
  };

  const fetchStatus = async () => {
    try {
      const res = await fetch("/api/status");
      const data = await res.json();
      setStatus(data);
    } catch (err) {
      console.error("Failed to fetch status:", err);
    }
  };

  const clearLogs = async () => {
    if (!confirm("确定要清空所有日志吗？")) return;
    try {
      await fetch("/api/logs/clear", { method: "POST" });
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
        await fetch("/api/restart", { method: "POST" });
      }
    } catch {
      setIsRefreshing(false);
    }
  };

  const currentLogs = activeTab === "errors" ? [...errors].reverse() : [...logs].reverse();

  const getLevelColor = (level: string) => {
    switch (level) {
      case "error": return "text-red-500";
      case "warn": return "text-yellow-500";
      case "info": return "text-blue-500";
      default: return "text-tm-tx-dim";
    }
  };

  return (
    <div className="min-h-screen bg-tm-bg flex flex-col">
      {/* Status Bar */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-tm-bd bg-tm-bg-el">
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <div
              className={`w-2 h-2 rounded-full ${status?.initStatus?.complete ? "bg-green-500" : "bg-yellow-500"}`}
            />
            <span className="font-mono text-[11px] text-tm-tx">
              服务: {status?.initStatus?.complete ? "运行中" : "初始化中"}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <div className={`w-2 h-2 rounded-full ${status?.wsClients ? "bg-green-500" : "bg-gray-500"}`} />
            <span className="font-mono text-[11px] text-tm-tx">
              WebSocket: {status?.wsClients || 0} 连接
            </span>
          </div>
          <div className="font-mono text-[10px] text-tm-tx-dim">
            {status?.serverTime}
          </div>
        </div>
        <button
          onClick={handleRestart}
          disabled={isRefreshing}
          className="font-mono text-[10px] px-3 py-1.5 rounded bg-red-600 hover:bg-red-700 text-white disabled:opacity-50"
        >
          {isRefreshing ? "重启中..." : "重启服务"}
        </button>
      </div>

      {/* Tabs */}
      <div className="flex border-b border-tm-bd">
        <button
          onClick={() => setActiveTab("all")}
          className={`px-4 py-2 font-mono text-[11px] ${
            activeTab === "all" ? "border-b-2 border-blue-500 text-tm-tx" : "text-tm-tx-dim"
          }`}
        >
          所有日志 ({logs.length})
        </button>
        <button
          onClick={() => setActiveTab("errors")}
          className={`px-4 py-2 font-mono text-[11px] ${
            activeTab === "errors" ? "border-b-2 border-red-500 text-tm-tx" : "text-tm-tx-dim"
          }`}
        >
          报错日志 ({errors.length})
        </button>
        <button
          onClick={fetchLogs}
          className="ml-auto px-3 py-2 font-mono text-[10px] text-tm-tx-dim hover:text-tm-tx"
        >
          刷新
        </button>
        <button
          onClick={clearLogs}
          className="px-3 py-2 font-mono text-[10px] text-tm-tx-dim hover:text-tm-neg"
        >
          清空
        </button>
      </div>

      {/* Log List */}
      <div
        ref={logContainerRef}
        className="flex-1 overflow-auto p-4 font-mono text-[10px] space-y-1"
      >
        {currentLogs.length === 0 ? (
          <div className="text-tm-tx-dim text-center py-8">
            暂无日志
          </div>
        ) : (
          currentLogs.map((log, i) => (
            <div key={i} className="flex gap-2">
              <span className="text-tm-tx-dim whitespace-nowrap">{log.time}</span>
              <span className={`whitespace-nowrap ${getLevelColor(log.level)}`}>
                [{log.level}]
              </span>
              <span className="text-sky-400 whitespace-nowrap">{log.category}</span>
              <span className="text-tm-tx break-all">{log.message}</span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}