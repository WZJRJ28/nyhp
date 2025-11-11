import { useEffect, useState } from 'react';

import Switch from '@/components/ui/Switch';
import { useAuthStore } from '@/store/useAuth';
import { useUIStore } from '@/store/useUI';
import { http } from '@/lib/http';
import type { Broker } from '@/types';

const SettingsPage = () => {
  const { user, logout } = useAuthStore((state) => ({ user: state.user, logout: state.logout }));
  const { theme, setTheme } = useUIStore((state) => ({ theme: state.theme, setTheme: state.setTheme }));
  const [broker, setBroker] = useState<Broker | null>(null);

  useEffect(() => {
    const loadBroker = async () => {
      if (!user?.brokerId) {
        return;
      }
      const result = await http.get<Broker>(`/brokers/${user.brokerId}`);
      if (result.data) {
        setBroker(result.data);
      }
    };

    loadBroker();
  }, [user?.brokerId]);

  if (!user) {
    return null;
  }

  return (
    <div className="space-y-6">
      <section className="rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <h2 className="text-lg font-semibold text-slate-900 dark:text-slate-100">个人信息</h2>
        <dl className="mt-4 grid gap-3 text-sm text-slate-700 dark:text-slate-200 md:grid-cols-2">
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">姓名</dt>
            <dd className="mt-1 font-medium">{user.fullName}</dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">邮箱</dt>
            <dd className="mt-1 font-medium">{user.email}</dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">电话</dt>
            <dd className="mt-1 font-medium">{user.phone}</dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">语言能力</dt>
            <dd className="mt-1 font-medium">{(user.languages ?? []).join(', ')}</dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">评分</dt>
            <dd className="mt-1 font-medium">{user.rating.toFixed(1)}</dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">角色</dt>
            <dd className="mt-1 font-medium">{user.role}</dd>
          </div>
        </dl>
      </section>

      <section className="rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <h2 className="text-lg font-semibold text-slate-900 dark:text-slate-100">经纪公司</h2>
        {broker ? (
          <dl className="mt-4 grid gap-3 text-sm text-slate-700 dark:text-slate-200 md:grid-cols-2">
            <div>
              <dt className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">名称</dt>
              <dd className="mt-1 font-medium">{broker.name}</dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">FEIN</dt>
              <dd className="mt-1 font-medium">{broker.fein}</dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">认证状态</dt>
              <dd className="mt-1 font-medium">{broker.verified ? '已认证' : '待验证'}</dd>
            </div>
          </dl>
        ) : (
          <p className="mt-4 text-sm text-slate-500 dark:text-slate-400">正在加载经纪公司信息...</p>
        )}
      </section>

      <section className="rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <h2 className="text-lg font-semibold text-slate-900 dark:text-slate-100">偏好设置</h2>
        <div className="mt-4 space-y-4">
          <Switch
            label="暗色模式"
            description="同步存储在浏览器"
            checked={theme === 'dark'}
            onChange={(checked) => setTheme(checked ? 'dark' : 'light')}
          />
          <button
            type="button"
            className="rounded-md border border-rose-400 px-4 py-2 text-sm font-semibold text-rose-600 hover:bg-rose-50 dark:border-rose-500 dark:text-rose-300 dark:hover:bg-rose-500/10"
            onClick={logout}
          >
            退出登录
          </button>
        </div>
      </section>
    </div>
  );
};

export default SettingsPage;
