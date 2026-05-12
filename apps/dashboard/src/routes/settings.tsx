import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect } from "react";
import { TopBar } from "@/components/TopBar";
import { useTheme } from "@/lib/theme";
import { useConfig } from "@/hooks/useConfig";
import { putConfig, testTelegram } from "@/lib/api";
import { DEFAULT_EVENT_CLASSIFICATION_TAGS, parseEventClassificationTags } from "@/lib/eventClassification";
import { toast } from "sonner";
import { Monitor, Globe, Send, Tag, DollarSign, Volume2, Info, Sun, Moon, Save, Trash2 } from "lucide-react";

export const Route = createFileRoute("/settings")({ component: SettingsPage });

type SettingsTab = 'general' | 'proxy' | 'telegram' | 'tags' | 'prices' | 'sound' | 'about';

const TABS: { id: SettingsTab; icon: typeof Monitor; title: string; desc: string }[] = [
  { id: 'general', icon: Monitor, title: '通用', desc: '主题、机器人参数' },
  { id: 'proxy', icon: Globe, title: '代理', desc: 'HTTP 代理配置' },
  { id: 'telegram', icon: Send, title: '电报', desc: 'Bot 与消息推送' },
  { id: 'tags', icon: Tag, title: '分类', desc: '赛事标签管理' },
  { id: 'prices', icon: DollarSign, title: '价格', desc: '资金区间与止损' },
  { id: 'sound', icon: Volume2, title: '声音', desc: '提醒与音效测试' },
  { id: 'about', icon: Info, title: '关于', desc: '版本与更新' },
];

const KEY_DESCRIPTIONS: Record<string, string> = {
  maxTradeSize: '单笔交易金额上限。',
  slippageTolerance: '允许的最优盘口价与实际成交量加权均价之间的最大偏离。',
  pollingInterval: '市场同步循环从 Polymarket 拉取报价的间隔（毫秒）。',
  orderBookLevels: '投注单 / 交易面板中，实时推送的 Polymarket 盘口档位数。',
  polymarketFokBuyExtraTicks: 'Polymarket FOK 买入：在最优卖价之上额外允许的 tick 档数。',
  polymarketFokSellExtraTicks: 'Polymarket FOK 卖出：在最优买价之下额外放宽的 tick 档数。',
  minOpenRiskShares: '风控列表与 CLOB 余额对账：仅保留份额 ≥ 本值的持仓。',
};

function SettingsPage() {
  const [active, setActive] = useState<SettingsTab>('general');
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
                  isActive ? "bg-brand/10 text-foreground border border-brand/30" : "hover:bg-accent border border-transparent"
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
              {active === 'general' && <GeneralTab rows={rows} onSave={save} />}
              {active === 'proxy' && <ProxyTab rows={rows} onSave={save} />}
              {active === 'telegram' && <TelegramTab rows={rows} onSave={save} />}
              {active === 'tags' && <TagsTab rows={rows} onSave={save} />}
              {active === 'prices' && <PricesTab rows={rows} onSave={save} />}
              {active === 'sound' && <SoundTab />}
              {active === 'about' && <AboutTab />}
            </>
          )}
        </div>
      </div>
    </>
  );
}

function GeneralTab({ rows, onSave }: { rows: { key: string; value: string }[]; onSave: (k: string, v: string) => Promise<void> }) {
  const { theme, setTheme } = useTheme();
  const [saving, setSaving] = useState<string | null>(null);
  const [edited, setEdited] = useState<Record<string, string>>({});

  const generalRows = rows.filter(r => !['httpPlatformProxyUrl', 'telegramBotToken', 'telegramAuthorizedChatId', 'eventClassificationTags', 'priceStopLossRanges'].includes(r.key));

  async function handleSave(key: string) {
    const value = edited[key] ?? rows.find(r => r.key === key)?.value ?? '';
    setSaving(key);
    try {
      await onSave(key, value);
      setEdited(prev => { const next = { ...prev }; delete next[key]; return next; });
      toast.success('已保存', { description: key });
    } catch (err) {
      toast.error('保存失败', { description: err instanceof Error ? err.message : '未知错误' });
    } finally {
      setSaving(null);
    }
  }

  return (
    <div className="space-y-5">
      <ThemeCard theme={theme} setTheme={setTheme} />

      <section className="surface rounded-xl border border-border p-5">
        <div className="flex items-start gap-3">
          <div className="size-8 rounded-md bg-accent flex items-center justify-center"><Monitor className="size-4 text-muted-foreground" /></div>
          <div>
            <p className="text-[13px] font-semibold">配置文件</p>
            <p className="text-[11.5px] text-muted-foreground mt-1">服务端将机器人参数与 <code className="px-1 py-0.5 rounded bg-accent text-foreground font-mono text-[11px]">~/.polybet/bot-settings.json</code> 同步。</p>
          </div>
        </div>
      </section>

      <section>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-[12.5px] font-semibold flex items-center gap-2">机器人参数</h3>
          <span className="text-[10.5px] text-muted-foreground font-mono">{generalRows.length} 项</span>
        </div>
        <div className="space-y-2.5">
          {generalRows.map((row) => {
            const isDirty = row.key in edited && edited[row.key] !== row.value;
            const isSaving = saving === row.key;
            return (
              <div key={row.key} className="surface rounded-lg border border-border p-4 flex items-start gap-4 hover:border-brand/30 transition">
                <div className="flex-1 min-w-0">
                  <p className="font-mono text-[12px] font-medium">{row.key}</p>
                  {KEY_DESCRIPTIONS[row.key] && <p className="text-[11px] text-muted-foreground mt-1 line-clamp-2">{KEY_DESCRIPTIONS[row.key]}</p>}
                </div>
                <input
                  value={edited[row.key] ?? row.value}
                  onChange={(e) => setEdited(prev => ({ ...prev, [row.key]: e.target.value }))}
                  className="h-8 w-28 px-2 num text-[12px] rounded-md border border-border bg-background focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/30 transition text-right" 
                />
                <button
                  onClick={() => handleSave(row.key)}
                  disabled={!isDirty || isSaving}
                  className="h-8 px-3 rounded-md text-[11.5px] font-medium transition flex items-center gap-1 disabled:opacity-50 disabled:cursor-not-allowed bg-brand/10 text-brand hover:bg-brand/20"
                >
                  <Save className="size-3" /> {isSaving ? '...' : '保存'}
                </button>
              </div>
            );
          })}
        </div>
      </section>
    </div>
  );
}

function ThemeCard({ theme, setTheme }: { theme: string; setTheme: (t: "light" | "dark") => void }) {
  return (
    <section className="surface rounded-xl border border-border p-5">
      <div className="flex items-start gap-3 mb-4">
        <div className="size-8 rounded-md bg-accent flex items-center justify-center"><Globe className="size-4 text-muted-foreground" /></div>
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

function ProxyTab({ rows, onSave }: { rows: { key: string; value: string }[]; onSave: (k: string, v: string) => Promise<void> }) {
  const [proxyDraft, setProxyDraft] = useState(rows.find(r => r.key === 'httpPlatformProxyUrl')?.value ?? '');
  const [saving, setSaving] = useState(false);

  async function handleSave() {
    setSaving(true);
    try {
      await onSave('httpPlatformProxyUrl', proxyDraft);
      toast.success('已保存', { description: '代理地址' });
    } catch (err) {
      toast.error('保存失败', { description: err instanceof Error ? err.message : '未知错误' });
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
        disabled={saving || proxyDraft === (rows.find(r => r.key === 'httpPlatformProxyUrl')?.value ?? '')}
        className="w-full h-10 rounded-md text-[12px] font-semibold transition disabled:opacity-50 bg-brand text-brand-foreground hover:opacity-90"
      >
        {saving ? '保存中...' : '保存代理'}
      </button>
    </div>
  );
}

function TelegramTab({ rows, onSave }: { rows: { key: string; value: string }[]; onSave: (k: string, v: string) => Promise<void> }) {
  const [tokenDraft, setTokenDraft] = useState(rows.find(r => r.key === 'telegramBotToken')?.value ?? '');
  const [chatDraft, setChatDraft] = useState(rows.find(r => r.key === 'telegramAuthorizedChatId')?.value ?? '');
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);

  async function handleSave() {
    setSaving(true);
    try {
      await onSave('telegramBotToken', tokenDraft);
      await onSave('telegramAuthorizedChatId', chatDraft);
      toast.success('已保存', { description: '电报配置' });
    } catch (err) {
      toast.error('保存失败', { description: err instanceof Error ? err.message : '未知错误' });
    } finally {
      setSaving(false);
    }
  }

  async function handleTest() {
    setTesting(true);
    try {
      await testTelegram();
      toast.success('测试成功', { description: '测试消息已发送到 Telegram' });
    } catch (err) {
      toast.error('测试失败', { description: err instanceof Error ? err.message : '未知错误' });
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
          <label className="text-[11px] font-semibold text-muted-foreground mb-1 block">Bot Token</label>
          <input
            type="password"
            value={tokenDraft}
            onChange={(e) => setTokenDraft(e.target.value)}
            className="w-full h-10 px-3 text-[13px] rounded-md border border-border bg-background focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/30 transition"
          />
        </div>
        <div>
          <label className="text-[11px] font-semibold text-muted-foreground mb-1 block">Authorized Chat ID</label>
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
          {saving ? '保存中...' : '保存电报配置'}
        </button>
        <button
          onClick={handleTest}
          disabled={testing || !tokenDraft || !chatDraft}
          className="w-full h-10 rounded-md text-[12px] font-semibold transition bg-sky-600 text-white hover:bg-sky-500 disabled:opacity-50"
        >
          {testing ? '发送中...' : '发送测试消息'}
        </button>
      </div>
    </div>
  );
}

function TagsTab({ rows, onSave }: { rows: { key: string; value: string }[]; onSave: (k: string, v: string) => Promise<void> }) {
  const [tags, setTags] = useState<string[]>(() => 
    parseEventClassificationTags(rows.find(r => r.key === 'eventClassificationTags')?.value ?? '')
  );
  const [tagInput, setTagInput] = useState('');
  const [saving, setSaving] = useState(false);

  const SUGGESTED = ['NBA', 'NCAAB', 'NHL', 'EPL', 'MLS', 'UCL', 'MLB'];

  function addTag(t: string) {
    const lower = t.trim().toLowerCase();
    if (!lower || tags.includes(lower)) return;
    setTags(prev => [...prev, lower]);
  }

  function removeTag(t: string) {
    setTags(prev => prev.filter(x => x !== t));
  }

  async function handleSave() {
    setSaving(true);
    try {
      await onSave('eventClassificationTags', JSON.stringify(tags));
      toast.success('已保存', { description: '赛事分类' });
    } catch (err) {
      toast.error('保存失败', { description: err instanceof Error ? err.message : '未知错误' });
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-5 space-y-4">
      <div className="text-[13px] font-semibold">赛事分类</div>
      <p className="text-[11px] text-muted-foreground leading-[1.55]">用于标记关注的联赛/标签（小写存储）。</p>
      <div className="flex flex-wrap gap-2">
        {tags.map((t) => (
          <span key={t} className="inline-flex items-center gap-1 rounded-full border border-sky-500/40 bg-sky-500/15 px-3 py-1 text-[11px] font-semibold text-sky-200">
            {t.toUpperCase()}
            <button onClick={() => removeTag(t)} className="p-0.5 rounded hover:bg-sky-500/25">
              <Trash2 className="size-3" />
            </button>
          </span>
        ))}
      </div>
      <div className="flex gap-2">
        <input
          value={tagInput}
          onChange={(e) => setTagInput(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addTag(tagInput); } }}
          placeholder="输入标签"
          className="flex-1 h-10 px-3 text-[12px] rounded-md border border-border bg-background focus:outline-none focus:border-brand"
        />
        <button onClick={() => addTag(tagInput)} className="px-4 h-10 rounded-md bg-sky-600 text-white text-[11px] font-semibold hover:bg-sky-500">
          + 添加
        </button>
      </div>
      <div className="flex flex-wrap gap-2">
        {SUGGESTED.map((label) => {
          const key = label.toLowerCase();
          const selected = tags.includes(key);
          return (
            <button
              key={label}
              disabled={selected}
              onClick={() => addTag(key)}
              className={`rounded-full border px-3 py-1 text-[11px] transition ${
                selected ? 'border-border bg-muted text-muted-foreground opacity-50' : 'border-border bg-background text-foreground hover:border-brand'
              }`}
            >
              {label}
            </button>
          );
        })}
      </div>
      <button
        onClick={handleSave}
        disabled={saving || JSON.stringify(tags) === (rows.find(r => r.key === 'eventClassificationTags')?.value ?? '')}
        className="w-full h-10 rounded-md text-[12px] font-semibold transition bg-brand text-brand-foreground hover:opacity-90 disabled:opacity-50"
      >
        {saving ? '保存中...' : '保存赛事分类'}
      </button>
    </div>
  );
}

function PricesTab({ rows, onSave }: { rows: { key: string; value: string }[]; onSave: (k: string, v: string) => Promise<void> }) {
  const DEFAULT_PRICE_ROWS = [
    { id: 'r1', name: '20-30¢', minCents: 20, maxCents: 30, fundPct: 17, stopLossPct: 20 },
    { id: 'r2', name: '30-40¢', minCents: 30, maxCents: 40, fundPct: 17, stopLossPct: 20 },
    { id: 'r3', name: '40-50¢', minCents: 40, maxCents: 50, fundPct: 17, stopLossPct: 20 },
    { id: 'r4', name: '50-60¢', minCents: 50, maxCents: 60, fundPct: 17, stopLossPct: 20 },
    { id: 'r5', name: '60-70¢', minCents: 60, maxCents: 70, fundPct: 16, stopLossPct: 20 },
    { id: 'r6', name: '70-80¢', minCents: 70, maxCents: 80, fundPct: 16, stopLossPct: 20 },
  ];

  const [priceRows, setPriceRows] = useState(() => {
    const raw = rows.find(r => r.key === 'priceStopLossRanges')?.value ?? '';
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
      await onSave('priceStopLossRanges', JSON.stringify(priceRows));
      toast.success('已保存', { description: '价格区间' });
    } catch (err) {
      toast.error('保存失败', { description: err instanceof Error ? err.message : '未知错误' });
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-5 space-y-4">
      <div className="text-[13px] font-semibold">价格区间</div>
      <p className="text-[11px] text-muted-foreground leading-[1.55]">按 YES 价格（美分）区间配置资金占比与默认止损比例。</p>
      <p className="text-[11px] text-muted-foreground">资金占比合计：{fundSum.toFixed(0)}%（建议接近 100%）</p>
      
      <div className="overflow-x-auto">
        <div className="grid gap-2 text-[10px] text-muted-foreground min-w-[500px]" style={{ gridTemplateColumns: '1fr 60px 60px 70px 70px 30px' }}>
          <span>名称</span><span>下限 ¢</span><span>上限 ¢</span><span>资金 %</span><span>止损 %</span><span />
          {priceRows.map((r, i) => (
            <div key={r.id} className="contents">
              <input value={r.name} onChange={(e) => setPriceRows(p => p.map((x, j) => j === i ? { ...x, name: e.target.value } : x))} className="h-8 px-2 text-[11px] rounded border border-border bg-background" />
              <input type="number" value={r.minCents} onChange={(e) => setPriceRows(p => p.map((x, j) => j === i ? { ...x, minCents: Number(e.target.value) || 0 } : x))} className="h-8 px-2 text-[11px] rounded border border-border bg-background" />
              <input type="number" value={r.maxCents} onChange={(e) => setPriceRows(p => p.map((x, j) => j === i ? { ...x, maxCents: Number(e.target.value) || 0 } : x))} className="h-8 px-2 text-[11px] rounded border border-border bg-background" />
              <input type="number" value={r.fundPct} onChange={(e) => setPriceRows(p => p.map((x, j) => j === i ? { ...x, fundPct: Number(e.target.value) || 0 } : x))} className="h-8 px-2 text-[11px] rounded border border-border bg-background" />
              <input type="number" value={r.stopLossPct} onChange={(e) => setPriceRows(p => p.map((x, j) => j === i ? { ...x, stopLossPct: Number(e.target.value) || 0 } : x))} className="h-8 px-2 text-[11px] rounded border border-border bg-background" />
              <button onClick={() => setPriceRows(p => p.filter((_, j) => j !== i))} className="text-danger hover:bg-destructive/10 rounded"><Trash2 className="size-4" /></button>
            </div>
          ))}
        </div>
      </div>
      <button
        onClick={() => setPriceRows(p => [...p, { id: `r${Date.now()}`, name: '新区间', minCents: 0, maxCents: 10, fundPct: 0, stopLossPct: 15 }])}
        className="px-3 h-8 rounded border border-border text-[11px] hover:bg-accent"
      >
        + 添加区间
      </button>
      <button
        onClick={handleSave}
        disabled={saving}
        className="w-full h-10 rounded-md text-[12px] font-semibold transition bg-brand text-brand-foreground hover:opacity-90 disabled:opacity-50"
      >
        {saving ? '保存中...' : '保存价格区间'}
      </button>
    </div>
  );
}

function SoundTab() {
  return (
    <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-5">
      <div className="text-[13px] font-semibold mb-3">声音提醒</div>
      <div className="text-[12px] text-muted-foreground">
        声音提醒功能需要桌面客户端支持。
      </div>
    </div>
  );
}

function AboutTab() {
  return (
    <div className="rounded-[var(--tm-rad)] border border-border bg-surface p-5">
      <div className="text-[13px] font-semibold mb-3">关于</div>
      <div className="space-y-2 text-[12px] text-muted-foreground">
        <p>Polybet - 预测市场交易系统</p>
        <p>版本: 1.0.0</p>
        <p className="text-brand hover:underline cursor-pointer">https://github.com/easyspace-ai/polybet</p>
      </div>
    </div>
  );
}