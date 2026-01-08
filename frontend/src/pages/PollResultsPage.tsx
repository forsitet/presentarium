import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { getSession, getQuestions } from '../api/polls'
import { AnswerBarChart } from '../components/AnswerBarChart'
import { Leaderboard } from '../components/Leaderboard'
import { useExport } from '../hooks/useExport'
import type { SessionDetail, Question } from '../types'

function Skeleton() {
  return (
    <div className="space-y-4 animate-pulse">
      <div className="h-8 bg-gray-200 rounded w-1/2" />
      <div className="h-4 bg-gray-100 rounded w-1/4" />
      <div className="grid grid-cols-3 gap-4 mt-6">
        {[1, 2, 3].map((i) => (
          <div key={i} className="h-20 bg-gray-100 rounded-xl" />
        ))}
      </div>
      <div className="h-64 bg-gray-100 rounded-xl mt-4" />
    </div>
  )
}

function TextResponses({ distribution }: { distribution: Record<string, number> }) {
  const entries = Object.entries(distribution)
  if (!entries.length) return <p className="text-gray-400 text-sm">Нет ответов</p>
  return (
    <ul className="space-y-1 max-h-48 overflow-y-auto">
      {entries.map(([text, count], i) => (
        <li key={i} className="flex items-center gap-2 text-sm">
          <span className="text-gray-500 shrink-0 tabular-nums w-6 text-right">{count}×</span>
          <span className="text-gray-800">{text.replace(/^"|"$/g, '')}</span>
        </li>
      ))}
    </ul>
  )
}

const QUESTION_TYPE_LABELS: Record<string, string> = {
  single_choice: 'Один ответ',
  multiple_choice: 'Несколько ответов',
  open_text: 'Открытый ответ',
  image_choice: 'Выбор изображения',
  word_cloud: 'Облако слов',
  brainstorm: 'Брейншторм',
}

export function PollResultsPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [session, setSession] = useState<SessionDetail | null>(null)
  const [questions, setQuestions] = useState<Question[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const { exportCsv, exportPdf, csvStatus, pdfStatus, errorMessage, dismissError } = useExport(id)

  useEffect(() => {
    if (!id) return
    ;(async () => {
      try {
        const s = await getSession(id)
        setSession(s)
        // fetch poll questions to get option texts for bar charts
        try {
          const qs = await getQuestions(s.poll_id)
          setQuestions(qs)
        } catch {
          // non-fatal: charts will show raw keys
        }
      } catch {
        setError('Не удалось загрузить данные сессии')
      } finally {
        setLoading(false)
      }
    })()
  }, [id])

  const formatDate = (iso?: string) => {
    if (!iso) return '—'
    return new Date(iso).toLocaleString('ru-RU', {
      day: '2-digit',
      month: '2-digit',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
  }

  const questionMap = new Map(questions.map((q) => [q.id, q]))

  return (
    <div className="min-h-screen bg-gray-50">
      <header className="bg-white border-b border-gray-200">
        <div className="max-w-4xl mx-auto px-4 py-4 flex items-center gap-4">
          <button
            onClick={() => navigate('/dashboard')}
            className="text-sm text-gray-500 hover:text-gray-700 flex items-center gap-1 transition-colors"
          >
            ← Назад
          </button>
          <h1 className="text-xl font-bold text-indigo-600">Presentarium</h1>
        </div>
      </header>

      <main className="max-w-4xl mx-auto px-4 py-8">
        {/* Toast notification */}
        {errorMessage && (
          <div className="fixed top-4 right-4 z-50 flex items-center gap-3 bg-red-600 text-white px-4 py-3 rounded-lg shadow-lg max-w-sm animate-fade-in-up">
            <span className="text-sm flex-1">{errorMessage}</span>
            <button onClick={dismissError} className="text-white/80 hover:text-white text-lg leading-none">✕</button>
          </div>
        )}

        {loading && <Skeleton />}

        {error && (
          <div className="p-4 bg-red-50 border border-red-200 rounded-lg text-red-700">{error}</div>
        )}

        {!loading && session && (
          <>
            {/* Session header */}
            <div className="mb-6">
              <h2 className="text-2xl font-bold text-gray-900">{session.poll_title}</h2>
              <p className="text-sm text-gray-500 mt-1">
                {formatDate(session.started_at)} — {formatDate(session.finished_at)}
              </p>
            </div>

            {/* Stats row */}
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-4 mb-8">
              <div className="bg-white rounded-xl border border-gray-100 p-4 text-center">
                <div className="text-2xl font-bold text-indigo-600">{session.participant_count}</div>
                <div className="text-xs text-gray-500 mt-1">Участников</div>
              </div>
              <div className="bg-white rounded-xl border border-gray-100 p-4 text-center">
                <div className="text-2xl font-bold text-indigo-600">
                  {Math.round(session.average_score)}
                </div>
                <div className="text-xs text-gray-500 mt-1">Средний балл</div>
              </div>
              <div className="bg-white rounded-xl border border-gray-100 p-4 text-center">
                <div className="text-2xl font-bold text-indigo-600">{session.questions.length}</div>
                <div className="text-xs text-gray-500 mt-1">Вопросов</div>
              </div>
            </div>

            {/* Export buttons */}
            <div className="flex gap-3 mb-8">
              <button
                onClick={exportCsv}
                disabled={csvStatus === 'loading'}
                className="px-4 py-2 bg-green-600 text-white text-sm font-medium rounded-lg hover:bg-green-700 transition-colors disabled:opacity-60 disabled:cursor-not-allowed flex items-center gap-2"
              >
                {csvStatus === 'loading' ? (
                  <><span className="inline-block w-4 h-4 border-2 border-white/40 border-t-white rounded-full animate-spin" />Скачивание…</>
                ) : csvStatus === 'success' ? (
                  '✓ Скачан'
                ) : (
                  'Скачать CSV'
                )}
              </button>
              <button
                onClick={() => exportPdf()}
                disabled={pdfStatus === 'loading'}
                className="px-4 py-2 bg-indigo-600 text-white text-sm font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-60 disabled:cursor-not-allowed flex items-center gap-2"
              >
                {pdfStatus === 'loading' ? (
                  <><span className="inline-block w-4 h-4 border-2 border-white/40 border-t-white rounded-full animate-spin" />Генерация…</>
                ) : pdfStatus === 'success' ? (
                  '✓ Скачан'
                ) : (
                  'Скачать PDF'
                )}
              </button>
            </div>

            {/* Leaderboard */}
            {session.leaderboard.length > 0 && (
              <div className="mb-8">
                <div className="bg-gray-900 rounded-xl p-5">
                  <Leaderboard entries={session.leaderboard} title="Итоговый лидерборд" />
                </div>
              </div>
            )}

            {/* Per-question stats */}
            <h3 className="text-lg font-bold text-gray-800 mb-4">Статистика по вопросам</h3>
            <div className="space-y-4">
              {session.questions.map((qStat, idx) => {
                const pollQuestion = questionMap.get(qStat.id)
                const isChoice =
                  qStat.type === 'single_choice' ||
                  qStat.type === 'multiple_choice' ||
                  qStat.type === 'image_choice'
                const isText = qStat.type === 'open_text' || qStat.type === 'word_cloud'

                return (
                  <div key={qStat.id} className="bg-white rounded-xl border border-gray-100 p-5">
                    <div className="flex items-start justify-between gap-4 mb-4">
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 mb-1">
                          <span className="text-xs font-medium text-gray-400">
                            Вопрос {idx + 1}
                          </span>
                          <span className="text-xs px-2 py-0.5 bg-gray-100 text-gray-500 rounded-full">
                            {QUESTION_TYPE_LABELS[qStat.type] ?? qStat.type}
                          </span>
                        </div>
                        <p className="text-gray-800 font-medium leading-snug">{qStat.text}</p>
                      </div>
                      <div className="text-right shrink-0">
                        <div className="text-lg font-bold text-indigo-600">{qStat.total_answers}</div>
                        <div className="text-xs text-gray-400">ответов</div>
                      </div>
                    </div>

                    {isChoice && pollQuestion?.options && qStat.answer_distribution && (
                      <div className="bg-gray-900 rounded-xl p-3" data-chart-index={idx}>
                        <AnswerBarChart
                          options={pollQuestion.options}
                          distribution={qStat.answer_distribution}
                          showCorrect
                        />
                      </div>
                    )}

                    {isText && qStat.answer_distribution && (
                      <div className="border-t border-gray-100 pt-3">
                        <p className="text-xs text-gray-400 mb-2 font-medium">Ответы участников:</p>
                        <TextResponses distribution={qStat.answer_distribution} />
                      </div>
                    )}

                    {qStat.type === 'brainstorm' && (
                      <div className="border-t border-gray-100 pt-3">
                        <p className="text-sm text-gray-500">Брейншторм — {qStat.total_answers} идей собрано</p>
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          </>
        )}
      </main>
    </div>
  )
}
