import { ErrorBoundary as ReactErrorBoundary, FallbackProps } from 'react-error-boundary';
import type { ReactNode } from 'react';

const Fallback = ({ error, resetErrorBoundary }: FallbackProps) => {
  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center gap-4 bg-white p-6 text-center dark:bg-slate-900">
      <div className="text-2xl font-semibold text-slate-900 dark:text-slate-100">出错了</div>
      <p className="max-w-md text-sm text-slate-600 dark:text-slate-300">{error.message}</p>
      <button
        type="button"
        className="rounded-md bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-500"
        onClick={resetErrorBoundary}
      >
        重试
      </button>
    </div>
  );
};

interface ErrorBoundaryProps {
  children: ReactNode;
}

const ErrorBoundary = ({ children }: ErrorBoundaryProps) => {
  return (
    <ReactErrorBoundary FallbackComponent={Fallback}>{children}</ReactErrorBoundary>
  );
};

export default ErrorBoundary;
