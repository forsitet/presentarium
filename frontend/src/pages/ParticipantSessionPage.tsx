import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { socket } from '../ws/socket'

type RoomStatus = 'waiting' | 'active' | 'showing_question' | 'showing_results' | 'finished'

interface QuestionOption {
  text: string
  is_correct?: boolean
  image_url?: string
}

interface CurrentQuestion {
  question_id: string
  type: string
  text: string
  options: QuestionOption[]
  time_limit_seconds: number
  points: number
  position: number
  total: number
}

interface AnswerResult {
  score: number
  is_correct?: boolean | null
}

interface LeaderboardEntry {
  rank: number
  id: string
  name: string
  score: number
}

interface FinalData {
  rankings: LeaderboardEntry[]
  my_rank: number
  my_score: number
}

// Kahoot-style colors for answer buttons
const OPTION_COLORS = [
  { bg: 'bg-red-500 hover:bg-red-400', selected: 'bg-red-600', icon: '▲' },
  { bg: 'bg-blue-500 hover:bg-blue-400', selected: 'bg-blue-600', icon: '◆' },
  { bg: 'bg-yellow-500 hover:bg-yellow-400', selected: 'bg-yellow-600', icon: '●' },
  { bg: 'bg-green-500 hover:bg-green-400', selected: 'bg-green-600', icon: '■' },
  { bg: 'bg-purple-500 hover:bg-purple-400', selected: 'bg-purple-600', icon: '★' },
  { bg: 'bg-pink-500 hover:bg-pink-400', selected: 'bg-pink-600', icon: '♥' },
]

function TimerBar({ remaining, total }: { remaining: number; total: number }) {
  const pct = total > 0 ? (remaining / total) * 100 : 0
  const colorClass =
    pct > 50 ? 'bg-green-400' : pct > 25 ? 'bg-yellow-400' : 'bg-red-400'
  return (
    <div className="w-full bg-white/20 rounded-full h-3 overflow-hidden">
      <div
        className={`h-full rounded-full transition-all duration-1000 ${colorClass}`}
        style={{ width: `${pct}%` }}
      />
    </div>
  )
}

export function ParticipantSessionPage() {
  const { code } = useParams<{ code: string }>()
  const navigate = useNavigate()

  const [status, setStatus] = useState<RoomStatus>('waiting')
  const [question, setQuestion] = useState<CurrentQuestion | null>(null)
  const [timeRemaining, setTimeRemaining] = useState(0)

  // Answer state
  const [selectedSingle, setSelectedSingle] = useState<number | null>(null)
  const [selectedMultiple, setSelectedMultiple] = useState<Set<number>>(new Set())
  const [textAnswer, setTextAnswer] = useState('')
  const [answerSubmitted, setAnswerSubmitted] = useState(false)
  const [answerResult, setAnswerResult] = useState<AnswerResult | null>(null)

  // Post-question results
  const [revealedOptions, setRevealedOptions] = useState<QuestionOption[]>([])
  const [myRank, setMyRank] = useState<number>(0)
  const [myScore, setMyScore] = useState<number>(0)

  // Final session data
  const [finalData, setFinalData] = useState<FinalData | null>(null)

  // Accumulate score across questions
  const totalScoreRef = useRef(0)

  useEffect(() => {
    if (!code) {
      navigate('/join')
      return
    }

    const sessionToken = localStorage.getItem(`session_token_${code}`)
    if (!sessionToken) {
      navigate(`/join/${code}`)
      return
    }

    socket.connect(code, undefined, undefined)

    const onStateChanged = (data: unknown) => {
      const d = data as { status?: RoomStatus }
      if (d?.status) setStatus(d.status)
    }

    const onQuestionStart = (data: unknown) => {
      const d = data as {
        question_id: string
        type: string
        text: string
        options?: QuestionOption[]
        time_limit_seconds: number
        points: number
        position: number
        total: number
      }
      setQuestion({
        question_id: d.question_id,
        type: d.type,
        text: d.text,
        options: d.options || [],
        time_limit_seconds: d.time_limit_seconds,
        points: d.points,
        position: d.position,
        total: d.total,
      })
      setTimeRemaining(d.time_limit_seconds)
      setSelectedSingle(null)
      setSelectedMultiple(new Set())
      setTextAnswer('')
      setAnswerSubmitted(false)
      setAnswerResult(null)
      setRevealedOptions([])
      setStatus('showing_question')
    }

    const onTimerTick = (data: unknown) => {
      const d = data as { remaining: number }
      setTimeRemaining(d.remaining)
    }

    const onAnswerAccepted = (data: unknown) => {
      const d = data as { score: number; is_correct?: boolean | null }
      setAnswerResult({ score: d.score, is_correct: d.is_correct })
      totalScoreRef.current += d.score
    }

    const onQuestionEnd = (data: unknown) => {
      const d = data as { question_id: string; options?: QuestionOption[] }
      if (d.options) setRevealedOptions(d.options)
      setStatus('showing_results')
    }

    const onLeaderboard = (data: unknown) => {
      const d = data as { rankings: LeaderboardEntry[]; my_rank?: number; my_score?: number }
      if (d.my_rank) setMyRank(d.my_rank)
      if (d.my_score !== undefined) setMyScore(d.my_score)
    }

    const onSessionEnd = (data: unknown) => {
      const d = data as { rankings: LeaderboardEntry[]; my_rank?: number; my_score?: number }
      setFinalData({
        rankings: d.rankings || [],
        my_rank: d.my_rank || 0,
        my_score: d.my_score || totalScoreRef.current,
      })
      setStatus('finished')
    }

    socket.on('room_state_changed', onStateChanged)
    socket.on('question_start', onQuestionStart)
    socket.on('timer_tick', onTimerTick)
    socket.on('answer_accepted', onAnswerAccepted)
    socket.on('question_end', onQuestionEnd)
    socket.on('leaderboard', onLeaderboard)
    socket.on('session_end', onSessionEnd)

    return () => {
      socket.off('room_state_changed', onStateChanged)
      socket.off('question_start', onQuestionStart)
      socket.off('timer_tick', onTimerTick)
      socket.off('answer_accepted', onAnswerAccepted)
      socket.off('question_end', onQuestionEnd)
      socket.off('leaderboard', onLeaderboard)
      socket.off('session_end', onSessionEnd)
    }
  }, [code, navigate])

  function submitSingle(idx: number) {
    if (answerSubmitted || !question) return
    setSelectedSingle(idx)
    setAnswerSubmitted(true)
    socket.send('submit_answer', { question_id: question.question_id, answer: idx })
  }

  function submitMultiple() {
    if (answerSubmitted || !question || selectedMultiple.size === 0) return
    setAnswerSubmitted(true)
    socket.send('submit_answer', {
      question_id: question.question_id,
      answer: Array.from(selectedMultiple),
    })
  }

  function toggleMultiple(idx: number) {
    if (answerSubmitted) return
    setSelectedMultiple((prev) => {
      const next = new Set(prev)
      if (next.has(idx)) next.delete(idx)
      else next.add(idx)
      return next
    })
  }

  function submitText() {
    if (answerSubmitted || !question || textAnswer.trim() === '') return
    setAnswerSubmitted(true)
    const msgType = question.type === 'word_cloud' ? 'submit_text' : 'submit_text'
    socket.send(msgType, { question_id: question.question_id, text: textAnswer.trim() })
  }

  // ---- Render helpers ----

  if (status === 'finished') {
    return <FinalScreen finalData={finalData} onHome={() => navigate('/join')} />
  }

  if (status === 'showing_results' && question) {
    return (
      <ResultsScreen
        question={question}
        revealedOptions={revealedOptions}
        answerResult={answerResult}
        selectedSingle={selectedSingle}
        selectedMultiple={selectedMultiple}
        myRank={myRank}
        myScore={myScore}
      />
    )
  }

  if (status === 'showing_question' && question) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-indigo-900 via-purple-900 to-pink-900 flex flex-col p-4">
        {/* Header */}
        <div className="flex items-center justify-between mb-4 text-white/80 text-sm">
          <span>
            Вопрос {question.position} / {question.total}
          </span>
          <span className="font-bold text-lg text-white">{timeRemaining}с</span>
          <span>{question.points} очков</span>
        </div>

        {/* Timer bar */}
        <div className="mb-5">
          <TimerBar remaining={timeRemaining} total={question.time_limit_seconds} />
        </div>

        {/* Question text */}
        <div className="bg-white/10 backdrop-blur rounded-2xl p-5 mb-6 text-center">
          <p className="text-white text-xl font-semibold leading-snug">{question.text}</p>
        </div>

        {/* Answer submitted overlay */}
        {answerSubmitted && (
          <div className="flex-1 flex items-center justify-center">
            <div className="bg-white/20 backdrop-blur rounded-2xl p-8 text-center text-white">
              <div className="text-5xl mb-3">✓</div>
              <p className="text-2xl font-bold">Ответ принят!</p>
              <p className="text-white/70 mt-2">Ждём завершения вопроса...</p>
            </div>
          </div>
        )}

        {/* Inputs */}
        {!answerSubmitted && (
          <div className="flex-1 flex flex-col">
            {(question.type === 'single_choice' || question.type === 'image_choice') && (
              <SingleChoiceInput
                options={question.options}
                selected={selectedSingle}
                onSelect={submitSingle}
              />
            )}
            {question.type === 'multiple_choice' && (
              <MultipleChoiceInput
                options={question.options}
                selected={selectedMultiple}
                onToggle={toggleMultiple}
                onSubmit={submitMultiple}
              />
            )}
            {(question.type === 'open_text' || question.type === 'word_cloud') && (
              <TextInput
                value={textAnswer}
                onChange={setTextAnswer}
                onSubmit={submitText}
                maxLength={question.type === 'word_cloud' ? 50 : 500}
                placeholder={
                  question.type === 'word_cloud' ? 'Введите слово...' : 'Введите ответ...'
                }
              />
            )}
            {question.type === 'brainstorm' && (
              <div className="flex-1 flex items-center justify-center">
                <p className="text-white/60 text-center">Режим брейншторма</p>
              </div>
            )}
          </div>
        )}
      </div>
    )
  }

  // Waiting / active state
  return (
    <div className="min-h-screen bg-gradient-to-br from-indigo-900 via-purple-900 to-pink-900 flex items-center justify-center p-4">
      <div className="text-center text-white">
        <div className="relative w-24 h-24 mx-auto mb-8">
          <div className="absolute inset-0 rounded-full border-4 border-indigo-400/30" />
          <div className="absolute inset-0 rounded-full border-4 border-t-white border-r-transparent border-b-transparent border-l-transparent animate-spin" />
          <div
            className="absolute inset-2 rounded-full border-4 border-t-transparent border-r-purple-400 border-b-transparent border-l-transparent animate-spin"
            style={{ animationDirection: 'reverse', animationDuration: '1.5s' }}
          />
        </div>
        <h1 className="text-4xl font-bold mb-3">Ждём начала...</h1>
        <p className="text-indigo-200 text-lg">Опрос скоро начнётся. Будьте готовы!</p>
        <div className="mt-8 flex justify-center gap-2">
          {[0, 1, 2].map((i) => (
            <div
              key={i}
              className="w-2 h-2 rounded-full bg-indigo-300 animate-bounce"
              style={{ animationDelay: `${i * 0.15}s` }}
            />
          ))}
        </div>
        <div className="mt-12 text-indigo-300/60 text-sm">
          Комната:{' '}
          <span className="font-mono font-bold tracking-widest text-indigo-200">{code}</span>
        </div>
      </div>
    </div>
  )
}

// ---- Sub-components ----

function SingleChoiceInput({
  options,
  selected,
  onSelect,
}: {
  options: QuestionOption[]
  selected: number | null
  onSelect: (idx: number) => void
}) {
  return (
    <div className="grid grid-cols-2 gap-3">
      {options.map((opt, idx) => {
        const color = OPTION_COLORS[idx % OPTION_COLORS.length]
        const isSelected = selected === idx
        return (
          <button
            key={idx}
            onClick={() => onSelect(idx)}
            className={`
              min-h-[64px] rounded-xl px-3 py-4 text-white font-semibold text-base
              flex items-center gap-2 transition-all active:scale-95
              ${isSelected ? color.selected : color.bg}
            `}
          >
            <span className="text-xl opacity-70">{color.icon}</span>
            {opt.image_url ? (
              <img src={opt.image_url} alt={opt.text} className="h-10 object-contain mx-auto" />
            ) : (
              <span className="flex-1 text-left leading-tight">{opt.text}</span>
            )}
          </button>
        )
      })}
    </div>
  )
}

function MultipleChoiceInput({
  options,
  selected,
  onToggle,
  onSubmit,
}: {
  options: QuestionOption[]
  selected: Set<number>
  onToggle: (idx: number) => void
  onSubmit: () => void
}) {
  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-2 gap-3">
        {options.map((opt, idx) => {
          const color = OPTION_COLORS[idx % OPTION_COLORS.length]
          const isSelected = selected.has(idx)
          return (
            <button
              key={idx}
              onClick={() => onToggle(idx)}
              className={`
                min-h-[64px] rounded-xl px-3 py-4 text-white font-semibold text-base
                flex items-center gap-2 transition-all active:scale-95 border-4
                ${isSelected ? `${color.selected} border-white` : `${color.bg} border-transparent`}
              `}
            >
              <span className="text-xl opacity-70">{color.icon}</span>
              <span className="flex-1 text-left leading-tight">{opt.text}</span>
              {isSelected && <span className="text-white text-lg">✓</span>}
            </button>
          )
        })}
      </div>
      <button
        onClick={onSubmit}
        disabled={selected.size === 0}
        className="mt-2 py-4 rounded-xl bg-white text-indigo-700 font-bold text-lg disabled:opacity-40 active:scale-95 transition-all"
      >
        Подтвердить ({selected.size})
      </button>
    </div>
  )
}

function TextInput({
  value,
  onChange,
  onSubmit,
  maxLength,
  placeholder,
}: {
  value: string
  onChange: (v: string) => void
  onSubmit: () => void
  maxLength: number
  placeholder: string
}) {
  return (
    <div className="flex flex-col gap-3">
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        maxLength={maxLength}
        placeholder={placeholder}
        rows={3}
        className="w-full rounded-xl bg-white/10 text-white placeholder-white/40 border border-white/20 p-4 text-lg focus:outline-none focus:border-white/60 resize-none"
      />
      <div className="flex justify-between text-white/50 text-sm px-1">
        <span>{value.length}/{maxLength}</span>
      </div>
      <button
        onClick={onSubmit}
        disabled={value.trim().length === 0}
        className="py-4 rounded-xl bg-white text-indigo-700 font-bold text-lg disabled:opacity-40 active:scale-95 transition-all"
      >
        Отправить
      </button>
    </div>
  )
}

function ResultsScreen({
  question,
  revealedOptions,
  answerResult,
  selectedSingle,
  selectedMultiple,
  myRank,
  myScore,
}: {
  question: CurrentQuestion
  revealedOptions: QuestionOption[]
  answerResult: AnswerResult | null
  selectedSingle: number | null
  selectedMultiple: Set<number>
  myRank: number
  myScore: number
}) {
  const opts = revealedOptions.length > 0 ? revealedOptions : question.options
  const isChoiceType =
    question.type === 'single_choice' ||
    question.type === 'multiple_choice' ||
    question.type === 'image_choice'

  return (
    <div className="min-h-screen bg-gradient-to-br from-indigo-900 via-purple-900 to-pink-900 flex flex-col p-4">
      {/* Score badge */}
      {answerResult && (
        <div className="mb-4 text-center">
          <div
            className={`inline-flex items-center gap-2 px-5 py-3 rounded-full font-bold text-lg ${
              answerResult.is_correct === true
                ? 'bg-green-500 text-white'
                : answerResult.is_correct === false
                  ? 'bg-red-500 text-white'
                  : 'bg-white/20 text-white'
            }`}
          >
            {answerResult.is_correct === true && '✓ Правильно!'}
            {answerResult.is_correct === false && '✗ Неправильно'}
            {answerResult.is_correct == null && '✓ Ответ принят'}
            {answerResult.score > 0 && (
              <span className="ml-2 bg-white/20 px-3 py-1 rounded-full text-sm">
                +{answerResult.score} очков
              </span>
            )}
          </div>
        </div>
      )}

      {/* Question text */}
      <div className="bg-white/10 backdrop-blur rounded-2xl p-4 mb-4 text-center">
        <p className="text-white text-lg font-semibold">{question.text}</p>
      </div>

      {/* Options with correct/wrong highlighting */}
      {isChoiceType && (
        <div className="grid grid-cols-2 gap-3 mb-5">
          {opts.map((opt, idx) => {
            const wasSelected =
              question.type === 'multiple_choice'
                ? selectedMultiple.has(idx)
                : selectedSingle === idx
            const isCorrect = opt.is_correct === true

            let bgClass = 'bg-white/10'
            let borderClass = 'border-transparent'
            if (isCorrect) {
              bgClass = 'bg-green-500'
              borderClass = 'border-green-300'
            } else if (wasSelected && !isCorrect) {
              bgClass = 'bg-red-500'
              borderClass = 'border-red-300'
            }

            return (
              <div
                key={idx}
                className={`min-h-[56px] rounded-xl px-3 py-3 text-white font-semibold border-2 ${bgClass} ${borderClass} flex items-center gap-2`}
              >
                <span className="text-lg opacity-70">{OPTION_COLORS[idx % OPTION_COLORS.length].icon}</span>
                <span className="flex-1 text-sm leading-tight">{opt.text}</span>
                {isCorrect && <span>✓</span>}
                {wasSelected && !isCorrect && <span>✗</span>}
              </div>
            )
          })}
        </div>
      )}

      {/* Rank */}
      {myRank > 0 && (
        <div className="bg-white/10 rounded-xl p-4 text-center text-white mb-3">
          <p className="text-white/60 text-sm mb-1">Ваше место</p>
          <p className="text-3xl font-bold">
            #{myRank}{' '}
            <span className="text-white/60 text-lg font-normal">· {myScore} очков</span>
          </p>
        </div>
      )}

      <p className="text-white/50 text-center text-sm mt-auto">Ждём следующий вопрос...</p>
    </div>
  )
}

function FinalScreen({
  finalData,
  onHome,
}: {
  finalData: FinalData | null
  onHome: () => void
}) {
  const top3 = finalData?.rankings.slice(0, 3) || []
  const podiumOrder = [1, 0, 2] // silver, gold, bronze display order

  return (
    <div className="min-h-screen bg-gradient-to-br from-indigo-900 via-purple-900 to-pink-900 flex flex-col items-center justify-center p-4 text-white">
      <div className="text-6xl mb-4">🏆</div>
      <h1 className="text-4xl font-bold mb-2">Опрос завершён!</h1>

      {finalData && finalData.my_rank > 0 && (
        <div className="mb-6 text-center">
          <p className="text-indigo-200 text-lg">
            Ваш результат:{' '}
            <span className="font-bold text-white">
              #{finalData.my_rank} · {finalData.my_score} очков
            </span>
          </p>
        </div>
      )}

      {/* Podium top-3 */}
      {top3.length >= 2 && (
        <div className="flex items-end gap-3 mb-8 w-full max-w-xs">
          {podiumOrder.map((podIdx) => {
            const entry = top3[podIdx]
            if (!entry) return <div key={podIdx} className="flex-1" />
            const heights = ['h-20', 'h-28', 'h-16']
            const colors = ['bg-gray-400', 'bg-yellow-400', 'bg-amber-600']
            const medals = ['🥈', '🥇', '🥉']
            return (
              <div key={podIdx} className="flex-1 flex flex-col items-center">
                <span className="text-2xl mb-1">{medals[podIdx]}</span>
                <p className="text-xs font-semibold mb-1 text-center truncate w-full text-center">
                  {entry.name}
                </p>
                <p className="text-xs text-white/70 mb-1">{entry.score}</p>
                <div className={`w-full rounded-t-lg ${heights[podIdx]} ${colors[podIdx]}`} />
              </div>
            )
          })}
        </div>
      )}

      {/* Full rankings */}
      {finalData && finalData.rankings.length > 0 && (
        <div className="w-full max-w-sm bg-white/10 rounded-2xl overflow-hidden mb-8">
          {finalData.rankings.slice(0, 10).map((entry) => (
            <div
              key={entry.id}
              className={`flex items-center gap-3 px-4 py-3 border-b border-white/10 last:border-0 ${
                entry.rank === finalData.my_rank ? 'bg-white/20' : ''
              }`}
            >
              <span className="w-6 text-center text-white/60 text-sm font-bold">
                {entry.rank}
              </span>
              <span className="flex-1 font-medium truncate">{entry.name}</span>
              <span className="text-white/80 font-bold">{entry.score}</span>
            </div>
          ))}
        </div>
      )}

      <button
        onClick={onHome}
        className="bg-white text-indigo-700 px-8 py-3 rounded-xl font-semibold text-lg hover:bg-indigo-50 transition-colors"
      >
        На главную
      </button>
    </div>
  )
}
