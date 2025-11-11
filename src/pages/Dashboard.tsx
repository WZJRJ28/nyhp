import { useEffect, useMemo, useState } from 'react';

import KPI from '@/components/ui/KPI';
import Table from '@/components/ui/Table';
import Badge from '@/components/ui/Badge';
import { http } from '@/lib/http';
import { fromNow } from '@/lib/date';
import type { Paginated, ReferralRequest } from '@/types';

const DashboardPage = () => {
  const [referrals, setReferrals] = useState<ReferralRequest[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    const load = async () => {
      setLoading(true);
      const result = await http.get<Paginated<ReferralRequest>>('/referrals?page=1&pageSize=12');
      if (result.data) {
        setReferrals(result.data.items);
      }
      setLoading(false);
    };

    load();
  }, []);

  const kpis = useMemo(() => {
    const total = referrals.length;
    const open = referrals.filter((item) => item.status === 'open').length;
    const inProgress = referrals.filter((item) => item.status === 'matched' || item.status === 'in_progress').length;
    const avgSla = referrals.length
      ? Math.round(referrals.reduce((acc, cur) => acc + cur.slaHours, 0) / referrals.length)
      : 0;

    return [
      { title: '开放转介', value: String(open), change: { value: `${total} 总计`, positive: true } },
      {
        title: '匹配 / 进行中',
        value: String(inProgress),
        change: {
          value: `${inProgress ? Math.round((inProgress / Math.max(total, 1)) * 100) : 0}% 占比`,
          positive: inProgress > 0,
        },
      },
      { title: '平均 SLA (小时)', value: `${avgSla}`, change: { value: '越低越好', positive: avgSla < 72 } },
    ];
  }, [referrals]);

  return (
    <div className="space-y-6">
      <section>
        <h2 className="text-lg font-semibold text-slate-900 dark:text-slate-100">欢迎回来 👋</h2>
        <p className="text-sm text-slate-600 dark:text-slate-300">快速了解经纪人转介网络运行情况</p>
      </section>

      <section className="grid gap-4 md:grid-cols-3">
        {kpis.map((item) => (
          <KPI key={item.title} title={item.title} value={item.value} change={item.change} />
        ))}
      </section>

      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">最新转介</h3>
        </div>
        <Table<ReferralRequest>
        data={referrals.slice(0, 6)}
        loading={loading}
        columns={[
          { key: 'id', header: '编号' },
          { key: 'region', header: '区域', accessor: (row) => (row.region ?? []).join(', ') },
            {
              key: 'price',
              header: '价格区间',
              accessor: (row) => `$${row.priceMin.toLocaleString()} - $${row.priceMax.toLocaleString()}`,
            },
            { key: 'propertyType', header: '物业类型' },
            { key: 'languages', header: '语言', accessor: (row) => (row.languages ?? []).join(', ') },
            {
              key: 'status',
              header: '状态',
              accessor: (row) => (
                <Badge
                  tone={
                    row.status === 'open'
                      ? 'info'
                      : row.status === 'matched' || row.status === 'in_progress'
                        ? 'success'
                        : 'default'
                  }
                >
                  {row.status}
                </Badge>
              ),
            },
            { key: 'createdAt', header: '创建时间', accessor: (row) => fromNow(row.createdAt) },
          ]}
        />
      </section>
    </div>
  );
};

export default DashboardPage;
