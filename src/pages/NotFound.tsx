import { Link } from 'react-router-dom';

const NotFoundPage = () => {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-slate-100 px-6 text-center dark:bg-slate-950">
      <h1 className="text-3xl font-semibold text-slate-900 dark:text-slate-100">404</h1>
      <p className="mt-2 text-sm text-slate-600 dark:text-slate-300">页面不存在，或已被移动。</p>
      <Link
        to="/login"
        className="mt-6 rounded-md bg-emerald-600 px-4 py-2 text-sm font-semibold text-white hover:bg-emerald-500"
      >
        返回登录页
      </Link>
    </div>
  );
};

export default NotFoundPage;
