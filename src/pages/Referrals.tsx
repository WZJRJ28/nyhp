import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import Drawer from '@/components/ui/Drawer';
import Select from '@/components/ui/Select';
import Table, { SortOrder, TableColumn } from '@/components/ui/Table';
import Badge from '@/components/ui/Badge';
import { http } from '@/lib/http';
import { formatDate, fromNow } from '@/lib/date';
import { useAuthStore } from '@/store/useAuth';
import { useUIStore } from '@/store/useUI';
import type { Paginated, ReferralRequest } from '@/types';

const statusOptions = [
  { label: '全部状态', value: '' },
  { label: 'Open', value: 'open' },
  { label: 'Matched', value: 'matched' },
  { label: 'Signed', value: 'signed' },
  { label: 'In Progress', value: 'in_progress' },
  { label: 'Closed', value: 'closed' },
  { label: 'Disputed', value: 'disputed' },
  { label: 'Cancelled', value: 'cancelled' },
];

const dealTypeOptions = [
  { label: '全部交易', value: '' },
  { label: 'Buy', value: 'buy' },
  { label: 'Sell', value: 'sell' },
  { label: 'Rent', value: 'rent' },
];

const ReferralsPage = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const userRole = useAuthStore((state) => state.user?.role ?? 'client');
  const [status, setStatus] = useState(searchParams.get('status') ?? '');
  const [dealType, setDealType] = useState(searchParams.get('dealType') ?? '');
  const [region, setRegion] = useState(searchParams.get('region') ?? '');
  const [page, setPage] = useState(Number(searchParams.get('page') ?? 1) || 1);
  const [sortKey, setSortKey] = useState(searchParams.get('sortKey') ?? 'createdAt');
  const [sortOrder, setSortOrder] = useState<SortOrder>((searchParams.get('sortOrder') as SortOrder) ?? 'desc');
  const [data, setData] = useState<Paginated<ReferralRequest>>({ items: [], total: 0, page: 1, pageSize: 10 });
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<ReferralRequest | null>(null);
  const pushToast = useUIStore((state) => state.pushToast);

  useEffect(() => {
    const fetchData = async () => {
      setLoading(true);
      const params = new URLSearchParams({ page: String(page), pageSize: '10', sortKey, sortOrder });
      if (status) params.set('status', status);
      if (dealType) params.set('dealType', dealType);
      if (region) params.set('region', region);
      const result = await http.get<Paginated<ReferralRequest>>(`/referrals?${params.toString()}`);
      if (result.data) {
        setData(result.data);
        const pendingId = searchParams.get('requestId');
        if (pendingId) {
          const found = result.data.items.find((item) => item.id === pendingId);
          if (found) {
            setSelected(found);
          }
        }
      } else {
        pushToast({ title: '加载失败', description: result.error?.message, type: 'error' });
      }
      setLoading(false);
    };

    fetchData();
  }, [status, dealType, region, page, sortKey, sortOrder, pushToast, searchParams]);

  useEffect(() => {
    const currentId = searchParams.get('requestId');
    if (!currentId) {
      setSelected(null);
    }
  }, [searchParams]);

  const updateParam = (key: string, value: string | number | null, resetPage = true) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (value === null || value === '' || Number.isNaN(value)) {
        next.delete(key);
      } else {
        next.set(key, String(value));
      }
      if (resetPage) {
        next.delete('page');
      }
      return next;
    }, { replace: true });
  };

  const canCancel = userRole === 'agent' || userRole === 'broker_admin';

  const handleCancel = useCallback(async (requestId: string) => {
    const reason = window.prompt('撤销原因（可选）', '');
    const result = await http.post<ReferralRequest>(`/referrals/${requestId}/cancel`, { reason });
    if (result.data) {
      const updatedReferral = result.data;
      pushToast({ title: '转介已撤销', type: 'info' });
      setData((prev) => ({ ...prev, items: prev.items.map((item) => (item.id === requestId ? updatedReferral : item)) }));
    } else {
      pushToast({ title: '撤销失败', description: result.error?.message, type: 'error' });
    }
  }, [pushToast]);

  const columns: TableColumn<ReferralRequest>[] = useMemo(
    () => [
      { key: 'id', header: '编号', sortable: true },
      { key: 'region', header: '区域', accessor: (row) => (row.region ?? []).join(', ') },
      {
        key: 'price',
        header: '价格区间',
        accessor: (row) => `$${row.priceMin.toLocaleString()} - $${row.priceMax.toLocaleString()}`,
      },
      { key: 'dealType', header: '交易类型' },
      { key: 'languages', header: '语言', accessor: (row) => (row.languages ?? []).join(', ') },
      {
        key: 'slaHours',
        header: 'SLA (h)',
        accessor: (row) => row.slaHours,
        sortable: true,
      },
      {
        key: 'status',
        header: '状态',
        accessor: (row) => (
          <Badge
            tone={
              row.status === 'open'
                ? 'info'
                : row.status === 'disputed'
                  ? 'danger'
                  : row.status === 'closed'
                    ? 'default'
                    : row.status === 'cancelled'
                      ? 'default'
                      : 'success'
            }
          >
            {row.status}
          </Badge>
        ),
      },
      { key: 'createdAt', header: '创建时间', accessor: (row) => fromNow(row.createdAt), sortable: true },
      {
        key: 'actions',
        header: '操作',
        accessor: (row) => (
          canCancel && row.status === 'open'
            ? (
              <button
                type="button"
                className="text-sm text-rose-500 hover:underline"
                onClick={() => handleCancel(row.id)}
              >
                撤销
              </button>
            )
            : row.status === 'cancelled'
              ? <span className="text-xs text-slate-400" title={row.cancelReason ?? ''}>已撤销</span>
              : null
        ),
      },
    ],
    [canCancel, handleCancel],
  );

  const handleSort = (key: string) => {
    if (sortKey === key) {
      const nextOrder = sortOrder === 'asc' ? 'desc' : 'asc';
      setSortOrder(nextOrder);
      updateParam('sortOrder', nextOrder);
    } else {
      setSortKey(key);
      setSortOrder('asc');
      updateParam('sortKey', key);
      updateParam('sortOrder', 'asc');
    }
  };

  const regionsAvailable = useMemo(() => {
    const unique = new Set<string>();
    data.items.forEach((item) => item.region.forEach((value) => unique.add(value)));
    return Array.from(unique);
  }, [data.items]);

  return (
    <div className="space-y-4">
      <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">筛选</h2>
        <div className="mt-4 grid gap-4 md:grid-cols-4">
          <Select
            label="状态"
            value={status}
            onChange={(event) => {
              const value = event.target.value;
              setStatus(value);
              setPage(1);
              updateParam('status', value || null);
            }}
            options={statusOptions}
          />
          <Select
            label="交易类型"
            value={dealType}
            onChange={(event) => {
              const value = event.target.value;
              setDealType(value);
              setPage(1);
              updateParam('dealType', value || null);
            }}
            options={dealTypeOptions}
          />
          <Select
            label="区域"
            value={region}
            onChange={(event) => {
              const value = event.target.value;
              setRegion(value);
              setPage(1);
              updateParam('region', value || null);
            }}
            options={[{ label: '全部区域', value: '' }, ...regionsAvailable.map((item) => ({ label: item, value: item }))]}
          />
          <button
            type="button"
            className="self-end rounded-md border border-slate-300 px-3 py-2 text-sm hover:bg-slate-100 dark:border-slate-700 dark:hover:bg-slate-800"
            onClick={() => {
              setStatus('');
              setDealType('');
              setRegion('');
              setPage(1);
              setSortKey('createdAt');
              setSortOrder('desc');
              setSearchParams(new URLSearchParams(), { replace: true });
            }}
          >
            重置条件
          </button>
        </div>
      </div>

      <Table<ReferralRequest>
        data={data.items}
        total={data.total}
        page={data.page}
        pageSize={data.pageSize}
        loading={loading}
        columns={columns}
        sortKey={sortKey}
        sortOrder={sortOrder}
        onSort={handleSort}
        onPageChange={(nextPage) => {
          setPage(nextPage);
          updateParam('page', nextPage, false);
        }}
        onRowClick={(row) => {
          setSelected(row);
          setSearchParams((prev) => {
            const next = new URLSearchParams(prev);
            next.set('requestId', row.id);
            return next;
          }, { replace: true });
        }}
        rowKey={(row) => row.id}
      />

      <Drawer
        open={Boolean(selected)}
        onClose={() => {
          setSelected(null);
          setSearchParams((prev) => {
            const next = new URLSearchParams(prev);
            next.delete('requestId');
            return next;
          }, { replace: true });
        }}
        title={selected ? `转介详情 #${selected.id}` : '转介详情'}
      >
        {selected ? (
          <div className="space-y-4 text-sm text-slate-700 dark:text-slate-200">
            <div>
              <h3 className="text-sm font-semibold">基础信息</h3>
            <p>区域：{(selected.region ?? []).join(', ')}</p>
              <p>交易类型：{selected.dealType}</p>
              <p>物业类型：{selected.propertyType}</p>
              <p>
                预算：${selected.priceMin.toLocaleString()} - ${selected.priceMax.toLocaleString()}
              </p>
              <p>SLA：{selected.slaHours} 小时</p>
            </div>
            <div>
              <h3 className="text-sm font-semibold">语言需求</h3>
            <p>{(selected.languages ?? []).join(', ')}</p>
            </div>
            <div>
              <h3 className="text-sm font-semibold">状态</h3>
              <p>{selected.status}</p>
              <p>创建于：{formatDate(selected.createdAt)}</p>
              <p>相对时间：{fromNow(selected.createdAt)}</p>
            </div>
          </div>
        ) : null}
      </Drawer>
    </div>
  );
};

export default ReferralsPage;
