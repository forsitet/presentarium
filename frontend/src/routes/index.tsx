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

export const router = createBrowserRouter([
  {
    path: '/login',
    lazy: async () => {
      const { LoginPage } = await import('../pages/LoginPage')
      return { Component: LoginPage }
    },
  },
  {
    path: '/register',
    lazy: async () => {
      const { RegisterPage } = await import('../pages/RegisterPage')
      return { Component: RegisterPage }
    },
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
        lazy: async () => {
          const { DashboardPage } = await import('../pages/DashboardPage')
          return { Component: DashboardPage }
        },
      },
      {
        path: 'polls/new',
        lazy: async () => {
          const { PollEditorPage } = await import('../pages/PollEditorPage')
          return { Component: PollEditorPage }
        },
      },
      {
        path: 'polls/:id/edit',
        lazy: async () => {
          const { PollEditorPage } = await import('../pages/PollEditorPage')
          return { Component: PollEditorPage }
        },
      },
      {
        path: 'host/:code',
        lazy: async () => {
          const { HostSessionPage } = await import('../pages/HostSessionPage')
          return { Component: HostSessionPage }
        },
      },
    ],
  },
  {
    path: 'join',
    lazy: async () => {
      const { JoinPage } = await import('../pages/JoinPage')
      return { Component: JoinPage }
    },
  },
  {
    path: 'join/:code',
    lazy: async () => {
      const { JoinPage } = await import('../pages/JoinPage')
      return { Component: JoinPage }
    },
  },
  {
    path: '*',
    element: <Navigate to="/dashboard" replace />,
  },
])
