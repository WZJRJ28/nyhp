import { forwardRef } from 'react';
import clsx from 'clsx';

interface NumberInputProps extends React.ComponentPropsWithoutRef<'input'> {
  label: string;
  error?: string;
}

const NumberInput = forwardRef<HTMLInputElement, NumberInputProps>(
  ({ label, error, className, id, required, ...props }, ref) => {
    const inputId = id ?? props.name;

    return (
      <div className="space-y-1">
        <label htmlFor={inputId} className="text-sm font-medium text-slate-700 dark:text-slate-200">
          {label}
          {required ? <span className="ml-1 text-rose-500">*</span> : null}
        </label>
        <input
          type="number"
          id={inputId}
          ref={ref}
          className={clsx(
            'w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 focus:border-emerald-500 focus:outline-none focus:ring-2 focus:ring-emerald-500/30 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100',
            error && 'border-rose-500 focus:border-rose-500 focus:ring-rose-500/30',
            className,
          )}
          {...props}
        />
        {error ? <p className="text-xs text-rose-500">{error}</p> : null}
      </div>
    );
  },
);

NumberInput.displayName = 'NumberInput';

export default NumberInput;
