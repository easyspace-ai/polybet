import { useState, useEffect, useCallback } from 'react';
import { Button } from '../components/ui/button';
import { toast } from '../components/ui/use-toast';
import {
  listPolymarketAccounts,
  createPolymarketAccount,
  activatePolymarketAccount,
  deletePolymarketAccount,
  getBalances,
  type PolymarketAccountListItem,
  type PolymarketAccountBalanceRow,
} from '../lib/api';
import { cn } from '../lib/utils';

function formatUsd(n: number): string {
  return n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

export function Accounts() {
  const [accounts, setAccounts] = useState<PolymarketAccountListItem[]>([]);
  const [balances, setBalances] = useState<PolymarketAccountBalanceRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState('');
  const [privateKey, setPrivateKey] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [list, bal] = await Promise.all([listPolymarketAccounts(), getBalances()]);
      setAccounts(list);
      setBalances(bal.polymarketAccounts ?? []);
    } catch (err) {
      toast({
        title: '加载失败',
        description: err instanceof Error ? err.message : '无法读取账号或余额',
        variant: 'destructive',
      });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const balanceById = new Map(balances.map((b) => [b.id, b.polymarket]));

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      toast({ title: '请填写名称', variant: 'destructive' });
      return;
    }
    setSubmitting(true);
    try {
      await createPolymarketAccount({
        name: name.trim(),
        privateKey: privateKey.trim(),
      });
      toast({ title: '已添加', description: '首个账号会自动设为当前下单账号', variant: 'success' });
      setName('');
      setPrivateKey('');
      await load();
    } catch (err) {
      toast({
        title: '添加失败',
        description: err instanceof Error ? err.message : '请求错误',
        variant: 'destructive',
      });
    } finally {
      setSubmitting(false);
    }
  }

  async function handleActivate(id: string) {
    try {
      await activatePolymarketAccount(id);
      toast({ title: '已切换', description: '后续下单将使用该账号', variant: 'success' });
      await load();
    } catch (err) {
      toast({
        title: '切换失败',
        description: err instanceof Error ? err.message : '请求错误',
        variant: 'destructive',
      });
    }
  }

  async function handleDelete(id: string) {
    if (!window.confirm('确定删除该账号？密钥将从本机数据库移除。')) return;
    try {
      await deletePolymarketAccount(id);
      toast({ title: '已删除', variant: 'default' });
      await load();
    } catch (err) {
      toast({
        title: '删除失败',
        description: err instanceof Error ? err.message : '请求错误',
        variant: 'destructive',
      });
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-tm-bg">
      <div className="h-10 shrink-0 flex items-center gap-4 px-4 bg-tm-bg border-b border-tm-bd">
        <span className="font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-dim">
          Polymarket 账号
        </span>
        <span className="font-mono text-[10px] text-tm-tx-mut">
          CLOB V2（POLY_1271）：只需私钥，服务端会推导 API Key 与 funder（CREATE2 deposit，与 polymarket-clob-v2-go-exmaple 一致）。余额来自 CLOB。
        </span>
      </div>

      <div className="flex-1 min-h-0 overflow-auto p-4 space-y-8 max-w-2xl">
        <section>
          <h2 className="font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-mut mb-3">
            已有账号
          </h2>
          {loading ? (
            <p className="font-mono text-[11px] text-tm-tx-dim">加载中…</p>
          ) : accounts.length === 0 ? (
            <p className="font-mono text-[11px] text-tm-tx-dim">暂无账号。请添加首个账号，或在 apps/bot/src/embeddedEnv.ts 中配置 POLYMARKET_* 作为后备。</p>
          ) : (
            <ul className="space-y-2">
              {accounts.map((a) => {
                const bal = balanceById.get(a.id);
                return (
                  <li
                    key={a.id}
                    className={cn(
                      'rounded-sm border px-3 py-2.5 flex flex-wrap items-center gap-2',
                      a.isActive ? 'border-tm-poly bg-tm-poly/10' : 'border-tm-bd bg-tm-bg-el',
                    )}
                  >
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-[12px] font-semibold text-tm-tx">{a.name}</span>
                        {a.isActive && (
                          <span className="font-mono text-[9px] px-1.5 py-0.5 rounded-sm bg-tm-poly text-tm-bg">
                            当前
                          </span>
                        )}
                      </div>
                      <p className="font-mono text-[10px] text-tm-tx-dim truncate mt-0.5" title={a.funderAddress}>
                        {a.funderAddress}
                      </p>
                    </div>
                    <div className="font-mono text-[13px] text-tm-tx shrink-0">
                      {bal == null ? <span className="text-tm-tx-mut">—</span> : `$${formatUsd(bal)}`}
                    </div>
                    <div className="flex gap-1.5 shrink-0">
                      {!a.isActive && (
                        <Button size="sm" variant="outline" className="h-7 text-[10px]" onClick={() => void handleActivate(a.id)}>
                          设为当前
                        </Button>
                      )}
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 text-[10px] text-tm-neg hover:text-tm-neg"
                        onClick={() => void handleDelete(a.id)}
                      >
                        删除
                      </Button>
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </section>

        <section>
          <h2 className="font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-mut mb-3">
            添加账号
          </h2>
          <form onSubmit={(e) => void handleCreate(e)} className="space-y-3 rounded-sm border border-tm-bd bg-tm-bg-el p-4">
            <div>
              <label className="font-mono text-[9px] text-tm-tx-mut block mb-1">显示名称</label>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full bg-tm-bg border border-tm-bd rounded-sm px-2 py-1.5 font-mono text-[12px] text-tm-tx"
                placeholder="例如 主号 / 小号"
              />
            </div>
            <div>
              <label className="font-mono text-[9px] text-tm-tx-mut block mb-1">Owner 私钥（Polygon EOA）</label>
              <p className="font-mono text-[9px] text-tm-tx-dim mb-1.5 leading-relaxed">
                仅保存在本机 SQLite。服务端调用 CLOB L1 推导 API Key，并用 CREATE2 推导 funder（与 polymarket-clob-v2-go-exmaple 一致）。
              </p>
              <input
                value={privateKey}
                onChange={(e) => setPrivateKey(e.target.value)}
                className="w-full bg-tm-bg border border-tm-bd rounded-sm px-2 py-1.5 font-mono text-[11px] text-tm-tx"
                type="password"
                autoComplete="off"
                placeholder="0x…"
              />
            </div>
            <Button type="submit" disabled={submitting} className="w-full bg-tm-poly text-tm-bg hover:bg-tm-poly/90">
              {submitting ? '提交中…' : '保存账号'}
            </Button>
          </form>
        </section>
      </div>
    </div>
  );
}
