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
    ],
  },
  {
    path: '*',
    element: <Navigate to="/dashboard" replace />,
  },
])
