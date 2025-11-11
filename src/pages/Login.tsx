import { FormEvent, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';

import Input from '@/components/ui/Input';
import { useAuthStore } from '@/store/useAuth';
import { useUIStore } from '@/store/useUI';

const LoginPage = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const login = useAuthStore((state) => state.login);
  const { pushToast } = useUIStore((state) => ({ pushToast: state.pushToast }));
  const [email, setEmail] = useState('alex.agent@example.com');
  const [password, setPassword] = useState('password');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError(null);
    setLoading(true);
    const result = await login({ email, password });
    setLoading(false);

    if (result.data) {
      pushToast({ title: '登录成功', description: '欢迎回来', type: 'success' });
      const redirectPath = (location.state as { from?: string } | null)?.from ?? '/app/dashboard';
      navigate(redirectPath, { replace: true });
    } else {
      setError(result.error?.message ?? '登录失败');
      pushToast({ title: '登录失败', description: result.error?.message ?? '请检查账号密码', type: 'error' });
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-100 px-4 dark:bg-slate-950">
      <div className="w-full max-w-md space-y-6 rounded-2xl border border-slate-200 bg-white p-8 shadow-lg dark:border-slate-800 dark:bg-slate-900">
        <div className="space-y-2 text-center">
          <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">Agent Referral Network</h1>
          <p className="text-sm text-slate-600 dark:text-slate-300">使用模拟账号登录以体验演示</p>
        </div>
        <form className="space-y-4" onSubmit={handleSubmit}>
          <Input
            label="邮箱"
            name="email"
            type="email"
            autoComplete="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            required
          />
          <Input
            label="密码"
            name="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            required
          />
          {error ? <p className="text-sm text-rose-500">{error}</p> : null}
          <button
            type="submit"
            disabled={loading}
            className="w-full rounded-md bg-emerald-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-70"
          >
            {loading ? '登录中...' : '登录'}
          </button>
        </form>
        <p className="text-center text-xs text-slate-500 dark:text-slate-400">MSW 将拦截 /auth/login 并返回模拟令牌</p>
      </div>
    </div>
  );
};

export default LoginPage;
