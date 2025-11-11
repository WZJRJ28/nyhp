import { FormEvent, useState } from 'react';
import { useNavigate } from 'react-router-dom';

import Input from '@/components/ui/Input';
import NumberInput from '@/components/ui/NumberInput';
import Select from '@/components/ui/Select';
import MultiSelect from '@/components/ui/MultiSelect';
import { useAuthStore } from '@/store/useAuth';
import { useUIStore } from '@/store/useUI';
import { http } from '@/lib/http';
import type { ReferralRequest } from '@/types';

const regions = ['曼哈顿', '皇后区', '布鲁克林', '长岛', '新泽西'];
const languagesOptions = ['English', '中文', 'Español', '한국어', 'Русский'];
const propertyTypeOptions = [
  { label: 'Condo', value: 'condo' },
  { label: 'Co-op', value: 'coop' },
  { label: '独栋 (SFH)', value: 'sfh' },
  { label: '租赁', value: 'rent' },
];
const dealTypeOptions = [
  { label: '买方', value: 'buy' },
  { label: '卖方', value: 'sell' },
  { label: '租赁', value: 'rent' },
];

const ReferralNewPage = () => {
  const navigate = useNavigate();
  const pushToast = useUIStore((state) => state.pushToast);
  const userRole = useAuthStore((state) => state.user?.role ?? 'client');
  const canCreate = userRole === 'agent' || userRole === 'broker_admin';
  const [form, setForm] = useState({
    region: regions[0],
    priceMin: 500000,
    priceMax: 900000,
    propertyType: 'condo',
    dealType: 'buy',
    languages: ['English'],
    slaHours: 48,
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);

  if (!canCreate) {
    return (
      <div className="rounded-xl border border-dashed border-slate-300 p-12 text-center text-sm text-slate-500 dark:border-slate-700 dark:text-slate-300">
        当前角色无法创建转介，请联系团队管理员。
      </div>
    );
  }

  const validate = () => {
    const nextErrors: Record<string, string> = {};
    if (!form.region) {
      nextErrors.region = '请选择区域';
    }
    if (form.priceMin <= 0 || form.priceMax <= 0) {
      nextErrors.priceMin = '价格必须大于 0';
    }
    if (form.priceMin >= form.priceMax) {
      nextErrors.priceMax = '最高价需大于最低价';
    }
    if (!form.languages.length) {
      nextErrors.languages = '至少选择一种语言';
    }
    if (form.slaHours <= 0) {
      nextErrors.slaHours = 'SLA 需为正整数';
    }
    setErrors(nextErrors);
    return Object.keys(nextErrors).length === 0;
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!validate()) {
      return;
    }

    setSubmitting(true);
    const payload = {
      region: [form.region],
      priceMin: Number(form.priceMin),
      priceMax: Number(form.priceMax),
      propertyType: form.propertyType,
      dealType: form.dealType,
      languages: form.languages,
      slaHours: Number(form.slaHours),
    };

    const result = await http.post<ReferralRequest>('/referrals', payload);
    setSubmitting(false);

    if (result.data) {
      pushToast({ title: '创建成功', description: `转介 #${result.data.id} 已创建`, type: 'success' });
      navigate(`/app/referrals?requestId=${result.data.id}`);
    } else {
      pushToast({ title: '创建失败', description: result.error?.message, type: 'error' });
    }
  };

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">创建新的转介需求</h1>
        <p className="text-sm text-slate-600 dark:text-slate-300">填写客户需求，系统将匹配最佳经纪人</p>
      </div>

      <form onSubmit={handleSubmit} className="space-y-6 rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <div className="grid gap-4 md:grid-cols-2">
          <Select
            label="区域"
            value={form.region}
            onChange={(event) => setForm((prev) => ({ ...prev, region: event.target.value }))}
            options={regions.map((item) => ({ label: item, value: item }))}
            error={errors.region}
          />
          <Select
            label="交易类型"
            value={form.dealType}
            onChange={(event) => setForm((prev) => ({ ...prev, dealType: event.target.value }))}
            options={dealTypeOptions}
          />
          <Select
            label="物业类型"
            value={form.propertyType}
            onChange={(event) => setForm((prev) => ({ ...prev, propertyType: event.target.value }))}
            options={propertyTypeOptions}
          />
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <NumberInput
            label="预算下限"
            value={form.priceMin}
            onChange={(event) => setForm((prev) => ({ ...prev, priceMin: Number(event.target.value) }))}
            error={errors.priceMin}
            min={0}
            step={1000}
            required
          />
          <NumberInput
            label="预算上限"
            value={form.priceMax}
            onChange={(event) => setForm((prev) => ({ ...prev, priceMax: Number(event.target.value) }))}
            error={errors.priceMax}
            min={0}
            step={1000}
            required
          />
        </div>

        <MultiSelect
          label="客户语言偏好"
          value={form.languages}
          onChange={(event) => {
            const selectedOptions = Array.from(event.target.selectedOptions).map((option) => option.value);
            setForm((prev) => ({ ...prev, languages: selectedOptions }));
          }}
          options={languagesOptions.map((item) => ({ label: item, value: item }))}
          error={errors.languages}
          hint="按住 Cmd/Ctrl 可多选"
          required
        />

        <NumberInput
          label="SLA（小时内响应）"
          value={form.slaHours}
          onChange={(event) => setForm((prev) => ({ ...prev, slaHours: Number(event.target.value) }))}
          error={errors.slaHours}
          min={1}
          step={1}
          required
        />

        <div className="flex items-center justify-end gap-3">
          <button
            type="button"
            className="rounded-md border border-slate-300 px-4 py-2 text-sm text-slate-600 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
            onClick={() => navigate('/app/referrals')}
          >
            取消
          </button>
          <button
            type="submit"
            className="rounded-md bg-emerald-600 px-4 py-2 text-sm font-semibold text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-60"
            disabled={submitting}
          >
            {submitting ? '提交中...' : '创建转介'}
          </button>
        </div>
      </form>
    </div>
  );
};

export default ReferralNewPage;
