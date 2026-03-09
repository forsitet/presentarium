import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { User } from '../types'

interface AuthState {
  user: User | null
  accessToken: string | null
  isAuthenticated: boolean
  login: (user: User, token: string) => void
  logout: () => void
  setToken: (token: string) => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      user: null,
      accessToken: null,
      isAuthenticated: false,
      login: (user, token) => {
        localStorage.setItem('access_token', token)
        set({ user, accessToken: token, isAuthenticated: true })
      },
      logout: () => {
        localStorage.removeItem('access_token')
        set({ user: null, accessToken: null, isAuthenticated: false })
      },
      setToken: (token) => {
        localStorage.setItem('access_token', token)
        set({ accessToken: token })
      },
    }),
    {
      name: 'auth-store',
      partialize: (state) => ({ user: state.user, accessToken: state.accessToken, isAuthenticated: state.isAuthenticated }),
    }
  )
)
