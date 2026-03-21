import { useState, useEffect, useCallback, useRef } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { QuestionTypeSelector } from '../components/QuestionTypeSelector'
import { QuestionEditorPanel } from '../components/QuestionEditorPanel'
import { QuestionPreview } from '../components/QuestionPreview'
import { ConfirmDialog } from '../components/ConfirmDialog'
import {
  getPoll,
  createPoll,
  updatePoll,
  getQuestions,
  createQuestion,
  updateQuestion,
  deleteQuestion,
  reorderQuestions,
} from '../api/polls'
import type { Poll, Question } from '../types'

type ScoringRule = Poll['scoring_rule']
type QuestionOrder = Poll['question_order']
type QuestionType = Question['type']

const TYPE_ICONS: Record<QuestionType, string> = {
  single_choice: '\u2611\uFE0F',
  multiple_choice: '\u2705',
  open_text: '\uD83D\uDCDD',
  image_choice: '\uD83D\uDDBC\uFE0F',
  word_cloud: '\u2601\uFE0F',
  brainstorm: '\uD83E\uDDE0',
}

const TYPE_SHORT: Record<QuestionType, string> = {
  single_choice: '\u041E\u0434\u0438\u043D.',
  multiple_choice: '\u041C\u043D\u043E\u0436.',
  open_text: '\u0422\u0435\u043A\u0441\u0442',
  image_choice: '\u041A\u0430\u0440\u0442.',
  word_cloud: '\u041E\u0431\u043B\u0430\u043A\u043E',
  brainstorm: '\u0428\u0442\u0443\u0440\u043C',
}

function makeDefaultOptions(): Array<{ text: string; is_correct: boolean }> {
  return [
    { text: '', is_correct: false },
    { text: '', is_correct: false },
  ]
}

export function PollEditorPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const isNew = !id

  // Poll state
  const [pollId, setPollId] = useState<string | null>(id ?? null)
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [scoringRule, setScoringRule] = useState<ScoringRule>('none')
  const [questionOrder, setQuestionOrder] = useState<QuestionOrder>('sequential')

  // Questions
  const [questions, setQuestions] = useState<Question[]>([])
  const [selectedQuestionId, setSelectedQuestionId] = useState<string | null>(null)

  // UI state
  const [loading, setLoading] = useState(!isNew)
  const [error, setError] = useState<string | null>(null)
  const [saveStatus, setSaveStatus] = useState<'idle' | 'saved' | 'error'>('idle')
  const [showTypeSelector, setShowTypeSelector] = useState(false)
  const [deleteTargetId, setDeleteTargetId] = useState<string | null>(null)

  // Refs for debounce
  const debounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const saveStatusRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const pollCreatedRef = useRef(false)
  const pollIdRef = useRef<string | null>(id ?? null)
  const creatingPollRef = useRef<Promise<string> | null>(null)

  // Keep pollIdRef in sync with state
  useEffect(() => {
    pollIdRef.current = pollId
  }, [pollId])

  // Cleanup timers
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
      if (saveStatusRef.current) clearTimeout(saveStatusRef.current)
    }
  }, [])

  const flashSaveStatus = useCallback((status: 'saved' | 'error') => {
    setSaveStatus(status)
    if (saveStatusRef.current) clearTimeout(saveStatusRef.current)
    saveStatusRef.current = setTimeout(() => setSaveStatus('idle'), 3000)
  }, [])

  // Load existing poll
  useEffect(() => {
    if (!id) return
    let cancelled = false
    async function load() {
      setLoading(true)
      setError(null)
      try {
        const [poll, qs] = await Promise.all([getPoll(id!), getQuestions(id!)])
        if (cancelled) return
        setTitle(poll.title)
        setDescription(poll.description ?? '')
        setScoringRule(poll.scoring_rule)
        setQuestionOrder(poll.question_order)
        const sorted = qs.sort((a, b) => a.position - b.position)
        setQuestions(sorted)
        if (sorted.length > 0) setSelectedQuestionId(sorted[0].id)
      } catch {
        if (!cancelled) setError('Не удалось загрузить опрос')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [id])

  // Ensure poll exists, creating it if necessary. Uses a lock (creatingPollRef)
  // so that concurrent callers (debounced save + add question) don't create
  // duplicate polls.
  const ensurePoll = useCallback(
    async (t: string, desc: string, scoring: ScoringRule, order: QuestionOrder): Promise<string> => {
      // Already created — return immediately
      if (pollIdRef.current && pollCreatedRef.current) {
        return pollIdRef.current
      }
      // Existing poll being edited (not new)
      if (pollIdRef.current && !isNew) {
        return pollIdRef.current
      }
      // Another caller is already creating — wait for it
      if (creatingPollRef.current) {
        return creatingPollRef.current
      }
      // Create the poll (with lock)
      const promise = createPoll({
        title: t.trim() || 'Новый опрос',
        description: desc.trim() || undefined,
        scoring_rule: scoring,
        question_order: order,
      }).then((poll) => {
        pollIdRef.current = poll.id
        setPollId(poll.id)
        pollCreatedRef.current = true
        window.history.replaceState(null, '', `/polls/${poll.id}/edit`)
        creatingPollRef.current = null
        return poll.id
      }).catch((err) => {
        creatingPollRef.current = null
        throw err
      })
      creatingPollRef.current = promise
      return promise
    },
    [isNew],
  )

  // Auto-save poll settings with debounce
  const savePollSettings = useCallback(
    async (newTitle: string, newDesc: string, newScoring: ScoringRule, newOrder: QuestionOrder) => {
      if (!newTitle.trim()) return

      try {
        const alreadyExisted = pollCreatedRef.current || !isNew
        const currentId = await ensurePoll(newTitle, newDesc, newScoring, newOrder)
        // Only update if the poll already existed before ensurePoll
        // (ensurePoll already created it with these settings otherwise)
        if (alreadyExisted) {
          await updatePoll(currentId, {
            title: newTitle.trim(),
            description: newDesc.trim() || undefined,
            scoring_rule: newScoring,
            question_order: newOrder,
          })
        }
        flashSaveStatus('saved')
      } catch {
        flashSaveStatus('error')
      }
    },
    [ensurePoll, isNew, flashSaveStatus],
  )

  const debouncedSave = useCallback(
    (newTitle: string, newDesc: string, newScoring: ScoringRule, newOrder: QuestionOrder) => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
      debounceRef.current = setTimeout(() => {
        savePollSettings(newTitle, newDesc, newScoring, newOrder)
      }, 500)
    },
    [savePollSettings],
  )

  const handleTitleChange = (val: string) => {
    setTitle(val)
    debouncedSave(val, description, scoringRule, questionOrder)
  }

  const handleDescriptionChange = (val: string) => {
    setDescription(val)
    debouncedSave(title, val, scoringRule, questionOrder)
  }

  const handleScoringChange = (val: ScoringRule) => {
    setScoringRule(val)
    debouncedSave(title, description, val, questionOrder)
  }

  const handleOrderChange = (val: QuestionOrder) => {
    setQuestionOrder(val)
    debouncedSave(title, description, scoringRule, val)
  }

  // Add question
  const handleAddQuestion = async (type: QuestionType) => {
    setShowTypeSelector(false)

    // Ensure poll exists first (uses lock to prevent duplicate creation)
    let currentPollId: string
    try {
      if (!title.trim()) {
        setTitle('Новый опрос')
      }
      currentPollId = await ensurePoll(title || 'Новый опрос', description, scoringRule, questionOrder)
    } catch {
      flashSaveStatus('error')
      return
    }

    const hasOptions = ['single_choice', 'multiple_choice', 'image_choice'].includes(type)
    const position = questions.length + 1

    try {
      const q = await createQuestion(currentPollId, {
        type,
        text: '',
        options: hasOptions ? makeDefaultOptions() : undefined,
        time_limit_seconds: 30,
        points: 100,
        position,
      })
      setQuestions((prev) => [...prev, q])
      setSelectedQuestionId(q.id)
      flashSaveStatus('saved')
    } catch {
      flashSaveStatus('error')
    }
  }

  // Save question
  const handleSaveQuestion = async (updated: Question) => {
    if (!pollId) return

    // Optimistic update
    setQuestions((prev) => prev.map((q) => (q.id === updated.id ? updated : q)))

    try {
      const saved = await updateQuestion(pollId, updated.id, {
        text: updated.text,
        options: updated.options,
        time_limit_seconds: updated.time_limit_seconds,
        points: updated.points,
      })
      setQuestions((prev) => prev.map((q) => (q.id === saved.id ? saved : q)))
    } catch {
      flashSaveStatus('error')
    }
  }

  // Delete question
  const handleDeleteQuestion = async () => {
    if (!pollId || !deleteTargetId) return

    try {
      await deleteQuestion(pollId, deleteTargetId)
      setQuestions((prev) => {
        const next = prev
          .filter((q) => q.id !== deleteTargetId)
          .map((q, i) => ({ ...q, position: i + 1 }))
        return next
      })
      if (selectedQuestionId === deleteTargetId) {
        setSelectedQuestionId(
          questions.find((q) => q.id !== deleteTargetId)?.id ?? null,
        )
      }
      flashSaveStatus('saved')
    } catch {
      flashSaveStatus('error')
    } finally {
      setDeleteTargetId(null)
    }
  }

  // Reorder
  const moveQuestion = async (questionId: string, direction: 'up' | 'down') => {
    const index = questions.findIndex((q) => q.id === questionId)
    if (index === -1) return
    if (direction === 'up' && index === 0) return
    if (direction === 'down' && index === questions.length - 1) return

    const swapIndex = direction === 'up' ? index - 1 : index + 1
    const next = [...questions]
    const temp = next[index]
    next[index] = next[swapIndex]
    next[swapIndex] = temp

    // Update positions
    const reordered = next.map((q, i) => ({ ...q, position: i + 1 }))
    setQuestions(reordered)

    if (pollId) {
      try {
        await reorderQuestions(
          pollId,
          reordered.map((q) => ({ id: q.id, position: q.position })),
        )
      } catch {
        flashSaveStatus('error')
      }
    }
  }

  const selectedQuestion = questions.find((q) => q.id === selectedQuestionId) ?? null

  // Loading skeleton
  if (loading) {
    return (
      <div className="min-h-screen bg-gray-50">
        <header className="bg-white border-b border-gray-200">
          <div className="max-w-7xl mx-auto px-4 py-4 flex items-center gap-4">
            <div className="h-5 w-32 bg-gray-200 rounded animate-pulse" />
            <div className="flex-1" />
            <div className="h-5 w-20 bg-gray-200 rounded animate-pulse" />
          </div>
        </header>
        <main className="max-w-7xl mx-auto px-4 py-8">
          <div className="space-y-4">
            <div className="h-10 bg-gray-200 rounded-lg w-1/2 animate-pulse" />
            <div className="h-8 bg-gray-200 rounded-lg w-3/4 animate-pulse" />
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 mt-6">
              <div className="lg:col-span-1 space-y-3">
                {[1, 2, 3].map((i) => (
                  <div key={i} className="h-16 bg-gray-200 rounded-lg animate-pulse" />
                ))}
              </div>
              <div className="lg:col-span-2 h-64 bg-gray-200 rounded-lg animate-pulse" />
            </div>
          </div>
        </main>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <header className="bg-white border-b border-gray-200 sticky top-0 z-30">
        <div className="max-w-7xl mx-auto px-4 py-3 flex items-center gap-4">
          <button
            onClick={() => navigate('/dashboard')}
            className="text-sm text-gray-600 hover:text-gray-900 transition-colors flex items-center gap-1"
          >
            <span>&larr;</span>
            <span>Назад к опросам</span>
          </button>
          <div className="flex-1" />
          <span className="text-xl font-bold text-indigo-600">Presentarium</span>
          <div className="flex-1" />
          {saveStatus === 'saved' && (
            <span className="text-sm text-emerald-600 font-medium">
              Сохранено
            </span>
          )}
          {saveStatus === 'error' && (
            <span className="text-sm text-red-600 font-medium">
              Ошибка сохранения
            </span>
          )}
          {saveStatus === 'idle' && <span className="w-28" />}
        </div>
      </header>

      <main className="max-w-7xl mx-auto px-4 py-6">
        {/* Error banner */}
        {error && (
          <div className="mb-4 p-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700 flex justify-between">
            <span>{error}</span>
            <button onClick={() => setError(null)} className="ml-4 text-red-400 hover:text-red-600">
              &times;
            </button>
          </div>
        )}

        {/* Poll settings */}
        <div className="bg-white rounded-xl border border-gray-200 p-5 mb-6">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="md:col-span-2">
              <label htmlFor="poll-title" className="block text-sm font-medium text-gray-700 mb-1">
                Название опроса
              </label>
              <input
                id="poll-title"
                type="text"
                value={title}
                onChange={(e) => handleTitleChange(e.target.value)}
                placeholder="Введите название..."
                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
            </div>
            <div className="md:col-span-2">
              <label htmlFor="poll-desc" className="block text-sm font-medium text-gray-700 mb-1">
                Описание
              </label>
              <input
                id="poll-desc"
                type="text"
                value={description}
                onChange={(e) => handleDescriptionChange(e.target.value)}
                placeholder="Необязательное описание..."
                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
            </div>
            <div>
              <label htmlFor="poll-scoring" className="block text-sm font-medium text-gray-700 mb-1">
                Правило начисления баллов
              </label>
              <select
                id="poll-scoring"
                value={scoringRule}
                onChange={(e) => handleScoringChange(e.target.value as ScoringRule)}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent bg-white"
              >
                <option value="none">Без баллов</option>
                <option value="correct_answer">За правильный ответ</option>
                <option value="speed_bonus">Бонус за скорость</option>
              </select>
            </div>
            <div>
              <label htmlFor="poll-order" className="block text-sm font-medium text-gray-700 mb-1">
                Порядок вопросов
              </label>
              <select
                id="poll-order"
                value={questionOrder}
                onChange={(e) => handleOrderChange(e.target.value as QuestionOrder)}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent bg-white"
              >
                <option value="sequential">Последовательный</option>
                <option value="random">Случайный</option>
              </select>
            </div>
          </div>
        </div>

        {/* Two-column layout: question list + editor */}
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          {/* Left column: question list */}
          <div className="lg:col-span-1">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-semibold text-gray-700">
                Вопросы ({questions.length})
              </h3>
            </div>

            {questions.length === 0 && (
              <div className="text-center py-10 bg-white rounded-xl border border-dashed border-gray-300">
                <p className="text-sm text-gray-400 mb-3">Пока нет вопросов</p>
                <button
                  onClick={() => setShowTypeSelector(true)}
                  className="px-4 py-2 bg-indigo-600 text-white text-sm font-medium rounded-lg hover:bg-indigo-700 transition-colors"
                >
                  + Добавить вопрос
                </button>
              </div>
            )}

            {questions.length > 0 && (
              <div className="space-y-2">
                {questions.map((q, index) => (
                  <div
                    key={q.id}
                    onClick={() => setSelectedQuestionId(q.id)}
                    className={`flex items-center gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                      selectedQuestionId === q.id
                        ? 'border-indigo-300 bg-indigo-50'
                        : 'border-gray-200 bg-white hover:border-gray-300'
                    }`}
                  >
                    {/* Reorder buttons */}
                    <div className="flex flex-col gap-0.5 flex-shrink-0">
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          moveQuestion(q.id, 'up')
                        }}
                        disabled={index === 0}
                        className="text-xs text-gray-400 hover:text-gray-700 disabled:opacity-20 disabled:pointer-events-none leading-none"
                        title="Вверх"
                      >
                        &uarr;
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          moveQuestion(q.id, 'down')
                        }}
                        disabled={index === questions.length - 1}
                        className="text-xs text-gray-400 hover:text-gray-700 disabled:opacity-20 disabled:pointer-events-none leading-none"
                        title="Вниз"
                      >
                        &darr;
                      </button>
                    </div>

                    {/* Question number */}
                    <span className="flex-shrink-0 w-6 h-6 rounded-full bg-gray-100 text-xs font-medium text-gray-600 flex items-center justify-center">
                      {index + 1}
                    </span>

                    {/* Content */}
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-1.5 mb-0.5">
                        <span className="text-sm">{TYPE_ICONS[q.type]}</span>
                        <span className="text-xs text-gray-500 font-medium">
                          {TYPE_SHORT[q.type]}
                        </span>
                      </div>
                      <p className="text-sm text-gray-800 truncate">
                        {q.text || 'Без текста'}
                      </p>
                    </div>
                  </div>
                ))}

                <button
                  onClick={() => setShowTypeSelector(true)}
                  className="w-full py-2.5 rounded-lg border border-dashed border-gray-300 text-sm text-gray-500 hover:border-indigo-300 hover:text-indigo-600 transition-colors"
                >
                  + Добавить вопрос
                </button>
              </div>
            )}
          </div>

          {/* Right column: editor + preview */}
          <div className="lg:col-span-2 space-y-5">
            {selectedQuestion ? (
              <>
                <QuestionEditorPanel
                  key={selectedQuestion.id}
                  question={selectedQuestion}
                  pollId={pollId!}
                  onSave={handleSaveQuestion}
                  onDelete={() => setDeleteTargetId(selectedQuestion.id)}
                />
                <div>
                  <h4 className="text-sm font-medium text-gray-500 mb-2">
                    Предпросмотр для участников
                  </h4>
                  <QuestionPreview question={selectedQuestion} />
                </div>
              </>
            ) : (
              <div className="flex items-center justify-center h-64 bg-white rounded-xl border border-gray-200">
                <p className="text-sm text-gray-400">
                  {questions.length === 0
                    ? 'Добавьте первый вопрос, чтобы начать'
                    : 'Выберите вопрос для редактирования'}
                </p>
              </div>
            )}
          </div>
        </div>
      </main>

      {/* Question type selector modal */}
      {showTypeSelector && (
        <QuestionTypeSelector
          onSelect={handleAddQuestion}
          onClose={() => setShowTypeSelector(false)}
        />
      )}

      {/* Delete question confirmation */}
      {deleteTargetId && (
        <ConfirmDialog
          title="Удалить вопрос?"
          message="Это действие нельзя отменить. Вопрос будет удален из опроса."
          confirmLabel="Удалить"
          onConfirm={handleDeleteQuestion}
          onCancel={() => setDeleteTargetId(null)}
        />
      )}
    </div>
  )
}
