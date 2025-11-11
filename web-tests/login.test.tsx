import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { beforeEach, describe, expect, it } from 'vitest';

import LoginPage from '@/pages/Login';
import DashboardPage from '@/pages/Dashboard';
import { useAuthStore } from '@/store/useAuth';

describe('登录流程', () => {
  beforeEach(() => {
    useAuthStore.getState().logout();
  });

  it('可以登录并看到 dashboard 问候语', async () => {
    const router = createMemoryRouter(
      [
        { path: '/login', element: <LoginPage /> },
        { path: '/app/dashboard', element: <DashboardPage /> },
      ],
      { initialEntries: ['/login'] },
    );

    render(<RouterProvider router={router} />);

    const emailField = screen.getByLabelText('邮箱');
    const passwordField = screen.getByLabelText('密码');
    const submitButton = screen.getByRole('button', { name: '登录' });

    await userEvent.clear(emailField);
    await userEvent.type(emailField, 'alex.agent@example.com');
    await userEvent.clear(passwordField);
    await userEvent.type(passwordField, 'password');
    await userEvent.click(submitButton);

    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/app/dashboard');
    });

    expect(await screen.findByText(/欢迎回来/i)).toBeInTheDocument();
  });
});

