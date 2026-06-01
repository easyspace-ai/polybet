import { useState, useEffect, useCallback, useMemo } from "react";
import { TopBar } from "@/components/TopBar";
import { MatchTimeVolume } from "@/components/MatchTimeVolume";
import { useMarketList } from "@/hooks/useMarketList";
import { useConfig } from "@/hooks/useConfig";
import {
  DEFAULT_EVENT_CLASSIFICATION_TAGS,
  parseEventClassificationTags,
} from "@/lib/eventClassification";
import {
  getAutoOrderConfig,
  putAutoOrderConfig,
  getTeams,
  type AutoOrderConfig,
  type AutoOrderGroup,
  type AutoOrderTeam,
  type GammaTeam,
} from "@/lib/api";
import {
  groupMarkets,
  formatDateHeader,
  localDateKey,
  isAmericanSport,
  get1X2,
  getSpreadMLTotal,
  type MatchGroup,
} from "@/lib/marketUtils";
import { parseUtcInstant } from "@/lib/kickoffTime";
import { polymarketEventUrl } from "@/lib/polymarketLinks";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { Bot, Plus, Settings2, Trash2, ExternalLink, RefreshCw } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;
const KICKOFF_GRACE_MS = 4 * 60 * 60 * 1000;

const CLOSED_MARKET_STATUSES = new Set([
  "closed",
  "resolved",
  "settled",
  "final",
  "finalized",
  "inactive",
  "cancelled",
  "canceled",
  "expired",
  "archived",
]);

function isOpenMarket(m: { status: string; startTime: string }): boolean {
  if (CLOSED_MARKET_STATUSES.has(m.status.toLowerCase())) return false;
  const ms = parseUtcInstant(m.startTime);
  if (ms == null) return true;
  return ms + KICKOFF_GRACE_MS > Date.now();
}

function isWithinNext7Days(startTime: string): boolean {
  const ms = parseUtcInstant(startTime);
  if (ms == null) return true;
  const now = Date.now();
  return ms >= now - KICKOFF_GRACE_MS && ms <= now + SEVEN_DAYS_MS;
}

function normTeam(s: string): string {
  return s.toLowerCase().trim();
}

function teamMatches(eventTeam: string, gt: AutoOrderTeam): boolean {
  const et = normTeam(eventTeam);
  if (et === normTeam(gt.name)) return true;
  if (gt.abbreviation && et === normTeam(gt.abbreviation)) return true;
  return false;
}

function groupMatchesMatch(g: AutoOrderGroup, match: MatchGroup): boolean {
  if (normTeam(match.league) !== normTeam(g.league)) return false;
  if (g.teams.length === 0) return false;
  const parts = match.name.split(" vs ").map((s) => s.trim());
  if (parts.length < 2) return false;
  const [t1, t2] = parts;
  return g.teams.some((t) => teamMatches(t1, t) || teamMatches(t2, t));
}

function newAutoOrderGroup(league: string): AutoOrderGroup {
  return {
    id: crypto.randomUUID(),
    name: "新分组",
    enabled: false,
    league: league.toLowerCase(),
    fundUsd: 50,
    teams: [],
    priceGate: { minCents: 55, maxCents: 75 },
    oddsBands: [{ minCents: 55, maxCents: 60, stakePct: 5 }],
    triggers: { minutesBeforeStart: 30, minEventVolumeUsd: 50000 },
  };
}

function OddsBadge({ price, label }: { price: number | null; label: string }) {
  if (price == null) {
    return (
      <div className="w-[80px] h-[48px] rounded-md border border-border flex items-center justify-center text-muted-foreground text-[11px]">
        —
      </div>
    );
  }
  return (
    <div className="w-[80px] h-[48px] rounded-md border border-border bg-surface flex flex-col items-center justify-center">
      <span className="text-[9px] font-mono text-muted-foreground truncate max-w-[72px]">{label}</span>
      <span className="text-[13px] font-semibold num">{(price * 100).toFixed(1)}¢</span>
    </div>
  );
}

function AutoOrderMatchRow({ group }: { group: MatchGroup }) {
  const { home, away } = get1X2(group);
  const { mlHome, mlAway } = getSpreadMLTotal(group);
  const american = isAmericanSport(group);
  const homeOutcome = american ? mlHome : home;
  const awayOutcome = american ? mlAway : away;
  const [team1, team2] = group.name.split(" vs ").map((s) => s.trim());
  const eventUrl = polymarketEventUrl(group.polySlug);

  return (
    <div className="grid grid-cols-[minmax(108px,auto)_1fr_auto] items-center gap-4 px-5 py-4 hover:bg-accent/30 transition-colors">
      <MatchTimeVolume startTime={group.startTime} eventVolume={group.eventVolume} />
      <a
        href={eventUrl ?? "#"}
        target={eventUrl ? "_blank" : undefined}
        rel={eventUrl ? "noopener noreferrer" : undefined}
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
          <span className="text-[13.5px] font-semibold truncate group-hover:text-brand transition-colors">
            {team1}
          </span>
          <span className="text-[13.5px] font-semibold text-muted-foreground/80 mt-0.5 truncate">{team2}</span>
        </div>
        {eventUrl && (
          <ExternalLink className="size-3 text-muted-foreground shrink-0 opacity-0 group-hover:opacity-100 transition-opacity" />
        )}
      </a>
      <div className="flex items-center gap-2">
        <OddsBadge price={homeOutcome?.polymarket?.impliedOdds ?? null} label={homeOutcome?.label ?? team1} />
        <OddsBadge price={awayOutcome?.polymarket?.impliedOdds ?? null} label={awayOutcome?.label ?? team2} />
      </div>
    </div>
  );
}

export function AutoOrderPage() {
  const { markets, loading: marketsLoading, refresh: refreshMarkets } = useMarketList();
  const { rows: configRows } = useConfig();
  const leagueTags = useMemo(() => {
    const raw = configRows.find((r) => r.key === "eventClassificationTags")?.value ?? "";
    const tags = parseEventClassificationTags(raw);
    return tags.length > 0 ? tags : DEFAULT_EVENT_CLASSIFICATION_TAGS;
  }, [configRows]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [cfg, setCfg] = useState<AutoOrderConfig | null>(null);
  const [dryRun, setDryRun] = useState(true);
  const [readOnly, setReadOnly] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [settingsGroupId, setSettingsGroupId] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await getAutoOrderConfig();
      const groups = resp.groups ?? [];
      setCfg({
        enabled: resp.enabled,
        dailyPool: resp.dailyPool,
        outcomePolicy: resp.outcomePolicy,
        groups,
      });
      setDryRun(resp.dryRun);
      setReadOnly(resp.readOnlyMode);
      setSelectedId((prev) => {
        if (prev && groups.some((g) => g.id === prev)) return prev;
        return groups[0]?.id ?? null;
      });
    } catch (err) {
      toast.error("加载失败", { description: err instanceof Error ? err.message : "未知错误" });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const selectedGroup = cfg?.groups.find((g) => g.id === selectedId) ?? null;
  const settingsGroup = cfg?.groups.find((g) => g.id === settingsGroupId) ?? null;

  const allMatchGroups = useMemo(
    () => groupMarkets(markets.filter(isOpenMarket)),
    [markets],
  );

  const groupMatches = useMemo(() => {
    if (!selectedGroup) return [];
    return allMatchGroups.filter(
      (m) =>
        isWithinNext7Days(m.startTime) &&
        groupMatchesMatch(selectedGroup, m),
    );
  }, [allMatchGroups, selectedGroup]);

  const matchesByDate = useMemo(() => {
    const byDate = new Map<string, MatchGroup[]>();
    for (const g of groupMatches) {
      const dk = localDateKey(g.startTime);
      if (!byDate.has(dk)) byDate.set(dk, []);
      byDate.get(dk)!.push(g);
    }
    for (const list of byDate.values()) {
      list.sort(
        (a, b) => (parseUtcInstant(a.startTime) ?? 0) - (parseUtcInstant(b.startTime) ?? 0),
      );
    }
    return byDate;
  }, [groupMatches]);

  function updateGroup(id: string, patch: Partial<AutoOrderGroup>) {
    setCfg((prev) =>
      prev
        ? {
            ...prev,
            groups: prev.groups.map((g) => (g.id === id ? { ...g, ...patch } : g)),
          }
        : prev,
    );
  }

  async function persistConfig(nextCfg: AutoOrderConfig, nextDryRun = dryRun) {
    setSaving(true);
    try {
      const resp = await putAutoOrderConfig({
        ...nextCfg,
        enabled: nextCfg.groups.some((g) => g.enabled),
        dryRun: nextDryRun,
      });
      setCfg({
        enabled: resp.enabled,
        dailyPool: resp.dailyPool,
        outcomePolicy: resp.outcomePolicy,
        groups: resp.groups,
      });
      setDryRun(resp.dryRun);
      return true;
    } catch (err) {
      toast.error("保存失败", { description: err instanceof Error ? err.message : "校验未通过" });
      return false;
    } finally {
      setSaving(false);
    }
  }

  async function handleSaveGroupSettings() {
    if (!cfg) return;
    const ok = await persistConfig(cfg);
    if (ok) {
      toast.success("已保存", { description: "分组配置" });
      setSettingsGroupId(null);
    }
  }

  async function handleToggleEnabled(groupId: string, enabled: boolean) {
    if (!cfg) return;
    const next = {
      ...cfg,
      groups: cfg.groups.map((g) => (g.id === groupId ? { ...g, enabled } : g)),
    };
    setCfg(next);
    await persistConfig(next);
  }

  async function handleDeleteGroup(groupId: string) {
    if (!cfg) return;
    const next = { ...cfg, groups: cfg.groups.filter((g) => g.id !== groupId) };
    setCfg(next);
    if (selectedId === groupId) setSelectedId(next.groups[0]?.id ?? null);
    if (settingsGroupId === groupId) setSettingsGroupId(null);
    await persistConfig(next);
  }

  async function handleCreateGroup() {
    if (!cfg) return;
    const league = leagueTags[0] ?? "nba";
    const g = newAutoOrderGroup(league);
    const next = { ...cfg, groups: [...cfg.groups, g] };
    setCfg(next);
    setSelectedId(g.id);
    setSettingsGroupId(g.id);
  }

  if (loading || !cfg) {
    return (
      <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
        <TopBar title="自动下单" subtitle="分组策略与触发" />
        <div className="flex-1 p-6 text-muted-foreground text-[13px]">加载自动下单配置...</div>
      </div>
    );
  }

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      <TopBar
        title="自动下单"
        subtitle={
          <>
            <span>未来 7 天赛事</span>
            {readOnly && (
              <>
                <span className="text-border">·</span>
                <span className="text-warning">只读模式</span>
              </>
            )}
          </>
        }
        actions={
          <label className="inline-flex items-center gap-2 h-8 px-3 rounded-md border border-border text-[11px] text-amber-200/90 cursor-pointer">
            <input
              type="checkbox"
              checked={dryRun}
              disabled={readOnly || saving}
              onChange={async (e) => {
                const v = e.target.checked;
                setDryRun(v);
                if (cfg) await persistConfig(cfg, v);
              }}
            />
            模拟模式
          </label>
        }
      />

      <div className="flex flex-1 min-h-0 overflow-hidden">
        {/* 左栏：分组 */}
        <aside className="w-[200px] shrink-0 border-r border-border flex flex-col min-h-0 overflow-hidden bg-background">
          <div className="p-3 shrink-0 flex items-center justify-between border-b border-border">
            <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">分组</span>
            <button
              type="button"
              onClick={() => void handleCreateGroup()}
              disabled={readOnly || saving}
              className="inline-flex items-center gap-1 h-7 px-2 rounded-md text-[11px] text-brand hover:bg-brand/10 transition disabled:opacity-50"
            >
              <Plus className="size-3.5" />
              新建
            </button>
          </div>

          <nav className="flex-1 min-h-0 overflow-y-auto scrollbar-thin px-2 py-2 space-y-1">
            {cfg.groups.length === 0 ? (
              <div className="px-3 py-6 text-center">
                <Bot className="size-6 text-muted-foreground/40 mx-auto mb-2" />
                <p className="text-[11px] text-muted-foreground">暂无分组</p>
              </div>
            ) : (
              cfg.groups.map((g) => {
                const active = g.id === selectedId;
                return (
                  <div
                    key={g.id}
                    className={cn(
                      "rounded-md border transition-all duration-200",
                      active
                        ? "border-brand/40 bg-brand/5"
                        : "border-transparent hover:border-border hover:bg-accent/40",
                    )}
                  >
                    <button
                      type="button"
                      onClick={() => setSelectedId(g.id)}
                      className="w-full text-left px-3 py-2.5"
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className={cn("text-[13px] font-medium truncate", active && "text-brand")}>
                          {g.name}
                        </span>
                        <span
                          className={cn(
                            "size-1.5 rounded-full shrink-0",
                            g.enabled ? "bg-success" : "bg-muted-foreground/40",
                          )}
                          title={g.enabled ? "已启用" : "未启用"}
                        />
                      </div>
                      <div className="text-[10px] text-muted-foreground mt-0.5 font-mono">
                        {g.league.toUpperCase()} · {g.fundUsd} pusd · {g.teams.length} 队
                      </div>
                    </button>
                    <div className="flex items-center gap-1 px-2 pb-2">
                      <button
                        type="button"
                        title="设置"
                        onClick={() => {
                          setSelectedId(g.id);
                          setSettingsGroupId(g.id);
                        }}
                        className="inline-flex items-center justify-center size-7 rounded-md hover:bg-brand/10 text-muted-foreground hover:text-brand transition"
                      >
                        <Settings2 className="size-3.5" />
                      </button>
                      <button
                        type="button"
                        title="删除"
                        disabled={readOnly || saving}
                        onClick={() => void handleDeleteGroup(g.id)}
                        className="inline-flex items-center justify-center size-7 rounded-md hover:bg-destructive/10 text-muted-foreground hover:text-red-400 transition disabled:opacity-50"
                      >
                        <Trash2 className="size-3.5" />
                      </button>
                    </div>
                  </div>
                );
              })
            )}
          </nav>
        </aside>

        {/* 右栏：当前分组赛事 */}
        <div className="flex-1 min-w-0 min-h-0 overflow-y-auto overflow-x-hidden scrollbar-thin px-2 py-6 animate-slide-up">
          {!selectedGroup ? (
            <div className="text-center py-16 text-muted-foreground">
              <Bot className="size-8 mx-auto mb-3 opacity-40" />
              <p className="text-[13px]">选择或新建分组</p>
            </div>
          ) : selectedGroup.teams.length === 0 ? (
            <div className="text-center py-16 text-muted-foreground">
              <p className="text-[13px]">「{selectedGroup.name}」尚未配置监控球队</p>
              <button
                type="button"
                onClick={() => setSettingsGroupId(selectedGroup.id)}
                className="mt-3 inline-flex items-center gap-1.5 h-8 px-3 rounded-md bg-brand/10 text-brand text-[12px] font-medium hover:bg-brand/20"
              >
                <Settings2 className="size-3.5" />
                打开设置
              </button>
            </div>
          ) : marketsLoading ? (
            <div className="text-center py-12 text-muted-foreground">
              <RefreshCw className="size-6 mx-auto mb-3 animate-spin opacity-60" />
              <p className="text-[12px]">正在同步市场数据…</p>
            </div>
          ) : matchesByDate.size === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <p className="text-[12px]">未来 7 天内暂无符合「{selectedGroup.name}」规则的赛事</p>
              <button
                type="button"
                onClick={() => void refreshMarkets()}
                className="mt-3 text-[11px] text-brand hover:underline"
              >
                刷新市场
              </button>
            </div>
          ) : (
            Array.from(matchesByDate.entries())
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([dateKey, dateGroups]) => (
                <div key={dateKey} className="mb-8">
                  <div className="flex items-center gap-3 mb-3">
                    <h2 className="text-[14px] font-semibold tracking-tight">{formatDateHeader(dateKey)}</h2>
                    <span className="text-[11px] text-muted-foreground font-mono">{dateGroups.length} 场</span>
                    <div className="flex-1 h-px bg-border" />
                    <span className="text-[10.5px] uppercase tracking-[0.18em] text-muted-foreground">独赢</span>
                  </div>
                  <div className="rounded-xl border border-border surface divide-y divide-border overflow-hidden">
                    {dateGroups.map((g) => (
                      <AutoOrderMatchRow
                        key={`${g.league}-${g.name}-${g.startTime}`}
                        group={g}
                      />
                    ))}
                  </div>
                </div>
              ))
          )}
        </div>
      </div>

      {settingsGroup && cfg && (
        <GroupSettingsDialog
          group={settingsGroup}
          allGroups={cfg.groups}
          leagueTags={leagueTags}
          open={settingsGroupId !== null}
          readOnly={readOnly}
          saving={saving}
          onOpenChange={(open) => {
            if (!open) setSettingsGroupId(null);
          }}
          onUpdate={(patch) => updateGroup(settingsGroup.id, patch)}
          onToggleEnabled={(enabled) => void handleToggleEnabled(settingsGroup.id, enabled)}
          onSave={() => void handleSaveGroupSettings()}
        />
      )}
    </div>
  );
}

function GroupSettingsDialog({
  group,
  allGroups,
  leagueTags,
  open,
  readOnly,
  saving,
  onOpenChange,
  onUpdate,
  onToggleEnabled,
  onSave,
}: {
  group: AutoOrderGroup;
  allGroups: AutoOrderGroup[];
  leagueTags: string[];
  open: boolean;
  readOnly: boolean;
  saving: boolean;
  onOpenChange: (open: boolean) => void;
  onUpdate: (patch: Partial<AutoOrderGroup>) => void;
  onToggleEnabled: (enabled: boolean) => void;
  onSave: () => void;
}) {
  const [teamSearch, setTeamSearch] = useState("");
  const [leagueTeams, setLeagueTeams] = useState<GammaTeam[]>([]);
  const [teamsLoading, setTeamsLoading] = useState(false);

  const leagueOptions = useMemo(() => {
    const lg = group.league.trim().toLowerCase();
    if (lg && !leagueTags.includes(lg)) {
      return [...leagueTags, lg];
    }
    return leagueTags;
  }, [leagueTags, group.league]);

  const teamsInOtherGroups = (() => {
    const ids = new Set<number>();
    allGroups.forEach((g) => {
      if (g.id === group.id) return;
      g.teams.forEach((t) => ids.add(t.id));
    });
    return ids;
  })();

  useEffect(() => {
    if (!open || !group.league.trim()) return;
    setTeamsLoading(true);
    getTeams(group.league)
      .then(setLeagueTeams)
      .catch((err) => {
        toast.error("球队列表加载失败", { description: err instanceof Error ? err.message : "" });
        setLeagueTeams([]);
      })
      .finally(() => setTeamsLoading(false));
  }, [open, group.league]);

  function handleLeagueChange(nextLeague: string) {
    const league = nextLeague.toLowerCase();
    if (league === group.league.toLowerCase()) return;
    onUpdate({ league, teams: [] });
    setTeamSearch("");
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="text-[15px]">分组设置 · {group.name}</DialogTitle>
          <DialogDescription className="text-[12px]">
            配置该分组的启用状态、资金额度、下单规则与金额
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-5">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-[11px] text-muted-foreground">名称</label>
              <input
                value={group.name}
                disabled={readOnly}
                onChange={(e) => onUpdate({ name: e.target.value })}
                className="mt-1 w-full h-9 px-2 rounded-md border border-border bg-background text-[12px] disabled:opacity-50"
              />
            </div>
            <div>
              <label className="text-[11px] text-muted-foreground">联赛</label>
              {leagueOptions.length === 0 ? (
                <p className="mt-1 text-[11px] text-muted-foreground">
                  请先在「设置 → 分类」中添加联赛
                </p>
              ) : (
                <select
                  value={group.league.toLowerCase()}
                  disabled={readOnly}
                  onChange={(e) => handleLeagueChange(e.target.value)}
                  className="mt-1 w-full h-9 px-2 rounded-md border border-border bg-background text-[12px] disabled:opacity-50"
                >
                  {leagueOptions.map((tag) => (
                    <option key={tag} value={tag}>
                      {tag.toUpperCase()}
                    </option>
                  ))}
                </select>
              )}
            </div>
          </div>

          <div className="flex items-center justify-between rounded-lg border border-border p-3">
            <div>
              <p className="text-[12px] font-medium">启用分组</p>
              <p className="text-[10px] text-muted-foreground mt-0.5">开启后该分组才会自动下单</p>
            </div>
            <button
              type="button"
              disabled={readOnly || saving}
              onClick={() => onToggleEnabled(!group.enabled)}
              className={cn(
                "relative w-11 h-6 rounded-full transition-colors disabled:opacity-50",
                group.enabled ? "bg-brand" : "bg-muted",
              )}
            >
              <span
                className={cn(
                  "absolute top-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform",
                  group.enabled ? "left-[22px]" : "left-0.5",
                )}
              />
            </button>
          </div>

          <section className="space-y-3">
            <h4 className="text-[12px] font-semibold border-b border-border pb-2">下单金额</h4>
            <div>
              <label className="text-[11px] text-muted-foreground">可用资金额度 (pusd)</label>
              <input
                type="number"
                min={0}
                step={1}
                disabled={readOnly}
                value={group.fundUsd}
                onChange={(e) => onUpdate({ fundUsd: Number(e.target.value) })}
                className="mt-1 w-full h-9 px-2 rounded-md border border-border bg-background text-[12px] disabled:opacity-50"
              />
              <p className="text-[10px] text-muted-foreground mt-1">该分组每日可用 USDC 上限</p>
            </div>

            <div>
              <div className="text-[11px] font-medium text-muted-foreground mb-2">赔率区间 → 下单比例</div>
              {group.oddsBands.map((b, i) => (
                <div key={i} className="flex gap-2 mb-2 items-center">
                  <input
                    type="number"
                    placeholder="min¢"
                    disabled={readOnly}
                    value={b.minCents}
                    onChange={(e) => {
                      const bands = [...group.oddsBands];
                      bands[i] = { ...b, minCents: Number(e.target.value) };
                      onUpdate({ oddsBands: bands });
                    }}
                    className="w-20 h-8 px-2 rounded border border-border bg-background text-[11px] disabled:opacity-50"
                  />
                  <span className="text-muted-foreground text-[11px]">–</span>
                  <input
                    type="number"
                    placeholder="max¢"
                    disabled={readOnly}
                    value={b.maxCents}
                    onChange={(e) => {
                      const bands = [...group.oddsBands];
                      bands[i] = { ...b, maxCents: Number(e.target.value) };
                      onUpdate({ oddsBands: bands });
                    }}
                    className="w-20 h-8 px-2 rounded border border-border bg-background text-[11px] disabled:opacity-50"
                  />
                  <input
                    type="number"
                    placeholder="stake%"
                    disabled={readOnly}
                    value={b.stakePct}
                    onChange={(e) => {
                      const bands = [...group.oddsBands];
                      bands[i] = { ...b, stakePct: Number(e.target.value) };
                      onUpdate({ oddsBands: bands });
                    }}
                    className="w-20 h-8 px-2 rounded border border-border bg-background text-[11px] disabled:opacity-50"
                  />
                  <span className="text-[10px] text-muted-foreground">%</span>
                  {!readOnly && (
                    <button
                      type="button"
                      className="text-[10px] text-red-400"
                      onClick={() => onUpdate({ oddsBands: group.oddsBands.filter((_, j) => j !== i) })}
                    >
                      删
                    </button>
                  )}
                </div>
              ))}
              {!readOnly && (
                <button
                  type="button"
                  className="text-[11px] text-brand hover:underline"
                  onClick={() =>
                    onUpdate({
                      oddsBands: [
                        ...group.oddsBands,
                        {
                          minCents: group.priceGate.minCents,
                          maxCents: group.priceGate.maxCents,
                          stakePct: 5,
                        },
                      ],
                    })
                  }
                >
                  + 添加区间
                </button>
              )}
            </div>
          </section>

          <section className="space-y-3">
            <h4 className="text-[12px] font-semibold border-b border-border pb-2">下单规则</h4>

            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="text-[11px] text-muted-foreground">价格下限 ¢</label>
                <input
                  type="number"
                  disabled={readOnly}
                  value={group.priceGate.minCents}
                  onChange={(e) =>
                    onUpdate({
                      priceGate: { ...group.priceGate, minCents: Number(e.target.value) },
                    })
                  }
                  className="mt-1 w-full h-9 px-2 rounded-md border border-border bg-background text-[12px] disabled:opacity-50"
                />
              </div>
              <div>
                <label className="text-[11px] text-muted-foreground">价格上限 ¢</label>
                <input
                  type="number"
                  disabled={readOnly}
                  value={group.priceGate.maxCents}
                  onChange={(e) =>
                    onUpdate({
                      priceGate: { ...group.priceGate, maxCents: Number(e.target.value) },
                    })
                  }
                  className="mt-1 w-full h-9 px-2 rounded-md border border-border bg-background text-[12px] disabled:opacity-50"
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="text-[11px] text-muted-foreground">开赛前分钟</label>
                <input
                  type="number"
                  disabled={readOnly}
                  value={group.triggers.minutesBeforeStart}
                  onChange={(e) =>
                    onUpdate({
                      triggers: {
                        ...group.triggers,
                        minutesBeforeStart: Number(e.target.value),
                      },
                    })
                  }
                  className="mt-1 w-full h-9 px-2 rounded-md border border-border bg-background text-[12px] disabled:opacity-50"
                />
              </div>
              <div>
                <label className="text-[11px] text-muted-foreground">最小 eventVolume USD</label>
                <input
                  type="number"
                  disabled={readOnly}
                  value={group.triggers.minEventVolumeUsd}
                  onChange={(e) =>
                    onUpdate({
                      triggers: {
                        ...group.triggers,
                        minEventVolumeUsd: Number(e.target.value),
                      },
                    })
                  }
                  className="mt-1 w-full h-9 px-2 rounded-md border border-border bg-background text-[12px] disabled:opacity-50"
                />
              </div>
            </div>

            <div>
              <label className="text-[11px] font-medium text-muted-foreground mb-2 block">
                监控球队
                <span className="ml-1 font-normal text-muted-foreground/80">
                  · {group.league.toUpperCase()}
                </span>
              </label>
              <input
                value={teamSearch}
                disabled={readOnly}
                onChange={(e) => setTeamSearch(e.target.value)}
                placeholder="搜索并添加球队..."
                className="w-full h-9 px-2 mb-2 rounded border border-border bg-background text-[12px] disabled:opacity-50"
              />
              <div className="flex flex-wrap gap-2 mb-2">
                {group.teams.map((t) => (
                  <span
                    key={t.id}
                    className="inline-flex items-center gap-1 rounded-full border border-border px-2 py-1 text-[11px]"
                  >
                    {t.name}
                    {!readOnly && (
                      <button
                        type="button"
                        onClick={() =>
                          onUpdate({
                            teams: group.teams.filter((x) => x.id !== t.id),
                          })
                        }
                      >
                        ×
                      </button>
                    )}
                  </span>
                ))}
              </div>
              <div className="max-h-36 overflow-y-auto rounded-md border border-border divide-y divide-border/50">
                {teamsLoading && <p className="text-[11px] text-muted-foreground p-2">加载中...</p>}
                {!teamsLoading &&
                  leagueTeams
                    .filter((t) => t.name.toLowerCase().includes(teamSearch.toLowerCase()))
                    .filter((t) => !group.teams.some((x) => x.id === t.id))
                    .filter((t) => !teamsInOtherGroups.has(t.id))
                    .slice(0, 20)
                    .map((t) => (
                      <button
                        key={t.id}
                        type="button"
                        disabled={readOnly}
                        className="w-full text-left px-3 py-2 hover:bg-accent text-[12px] disabled:opacity-50"
                        onClick={() => {
                          onUpdate({
                            teams: [
                              ...group.teams,
                              { id: t.id, name: t.name, abbreviation: t.abbreviation },
                            ],
                          });
                          setTeamSearch("");
                        }}
                      >
                        {t.name} {t.abbreviation ? `(${t.abbreviation})` : ""}
                      </button>
                    ))}
              </div>
            </div>
          </section>
        </div>

        <DialogFooter className="gap-2">
          <button
            type="button"
            onClick={() => onOpenChange(false)}
            className="h-9 px-4 rounded-md border border-border text-[12px] hover:bg-accent"
          >
            取消
          </button>
          <button
            type="button"
            disabled={readOnly || saving}
            onClick={onSave}
            className="h-9 px-4 rounded-md bg-brand text-brand-foreground text-[12px] font-semibold hover:opacity-90 disabled:opacity-50"
          >
            {saving ? "保存中..." : "保存"}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
