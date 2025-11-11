import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import Table from '@/components/ui/Table';
import Badge from '@/components/ui/Badge';
import { http } from '@/lib/http';
import { useAuthStore } from '@/store/useAuth';
import { useUIStore } from '@/store/useUI';
import type { ReferralMatch } from '@/types';

const ReferralsInbox = () => {
  const { pushToast } = useUIStore((state) => ({ pushToast: state.pushToast }));
  const userRole = useAuthStore((state) => state.user?.role ?? 'client');
  const [matches, setMatches] = useState<ReferralMatch[]>([]);
  const [loading, setLoading] = useState(false);

  const canView = userRole === 'agent' || userRole === 'broker_admin';

  useEffect(() => {
    if (!canView) {
      return;
    }
    const load = async () => {
      setLoading(true);
      const result = await http.get<{ items: ReferralMatch[] }>('/matches');
      if (result.data) {
        setMatches(result.data.items);
      } else {
        pushToast({ title: '加载失败', description: result.error?.message, type: 'error' });
      }
      setLoading(false);
    };
    load();
  }, [canView, pushToast]);

  const updateMatch = useCallback(async (matchId: string, requestId: string, state: 'accepted' | 'declined') => {
    const result = await http.patch<ReferralMatch>(`/referrals/${requestId}/matches/${matchId}`, { state });
    if (result.data) {
      setMatches((prev) => prev.map((item) => (item.id === matchId ? result.data! : item)));
      pushToast({
        title: state === 'accepted' ? '已接受转介' : '已拒绝转介',
        description:
          state === 'accepted' && result.data.agreement
            ? `协议 #${result.data.agreement.id.slice(0, 8)} 已同步创建`
            : undefined,
        type: 'success',
      });
    } else {
      pushToast({ title: '操作失败', description: result.error?.message, type: 'error' });
    }
  }, [pushToast]);

  const columns = useMemo(
    () => [
      { key: 'requestId', header: '转介编号' },
      {
        key: 'state',
        header: '状态',
        accessor: (row: ReferralMatch) => (
          <Badge tone={row.state === 'accepted' ? 'success' : row.state === 'declined' ? 'default' : 'info'}>
            {row.state}
          </Badge>
        ),
      },
      {
        key: 'score',
        header: '匹配度',
        accessor: (row: ReferralMatch) => (row.score ? `${Math.round(row.score * 100)}%` : '—'),
      },
      { key: 'createdAt', header: '邀请时间', accessor: (row: ReferralMatch) => new Date(row.createdAt).toLocaleString() },
      {
        key: 'agreement',
        header: '协议',
        accessor: (row: ReferralMatch) =>
          row.agreement ? (
            <Link to="/app/agreements" className="text-sm text-emerald-600 hover:underline">
              #{row.agreement.id.slice(0, 8)}
            </Link>
          ) : row.state === 'accepted' ? (
            <span className="text-xs text-amber-500">同步中</span>
          ) : (
            '—'
          ),
      },
      {
        key: 'actions',
        header: '操作',
        accessor: (row: ReferralMatch) =>
          row.state === 'invited' ? (
            <div className="space-x-2">
              <button
                type="button"
                className="text-sm text-emerald-600 hover:underline"
                onClick={() => updateMatch(row.id, row.requestId, 'accepted')}
              >
                接受
              </button>
              <button
                type="button"
                className="text-sm text-rose-500 hover:underline"
                onClick={() => updateMatch(row.id, row.requestId, 'declined')}
              >
                拒绝
              </button>
            </div>
          ) : row.agreement ? (
            <Link to="/app/agreements" className="text-xs text-emerald-600 hover:underline">
              查看协议
            </Link>
          ) : (
            <span className="text-xs text-slate-400">已处理</span>
          ),
      },
    ],
    [updateMatch],
  );

  if (!canView) {
    return (
      <div className="rounded-xl border border-dashed border-slate-300 p-12 text-center text-sm text-slate-500 dark:border-slate-700 dark:text-slate-300">
        当前角色暂无匹配邀请。
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">待处理转介邀请</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400">查看并处理其他经纪人邀请你承接的转介。</p>
      </div>
      <Table<ReferralMatch> data={matches} loading={loading} columns={columns} />
    </div>
  );
};

export default ReferralsInbox;
