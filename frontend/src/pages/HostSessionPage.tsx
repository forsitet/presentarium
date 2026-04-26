import { useEffect, useState, useCallback, useMemo } from 'react'
import { useParams, useNavigate, useLocation } from 'react-router-dom'
import { QRCodeSVG } from 'qrcode.react'
import { useAuthStore } from '../stores/authStore'
import { useSessionStore } from '../stores/sessionStore'
import { socket } from '../ws/socket'
import { getRoomParticipants, changeRoomState, getQuestions, getRoomInfo } from '../api/polls'
import { ParticipantList } from '../components/ParticipantList'
import { AnswerBarChart } from '../components/AnswerBarChart'
import { Leaderboard } from '../components/Leaderboard'
import { WordCloudView } from '../components/WordCloudView'
import { BrainstormBoard } from '../components/BrainstormBoard'
import { PresentationPicker } from '../components/PresentationPicker'
import { SlideViewer } from '../components/SlideViewer'
import type { Participant, Question, Presentation, WSPresentationOpened, WSSlideChanged } from '../types'

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

  // pollId may come from location state (normal flow) or fetched from API (reconnect)
  const [pollId, setPollId] = useState<string | null>(
    (location.state as { pollId?: string } | null)?.pollId ?? null,
  )

  const [roomReady, setRoomReady] = useState(false) // true once getRoomInfo completes (Hub room exists)
  const [copied, setCopied] = useState(false)
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showAnswerDistribution, setShowAnswerDistribution] = useState(false)

  // Quiz state
  const [questions, setQuestions] = useState<Question[]>([])
  const [activeQ, setActiveQ] = useState<QuestionStartPayload | null>(null)
  const [timer, setTimer] = useState(0)
  const [answered, setAnswered] = useState(0)
  const [totalPart, setTotalPart] = useState(0)
  const [avgResponseMs, setAvgResponseMs] = useState(0)
  const [answeredParticipantIds, setAnsweredParticipantIds] = useState<Set<string>>(new Set())
  const [disconnectedDuringQuestion, setDisconnectedDuringQuestion] = useState<Map<string, string>>(new Map())
  const [distribution, setDistribution] = useState<Record<string, number>>({})
  const [revealedOptions, setRevealedOptions] = useState<RevealedOption[] | null>(null)
  const [lbEntries, setLbEntries] = useState<LBEntry[]>([])
  const [finalLb, setFinalLb] = useState<LBEntry[]>([])

  // Session question order (received via room_started WS; empty = sequential by position)
  const [questionOrder, setQuestionOrder] = useState<string[]>([])

  // Word cloud state
  const [wordcloudWords, setWordcloudWords] = useState<WordCloudWord[]>([])
  const [hiddenWords, setHiddenWords] = useState<Set<string>>(new Set())

  // Presentation state
  const [activePresentation, setActivePresentation] = useState<WSPresentationOpened | null>(null)
  const [showPresentationPicker, setShowPresentationPicker] = useState(false)

  // Screen transition key — changes on each state/question transition to re-trigger CSS fade-in
  const screenKey = `${roomStatus}-${activeQ?.question_id ?? ''}`

  const joinUrl = `${window.location.origin}/join/${code}`

  // On mount: fetch room info to get pollId (if missing) and sync roomStatus.
  // This also ensures the Hub room exists (backend re-creates it if lost after restart).
  // Must complete BEFORE the WS connection is established.
  useEffect(() => {
    if (!code) return
    getRoomInfo(code)
      .then((info) => {
        if (!pollId && info.poll_id) setPollId(info.poll_id)
        setShowAnswerDistribution(!!info.show_answer_distribution)
        // Sync status so we don't show "waiting" when the room is already active
        const validStatuses = ['waiting', 'active', 'showing_question', 'showing_results', 'finished'] as const
        type RS = typeof validStatuses[number]
        if (validStatuses.includes(info.status as RS)) {
          // If the room is mid-question but we have no activeQ data, show as 'active'
          // so the host can use "Показать вопрос" / "Завершить" buttons
          const mapped = info.status === 'showing_question' ? 'active' : info.status as RS
          if (roomStatus === 'waiting' && mapped !== 'waiting') {
            setStatus(mapped)
          }
        }
      })
      .catch(() => {})
      .finally(() => setRoomReady(true))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [code])

  // Fetch questions once pollId is known
  useEffect(() => {
    if (!pollId) return
    getQuestions(pollId).then(setQuestions).catch(() => {})
  }, [pollId])

  // Find next question to show after the current one.
  // Uses server-provided questionOrder when available (random or sequential shuffled order),
  // falls back to position-based sort otherwise.
  const nextQuestion = useMemo<Question | null>(() => {
    if (!questions.length) return null
    if (questionOrder.length > 0) {
      if (!activeQ) return questions.find((q) => q.id === questionOrder[0]) ?? null
      const currentIdx = questionOrder.indexOf(activeQ.question_id)
      if (currentIdx < 0 || currentIdx >= questionOrder.length - 1) return null
      return questions.find((q) => q.id === questionOrder[currentIdx + 1]) ?? null
    }
    // Fallback: sequential by position
    if (!activeQ) return questions.slice().sort((a, b) => a.position - b.position)[0] ?? null
    const sorted = questions.slice().sort((a, b) => a.position - b.position)
    const idx = sorted.findIndex((q) => q.position > activeQ.position)
    return idx >= 0 ? sorted[idx] : null
  }, [questions, activeQ, questionOrder])

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

  // ── Presentation controls ──────────────────────────────────────────────────
  const handlePickPresentation = useCallback((p: Presentation) => {
    setShowPresentationPicker(false)
    // Server will broadcast presentation_opened with resolved slide URLs —
    // we just optimistically request; setActivePresentation happens in WS handler.
    socket.send('open_presentation', { presentation_id: p.id, slide_position: 1 })
  }, [])

  const handleSlidePrev = useCallback(() => {
    if (!activePresentation) return
    const next = Math.max(1, activePresentation.current_slide_position - 1)
    if (next === activePresentation.current_slide_position) return
    socket.send('change_slide', { slide_position: next })
  }, [activePresentation])

  const handleSlideNext = useCallback(() => {
    if (!activePresentation) return
    const next = Math.min(
      activePresentation.slide_count,
      activePresentation.current_slide_position + 1,
    )
    if (next === activePresentation.current_slide_position) return
    socket.send('change_slide', { slide_position: next })
  }, [activePresentation])

  const handleSlideJump = useCallback((position: number) => {
    if (!activePresentation) return
    if (position < 1 || position > activePresentation.slide_count) return
    if (position === activePresentation.current_slide_position) return
    socket.send('change_slide', { slide_position: position })
  }, [activePresentation])

  const handleClosePresentation = useCallback(() => {
    socket.send('close_presentation', {})
  }, [])

  // WebSocket setup — waits for roomReady (getRoomInfo complete) and accessToken.
  // getRoomInfo must finish first so the backend Hub room is guaranteed to exist.
  useEffect(() => {
    if (!code || !accessToken || !roomReady) return

    setRoom(code)
    getRoomParticipants(code).then(setParticipants).catch(() => {})
    // Use the freshest available token: the interceptor may have refreshed it
    // (updating localStorage) before the Zustand store re-renders.
    const wsToken = localStorage.getItem('access_token') || accessToken
    socket.connect(code, wsToken)

    const onParticipantJoined = (data: unknown) => addParticipant(data as Participant)
    const onParticipantLeft = (data: unknown) => {
      const { id, name } = data as { id: string; name?: string }
      removeParticipant(id)
      // Track disconnection so we can show ✗ during an active question
      setDisconnectedDuringQuestion((prev) => {
        const next = new Map(prev)
        next.set(id, name ?? id)
        return next
      })
    }

    const onQuestionStart = (data: unknown) => {
      const q = data as QuestionStartPayload
      setActiveQ(q)
      setTimer(q.time_limit_seconds)
      setAnswered(0)
      setTotalPart(0)
      setAvgResponseMs(0)
      setAnsweredParticipantIds(new Set())
      setDisconnectedDuringQuestion(new Map())
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
      const { answered: a, total: t, participant_id, avg_response_ms } = data as {
        answered: number
        total: number
        participant_id?: string
        avg_response_ms?: number
      }
      setAnswered(a)
      setTotalPart(t)
      if (avg_response_ms !== undefined) setAvgResponseMs(avg_response_ms)
      if (participant_id) {
        setAnsweredParticipantIds((prev) => {
          const next = new Set(prev)
          next.add(participant_id)
          return next
        })
      }
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

    const onRoomStarted = (data: unknown) => {
      const { question_order } = data as { question_order?: string[] }
      if (Array.isArray(question_order)) setQuestionOrder(question_order)
    }

    const onSessionEnd = (data: unknown) => {
      const { rankings } = data as { rankings: LBEntry[] }
      setFinalLb(rankings)
      setStatus('finished')
    }

    const onPresentationOpened = (data: unknown) => {
      setActivePresentation(data as WSPresentationOpened)
    }

    const onSlideChanged = (data: unknown) => {
      const { slide_position } = data as WSSlideChanged
      setActivePresentation((prev) =>
        prev ? { ...prev, current_slide_position: slide_position } : prev,
      )
    }

    const onPresentationClosed = () => {
      setActivePresentation(null)
    }

    socket.on('participant_joined', onParticipantJoined)
    socket.on('participant_left', onParticipantLeft)
    socket.on('room_started', onRoomStarted)
    socket.on('question_start', onQuestionStart)
    socket.on('timer_tick', onTimerTick)
    socket.on('answer_count', onAnswerCount)
    socket.on('question_end', onQuestionEnd)
    socket.on('results', onResults)
    socket.on('leaderboard', onLeaderboard)
    socket.on('session_end', onSessionEnd)
    socket.on('wordcloud_update', onWordcloudUpdate)
    socket.on('presentation_opened', onPresentationOpened)
    socket.on('slide_changed', onSlideChanged)
    socket.on('presentation_closed', onPresentationClosed)

    return () => {
      socket.off('participant_joined', onParticipantJoined)
      socket.off('participant_left', onParticipantLeft)
      socket.off('room_started', onRoomStarted)
      socket.off('question_start', onQuestionStart)
      socket.off('timer_tick', onTimerTick)
      socket.off('answer_count', onAnswerCount)
      socket.off('question_end', onQuestionEnd)
      socket.off('results', onResults)
      socket.off('leaderboard', onLeaderboard)
      socket.off('session_end', onSessionEnd)
      socket.off('wordcloud_update', onWordcloudUpdate)
      socket.off('presentation_opened', onPresentationOpened)
      socket.off('slide_changed', onSlideChanged)
      socket.off('presentation_closed', onPresentationClosed)
      socket.disconnect()
      reset()
    }
  }, [code, accessToken, roomReady, setRoom, setParticipants, addParticipant, removeParticipant, setStatus, reset])

  if (!code) {
    return (
      <div className="min-h-screen bg-gray-900 flex items-center justify-center text-white">
        <p>Код комнаты не найден</p>
      </div>
    )
  }

  // Shared presentation overlay/picker + floating button. Included as a child
  // of each branch's top-level wrapper — the fixed-positioned elements will
  // cover the screen regardless of parent, and the button keeps the control
  // accessible at every stage of the session.
  const presentationLayer = (
    <>
      {/* Floating toggle button — bottom-right, visible on all screens */}
      {roomStatus !== 'finished' && (
        <div className="fixed bottom-6 right-6 z-30 flex flex-col gap-2 items-end">
          {activePresentation ? (
            <button
              onClick={handleClosePresentation}
              className="bg-red-600 hover:bg-red-500 text-white px-4 py-2.5 rounded-full text-sm font-medium shadow-lg transition-colors flex items-center gap-2"
              title="Закрыть презентацию для всех"
            >
              <span>✕</span>
              <span className="hidden sm:inline">Закрыть презентацию</span>
            </button>
          ) : (
            <button
              onClick={() => setShowPresentationPicker(true)}
              className="bg-indigo-600 hover:bg-indigo-500 text-white px-4 py-2.5 rounded-full text-sm font-medium shadow-lg transition-colors flex items-center gap-2"
              title="Показать презентацию"
            >
              <span>📽</span>
              <span className="hidden sm:inline">Показать презентацию</span>
            </button>
          )}
        </div>
      )}

      {/* Picker modal */}
      {showPresentationPicker && (
        <PresentationPicker
          onPick={handlePickPresentation}
          onClose={() => setShowPresentationPicker(false)}
        />
      )}

      {/* Full-screen slide viewer — host mode with controls */}
      {activePresentation && (
        <SlideViewer
          title={activePresentation.title}
          slides={activePresentation.slides}
          currentPosition={activePresentation.current_slide_position}
          onPrev={handleSlidePrev}
          onNext={handleSlideNext}
          onJump={handleSlideJump}
          onClose={handleClosePresentation}
        />
      )}
    </>
  )

  // ════════════════════════════════════════════════════════════════
  // WAITING — show QR, code, participant list, Start button
  // ════════════════════════════════════════════════════════════════
  if (roomStatus === 'waiting') {
    return (
      <>
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

        <div className="max-w-5xl mx-auto px-4 sm:px-6 py-6 sm:py-10 grid grid-cols-1 lg:grid-cols-2 gap-6 sm:gap-10">
          {/* Left: room code + QR */}
          <div className="flex flex-col items-center">
            <p className="text-gray-400 text-sm uppercase tracking-widest mb-2">Код комнаты</p>
            <div className="text-5xl sm:text-7xl lg:text-8xl font-black tracking-widest text-white mb-4 sm:mb-6 font-mono">
              {code}
            </div>
            <div className="bg-white p-3 sm:p-4 rounded-2xl shadow-lg mb-4 sm:mb-6">
              <div className="sm:hidden"><QRCodeSVG value={joinUrl} size={160} /></div>
              <div className="hidden sm:block"><QRCodeSVG value={joinUrl} size={200} /></div>
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
      {presentationLayer}
      </>
    )
  }

  // ════════════════════════════════════════════════════════════════
  // ACTIVE — session started, ready to show first question
  // ════════════════════════════════════════════════════════════════
  if (roomStatus === 'active') {
    const firstQ = questionOrder.length > 0
      ? (questions.find((q) => q.id === questionOrder[0]) ?? null)
      : (questions.slice().sort((a, b) => a.position - b.position)[0] ?? null)
    return (
      <>
      <div className="min-h-screen bg-gray-900 text-white flex flex-col">
        <div className="bg-gray-800 border-b border-gray-700 px-6 py-4 flex items-center justify-between">
          <span className="text-gray-400 text-sm">
            Код: <span className="font-mono text-white font-bold">{code}</span>
          </span>
          <span className="text-green-400 font-medium text-sm">
            ● Активна · {participants.length} участников
          </span>
        </div>

        <div key={screenKey} className="flex-1 flex items-center justify-center px-4 sm:px-6 animate-fade-in-up">
          <div className="max-w-lg w-full text-center">
            <div className="text-4xl sm:text-5xl mb-4 sm:mb-6">✅</div>
            <h2 className="text-2xl sm:text-3xl font-bold mb-2">Опрос запущен!</h2>
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
      {presentationLayer}
      </>
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
      <>
      <div className="min-h-screen bg-gray-900 text-white flex flex-col">
        {/* Top bar: progress, timer, answer count, end-early button */}
        <div className="bg-gray-800 border-b border-gray-700 px-3 sm:px-6 py-2 sm:py-3 flex flex-wrap items-center gap-2 sm:gap-4">
          <span className="text-gray-400 text-xs sm:text-sm font-medium whitespace-nowrap">
            Вопрос {activeQ.position} / {activeQ.total}
          </span>

          {/* Timer bar */}
          <div className="flex items-center gap-2 sm:gap-3 flex-1 min-w-[120px] max-w-md">
            <div className="flex-1 bg-gray-700 rounded-full h-3 sm:h-4 overflow-hidden">
              <div
                className={`h-full rounded-full transition-all duration-1000 ${timerColor}`}
                style={{ width: `${timerPct}%` }}
              />
            </div>
            <span className={`font-mono text-xl sm:text-2xl font-bold w-8 sm:w-10 text-right ${
              timer <= 5 ? 'text-red-400 animate-timer-urgent' : 'text-white'
            }`}>
              {timer}
            </span>
          </div>

          <span className="text-gray-300 text-xs sm:text-sm whitespace-nowrap">
            <span className="text-white font-bold text-base sm:text-lg">{answered}</span>
            <span className="text-gray-500"> / {totalPart || participants.length}</span>
          </span>

          <button
            onClick={handleEndQuestion}
            className="bg-orange-600 hover:bg-orange-500 text-white text-xs sm:text-sm px-3 sm:px-4 py-1.5 sm:py-2 rounded-lg transition-colors font-medium whitespace-nowrap"
          >
            Завершить
          </button>
        </div>

        {/* Main content — word_cloud gets a full-screen layout; others use the split grid */}
        {activeQ.type === 'word_cloud' ? (
          <div key={screenKey} className="flex-1 w-full px-3 sm:px-6 py-4 sm:py-6 flex flex-col animate-fade-in-up">
            {/* Question text */}
            <div className="text-center mb-4">
              <p className="text-xl sm:text-2xl lg:text-3xl font-bold text-white leading-snug animate-question-in">
                {activeQ.text || 'Предложите ваши слова'}
              </p>
              <p className="text-gray-400 text-sm mt-2">
                {answered} ответов от {totalPart || participants.length} участников
              </p>
            </div>

            {/* Full-screen word cloud */}
            <div className="flex-1 min-h-0">
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
                fullScreen
              />
            </div>
          </div>
        ) : (
          <div key={screenKey} className="flex-1 max-w-7xl mx-auto w-full px-3 sm:px-6 py-4 sm:py-8 grid grid-cols-1 lg:grid-cols-2 gap-5 sm:gap-8 animate-fade-in-up">
            {/* Left: question text + option tiles */}
            <div className="flex flex-col gap-4 sm:gap-5">
              <div className="bg-gray-800 rounded-2xl border border-gray-700 p-4 sm:p-8 flex items-center justify-center flex-1 min-h-[120px] sm:min-h-[180px]">
                <p className="text-xl sm:text-2xl lg:text-3xl font-bold text-white text-center leading-snug animate-question-in">
                  {activeQ.text || 'Вопрос...'}
                </p>
              </div>

              {hasOptions && (
                <div className="grid grid-cols-2 gap-3">
                  {activeQ.options!.map((opt, i) => (
                    <div
                      key={i}
                      className={`${OPTION_BG[i % OPTION_BG.length]} rounded-xl p-4 flex items-center gap-3 animate-slide-stagger`}
                      style={{ animationDelay: `${i * 60}ms` }}
                    >
                      <span className="text-white text-xl font-bold">{OPTION_SHAPES[i]}</span>
                      <span className="text-white font-semibold text-sm leading-snug flex-1 line-clamp-2">
                        {opt.text || `Вариант ${i + 1}`}
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
                  showAnswerDistribution ? (
                    <AnswerBarChart
                      options={activeQ.options!}
                      distribution={distribution}
                      showCorrect={false}
                    />
                  ) : (
                    <p className="text-gray-500 text-sm text-center py-12">
                      Распределение скрыто. Показывается после завершения вопроса.
                    </p>
                  )
                ) : activeQ.type === 'brainstorm' ? (
                  <BrainstormBoard
                    questionId={activeQ.question_id}
                    answeredCount={answered}
                    totalParticipants={totalPart || participants.length}
                  />
                ) : (
                  <p className="text-gray-500 text-sm text-center py-12">
                    {answered} текстовых ответов
                  </p>
                )}
              </div>

              {/* Monitoring panel — hide for brainstorm (BrainstormBoard has its own stats) */}
              {activeQ.type !== 'brainstorm' && (
                <div className="bg-gray-800 rounded-2xl border border-gray-700 p-5 space-y-4">
                  {/* Progress bar */}
                  <div>
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-gray-400 text-sm">Ответили</span>
                      <span className="text-white font-bold">
                        {answered} / {totalPart || participants.length}
                        {(totalPart || participants.length) > 0 && (
                          <span className="text-gray-400 font-normal text-sm ml-1">
                            ({Math.round((answered / (totalPart || participants.length)) * 100)}%)
                          </span>
                        )}
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

                  {/* Avg response time */}
                  {avgResponseMs > 0 && (
                    <div className="flex items-center justify-between text-sm">
                      <span className="text-gray-400">Среднее время ответа</span>
                      <span className="text-white font-medium tabular-nums">
                        {(avgResponseMs / 1000).toFixed(1)} сек
                      </span>
                    </div>
                  )}

                  {/* Per-participant status list */}
                  {(participants.length > 0 || disconnectedDuringQuestion.size > 0) && (
                    <div>
                      <p className="text-gray-400 text-xs uppercase tracking-wide mb-2">
                        Участники
                      </p>
                      <div className="space-y-1 max-h-36 overflow-y-auto">
                        {participants.map((p) => {
                          const hasAnswered = answeredParticipantIds.has(p.id)
                          return (
                            <div key={p.id} className="flex items-center gap-2 text-sm">
                              <span
                                className={`text-base leading-none ${
                                  hasAnswered ? 'text-green-400' : 'text-gray-500'
                                }`}
                                title={hasAnswered ? 'Ответил' : 'Ожидает'}
                              >
                                {hasAnswered ? '✓' : '◷'}
                              </span>
                              <span
                                className={`truncate flex-1 ${
                                  hasAnswered ? 'text-gray-300' : 'text-gray-500'
                                }`}
                              >
                                {p.name}
                              </span>
                            </div>
                          )
                        })}
                        {Array.from(disconnectedDuringQuestion.entries()).map(([id, name]) => (
                          <div key={id} className="flex items-center gap-2 text-sm">
                            <span className="text-red-500 text-base leading-none" title="Отключился">
                              ✗
                            </span>
                            <span className="truncate flex-1 text-red-400 line-through">{name}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
        )}
      </div>
      {presentationLayer}
      </>
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
      <>
      <div className="min-h-screen bg-gray-900 text-white flex flex-col">
        {/* Top bar with navigation buttons */}
        <div className="bg-gray-800 border-b border-gray-700 px-3 sm:px-6 py-2 sm:py-3 flex flex-wrap items-center justify-between gap-2 sm:gap-4">
          <span className="text-gray-400 text-xs sm:text-sm">
            Результаты: вопрос {activeQ.position} из {activeQ.total}
          </span>
          <div className="flex gap-2 sm:gap-3">
            {nextQuestion && (
              <button
                onClick={() => handleShowQuestion(nextQuestion.id)}
                className="bg-indigo-600 hover:bg-indigo-500 text-white text-xs sm:text-sm px-3 sm:px-5 py-1.5 sm:py-2 rounded-lg transition-colors font-medium"
              >
                Следующий →
              </button>
            )}
            <button
              onClick={handleEndSession}
              className="bg-red-700 hover:bg-red-600 text-white text-xs sm:text-sm px-3 sm:px-5 py-1.5 sm:py-2 rounded-lg transition-colors font-medium"
            >
              Завершить
            </button>
          </div>
        </div>

        {/* Word cloud results get a full-screen layout; others use the split grid */}
        {activeQ.type === 'word_cloud' ? (
          <div key={screenKey} className="flex-1 w-full px-3 sm:px-6 py-4 sm:py-6 flex flex-col animate-fade-in-up">
            {/* Question text */}
            <div className="text-center mb-4">
              <p className="text-xl sm:text-2xl font-bold text-white leading-snug">
                {activeQ.text || 'Облако слов'}
              </p>
            </div>

            {/* Full-screen word cloud */}
            <div className="flex-1 min-h-0">
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
                fullScreen
              />
            </div>

            {/* Bottom bar: next question */}
            {nextQuestion && (
              <div className="flex-shrink-0 mt-4 flex items-center justify-center gap-4">
                <button
                  onClick={() => handleShowQuestion(nextQuestion.id)}
                  className="bg-indigo-600 hover:bg-indigo-500 text-white px-6 py-3 rounded-xl text-sm font-bold transition-all active:scale-[0.98]"
                >
                  Следующий вопрос →
                </button>
              </div>
            )}
          </div>
        ) : (
          <div key={screenKey} className="flex-1 max-w-7xl mx-auto w-full px-3 sm:px-6 py-4 sm:py-8 grid grid-cols-1 lg:grid-cols-2 gap-5 sm:gap-8 animate-fade-in-up">
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
                          className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm animate-slide-stagger ${
                            revealedOptions
                              ? opt.is_correct
                                ? 'bg-green-900/40 border border-green-700 text-green-300'
                                : 'bg-gray-700/40 text-gray-400'
                              : 'bg-gray-700/50 text-gray-300'
                          }`}
                          style={{ animationDelay: `${i * 80}ms` }}
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
                ) : (
                  <p className="text-gray-500 text-sm text-center py-8">
                    {activeQ.type === 'brainstorm'
                      ? 'Результаты брейншторма показаны на доске идей'
                      : `Текстовых ответов: ${Object.values(distribution).reduce((a, b) => a + b, 0)}`}
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
        )}
      </div>
      {presentationLayer}
      </>
    )
  }

  // ════════════════════════════════════════════════════════════════
  // FINISHED — final leaderboard
  // ════════════════════════════════════════════════════════════════
  if (roomStatus === 'finished') {
    return (
      <div className="min-h-screen bg-gray-900 text-white flex flex-col items-center justify-center px-4 sm:px-6 py-8 sm:py-12">
        <div key={screenKey} className="max-w-lg w-full text-center animate-fade-in-up">
          <div className="text-5xl sm:text-6xl mb-4 sm:mb-6">🏆</div>
          <h2 className="text-2xl sm:text-3xl font-bold mb-2">Опрос завершён!</h2>
          <p className="text-gray-400 mb-6 sm:mb-8">Итоговые результаты</p>

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
