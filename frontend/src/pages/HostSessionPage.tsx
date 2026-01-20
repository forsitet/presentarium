import { useEffect, useState, useCallback, useMemo } from 'react'
import { useParams, useNavigate, useLocation } from 'react-router-dom'
import { QRCodeSVG } from 'qrcode.react'
import { useAuthStore } from '../stores/authStore'
import { useSessionStore } from '../stores/sessionStore'
import { socket } from '../ws/socket'
import { getRoomParticipants, changeRoomState, getQuestions } from '../api/polls'
import { ParticipantList } from '../components/ParticipantList'
import { AnswerBarChart } from '../components/AnswerBarChart'
import { Leaderboard } from '../components/Leaderboard'
import { WordCloudView } from '../components/WordCloudView'
import type { Participant, Question } from '../types'

interface WordCloudWord {
  text: string
  count: number
}

// --- WS payload types ---

interface QuestionStartPayload {
  question_id: string
  type: string
  text: string
  options?: Array<{ text: string; image_url?: string }>
  time_limit_seconds: number
  points: number
  position: number
  total: number
}

interface RevealedOption {
  text: string
  is_correct: boolean
  image_url?: string
}

interface LBEntry {
  rank: number
  id: string
  name: string
  score: number
}

// --- Option color palette (Kahoot-style) ---
const OPTION_BG = [
  'bg-red-600',
  'bg-blue-600',
  'bg-yellow-500',
  'bg-green-600',
  'bg-purple-600',
  'bg-cyan-600',
]
const OPTION_SHAPES = ['▲', '◆', '●', '■', '★', '⬟']

// --- Main component ---

export function HostSessionPage() {
  const { code } = useParams<{ code: string }>()
  const navigate = useNavigate()
  const location = useLocation()
  const { accessToken } = useAuthStore()
  const {
    participants,
    setParticipants,
    addParticipant,
    removeParticipant,
    setRoom,
    reset,
    roomStatus,
    setStatus,
  } = useSessionStore()

  // pollId is passed from DashboardPage as location state
  const pollId = (location.state as { pollId?: string } | null)?.pollId ?? null

  const [copied, setCopied] = useState(false)
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Quiz state
  const [questions, setQuestions] = useState<Question[]>([])
  const [activeQ, setActiveQ] = useState<QuestionStartPayload | null>(null)
  const [timer, setTimer] = useState(0)
  const [answered, setAnswered] = useState(0)
  const [totalPart, setTotalPart] = useState(0)
  const [distribution, setDistribution] = useState<Record<string, number>>({})
  const [revealedOptions, setRevealedOptions] = useState<RevealedOption[] | null>(null)
  const [lbEntries, setLbEntries] = useState<LBEntry[]>([])
  const [finalLb, setFinalLb] = useState<LBEntry[]>([])

  // Word cloud state
  const [wordcloudWords, setWordcloudWords] = useState<WordCloudWord[]>([])
  const [hiddenWords, setHiddenWords] = useState<Set<string>>(new Set())

  const joinUrl = `${window.location.origin}/join/${code}`

  // Fetch questions once pollId is known
  useEffect(() => {
    if (!pollId) return
    getQuestions(pollId).then(setQuestions).catch(() => {})
  }, [pollId])

  // Find next question to show after the current one
  const nextQuestion = useMemo<Question | null>(() => {
    if (!questions.length) return null
    if (!activeQ) return questions.slice().sort((a, b) => a.position - b.position)[0] ?? null
    const sorted = questions.slice().sort((a, b) => a.position - b.position)
    const idx = sorted.findIndex((q) => q.position > activeQ.position)
    return idx >= 0 ? sorted[idx] : null
  }, [questions, activeQ])

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(joinUrl).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [joinUrl])

  const handleStart = useCallback(async () => {
    if (!code || participants.length === 0) return
    setStarting(true)
    try {
      await changeRoomState(code, 'start')
      setStatus('active')
    } catch {
      setError('Не удалось начать опрос. Попробуйте снова.')
      setStarting(false)
    }
  }, [code, participants.length, setStatus])

  const handleShowQuestion = useCallback((questionId: string) => {
    socket.send('show_question', { question_id: questionId })
  }, [])

  const handleEndQuestion = useCallback(() => {
    socket.send('end_question', {})
  }, [])

  const handleEndSession = useCallback(async () => {
    if (!code) return
    try {
      await changeRoomState(code, 'end')
      // session_end WS will drive transition to 'finished'
    } catch {
      setError('Не удалось завершить сессию')
    }
  }, [code])

  // WebSocket setup — runs once when code/token are known
  useEffect(() => {
    if (!code) return

    setRoom(code)
    getRoomParticipants(code).then(setParticipants).catch(() => {})
    socket.connect(code, accessToken ?? undefined)

    const onParticipantJoined = (data: unknown) => addParticipant(data as Participant)
    const onParticipantLeft = (data: unknown) =>
      removeParticipant((data as { id: string }).id)

    const onQuestionStart = (data: unknown) => {
      const q = data as QuestionStartPayload
      setActiveQ(q)
      setTimer(q.time_limit_seconds)
      setAnswered(0)
      setTotalPart(0)
      setDistribution({})
      setRevealedOptions(null)
      setLbEntries([])
      setWordcloudWords([])
      setHiddenWords(new Set())
      setStatus('showing_question')
    }

    const onWordcloudUpdate = (data: unknown) => {
      const { words } = data as { words: WordCloudWord[] }
      setWordcloudWords(words ?? [])
    }

    const onTimerTick = (data: unknown) => {
      setTimer((data as { remaining: number }).remaining)
    }

    const onAnswerCount = (data: unknown) => {
      const { answered: a, total: t } = data as { answered: number; total: number }
      setAnswered(a)
      setTotalPart(t)
    }

    const onQuestionEnd = (data: unknown) => {
      const { options } = data as { question_id: string; options?: RevealedOption[] }
      if (options) setRevealedOptions(options)
    }

    const onResults = (data: unknown) => {
      const { answer_counts } = data as { answer_counts: Record<string, number> }
      setDistribution(answer_counts)
    }

    const onLeaderboard = (data: unknown) => {
      const { rankings } = data as { rankings: LBEntry[] }
      setLbEntries(rankings)
      setStatus('showing_results')
    }

    const onSessionEnd = (data: unknown) => {
      const { rankings } = data as { rankings: LBEntry[] }
      setFinalLb(rankings)
      setStatus('finished')
    }

    socket.on('participant_joined', onParticipantJoined)
    socket.on('participant_left', onParticipantLeft)
    socket.on('question_start', onQuestionStart)
    socket.on('timer_tick', onTimerTick)
    socket.on('answer_count', onAnswerCount)
    socket.on('question_end', onQuestionEnd)
    socket.on('results', onResults)
    socket.on('leaderboard', onLeaderboard)
    socket.on('session_end', onSessionEnd)
    socket.on('wordcloud_update', onWordcloudUpdate)

    return () => {
      socket.off('participant_joined', onParticipantJoined)
      socket.off('participant_left', onParticipantLeft)
      socket.off('question_start', onQuestionStart)
      socket.off('timer_tick', onTimerTick)
      socket.off('answer_count', onAnswerCount)
      socket.off('question_end', onQuestionEnd)
      socket.off('results', onResults)
      socket.off('leaderboard', onLeaderboard)
      socket.off('session_end', onSessionEnd)
      socket.off('wordcloud_update', onWordcloudUpdate)
      socket.disconnect()
      reset()
    }
  }, [code, accessToken, setRoom, setParticipants, addParticipant, removeParticipant, setStatus, reset])

  if (!code) {
    return (
      <div className="min-h-screen bg-gray-900 flex items-center justify-center text-white">
        <p>Код комнаты не найден</p>
      </div>
    )
  }

  // ════════════════════════════════════════════════════════════════
  // WAITING — show QR, code, participant list, Start button
  // ════════════════════════════════════════════════════════════════
  if (roomStatus === 'waiting') {
    return (
      <div className="min-h-screen bg-gray-900 text-white">
        <div className="bg-gray-800 border-b border-gray-700 px-6 py-4 flex items-center justify-between">
          <button
            onClick={() => navigate('/dashboard')}
            className="text-gray-400 hover:text-white transition-colors text-sm"
          >
            ← Дашборд
          </button>
          <span className="text-gray-400 text-sm">Экран ожидания</span>
        </div>

        <div className="max-w-5xl mx-auto px-6 py-10 grid grid-cols-1 lg:grid-cols-2 gap-10">
          {/* Left: room code + QR */}
          <div className="flex flex-col items-center">
            <p className="text-gray-400 text-sm uppercase tracking-widest mb-2">Код комнаты</p>
            <div className="text-8xl font-black tracking-widest text-white mb-6 font-mono">
              {code}
            </div>
            <div className="bg-white p-4 rounded-2xl shadow-lg mb-6">
              <QRCodeSVG value={joinUrl} size={200} />
            </div>
            <button
              onClick={handleCopy}
              className="flex items-center gap-2 bg-gray-700 hover:bg-gray-600 text-white px-5 py-3 rounded-xl transition-colors text-sm font-medium w-full max-w-xs justify-center"
            >
              {copied ? '✓ Скопировано!' : '🔗 Скопировать ссылку'}
            </button>
            <p className="text-gray-500 text-xs mt-3 text-center break-all max-w-xs">{joinUrl}</p>
          </div>

          {/* Right: participant list + start button */}
          <div className="flex flex-col">
            <div className="bg-gray-800 rounded-2xl p-6 flex-1 border border-gray-700">
              <ParticipantList participants={participants} />
            </div>
            {error && (
              <div className="mt-4 bg-red-900/40 border border-red-700 text-red-300 px-4 py-3 rounded-xl text-sm">
                {error}
              </div>
            )}
            <button
              onClick={handleStart}
              disabled={participants.length === 0 || starting}
              className={`mt-6 w-full py-4 rounded-2xl text-lg font-bold transition-all ${
                participants.length === 0 || starting
                  ? 'bg-gray-700 text-gray-500 cursor-not-allowed'
                  : 'bg-indigo-600 hover:bg-indigo-500 text-white shadow-lg hover:shadow-indigo-500/30 active:scale-[0.98]'
              }`}
            >
              {starting
                ? 'Запускаем...'
                : participants.length === 0
                  ? 'Ждём участников...'
                  : `Начать опрос (${participants.length})`}
            </button>
          </div>
        </div>
      </div>
    )
  }

  // ════════════════════════════════════════════════════════════════
  // ACTIVE — session started, ready to show first question
  // ════════════════════════════════════════════════════════════════
  if (roomStatus === 'active') {
    const firstQ = questions.slice().sort((a, b) => a.position - b.position)[0] ?? null
    return (
      <div className="min-h-screen bg-gray-900 text-white flex flex-col">
        <div className="bg-gray-800 border-b border-gray-700 px-6 py-4 flex items-center justify-between">
          <span className="text-gray-400 text-sm">
            Код: <span className="font-mono text-white font-bold">{code}</span>
          </span>
          <span className="text-green-400 font-medium text-sm">
            ● Активна · {participants.length} участников
          </span>
        </div>

        <div className="flex-1 flex items-center justify-center px-6">
          <div className="max-w-lg w-full text-center">
            <div className="text-5xl mb-6">✅</div>
            <h2 className="text-3xl font-bold mb-2">Опрос запущен!</h2>
            <p className="text-gray-400 mb-8">
              Все участники подключены. Нажмите «Показать вопрос», чтобы начать.
            </p>

            {firstQ ? (
              <div className="bg-gray-800 rounded-2xl border border-gray-700 p-6 mb-6 text-left">
                <p className="text-gray-400 text-xs uppercase tracking-wide mb-2">
                  Вопрос 1 из {questions.length}
                </p>
                <p className="text-white text-lg font-medium leading-snug">{firstQ.text}</p>
              </div>
            ) : (
              <p className="text-gray-500 mb-6">Загрузка вопросов...</p>
            )}

            <button
              onClick={() => firstQ && handleShowQuestion(firstQ.id)}
              disabled={!firstQ}
              className={`w-full py-4 rounded-2xl text-lg font-bold transition-all ${
                firstQ
                  ? 'bg-indigo-600 hover:bg-indigo-500 text-white shadow-lg active:scale-[0.98]'
                  : 'bg-gray-700 text-gray-500 cursor-not-allowed'
              }`}
            >
              Показать первый вопрос
            </button>

            <button
              onClick={handleEndSession}
              className="mt-3 w-full py-3 rounded-xl text-sm text-gray-400 hover:text-red-400 transition-colors"
            >
              Завершить сессию досрочно
            </button>
          </div>
        </div>
      </div>
    )
  }

  // ════════════════════════════════════════════════════════════════
  // SHOWING QUESTION — live question with timer and answer tracking
  // ════════════════════════════════════════════════════════════════
  if (roomStatus === 'showing_question' && activeQ) {
    const timerPct =
      activeQ.time_limit_seconds > 0 ? (timer / activeQ.time_limit_seconds) * 100 : 0
    const timerColor =
      timerPct > 60 ? 'bg-green-500' : timerPct > 25 ? 'bg-yellow-500' : 'bg-red-500'
    const hasOptions = (activeQ.options?.length ?? 0) > 0

    return (
      <div className="min-h-screen bg-gray-900 text-white flex flex-col">
        {/* Top bar: progress, timer, answer count, end-early button */}
        <div className="bg-gray-800 border-b border-gray-700 px-6 py-3 flex items-center gap-6">
          <span className="text-gray-400 text-sm font-medium whitespace-nowrap">
            Вопрос {activeQ.position} / {activeQ.total}
          </span>

          {/* Timer bar */}
          <div className="flex items-center gap-3 flex-1 max-w-md">
            <div className="flex-1 bg-gray-700 rounded-full h-4 overflow-hidden">
              <div
                className={`h-full rounded-full transition-all duration-1000 ${timerColor}`}
                style={{ width: `${timerPct}%` }}
              />
            </div>
            <span className="text-white font-mono text-2xl font-bold w-10 text-right">
              {timer}
            </span>
          </div>

          <span className="text-gray-300 text-sm whitespace-nowrap">
            <span className="text-white font-bold text-lg">{answered}</span>
            <span className="text-gray-500"> / {totalPart || participants.length} ответили</span>
          </span>

          <button
            onClick={handleEndQuestion}
            className="bg-orange-600 hover:bg-orange-500 text-white text-sm px-4 py-2 rounded-lg transition-colors font-medium whitespace-nowrap"
          >
            Завершить раньше
          </button>
        </div>

        {/* Main content: question + live chart */}
        <div className="flex-1 max-w-7xl mx-auto w-full px-6 py-8 grid grid-cols-1 lg:grid-cols-2 gap-8">
          {/* Left: question text + option tiles */}
          <div className="flex flex-col gap-5">
            <div className="bg-gray-800 rounded-2xl border border-gray-700 p-8 flex items-center justify-center flex-1 min-h-[180px]">
              <p className="text-2xl lg:text-3xl font-bold text-white text-center leading-snug">
                {activeQ.text}
              </p>
            </div>

            {hasOptions && (
              <div className="grid grid-cols-2 gap-3">
                {activeQ.options!.map((opt, i) => (
                  <div
                    key={i}
                    className={`${OPTION_BG[i % OPTION_BG.length]} rounded-xl p-4 flex items-center gap-3`}
                  >
                    <span className="text-white text-xl font-bold">{OPTION_SHAPES[i]}</span>
                    <span className="text-white font-semibold text-sm leading-snug flex-1 line-clamp-2">
                      {opt.text}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* Right: live bar chart + progress */}
          <div className="flex flex-col gap-5">
            <div className="bg-gray-800 rounded-2xl border border-gray-700 p-5 flex-1">
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-white font-bold">Распределение ответов</h3>
                <span className="text-sm text-gray-400">
                  {answered} / {totalPart || participants.length}
                </span>
              </div>
              {hasOptions ? (
                <AnswerBarChart
                  options={activeQ.options!}
                  distribution={distribution}
                  showCorrect={false}
                />
              ) : activeQ.type === 'word_cloud' ? (
                <WordCloudView
                  words={wordcloudWords}
                  hiddenWords={hiddenWords}
                  onHideWord={(word) =>
                    setHiddenWords((prev) => {
                      const next = new Set(prev)
                      if (next.has(word)) next.delete(word)
                      else next.add(word)
                      return next
                    })
                  }
                  showModerationPanel
                />
              ) : (
                <p className="text-gray-500 text-sm text-center py-12">
                  {answered} текстовых ответов
                </p>
              )}
            </div>

            {/* Answered progress bar */}
            <div className="bg-gray-800 rounded-2xl border border-gray-700 p-5">
              <div className="flex items-center justify-between mb-2">
                <span className="text-gray-400 text-sm">Ответили</span>
                <span className="text-white font-bold">
                  {answered} / {totalPart || participants.length}
                </span>
              </div>
              <div className="h-3 bg-gray-700 rounded-full overflow-hidden">
                <div
                  className="h-full bg-indigo-500 rounded-full transition-all duration-300"
                  style={{
                    width: `${
                      (totalPart || participants.length) > 0
                        ? (answered / (totalPart || participants.length)) * 100
                        : 0
                    }%`,
                  }}
                />
              </div>
            </div>
          </div>
        </div>
      </div>
    )
  }

  // ════════════════════════════════════════════════════════════════
  // SHOWING RESULTS — chart with correct answers + leaderboard
  // ════════════════════════════════════════════════════════════════
  if (roomStatus === 'showing_results' && activeQ) {
    // Use revealed options (with is_correct) if available; fall back to question options
    const displayOptions: Array<{ text: string; is_correct?: boolean; image_url?: string }> =
      revealedOptions && revealedOptions.length > 0
        ? revealedOptions
        : (activeQ.options ?? [])
    const hasOptions = displayOptions.length > 0

    return (
      <div className="min-h-screen bg-gray-900 text-white flex flex-col">
        {/* Top bar with navigation buttons */}
        <div className="bg-gray-800 border-b border-gray-700 px-6 py-3 flex items-center justify-between gap-4">
          <span className="text-gray-400 text-sm">
            Результаты: вопрос {activeQ.position} из {activeQ.total}
          </span>
          <div className="flex gap-3">
            {nextQuestion && (
              <button
                onClick={() => handleShowQuestion(nextQuestion.id)}
                className="bg-indigo-600 hover:bg-indigo-500 text-white text-sm px-5 py-2 rounded-lg transition-colors font-medium"
              >
                Следующий вопрос →
              </button>
            )}
            <button
              onClick={handleEndSession}
              className="bg-red-700 hover:bg-red-600 text-white text-sm px-5 py-2 rounded-lg transition-colors font-medium"
            >
              Завершить опрос
            </button>
          </div>
        </div>

        <div className="flex-1 max-w-7xl mx-auto w-full px-6 py-8 grid grid-cols-1 lg:grid-cols-2 gap-8">
          {/* Left: question text + chart */}
          <div className="flex flex-col gap-5">
            <div className="bg-gray-800 rounded-2xl border border-gray-700 p-6">
              <p className="text-xl font-bold text-white mb-5 leading-snug">{activeQ.text}</p>

              {hasOptions ? (
                <>
                  <AnswerBarChart
                    options={displayOptions}
                    distribution={distribution}
                    showCorrect={!!revealedOptions}
                  />
                  {/* Option list with correct/incorrect markers */}
                  <div className="mt-4 grid grid-cols-2 gap-2">
                    {displayOptions.map((opt, i) => (
                      <div
                        key={i}
                        className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm ${
                          revealedOptions
                            ? opt.is_correct
                              ? 'bg-green-900/40 border border-green-700 text-green-300'
                              : 'bg-gray-700/40 text-gray-400'
                            : 'bg-gray-700/50 text-gray-300'
                        }`}
                      >
                        {revealedOptions && (
                          <span className="font-bold">{opt.is_correct ? '✓' : '✗'}</span>
                        )}
                        <span className="truncate flex-1">{opt.text}</span>
                        <span className="ml-auto font-bold text-white tabular-nums">
                          {distribution[String(i)] ?? 0}
                        </span>
                      </div>
                    ))}
                  </div>
                </>
              ) : activeQ.type === 'word_cloud' ? (
                <WordCloudView
                  words={wordcloudWords}
                  hiddenWords={hiddenWords}
                  onHideWord={(word) =>
                    setHiddenWords((prev) => {
                      const next = new Set(prev)
                      if (next.has(word)) next.delete(word)
                      else next.add(word)
                      return next
                    })
                  }
                  showModerationPanel
                />
              ) : (
                <p className="text-gray-500 text-sm text-center py-8">
                  Текстовых ответов:{' '}
                  {Object.values(distribution).reduce((a, b) => a + b, 0)}
                </p>
              )}
            </div>
          </div>

          {/* Right: leaderboard + next question preview */}
          <div className="flex flex-col gap-5">
            <Leaderboard entries={lbEntries} title="Топ-5 после вопроса" />

            {nextQuestion && (
              <div className="bg-gray-800 rounded-2xl border border-gray-700 p-5">
                <p className="text-gray-400 text-xs uppercase tracking-wide mb-2">
                  Следующий · {nextQuestion.position} из {questions.length}
                </p>
                <p className="text-white font-medium leading-snug">{nextQuestion.text}</p>
                <button
                  onClick={() => handleShowQuestion(nextQuestion.id)}
                  className="mt-4 w-full bg-indigo-600 hover:bg-indigo-500 text-white py-3 rounded-xl text-sm font-bold transition-all active:scale-[0.98]"
                >
                  Показать следующий вопрос
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
    )
  }

  // ════════════════════════════════════════════════════════════════
  // FINISHED — final leaderboard
  // ════════════════════════════════════════════════════════════════
  if (roomStatus === 'finished') {
    return (
      <div className="min-h-screen bg-gray-900 text-white flex flex-col items-center justify-center px-6 py-12">
        <div className="max-w-lg w-full text-center">
          <div className="text-6xl mb-6">🏆</div>
          <h2 className="text-3xl font-bold mb-2">Опрос завершён!</h2>
          <p className="text-gray-400 mb-8">Итоговые результаты</p>

          <div className="mb-8">
            <Leaderboard entries={finalLb} title="Финальный лидерборд" />
          </div>

          <button
            onClick={() => navigate('/dashboard')}
            className="bg-indigo-600 hover:bg-indigo-500 text-white px-8 py-3 rounded-2xl text-base font-bold transition-all active:scale-[0.98]"
          >
            Вернуться на дашборд
          </button>
        </div>
      </div>
    )
  }

  // Loading fallback
  return (
    <div className="min-h-screen bg-gray-900 flex items-center justify-center text-white">
      <p className="text-gray-500">Загрузка...</p>
    </div>
  )
}
