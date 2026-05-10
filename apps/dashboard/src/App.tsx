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
import { getSetupStatus, type SetupStatus } from './lib/api';

const isPublic = import.meta.env.VITE_PUBLIC_MODE === 'true';

function OnboardingGate() {
  const loc = useLocation();
  const [status, setStatus] = useState<SetupStatus | null>(null);
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
            {!isPublic && <Route path="settings" element={<Settings />} />}
            {!isPublic && <Route path="accounts" element={<Accounts />} />}
          </Route>
        </Route>
      </Routes>
      <Toaster />
    </BrowserRouter>
  );
}
