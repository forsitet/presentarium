import { useEffect, useRef, useState } from 'react'
import { socket } from '../ws/socket'

export type BrainstormPhase = 'collecting' | 'voting' | 'results'

interface Idea {
  id: string
  text: string
  votes_count: number
}

interface BrainstormInputProps {
  questionId: string
}

const MAX_IDEAS = 5
const MAX_VOTES = 3
const MAX_IDEA_LEN = 300

export function BrainstormInput({ questionId }: BrainstormInputProps) {
  const [phase, setPhase] = useState<BrainstormPhase>('collecting')
  const [inputText, setInputText] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // Collecting phase state
  const [myIdeas, setMyIdeas] = useState<Idea[]>([])
  const myIdeaIds = useRef<Set<string>>(new Set())

  // Voting phase state
  const [votingIdeas, setVotingIdeas] = useState<Idea[]>([])
  const [myVotes, setMyVotes] = useState<Set<string>>(new Set())

  // Results phase state
  const [rankedIdeas, setRankedIdeas] = useState<Idea[]>([])

  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const onIdeaAdded = (data: unknown) => {
      const idea = data as Idea
      setMyIdeas((prev) => {
        if (prev.some((i) => i.id === idea.id)) return prev
        return [...prev, idea]
      })
      myIdeaIds.current.add(idea.id)
      setSubmitting(false)
    }

    const onPhaseChanged = (data: unknown) => {
      const { phase: newPhase, ideas } = data as {
        phase: BrainstormPhase
        ideas?: Idea[]
      }
      setPhase(newPhase)
      setError(null)

      if (newPhase === 'voting' && ideas) {
        // Show others' ideas (exclude own)
        setVotingIdeas(
          ideas.filter((i) => !myIdeaIds.current.has(i.id))
        )
      }
      if (newPhase === 'results' && ideas) {
        setRankedIdeas(ideas)
      }
    }

    const onVoteUpdated = (data: unknown) => {
      const { idea_id, votes_count } = data as { idea_id: string; votes_count: number }
      setVotingIdeas((prev) =>
        prev.map((i) => (i.id === idea_id ? { ...i, votes_count } : i))
      )
      setRankedIdeas((prev) =>
        prev.map((i) => (i.id === idea_id ? { ...i, votes_count } : i))
      )
    }

    const onAnswerHidden = (data: unknown) => {
      const { idea_id, is_hidden } = data as {
        idea_id: string
        is_hidden: boolean
      }
      if (is_hidden) {
        setVotingIdeas((prev) => prev.filter((i) => i.id !== idea_id))
        setRankedIdeas((prev) => prev.filter((i) => i.id !== idea_id))
      }
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

  function submitIdea() {
    const text = inputText.trim()
    if (!text || myIdeas.length >= MAX_IDEAS || submitting) return
    setSubmitting(true)
    setError(null)
    setInputText('')
    socket.send('submit_idea', { question_id: questionId, text })
    // Timeout fallback if server doesn't respond
    setTimeout(() => setSubmitting(false), 4000)
  }

  function submitVote(ideaId: string) {
    if (myVotes.has(ideaId) || myVotes.size >= MAX_VOTES) return
    setMyVotes((prev) => new Set([...prev, ideaId]))
    socket.send('submit_vote', { question_id: questionId, idea_id: ideaId })
  }

  // ── Collecting phase ──────────────────────────────────────────
  if (phase === 'collecting') {
    const remaining = MAX_IDEAS - myIdeas.length
    return (
      <div className="flex flex-col gap-4">
        {/* Input */}
        <div className="bg-white/10 rounded-2xl p-4">
          <div className="flex items-center justify-between mb-2">
            <span className="text-white font-semibold text-sm">Ваши идеи</span>
            <span
              className={`text-sm font-bold ${remaining === 0 ? 'text-red-400' : 'text-indigo-300'}`}
            >
              {remaining} / {MAX_IDEAS} осталось
            </span>
          </div>
          <textarea
            value={inputText}
            onChange={(e) => setInputText(e.target.value)}
            maxLength={MAX_IDEA_LEN}
            disabled={remaining === 0 || submitting}
            placeholder="Введите идею..."
            rows={3}
            className="w-full rounded-xl bg-white/10 text-white placeholder-white/40 border border-white/20 p-3 text-base focus:outline-none focus:border-white/60 resize-none disabled:opacity-50"
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                submitIdea()
              }
            }}
          />
          <div className="flex items-center justify-between mt-2">
            <span className="text-white/40 text-xs">
              {inputText.length}/{MAX_IDEA_LEN}
            </span>
            <button
              onClick={submitIdea}
              disabled={!inputText.trim() || remaining === 0 || submitting}
              className="bg-indigo-500 hover:bg-indigo-400 disabled:opacity-40 text-white px-5 py-2 rounded-xl text-sm font-bold transition-all active:scale-95"
            >
              {submitting ? '...' : '+ Добавить'}
            </button>
          </div>
          {error && <p className="text-red-400 text-xs mt-1">{error}</p>}
        </div>

        {/* Own ideas list */}
        {myIdeas.length > 0 && (
          <div className="flex flex-col gap-2">
            {myIdeas.map((idea, i) => (
              <div
                key={idea.id}
                className="bg-white/10 rounded-xl px-4 py-3 text-white text-sm flex items-center gap-2 animate-fade-in-up"
                style={{ animationDelay: `${i * 40}ms` }}
              >
                <span className="text-indigo-300 font-bold text-xs">{i + 1}.</span>
                <span className="flex-1 leading-snug">{idea.text}</span>
                <span className="text-green-400 text-xs">✓</span>
              </div>
            ))}
          </div>
        )}

        {myIdeas.length === 0 && (
          <p className="text-white/40 text-sm text-center py-4">
            Поделитесь своими идеями!
          </p>
        )}

        <p className="text-white/30 text-xs text-center">
          Ждём, пока организатор откроет голосование...
        </p>
      </div>
    )
  }

  // ── Voting phase ──────────────────────────────────────────────
  if (phase === 'voting') {
    const votesLeft = MAX_VOTES - myVotes.size
    return (
      <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <span className="text-white font-semibold">Голосуйте за лучшие идеи</span>
          <span
            className={`text-sm font-bold px-3 py-1 rounded-full ${
              votesLeft === 0 ? 'bg-red-500/30 text-red-300' : 'bg-indigo-500/30 text-indigo-300'
            }`}
          >
            {votesLeft} голосов
          </span>
        </div>

        {votingIdeas.length === 0 ? (
          <p className="text-white/40 text-sm text-center py-8">
            Нет идей для голосования
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            {votingIdeas.map((idea, i) => {
              const voted = myVotes.has(idea.id)
              const canVote = !voted && votesLeft > 0
              return (
                <div
                  key={idea.id}
                  className={`rounded-xl px-4 py-3 flex items-center gap-3 transition-all animate-fade-in-up ${
                    voted
                      ? 'bg-indigo-600/50 border border-indigo-400'
                      : 'bg-white/10 border border-transparent'
                  }`}
                  style={{ animationDelay: `${i * 50}ms` }}
                >
                  <span className="flex-1 text-white text-sm leading-snug">{idea.text}</span>
                  <button
                    onClick={() => submitVote(idea.id)}
                    disabled={!canVote && !voted}
                    className={`shrink-0 w-10 h-10 rounded-full text-lg font-bold transition-all active:scale-90 ${
                      voted
                        ? 'bg-indigo-500 text-white cursor-default'
                        : canVote
                          ? 'bg-white/20 hover:bg-white/30 text-white'
                          : 'bg-white/5 text-white/20 cursor-not-allowed'
                    }`}
                  >
                    {voted ? '✓' : '+'}
                  </button>
                </div>
              )
            })}
          </div>
        )}
      </div>
    )
  }

  // ── Results phase ─────────────────────────────────────────────
  return (
    <div className="flex flex-col gap-3">
      <h3 className="text-white font-bold text-center text-lg">Результаты брейншторма</h3>
      {rankedIdeas.length === 0 ? (
        <p className="text-white/40 text-sm text-center py-8">Ждём результатов...</p>
      ) : (
        rankedIdeas.map((idea, i) => (
          <div
            key={idea.id}
            className="bg-white/10 rounded-xl px-4 py-3 flex items-center gap-3 animate-slide-stagger"
            style={{ animationDelay: `${i * 80}ms` }}
          >
            <span className="text-indigo-300 font-black text-lg w-6 text-center">
              {i + 1}
            </span>
            <span className="flex-1 text-white text-sm leading-snug">{idea.text}</span>
            <span className="bg-indigo-500 text-white text-xs font-bold px-2 py-1 rounded-full shrink-0">
              {idea.votes_count} 👍
            </span>
          </div>
        ))
      )}
    </div>
  )
}
