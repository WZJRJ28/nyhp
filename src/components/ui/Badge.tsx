import clsx from 'clsx';

interface BadgeProps {
  children: string;
  tone?: 'default' | 'success' | 'warning' | 'danger' | 'info';
}

const toneStyles: Record<NonNullable<BadgeProps['tone']>, string> = {
  default: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-200',
  success: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300',
  warning: 'bg-amber-100 text-amber-700 dark:bg-amber-500/10 dark:text-amber-300',
  danger: 'bg-rose-100 text-rose-700 dark:bg-rose-500/10 dark:text-rose-300',
  info: 'bg-sky-100 text-sky-700 dark:bg-sky-500/10 dark:text-sky-300',
};

const Badge = ({ children, tone = 'default' }: BadgeProps) => {
  return (
    <span className={clsx('inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium', toneStyles[tone])}>
      {children}
    </span>
  );
};

export default Badge;
