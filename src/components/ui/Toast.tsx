import { useEffect } from 'react';
import clsx from 'clsx';

import { useUIStore } from '@/store/useUI';

const toneStyles: Record<string, string> = {
  info: 'border-sky-500 text-sky-900 dark:border-sky-400 dark:text-sky-200',
  success: 'border-emerald-500 text-emerald-900 dark:border-emerald-400 dark:text-emerald-200',
  error: 'border-rose-500 text-rose-900 dark:border-rose-400 dark:text-rose-200',
};

const Toasts = () => {
  const { toasts, dismissToast } = useUIStore((state) => ({
    toasts: state.toasts,
    dismissToast: state.dismissToast,
  }));

  useEffect(() => {
    if (toasts.length === 0) {
      return;
    }

    const timers = toasts.map((toast) =>
      window.setTimeout(() => {
        dismissToast(toast.id);
      }, 4000),
    );

    return () => {
      timers.forEach((timer) => window.clearTimeout(timer));
    };
  }, [toasts, dismissToast]);

  if (toasts.length === 0) {
    return null;
  }

  return (
    <div className="pointer-events-none fixed inset-x-0 top-4 z-50 flex flex-col items-center gap-2 px-4">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className={clsx(
            'pointer-events-auto flex w-full max-w-sm items-start gap-3 rounded-xl border bg-white p-4 shadow-lg dark:bg-slate-900',
            toast.type ? toneStyles[toast.type] : 'border-slate-200 text-slate-900 dark:border-slate-700 dark:text-slate-100',
          )}
          role="status"
          aria-live="polite"
        >
          <div className="flex-1">
            <p className="text-sm font-semibold">{toast.title}</p>
            {toast.description ? (
              <p className="mt-1 text-xs text-slate-600 dark:text-slate-300">{toast.description}</p>
            ) : null}
          </div>
          <button
            type="button"
            className="text-xs text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-100"
            onClick={() => dismissToast(toast.id)}
          >
            关闭
          </button>
        </div>
      ))}
    </div>
  );
};

export default Toasts;
