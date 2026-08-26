import React from "react";
import { createRoot } from "react-dom/client";
import { Navigate, RouterProvider, createHashRouter } from "react-router-dom";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import { ConfirmProvider } from "@/components/confirm";
import { Shell } from "@/components/shell";
import { ErrorBoundary } from "@/components/error-boundary";
import { LiveProvider } from "@/lib/live";
import { ThemeProvider } from "@/lib/theme";
import { AuthProvider, useAuth } from "@/lib/auth";
import Jobs from "@/routes/jobs";
import JobDetail from "@/routes/job-detail";
import Repositories from "@/routes/repositories";
import RepositoryDetail from "@/routes/repository-detail";
import FoldersRoute from "@/routes/folders";
import ActivityRoute from "@/routes/activity";
import RunView from "@/routes/run";
import NotFound from "@/routes/not-found";
import Setup from "@/routes/setup";
import Login from "@/routes/login";
import Settings from "@/routes/settings";
import "./index.css";

/**
 * RequireAuth sends first-open users to setup, everyone else to login until a
 * session exists. LiveProvider only mounts while authenticated so SSE does not
 * hammer 401s before login.
 */
function RequireAuth({ children }) {
  const { loading, setupRequired, authenticated } = useAuth();
  if (loading) {
    return <div className="min-h-dvh bg-background" />;
  }
  if (setupRequired) {
    return <Navigate to="/setup" replace />;
  }
  if (!authenticated) {
    return <Navigate to="/login" replace />;
  }
  return <LiveProvider>{children}</LiveProvider>;
}

// Hash routing keeps every URL reachable from the Go file server without any
// server-side rewrite rules, and preserves the #/jobs/... links the previous UI
// used, so old bookmarks still resolve.
const router = createHashRouter([
  { path: "/setup", element: <Setup /> },
  { path: "/login", element: <Login /> },
  {
    path: "/",
    element: (
      <RequireAuth>
        <Shell />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <Navigate to="/jobs" replace /> },
      { path: "jobs", element: <Jobs /> },
      { path: "jobs/:id", element: <JobDetail /> },
      { path: "repositories", element: <Repositories /> },
      { path: "repositories/:id", element: <RepositoryDetail /> },
      { path: "folders", element: <FoldersRoute /> },
      { path: "activity", element: <ActivityRoute /> },
      { path: "runs/:id", element: <RunView /> },
      { path: "settings", element: <Settings /> },
      { path: "*", element: <NotFound /> },
    ],
  },
]);

createRoot(document.getElementById("root")).render(
  <React.StrictMode>
    <ThemeProvider>
      <AuthProvider>
        <TooltipProvider delayDuration={250}>
          <ConfirmProvider>
            <ErrorBoundary>
              <RouterProvider router={router} />
            </ErrorBoundary>
            <Toaster />
          </ConfirmProvider>
        </TooltipProvider>
      </AuthProvider>
    </ThemeProvider>
  </React.StrictMode>,
);
