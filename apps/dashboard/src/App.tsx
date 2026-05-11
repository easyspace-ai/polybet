import { useCallback, useEffect, useState } from 'react';
import {
  BrowserRouter,
  Routes,
  Route,
  Navigate,
  Outlet,
  useLocation,
} from 'react-router-dom';
import { Layout } from './components/Layout';
import { Toaster } from './components/ui/toaster';
import { Markets } from './pages/Markets';
import { History } from './pages/History';
import { Settings } from './pages/Settings';
import { Accounts } from './pages/Accounts';
import { RiskControl } from './pages/RiskControl';
import { Setup } from './pages/Setup';
import { LogPage } from './pages/log-page';
import { getSetupStatus, type SetupStatus } from './lib/api';

const isPublic = import.meta.env.VITE_PUBLIC_MODE === 'true';

interface InitStepStatus {
  status: string;
  error?: string;
  details?: {
    blocked?: boolean;
    country?: string;
    count?: number;
  };
}

interface InitStatus {
  configCheck: InitStepStatus;
  proxyCheck: InitStepStatus;
  balanceCache: InitStepStatus;
  positionCache: InitStepStatus;
  complete: boolean;
}

function InitLoadingPage({ onComplete }: { onComplete: () => void }) {
  const [status, setStatus] = useState<InitStatus | null>(null);

  useEffect(() => {
    const poll = async () => {
      try {
        const res = await fetch('/api/setup/init-status');
        if (res.ok) {
          const data: InitStatus = await res.json();
          setStatus(data);
          if (data.complete) {
            onComplete();
          }
        }
      } catch {}
    };
    poll();
    const interval = setInterval(poll, 2000);
    return () => clearInterval(interval);
  }, [onComplete]);

  const steps = [
    { key: 'configCheck', name: '环境检测' },
    { key: 'proxyCheck', name: '代理检测' },
    { key: 'balanceCache', name: '余额缓存' },
    { key: 'positionCache', name: '持仓缓存' },
  ];

  const getStepStatus = (key: string): InitStepStatus => {
    if (!status) return { status: 'pending' };
    const s = status as InitStatus;
    const v = s[key as keyof InitStatus];
    if (!v) return { status: 'pending' };
    if (typeof v === 'boolean') return { status: 'pending' };
    return v;
  };

  const completed = steps.filter(s => getStepStatus(s.key).status === 'done').length;
  const progress = status ? (completed / steps.length) * 100 : 0;

  const getStatusColor = (s: string) => {
    switch (s) {
      case 'done': return '#22c55e';
      case 'loading': return '#3b82f6';
      case 'error': return '#ef4444';
      default: return '#9ca3af';
    }
  };

  const getStatusIcon = (s: string) => {
    switch (s) {
      case 'done': return '✓';
      case 'loading': return '◐';
      case 'error': return '✗';
      default: return '○';
    }
  };

  return (
    <div className="min-h-screen flex flex-col items-center justify-center bg-tm-bg p-6">
      <div className="mb-8">
        <div className="w-16 h-16 mx-auto mb-4 rounded-2xl bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center">
          <span className="text-3xl">🎯</span>
        </div>
        <h1 className="font-mono text-[18px] font-bold text-center mb-2">Polybet</h1>
        <p className="font-mono text-[11px] text-tm-tx-dim text-center">正在初始化系统...</p>
      </div>

      <div className="w-full max-w-xs h-2 bg-tm-bg-el rounded-full mb-8">
        <div
          className="h-full rounded-full transition-all duration-300"
          style={{
            width: `${progress}%`,
            background: progress === 100 ? '#22c55e' : '#3b82f6'
          }}
        />
      </div>

      <div className="w-full max-w-xs space-y-3 mb-8">
        {steps.map(step => {
          const stepStatus = getStepStatus(step.key);
          const details = stepStatus.details;
          let detailText = '';
          if (step.key === 'proxyCheck' && details) {
            detailText = details.blocked ? `受限: ${details.country}` : `正常: ${details.country}`;
          } else if ((step.key === 'balanceCache' || step.key === 'positionCache') && details && typeof details === 'object' && 'count' in details) {
            detailText = `${(details as {count: number}).count} 个`;
          }
          const s = stepStatus.status;

          return (
            <div key={step.key} className="flex items-center gap-3 py-2 border-b border-tm-bd">
              <span
                className="w-6 h-6 flex items-center justify-center rounded-full text-[10px] text-white"
                style={{ background: getStatusColor(s) }}
              >
                {getStatusIcon(s)}
              </span>
              <div className="flex-1">
                <div className="font-mono text-[11px]">{step.name}</div>
                {detailText && <div className="font-mono text-[10px] text-tm-tx-dim">{detailText}</div>}
                {stepStatus.error && <div className="font-mono text-[10px] text-red-500">{stepStatus.error}</div>}
              </div>
            </div>
          );
        })}
      </div>

      <div className="mt-8 text-center">
        <a
          href="https://polybet.ai"
          target="_blank"
          rel="noopener noreferrer"
          className="font-mono text-[10px] text-tm-tx-dim hover:text-tm-tx underline"
        >
          polybet.ai
        </a>
      </div>

      {status?.complete && (
        <p className="mt-6 font-mono text-[11px] text-green-500">初始化完成，正在进入...</p>
      )}
    </div>
  );
}

function OnboardingGate() {
  const loc = useLocation();
  const [status, setStatus] = useState<SetupStatus | null>(null);
  const [initComplete, setInitComplete] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(() => {
    setErr(null);
    return getSetupStatus()
      .then(setStatus)
      .catch((e: unknown) => setErr(e instanceof Error ? e.message : '请求失败'));
  }, []);

  useEffect(() => {
    if (isPublic) {
      return;
    }
    void load();
  }, [load, loc.pathname]);

  if (isPublic) {
    return <Outlet />;
  }

  if (!initComplete) {
    return <InitLoadingPage onComplete={() => setInitComplete(true)} />;
  }

  if (err) {
    return (
      <div className="min-h-screen flex flex-col items-center justify-center gap-4 bg-tm-bg p-6 text-tm-tx">
        <p className="font-mono text-[12px] text-center max-w-md text-tm-tx-dim">
          无法读取安装状态：{err}
          <span className="block mt-2 text-[10px]">
            请确认 Bot 已启动，且 Dashboard 的 Vite 代理端口与 Bot 的 PORT 一致（见 apps/dashboard/.env.development 与 apps/bot/src/embeddedEnv.ts）。
          </span>
        </p>
        <button
          type="button"
          className="font-mono text-[10px] px-3 py-1.5 rounded-sm border border-tm-bd bg-tm-bg-el hover:bg-tm-bg text-tm-tx"
          onClick={() => void load()}
        >
          重试
        </button>
      </div>
    );
  }
  if (status === null) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-tm-bg text-tm-tx-dim font-mono text-[11px]">
        正在连接后端…
      </div>
    );
  }
  if (status.needsOnboarding) {
    return <Navigate to="/setup" replace />;
  }
  return <Outlet />;
}

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/setup" element={isPublic ? <Navigate to="/" replace /> : <Setup />} />
        <Route element={<OnboardingGate />}>
          <Route element={<Layout />}>
            <Route index element={<Markets />} />
            {!isPublic && <Route path="history" element={<History />} />}
            <Route path="risk" element={<RiskControl />} />
            <Route path="coverage" element={<Navigate to="/risk" replace />} />
            {!isPublic && <Route path="logs" element={<LogPage />} />}
            {!isPublic && <Route path="settings" element={<Settings />} />}
            {!isPublic && <Route path="accounts" element={<Accounts />} />}
          </Route>
        </Route>
      </Routes>
      <Toaster />
    </BrowserRouter>
  );
}
