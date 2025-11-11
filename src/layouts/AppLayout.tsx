import { useState } from 'react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';
import clsx from 'clsx';

import Toasts from '@/components/ui/Toast';
import Switch from '@/components/ui/Switch';
import { useAuthStore } from '@/store/useAuth';
import { useUIStore } from '@/store/useUI';

const usingMocks = import.meta.env.DEV && import.meta.env.VITE_USE_MOCKS !== 'false';

const navItems = [
  { to: '/app/dashboard', label: 'Dashboard', roles: ['agent', 'broker_admin', 'client'] },
  { to: '/app/referrals', label: 'Referrals', roles: ['agent', 'broker_admin', 'client'] },
  { to: '/app/referrals/invitations', label: 'Invitations', roles: ['agent', 'broker_admin'] },
  { to: '/app/referrals/new', label: 'Create Referral', roles: ['agent', 'broker_admin'] },
  { to: '/app/agreements', label: 'Agreements', roles: ['agent', 'broker_admin', 'client'] },
  { to: '/app/timeline', label: 'Timeline', roles: ['agent', 'broker_admin', 'client'] },
  { to: '/app/settings', label: 'Settings', roles: ['agent', 'broker_admin', 'client'] },
];

const AppLayout = () => {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const { user, logout } = useAuthStore((state) => ({
    user: state.user,
    logout: state.logout,
  }));
  const { theme, setTheme } = useUIStore((state) => ({
    theme: state.theme,
    setTheme: state.setTheme,
  }));
  const location = useLocation();

  const role = user?.role ?? 'client';
  const visibleNavItems = navItems.filter((item) => item.roles.includes(role));

  return (
    <div className="flex min-h-screen bg-slate-100 text-slate-900 dark:bg-slate-950 dark:text-slate-100">
      <Toasts />
      <aside
        className={clsx(
          'fixed inset-y-0 z-30 w-64 space-y-6 border-r border-slate-200 bg-white px-6 py-6 transition-transform dark:border-slate-800 dark:bg-slate-900 lg:static lg:translate-x-0',
          sidebarOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0',
        )}
      >
        <div className="flex items-center justify-between">
          <span className="text-lg font-semibold">Agent Referral</span>
          <button
            type="button"
            className="text-sm text-slate-500 hover:text-slate-800 focus:outline-none lg:hidden"
            onClick={() => setSidebarOpen(false)}
          >
            关闭
          </button>
        </div>
        <nav className="space-y-1">
          {visibleNavItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) =>
                clsx(
                  'flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition',
                  isActive
                    ? 'bg-emerald-500/10 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-200'
                    : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-slate-800 dark:hover:text-white',
                )
              }
              onClick={() => setSidebarOpen(false)}
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
        <div className="mt-auto space-y-4 text-sm text-slate-500 dark:text-slate-300">
          <div>
            <p className="font-semibold text-slate-700 dark:text-slate-100">{user?.fullName}</p>
            <p>{user?.email}</p>
          </div>
          <Switch
            label="暗色模式"
            description="切换界面主题"
            checked={theme === 'dark'}
            onChange={(checked) => setTheme(checked ? 'dark' : 'light')}
          />
          <button
            type="button"
            className="w-full rounded-md border border-slate-300 px-3 py-2 text-left font-medium text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-100 dark:hover:bg-slate-800"
            onClick={logout}
          >
            退出登录
          </button>
        </div>
      </aside>

      <div className="flex flex-1 flex-col">
        <header className="flex items-center justify-between border-b border-slate-200 bg-white px-6 py-4 dark:border-slate-800 dark:bg-slate-900">
          <div className="flex items-center gap-3">
            <button
              type="button"
              className="rounded-lg border border-slate-300 px-3 py-2 text-sm shadow-sm hover:bg-slate-100 focus:outline-none focus:ring-2 focus:ring-emerald-500 lg:hidden dark:border-slate-700 dark:hover:bg-slate-800"
              onClick={() => setSidebarOpen(true)}
            >
              菜单
            </button>
            <div>
              <p className="text-sm font-semibold text-slate-700 dark:text-slate-100">{user?.fullName}</p>
              <p className="text-xs text-slate-500 dark:text-slate-400">{location.pathname}</p>
            </div>
          </div>
          <div className="text-right text-xs text-slate-500 dark:text-slate-400">
            {usingMocks ? 'Mock 环境 · 数据来自 MSW' : '实时 API · 连接后端服务'}
          </div>
        </header>
        <main className="flex-1 bg-slate-50 p-6 dark:bg-slate-950">
          <Outlet />
        </main>
      </div>
    </div>
  );
};

export default AppLayout;
