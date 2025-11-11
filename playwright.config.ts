import { defineConfig } from '@playwright/test';

const backendEnv = {
  DATABASE_URL: process.env.DATABASE_URL ?? 'postgres://testuser:pass@127.0.0.1:5432/acn_stress?sslmode=disable',
  JWT_SECRET: process.env.JWT_SECRET ?? 'integration-secret',
};

export default defineConfig({
  testDir: './integration',
  timeout: 120_000,
  expect: {
    timeout: 10_000,
  },
  fullyParallel: true,
  use: {
    baseURL: 'http://127.0.0.1:5173',
    trace: 'retain-on-failure',
  },
  webServer: [
    {
      command: 'go run ./cmd/api',
      cwd: 'backend',
      env: backendEnv,
      port: 8080,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      command: 'npm run dev -- --host 127.0.0.1 --port 5173',
      cwd: '.',
      env: {
        VITE_USE_MOCKS: 'false',
        VITE_BYPASS_AUTH: 'false',
      },
      port: 5173,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
  ],
});
