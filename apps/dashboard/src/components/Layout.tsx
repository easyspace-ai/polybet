import type { CSSProperties } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import { LineChart, History as HistoryIcon, Settings as SettingsIcon, Shield, Wallet } from 'lucide-react';
import { cn } from '../lib/utils';
import { GITHUB_REPO_URL } from '../lib/constants';

const isPublic = import.meta.env.VITE_PUBLIC_MODE === 'true';

const isElectron =
  typeof navigator !== 'undefined' && navigator.userAgent.includes('Electron');
const isMacElectron =
  isElectron &&
  (navigator.platform.startsWith('Mac') || navigator.userAgent.includes('Mac OS'));

// Official GitHub mark (inline SVG — lucide-react no longer ships brand icons due to trademark concerns)
function GithubIcon({ size = 16 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
    </svg>
  );
}

const navItems = [
  { to: '/',          label: '市场', icon: LineChart,    end: true },
  { to: '/risk',      label: '风控', icon: Shield,      end: undefined },
  ...(!isPublic ? [
    { to: '/history',  label: '历史', icon: HistoryIcon,  end: undefined },
    { to: '/accounts', label: '账号', icon: Wallet,      end: undefined },
    { to: '/settings', label: '设置', icon: SettingsIcon, end: undefined },
  ] : []),
];

export function Layout() {
  return (
    <div className="h-screen flex flex-col bg-tm-bg text-tm-tx overflow-hidden">
      {isMacElectron ? (
        <div
          className="shrink-0 flex h-8 w-full border-b border-tm-bd bg-tm-bg-sunk z-40"
          aria-hidden
        >
          <div className="w-[76px] shrink-0" />
          <div
            className="min-w-0 flex-1"
            style={{ WebkitAppRegion: 'drag' } as CSSProperties}
          />
        </div>
      ) : null}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col md:flex-row">
        {/* Mobile top header (≤md). Hidden on desktop. */}
        <header className="md:hidden h-11 shrink-0 flex items-center justify-between px-3 bg-tm-bg-sunk border-b border-tm-bd">
          <span className="font-mono text-[11px] font-bold tracking-[0.18em] text-tm-accent">SPMA</span>
          <div className="flex items-center gap-3">
            {isPublic && (
              <a
                href={GITHUB_REPO_URL}
                target="_blank"
                rel="noopener noreferrer"
                aria-label="在 GitHub 查看源码"
                className="flex items-center justify-center w-11 h-11 rounded-sm text-tm-tx-mut hover:text-tm-tx hover:bg-tm-bg-el/60 transition-colors"
              >
                <GithubIcon size={18} />
              </a>
            )}
            <span className="flex items-center gap-1.5">
              <span className="relative flex h-1.5 w-1.5">
                <span className="absolute inset-0 rounded-full bg-tm-pos animate-tm-pulse" />
                <span className="relative rounded-full h-1.5 w-1.5 bg-tm-pos" />
              </span>
              <span className="font-mono text-[9px] tracking-[0.1em] text-tm-tx-mut">在线</span>
            </span>
          </div>
        </header>

        {/* Desktop left rail (≥md). Hidden on mobile. */}
        <aside className="hidden md:flex w-14 shrink-0 flex-col items-stretch bg-tm-bg-sunk border-r border-tm-bd">
          <div className="h-10 flex items-center justify-center border-b border-tm-bd">
            <span className="font-mono text-[10px] font-bold tracking-[0.15em] text-tm-accent">SPMA</span>
          </div>

          <nav className="flex-1 flex flex-col py-2">
            {navItems.map(({ to, label, icon: Icon, end }) => (
              <NavLink
                key={to}
                to={to}
                end={end}
                className={({ isActive }) =>
                  cn(
                    'relative flex flex-col items-center justify-center gap-1 py-3 transition-colors',
                    'text-tm-tx-mut hover:text-tm-tx hover:bg-tm-bg-el/60',
                    isActive && 'text-tm-tx bg-tm-bg-el border-l-2 border-tm-accent',
                  )
                }
              >
                <Icon size={16} strokeWidth={1.75} />
                <span className="font-mono text-[9px] font-semibold tracking-[0.1em]">{label}</span>
              </NavLink>
            ))}
          </nav>

          {isPublic && (
            <a
              href={GITHUB_REPO_URL}
              target="_blank"
              rel="noopener noreferrer"
              aria-label="在 GitHub 查看源码"
              title="在 GitHub 查看源码"
              className="flex flex-col items-center justify-center gap-1 py-3 border-t border-tm-bd text-tm-tx-mut hover:text-tm-tx hover:bg-tm-bg-el/60 transition-colors"
            >
              <GithubIcon size={16} />
              <span className="font-mono text-[9px] font-semibold tracking-[0.1em]">源码</span>
            </a>
          )}

          <div className="h-12 border-t border-tm-bd flex flex-col items-center justify-center gap-1">
            <span className="relative flex h-1.5 w-1.5">
              <span className="absolute inset-0 rounded-full bg-tm-pos animate-tm-pulse" />
              <span className="relative rounded-full h-1.5 w-1.5 bg-tm-pos" />
            </span>
            <span className="font-mono text-[8px] tracking-[0.1em] text-tm-tx-mut">在线</span>
          </div>
        </aside>

        <main className="flex-1 min-w-0 overflow-y-auto pb-14 md:pb-0">
          <Outlet />
        </main>

        {/* Mobile bottom tab bar (≤md). Hidden on desktop. Respects iOS safe area. */}
        <nav
          className="md:hidden fixed bottom-0 left-0 right-0 z-30 h-14 bg-tm-bg-sunk border-t border-tm-bd flex items-stretch"
          style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
        >
          {navItems.map(({ to, label, icon: Icon, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) =>
                cn(
                  'relative flex-1 flex flex-col items-center justify-center gap-0.5 transition-colors',
                  'text-tm-tx-mut hover:text-tm-tx',
                  isActive && 'text-tm-tx',
                )
              }
            >
              {({ isActive }) => (
                <>
                  {isActive && (
                    <span className="absolute top-0 left-4 right-4 h-0.5 bg-tm-accent" />
                  )}
                  <Icon size={20} strokeWidth={1.75} />
                  <span className="font-mono text-[9px] font-semibold tracking-[0.1em]">{label}</span>
                </>
              )}
            </NavLink>
          ))}
        </nav>
      </div>
    </div>
  );
}
