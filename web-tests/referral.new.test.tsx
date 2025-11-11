import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { beforeEach, describe, expect, it } from 'vitest';

import ReferralNewPage from '@/pages/ReferralNew';
import ReferralsPage from '@/pages/Referrals';
import { useAuthStore } from '@/store/useAuth';

const renderWithRouter = () => {
  const router = createMemoryRouter(
    [
      { path: '/app/referrals/new', element: <ReferralNewPage /> },
      { path: '/app/referrals', element: <ReferralsPage /> },
    ],
    { initialEntries: ['/app/referrals/new'] },
  );

  return { router, ...render(<RouterProvider router={router} />) };
};

describe('创建转介', () => {
  beforeEach(() => {
    useAuthStore.getState().logout();
  });

  it('提交表单后在列表中能看到新转介', async () => {
    const { router } = renderWithRouter();

    const regionSelect = screen.getByLabelText('区域');
    const minInput = screen.getByLabelText('预算下限');
    const maxInput = screen.getByLabelText('预算上限');
    const languagesSelect = screen.getByLabelText('客户语言偏好');
    const slaInput = screen.getByLabelText('SLA（小时内响应）');

    await userEvent.selectOptions(regionSelect, '新泽西');
    await userEvent.clear(minInput);
    await userEvent.type(minInput, '300000');
    await userEvent.clear(maxInput);
    await userEvent.type(maxInput, '450000');
    await userEvent.deselectOptions(languagesSelect, ['English']);
    await userEvent.selectOptions(languagesSelect, ['中文']);
    await userEvent.clear(slaInput);
    await userEvent.type(slaInput, '36');

    await userEvent.click(screen.getByRole('button', { name: '创建转介' }));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/app/referrals');
    });

    const table = await screen.findByRole('table');
    const rows = within(table).getAllByRole('row');
    const hasNewRow = rows.some((row) => within(row).queryByText('新泽西'));
    expect(hasNewRow).toBe(true);
  });
});

