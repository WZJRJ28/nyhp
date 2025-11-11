import React, { StrictMode, useEffect } from 'react';
import ReactDOM from 'react-dom/client';
import { RouterProvider } from 'react-router-dom';

import AppRouter from '@/routes/router';
import '@/assets/styles.css';
import { useUIStore } from '@/store/useUI';

const useMocks = import.meta.env.DEV && import.meta.env.VITE_USE_MOCKS !== 'false';

async function enableMocking() {
  if (!useMocks) {
    return Promise.resolve();
  }

  const { worker } = await import('@/mocks/browser');
  return worker.start({
    onUnhandledRequest: 'bypass',
    serviceWorker: {
      url: '/mockServiceWorker.js',
    },
  });
}

const ThemeInitializer: React.FC = () => {
  const theme = useUIStore((state) => state.theme);

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark');
  }, [theme]);

  return null;
};

enableMocking().catch((error) => {
  if (useMocks) {
    console.error('MSW 启动失败，后续请求将直连 API', error);
  }
}).finally(() => {
  const rootElement = document.getElementById('root');
  if (!rootElement) {
    throw new Error('未找到根节点 #root');
  }

  ReactDOM.createRoot(rootElement).render(
    <StrictMode>
      <ThemeInitializer />
      <RouterProvider router={AppRouter} />
    </StrictMode>,
  );
});
