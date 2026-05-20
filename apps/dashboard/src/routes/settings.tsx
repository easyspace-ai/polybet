import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect, useCallback, Fragment } from "react";
import { TopBar } from "@/components/TopBar";
import { useTheme } from "@/lib/theme";
import { useUiPreferences, type UiFontScale, type UiTextContrast } from "@/lib/uiPreferences";
import { useConfig } from "@/hooks/useConfig";
import { useSoundSettings } from "@/hooks/useSoundSettings";
import { useAutoRefreshSettings } from "@/hooks/useAutoRefreshSettings";
import { putConfig, testTelegram, getSports, type GammaSport } from "@/lib/api";
import {
  DEFAULT_EVENT_CLASSIFICATION_TAGS,
  parseEventClassificationTags,
} from "@/lib/eventClassification";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover";
import { Command, CommandInput, CommandList, CommandEmpty, CommandGroup, CommandItem } from "@/components/ui/command";
import {
  Monitor,
  Globe,
  Send,
  Tag,
  DollarSign,
  Volume2,
  Info,
  Sun,
  Moon,
  Save,
  Trash2,
  RefreshCw,
  Download,
  CheckCircle,
  ChevronsUpDown,
  Radio,
  Scale,
} from "lucide-react";
import { applyWSConfigPatch } from "@/hooks/useWSConfig";

export const Route = createFileRoute("/settings")({ component: SettingsPage });

type SettingsTab = "general" | "connection" | "proxy" | "telegram" | "tags" | "prices" | "riskClose" | "sound" | "about";

const TABS: { id: SettingsTab; icon: typeof Monitor; title: string; desc: string }[] = [
  { id: "general", icon: Monitor, title: "通用", desc: "主题、机器人参数" },
  { id: "connection", icon: Radio, title: "连接与 WS", desc: "心跳、重连与兜底间隔" },
  { id: "proxy", icon: Globe, title: "代理", desc: "HTTP 代理配置" },
  { id: "telegram", icon: Send, title: "电报", desc: "Bot 与消息推送" },
  { id: "tags", icon: Tag, title: "分类", desc: "赛事标签管理" },
  { id: "prices", icon: DollarSign, title: "价格", desc: "资金区间与止损" },
  { id: "riskClose", icon: Scale, title: "止损平仓", desc: "FOK / FAK / 对冲执行" },
  { id: "sound", icon: Volume2, title: "声音", desc: "提醒与音效测试" },
  { id: "about", icon: Info, title: "关于", desc: "版本与更新" },
];

const KEY_DESCRIPTIONS: Record<string, string> = {
  maxTradeSize: "单笔交易金额上限。",
  slippageTolerance: "允许的最优盘口价与实际成交量加权均价之间的最大偏离。",
  pollingInterval: "市场同步（Gamma / 赛事列表）轮询间隔，单位：分钟。默认 60 表示每小时一次。",
  orderBookLevels: "投注单 / 交易面板中，实时推送的 Polymarket 盘口档位数。",
  polymarketFokBuyExtraTicks: "Polymarket FOK 买入：在最优卖价之上额外允许的 tick 档数。",
  polymarketFokSellExtraTicks: "Polymarket FOK 卖出：在最优买价之下额外放宽的 tick 档数。",
  riskCloseExecutionMode: "全局止损触发后的 CLOB 执行方式：整笔 FOK 卖、FAK 卖（可部分成交并重试）、或对手 token 上 FOK 买单对冲。",
  riskCloseFakWorstPrice: "FAK 卖出时的 worst-price 限价（0–1，如 0.01 表示最低约 1¢），会按市场 tick 截断。",
  riskHedgeBuySizing: "对冲模式：按持仓等值美元（notional）或按同份额估算买单预算（shares）。",
  riskHedgeAutoHidePosition: "对冲成功后是否自动「不再监控」原 YES 行（默认 true），避免止损引擎反复触发；链上原仓仍在。",
  minOpenRiskShares: "风控列表与 CLOB 余额对账：仅保留份额 ≥ 本值的持仓。",
};

const WS_KEY_DESCRIPTIONS: Record<string, string> = {
  wsClobPingIntervalSec: "Polymarket CLOB：text PING 间隔（秒）。",
  wsClobPongTimeoutSec: "无 PONG 时判定僵死并强制重连（秒）。",
  wsClobBackoffBaseSec: "CLOB 重连指数退避起点（秒）。",
  wsClobBackoffMaxSec: "CLOB 重连退避上限（秒）。",
  wsClobBackoffJitterPct: "CLOB 重连抖动比例（0–100）。",
  wsClobReconnectStableSec: "稳定连接后重置重试计数（秒）。",
  wsClobMaxReconnectAttempts: "最大重试次数，0=无限。",
  wsClobSleepThresholdSec: "休眠唤醒检测阈值（秒）。",
  wsHealthCheckIntervalSec: "服务端 WS 健康巡检周期（秒）。",
  wsBookStaleThresholdSec: "盘口缓存过期后 REST 兜底（秒）。",
  wsPositionsReconcileOpenSec: "有开仓时 Data API 对账间隔（秒）。",
  wsPositionsReconcileIdleSec: "无开仓时对账间隔（秒）。",
  wsRestTradesIntervalSec: "REST trades 同步间隔（秒）。",
  wsStoplossReconcileSec: "止损引擎订阅 reconcile（秒）。",
  wsDashPingIntervalSec: "Dashboard↔服务端 ping 间隔（秒）。",
  wsDashPongTimeoutSec: "无 pong 判 STALE（秒）。",
  wsDashBackoffBaseSec: "前端 WS 退避起点（秒）。",
  wsDashBackoffMaxSec: "前端 WS 退避上限（秒）。",
  wsDashBackoffJitterPct: "前端重连抖动（0–100）。",
  wsDashSleepThresholdSec: "Tab 休眠检测 rAF 阈值（秒）。",
  wsRiskPollIntervalSec: "风控页 REST 状态/仓位兜底轮询（秒）。",
  wsAutoReconnectOnDisconnect: "前端 WS 断开时自动重连（true/false）。",
  wsAutoRequestUpstreamReconnect: "检测到上游断开时自动 POST 重连（true/false）。",
};

const WS_KEY_GROUPS: { title: string; keys: string[] }[] = [
  {
    title: "Polymarket CLOB",
    keys: [
      "wsClobPingIntervalSec",
      "wsClobPongTimeoutSec",
      "wsClobBackoffBaseSec",
      "wsClobBackoffMaxSec",
      "wsClobBackoffJitterPct",
      "wsClobReconnectStableSec",
      "wsClobMaxReconnectAttempts",
      "wsClobSleepThresholdSec",
    ],
  },
  {
    title: "服务端巡检",
    keys: [
      "wsHealthCheckIntervalSec",
      "wsBookStaleThresholdSec",
      "wsPositionsReconcileOpenSec",
      "wsPositionsReconcileIdleSec",
      "wsRestTradesIntervalSec",
      "wsStoplossReconcileSec",
    ],
  },
  {
    title: "Dashboard 客户端",
    keys: [
      "wsDashPingIntervalSec",
      "wsDashPongTimeoutSec",
      "wsDashBackoffBaseSec",
      "wsDashBackoffMaxSec",
      "wsDashBackoffJitterPct",
      "wsDashSleepThresholdSec",
    ],
  },
  { title: "风控页兜底", keys: ["wsRiskPollIntervalSec", "wsAutoReconnectOnDisconnect", "wsAutoRequestUpstreamReconnect"] },
];

function SettingsPage() {
  const [active, setActive] = useState<SettingsTab>("general");
  const { rows, loading, error, refresh, save } = useConfig();

  return (
    <>
      <TopBar title="设置" subtitle={<span>{rows.length} 项配置</span>} />

      <div className="p-6 grid grid-cols-[260px_1fr] gap-6 animate-slide-up">
        <nav className="space-y-1">
          {TABS.map((t) => {
            const Icon = t.icon;
            const isActive = active === t.id;
            return (
              <button
                key={t.id}
                onClick={() => setActive(t.id)}
                className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-left transition ${
                  isActive
                    ? "bg-brand/10 text-foreground border border-brand/30"
                    : "hover:bg-accent border border-transparent"
                }`}
              >
                <Icon className={`size-4 ${isActive ? "text-brand" : "text-muted-foreground"}`} />
                <div className="flex flex-col leading-tight">
                  <span className="text-[12.5px] font-medium">{t.title}</span>
                  <span className="text-[10.5px] text-muted-foreground">{t.desc}</span>
                </div>
              </button>
            );
          })}
        </nav>

        <div className="space-y-5">
          {loading ? (
            <div className="text-muted-foreground">加载中...</div>
          ) : error ? (
            <div className="p-4 rounded-md border border-destructive/30 bg-destructive/10 text-destructive">
              {error}
            </div>
          ) : (
            <>
              {active === "general" && <GeneralTab rows={rows} onSave={save} />}
              {active === "connection" && <ConnectionTab rows={rows} onSave={save} />}
              {active === "proxy" && <ProxyTab rows={rows} onSave={save} />}
              {active === "telegram" && <TelegramTab rows={rows} onSave={save} />}
              {active === "tags" && <TagsTab rows={rows} onSave={save} />}
              {active === "prices" && <PricesTab rows={rows} onSave={save} />}
              {active === "riskClose" && <RiskCloseExecutionTab rows={rows} onSave={save} />}
              {active === "sound" && <SoundTab />}
              {active === "about" && <AboutTab />}
            </>
          )}
        </div>
      </div>
    </>
  );
}

function ConnectionTab({
  rows,
  onSave,
}: {
  rows: { key: string; value: string }[];
  onSave: (k: string, v: string) => Promise<void>;
}) {
  const [saving, setSaving] = useState<string | null>(null);
  const [edited, setEdited] = useState<Record<string, string>>({});

  async function handleSave(key: string) {
    const value = edited[key] ?? rows.find((r) => r.key === key)?.value ?? "";
    setSaving(key);
    try {
      await onSave(key, value);
      applyWSConfigPatch(key, value);
      setEdited((prev) => {
        const next = { ...prev };
        delete next[key];
        return next;
      });
      toast.success("已保存", {
        description: key.startsWith("wsDash") ? "Dashboard WS 参数已立即应用" : "CLOB 参数将在下次重连时生效",
      });
    } catch (err) {
      toast.error("保存失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setSaving(null);
    }
  }

  return (
    <div className="space-y-6">
      {WS_KEY_GROUPS.map((group) => (
        <section key={group.title} className="surface rounded-xl border border-border p-5">
          <h3 className="text-[13px] font-semibold mb-4">{group.title}</h3>
          <div className="space-y-2.5">
            {group.keys.map((key) => {
              const row = rows.find((r) => r.key === key);
              const value = edited[key] ?? row?.value ?? "";
              const isDirty = key in edited && edited[key] !== row?.value;
              return (
                <div key={key} className="rounded-lg border border-border p-4 flex items-start gap-4">
                  <div className="flex-1 min-w-0">
                    <p className="font-mono text-[12px] font-medium">{key}</p>
                    <p className="text-[11px] text-muted-foreground mt-1">{WS_KEY_DESCRIPTIONS[key]}</p>
                  </div>
                  <input
                    type="text"
                    value={value}
                    onChange={(e) => setEdited((prev) => ({ ...prev, [key]: e.target.value }))}
                    className="w-28 h-8 px-2 rounded-md border border-border bg-background font-mono text-[12px]"
                  />
                  <button
                    type="button"
                    disabled={!isDirty || saving === key}
                    onClick={() => void handleSave(key)}
                    className="h-8 px-3 rounded-md border border-border hover:bg-accent text-[11px] disabled:opacity-40"
                  >
                    {saving === key ? "…" : "保存"}
                  </button>
                </div>
              );
            })}
          </div>
        </section>
      ))}
    </div>
  );
}

function GeneralTab({
  rows,
  onSave,
}: {
  rows: { key: string; value: string }[];
  onSave: (k: string, v: string) => Promise<void>;
}) {
  const { theme, setTheme } = useTheme();
  const [saving, setSaving] = useState<string | null>(null);
  const [edited, setEdited] = useState<Record<string, string>>({});

  const generalRows = rows.filter(
    (r) =>
      !r.key.startsWith("ws") &&
      ![
        "httpPlatformProxyUrl",
        "telegramBotToken",
        "telegramAuthorizedChatId",
        "eventClassificationTags",
        "priceStopLossRanges",
      ].includes(r.key),
  );

  async function handleSave(key: string) {
    const value = edited[key] ?? rows.find((r) => r.key === key)?.value ?? "";
    setSaving(key);
    try {
      await onSave(key, value);
      setEdited((prev) => {
        const next = { ...prev };
        delete next[key];
        return next;
      });
      toast.success("已保存", { description: key });
    } catch (err) {
      toast.error("保存失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setSaving(null);
    }
  }

  return (
    <div className="space-y-5">
      <ThemeCard theme={theme} setTheme={setTheme} />
      <TypographyPreferencesCard />
      <AutoRefreshCard />

      <section className="surface rounded-xl border border-border p-5">
        <div className="flex items-start gap-3">
          <div className="size-8 rounded-md bg-accent flex items-center justify-center">
            <Monitor className="size-4 text-muted-foreground" />
          </div>
          <div>
            <p className="text-[13px] font-semibold">配置文件</p>
            <p className="text-[11.5px] text-muted-foreground mt-1">
              服务端将机器人参数与{" "}
              <code className="px-1 py-0.5 rounded bg-accent text-foreground font-mono text-[11px]">
                ~/.polybet/bot-settings.json
              </code>{" "}
              同步。
            </p>
          </div>
        </div>
      </section>

      <section>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-[12.5px] font-semibold flex items-center gap-2">机器人参数</h3>
          <span className="text-[10.5px] text-muted-foreground font-mono">
            {generalRows.length} 项
          </span>
        </div>
        <div className="space-y-2.5">
          {generalRows.map((row) => {
            const isDirty = row.key in edited && edited[row.key] !== row.value;
            const isSaving = saving === row.key;
            return (
              <div
                key={row.key}
                className="surface rounded-lg border border-border p-4 flex items-start gap-4 hover:border-brand/30 transition"
              >
                <div className="flex-1 min-w-0">
                  <p className="font-mono text-[12px] font-medium">{row.key}</p>
                  {KEY_DESCRIPTIONS[row.key] && (
                    <p className="text-[11px] text-muted-foreground mt-1 line-clamp-2">
                      {KEY_DESCRIPTIONS[row.key]}
                    </p>
                  )}
                </div>
                <input
                  value={edited[row.key] ?? row.value}
                  onChange={(e) => setEdited((prev) => ({ ...prev, [row.key]: e.target.value }))}
                  className="h-8 w-28 px-2 num text-[12px] rounded-md border border-border bg-background focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/30 transition text-right"
                />
                <button
                  onClick={() => handleSave(row.key)}
                  disabled={!isDirty || isSaving}
                  className="h-8 px-3 rounded-md text-[11.5px] font-medium transition flex items-center gap-1 disabled:opacity-50 disabled:cursor-not-allowed bg-brand/10 text-brand hover:bg-brand/20"
                >
                  <Save className="size-3" /> {isSaving ? "..." : "保存"}
                </button>
              </div>
            );
          })}
        </div>
      </section>
    </div>
  );
}

function AutoRefreshCard() {
  const { settings, setEnabled, setIntervalMinutes } = useAutoRefreshSettings();
  const [draftMinutes, setDraftMinutes] = useState(String(settings.intervalMinutes));

  useEffect(() => {
    setDraftMinutes(String(settings.intervalMinutes));
  }, [settings.intervalMinutes]);

  function commitInterval(raw: string) {
    const parsed = Number(raw);
    if (!Number.isFinite(parsed)) {
      setDraftMinutes(String(settings.intervalMinutes));
      return;
    }
    setIntervalMinutes(parsed);
  }

  return (
    <section className="surface rounded-xl border border-border p-5">
      <div className="flex items-start gap-3 mb-4">
        <div className="size-8 rounded-md bg-accent flex items-center justify-center">
          <RefreshCw className="size-4 text-muted-foreground" />
        </div>
        <div>
          <p className="text-[13px] font-semibold">页面自动刷新</p>
          <p className="text-[11.5px] text-muted-foreground mt-1">
            定时整页刷新 Dashboard，避免长时间运行后数据或连接状态陈旧
          </p>
        </div>
      </div>

      <div className="space-y-4">
        <div className="flex items-center justify-between rounded-lg border border-border p-4">
          <div>
            <p className="text-[12px] font-medium">启用自动刷新</p>
            <p className="text-[11px] text-muted-foreground mt-0.5">关闭后不再定时刷新页面</p>
          </div>
          <button
            type="button"
            onClick={() => setEnabled(!settings.enabled)}
            className={cn(
              "relative w-11 h-6 rounded-full transition-colors",
              settings.enabled ? "bg-brand" : "bg-muted",
            )}
          >
            <span
              className={cn(
                "absolute top-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform",
                settings.enabled ? "left-[22px]" : "left-0.5",
              )}
            />
          </button>
        </div>

        <div className="flex items-center justify-between rounded-lg border border-border p-4">
          <div>
            <p className="text-[12px] font-medium">刷新间隔</p>
            <p className="text-[11px] text-muted-foreground mt-0.5">两次整页刷新之间的分钟数（1–1440）</p>
          </div>
          <div className="flex items-center gap-2">
            <input
              type="number"
              min={1}
              max={1440}
              step={1}
              disabled={!settings.enabled}
              value={draftMinutes}
              onChange={(e) => setDraftMinutes(e.target.value)}
              onBlur={() => commitInterval(draftMinutes)}
              onKeyDown={(e) => {
                if (e.key === "Enter") commitInterval(draftMinutes);
              }}
              className="h-8 w-20 px-2 num text-[12px] rounded-md border border-border bg-background focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/30 transition text-right disabled:opacity-50"
            />
            <span className="text-[12px] text-muted-foreground">分钟</span>
          </div>
        </div>
      </div>
    </section>
  );
}

function ThemeCard({
  theme,
  setTheme,
}: {
  theme: string;
  setTheme: (t: "light" | "dark") => void;
}) {
  return (
    <section className="surface rounded-xl border border-border p-5">
      <div className="flex items-start gap-3 mb-4">
        <div className="size-8 rounded-md bg-accent flex items-center justify-center">
          <Globe className="size-4 text-muted-foreground" />
        </div>
        <div>
          <p className="text-[13px] font-semibold">界面主题</p>
          <p className="text-[11.5px] text-muted-foreground mt-1">选择适合交易环境的配色方案</p>
        </div>
      </div>
      <div className="inline-flex p-1 bg-accent rounded-lg gap-1">
        <button
          onClick={() => setTheme("light")}
          className={`flex items-center gap-2 px-4 py-2 rounded-md text-[12px] font-medium transition ${theme === "light" ? "bg-brand text-brand-foreground shadow-sm" : "text-muted-foreground"}`}
        >
          <Sun className="size-3.5" /> 浅色
        </button>
        <button
          onClick={() => setTheme("dark")}
          className={`flex items-center gap-2 px-4 py-2 rounded-md text-[12px] font-medium transition ${theme === "dark" ? "bg-brand text-brand-foreground shadow-sm" : "text-muted-foreground"}`}
        >
          <Moon className="size-3.5" /> 深色
        </button>
      </div>
    </section>
  );
}

function TypographyPreferencesCard() {
  const { prefs, setPrefs } = useUiPreferences();

  return (
    <section className="surface rounded-xl border border-border p-5">
      <div className="flex items-start gap-3 mb-4">
        <div className="size-8 rounded-md bg-accent flex items-center justify-center">
          <Monitor className="size-4 text-muted-foreground" />
        </div>
        <div>
          <p className="text-[13px] font-semibold">字体与对比度</p>
          <p className="text-[11.5px] text-muted-foreground mt-1">
            调整全局字号与文字深浅，监控页等密集表格会同步生效
          </p>
        </div>
      </div>
      <div className="space-y-4">
        <div>
          <p className="text-[12px] font-medium mb-2">字号</p>
          <div className="inline-flex p-1 bg-accent rounded-lg gap-1">
            {(
              [
                { id: "compact" as UiFontScale, label: "紧凑" },
                { id: "normal" as UiFontScale, label: "标准" },
                { id: "comfortable" as UiFontScale, label: "偏大" },
              ] as const
            ).map((opt) => (
              <button
                key={opt.id}
                type="button"
                onClick={() => setPrefs({ fontScale: opt.id })}
                className={cn(
                  "px-3 py-1.5 rounded-md text-[12px] font-medium transition",
                  prefs.fontScale === opt.id
                    ? "bg-brand text-brand-foreground shadow-sm"
                    : "text-muted-foreground",
                )}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>
        <div>
          <p className="text-[12px] font-medium mb-2">文字对比度</p>
          <div className="inline-flex p-1 bg-accent rounded-lg gap-1">
            {(
              [
                { id: "normal" as UiTextContrast, label: "标准" },
                { id: "strong" as UiTextContrast, label: "更深" },
              ] as const
            ).map((opt) => (
              <button
                key={opt.id}
                type="button"
                onClick={() => setPrefs({ textContrast: opt.id })}
                className={cn(
                  "px-3 py-1.5 rounded-md text-[12px] font-medium transition",
                  prefs.textContrast === opt.id
                    ? "bg-brand text-brand-foreground shadow-sm"
                    : "text-muted-foreground",
                )}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}

function ProxyTab({
  rows,
  onSave,
}: {
  rows: { key: string; value: string }[];
  onSave: (k: string, v: string) => Promise<void>;
}) {
  const [proxyDraft, setProxyDraft] = useState(
    rows.find((r) => r.key === "httpPlatformProxyUrl")?.value ?? "",
  );
  const [saving, setSaving] = useState(false);

  async function handleSave() {
    setSaving(true);
    try {
      await onSave("httpPlatformProxyUrl", proxyDraft);
      toast.success("已保存", { description: "代理地址" });
    } catch (err) {
      toast.error("保存失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-5 space-y-4">
      <div>
        <div className="text-[13px] font-semibold">HTTP(S) 代理地址</div>
        <p className="mt-1 text-[11px] text-muted-foreground leading-[1.55]">
          非空时覆盖默认代理设置，经 CONNECT 转发 Polymarket 等出站请求。保存后立即生效。
        </p>
      </div>
      <input
        value={proxyDraft}
        onChange={(e) => setProxyDraft(e.target.value)}
        placeholder="https://user:pass@host:port 或留空使用默认值"
        className="w-full h-10 px-3 text-[13px] rounded-md border border-border bg-background focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/30 transition"
      />
      <button
        onClick={handleSave}
        disabled={
          saving || proxyDraft === (rows.find((r) => r.key === "httpPlatformProxyUrl")?.value ?? "")
        }
        className="w-full h-10 rounded-md text-[12px] font-semibold transition disabled:opacity-50 bg-brand text-brand-foreground hover:opacity-90"
      >
        {saving ? "保存中..." : "保存代理"}
      </button>
    </div>
  );
}

function TelegramTab({
  rows,
  onSave,
}: {
  rows: { key: string; value: string }[];
  onSave: (k: string, v: string) => Promise<void>;
}) {
  const [tokenDraft, setTokenDraft] = useState(
    rows.find((r) => r.key === "telegramBotToken")?.value ?? "",
  );
  const [chatDraft, setChatDraft] = useState(
    rows.find((r) => r.key === "telegramAuthorizedChatId")?.value ?? "",
  );
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);

  async function handleSave() {
    setSaving(true);
    try {
      await onSave("telegramBotToken", tokenDraft);
      await onSave("telegramAuthorizedChatId", chatDraft);
      toast.success("已保存", { description: "电报配置" });
    } catch (err) {
      toast.error("保存失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setSaving(false);
    }
  }

  async function handleTest() {
    setTesting(true);
    try {
      await testTelegram();
      toast.success("测试成功", { description: "测试消息已发送到 Telegram" });
    } catch (err) {
      toast.error("测试失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setTesting(false);
    }
  }

  return (
    <div className="space-y-4">
      <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-5 space-y-4">
        <p className="text-[11px] text-muted-foreground leading-[1.55]">
          对应 Bot Token 与 Authorized Chat ID。修改 Token 后需重启进程才能重连 Bot。
        </p>
        <div>
          <label className="text-[11px] font-semibold text-muted-foreground mb-1 block">
            Bot Token
          </label>
          <input
            type="password"
            value={tokenDraft}
            onChange={(e) => setTokenDraft(e.target.value)}
            className="w-full h-10 px-3 text-[13px] rounded-md border border-border bg-background focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/30 transition"
          />
        </div>
        <div>
          <label className="text-[11px] font-semibold text-muted-foreground mb-1 block">
            Authorized Chat ID
          </label>
          <input
            value={chatDraft}
            onChange={(e) => setChatDraft(e.target.value)}
            className="w-full h-10 px-3 text-[13px] rounded-md border border-border bg-background focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/30 transition"
          />
        </div>
        <button
          onClick={handleSave}
          disabled={saving}
          className="w-full h-10 rounded-md text-[12px] font-semibold transition bg-brand text-brand-foreground hover:opacity-90 disabled:opacity-50"
        >
          {saving ? "保存中..." : "保存电报配置"}
        </button>
        <button
          onClick={handleTest}
          disabled={testing || !tokenDraft || !chatDraft}
          className="w-full h-10 rounded-md text-[12px] font-semibold transition bg-sky-600 text-white hover:bg-sky-500 disabled:opacity-50"
        >
          {testing ? "发送中..." : "发送测试消息"}
        </button>
      </div>
    </div>
  );
}

function TagsTab({
  rows,
  onSave,
}: {
  rows: { key: string; value: string }[];
  onSave: (k: string, v: string) => Promise<void>;
}) {
  const [tags, setTags] = useState<string[]>(() =>
    parseEventClassificationTags(
      rows.find((r) => r.key === "eventClassificationTags")?.value ?? "",
    ),
  );
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [sports, setSports] = useState<GammaSport[]>([]);
  const [sportsLoading, setSportsLoading] = useState(false);
  const [sportsError, setSportsError] = useState("");

  const SPORTS_CACHE_KEY = "polybet_sports_cache";
  const SPORTS_CACHE_TTL = 60 * 60 * 1000;

  async function loadSports(force = false) {
    setSportsLoading(true);
    setSportsError("");
    try {
      if (!force) {
        const cached = localStorage.getItem(SPORTS_CACHE_KEY);
        if (cached) {
          try {
            const { data, ts } = JSON.parse(cached);
            if (Date.now() - ts < SPORTS_CACHE_TTL) {
              setSports(data);
              setSportsLoading(false);
              return;
            }
          } catch {}
        }
      }
      const data = await getSports();
      setSports(data);
      localStorage.setItem(SPORTS_CACHE_KEY, JSON.stringify({ data, ts: Date.now() }));
    } catch (err) {
      setSportsError(err instanceof Error ? err.message : "获取赛事列表失败");
      const cached = localStorage.getItem(SPORTS_CACHE_KEY);
      if (cached) {
        try {
          const { data } = JSON.parse(cached);
          setSports(data);
        } catch {}
      }
    } finally {
      setSportsLoading(false);
    }
  }

  useEffect(() => {
    loadSports();
  }, []);

  function removeTag(t: string) {
    setTags((prev) => prev.filter((x) => x !== t));
  }

  function toggleTag(slug: string) {
    setTags((prev) =>
      prev.includes(slug) ? prev.filter((x) => x !== slug) : [...prev, slug],
    );
  }

  async function handleSave() {
    setSaving(true);
    try {
      await onSave("eventClassificationTags", JSON.stringify(tags));
      toast.success("已保存", { description: "赛事分类" });
    } catch (err) {
      toast.error("保存失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setSaving(false);
    }
  }

  const sortedSports = [...sports]
    .filter((s) => s.sport)
    .sort((a, b) => a.sport.localeCompare(b.sport));

  return (
    <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-5 space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <div className="text-[13px] font-semibold">赛事分类</div>
          <p className="text-[11px] text-muted-foreground leading-[1.55] mt-0.5">
            从 Gamma API 选择关注的联赛，数据缓存 1 小时
          </p>
        </div>
        <button
          onClick={() => loadSports(true)}
          disabled={sportsLoading}
          className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md border border-border text-[11px] text-muted-foreground hover:text-foreground hover:border-brand transition disabled:opacity-50"
        >
          <RefreshCw className={`size-3 ${sportsLoading ? "animate-spin" : ""}`} />
          {sportsLoading ? "加载中..." : "刷新"}
        </button>
      </div>

      <div className="flex flex-wrap gap-2 min-h-8">
        {tags.length === 0 && (
          <span className="text-[11px] text-muted-foreground">尚未选择联赛</span>
        )}
        {tags.map((t) => (
          <span
            key={t}
            className="inline-flex items-center gap-1 rounded-full border border-sky-500/40 bg-sky-500/15 px-3 py-1 text-[11px] font-semibold text-sky-200"
          >
            {t.toUpperCase()}
            <button onClick={() => removeTag(t)} className="p-0.5 rounded hover:bg-sky-500/25">
              <Trash2 className="size-3" />
            </button>
          </span>
        ))}
      </div>

      {sportsError && (
        <p className="text-[11px] text-red-400">{sportsError}</p>
      )}

      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button
            role="combobox"
            aria-expanded={open}
            className="w-full flex items-center justify-between h-10 px-3 text-[12px] rounded-md border border-border bg-background hover:border-brand transition"
          >
            <span className={tags.length === 0 ? "text-muted-foreground" : ""}>
              {tags.length === 0
                ? "搜索并选择联赛..."
                : `已选 ${tags.length} 个联赛`}
            </span>
            <ChevronsUpDown className="size-3.5 text-muted-foreground shrink-0" />
          </button>
        </PopoverTrigger>
        <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
          <Command>
            <CommandInput placeholder="搜索联赛..." />
            <CommandList>
              <CommandEmpty>
                {sportsLoading ? "加载中..." : "未找到匹配的联赛"}
              </CommandEmpty>
              <CommandGroup>
                {sortedSports.map((s) => {
                  const selected = tags.includes(s.sport);
                  return (
                    <CommandItem
                      key={s.sport}
                      value={s.sport}
                      onSelect={() => {
                        toggleTag(s.sport);
                      }}
                    >
                      <div className="flex items-center justify-between w-full">
                        <span className="flex items-center gap-2">
                          <span
                            className={`size-4 rounded-sm border flex items-center justify-center transition ${
                              selected
                                ? "bg-sky-500 border-sky-500"
                                : "border-border"
                            }`}
                          >
                            {selected && (
                              <CheckCircle className="size-3 text-white" />
                            )}
                          </span>
                          <span className="text-[12px] font-medium">
                            {s.sport.toUpperCase()}
                          </span>
                        </span>
                        <span className="text-[10px] text-muted-foreground truncate max-w-[140px] ml-2">
                          series={s.series} tags={s.tags}
                        </span>
                      </div>
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>

      <button
        onClick={handleSave}
        disabled={
          saving ||
          JSON.stringify(tags) ===
            (rows.find((r) => r.key === "eventClassificationTags")?.value ?? "")
        }
        className="w-full h-10 rounded-md text-[12px] font-semibold transition bg-brand text-brand-foreground hover:opacity-90 disabled:opacity-50"
      >
        {saving ? "保存中..." : "保存赛事分类"}
      </button>
    </div>
  );
}

function RiskCloseExecutionTab({
  rows,
  onSave,
}: {
  rows: { key: string; value: string }[];
  onSave: (k: string, v: string) => Promise<void>;
}) {
  const row = (k: string, d: string) => (rows.find((r) => r.key === k)?.value ?? d).trim();

  const [execMode, setExecMode] = useState(row("riskCloseExecutionMode", "fok_sell"));
  const [worst, setWorst] = useState(row("riskCloseFakWorstPrice", "0.01"));
  const [hedgeSizing, setHedgeSizing] = useState(row("riskHedgeBuySizing", "notional"));
  const [autoHide, setAutoHide] = useState(row("riskHedgeAutoHidePosition", "true").toLowerCase() !== "false");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setExecMode(row("riskCloseExecutionMode", "fok_sell"));
    setWorst(row("riskCloseFakWorstPrice", "0.01"));
    setHedgeSizing(row("riskHedgeBuySizing", "notional"));
    setAutoHide(row("riskHedgeAutoHidePosition", "true").toLowerCase() !== "false");
  }, [rows]);

  async function handleSave() {
    setSaving(true);
    try {
      await onSave("riskCloseExecutionMode", execMode);
      await onSave("riskCloseFakWorstPrice", worst.trim() || "0.01");
      await onSave("riskHedgeBuySizing", hedgeSizing);
      await onSave("riskHedgeAutoHidePosition", autoHide ? "true" : "false");
      toast.success("已保存", { description: "止损平仓执行" });
    } catch (err) {
      toast.error("保存失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-5 space-y-5 max-w-xl">
      <div>
        <div className="text-[13px] font-semibold">止损平仓执行（全局）</div>
        <p className="text-[11px] text-muted-foreground mt-1 leading-[1.55]">
          自动止损、手动卖出与一键平仓子任务均读取此处配置。FAK 部分成交会保留持仓并自动重试；对冲不会卖出原 YES，仅买对手 outcome。
        </p>
      </div>

      <div className="space-y-2">
        <div className="text-[11px] font-medium text-foreground">执行方式</div>
        <label className="flex items-start gap-2 text-[12px] cursor-pointer">
          <input type="radio" name="riskCloseMode" checked={execMode === "fok_sell"} onChange={() => setExecMode("fok_sell")} className="mt-0.5" />
          <span>
            <span className="font-medium">FOK 卖出</span>
            <span className="block text-[10.5px] text-muted-foreground">整笔立刻成交否则取消（默认）。</span>
          </span>
        </label>
        <label className="flex items-start gap-2 text-[12px] cursor-pointer">
          <input type="radio" name="riskCloseMode" checked={execMode === "fak_sell"} onChange={() => setExecMode("fak_sell")} className="mt-0.5" />
          <span>
            <span className="font-medium">FAK 卖出</span>
            <span className="block text-[10.5px] text-muted-foreground">能成交多少算多少，余量取消；未平完会重试。</span>
          </span>
        </label>
        <label className="flex items-start gap-2 text-[12px] cursor-pointer">
          <input type="radio" name="riskCloseMode" checked={execMode === "hedge_fok_buy"} onChange={() => setExecMode("hedge_fok_buy")} className="mt-0.5" />
          <span>
            <span className="font-medium">反向 FOK 买单对冲</span>
            <span className="block text-[10.5px] text-muted-foreground">仅在二元市场（两个 clobTokenIds）可用；不关闭原 YES 记录。</span>
          </span>
        </label>
      </div>

      {execMode === "fak_sell" && (
        <div className="space-y-1">
          <label className="text-[11px] font-medium">FAK worst 价（0–1）</label>
          <input
            value={worst}
            onChange={(e) => setWorst(e.target.value)}
            className="w-full h-9 px-2 text-[12px] rounded border border-border bg-background"
          />
          <p className="text-[10px] text-muted-foreground">{KEY_DESCRIPTIONS.riskCloseFakWorstPrice}</p>
        </div>
      )}

      {execMode === "hedge_fok_buy" && (
        <div className="space-y-3">
          <div className="space-y-2">
            <div className="text-[11px] font-medium">对冲预算</div>
            <label className="flex items-center gap-2 text-[12px] cursor-pointer">
              <input type="radio" name="hedgeSizing" checked={hedgeSizing === "notional"} onChange={() => setHedgeSizing("notional")} />
              按持仓等值美元（max(bid,ask) 作 YES 标记价）
            </label>
            <label className="flex items-center gap-2 text-[12px] cursor-pointer">
              <input type="radio" name="hedgeSizing" checked={hedgeSizing === "shares"} onChange={() => setHedgeSizing("shares")} />
              按同份额 × 对手卖价上限估算 USDC
            </label>
          </div>
          <label className="flex items-center gap-2 text-[12px] cursor-pointer">
            <input type="checkbox" checked={autoHide} onChange={(e) => setAutoHide(e.target.checked)} />
            对冲成功后自动「不再监控」该 YES（推荐）
          </label>
          <p className="text-[10px] text-muted-foreground">{KEY_DESCRIPTIONS.riskHedgeAutoHidePosition}</p>
        </div>
      )}

      <button
        type="button"
        disabled={saving}
        onClick={() => void handleSave()}
        className="w-full h-10 rounded-md text-[12px] font-semibold transition bg-brand text-brand-foreground hover:opacity-90 disabled:opacity-50"
      >
        {saving ? "保存中..." : "保存止损平仓配置"}
      </button>
    </div>
  );
}

function PricesTab({
  rows,
  onSave,
}: {
  rows: { key: string; value: string }[];
  onSave: (k: string, v: string) => Promise<void>;
}) {
  const DEFAULT_PRICE_ROWS = [
    { id: "r1", name: "20-30¢", minCents: 20, maxCents: 30, fundPct: 17, stopLossPct: 20 },
    { id: "r2", name: "30-40¢", minCents: 30, maxCents: 40, fundPct: 17, stopLossPct: 20 },
    { id: "r3", name: "40-50¢", minCents: 40, maxCents: 50, fundPct: 17, stopLossPct: 20 },
    { id: "r4", name: "50-60¢", minCents: 50, maxCents: 60, fundPct: 17, stopLossPct: 20 },
    { id: "r5", name: "60-70¢", minCents: 60, maxCents: 70, fundPct: 16, stopLossPct: 20 },
    { id: "r6", name: "70-80¢", minCents: 70, maxCents: 80, fundPct: 16, stopLossPct: 20 },
  ];

  const [priceRows, setPriceRows] = useState(() => {
    const raw = rows.find((r) => r.key === "priceStopLossRanges")?.value ?? "";
    if (!raw.trim()) return DEFAULT_PRICE_ROWS;
    try {
      const p = JSON.parse(raw);
      if (!Array.isArray(p) || p.length === 0) return DEFAULT_PRICE_ROWS;
      return p;
    } catch {
      return DEFAULT_PRICE_ROWS;
    }
  });
  const [saving, setSaving] = useState(false);

  const fundSum = priceRows.reduce((a, r) => a + (Number.isFinite(r.fundPct) ? r.fundPct : 0), 0);

  async function handleSave() {
    setSaving(true);
    try {
      await onSave("priceStopLossRanges", JSON.stringify(priceRows));
      toast.success("已保存", { description: "价格区间" });
    } catch (err) {
      toast.error("保存失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-5 space-y-4">
      <div className="text-[13px] font-semibold">价格区间</div>
      <p className="text-[11px] text-muted-foreground leading-[1.55]">
        按 YES 价格（美分）区间配置资金占比与默认止损比例。
      </p>
      <p className="text-[11px] text-muted-foreground">
        资金占比合计：{fundSum.toFixed(0)}%（建议接近 100%）
      </p>

      <div className="overflow-x-auto">
        <div
          className="grid gap-2 text-[10px] text-muted-foreground min-w-[500px]"
          style={{ gridTemplateColumns: "1fr 60px 60px 70px 70px 30px" }}
        >
          <span>名称</span>
          <span>下限 ¢</span>
          <span>上限 ¢</span>
          <span>资金 %</span>
          <span>止损 %</span>
          <span />
          {priceRows.map((r, i) => (
            <div key={r.id} className="contents">
              <input
                value={r.name}
                onChange={(e) =>
                  setPriceRows((p) =>
                    p.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)),
                  )
                }
                className="h-8 px-2 text-[11px] rounded border border-border bg-background"
              />
              <input
                type="number"
                value={r.minCents}
                onChange={(e) =>
                  setPriceRows((p) =>
                    p.map((x, j) =>
                      j === i ? { ...x, minCents: Number(e.target.value) || 0 } : x,
                    ),
                  )
                }
                className="h-8 px-2 text-[11px] rounded border border-border bg-background"
              />
              <input
                type="number"
                value={r.maxCents}
                onChange={(e) =>
                  setPriceRows((p) =>
                    p.map((x, j) =>
                      j === i ? { ...x, maxCents: Number(e.target.value) || 0 } : x,
                    ),
                  )
                }
                className="h-8 px-2 text-[11px] rounded border border-border bg-background"
              />
              <input
                type="number"
                value={r.fundPct}
                onChange={(e) =>
                  setPriceRows((p) =>
                    p.map((x, j) => (j === i ? { ...x, fundPct: Number(e.target.value) || 0 } : x)),
                  )
                }
                className="h-8 px-2 text-[11px] rounded border border-border bg-background"
              />
              <input
                type="number"
                value={r.stopLossPct}
                onChange={(e) =>
                  setPriceRows((p) =>
                    p.map((x, j) =>
                      j === i ? { ...x, stopLossPct: Number(e.target.value) || 0 } : x,
                    ),
                  )
                }
                className="h-8 px-2 text-[11px] rounded border border-border bg-background"
              />
              <button
                onClick={() => setPriceRows((p) => p.filter((_, j) => j !== i))}
                className="text-danger hover:bg-destructive/10 rounded"
              >
                <Trash2 className="size-4" />
              </button>
            </div>
          ))}
        </div>
      </div>
      <button
        onClick={() =>
          setPriceRows((p) => [
            ...p,
            {
              id: `r${Date.now()}`,
              name: "新区间",
              minCents: 0,
              maxCents: 10,
              fundPct: 0,
              stopLossPct: 15,
            },
          ])
        }
        className="px-3 h-8 rounded border border-border text-[11px] hover:bg-accent"
      >
        + 添加区间
      </button>
      <button
        onClick={handleSave}
        disabled={saving}
        className="w-full h-10 rounded-md text-[12px] font-semibold transition bg-brand text-brand-foreground hover:opacity-90 disabled:opacity-50"
      >
        {saving ? "保存中..." : "保存价格区间"}
      </button>
    </div>
  );
}

function SoundTab() {
  const [playing, setPlaying] = useState<string | null>(null);
  const { settings, setEnabled, setBuyEnabled, setSellEnabled, setAlertEnabled } =
    useSoundSettings();

  const isElectron =
    typeof window !== "undefined" &&
    typeof navigator !== "undefined" &&
    navigator.userAgent.includes("Electron");

  const playSound = async (soundName: string) => {
    if (typeof window === "undefined") return;
    const api = (
      window as unknown as {
        desktopAPI?: {
          playSound?: (s: string) => Promise<{ ok: true } | { ok: false; error: string }>;
        };
      }
    ).desktopAPI;
    if (!isElectron || !api?.playSound) {
      toast.error("仅在桌面客户端中支持声音播放");
      return;
    }

    setPlaying(soundName);
    try {
      const result = await api.playSound(soundName);
      if (result.ok) {
        toast.success(`已播放 ${soundName} 声音`);
      } else {
        toast.error(result.error);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "未知错误");
    } finally {
      setPlaying(null);
    }
  };

  const soundItems: { key: string; label: string; desc: string }[] = [
    { key: "buy", label: "开单", desc: "买入成交提示音" },
    { key: "sell", label: "平仓", desc: "卖出成交提示音" },
    { key: "alert", label: "系统错误", desc: "异常情况告警音" },
  ];

  return (
    <div>
      <div className="mb-4 font-mono text-[12px] font-semibold tracking-[0.2em] text-muted-foreground">
        声音提醒
      </div>

      {!isElectron ? (
        <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-5">
          <div className="text-[12px] text-muted-foreground">
            声音提醒功能需要桌面客户端支持。当前为浏览器环境，可使用 Web Audio 播放测试音。
          </div>
        </div>
      ) : (
        <div className="space-y-4">
          <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-4">
            <div className="flex items-center justify-between">
              <div>
                <div className="text-[13px] font-semibold">声音提醒</div>
                <div className="text-[11.5px] text-muted-foreground">开启/关闭所有声音提醒</div>
              </div>
              <button
                type="button"
                onClick={() => setEnabled(!settings.enabled)}
                className={cn(
                  "relative w-11 h-6 rounded-full transition-colors",
                  settings.enabled ? "bg-brand" : "bg-muted",
                )}
              >
                <span
                  className={cn(
                    "absolute top-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform",
                    settings.enabled ? "left-[22px]" : "left-0.5",
                  )}
                />
              </button>
            </div>
          </div>

          <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-4 space-y-3">
            <div className="text-[11px] font-semibold">分类开关</div>

            <div className="flex items-center justify-between">
              <div>
                <div className="text-[11px] text-foreground">开单提醒</div>
                <div className="text-[9px] text-muted-foreground">买入成交提示音</div>
              </div>
              <button
                type="button"
                onClick={() => setBuyEnabled(!settings.buyEnabled)}
                disabled={!settings.enabled}
                className={cn(
                  "relative w-9 h-5 rounded-full transition-colors",
                  settings.buyEnabled && settings.enabled ? "bg-brand" : "bg-muted",
                  !settings.enabled && "opacity-50",
                )}
              >
                <span
                  className={cn(
                    "absolute top-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform",
                    settings.buyEnabled && settings.enabled ? "left-[18px]" : "left-0.5",
                  )}
                />
              </button>
            </div>

            <div className="flex items-center justify-between">
              <div>
                <div className="text-[11px] text-foreground">平仓提醒</div>
                <div className="text-[9px] text-muted-foreground">卖出/止损成交提示音</div>
              </div>
              <button
                type="button"
                onClick={() => setSellEnabled(!settings.sellEnabled)}
                disabled={!settings.enabled}
                className={cn(
                  "relative w-9 h-5 rounded-full transition-colors",
                  settings.sellEnabled && settings.enabled ? "bg-brand" : "bg-muted",
                  !settings.enabled && "opacity-50",
                )}
              >
                <span
                  className={cn(
                    "absolute top-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform",
                    settings.sellEnabled && settings.enabled ? "left-[18px]" : "left-0.5",
                  )}
                />
              </button>
            </div>

            <div className="flex items-center justify-between">
              <div>
                <div className="text-[11px] text-foreground">系统提醒</div>
                <div className="text-[9px] text-muted-foreground">WebSocket 断开等告警音</div>
              </div>
              <button
                type="button"
                onClick={() => setAlertEnabled(!settings.alertEnabled)}
                disabled={!settings.enabled}
                className={cn(
                  "relative w-9 h-5 rounded-full transition-colors",
                  settings.alertEnabled && settings.enabled ? "bg-brand" : "bg-muted",
                  !settings.enabled && "opacity-50",
                )}
              >
                <span
                  className={cn(
                    "absolute top-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform",
                    settings.alertEnabled && settings.enabled ? "left-[18px]" : "left-0.5",
                  )}
                />
              </button>
            </div>
          </div>

          <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-4">
            <div className="text-[11px] font-semibold mb-3">测试播放</div>
            <div className="text-[10px] text-muted-foreground mb-3">
              点击下方按钮测试播放对应的提示音。
            </div>
            <div className="grid gap-3">
              {soundItems.map((item) => (
                <button
                  key={item.key}
                  type="button"
                  onClick={() => playSound(item.key)}
                  disabled={playing !== null || !settings.enabled}
                  className={cn(
                    "flex items-center justify-between rounded-[var(--tm-rad)] border border-border bg-background px-3 py-2 transition-colors",
                    playing !== null || !settings.enabled
                      ? "opacity-50 cursor-not-allowed"
                      : "hover:border-brand hover:bg-accent/50",
                  )}
                >
                  <div className="text-left">
                    <div className="text-[11px] font-semibold">{item.label}</div>
                    <div className="text-[9px] text-muted-foreground">{item.desc}</div>
                  </div>
                  <div
                    className={cn(
                      "flex h-6 w-6 items-center justify-center rounded-full font-mono text-[9px] font-bold",
                      playing === item.key
                        ? "bg-brand text-black animate-pulse"
                        : "bg-muted text-muted-foreground",
                    )}
                  >
                    {playing === item.key ? "..." : "▶"}
                  </div>
                </button>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function AboutTab() {
  const [checking, setChecking] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [downloadProgress, setDownloadProgress] = useState(0);
  const [versionInfo, setVersionInfo] = useState<{
    current: string;
    latest: string;
    available: boolean;
  } | null>(null);
  const [updateReady, setUpdateReady] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isElectron =
    typeof window !== "undefined" &&
    typeof navigator !== "undefined" &&
    navigator.userAgent.includes("Electron");
  const updater = isElectron ? window.updater : null;

  const handleDownload = useCallback(async () => {
    if (!updater) return;
    setDownloading(true);
    setDownloadProgress(0);
    try {
      await updater.downloadUpdate();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setDownloading(false);
    }
  }, [updater]);

  const showUpdateDialog = useCallback(
    (version: string, releaseNotes?: string) => {
      const confirmed = confirm(
        `发现新版本 v${version}！\n\n${releaseNotes ? `更新内容：\n${releaseNotes}\n\n` : ""}是否立即更新？更新将自动重启应用。`,
      );
      if (confirmed) {
        void handleDownload();
      }
    },
    [handleDownload],
  );

  useEffect(() => {
    if (!updater) return;

    const unsubAvailable = updater.onUpdateAvailable((info) => {
      setVersionInfo((prev) => (prev ? { ...prev, available: true, latest: info.version } : null));
      showUpdateDialog(info.version, info.releaseNotes);
    });

    const unsubProgress = updater.onDownloadProgress((progress) => {
      setDownloadProgress(progress.percent);
    });

    const unsubDownloaded = updater.onUpdateDownloaded(() => {
      setUpdateReady(true);
      setDownloading(false);
    });

    return () => {
      unsubAvailable();
      unsubProgress();
      unsubDownloaded();
    };
  }, [showUpdateDialog, updater]);

  const handleCheck = async () => {
    if (!updater) {
      setError("仅在 Electron 客户端支持版本检查");
      return;
    }
    setChecking(true);
    setError(null);
    try {
      const result = await updater.checkForUpdates();
      if (result.ok) {
        setVersionInfo({
          current: result.currentVersion,
          latest: result.latestVersion,
          available: result.available,
        });
        if (result.available) {
          showUpdateDialog(result.latestVersion, undefined);
        }
      } else {
        setError(result.error);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setChecking(false);
    }
  };

  const handleInstall = async () => {
    if (!updater) return;
    await updater.installUpdate();
  };

  return (
    <div>
      <div className="mb-4 font-mono text-[12px] font-semibold tracking-[0.2em] text-muted-foreground">
        版本信息
      </div>

      <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-4">
        {!isElectron ? (
          <div className="text-[12px] text-muted-foreground space-y-3">
            <p>Polybet - 预测市场交易系统</p>
            <p>版本: 1.0.0</p>
            <p className="text-muted-foreground/60">版本检查与自动更新仅在桌面客户端中可用</p>
          </div>
        ) : (
          <>
            <div className="flex items-center justify-between mb-4">
              <div>
                <div className="text-[11px] text-muted-foreground">当前版本</div>
                <div className="text-[14px] font-bold">{versionInfo?.current || "检测中..."}</div>
              </div>
              <button
                type="button"
                onClick={handleCheck}
                disabled={checking || downloading}
                className={cn(
                  "flex items-center gap-2 rounded-sm px-3 py-2 font-mono text-[10px] font-bold",
                  checking || downloading
                    ? "bg-muted text-muted-foreground cursor-not-allowed"
                    : "bg-brand text-brand-foreground hover:opacity-90",
                )}
              >
                <RefreshCw className={cn("w-3 h-3", checking && "animate-spin")} />
                {checking ? "检查中..." : "检查更新"}
              </button>
            </div>

            {versionInfo && (
              <div className="mb-4 text-[11px] text-muted-foreground">
                最新版本:{" "}
                <span className={versionInfo.available ? "text-brand font-bold" : ""}>
                  {versionInfo.latest}
                </span>
                {versionInfo.available && !updateReady && !downloading && (
                  <span className="ml-2 text-brand">(有可用更新)</span>
                )}
              </div>
            )}

            {error && (
              <div className="mb-3 rounded-sm border border-destructive/30 bg-destructive/10 px-3 py-2 text-[11px] text-destructive">
                {error}
              </div>
            )}

            {downloading && (
              <div className="mb-3">
                <div className="flex items-center justify-between mb-1">
                  <span className="text-[10px] text-muted-foreground">下载中...</span>
                  <span className="text-[10px]">{downloadProgress.toFixed(0)}%</span>
                </div>
                <div className="h-1.5 rounded-full bg-muted overflow-hidden">
                  <div
                    className="h-full bg-brand transition-all duration-300"
                    style={{ width: `${downloadProgress}%` }}
                  />
                </div>
              </div>
            )}

            {updateReady && (
              <div className="flex items-center gap-3 p-3 rounded-sm bg-success/10 border border-success/30">
                <CheckCircle className="w-5 h-5 text-success" />
                <div className="flex-1">
                  <div className="text-[11px] font-bold text-success">更新已下载</div>
                  <div className="text-[10px] text-muted-foreground">
                    点击下方按钮安装更新并重启
                  </div>
                </div>
                <button
                  type="button"
                  onClick={handleInstall}
                  className="flex items-center gap-2 rounded-sm px-3 py-2 bg-success text-black font-mono text-[10px] font-bold hover:bg-success/90"
                >
                  <Download className="w-3 h-3" />
                  安装并重启
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
