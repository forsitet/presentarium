import { useEffect, useState } from 'react'
import { socket } from '../ws/socket'
import type { BrainstormPhase } from './BrainstormInput'

interface Idea {
  id: string
  participant_id: string
  text: string
  votes_count: number
  is_hidden: boolean
}

interface BrainstormBoardProps {
  questionId: string
  answeredCount: number
  totalParticipants: number
}

export function BrainstormBoard({
  questionId,
  answeredCount,
  totalParticipants,
}: BrainstormBoardProps) {
  const [phase, setPhase] = useState<BrainstormPhase>('collecting')
  const [ideas, setIdeas] = useState<Idea[]>([])
  const [phaseChanging, setPhaseChanging] = useState(false)

  useEffect(() => {
    const onIdeaAdded = (data: unknown) => {
      const idea = data as Omit<Idea, 'is_hidden'>
      setIdeas((prev) => {
        if (prev.some((i) => i.id === idea.id)) return prev
        return [...prev, { ...idea, is_hidden: false }]
      })
    }

    const onPhaseChanged = (data: unknown) => {
      const { phase: newPhase, ideas: newIdeas } = data as {
        phase: BrainstormPhase
        ideas?: Idea[]
      }
      setPhase(newPhase)
      setPhaseChanging(false)
      if (newIdeas) {
        setIdeas(newIdeas.map((i) => ({ ...i, is_hidden: i.is_hidden ?? false })))
      }
    }

    const onVoteUpdated = (data: unknown) => {
      const { idea_id, votes_count } = data as { idea_id: string; votes_count: number }
      setIdeas((prev) =>
        prev.map((i) => (i.id === idea_id ? { ...i, votes_count } : i))
      )
    }

    const onAnswerHidden = (data: unknown) => {
      const { id, is_hidden } = data as { id: string; is_hidden: boolean }
      setIdeas((prev) =>
        prev.map((i) => (i.id === id ? { ...i, is_hidden } : i))
      )
    }

    socket.on('brainstorm_idea_added', onIdeaAdded)
    socket.on('brainstorm_phase_changed', onPhaseChanged)
    socket.on('brainstorm_vote_updated', onVoteUpdated)
    socket.on('answer_hidden', onAnswerHidden)

    return () => {
      socket.off('brainstorm_idea_added', onIdeaAdded)
      socket.off('brainstorm_phase_changed', onPhaseChanged)
      socket.off('brainstorm_vote_updated', onVoteUpdated)
      socket.off('answer_hidden', onAnswerHidden)
    }
  }, [])

  function toggleHide(ideaId: string, currentHidden: boolean) {
    socket.send('brainstorm_hide_idea', {
      idea_id: ideaId,
      is_hidden: !currentHidden,
    })
  }

  function advancePhase() {
    const nextPhase = phase === 'collecting' ? 'voting' : 'results'
    setPhaseChanging(true)
    socket.send('brainstorm_change_phase', {
      question_id: questionId,
      phase: nextPhase,
    })
  }


  const hiddenIdeas = ideas.filter((i) => i.is_hidden)
  const sortedResults =
    phase === 'results'
      ? [...ideas].sort((a, b) => b.votes_count - a.votes_count)
      : []

  const PHASE_LABELS: Record<BrainstormPhase, string> = {
    collecting: 'Сбор идей',
    voting: 'Голосование',
    results: 'Результаты',
  }

  return (
    <div className="flex flex-col gap-4 h-full">
      {/* Phase header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          {(['collecting', 'voting', 'results'] as BrainstormPhase[]).map((p) => (
            <div
              key={p}
              className={`px-3 py-1.5 rounded-full text-xs font-bold transition-all ${
                p === phase
                  ? 'bg-indigo-600 text-white'
                  : 'bg-gray-700 text-gray-400'
              }`}
            >
              {PHASE_LABELS[p]}
            </div>
          ))}
        </div>

        {phase !== 'results' && (
          <button
            onClick={advancePhase}
            disabled={phaseChanging}
            className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-2 rounded-lg font-bold transition-all active:scale-[0.97]"
          >
            {phaseChanging
              ? '...'
              : phase === 'collecting'
                ? 'Открыть голосование →'
                : 'Показать результаты →'}
          </button>
        )}
      </div>

      {/* Stats row */}
      <div className="flex gap-3 text-sm">
        <div className="bg-gray-700 rounded-lg px-3 py-2 flex items-center gap-2">
          <span className="text-gray-400">Идей:</span>
          <span className="text-white font-bold">{ideas.length}</span>
        </div>
        {phase === 'collecting' && (
          <div className="bg-gray-700 rounded-lg px-3 py-2 flex items-center gap-2">
            <span className="text-gray-400">Участников ответили:</span>
            <span className="text-white font-bold">
              {answeredCount} / {totalParticipants}
            </span>
          </div>
        )}
        {hiddenIdeas.length > 0 && (
          <div className="bg-gray-700 rounded-lg px-3 py-2 flex items-center gap-2">
            <span className="text-gray-400">Скрыто:</span>
            <span className="text-orange-400 font-bold">{hiddenIdeas.length}</span>
          </div>
        )}
      </div>

      {/* Ideas list */}
      {phase === 'results' ? (
        <ResultsList ideas={sortedResults} onToggleHide={toggleHide} />
      ) : (
        <IdeasList ideas={ideas} onToggleHide={toggleHide} />
      )}
    </div>
  )
}

function IdeasList({
  ideas,
  onToggleHide,
}: {
  ideas: Idea[]
  onToggleHide: (id: string, hidden: boolean) => void
}) {
  if (ideas.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <p className="text-gray-500 text-sm">Ждём идей от участников...</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2 overflow-y-auto max-h-64">
      {ideas.map((idea, i) => (
        <div
          key={idea.id}
          className={`flex items-start gap-3 rounded-xl px-4 py-3 transition-all animate-fade-in-up ${
            idea.is_hidden
              ? 'bg-gray-700/40 border border-dashed border-gray-600'
              : 'bg-gray-700 border border-gray-600'
          }`}
          style={{ animationDelay: `${i * 30}ms` }}
        >
          <span
            className={`flex-1 text-sm leading-snug ${
              idea.is_hidden ? 'text-gray-500 line-through' : 'text-white'
            }`}
          >
            {idea.text}
          </span>
          <button
            onClick={() => onToggleHide(idea.id, idea.is_hidden)}
            className={`shrink-0 text-xs px-2 py-1 rounded-lg transition-all ${
              idea.is_hidden
                ? 'bg-green-700/50 hover:bg-green-700 text-green-300'
                : 'bg-orange-700/50 hover:bg-orange-700 text-orange-300'
            }`}
            title={idea.is_hidden ? 'Показать' : 'Скрыть'}
          >
            {idea.is_hidden ? '👁 Показать' : '✕ Скрыть'}
          </button>
        </div>
      ))}
    </div>
  )
}

function ResultsList({
  ideas,
  onToggleHide,
}: {
  ideas: Idea[]
  onToggleHide: (id: string, hidden: boolean) => void
}) {
  if (ideas.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <p className="text-gray-500 text-sm">Нет идей</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2 overflow-y-auto max-h-72">
      {ideas.map((idea, i) => (
        <div
          key={idea.id}
          className={`flex items-center gap-3 rounded-xl px-4 py-3 animate-slide-stagger ${
            idea.is_hidden ? 'bg-gray-700/40 opacity-60' : 'bg-gray-700'
          }`}
          style={{ animationDelay: `${i * 60}ms` }}
        >
          <span className="text-indigo-400 font-black text-lg w-7 text-center shrink-0">
            {i + 1}
          </span>
          <span
            className={`flex-1 text-sm leading-snug ${
              idea.is_hidden ? 'text-gray-500 line-through' : 'text-white'
            }`}
          >
            {idea.text}
          </span>
          <span className="bg-indigo-600 text-white text-xs font-bold px-2 py-1 rounded-full shrink-0">
            {idea.votes_count} 👍
          </span>
          <button
            onClick={() => onToggleHide(idea.id, idea.is_hidden)}
            className={`shrink-0 text-xs px-2 py-1 rounded-lg transition-all ${
              idea.is_hidden
                ? 'bg-green-700/50 hover:bg-green-700 text-green-300'
                : 'bg-orange-700/50 hover:bg-orange-700 text-orange-300'
            }`}
          >
            {idea.is_hidden ? '👁' : '✕'}
          </button>
        </div>
      ))}
    </div>
  )
}
