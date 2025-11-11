import { Fragment, ReactNode } from 'react';
import clsx from 'clsx';

export type SortOrder = 'asc' | 'desc';

export interface TableColumn<T> {
  key: string;
  header: string;
  accessor?: (row: T) => ReactNode;
  sortable?: boolean;
  className?: string;
}

interface TableProps<T> {
  data: T[];
  columns: TableColumn<T>[];
  loading?: boolean;
  emptyText?: string;
  sortKey?: string;
  sortOrder?: SortOrder;
  onSort?: (key: string) => void;
  page?: number;
  pageSize?: number;
  total?: number;
  onPageChange?: (page: number) => void;
  rowKey?: (row: T) => string;
  onRowClick?: (row: T) => void;
}

function Table<T>({
  data,
  columns,
  loading,
  emptyText = '暂无数据',
  sortKey,
  sortOrder,
  onSort,
  page = 1,
  pageSize = 10,
  total = data.length,
  onPageChange,
  rowKey,
  onRowClick,
}: TableProps<T>) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <table className="min-w-full divide-y divide-slate-200 text-sm dark:divide-slate-800">
        <thead className="bg-slate-50 dark:bg-slate-800/50">
          <tr>
            {columns.map((column) => {
              const isActive = sortKey === column.key;
              return (
                <th
                  key={column.key}
                  scope="col"
                  className={clsx('px-4 py-3 text-left font-semibold text-slate-600 dark:text-slate-200', column.className)}
                >
                  {column.sortable ? (
                    <button
                      type="button"
                      onClick={() => onSort?.(column.key)}
                      className="flex items-center gap-1 text-slate-600 hover:text-slate-900 dark:text-slate-200 dark:hover:text-white"
                    >
                      {column.header}
                      {isActive ? <span>{sortOrder === 'asc' ? '↑' : '↓'}</span> : null}
                    </button>
                  ) : (
                    column.header
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-200 bg-white dark:divide-slate-800 dark:bg-slate-900">
          {loading ? (
            <tr>
              <td colSpan={columns.length} className="px-4 py-6 text-center text-slate-500 dark:text-slate-400">
                加载中...
              </td>
            </tr>
          ) : data.length === 0 ? (
            <tr>
              <td colSpan={columns.length} className="px-4 py-6 text-center text-slate-500 dark:text-slate-400">
                {emptyText}
              </td>
            </tr>
          ) : (
            data.map((row, index) => {
              const key = rowKey ? rowKey(row) : `${index}`;
              return (
                <tr
                  key={key}
                  onClick={() => onRowClick?.(row)}
                  className={clsx(
                    onRowClick && 'cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-800/60',
                  )}
                >
                  {columns.map((column) => (
                    <td key={column.key} className={clsx('px-4 py-3 text-slate-700 dark:text-slate-200', column.className)}>
                      {column.accessor ? column.accessor(row) : ((row as Record<string, unknown>)[column.key] as ReactNode)}
                    </td>
                  ))}
                </tr>
              );
            })
          )}
        </tbody>
      </table>
      <div className="flex items-center justify-between bg-slate-50 px-4 py-3 text-xs text-slate-600 dark:bg-slate-800/60 dark:text-slate-300">
        <span>
          第 {page} / {totalPages} 页 · 共 {total} 条
        </span>
        <div className="inline-flex items-center gap-2">
          <button
            type="button"
            className="rounded-md border border-slate-300 px-2 py-1 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700"
            onClick={() => onPageChange?.(Math.max(1, page - 1))}
            disabled={page <= 1}
          >
            上一页
          </button>
          <button
            type="button"
            className="rounded-md border border-slate-300 px-2 py-1 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700"
            onClick={() => onPageChange?.(Math.min(totalPages, page + 1))}
            disabled={page >= totalPages}
          >
            下一页
          </button>
        </div>
      </div>
    </div>
  );
}

export default Table;
