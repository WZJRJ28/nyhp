import { Navigate, createBrowserRouter } from 'react-router-dom';

import App from '@/App';
import RouteGuard from '@/routes/Guard';
import AppLayout from '@/layouts/AppLayout';
import ErrorBoundary from '@/components/ErrorBoundary';
import RouteErrorBoundary from '@/components/RouteErrorBoundary';
import DashboardPage from '@/pages/Dashboard';
import ReferralsPage from '@/pages/Referrals';
import ReferralNewPage from '@/pages/ReferralNew';
import ReferralsInboxPage from '@/pages/ReferralsInbox';
import AgreementsPage from '@/pages/Agreements';
import TimelinePage from '@/pages/Timeline';
import SettingsPage from '@/pages/Settings';
import LoginPage from '@/pages/Login';
import NotFoundPage from '@/pages/NotFound';

const router = createBrowserRouter([
  {
    path: '/',
    element: <Navigate to="/login" replace />,
  },
  {
    path: '/login',
    element: <LoginPage />,
    errorElement: <RouteErrorBoundary />,
  },
  {
    path: '/app',
    element: (
      <ErrorBoundary>
        <RouteGuard>
          <App />
        </RouteGuard>
      </ErrorBoundary>
    ),
    errorElement: <RouteErrorBoundary />,
    children: [
      {
        element: <AppLayout />,
        children: [
          {
            index: true,
            element: <Navigate to="dashboard" replace />,
          },
          {
            path: 'dashboard',
            element: <DashboardPage />,
          },
          {
            path: 'referrals',
            element: <ReferralsPage />,
          },
          {
            path: 'referrals/invitations',
            element: <ReferralsInboxPage />,
          },
          {
            path: 'referrals/new',
            element: <ReferralNewPage />,
          },
          {
            path: 'agreements',
            element: <AgreementsPage />,
          },
          {
            path: 'timeline',
            element: <TimelinePage />,
          },
          {
            path: 'settings',
            element: <SettingsPage />,
          },
        ],
      },
    ],
  },
  {
    path: '*',
    element: <NotFoundPage />,
  },
]);

export default router;
