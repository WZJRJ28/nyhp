import { test, expect, request } from '@playwright/test';

const TEST_EMAIL = process.env.E2E_EMAIL ?? 'alex.agent@example.com';
const TEST_PASSWORD = process.env.E2E_PASSWORD ?? 'password';
const TEST_FULL_NAME = process.env.E2E_FULL_NAME ?? 'Alex Agent';

test.describe('Authentication flow', () => {
  test.beforeAll(async () => {
    const api = await request.newContext();
    const response = await api.post('http://127.0.0.1:8080/auth/register', {
      data: {
        email: TEST_EMAIL,
        password: TEST_PASSWORD,
        full_name: TEST_FULL_NAME,
      },
    });

    const status = response.status();
    if (status !== 201 && status !== 409) {
      const body = await response.text();
      throw new Error(`failed to ensure test user: status=${status} body=${body}`);
    }
    await api.dispose();
  });

  test('user can login via UI and sees dashboard', async ({ page }) => {
    await page.goto('/login');

    await page.fill('input[name="email"]', TEST_EMAIL);
    await page.fill('input[name="password"]', TEST_PASSWORD);

    await Promise.all([
      page.waitForURL('**/app/dashboard'),
      page.click('button[type="submit"]'),
    ]);

    await expect(page.getByRole('heading', { level: 2, name: /欢迎回来/ })).toBeVisible();
  });
});

