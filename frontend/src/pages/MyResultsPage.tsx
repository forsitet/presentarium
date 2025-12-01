import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getParticipantSessionHistory } from '../api/polls'
import type { ParticipantHistorySummary } from '../types'

interface HistoryEntry {
  session_token: string
  room_code: string
  saved_at: string
}

interface LoadedResult extends ParticipantHistorySummary {
  saved_at: string
}

function formatDate(iso?: string): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleDateString('ru-RU', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  })
}

function rankEmoji(rank: number): string {
  if (rank === 1) return '🥇'
  if (rank === 2) return '🥈'
  if (rank === 3) return '🥉'
  return `#${rank}`
}

export function MyResultsPage() {
  const [results, setResults] = useState<LoadedResult[]>([])
  const [loading, setLoading] = useState(true)
  const [hasHistory, setHasHistory] = useState(true)

  useEffect(() => {
    let raw: string | null = null
    try {
      raw = localStorage.getItem('participant_history')
    } catch {
      setHasHistory(false)
      setLoading(false)
      return
    }

    if (!raw) {
      setLoading(false)
      return
    }

    let entries: HistoryEntry[] = []
    try {
      entries = JSON.parse(raw)
    } catch {
      setLoading(false)
      return
    }

    if (entries.length === 0) {
      setLoading(false)
      return
    }

    Promise.all(
      entries.map(async (entry) => {
        try {
          const summary = await getParticipantSessionHistory(entry.session_token)
          return { ...summary, saved_at: entry.saved_at } as LoadedResult
        } catch {
          return null
        }
      }),
    ).then((loaded) => {
      setResults(loaded.filter((r): r is LoadedResult => r !== null))
      setLoading(false)
    })
  }, [])

  return (
    <div className="min-h-screen bg-gradient-to-br from-indigo-900 via-purple-900 to-pink-900 flex items-center justify-center p-4">
      <div className="w-full max-w-lg">
        <div className="text-center mb-6">
          <div className="text-5xl mb-3">📋</div>
          <h1 className="text-3xl font-bold text-white mb-1">Мои результаты</h1>
          <p className="text-white/60 text-sm">История пройденных опросов</p>
        </div>

        <div className="bg-white rounded-2xl shadow-2xl p-6">
          {!hasHistory && (
            <div className="text-center py-8">
              <div className="text-4xl mb-3">⚠️</div>
              <p className="text-gray-600 font-medium mb-1">История недоступна</p>
              <p className="text-gray-400 text-sm">
                История хранится в localStorage браузера. Она могла быть очищена.
              </p>
            </div>
          )}

          {hasHistory && loading && (
            <div className="space-y-4">
              {[1, 2, 3].map((i) => (
                <div key={i} className="animate-pulse bg-gray-100 rounded-xl h-20" />
              ))}
            </div>
          )}

          {hasHistory && !loading && results.length === 0 && (
            <div className="text-center py-8">
              <div className="text-4xl mb-3">🎯</div>
              <p className="text-gray-600 font-medium mb-1">Опросов пока нет</p>
              <p className="text-gray-400 text-sm">Пройдите свой первый опрос!</p>
            </div>
          )}

          {hasHistory && !loading && results.length > 0 && (
            <div className="space-y-3">
              {results.map((r) => (
                <div
                  key={r.session_id}
                  className="border border-gray-200 rounded-xl p-4 hover:bg-gray-50 transition-colors"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="font-semibold text-gray-900 truncate">{r.poll_title}</p>
                      <p className="text-gray-400 text-sm mt-0.5">{formatDate(r.finished_at || r.saved_at)}</p>
                    </div>
                    <div className="shrink-0 text-right">
                      <div className="text-2xl font-black text-indigo-600">{r.total_score}</div>
                      <div className="text-xs text-gray-400">очков</div>
                    </div>
                  </div>
                  <div className="flex items-center gap-4 mt-3 pt-3 border-t border-gray-100">
                    <span className="text-lg">{rankEmoji(r.my_rank)}</span>
                    <span className="text-sm text-gray-600">
                      {r.my_rank} место из {r.total_participants}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )}

          <div className="mt-6 pt-4 border-t border-gray-100 text-center">
            <Link
              to="/join"
              className="inline-block bg-indigo-600 text-white px-6 py-2.5 rounded-lg font-medium hover:bg-indigo-700 transition-colors text-sm"
            >
              Войти в опрос
            </Link>
          </div>
        </div>

        {hasHistory && (
          <p className="text-center text-white/40 text-xs mt-4">
            История хранится локально в браузере. Очистка браузера удалит её.
          </p>
        )}
      </div>
    </div>
  )
}
