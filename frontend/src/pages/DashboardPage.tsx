import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { PollCard } from '../components/PollCard'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { PresentationUploader } from '../components/PresentationUploader'
import { useAuthStore } from '../stores/authStore'
import { getPolls, deletePoll, copyPoll, createRoom, getSessions } from '../api/polls'
import { listPresentations, deletePresentation } from '../api/presentations'
import { apiClient } from '../api/client'
import type { Poll, SessionSummary, Presentation } from '../types'

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

function SessionHistoryTab() {
  const navigate = useNavigate()
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getSessions()
      .then(setSessions)
      .catch(() => setError('Не удалось загрузить историю'))
      .finally(() => setLoading(false))
  }, [])

  const formatDate = (iso?: string) => {
    if (!iso) return '—'
    return new Date(iso).toLocaleDateString('ru-RU', {
      day: '2-digit',
      month: '2-digit',
      year: 'numeric',
    })
  }

  if (loading) {
    return (
      <div className="space-y-3 animate-pulse">
        {[1, 2, 3].map((i) => (
          <div key={i} className="h-20 bg-gray-100 rounded-xl" />
        ))}
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700">
        {error}
      </div>
    )
  }

  if (!sessions.length) {
    return (
      <div className="flex flex-col items-center justify-center py-24 text-center">
        <div className="w-16 h-16 mb-4 rounded-full bg-indigo-50 flex items-center justify-center text-3xl">
          📊
        </div>
        <h3 className="text-lg font-semibold text-gray-700 mb-1">История пуста</h3>
        <p className="text-sm text-gray-400">Проведите первый опрос, чтобы здесь появилась статистика</p>
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {sessions.map((s) => (
        <button
          key={s.id}
          onClick={() => navigate(`/sessions/${s.id}`)}
          className="w-full text-left bg-white rounded-xl border border-gray-100 p-4 hover:border-indigo-200 hover:shadow-sm transition-all"
        >
          <div className="flex items-center justify-between gap-4">
            <div className="flex-1 min-w-0">
              <p className="font-semibold text-gray-900 truncate">{s.poll_title}</p>
              <p className="text-xs text-gray-400 mt-0.5">{formatDate(s.started_at)}</p>
            </div>
            <div className="flex items-center gap-6 shrink-0 text-right">
              <div>
                <div className="text-sm font-bold text-gray-700">{s.participant_count}</div>
                <div className="text-xs text-gray-400">участников</div>
              </div>
              <div>
                <div className="text-sm font-bold text-indigo-600">{Math.round(s.average_score)}</div>
                <div className="text-xs text-gray-400">ср. балл</div>
              </div>
              <span className="text-gray-300 text-lg">›</span>
            </div>
          </div>
        </button>
      ))}
    </div>
  )
}

function PresentationsTab() {
  const [presentations, setPresentations] = useState<Presentation[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showUploader, setShowUploader] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await listPresentations()
      setPresentations(data)
    } catch {
      setError('Не удалось загрузить список презентаций')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const handleUploaded = (p: Presentation) => {
    setPresentations((prev) => [p, ...prev.filter((x) => x.id !== p.id)])
    setShowUploader(false)
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      await deletePresentation(deleteTarget)
      setPresentations((prev) => prev.filter((p) => p.id !== deleteTarget))
    } catch {
      setError('Не удалось удалить презентацию')
    } finally {
      setDeleteTarget(null)
    }
  }

  const formatDate = (iso: string) =>
    new Date(iso).toLocaleDateString('ru-RU', {
      day: '2-digit',
      month: '2-digit',
      year: 'numeric',
    })

  return (
    <div>
      {/* Uploader block — always visible at top, collapsible */}
      {!showUploader ? (
        <button
          onClick={() => setShowUploader(true)}
          className="w-full mb-4 px-4 py-3 bg-white border-2 border-dashed border-gray-300 rounded-xl text-sm font-medium text-gray-600 hover:border-indigo-300 hover:bg-indigo-50/50 hover:text-indigo-600 transition-colors"
        >
          + Загрузить презентацию
        </button>
      ) : (
        <div className="mb-4">
          <PresentationUploader
            onUploaded={handleUploaded}
            onCancel={() => setShowUploader(false)}
          />
        </div>
      )}

      {error && (
        <div className="mb-4 p-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700 flex justify-between">
          <span>{error}</span>
          <button onClick={() => setError(null)} className="ml-4 text-red-400 hover:text-red-600">
            ✕
          </button>
        </div>
      )}

      {loading && (
        <div className="space-y-2 animate-pulse">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-16 bg-gray-100 rounded-xl" />
          ))}
        </div>
      )}

      {!loading && presentations.length === 0 && !showUploader && (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="w-16 h-16 mb-4 rounded-full bg-indigo-50 flex items-center justify-center text-3xl">
            📽
          </div>
          <h3 className="text-lg font-semibold text-gray-700 mb-1">Пока нет презентаций</h3>
          <p className="text-sm text-gray-400">
            Загрузите .pptx, чтобы показывать слайды участникам во время опроса
          </p>
        </div>
      )}

      {!loading && presentations.length > 0 && (
        <div className="space-y-2">
          {presentations.map((p) => {
            const isReady = p.status === 'ready'
            const isFailed = p.status === 'failed'
            const isProcessing = p.status === 'processing'
            return (
              <div
                key={p.id}
                className="bg-white rounded-xl border border-gray-100 p-4 flex items-center gap-3 hover:border-indigo-200 transition-colors"
              >
                <div className="w-10 h-10 rounded-lg bg-indigo-50 text-indigo-600 flex items-center justify-center text-xl shrink-0">
                  📽
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-gray-900 truncate">
                    {p.title}
                  </p>
                  <p className="text-xs text-gray-500 mt-0.5">
                    {isReady && `${p.slide_count} слайд(ов) · ${formatDate(p.created_at)}`}
                    {isProcessing && 'Обрабатывается…'}
                    {isFailed && (p.error_message || 'Ошибка конвертации')}
                  </p>
                </div>
                {isProcessing && (
                  <div className="w-4 h-4 rounded-full border-2 border-indigo-200 border-t-indigo-500 animate-spin" />
                )}
                {isFailed && <span className="text-red-500 text-sm">⚠</span>}
                <button
                  onClick={() => setDeleteTarget(p.id)}
                  className="px-2 py-1.5 text-sm rounded-lg border border-red-200 text-red-500 hover:bg-red-50 transition-colors"
                  title="Удалить"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                </button>
              </div>
            )
          })}
        </div>
      )}

      {deleteTarget && (
        <ConfirmDialog
          title="Удалить презентацию?"
          message="Файл и все слайды будут удалены безвозвратно."
          confirmLabel="Удалить"
          onConfirm={handleDelete}
          onCancel={() => setDeleteTarget(null)}
        />
      )}
    </div>
  )
}

export function DashboardPage() {
  const navigate = useNavigate()
  const { user, logout } = useAuthStore()

  const [tab, setTab] = useState<'polls' | 'presentations' | 'history'>('polls')
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
        {/* Tabs + action button */}
        <div className="flex items-center justify-between mb-6">
          <div className="flex gap-1 bg-gray-100 rounded-lg p-1">
            <button
              onClick={() => setTab('polls')}
              className={`px-4 py-1.5 text-sm font-medium rounded-md transition-colors ${
                tab === 'polls'
                  ? 'bg-white text-gray-900 shadow-sm'
                  : 'text-gray-500 hover:text-gray-700'
              }`}
            >
              Мои опросы
            </button>
            <button
              onClick={() => setTab('presentations')}
              className={`px-4 py-1.5 text-sm font-medium rounded-md transition-colors ${
                tab === 'presentations'
                  ? 'bg-white text-gray-900 shadow-sm'
                  : 'text-gray-500 hover:text-gray-700'
              }`}
            >
              Презентации
            </button>
            <button
              onClick={() => setTab('history')}
              className={`px-4 py-1.5 text-sm font-medium rounded-md transition-colors ${
                tab === 'history'
                  ? 'bg-white text-gray-900 shadow-sm'
                  : 'text-gray-500 hover:text-gray-700'
              }`}
            >
              История
            </button>
          </div>
          {tab === 'polls' && (
            <button
              onClick={() => navigate('/polls/new')}
              className="px-4 py-2 bg-indigo-600 text-white text-sm font-medium rounded-lg hover:bg-indigo-700 transition-colors"
            >
              + Создать опрос
            </button>
          )}
        </div>

        {tab === 'history' && <SessionHistoryTab />}

        {tab === 'presentations' && <PresentationsTab />}

        {tab === 'polls' && (
          <>
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
          </>
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
