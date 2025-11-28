import { create } from 'zustand'
import type { Participant, Question, LeaderboardEntry } from '../types'

type RoomStatus = 'waiting' | 'active' | 'showing_question' | 'showing_results' | 'finished'

interface SessionState {
  roomCode: string | null
  pollId: string | null
  roomStatus: RoomStatus
  participants: Participant[]
  currentQuestion: Question | null
  leaderboard: LeaderboardEntry[]
  answerCount: number

  setRoom: (code: string, pollId?: string) => void
  setStatus: (status: RoomStatus) => void
  addParticipant: (p: Participant) => void
  removeParticipant: (id: string) => void
  setParticipants: (ps: Participant[]) => void
  setCurrentQuestion: (q: Question | null) => void
  setLeaderboard: (lb: LeaderboardEntry[]) => void
  setAnswerCount: (n: number) => void
  reset: () => void
}

const initialState = {
  roomCode: null,
  pollId: null,
  roomStatus: 'waiting' as RoomStatus,
  participants: [],
  currentQuestion: null,
  leaderboard: [],
  answerCount: 0,
}

export const useSessionStore = create<SessionState>()((set) => ({
  ...initialState,

  setRoom: (code, pollId) => set({ roomCode: code, pollId }),
  setStatus: (status) => set({ roomStatus: status }),

  addParticipant: (p) =>
    set((s) => ({
      participants: s.participants.find((x) => x.id === p.id)
        ? s.participants
        : [...s.participants, p],
    })),

  removeParticipant: (id) =>
    set((s) => ({
      participants: s.participants.filter((p) => p.id !== id),
    })),

  setParticipants: (ps) => set({ participants: ps }),
  setCurrentQuestion: (q) => set({ currentQuestion: q }),
  setLeaderboard: (lb) => set({ leaderboard: lb }),
  setAnswerCount: (n) => set({ answerCount: n }),
  reset: () => set(initialState),
}))
