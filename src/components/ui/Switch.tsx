import clsx from 'clsx';

interface SwitchProps {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  description?: string;
}

const Switch = ({ label, checked, onChange, description }: SwitchProps) => {
  const toggle = () => onChange(!checked);

  return (
    <div className="flex w-full items-center justify-between gap-4 rounded-lg border border-slate-200 bg-white px-4 py-3 shadow-sm transition hover:border-emerald-300 dark:border-slate-700 dark:bg-slate-900">
      <div className="flex flex-col">
        <span className="text-sm font-medium text-slate-800 dark:text-slate-100">{label}</span>
        {description ? <span className="text-xs text-slate-500 dark:text-slate-400">{description}</span> : null}
      </div>
      <button
        type="button"
        role="switch"
        aria-label={label}
        aria-checked={checked}
        onClick={toggle}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            toggle();
          }
        }}
        className={clsx(
          'relative flex h-6 w-11 flex-shrink-0 cursor-pointer items-center rounded-full transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500',
          checked ? 'bg-emerald-500' : 'bg-slate-400 dark:bg-slate-600',
        )}
      >
        <span
          className={clsx(
            'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform',
            checked ? 'translate-x-5' : 'translate-x-1',
          )}
        />
      </button>
    </div>
  );
};

export default Switch;
