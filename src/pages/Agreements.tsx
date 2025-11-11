import { Fragment, useEffect, useState } from 'react';
import { Dialog, Transition } from '@headlessui/react';

import Table from '@/components/ui/Table';
import Select from '@/components/ui/Select';
import NumberInput from '@/components/ui/NumberInput';
import Input from '@/components/ui/Input';
import { http } from '@/lib/http';
import { formatDate } from '@/lib/date';
import { useAuthStore } from '@/store/useAuth';
import { useUIStore } from '@/store/useUI';
import type { Agreement, Broker, Paginated, ReferralRequest } from '@/types';

interface AgreementPayload {
  requestId: string;
  referrerBrokerId: string;
  refereeBrokerId: string;
  feeRate: number;
  protectDays: number;
}

const AgreementsPage = () => {
  const [agreements, setAgreements] = useState<Agreement[]>([]);
  const [loading, setLoading] = useState(false);
  const [showModal, setShowModal] = useState(false);
  const [referrals, setReferrals] = useState<ReferralRequest[]>([]);
  const [brokers, setBrokers] = useState<Broker[]>([]);
  const [form, setForm] = useState<AgreementPayload>({
    requestId: '',
    referrerBrokerId: '',
    refereeBrokerId: '',
    feeRate: 30,
    protectDays: 90,
  });
  const pushToast = useUIStore((state) => state.pushToast);
  const currentUser = useAuthStore((state) => state.user);

  useEffect(() => {
    const load = async () => {
      setLoading(true);
      const [agreementsRes, referralsRes, brokersRes] = await Promise.all([
        http.get<Paginated<Agreement>>('/agreements?page=1&pageSize=20'),
        http.get<Paginated<ReferralRequest>>('/referrals?page=1&pageSize=50'),
        http.get<{ items: Broker[]; total: number }>('/brokers?limit=100'),
      ]);
      if (agreementsRes.data) {
        setAgreements(agreementsRes.data.items);
      }
      if (referralsRes.data) {
        setReferrals(referralsRes.data.items);
        setForm((prev) => ({
          ...prev,
          requestId: referralsRes.data.items[0]?.id ?? prev.requestId,
        }));
      }
      if (brokersRes.data) {
        setBrokers(brokersRes.data.items);
      }
      setLoading(false);
    };

    load();
  }, []);

  useEffect(() => {
    if (currentUser?.brokerId) {
      setForm((prev) => ({ ...prev, referrerBrokerId: prev.referrerBrokerId || currentUser.brokerId }));
    }
  }, [currentUser?.brokerId]);

  const handleCreate = async () => {
    if (!form.requestId || !form.referrerBrokerId || !form.refereeBrokerId) {
      pushToast({ title: '请完整填写表单', type: 'info' });
      return;
    }

    const result = await http.post<Agreement>('/agreements', form);
    if (result.data) {
      setAgreements((prev) => [result.data, ...prev]);
      pushToast({ title: '协议已创建', description: `#${result.data.id}`, type: 'success' });
      setShowModal(false);
    } else {
      pushToast({ title: '创建失败', description: result.error?.message, type: 'error' });
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">协议管理</h1>
          <p className="text-sm text-slate-600 dark:text-slate-300">跟踪转介协议与保护期</p>
        </div>
        <button
          type="button"
          className="rounded-md bg-emerald-600 px-4 py-2 text-sm font-semibold text-white hover:bg-emerald-500"
          onClick={() => setShowModal(true)}
        >
          新建协议
        </button>
      </div>

      <Table<Agreement>
        data={agreements}
        loading={loading}
        columns={[
          { key: 'id', header: '编号' },
          { key: 'requestId', header: '转介需求' },
          { key: 'feeRate', header: '分成比例', accessor: (row) => `${row.feeRate}%` },
          { key: 'protectDays', header: '保护期 (天)' },
          {
            key: 'effectiveAt',
            header: '生效时间',
            accessor: (row) => (row.effectiveAt ? formatDate(row.effectiveAt, 'yyyy-MM-dd') : '即刻'),
          },
        ]}
      />

      <Transition show={showModal} as={Fragment}>
        <Dialog as="div" className="relative z-50" onClose={() => setShowModal(false)}>
          <Transition.Child
            as={Fragment}
            enter="ease-out duration-200"
            enterFrom="opacity-0"
            enterTo="opacity-100"
            leave="ease-in duration-150"
            leaveFrom="opacity-100"
            leaveTo="opacity-0"
          >
            <div className="fixed inset-0 bg-slate-950/50" aria-hidden="true" />
          </Transition.Child>
          <div className="fixed inset-0 overflow-y-auto">
            <div className="flex min-h-full items-center justify-center p-4">
              <Transition.Child
                as={Fragment}
                enter="ease-out duration-200"
                enterFrom="opacity-0 scale-95"
                enterTo="opacity-100 scale-100"
                leave="ease-in duration-150"
                leaveFrom="opacity-100 scale-100"
                leaveTo="opacity-0 scale-95"
              >
                <Dialog.Panel className="w-full max-w-lg space-y-4 rounded-2xl bg-white p-6 shadow-xl dark:bg-slate-900">
                  <Dialog.Title className="text-lg font-semibold text-slate-900 dark:text-slate-100">
                    创建协议
                  </Dialog.Title>
                  <div className="space-y-4">
                    <Select
                      label="关联转介"
                      value={form.requestId}
                      onChange={(event) => setForm((prev) => ({ ...prev, requestId: event.target.value }))}
                      options={referrals.map((item) => ({ label: `${item.id} · ${(item.region ?? []).join(', ')}`, value: item.id }))}
                    />
                    <Select
                      label="推荐经纪公司（Referrer Broker）"
                      value={form.referrerBrokerId}
                      onChange={(event) => setForm((prev) => ({ ...prev, referrerBrokerId: event.target.value }))}
                      required
                      options={[
                        { label: form.referrerBrokerId ? '请选择其他经纪公司' : '请选择经纪公司', value: '' },
                        ...brokers.map((item) => ({
                          label: `${item.name} · ${item.fein}`,
                          value: item.id,
                        })),
                      ]}
                    />
                    {currentUser?.brokerId ? (
                      <p className="text-xs text-slate-500 dark:text-slate-400">
                        默认使用当前账号所属经纪公司，必要时可以从列表中切换。
                      </p>
                    ) : null}
                    <Select
                      label="合作经纪公司（Referee Broker）"
                      value={form.refereeBrokerId}
                      onChange={(event) => setForm((prev) => ({ ...prev, refereeBrokerId: event.target.value }))}
                      required
                      options={[
                        { label: '请选择合作经纪公司', value: '' },
                        ...brokers.map((item) => ({
                          label: `${item.name} · ${item.fein}`,
                          value: item.id,
                        })),
                      ]}
                    />
                    <NumberInput
                      label="分成比例 %"
                      value={form.feeRate}
                      onChange={(event) => setForm((prev) => ({ ...prev, feeRate: Number(event.target.value) }))}
                      min={0}
                      max={100}
                    />
                    <NumberInput
                      label="保护期 (天)"
                      value={form.protectDays}
                      onChange={(event) => setForm((prev) => ({ ...prev, protectDays: Number(event.target.value) }))}
                      min={0}
                    />
                  </div>
                  <div className="flex justify-end gap-3 text-sm">
                    <button
                      type="button"
                      className="rounded-md border border-slate-300 px-4 py-2 text-slate-600 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
                      onClick={() => setShowModal(false)}
                    >
                      取消
                    </button>
                    <button
                      type="button"
                      className="rounded-md bg-emerald-600 px-4 py-2 font-semibold text-white hover:bg-emerald-500"
                      onClick={handleCreate}
                    >
                      创建
                    </button>
                  </div>
                </Dialog.Panel>
              </Transition.Child>
            </div>
          </div>
        </Dialog>
      </Transition>
    </div>
  );
};

export default AgreementsPage;
