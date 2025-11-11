import { forwardRef } from 'react';
import clsx from 'clsx';

interface SelectOption {
  label: string;
  value: string;
}

interface SelectProps extends Omit<React.ComponentPropsWithoutRef<'select'>, 'children'> {
  label: string;
  options: SelectOption[];
  error?: string;
}

const Select = forwardRef<HTMLSelectElement, SelectProps>(({ label, options, error, className, id, required, ...props }, ref) => {
  const selectId = id ?? props.name;

  return (
    <div className="space-y-1">
      <label htmlFor={selectId} className="text-sm font-medium text-slate-700 dark:text-slate-200">
        {label}
        {required ? <span className="ml-1 text-rose-500">*</span> : null}
      </label>
      <select
        id={selectId}
        ref={ref}
        className={clsx(
          'w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 focus:border-emerald-500 focus:outline-none focus:ring-2 focus:ring-emerald-500/30 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100',
          error && 'border-rose-500 focus:border-rose-500 focus:ring-rose-500/30',
          className,
        )}
        {...props}
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
      {error ? <p className="text-xs text-rose-500">{error}</p> : null}
    </div>
  );
});

Select.displayName = 'Select';

export default Select;
