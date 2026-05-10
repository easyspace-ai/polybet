import { useState, useEffect, useCallback, Fragment } from 'react';
import { getConfig, putConfig, type ConfigRow } from '../lib/api';
import { toast } from '../components/ui/use-toast';
import { cn } from '../lib/utils';
import { useOddsFormat } from '../hooks/useOddsFormat';
import { type OddsFormat } from '../lib/oddsFormat';
import { Trash2 } from 'lucide-react';
import {
  DEFAULT_EVENT_CLASSIFICATION_TAGS,
  parseEventClassificationTags,
} from '../lib/eventClassification';

const ODDS_FORMAT_OPTIONS: { value: OddsFormat; label: string }[] = [
  { value: 'decimal', label: '欧赔' },
  { value: 'american', label: '美式' },
  { value: 'percent', label: '概率' },
];

const RESERVED_CONFIG_KEYS = new Set([
  'httpPlatformProxyUrl',
  'telegramBotToken',
  'telegramAuthorizedChatId',
  'eventClassificationTags',
  'priceStopLossRanges',
]);

const SUGGESTED_LEAGUE_TAGS = ['NBA', 'NCAAB', 'NHL', 'EPL', 'MLS', 'UCL', 'MLB'];

export interface PriceStopLossRangeRow {
  id: string;
  name: string;
  minCents: number;
  maxCents: number;
  fundPct: number;
  stopLossPct: number;
}

const DEFAULT_PRICE_ROWS: PriceStopLossRangeRow[] = [
  { id: 'r1', name: '20-30¢', minCents: 20, maxCents: 30, fundPct: 17, stopLossPct: 20 },
  { id: 'r2', name: '30-40¢', minCents: 30, maxCents: 40, fundPct: 17, stopLossPct: 20 },
  { id: 'r3', name: '40-50¢', minCents: 40, maxCents: 50, fundPct: 17, stopLossPct: 20 },
  { id: 'r4', name: '50-60¢', minCents: 50, maxCents: 60, fundPct: 17, stopLossPct: 20 },
  { id: 'r5', name: '60-70¢', minCents: 60, maxCents: 70, fundPct: 16, stopLossPct: 20 },
  { id: 'r6', name: '70-80¢', minCents: 70, maxCents: 80, fundPct: 16, stopLossPct: 20 },
];

type SettingsTab = 'general' | 'proxy' | 'telegram' | 'tags' | 'prices';

const TABS: { id: SettingsTab; label: string }[] = [
  { id: 'general', label: '通用 / 路由' },
  { id: 'proxy', label: '代理' },
  { id: 'telegram', label: '电报' },
  { id: 'tags', label: '赛事分类' },
  { id: 'prices', label: '价格区间' },
];

function OddsFormatToggle() {
  const [format, setFormat] = useOddsFormat();
  return (
    <div
      className="mb-4 rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-el px-3.5 py-3"
    >
      <div className="font-mono text-[11px] font-semibold text-tm-tx">赔率显示</div>
      <div className="mt-1 font-mono text-[10px] leading-[1.5] text-tm-tx-mut">
        控制看板中赔率的展示方式：欧洲盘（如 2.06）、美式盘（如 +106 / -120）或隐含概率（如
        48.5%）。仅保存在本机浏览器，不影响机器人实际行为。
      </div>
      <div className="mt-2.5 inline-flex rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-sunk overflow-hidden">
        {ODDS_FORMAT_OPTIONS.map((opt) => (
          <button
            key={opt.value}
            type="button"
            onClick={() => setFormat(opt.value)}
            className={cn(
              'px-3 py-1.5 font-mono text-[10px] font-bold tracking-wider transition-colors',
              format === opt.value
                ? 'bg-tm-accent text-black'
                : 'text-tm-tx-dim hover:text-tm-tx hover:bg-tm-bg-el',
            )}
          >
            {opt.label}
          </button>
        ))}
      </div>
    </div>
  );
}

const KEY_DESCRIPTIONS: Record<string, string> = {
  maxTradeSize:
    '单笔交易金额上限。路由在拆单前会先比对请求规模与本值；超出则直接拒绝（size_exceeds_max），不会进行盘口遍历、资金分配或链上调用。',
  slippageTolerance:
    '允许的最优盘口价与实际成交量加权均价之间的最大偏离。路由合并 Polymarket 各档深度并撮合后，若偏离超过本阈值则中止（slippage_exceeded），写入失败记录并告警 Telegram，且不会提交订单。',
  pollingInterval:
    '市场同步循环从 Polymarket 拉取报价的间隔（毫秒）。更短 = 盘口更新更及时，但 API 压力更大。',
  orderBookLevels:
    '投注单 / 交易面板中，实时推送的 Polymarket 盘口档位数。越大可见深度越多，经 WebSocket 传输的数据也越多。范围 3–25。',
  polymarketFokBuyExtraTicks:
    'Polymarket FOK 买入：在最优卖价（best ask）之上额外允许的 tick 档数，用于放宽限价，减少「无法完全成交」被拒。路由仍会先按 slippageTolerance 约束计划价；此处在盘口侧再抬高上限。整数 0–50，默认 5。',
  polymarketFokSellExtraTicks:
    'Polymarket FOK 卖出（含风控平仓）：在最优买价（best bid）之下额外放宽的 tick 档数（与原先固定 10 tick 思路相同，可配置）。整数 0–50，默认 5。',
  minOpenRiskShares:
    '风控列表与 CLOB 余额对账：仅保留份额 ≥ 本值的持仓（默认 1）。低于该值的链上余额会对应关闭本地仓位；可略大于 1 用于过滤极小仓位。必须为正数，≤ 1000000。',
};

function rowValue(rows: ConfigRow[], key: string): string {
  return rows.find((r) => r.key === key)?.value ?? '';
}

function parsePriceRowsJson(raw: string): PriceStopLossRangeRow[] {
  if (!raw.trim()) return DEFAULT_PRICE_ROWS.map((r) => ({ ...r }));
  try {
    const p = JSON.parse(raw) as unknown;
    if (!Array.isArray(p) || p.length === 0) return DEFAULT_PRICE_ROWS.map((r) => ({ ...r }));
    return p.map((row: unknown, i: number) => {
      const o = row as Record<string, unknown>;
      return {
        id: typeof o.id === 'string' && o.id ? o.id : `r${i + 1}`,
        name: String(o.name ?? ''),
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

export function Settings() {
  const [tab, setTab] = useState<SettingsTab>('general');
  const [rows, setRows] = useState<ConfigRow[]>([]);
  const [edited, setEdited] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const [proxyDraft, setProxyDraft] = useState('');
  const [tgTokenDraft, setTgTokenDraft] = useState('');
  const [tgChatDraft, setTgChatDraft] = useState('');
  const [tags, setTags] = useState<string[]>([...DEFAULT_EVENT_CLASSIFICATION_TAGS]);
  const [tagInput, setTagInput] = useState('');
  const [priceRows, setPriceRows] = useState<PriceStopLossRangeRow[]>(() =>
    DEFAULT_PRICE_ROWS.map((r) => ({ ...r })),
  );

  const reload = useCallback((options?: { silent?: boolean }) => {
    const silent = options?.silent;
    if (!silent) setLoading(true);
    return getConfig()
      .then((data) => {
        setRows(data);
        setError(null);
        setProxyDraft(data.find((r) => r.key === 'httpPlatformProxyUrl')?.value ?? '');
        setTgTokenDraft(data.find((r) => r.key === 'telegramBotToken')?.value ?? '');
        setTgChatDraft(data.find((r) => r.key === 'telegramAuthorizedChatId')?.value ?? '');
        setTags(parseEventClassificationTags(rowValue(data, 'eventClassificationTags')));
        setPriceRows(parsePriceRowsJson(rowValue(data, 'priceStopLossRanges')));
        setEdited({});
      })
      .catch((err) => setError(err instanceof Error ? err.message : '加载配置失败'))
      .finally(() => {
        if (!silent) setLoading(false);
      });
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  function getValue(key: string) {
    return key in edited ? edited[key] : (rows.find((r) => r.key === key)?.value ?? '');
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
      toast({ title: '已保存', description: `已更新 ${key}`, variant: 'success' });
    } catch (err) {
      toast({
        title: '保存失败',
        description: err instanceof Error ? err.message : '未知错误',
        variant: 'destructive',
      });
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
      toast({ title: '已保存', description: label, variant: 'success' });
    } catch (err) {
      toast({
        title: '保存失败',
        description: err instanceof Error ? err.message : '未知错误',
        variant: 'destructive',
      });
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
    setTagInput('');
  }

  function removeTag(t: string) {
    setTags((prev) => prev.filter((x) => x !== t));
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-tm-bg">
      <div className="h-10 shrink-0 flex items-center gap-4 px-4 bg-tm-bg border-b border-tm-bd">
        <span className="font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-dim">
          配置
        </span>
        <span className="font-mono text-[10px] px-1.5 py-0.5 rounded-sm bg-tm-bg-el border border-tm-bd text-tm-tx-dim">
          {rows.length} 项
        </span>
      </div>

      <div className="shrink-0 border-b border-tm-bd bg-tm-bg px-4 py-2 flex flex-wrap gap-1">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            className={cn(
              'rounded-sm px-2.5 py-1 font-mono text-[10px] font-semibold tracking-wider transition-colors',
              tab === t.id
                ? 'bg-tm-accent text-black'
                : 'border border-tm-bd bg-tm-bg-el text-tm-tx-dim hover:text-tm-tx',
            )}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div className="flex-1 min-h-0 overflow-auto p-5">
        <div className="max-w-[720px]">
          {error && (
            <div className="mb-3 rounded-sm border border-tm-neg/30 bg-tm-neg/10 px-3 py-2 font-mono text-[11px] text-tm-neg">
              {error}
            </div>
          )}

          {loading && (
            <div className="font-mono text-[11px] text-tm-tx-mut tracking-wider">加载中…</div>
          )}

          {!loading && tab === 'general' && (
            <>
              <div className="mb-3 font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-dim">
                显示
              </div>
              <OddsFormatToggle />

              <div className="mb-4 rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-el px-3.5 py-3">
                <div className="font-mono text-[11px] font-semibold text-tm-tx">配置文件</div>
                <div className="mt-1 font-mono text-[10px] leading-[1.5] text-tm-tx-mut">
                  服务端将机器人参数与{' '}
                  <span className="text-tm-tx">~/.polybet/bot-settings.json</span>{' '}
                  同步：进程启动时从该文件读入并覆盖数据库中同名键，每次在设置里保存后重写完整快照（文件权限 0600）。也可直接编辑该
                  JSON 后重启服务。
                </div>
              </div>

              {generalRows.length === 0 && !error && (
                <div className="font-mono text-[11px] text-tm-tx-mut tracking-wider">
                  未找到可编辑的通用配置项
                </div>
              )}

              {generalRows.length > 0 && (
                <>
                  <div className="mb-3 font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-dim">
                    机器人参数
                  </div>
                  {generalRows.map((row) => {
                    const isDirty = row.key in edited && edited[row.key] !== row.value;
                    const isSaving = saving === row.key;
                    return (
                      <div
                        key={row.key}
                        className="mb-1.5 grid items-center rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-el px-3.5 py-3.5"
                        style={{ gridTemplateColumns: '1fr 180px 60px', columnGap: 12 }}
                      >
                        <div className="min-w-0">
                          <div className="font-mono text-[11px] font-semibold text-tm-tx break-all">
                            {row.key}
                          </div>
                          {KEY_DESCRIPTIONS[row.key] && (
                            <div className="mt-1 pr-3 font-mono text-[10px] leading-[1.5] text-tm-tx-mut">
                              {KEY_DESCRIPTIONS[row.key]}
                            </div>
                          )}
                        </div>

                        <div className="flex items-center gap-1.5 rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-sunk px-2.5 py-1.5 focus-within:border-tm-accent">
                          <input
                            value={getValue(row.key)}
                            onChange={(e) =>
                              setEdited((prev) => ({ ...prev, [row.key]: e.target.value }))
                            }
                            className="min-w-0 flex-1 bg-transparent font-mono text-[13px] font-semibold text-tm-tx outline-none"
                          />
                        </div>

                        <button
                          type="button"
                          onClick={() => handleSave(row.key)}
                          disabled={!isDirty || isSaving}
                          className={cn(
                            'rounded-[var(--tm-rad)] py-1.5 font-mono text-[10px] font-bold tracking-wider transition-colors',
                            isDirty && !isSaving
                              ? 'bg-tm-accent text-black hover:bg-tm-accent/90'
                              : 'bg-tm-bg-sunk text-tm-tx-mut cursor-not-allowed',
                          )}
                        >
                          {isSaving ? '…' : '保存'}
                        </button>
                      </div>
                    );
                  })}
                </>
              )}
            </>
          )}

          {!loading && tab === 'proxy' && (
            <div className="rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-el px-3.5 py-3.5 space-y-3">
              <div>
                <div className="font-mono text-[11px] font-semibold text-tm-tx">HTTP(S) 代理地址</div>
                <p className="mt-1 font-mono text-[10px] leading-[1.55] text-tm-tx-mut">
                  与 <span className="text-tm-tx-dim">HTTP_PLATFORM_PROXY_URL</span>{' '}
                  相同语义：非空时覆盖 embeddedEnv 中的代理设置，经 CONNECT 转发 Polymarket 等出站请求。保存后立即生效。
                </p>
              </div>
              <input
                value={proxyDraft}
                onChange={(e) => setProxyDraft(e.target.value)}
                placeholder="https://user:pass@host:port 或留空使用 embeddedEnv 默认值"
                className="w-full rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-sunk px-2.5 py-2 font-mono text-[12px] text-tm-tx outline-none focus:border-tm-accent"
              />
              <button
                type="button"
                disabled={saving === 'httpPlatformProxyUrl' || proxyDraft === rowValue(rows, 'httpPlatformProxyUrl')}
                onClick={() => void saveStandalone('httpPlatformProxyUrl', proxyDraft, '代理地址')}
                className={cn(
                  'w-full rounded-[var(--tm-rad)] py-2 font-mono text-[10px] font-bold tracking-wider',
                  saving === 'httpPlatformProxyUrl' || proxyDraft === rowValue(rows, 'httpPlatformProxyUrl')
                    ? 'bg-tm-bg-sunk text-tm-tx-mut cursor-not-allowed'
                    : 'bg-tm-accent text-black hover:bg-tm-accent/90',
                )}
              >
                {saving === 'httpPlatformProxyUrl' ? '保存中…' : '保存代理'}
              </button>
            </div>
          )}

          {!loading && tab === 'telegram' && (
            <div className="space-y-4">
              <div className="rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-el px-3.5 py-3.5 space-y-3">
                <p className="font-mono text-[10px] leading-[1.55] text-tm-tx-mut">
                  对应 <span className="text-tm-tx-dim">TELEGRAM_BOT_TOKEN</span> 与{' '}
                  <span className="text-tm-tx-dim">TELEGRAM_AUTHORIZED_CHAT_ID</span>。
                  此处非空时优先于 embeddedEnv。修改 Token 后需重启进程才能重连 Bot。
                </p>
                <div>
                  <label className="block font-mono text-[10px] font-semibold text-tm-tx-dim mb-1">
                    Bot Token
                  </label>
                  <input
                    type="password"
                    autoComplete="off"
                    value={tgTokenDraft}
                    onChange={(e) => setTgTokenDraft(e.target.value)}
                    className="w-full rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-sunk px-2.5 py-2 font-mono text-[12px] text-tm-tx outline-none focus:border-tm-accent"
                  />
                </div>
                <div>
                  <label className="block font-mono text-[10px] font-semibold text-tm-tx-dim mb-1">
                    Authorized Chat ID
                  </label>
                  <input
                    value={tgChatDraft}
                    onChange={(e) => setTgChatDraft(e.target.value)}
                    className="w-full rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-sunk px-2.5 py-2 font-mono text-[12px] text-tm-tx outline-none focus:border-tm-accent"
                  />
                </div>
                <button
                  type="button"
                  disabled={
                    saving === 'telegram' ||
                    (tgTokenDraft === rowValue(rows, 'telegramBotToken')
                      && tgChatDraft === rowValue(rows, 'telegramAuthorizedChatId'))
                  }
                  onClick={async () => {
                    setSaving('telegram');
                    try {
                      await putConfig('telegramBotToken', tgTokenDraft);
                      await putConfig('telegramAuthorizedChatId', tgChatDraft);
                      await reload({ silent: true });
                      toast({ title: '已保存', description: '电报配置', variant: 'success' });
                    } catch (err) {
                      toast({
                        title: '保存失败',
                        description: err instanceof Error ? err.message : '未知错误',
                        variant: 'destructive',
                      });
                    } finally {
                      setSaving(null);
                    }
                  }}
                  className={cn(
                    'w-full rounded-[var(--tm-rad)] py-2 font-mono text-[10px] font-bold tracking-wider',
                    saving === 'telegram' ? 'bg-tm-bg-sunk text-tm-tx-mut' : 'bg-tm-accent text-black hover:bg-tm-accent/90',
                  )}
                >
                  {saving === 'telegram' ? '保存中…' : '保存电报配置'}
                </button>
              </div>
            </div>
          )}

          {!loading && tab === 'tags' && (
            <div className="rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-el px-3.5 py-3.5 space-y-3">
              <div className="font-mono text-[11px] font-semibold text-tm-tx">赛事分类</div>
              <p className="font-mono text-[10px] leading-[1.55] text-tm-tx-mut">
                用于标记关注的联赛/标签（小写存储）。可与后续同步或筛选逻辑联动。
              </p>
              <div className="flex flex-wrap gap-1.5">
                {tags.map((t) => (
                  <span
                    key={t}
                    className="inline-flex items-center gap-1 rounded-full border border-sky-500/40 bg-sky-500/15 px-2 py-0.5 font-mono text-[10px] font-semibold text-sky-200"
                  >
                    {t.toUpperCase()}
                    <button
                      type="button"
                      onClick={() => removeTag(t)}
                      className="p-0.5 rounded hover:bg-sky-500/25 text-tm-tx-mut"
                      aria-label={`删除 ${t}`}
                    >
                      <Trash2 className="w-3 h-3" />
                    </button>
                  </span>
                ))}
              </div>
              <div className="flex gap-2">
                <input
                  value={tagInput}
                  onChange={(e) => setTagInput(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      addTag(tagInput);
                    }
                  }}
                  placeholder="输入标签，例如 EPL、Soccer、MLB"
                  className="min-w-0 flex-1 rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-sunk px-2.5 py-2 font-mono text-[11px] text-tm-tx outline-none focus:border-tm-accent"
                />
                <button
                  type="button"
                  onClick={() => addTag(tagInput)}
                  className="shrink-0 rounded-[var(--tm-rad)] bg-sky-600 px-3 py-2 font-mono text-[10px] font-bold text-white hover:bg-sky-500"
                >
                  + 添加分类
                </button>
              </div>
              <div className="flex flex-wrap gap-1.5 pt-1">
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
                        'rounded-full border px-2 py-0.5 font-mono text-[10px] transition-colors',
                        selected
                          ? 'border-tm-bd bg-tm-bg-sunk text-tm-tx-mut cursor-default opacity-50'
                          : 'border-tm-bd bg-tm-bg text-tm-tx hover:border-tm-accent',
                      )}
                    >
                      {label}
                    </button>
                  );
                })}
              </div>
              <button
                type="button"
                disabled={
                  saving === 'eventClassificationTags' ||
                  JSON.stringify(tags) === rowValue(rows, 'eventClassificationTags')
                }
                onClick={() =>
                  void saveStandalone('eventClassificationTags', JSON.stringify(tags), '赛事分类')
                }
                className={cn(
                  'w-full rounded-[var(--tm-rad)] py-2 font-mono text-[10px] font-bold tracking-wider',
                  saving === 'eventClassificationTags' ||
                  JSON.stringify(tags) === rowValue(rows, 'eventClassificationTags')
                    ? 'bg-tm-bg-sunk text-tm-tx-mut cursor-not-allowed'
                    : 'bg-tm-accent text-black hover:bg-tm-accent/90',
                )}
              >
                {saving === 'eventClassificationTags' ? '保存中…' : '保存赛事分类'}
              </button>
            </div>
          )}

          {!loading && tab === 'prices' && (
            <div className="rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg-el px-3.5 py-3.5 space-y-3">
              <div className="flex items-start justify-between gap-2">
                <div>
                  <div className="font-mono text-[11px] font-semibold text-tm-tx">价格区间</div>
                  <p className="mt-1 font-mono text-[10px] leading-[1.55] text-tm-tx-mut max-w-md">
                    按 YES 价格（美分）区间配置资金占比与默认止损比例。开仓成交价落在某一区间时，可用{' '}
                    <span className="text-tm-tx-dim">stopLossPct</span> 作为该仓位止损（由执行层读取配置后实现）。
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() =>
                    setPriceRows((prev) => [
                      ...prev,
                      {
                        id: `r${Date.now()}`,
                        name: '新区间',
                        minCents: 0,
                        maxCents: 10,
                        fundPct: 0,
                        stopLossPct: 15,
                      },
                    ])
                  }
                  className="shrink-0 rounded-[var(--tm-rad)] border border-tm-bd bg-tm-bg px-2 py-1 font-mono text-[10px] font-bold text-tm-tx hover:border-tm-accent"
                >
                  + 添加区间
                </button>
              </div>
              <p className="font-mono text-[10px] text-tm-tx-dim">
                资金占比合计：{fundSum.toFixed(0)}%（建议接近 100%）
              </p>
              <div className="overflow-x-auto">
                <div
                  className="grid gap-x-2 gap-y-1.5 items-center font-mono text-[9px] text-tm-tx-mut tracking-wide min-w-[560px]"
                  style={{
                    gridTemplateColumns: 'minmax(72px,1fr) 56px 56px 64px 72px 28px',
                  }}
                >
                  <span>名称</span>
                  <span>下限 ¢</span>
                  <span>上限 ¢</span>
                  <span>资金占比 %</span>
                  <span>止损 %</span>
                  <span />
                  {priceRows.map((r, idx) => (
                    <Fragment key={r.id}>
                      <input
                        value={r.name}
                        onChange={(e) =>
                          setPriceRows((prev) =>
                            prev.map((x, i) => (i === idx ? { ...x, name: e.target.value } : x)),
                          )
                        }
                        className="rounded-sm border border-tm-bd bg-tm-bg-sunk px-1.5 py-1 text-[11px] text-tm-tx"
                      />
                      <input
                        type="number"
                        value={r.minCents}
                        onChange={(e) =>
                          setPriceRows((prev) =>
                            prev.map((x, i) =>
                              i === idx ? { ...x, minCents: Number(e.target.value) || 0 } : x,
                            ),
                          )
                        }
                        className="rounded-sm border border-tm-bd bg-tm-bg-sunk px-1 py-1 text-[11px] text-tm-tx"
                      />
                      <input
                        type="number"
                        value={r.maxCents}
                        onChange={(e) =>
                          setPriceRows((prev) =>
                            prev.map((x, i) =>
                              i === idx ? { ...x, maxCents: Number(e.target.value) || 0 } : x,
                            ),
                          )
                        }
                        className="rounded-sm border border-tm-bd bg-tm-bg-sunk px-1 py-1 text-[11px] text-tm-tx"
                      />
                      <input
                        type="number"
                        value={r.fundPct}
                        onChange={(e) =>
                          setPriceRows((prev) =>
                            prev.map((x, i) =>
                              i === idx ? { ...x, fundPct: Number(e.target.value) || 0 } : x,
                            ),
                          )
                        }
                        className="rounded-sm border border-tm-bd bg-tm-bg-sunk px-1 py-1 text-[11px] text-tm-tx"
                      />
                      <input
                        type="number"
                        value={r.stopLossPct}
                        onChange={(e) =>
                          setPriceRows((prev) =>
                            prev.map((x, i) =>
                              i === idx ? { ...x, stopLossPct: Number(e.target.value) || 0 } : x,
                            ),
                          )
                        }
                        className="rounded-sm border border-tm-bd bg-tm-bg-sunk px-1 py-1 text-[11px] text-tm-tx"
                      />
                      <button
                        type="button"
                        onClick={() => setPriceRows((prev) => prev.filter((_, i) => i !== idx))}
                        className="flex justify-center p-1 rounded-sm hover:bg-tm-neg/15 text-tm-tx-mut"
                        aria-label="删除区间"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </Fragment>
                  ))}
                </div>
              </div>
              <button
                type="button"
                disabled={
                  saving === 'priceStopLossRanges' ||
                  JSON.stringify(priceRows) === rowValue(rows, 'priceStopLossRanges')
                }
                onClick={() =>
                  void saveStandalone(
                    'priceStopLossRanges',
                    JSON.stringify(priceRows),
                    '价格区间',
                  )
                }
                className={cn(
                  'w-full rounded-[var(--tm-rad)] py-2 font-mono text-[10px] font-bold tracking-wider',
                  saving === 'priceStopLossRanges' ||
                  JSON.stringify(priceRows) === rowValue(rows, 'priceStopLossRanges')
                    ? 'bg-tm-bg-sunk text-tm-tx-mut cursor-not-allowed'
                    : 'bg-tm-accent text-black hover:bg-tm-accent/90',
                )}
              >
                {saving === 'priceStopLossRanges' ? '保存中…' : '保存价格区间'}
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
