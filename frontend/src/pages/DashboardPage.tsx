import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { PollCard } from '../components/PollCard'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { useAuthStore } from '../stores/authStore'
import { getPolls, deletePoll, copyPoll, createRoom } from '../api/polls'
import { apiClient } from '../api/client'
import type { Poll } from '../types'

function PollCardSkeleton() {
  return (
    <div className="bg-white rounded-xl border border-gray-100 p-5 flex flex-col gap-3 animate-pulse">
      <div className="h-5 bg-gray-200 rounded w-3/4" />
      <div className="h-4 bg-gray-100 rounded w-full" />
      <div className="h-3 bg-gray-100 rounded w-1/4 mt-1" />
      <div className="flex gap-2 pt-1 border-t border-gray-100">
        <div className="flex-1 h-8 bg-gray-100 rounded-lg" />
        <div className="flex-1 h-8 bg-indigo-100 rounded-lg" />
        <div className="w-20 h-8 bg-gray-100 rounded-lg" />
        <div className="w-20 h-8 bg-red-50 rounded-lg" />
      </div>
    </div>
  )
}

export function DashboardPage() {
  const navigate = useNavigate()
  const { user, logout } = useAuthStore()

  const [polls, setPolls] = useState<Poll[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [launching, setLaunching] = useState<string | null>(null)

  const loadPolls = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await getPolls()
      setPolls(data)
    } catch {
      setError('Не удалось загрузить опросы')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadPolls()
  }, [loadPolls])

  const handleLogout = async () => {
    try {
      await apiClient.post('/auth/logout')
    } catch {
      // ignore
    }
    logout()
    navigate('/login', { replace: true })
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      await deletePoll(deleteTarget)
      setPolls((prev) => prev.filter((p) => p.id !== deleteTarget))
    } catch {
      setError('Не удалось удалить опрос')
    } finally {
      setDeleteTarget(null)
    }
  }

  const handleCopy = async (id: string) => {
    try {
      const copy = await copyPoll(id)
      setPolls((prev) => [copy, ...prev])
    } catch {
      setError('Не удалось скопировать опрос')
    }
  }

  const handleLaunch = async (id: string) => {
    setLaunching(id)
    try {
      const { room_code } = await createRoom(id)
      navigate(`/host/${room_code}`, { state: { pollId: id } })
    } catch (err: unknown) {
      const axiosErr = err as { response?: { status: number } }
      if (axiosErr?.response?.status === 409) {
        setError('У этого опроса уже есть активная комната')
      } else {
        setError('Не удалось запустить опрос')
      }
    } finally {
      setLaunching(null)
    }
  }

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <header className="bg-white border-b border-gray-200">
        <div className="max-w-5xl mx-auto px-4 py-4 flex items-center justify-between">
          <h1 className="text-xl font-bold text-indigo-600">Presentarium</h1>
          <div className="flex items-center gap-4">
            <span className="text-sm text-gray-600">{user?.name}</span>
            <button
              onClick={handleLogout}
              className="text-sm text-gray-500 hover:text-gray-700 transition-colors"
            >
              Выйти
            </button>
          </div>
        </div>
      </header>

      <main className="max-w-5xl mx-auto px-4 py-8">
        {/* Page title + create button */}
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-2xl font-bold text-gray-900">Мои опросы</h2>
          <button
            onClick={() => navigate('/polls/new')}
            className="px-4 py-2 bg-indigo-600 text-white text-sm font-medium rounded-lg hover:bg-indigo-700 transition-colors"
          >
            + Создать опрос
          </button>
        </div>

        {/* Error banner */}
        {error && (
          <div className="mb-4 p-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700 flex justify-between">
            <span>{error}</span>
            <button onClick={() => setError(null)} className="ml-4 text-red-400 hover:text-red-600">
              ✕
            </button>
          </div>
        )}

        {/* Loading skeletons */}
        {loading && (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
            {[1, 2, 3].map((i) => (
              <PollCardSkeleton key={i} />
            ))}
          </div>
        )}

        {/* Empty state */}
        {!loading && polls.length === 0 && (
          <div className="flex flex-col items-center justify-center py-24 text-center">
            <div className="w-16 h-16 mb-4 rounded-full bg-indigo-50 flex items-center justify-center text-3xl">
              📋
            </div>
            <h3 className="text-lg font-semibold text-gray-700 mb-1">Пока нет опросов</h3>
            <p className="text-sm text-gray-400 mb-5">Создайте первый опрос, чтобы начать</p>
            <button
              onClick={() => navigate('/polls/new')}
              className="px-5 py-2 bg-indigo-600 text-white text-sm font-medium rounded-lg hover:bg-indigo-700 transition-colors"
            >
              Создать опрос
            </button>
          </div>
        )}

        {/* Poll cards grid */}
        {!loading && polls.length > 0 && (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
            {polls.map((poll) => (
              <div key={poll.id} className={launching === poll.id ? 'opacity-60 pointer-events-none' : ''}>
                <PollCard
                  poll={poll}
                  onEdit={(id) => navigate(`/polls/${id}/edit`)}
                  onLaunch={handleLaunch}
                  onDelete={(id) => setDeleteTarget(id)}
                  onCopy={handleCopy}
                />
              </div>
            ))}
          </div>
        )}
      </main>

      {/* Delete confirmation dialog */}
      {deleteTarget && (
        <ConfirmDialog
          title="Удалить опрос?"
          message="Это действие нельзя отменить. Все вопросы будут удалены вместе с опросом."
          confirmLabel="Удалить"
          onConfirm={handleDelete}
          onCancel={() => setDeleteTarget(null)}
        />
      )}
    </div>
  )
}
