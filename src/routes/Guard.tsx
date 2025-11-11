import { ReactNode, useEffect } from 'react';
import { Navigate, useLocation } from 'react-router-dom';

import { useAuthStore } from '@/store/useAuth';

interface RouteGuardProps {
  children: ReactNode;
}

const RouteGuard = ({ children }: RouteGuardProps) => {
  const location = useLocation();
  const { token, user, status, fetchMe } = useAuthStore((state) => ({
    token: state.token,
    user: state.user,
    status: state.status,
    fetchMe: state.fetchMe,
  }));

  useEffect(() => {
    if (token && !user && status === 'idle') {
      void fetchMe();
    }
  }, [token, user, status, fetchMe]);

  if (!token) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }

  if (status === 'loading' || !user) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-100 dark:bg-slate-900">
        <div className="h-12 w-12 animate-spin rounded-full border-4 border-emerald-500 border-t-transparent" aria-label="加载中" />
      </div>
    );
  }

  return <>{children}</>;
};

export default RouteGuard;
