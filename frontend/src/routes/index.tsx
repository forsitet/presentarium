import { createBrowserRouter, Navigate, Outlet } from 'react-router-dom'
import { useAuthStore } from '../stores/authStore'

function ProtectedRoute() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }
  return <Outlet />
}

export { ProtectedRoute }

/**
 * Wrapper around dynamic import() that auto-reloads the page once
 * when a chunk fails to load (stale deployment / cache mismatch).
 */
function lazyRetry<T>(factory: () => Promise<T>): () => Promise<T> {
  return async () => {
    try {
      return await factory()
    } catch (err) {
      // Only reload once per session to avoid infinite reload loops.
      const key = 'chunk-reload'
      if (!sessionStorage.getItem(key)) {
        sessionStorage.setItem(key, '1')
        window.location.reload()
      }
      throw err
    }
  }
}

export const router = createBrowserRouter([
  {
    path: '/login',
    lazy: lazyRetry(async () => {
      const { LoginPage } = await import('../pages/LoginPage')
      return { Component: LoginPage }
    }),
  },
  {
    path: '/register',
    lazy: lazyRetry(async () => {
      const { RegisterPage } = await import('../pages/RegisterPage')
      return { Component: RegisterPage }
    }),
  },
  {
    path: '/',
    element: <ProtectedRoute />,
    children: [
      {
        index: true,
        element: <Navigate to="/dashboard" replace />,
      },
      {
        path: 'dashboard',
        lazy: lazyRetry(async () => {
          const { DashboardPage } = await import('../pages/DashboardPage')
          return { Component: DashboardPage }
        }),
      },
      {
        path: 'polls/new',
        lazy: lazyRetry(async () => {
          const { PollEditorPage } = await import('../pages/PollEditorPage')
          return { Component: PollEditorPage }
        }),
      },
      {
        path: 'polls/:id/edit',
        lazy: lazyRetry(async () => {
          const { PollEditorPage } = await import('../pages/PollEditorPage')
          return { Component: PollEditorPage }
        }),
      },
      {
        path: 'host/:code',
        lazy: lazyRetry(async () => {
          const { HostSessionPage } = await import('../pages/HostSessionPage')
          return { Component: HostSessionPage }
        }),
      },
      {
        path: 'sessions/:id',
        lazy: lazyRetry(async () => {
          const { PollResultsPage } = await import('../pages/PollResultsPage')
          return { Component: PollResultsPage }
        }),
      },
    ],
  },
  {
    path: 'join',
    lazy: lazyRetry(async () => {
      const { JoinPage } = await import('../pages/JoinPage')
      return { Component: JoinPage }
    }),
  },
  {
    path: 'join/:code',
    lazy: lazyRetry(async () => {
      const { JoinPage } = await import('../pages/JoinPage')
      return { Component: JoinPage }
    }),
  },
  {
    path: 'session/:code',
    lazy: lazyRetry(async () => {
      const { ParticipantSessionPage } = await import('../pages/ParticipantSessionPage')
      return { Component: ParticipantSessionPage }
    }),
  },
  {
    path: 'my-results',
    lazy: lazyRetry(async () => {
      const { MyResultsPage } = await import('../pages/MyResultsPage')
      return { Component: MyResultsPage }
    }),
  },
  {
    path: '*',
    element: <Navigate to="/dashboard" replace />,
  },
])
