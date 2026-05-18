import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { TopBar } from "@/components/TopBar";
import { Trash2, Plus, Loader2 } from "lucide-react";
import { useAccounts } from "@/hooks/useAccounts";
import { useBalanceCache } from "@/hooks/useBalanceCache";
import { refreshMonitorData } from "@/hooks/useMonitorCache";
import { toast } from "sonner";

export const Route = createFileRoute("/accounts")({ component: AccountsPage });

function formatUsd(n: number): string {
  return n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function AccountsPage() {
  const { accounts, loading, error, create, activate, remove, refresh } = useAccounts();
  const { balance, loading: balanceLoading, refresh: refreshBalance } = useBalanceCache();
  const [name, setName] = useState('');
  const [privateKey, setPrivateKey] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [activatingId, setActivatingId] = useState<string | null>(null);

  const balanceById = new Map(
    (balance?.polymarketAccounts ?? []).map((b) => [b.id, b.polymarket]),
  );

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      toast.error('请填写名称');
      return;
    }
    setSubmitting(true);
    try {
      await create({ name: name.trim(), privateKey: privateKey.trim() });
      await refresh();
      toast.success('已添加', { description: '首个账号会自动设为当前下单账号' });
      setName('');
      setPrivateKey('');
      refreshBalance();
    } catch (err) {
      toast.error('添加失败', { description: err instanceof Error ? err.message : '请求错误' });
    } finally {
      setSubmitting(false);
    }
  }

async function handleActivate(id: string) {
    setActivatingId(id);
    try {
      await activate(id);
      toast.success('已设为默认账号', { description: '后续下单将使用该账号' });
      await refreshBalance();
      refreshMonitorData();
    } catch (err) {
      toast.error('切换失败', { description: err instanceof Error ? err.message : '请求错误' });
    } finally {
      setActivatingId(null);
    }
  }

  async function handleDelete(id: string) {
    if (!window.confirm('确定删除该账号？密钥将从本机数据库移除。')) return;
    try {
      await remove(id);
      toast('已删除');
      refreshBalance();
    } catch (err) {
      toast.error('删除失败', { description: err instanceof Error ? err.message : '请求错误' });
    }
  }

  return (
    <>
      <TopBar
        title="Polymarket 账号"
        subtitle={
          <span className="font-mono">CLOB V2 (POLY_1271)：服务端推导 API Key 与 funder (CREATE2 deposit)</span>
        }
      />

      <div className="p-6 space-y-6 animate-slide-up max-w-4xl">
        {error && (
          <div className="p-4 rounded-md border border-destructive/30 bg-destructive/10 text-destructive text-[12px]">
            {error}
          </div>
        )}

        <section className="space-y-3">
          <p className="text-[11px] uppercase tracking-widest text-muted-foreground font-medium">已有账号</p>
          
          {loading ? (
            <div className="text-[12px] text-muted-foreground">加载中...</div>
          ) : accounts.length === 0 ? (
            <div className="surface rounded-xl border border-border p-5 text-[12px] text-muted-foreground">
              暂无账号。请添加首个账号，或在 apps/bot/src/embeddedEnv.ts 中配置 POLYMARKET_* 作为后备。
            </div>
          ) : (
            accounts.map((a) => {
              const bal = balanceById.get(a.id);
              return (
                <div
                  key={a.id}
                  className={`surface rounded-xl border p-4 flex items-center justify-between hover:shadow-[0_0_0_3px_color-mix(in_oklab,var(--color-brand)_15%,transparent)] transition-shadow ${
                    a.isActive ? 'border-brand/40' : 'border-border'
                  }`}
                >
                  <div className="flex items-center gap-3">
                    <div className={`size-10 rounded-lg flex items-center justify-center font-mono text-[14px] ${
                      a.isActive ? 'bg-brand/10 border border-brand/30 text-brand' : 'bg-accent text-muted-foreground'
                    }`}>
                      {a.name.charAt(0).toUpperCase()}
                    </div>
                    <div className="flex flex-col">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-[13px] font-medium">{a.name}</span>
                        {a.isActive && (
                          <span className="px-1.5 py-0.5 text-[10px] rounded bg-brand/15 text-brand font-medium">默认</span>
                        )}
                      </div>
                      <span className="font-mono text-[10.5px] text-muted-foreground mt-0.5 truncate max-w-[300px]">{a.funderAddress}</span>
                    </div>
                  </div>
                  <div className="flex items-center gap-4">
                    <span className={`num text-[14px] font-medium ${a.isActive ? 'text-brand' : ''}`}>
                      {bal == null && balanceLoading ? (
                        <Loader2 className="size-4 animate-spin text-muted-foreground" />
                      ) : bal == null ? (
                        '—'
                      ) : (
                        `$${formatUsd(bal)}`
                      )}
                    </span>
                    <div className="flex gap-2">
{!a.isActive && (
                         <button
                           onClick={() => handleActivate(a.id)}
                           disabled={activatingId === a.id}
                           className="h-8 px-3 text-[11px] rounded-md border border-border bg-surface hover:bg-accent transition flex items-center gap-1.5 disabled:opacity-50 disabled:cursor-not-allowed"
                         >
                           {activatingId === a.id ? (
                             <Loader2 className="size-3.5 animate-spin" />
                           ) : null}
                           {activatingId === a.id ? '切换中...' : '设为默认'}
                         </button>
                       )}
                      <button 
                        onClick={() => handleDelete(a.id)}
                        className="h-8 px-3 text-[11px] text-danger hover:underline flex items-center gap-1"
                      >
                        <Trash2 className="size-3" /> 删除
                      </button>
                    </div>
                  </div>
                </div>
              );
            })
          )}
        </section>

        <section className="space-y-3">
          <p className="text-[11px] uppercase tracking-widest text-muted-foreground font-medium">添加账号</p>
          <form onSubmit={(e) => void handleCreate(e)} className="surface rounded-xl border border-border p-5 space-y-5">
            <div className="space-y-1.5">
              <label className="text-[11px] font-medium text-muted-foreground">显示名称</label>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例如 主号 / 小号"
                className="w-full h-10 px-3 text-[13px] rounded-md border border-border bg-background focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/30 transition"
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-[11px] font-medium text-muted-foreground">Owner 私钥 (Polygon EOA)</label>
              <p className="text-[10.5px] text-muted-foreground">仅保存在本机 SQLite。服务端调用 CLOB L1 推导 API Key，并用 CREATE2 推导 funder。</p>
              <input
                value={privateKey}
                onChange={(e) => setPrivateKey(e.target.value)}
                type="password"
                placeholder="0x…"
                className="w-full h-10 px-3 text-[13px] font-mono rounded-md border border-border bg-background focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/30 transition"
              />
            </div>
            <button 
              type="submit" 
              disabled={submitting}
              className="w-full h-11 rounded-md bg-brand text-brand-foreground text-[13px] font-semibold hover:opacity-90 active:scale-[0.99] transition flex items-center justify-center gap-2 disabled:opacity-50"
            >
              <Plus className="size-4" /> {submitting ? '提交中...' : '保存账号'}
            </button>
          </form>
        </section>
      </div>
    </>
  );
}