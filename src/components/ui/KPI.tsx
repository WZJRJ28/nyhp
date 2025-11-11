import clsx from 'clsx';

interface KPIProps {
  title: string;
  value: string;
  change?: {
    value: string;
    positive?: boolean;
  };
  icon?: React.ReactNode;
}

const KPI = ({ title, value, change, icon }: KPIProps) => {
  return (
    <div className="flex flex-col gap-3 rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className="flex items-center justify-between text-sm font-medium text-slate-500 dark:text-slate-400">
        {title}
        {icon}
      </div>
      <div className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{value}</div>
      {change ? (
        <div
          className={clsx(
            'text-sm font-medium',
            change.positive === false
              ? 'text-rose-500'
              : change.positive
                ? 'text-emerald-500'
                : 'text-slate-500 dark:text-slate-400',
          )}
        >
          {change.value}
        </div>
      ) : null}
    </div>
  );
};

export default KPI;
