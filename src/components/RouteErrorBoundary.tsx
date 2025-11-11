import { isRouteErrorResponse, useRouteError } from 'react-router-dom';

const RouteErrorBoundary = () => {
  const error = useRouteError();

  if (isRouteErrorResponse(error)) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center bg-slate-50 px-6 text-center dark:bg-slate-900">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{error.status}</h1>
        <p className="mt-2 text-sm text-slate-600 dark:text-slate-300">{error.statusText}</p>
      </div>
    );
  }

  if (error instanceof Error) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center bg-slate-50 px-6 text-center dark:bg-slate-900">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">页面发生错误</h1>
        <p className="mt-2 text-sm text-slate-600 dark:text-slate-300">{error.message}</p>
      </div>
    );
  }

  return null;
};

export default RouteErrorBoundary;
