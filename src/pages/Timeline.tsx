import { useEffect, useMemo, useState } from 'react';

import { http } from '@/lib/http';
import { useUIStore } from '@/store/useUI';
import { fromNow, formatDate } from '@/lib/date';
import type { Agreement, Paginated, TimelineEvent } from '@/types';

interface GroupedTimeline {
  agreement: Agreement | undefined;
  events: TimelineEvent[];
}

const TimelinePage = () => {
  const [events, setEvents] = useState<TimelineEvent[]>([]);
  const [agreements, setAgreements] = useState<Agreement[]>([]);
  const pushToast = useUIStore((state) => state.pushToast);

  useEffect(() => {
    const load = async () => {
      const [eventRes, agreementRes] = await Promise.all([
        http.get<Paginated<TimelineEvent>>('/events?page=1&pageSize=50'),
        http.get<Paginated<Agreement>>('/agreements?page=1&pageSize=50'),
      ]);
      if (eventRes.data) {
        setEvents(eventRes.data.items);
      } else if (eventRes.error) {
        pushToast({ title: '事件加载失败', description: eventRes.error.message, type: 'error' });
      }
      if (agreementRes.data) {
        setAgreements(agreementRes.data.items);
      } else if (agreementRes.error) {
        pushToast({ title: '协议加载失败', description: agreementRes.error.message, type: 'error' });
      }
    };

    load();
  }, [pushToast]);

  const grouped = useMemo<GroupedTimeline[]>(() => {
    const map = new Map<string, TimelineEvent[]>();
    events.forEach((event) => {
      if (!map.has(event.agreementId)) {
        map.set(event.agreementId, []);
      }
      map.get(event.agreementId)?.push(event);
    });

    return Array.from(map.entries()).map(([agreementId, groupEvents]) => {
      const agreement = agreements.find((item) => item.id === agreementId);
      const sortedEvents = groupEvents.sort((a, b) => new Date(b.at).getTime() - new Date(a.at).getTime());
      return { agreement, events: sortedEvents };
    });
  }, [events, agreements]);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">成交时间线</h1>
        <p className="text-sm text-slate-600 dark:text-slate-300">按协议聚合的最新动态</p>
      </div>

      {grouped.length === 0 ? (
        <div className="rounded-xl border border-dashed border-slate-300 p-12 text-center text-sm text-slate-500 dark:border-slate-700 dark:text-slate-300">
          暂无时间线事件
        </div>
      ) : (
        <div className="space-y-6">
          {grouped.map((group) => (
            <section
              key={group.agreement?.id ?? group.events[0]?.agreementId}
              className="rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900"
            >
              <header className="mb-4">
                <h2 className="text-lg font-semibold text-slate-900 dark:text-slate-100">
                  协议 #{group.agreement?.id ?? group.events[0]?.agreementId}
                </h2>
                <p className="text-xs text-slate-500 dark:text-slate-400">
                  保护期：{group.agreement?.protectDays ?? '—'} 天 · 分成：{group.agreement?.feeRate ?? '—'}%
                </p>
              </header>
              <ol className="space-y-4">
                {group.events.map((event) => (
                  <li key={event.id} className="flex gap-4 text-sm">
                    <span className="mt-1 h-2 w-2 rounded-full bg-emerald-500" aria-hidden="true" />
                    <div className="flex-1">
                      <p className="font-semibold text-slate-800 dark:text-slate-100">{event.type}</p>
                      <p className="text-xs text-slate-500 dark:text-slate-400">
                        {formatDate(event.at)} · {fromNow(event.at)}
                      </p>
                      {event.payload ? (
                        <pre className="mt-2 rounded-md bg-slate-100 p-2 text-xs text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                          {JSON.stringify(event.payload, null, 2)}
                        </pre>
                      ) : null}
                    </div>
                  </li>
                ))}
              </ol>
            </section>
          ))}
        </div>
      )}
    </div>
  );
};

export default TimelinePage;
