import { createFileRoute } from "@tanstack/react-router";
import { TopBar } from "@/components/TopBar";
import { useState } from "react";
import { RefreshCw, Trash2 } from "lucide-react";

export const Route = createFileRoute("/logs")({ component: LogsPage });

const allLogs = Array.from({ length: 38 }).map((_, i) => {
  const base = new Date(2026, 4, 12, 2, 53 - i, 26);
  const types = [
    { tag: "WebSocket", txt: `Dashboard 连接: 127.0.0.1:${62996 - i * 31}`, lvl: "info" },
    { tag: "市场同步", txt: "同步完成，共 16 个市场", lvl: "info" },
  ];
  const t = types[i % 2];
  return {
    time: base.toISOString().slice(0, 19).replace("T", " "),
    lvl: t.lvl,
    tag: t.tag,
    txt: t.txt,
  };
});

function LogsPage() {
  const [tab, setTab] = useState<"all" | "err">("all");
  const filtered = tab === "all" ? allLogs : [];
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
            <span>2026-05-12 02:53:45</span>
          </>
        }
        actions={
          <button className="h-8 px-3 text-[12px] rounded-md bg-destructive text-destructive-foreground hover:opacity-90 transition flex items-center gap-1.5 font-medium">
            <RefreshCw className="size-3.5" /> 重启服务
          </button>
        }
      />

      <div className="p-6 space-y-4 animate-slide-up">
        <div className="flex items-center justify-between">
          <div className="flex gap-1 border-b border-border w-full">
            {[
              { id: "all", label: `所有日志 (${allLogs.length})` },
              { id: "err", label: "报错日志 (0)" },
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
              <button className="h-7 px-2.5 text-[11px] rounded border border-border hover:bg-accent transition flex items-center gap-1">
                <RefreshCw className="size-3" /> 刷新
              </button>
              <button className="h-7 px-2.5 text-[11px] rounded border border-border hover:bg-accent transition flex items-center gap-1">
                <Trash2 className="size-3" /> 清空
              </button>
            </div>
          </div>
        </div>

        <div className="surface-elevated rounded-xl border border-border p-4 font-mono text-[11.5px] max-h-[70vh] overflow-y-auto scrollbar-thin">
          {filtered.length === 0 ? (
            <p className="text-center text-muted-foreground py-12">暂无日志</p>
          ) : (
            filtered.map((l, i) => (
              <div key={i} className="flex items-start gap-3 py-1 px-2 -mx-2 rounded hover:bg-accent/30 transition-colors">
                <span className="text-muted-foreground shrink-0">{l.time}</span>
                <span className="text-brand shrink-0">[{l.lvl}]</span>
                <span className="text-warning shrink-0">{l.tag}</span>
                <span className="text-foreground/70">{l.txt}</span>
              </div>
            ))
          )}
        </div>
      </div>
    </>
  );
}
