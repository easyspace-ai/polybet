import { createFileRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/app-shell";
import { useState, useEffect, useCallback, Fragment } from "react";
import { getConfig, putConfig, testTelegram, type ConfigRow } from "@/lib/api";
import { toast } from "@/lib/toast";
import { cn } from "@/lib/utils";
import { useTheme, type Theme } from "@/hooks/useTheme";
import { useSoundSettings } from "@/hooks/useSoundSettings";
import {
  Trash2,
  RefreshCw,
  Monitor,
  Network,
  MessageSquare,
  Tag,
  DollarSign,
  Volume2,
  Info,
  Sun,
  Moon,
  Globe,
  Shield,
  Cpu,
  Activity,
  AlertTriangle,
  ChevronRight,
  Play,
  Save,
  Plus,
} from "lucide-react";
import {
  DEFAULT_EVENT_CLASSIFICATION_TAGS,
  parseEventClassificationTags,
} from "@/lib/eventClassification";

export const Route = createFileRoute("/settings")({
  component: SettingsPage,
});

const RESERVED_CONFIG_KEYS = new Set([
  "httpPlatformProxyUrl",
  "telegramBotToken",
  "telegramAuthorizedChatId",
  "eventClassificationTags",
  "priceStopLossRanges",
]);

const SUGGESTED_LEAGUE_TAGS = ["NBA", "NCAAB", "NHL", "EPL", "MLS", "UCL", "MLB"];

export interface PriceStopLossRangeRow {
  id: string;
  name: string;
  minCents: number;
  maxCents: number;
  fundPct: number;
  stopLossPct: number;
}

const DEFAULT_PRICE_ROWS: PriceStopLossRangeRow[] = [
  { id: "r1", name: "20-30¢", minCents: 20, maxCents: 30, fundPct: 17, stopLossPct: 20 },
  { id: "r2", name: "30-40¢", minCents: 30, maxCents: 40, fundPct: 17, stopLossPct: 20 },
  { id: "r3", name: "40-50¢", minCents: 40, maxCents: 50, fundPct: 17, stopLossPct: 20 },
  { id: "r4", name: "50-60¢", minCents: 50, maxCents: 60, fundPct: 17, stopLossPct: 20 },
  { id: "r5", name: "60-70¢", minCents: 60, maxCents: 70, fundPct: 16, stopLossPct: 20 },
  { id: "r6", name: "70-80¢", minCents: 70, maxCents: 80, fundPct: 16, stopLossPct: 20 },
];

type SettingsTab = "general" | "proxy" | "telegram" | "tags" | "prices" | "sound" | "version";

interface TabConfig {
  id: SettingsTab;
  label: string;
  description: string;
  icon: React.ComponentType<{ size?: number; className?: string; strokeWidth?: number }>;
}

const TABS: TabConfig[] = [
  { id: "general", label: "通用", description: "主题、机器人参数", icon: Monitor },
  { id: "proxy", label: "代理", description: "HTTP 代理配置", icon: Network },
  { id: "telegram", label: "电报", description: "Bot 与消息推送", icon: MessageSquare },
  { id: "tags", label: "分类", description: "赛事标签管理", icon: Tag },
  { id: "prices", label: "价格", description: "资金区间与止损", icon: DollarSign },
  { id: "sound", label: "声音", description: "提醒与音效测试", icon: Volume2 },
  { id: "version", label: "关于", description: "版本与更新", icon: Info },
];

const THEME_OPTIONS: { value: Theme; label: string; icon: React.ComponentType<{ size?: number; className?: string; strokeWidth?: number }> }[] = [
  { value: "light", label: "浅色", icon: Sun },
  { value: "dark", label: "深色", icon: Moon },
];

const KEY_DESCRIPTIONS: Record<string, string> = {
  maxTradeSize:
    "单笔交易金额上限。路由在拆单前会先比对请求规模与本值；超出则直接拒绝（size_exceeds_max），不会进行盘口遍历、资金分配或链上调用。",
  slippageTolerance:
    "允许的最优盘口价与实际成交量加权均价之间的最大偏离。路由合并 Polymarket 各档深度并撮合后，若偏离超过本阈值则中止（slippage_exceeded），写入失败记录并告警 Telegram，且不会提交订单。",
  pollingInterval: "市场同步循环从 Polymarket 拉取报价的间隔（毫秒）。更短 = 盘口更新更及时，但 API 压力更大。",
  orderBookLevels:
    "投注单 / 交易面板中，实时推送的 Polymarket 盘口档位数。越大可见深度越多，经 WebSocket 传输的数据也越多。范围 3–25。",
  polymarketFokBuyExtraTicks:
    "Polymarket FOK 买入：在最优卖价（best ask）之上额外允许的 tick 档数，用于放宽限价，减少「无法完全成交」被拒。",
  polymarketFokSellExtraTicks:
    "Polymarket FOK 卖出（含风控平仓）：在最优买价（best bid）之下额外放宽的 tick 档数。整数 0–50，默认 5。",
  minOpenRiskShares:
    "风控列表与 CLOB 余额对账：仅保留份额 ≥ 本值的持仓（默认 1）。低于该值的链上余额会对应关闭本地仓位。",
};

function rowValue(rows: ConfigRow[], key: string): string {
  return rows.find((r) => r.key === key)?.value ?? "";
}

function parsePriceRowsJson(raw: string): PriceStopLossRangeRow[] {
  if (!raw.trim()) return DEFAULT_PRICE_ROWS.map((r) => ({ ...r }));
  try {
    const p = JSON.parse(raw) as unknown;
    if (!Array.isArray(p) || p.length === 0) return DEFAULT_PRICE_ROWS.map((r) => ({ ...r }));
    return p.map((row: unknown, i: number) => {
      const o = row as Record<string, unknown>;
      return {
        id: typeof o.id === "string" && o.id ? o.id : `r${i + 1}`,
        name: String(o.name ?? ""),
        minCents: Number(o.minCents) || 0,
        maxCents: Number(o.maxCents) || 0,
        fundPct: Number(o.fundPct) || 0,
        stopLossPct: Number(o.stopLossPct) || 0,
      };
    });
  } catch {
    return DEFAULT_PRICE_ROWS.map((r) => ({ ...r }));
  }
}

/* ===== THEME TOGGLE ===== */
function ThemeToggle() {
  const { theme, setTheme } = useTheme();

  return (
    <div className="flex items-center gap-1 p-1 rounded-lg bg-surface border border-border w-fit">
      {THEME_OPTIONS.map((opt) => {
        const Icon = opt.icon;
        const isActive = theme === opt.value;
        return (
          <button
            key={opt.value}
            type="button"
            onClick={() => setTheme(opt.value)}
            className={cn(
              "relative flex items-center gap-1.5 px-3 py-1.5 rounded-md",
              "text-xs font-medium transition-all duration-200",
              isActive
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <Icon size={13} />
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}

/* ===== SAVE BUTTON ===== */
function SaveButton({
  disabled,
  loading,
  onClick,
  variant = "primary",
  children,
}: {
  disabled?: boolean;
  loading?: boolean;
  onClick: () => void;
  variant?: "primary" | "secondary" | "danger";
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled || loading}
      className={cn(
        "relative inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-md",
        "text-xs font-semibold tracking-wide",
        "transition-all duration-200",
        "focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/40",
        disabled || loading
          ? "bg-surface text-muted-foreground cursor-not-allowed border border-border"
          : variant === "primary"
            ? "bg-primary text-primary-foreground hover:bg-primary/90"
            : variant === "danger"
              ? "bg-down/90 text-white hover:bg-down"
              : "bg-surface text-foreground border border-border hover:bg-surface-hover"
      )}
    >
      {loading && <RefreshCw size={12} className="animate-spin" />}
      {!loading && variant === "primary" && <Save size={12} />}
      {children}
    </button>
  );
}

/* ===== CONFIG ROW ===== */
function ConfigRowItem({
  row,
  isSaving,
  onSave,
  onChange,
}: {
  row: ConfigRow;
  isSaving: boolean;
  onSave: () => void;
  onChange: (v: string) => void;
}) {
  const [value, setValue] = useState(row.value);

  useEffect(() => {
    setValue(row.value);
  }, [row.value]);

  const isDirty = value !== row.value;
  const description = KEY_DESCRIPTIONS[row.key];

  return (
    <div
      className={cn(
        "group rounded-md border transition-all duration-150",
        "bg-sidebar",
        isDirty ? "border-primary/40 bg-primary/[0.03]" : "border-border hover:border-ring"
      )}
    >
      <div className="flex items-start justify-between gap-3 p-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-1">
            <code className="text-[11px] font-mono font-semibold text-foreground">{row.key}</code>
            {isDirty && (
              <span className="text-[9px] font-mono text-warning px-1 py-0.5 rounded-sm bg-warning/10 border border-warning/20">
                未保存
              </span>
            )}
          </div>
          {description && (
            <div className="text-[10px] text-muted-foreground leading-relaxed line-clamp-2">
              {description}
            </div>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <input
            type="text"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            className={cn(
              "w-36 rounded-md bg-surface border px-2.5 py-1.5",
              "text-xs font-mono text-foreground text-right",
              "outline-none transition-all duration-150",
              "focus:border-primary/60",
              isDirty ? "border-primary/50" : "border-border"
            )}
          />
          <SaveButton
            disabled={!isDirty}
            loading={isSaving}
            onClick={() => {
              onChange(value);
              onSave();
            }}
          >
            保存
          </SaveButton>
        </div>
      </div>
    </div>
  );
}

/* ===== SOUND TEST ===== */
function SoundTestPanel() {
  const [playing, setPlaying] = useState<string | null>(null);
  const { settings, setEnabled, setBuyEnabled, setSellEnabled, setAlertEnabled } = useSoundSettings();
  const isElectron = typeof navigator !== "undefined" && navigator.userAgent.includes("Electron");

  const playSound = async (soundName: string) => {
    const api = (
      window as unknown as {
        desktopAPI?: { playSound?: (s: string) => Promise<{ ok: true } | { ok: false; error: string }> };
      }
    ).desktopAPI;
    if (!isElectron || !api?.playSound) {
      toast({ title: "无法播放", description: "仅在桌面客户端中支持声音播放", variant: "destructive" });
      return;
    }
    setPlaying(soundName);
    try {
      const result = await api.playSound(soundName);
      if (result.ok) {
        toast({ title: "播放成功", description: `已播放 ${soundName} 声音`, variant: "success" });
      } else {
        toast({ title: "播放失败", description: result.error, variant: "destructive" });
      }
    } catch (err) {
      toast({ title: "播放失败", description: err instanceof Error ? err.message : "未知错误", variant: "destructive" });
    } finally {
      setTimeout(() => setPlaying(null), 800);
    }
  };

  const soundItems = [
    { key: "buy", label: "开单", desc: "买入成交", color: "text-primary" },
    { key: "sell", label: "平仓", desc: "卖出成交", color: "text-up" },
    { key: "alert", label: "告警", desc: "异常提示", color: "text-warning" },
  ] as const;

  return (
    <div className="space-y-5">
      {/* Master toggle */}
      <div className="flex items-center justify-between py-2">
        <div>
          <div className="text-sm font-medium text-foreground">声音提醒</div>
          <div className="text-[10px] text-muted-foreground mt-0.5">
            {isElectron ? "桌面客户端音效反馈" : "仅在桌面客户端可用"}
          </div>
        </div>
        <button
          type="button"
          onClick={() => setEnabled(!settings.enabled)}
          className={cn(
            "relative inline-flex h-5 w-9 items-center rounded-full transition-colors",
            settings.enabled ? "bg-primary" : "bg-muted-foreground/30"
          )}
        >
          <span
            className={cn(
              "inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform",
              settings.enabled ? "translate-x-4.5" : "translate-x-0.5"
            )}
            style={{ transform: settings.enabled ? "translateX(18px)" : "translateX(2px)" }}
          />
        </button>
      </div>

      {/* Sub toggles */}
      <div className="space-y-1">
        {[
          { label: "开单提醒", desc: "买入成交提示音", setter: setBuyEnabled, enabled: settings.buyEnabled },
          { label: "平仓提醒", desc: "卖出/止损成交提示音", setter: setSellEnabled, enabled: settings.sellEnabled },
          { label: "系统告警", desc: "WebSocket 断开等异常提示", setter: setAlertEnabled, enabled: settings.alertEnabled },
        ].map((item) => (
          <div key={item.label} className="flex items-center justify-between py-2 opacity-100">
            <div>
              <div className={cn("text-sm font-medium", !settings.enabled && "text-muted-foreground")}>{item.label}</div>
              <div className="text-[10px] text-muted-foreground">{item.desc}</div>
            </div>
            <button
              type="button"
              disabled={!settings.enabled}
              onClick={() => item.setter(!item.enabled)}
              className={cn(
                "relative inline-flex h-5 w-9 items-center rounded-full transition-colors",
                !settings.enabled && "opacity-40 cursor-not-allowed",
                item.enabled ? "bg-primary" : "bg-muted-foreground/30"
              )}
            >
              <span
                className="inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform"
                style={{ transform: item.enabled ? "translateX(18px)" : "translateX(2px)" }}
              />
            </button>
          </div>
        ))}
      </div>

      {/* Audio console */}
      <div className="pt-3 border-t border-border/50">
        <div className="text-[10px] font-medium text-muted-foreground mb-2.5 uppercase tracking-wider">音效测试</div>
        <div className="grid grid-cols-3 gap-2">
          {soundItems.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => playSound(item.key)}
              disabled={!settings.enabled || playing !== null}
              className={cn(
                "relative flex flex-col items-center gap-2 p-3 rounded-lg",
                "border bg-surface",
                "transition-all duration-200",
                "hover:border-primary/40 hover:bg-primary/[0.04]",
                (!settings.enabled || playing !== null) && "opacity-40 cursor-not-allowed",
                playing === item.key && "border-primary/60 bg-primary/[0.06]"
              )}
            >
              <div
                className={cn(
                  "relative flex items-center justify-center w-9 h-9 rounded-full",
                  "bg-sidebar border border-border",
                  "transition-all duration-200",
                  playing === item.key && item.color
                )}
              >
                <Play size={14} className={cn(playing === item.key && item.color)} />
              </div>
              <div className="text-center">
                <div className="text-xs font-medium text-foreground">{item.label}</div>
                <div className="text-[10px] text-muted-foreground mt-0.5">{item.desc}</div>
              </div>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

/* ===== VERSION TAB ===== */
function VersionPanel() {
  const [checking, setChecking] = useState(false);
  const [updateInfo, setUpdateInfo] = useState<
    { available: boolean; currentVersion: string; latestVersion: string } | null
  >(null);

  const checkUpdates = async () => {
    if (!window.updater?.checkForUpdates) return;
    setChecking(true);
    try {
      const result = await window.updater.checkForUpdates();
      if (result.ok) {
        setUpdateInfo(result);
        if (result.available) {
          toast({ title: "发现更新", description: `新版本 ${result.latestVersion} 可用`, variant: "success" });
        } else {
          toast({ title: "已是最新", description: `当前版本 ${result.currentVersion}`, variant: "success" });
        }
      } else {
        toast({ title: "检查失败", description: result.error, variant: "destructive" });
      }
    } catch (err) {
      toast({ title: "检查失败", description: err instanceof Error ? err.message : "未知错误", variant: "destructive" });
    } finally {
      setChecking(false);
    }
  };

  return (
    <div className="flex flex-col items-center justify-center py-12 px-4">
      <div className="relative mb-5">
        <div className="w-14 h-14 rounded-xl bg-surface border border-border flex items-center justify-center">
          <Activity className="w-7 h-7 text-primary" />
        </div>
      </div>
      <h2 className="text-base font-bold text-foreground tracking-tight">PolyBet</h2>
      <p className="text-xs text-muted-foreground mt-1">AI 驱动的高频交易终端</p>
      <div className="mt-6 w-full max-w-xs space-y-3">
        <div className="flex items-center justify-between py-2 border-b border-border/50">
          <span className="text-[10px] text-muted-foreground">当前版本</span>
          <span className="text-xs font-mono text-foreground">v{import.meta.env.VITE_APP_VERSION ?? "0.1.0"}</span>
        </div>
        <div className="flex items-center justify-between py-2 border-b border-border/50">
          <span className="text-[10px] text-muted-foreground">平台</span>
          <span className="text-xs font-mono text-muted-foreground">Polymarket / CLOB</span>
        </div>
        <div className="flex items-center justify-between py-2">
          <span className="text-[10px] text-muted-foreground">引擎</span>
          <span className="text-xs font-mono text-muted-foreground">AI 路由 + 风控</span>
        </div>
      </div>
      {window.updater && (
        <button
          type="button"
          onClick={() => void checkUpdates()}
          disabled={checking}
          className={cn(
            "mt-6 px-4 py-2 rounded-md text-xs font-semibold",
            "bg-surface border border-border text-muted-foreground",
            "hover:bg-surface-hover hover:text-foreground hover:border-ring",
            "transition-all duration-150",
            checking && "opacity-50 cursor-wait"
          )}
        >
          {checking ? (
            <span className="flex items-center gap-1.5">
              <RefreshCw size={12} className="animate-spin" />
              检查中...
            </span>
          ) : (
            "检查更新"
          )}
        </button>
      )}
      {updateInfo?.available && (
        <div className="mt-3 px-3 py-2 rounded-md bg-primary/10 border border-primary/30 text-xs text-primary">
          新版本 {updateInfo.latestVersion} 可用
        </div>
      )}
    </div>
  );
}

/* ===== SETTINGS PAGE ===== */
function SettingsPage() {
  const [tab, setTab] = useState<SettingsTab>("general");
  const [rows, setRows] = useState<ConfigRow[]>([]);
  const [edited, setEdited] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const [proxyDraft, setProxyDraft] = useState("");
  const [tgTokenDraft, setTgTokenDraft] = useState("");
  const [tgChatDraft, setTgChatDraft] = useState("");
  const [testingTg, setTestingTg] = useState(false);
  const [tags, setTags] = useState<string[]>([...DEFAULT_EVENT_CLASSIFICATION_TAGS]);
  const [tagInput, setTagInput] = useState("");
  const [priceRows, setPriceRows] = useState<PriceStopLossRangeRow[]>(() => DEFAULT_PRICE_ROWS.map((r) => ({ ...r })));

  const reload = useCallback((options?: { silent?: boolean }) => {
    const silent = options?.silent;
    if (!silent) setLoading(true);
    return getConfig()
      .then((data) => {
        setRows(data);
        setError(null);
        setProxyDraft(data.find((r) => r.key === "httpPlatformProxyUrl")?.value ?? "");
        setTgTokenDraft(data.find((r) => r.key === "telegramBotToken")?.value ?? "");
        setTgChatDraft(data.find((r) => r.key === "telegramAuthorizedChatId")?.value ?? "");
        setTags(parseEventClassificationTags(rowValue(data, "eventClassificationTags")));
        setPriceRows(parsePriceRowsJson(rowValue(data, "priceStopLossRanges")));
        setEdited({});
      })
      .catch((err) => setError(err instanceof Error ? err.message : "加载配置失败"))
      .finally(() => {
        if (!silent) setLoading(false);
      });
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  function getValue(key: string) {
    return key in edited ? edited[key] : rows.find((r) => r.key === key)?.value ?? "";
  }

  async function handleSave(key: string) {
    const value = getValue(key);
    setSaving(key);
    try {
      await putConfig(key, value);
      setRows((prev) => prev.map((r) => (r.key === key ? { ...r, value } : r)));
      setEdited((prev) => {
        const next = { ...prev };
        delete next[key];
        return next;
      });
      toast({ title: "已保存", description: `已更新 ${key}`, variant: "success" });
    } catch (err) {
      toast({ title: "保存失败", description: err instanceof Error ? err.message : "未知错误", variant: "destructive" });
    } finally {
      setSaving(null);
    }
  }

  async function saveStandalone(key: string, value: string, label: string) {
    setSaving(key);
    try {
      await putConfig(key, value);
      setRows((prev) => {
        const i = prev.findIndex((r) => r.key === key);
        if (i >= 0) {
          const next = [...prev];
          next[i] = { ...next[i], value };
          return next;
        }
        return [...prev, { key, value }].sort((a, b) => a.key.localeCompare(b.key));
      });
      toast({ title: "已保存", description: label, variant: "success" });
    } catch (err) {
      toast({ title: "保存失败", description: err instanceof Error ? err.message : "未知错误", variant: "destructive" });
    } finally {
      setSaving(null);
    }
  }

  const generalRows = rows.filter((r) => !RESERVED_CONFIG_KEYS.has(r.key));
  const fundSum = priceRows.reduce((a, r) => a + (Number.isFinite(r.fundPct) ? r.fundPct : 0), 0);

  function addTag(raw: string) {
    const t = raw.trim().toLowerCase();
    if (!t) return;
    if (tags.includes(t)) return;
    setTags((prev) => [...prev, t]);
    setTagInput("");
  }

  function removeTag(t: string) {
    setTags((prev) => prev.filter((x) => x !== t));
  }

  const activeTab = TABS.find((t) => t.id === tab)!;

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <PageHeader
        left={
          <>
            <div className="flex items-center justify-center w-7 h-7 rounded-md bg-surface border border-border">
              <activeTab.icon size={14} className="text-primary" />
            </div>
            <span className="text-sm font-bold text-foreground tracking-tight">设置</span>
            <span className="text-[10px] text-muted-foreground font-mono">{rows.length} 项配置</span>
          </>
        }
      />

      <div className="flex flex-1 min-h-0 overflow-hidden">
        {/* Settings sidebar */}
        <aside className="hidden md:flex w-56 shrink-0 flex-col border-r border-border bg-sidebar/60 overflow-y-auto">
          <div className="p-2 space-y-0.5">
            {TABS.map((t) => {
              const Icon = t.icon;
              const isActive = tab === t.id;
              return (
                <button
                  key={t.id}
                  type="button"
                  onClick={() => setTab(t.id)}
                  className={cn(
                    "w-full flex items-start gap-3 px-3 py-2.5 rounded-md",
                    "transition-all duration-200",
                    "text-left",
                    isActive
                      ? "bg-primary/[0.07] text-primary"
                      : "text-muted-foreground hover:text-foreground hover:bg-surface/40"
                  )}
                >
                  <Icon size={15} strokeWidth={isActive ? 2 : 1.5} className="shrink-0 mt-0.5" />
                  <div className="min-w-0">
                    <div className={cn("text-[13px] font-medium", isActive && "font-semibold")}>{t.label}</div>
                    <div className="text-[10px] text-muted-foreground mt-0.5 leading-snug">{t.description}</div>
                  </div>
                  {isActive && <ChevronRight size={12} className="shrink-0 ml-auto mt-1 opacity-60" />}
                </button>
              );
            })}
          </div>
        </aside>

        {/* Mobile tab selector */}
        <div className="md:hidden shrink-0 flex items-center gap-1 px-3 py-2 bg-sidebar/80 border-b border-border overflow-x-auto">
          {TABS.map((t) => {
            const Icon = t.icon;
            const isActive = tab === t.id;
            return (
              <button
                key={t.id}
                type="button"
                onClick={() => setTab(t.id)}
                className={cn(
                  "flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium whitespace-nowrap",
                  "transition-all duration-150",
                  isActive
                    ? "bg-primary/15 text-primary border border-primary/30"
                    : "text-muted-foreground hover:text-foreground border border-transparent"
                )}
              >
                <Icon size={13} />
                {t.label}
              </button>
            );
          })}
        </div>

        {/* Content */}
        <div className="flex-1 min-h-0 overflow-y-auto">
          <div className="max-w-2xl mx-auto p-5 space-y-4">
            {/* Error */}
            {error && (
              <div className="flex items-center gap-2.5 p-3 rounded-lg border border-down/30 bg-down/[0.06]">
                <AlertTriangle size={14} className="text-down shrink-0" />
                <span className="text-xs text-down">{error}</span>
              </div>
            )}

            {loading && (
              <div className="flex items-center justify-center py-16">
                <div className="flex flex-col items-center gap-3">
                  <RefreshCw size={18} className="animate-spin text-muted-foreground" />
                  <span className="text-[10px] text-muted-foreground font-mono">加载配置...</span>
                </div>
              </div>
            )}

            {/* ===== GENERAL TAB ===== */}
            {!loading && tab === "general" && (
              <div className="space-y-4">
                {/* Theme */}
                <div className="rounded-md border border-border bg-surface/40 p-5">
                  <div className="flex items-start gap-3 mb-4">
                    <div className="grid size-8 place-items-center rounded bg-primary/10 ring-1 ring-primary/20">
                      <Globe size={14} className="text-primary" />
                    </div>
                    <div>
                      <div className="font-semibold text-sm">界面主题</div>
                      <div className="text-xs text-muted-foreground mt-0.5">选择适合交易环境的配色方案</div>
                    </div>
                  </div>
                  <ThemeToggle />
                </div>

                {/* Config info */}
                <div className="rounded-md border border-border bg-surface/40 p-5">
                  <div className="flex items-start gap-3 mb-4">
                    <div className="grid size-8 place-items-center rounded bg-primary/10 ring-1 ring-primary/20">
                      <Shield size={14} className="text-primary" />
                    </div>
                    <div>
                      <div className="font-semibold text-sm">配置文件</div>
                      <div className="text-xs text-muted-foreground mt-0.5">服务端配置存储位置</div>
                    </div>
                  </div>
                  <p className="text-[10px] text-muted-foreground leading-relaxed">
                    服务端将机器人参数与{" "}
                    <code className="px-1.5 py-0.5 rounded-sm bg-surface border border-border text-muted-foreground font-mono text-[10px]">
                      ~/.polybet/bot-settings.json
                    </code>{" "}
                    同步。也可直接编辑该 JSON 后重启服务。
                  </p>
                </div>

                {/* Bot parameters */}
                {generalRows.length > 0 && (
                  <div className="space-y-3">
                    <div className="flex items-center gap-2 px-1">
                      <Cpu size={13} className="text-muted-foreground" />
                      <span className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">
                        机器人参数
                      </span>
                      <span className="ml-auto text-[10px] font-mono text-muted-foreground">{generalRows.length} 项</span>
                    </div>
                    <div className="space-y-2">
                      {generalRows.map((row) => {
                        const isDirty = row.key in edited && edited[row.key] !== row.value;
                        const isSaving = saving === row.key;
                        return (
                          <ConfigRowItem
                            key={row.key}
                            row={row}
                            isSaving={isSaving}
                            onSave={() => handleSave(row.key)}
                            onChange={(v) => setEdited((prev) => ({ ...prev, [row.key]: v }))}
                          />
                        );
                      })}
                    </div>
                  </div>
                )}
              </div>
            )}

            {/* ===== PROXY TAB ===== */}
            {!loading && tab === "proxy" && (
              <div className="space-y-4">
                <div className="rounded-md border border-border bg-surface/40 p-5">
                  <div className="flex items-start gap-3 mb-4">
                    <div className="grid size-8 place-items-center rounded bg-primary/10 ring-1 ring-primary/20">
                      <Network size={14} className="text-primary" />
                    </div>
                    <div>
                      <div className="font-semibold text-sm">HTTP(S) 代理</div>
                      <div className="text-xs text-muted-foreground mt-0.5">非空时覆盖默认代理设置，经 CONNECT 转发 Polymarket 出站请求</div>
                    </div>
                  </div>
                  <div className="space-y-3">
                    <input
                      type="text"
                      value={proxyDraft}
                      onChange={(e) => setProxyDraft(e.target.value)}
                      placeholder="https://user:pass@host:port"
                      className="w-full bg-background border border-border rounded-md px-3 py-2 text-sm text-foreground focus:outline-none focus:border-ring"
                    />
                    <SaveButton
                      disabled={proxyDraft === rowValue(rows, "httpPlatformProxyUrl")}
                      loading={saving === "httpPlatformProxyUrl"}
                      onClick={() => saveStandalone("httpPlatformProxyUrl", proxyDraft, "代理地址")}
                    >
                      保存代理
                    </SaveButton>
                  </div>
                </div>
              </div>
            )}

            {/* ===== TELEGRAM TAB ===== */}
            {!loading && tab === "telegram" && (
              <div className="space-y-4">
                <div className="rounded-md border border-border bg-surface/40 p-5">
                  <div className="flex items-start gap-3 mb-4">
                    <div className="grid size-8 place-items-center rounded bg-primary/10 ring-1 ring-primary/20">
                      <MessageSquare size={14} className="text-primary" />
                    </div>
                    <div>
                      <div className="font-semibold text-sm">Telegram Bot 配置</div>
                      <div className="text-xs text-muted-foreground mt-0.5">告警推送与状态通知</div>
                    </div>
                  </div>
                  <div className="space-y-4">
                    <p className="text-[10px] text-muted-foreground leading-relaxed">
                      对应{" "}
                      <code className="px-1 py-0.5 rounded-sm bg-surface border border-border text-muted-foreground font-mono text-[10px]">
                        TELEGRAM_BOT_TOKEN
                      </code>{" "}
                      与{" "}
                      <code className="px-1 py-0.5 rounded-sm bg-surface border border-border text-muted-foreground font-mono text-[10px]">
                        TELEGRAM_AUTHORIZED_CHAT_ID
                      </code>
                      。修改 Token 后需重启进程。
                    </p>
                    <div className="space-y-3">
                      <div>
                        <label className="text-[10px] text-muted-foreground block mb-1">Bot Token</label>
                        <input
                          type="password"
                          value={tgTokenDraft}
                          onChange={(e) => setTgTokenDraft(e.target.value)}
                          placeholder="123456:ABC-..."
                          className="w-full bg-background border border-border rounded-md px-3 py-2 text-sm text-foreground focus:outline-none focus:border-ring"
                        />
                      </div>
                      <div>
                        <label className="text-[10px] text-muted-foreground block mb-1">Authorized Chat ID</label>
                        <input
                          type="text"
                          value={tgChatDraft}
                          onChange={(e) => setTgChatDraft(e.target.value)}
                          placeholder="123456789"
                          className="w-full bg-background border border-border rounded-md px-3 py-2 text-sm text-foreground focus:outline-none focus:border-ring"
                        />
                      </div>
                    </div>
                    <div className="flex gap-2">
                      <SaveButton
                        disabled={
                          tgTokenDraft === rowValue(rows, "telegramBotToken") &&
                          tgChatDraft === rowValue(rows, "telegramAuthorizedChatId")
                        }
                        loading={saving === "telegram"}
                        onClick={async () => {
                          setSaving("telegram");
                          try {
                            await putConfig("telegramBotToken", tgTokenDraft);
                            await putConfig("telegramAuthorizedChatId", tgChatDraft);
                            await reload({ silent: true });
                            toast({ title: "已保存", description: "Telegram 配置", variant: "success" });
                          } catch (err) {
                            toast({
                              title: "保存失败",
                              description: err instanceof Error ? err.message : "未知错误",
                              variant: "destructive",
                            });
                          } finally {
                            setSaving(null);
                          }
                        }}
                      >
                        保存配置
                      </SaveButton>
                      <button
                        type="button"
                        disabled={testingTg || !tgTokenDraft || !tgChatDraft}
                        onClick={async () => {
                          setTestingTg(true);
                          try {
                            await testTelegram();
                            toast({ title: "测试成功", description: "测试消息已发送", variant: "success" });
                          } catch (err) {
                            toast({
                              title: "测试失败",
                              description: err instanceof Error ? err.message : "未知错误",
                              variant: "destructive",
                            });
                          } finally {
                            setTestingTg(false);
                          }
                        }}
                        className={cn(
                          "px-3.5 py-1.5 rounded-md text-xs font-medium",
                          "transition-all duration-150",
                          testingTg || !tgTokenDraft || !tgChatDraft
                            ? "bg-surface text-muted-foreground cursor-not-allowed border border-border"
                            : "bg-info/15 text-info border border-info/30 hover:bg-info/25"
                        )}
                      >
                        {testingTg ? (
                          <span className="flex items-center gap-1.5">
                            <RefreshCw size={11} className="animate-spin" />
                            发送中...
                          </span>
                        ) : (
                          "测试消息"
                        )}
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            )}

            {/* ===== TAGS TAB ===== */}
            {!loading && tab === "tags" && (
              <div className="space-y-4">
                <div className="rounded-md border border-border bg-surface/40 p-5">
                  <div className="flex items-start gap-3 mb-4">
                    <div className="grid size-8 place-items-center rounded bg-primary/10 ring-1 ring-primary/20">
                      <Tag size={14} className="text-primary" />
                    </div>
                    <div>
                      <div className="font-semibold text-sm">赛事分类</div>
                      <div className="text-xs text-muted-foreground mt-0.5">用于标记关注的联赛与标签</div>
                    </div>
                  </div>
                  <div className="flex flex-wrap gap-1.5 mb-3">
                    {tags.map((t) => (
                      <span
                        key={t}
                        className={cn(
                          "inline-flex items-center gap-1 rounded-full",
                          "border border-primary/30 bg-primary/10",
                          "px-2.5 py-1 text-[10px] font-medium text-primary",
                          "transition-all duration-150"
                        )}
                      >
                        {t.toUpperCase()}
                        <button
                          type="button"
                          onClick={() => removeTag(t)}
                          className="ml-0.5 p-0.5 rounded-full hover:bg-primary/20 transition-colors"
                        >
                          <Trash2 size={10} />
                        </button>
                      </span>
                    ))}
                  </div>
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={tagInput}
                      onChange={(e) => setTagInput(e.target.value)}
                      placeholder="输入标签..."
                      onKeyDown={(e) => {
                        if (e.key === "Enter") {
                          e.preventDefault();
                          addTag(tagInput);
                        }
                      }}
                      className="flex-1 bg-background border border-border rounded-md px-3 py-2 text-sm text-foreground focus:outline-none focus:border-ring"
                    />
                    <button
                      type="button"
                      onClick={() => addTag(tagInput)}
                      className="px-3 py-2 rounded-md text-xs font-medium bg-primary/15 text-primary border border-primary/30 hover:bg-primary/25 transition-all duration-150"
                    >
                      <Plus size={14} className="inline mr-1 -mt-0.5" />
                      添加
                    </button>
                  </div>
                  <div className="flex flex-wrap gap-1.5 mt-3 pt-3 border-t border-border/50">
                    {SUGGESTED_LEAGUE_TAGS.map((label) => {
                      const key = label.toLowerCase();
                      const selected = tags.includes(key);
                      return (
                        <button
                          key={label}
                          type="button"
                          disabled={selected}
                          onClick={() => addTag(key)}
                          className={cn(
                            "px-2 py-1 rounded-full text-[10px] font-medium transition-all duration-150",
                            selected
                              ? "border border-border bg-surface text-muted-foreground cursor-default"
                              : "border border-border bg-sidebar text-muted-foreground hover:border-primary/50 hover:text-primary"
                          )}
                        >
                          {label}
                        </button>
                      );
                    })}
                  </div>
                  <div className="mt-4 pt-3 border-t border-border/50 flex justify-end">
                    <SaveButton
                      disabled={JSON.stringify(tags) === rowValue(rows, "eventClassificationTags")}
                      loading={saving === "eventClassificationTags"}
                      onClick={() => saveStandalone("eventClassificationTags", JSON.stringify(tags), "赛事分类")}
                    >
                      保存分类
                    </SaveButton>
                  </div>
                </div>
              </div>
            )}

            {/* ===== PRICES TAB ===== */}
            {!loading && tab === "prices" && (
              <div className="space-y-4">
                <div className="rounded-md border border-border bg-surface/40 p-5">
                  <div className="flex items-start justify-between mb-4">
                    <div className="flex items-start gap-3">
                      <div className="grid size-8 place-items-center rounded bg-primary/10 ring-1 ring-primary/20">
                        <DollarSign size={14} className="text-primary" />
                      </div>
                      <div>
                        <div className="font-semibold text-sm">价格区间</div>
                        <div className="text-xs text-muted-foreground mt-0.5">资金分配比例与止损设置</div>
                      </div>
                    </div>
                    <button
                      type="button"
                      onClick={() =>
                        setPriceRows((prev) => [
                          ...prev,
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
                      className="px-2.5 py-1.5 rounded-md text-[10px] font-medium bg-surface border border-border text-muted-foreground hover:text-foreground hover:border-ring transition-all duration-150"
                    >
                      <Plus size={12} className="inline mr-1 -mt-0.5" />
                      添加
                    </button>
                  </div>

                  <div className="mb-3 flex items-center gap-2">
                    <span className="text-[10px] text-muted-foreground">资金占比合计</span>
                    <span className={cn("text-xs font-mono font-bold", fundSum === 100 ? "text-up" : "text-warning")}>
                      {fundSum.toFixed(0)}%
                    </span>
                    {fundSum !== 100 && <span className="text-[10px] text-warning">（建议接近 100%）</span>}
                  </div>

                  <div className="space-y-1.5">
                    <div
                      className="grid gap-2 text-[10px] font-medium text-muted-foreground uppercase tracking-wider px-1"
                      style={{ gridTemplateColumns: "1fr 52px 52px 60px 60px 32px" }}
                    >
                      <span>名称</span>
                      <span className="text-right">下限</span>
                      <span className="text-right">上限</span>
                      <span className="text-right">资金%</span>
                      <span className="text-right">止损%</span>
                      <span />
                    </div>
                    {priceRows.map((r, idx) => (
                      <div
                        key={r.id}
                        className="grid gap-2 items-center px-1 py-1 rounded-md hover:bg-surface/30 transition-colors"
                        style={{ gridTemplateColumns: "1fr 52px 52px 60px 60px 32px" }}
                      >
                        <input
                          value={r.name}
                          onChange={(e) =>
                            setPriceRows((prev) =>
                              prev.map((x, i) => (i === idx ? { ...x, name: e.target.value } : x))
                            )
                          }
                          className="px-2 py-1.5 rounded-md bg-background border border-border text-xs text-foreground outline-none focus:border-ring transition-colors"
                        />
                        <input
                          type="number"
                          value={r.minCents}
                          onChange={(e) =>
                            setPriceRows((prev) =>
                              prev.map((x, i) => (i === idx ? { ...x, minCents: Number(e.target.value) || 0 } : x))
                            )
                          }
                          className="px-2 py-1.5 rounded-md bg-background border border-border text-xs text-foreground font-mono text-right outline-none focus:border-ring transition-colors"
                        />
                        <input
                          type="number"
                          value={r.maxCents}
                          onChange={(e) =>
                            setPriceRows((prev) =>
                              prev.map((x, i) => (i === idx ? { ...x, maxCents: Number(e.target.value) || 0 } : x))
                            )
                          }
                          className="px-2 py-1.5 rounded-md bg-background border border-border text-xs text-foreground font-mono text-right outline-none focus:border-ring transition-colors"
                        />
                        <input
                          type="number"
                          value={r.fundPct}
                          onChange={(e) =>
                            setPriceRows((prev) =>
                              prev.map((x, i) => (i === idx ? { ...x, fundPct: Number(e.target.value) || 0 } : x))
                            )
                          }
                          className="px-2 py-1.5 rounded-md bg-background border border-border text-xs text-foreground font-mono text-right outline-none focus:border-ring transition-colors"
                        />
                        <input
                          type="number"
                          value={r.stopLossPct}
                          onChange={(e) =>
                            setPriceRows((prev) =>
                              prev.map((x, i) => (i === idx ? { ...x, stopLossPct: Number(e.target.value) || 0 } : x))
                            )
                          }
                          className="px-2 py-1.5 rounded-md bg-background border border-border text-xs text-foreground font-mono text-right outline-none focus:border-ring transition-colors"
                        />
                        <button
                          type="button"
                          onClick={() => setPriceRows((prev) => prev.filter((_, i) => i !== idx))}
                          className="flex items-center justify-center p-1.5 rounded-md hover:bg-down/10 text-muted-foreground hover:text-down transition-colors"
                        >
                          <Trash2 size={12} />
                        </button>
                      </div>
                    ))}
                  </div>

                  <div className="mt-4 pt-3 border-t border-border/50 flex justify-end">
                    <SaveButton
                      disabled={JSON.stringify(priceRows) === rowValue(rows, "priceStopLossRanges")}
                      loading={saving === "priceStopLossRanges"}
                      onClick={() => saveStandalone("priceStopLossRanges", JSON.stringify(priceRows), "价格区间")}
                    >
                      保存区间
                    </SaveButton>
                  </div>
                </div>
              </div>
            )}

            {/* ===== SOUND TAB ===== */}
            {!loading && tab === "sound" && (
              <div className="rounded-md border border-border bg-surface/40 p-5">
                <div className="flex items-start gap-3 mb-4">
                  <div className="grid size-8 place-items-center rounded bg-primary/10 ring-1 ring-primary/20">
                    <Volume2 size={14} className="text-primary" />
                  </div>
                  <div>
                    <div className="font-semibold text-sm">声音提醒</div>
                    <div className="text-xs text-muted-foreground mt-0.5">交易音效与系统告警</div>
                  </div>
                </div>
                <SoundTestPanel />
              </div>
            )}

            {/* ===== VERSION TAB ===== */}
            {!loading && tab === "version" && (
              <div className="rounded-md border border-border bg-surface/40 overflow-hidden">
                <VersionPanel />
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

declare global {
  interface Window {
    updater?: {
      checkForUpdates: () => Promise<
        | { ok: true; currentVersion: string; latestVersion: string; available: boolean }
        | { ok: false; error: string }
      >;
      downloadUpdate: () => Promise<void>;
      installUpdate: () => Promise<void>;
      onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string }) => void) => () => void;
      onDownloadProgress: (callback: (progress: { percent: number }) => void) => () => void;
      onUpdateDownloaded: (callback: () => void) => () => void;
    };
  }
}
