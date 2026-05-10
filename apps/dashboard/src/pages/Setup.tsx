import { useState, useCallback, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button } from '../components/ui/button';
import { toast } from '../components/ui/use-toast';
import {
  getSetupStatus,
  postSetupComplete,
  putConfig,
  createPolymarketAccount,
} from '../lib/api';

type Step = 1 | 2 | 3;

export function Setup() {
  const navigate = useNavigate();
  const [step, setStep] = useState<Step>(1);
  const [checking, setChecking] = useState(true);
  const [proxyUrl, setProxyUrl] = useState('');
  const [polyName, setPolyName] = useState('');
  const [polyPk, setPolyPk] = useState('');
  const [saving, setSaving] = useState(false);

  const verifyNotDone = useCallback(async () => {
    setChecking(true);
    try {
      const s = await getSetupStatus();
      if (!s.needsOnboarding) {
        navigate('/', { replace: true });
        return;
      }
    } catch {
      // stay on setup if backend flaky
    } finally {
      setChecking(false);
    }
  }, [navigate]);

  useEffect(() => {
    void verifyNotDone();
  }, [verifyNotDone]);

  async function saveProxyAndNext() {
    setSaving(true);
    try {
      await putConfig('httpPlatformProxyUrl', proxyUrl.trim());
      toast({ title: '已保存', description: '出站代理已写入本机配置', variant: 'success' });
      setStep(2);
    } catch (err) {
      toast({
        title: '保存失败',
        description: err instanceof Error ? err.message : '请检查 URL 或后端日志',
        variant: 'destructive',
      });
    } finally {
      setSaving(false);
    }
  }

  function skipProxy() {
    toast({
      title: '已跳过',
      description: '未配置代理时，访问 Polymarket 等外网需本机可直接连通；若环境需要代理，稍后在「设置」中补全。',
      variant: 'default',
    });
    setStep(2);
  }

  async function savePolyAndNext() {
    if (!polyName.trim()) {
      toast({ title: '请填写账号名称', variant: 'destructive' });
      return;
    }
    if (!polyPk.trim()) {
      toast({ title: '请填写私钥', variant: 'destructive' });
      return;
    }
    setSaving(true);
    try {
      await createPolymarketAccount({ name: polyName.trim(), privateKey: polyPk.trim() });
      toast({ title: '账号已添加', description: '首个账号已自动设为当前交易账号', variant: 'success' });
      setPolyPk('');
      setStep(3);
    } catch (err) {
      toast({
        title: '添加失败',
        description: err instanceof Error ? err.message : '请检查网络、代理与私钥',
        variant: 'destructive',
      });
    } finally {
      setSaving(false);
    }
  }

  function skipPoly() {
    toast({
      title: '已跳过 Polymarket',
      description:
        '用户订单 WebSocket、风控 REST 同步与下单将不可用，直至在「账号」页添加账号或在 embeddedEnv 设置 POLYMARKET_PRIVATE_KEY。',
      variant: 'default',
    });
    setStep(3);
  }

  async function finish() {
    setSaving(true);
    try {
      await postSetupComplete();
      toast({ title: '安装完成', description: '正在启动行情同步与实时服务…', variant: 'success' });
      navigate('/', { replace: true });
    } catch (err) {
      toast({
        title: '启动失败',
        description: err instanceof Error ? err.message : '请查看后端日志',
        variant: 'destructive',
      });
    } finally {
      setSaving(false);
    }
  }

  if (checking) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-tm-bg text-tm-tx-dim font-mono text-[11px]">
        检查安装状态…
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-tm-bg text-tm-tx flex flex-col">
      <header className="shrink-0 border-b border-tm-bd px-4 py-3 flex items-center justify-between gap-4">
        <div>
          <h1 className="font-mono text-[11px] font-semibold tracking-[0.2em] text-tm-accent">首次安装</h1>
          <p className="font-mono text-[10px] text-tm-tx-mut mt-0.5">完成后再启动行情同步、实时通道与风控任务</p>
        </div>
        <div className="font-mono text-[9px] text-tm-tx-dim">
          步骤 {step} / 3
        </div>
      </header>

      <main className="flex-1 overflow-auto p-4 md:p-8 max-w-xl mx-auto w-full space-y-6">
        {step === 1 && (
          <section className="space-y-4 rounded-sm border border-tm-bd bg-tm-bg-el p-4">
            <h2 className="font-mono text-[10px] font-semibold tracking-[0.15em] text-tm-tx-mut">1. 出站 HTTP 代理（可选）</h2>
            <p className="font-mono text-[10px] text-tm-tx-dim leading-relaxed">
              若本机访问 Polymarket 需要 HTTP(S) CONNECT 代理，在此填写（与「设置」中的 httpPlatformProxyUrl 相同）。仅保存到本机 SQLite，不经过外网。
            </p>
            <input
              value={proxyUrl}
              onChange={(e) => setProxyUrl(e.target.value)}
              className="w-full bg-tm-bg border border-tm-bd rounded-sm px-2 py-2 font-mono text-[11px] text-tm-tx"
              placeholder="例如 http://127.0.0.1:7890 或留空"
              autoComplete="off"
            />
            <div className="flex flex-wrap gap-2">
              <Button type="button" size="sm" className="h-8 text-[10px]" disabled={saving} onClick={() => void saveProxyAndNext()}>
                保存并继续
              </Button>
              <Button type="button" size="sm" variant="outline" className="h-8 text-[10px]" disabled={saving} onClick={skipProxy}>
                跳过
              </Button>
            </div>
          </section>
        )}

        {step === 2 && (
          <section className="space-y-4 rounded-sm border border-tm-bd bg-tm-bg-el p-4">
            <h2 className="font-mono text-[10px] font-semibold tracking-[0.15em] text-tm-tx-mut">2. Polymarket 交易账号（可选）</h2>
            <p className="font-mono text-[10px] text-tm-tx-dim leading-relaxed">
              添加 Polygon EOA 私钥后，服务端会推导 CLOB API Key 与 funder（需能访问 Polymarket；若上一步未配代理，请确保本机直连可用）。
            </p>
            <div>
              <label className="font-mono text-[9px] text-tm-tx-mut block mb-1">显示名称</label>
              <input
                value={polyName}
                onChange={(e) => setPolyName(e.target.value)}
                className="w-full bg-tm-bg border border-tm-bd rounded-sm px-2 py-1.5 font-mono text-[12px] text-tm-tx"
                placeholder="例如 主号"
              />
            </div>
            <div>
              <label className="font-mono text-[9px] text-tm-tx-mut block mb-1">Owner 私钥</label>
              <input
                value={polyPk}
                onChange={(e) => setPolyPk(e.target.value)}
                type="password"
                className="w-full bg-tm-bg border border-tm-bd rounded-sm px-2 py-1.5 font-mono text-[11px] text-tm-tx"
                placeholder="0x…"
                autoComplete="off"
              />
            </div>
            <div className="flex flex-wrap gap-2">
              <Button type="button" size="sm" className="h-8 text-[10px]" disabled={saving} onClick={() => void savePolyAndNext()}>
                添加并继续
              </Button>
              <Button type="button" size="sm" variant="outline" className="h-8 text-[10px]" disabled={saving} onClick={skipPoly}>
                跳过
              </Button>
              <Button type="button" size="sm" variant="ghost" className="h-8 text-[10px]" onClick={() => setStep(1)}>
                上一步
              </Button>
            </div>
          </section>
        )}

        {step === 3 && (
          <section className="space-y-4 rounded-sm border border-tm-bd bg-tm-bg-el p-4">
            <h2 className="font-mono text-[10px] font-semibold tracking-[0.15em] text-tm-tx-mut">3. 进入应用</h2>
            <p className="font-mono text-[10px] text-tm-tx-dim leading-relaxed">
              确认后将标记安装完成，并启动市场同步、Polymarket 实时连接、用户订单 WebSocket 与风控定时任务。之后仍可在「设置」「账号」中修改。
            </p>
            <div className="flex flex-wrap gap-2">
              <Button type="button" size="sm" className="h-8 text-[10px]" disabled={saving} onClick={() => void finish()}>
                完成并启动
              </Button>
              <Button type="button" size="sm" variant="ghost" className="h-8 text-[10px]" disabled={saving} onClick={() => setStep(2)}>
                上一步
              </Button>
            </div>
          </section>
        )}
      </main>
    </div>
  );
}
