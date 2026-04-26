import axios from 'axios'
import { apiClient } from './client'
import type { Poll, Question, Participant, SessionSummary, SessionDetail, ParticipantHistorySummary } from '../types'

const BASE_URL = import.meta.env.VITE_API_URL || '/api'

export async function getPolls(): Promise<Poll[]> {
  const res = await apiClient.get<Poll[]>('/polls')
  return res.data ?? []
}

export async function deletePoll(id: string): Promise<void> {
  await apiClient.delete(`/polls/${id}`)
}

export async function copyPoll(id: string): Promise<Poll> {
  const res = await apiClient.post<Poll>(`/polls/${id}/copy`)
  return res.data
}

export async function createRoom(pollId: string): Promise<{ room_code: string }> {
  const res = await apiClient.post<{ room_code: string; join_url: string }>('/rooms', { poll_id: pollId })
  return res.data
}

export async function getPoll(id: string): Promise<Poll> {
  const res = await apiClient.get<Poll>(`/polls/${id}`)
  return res.data
}

export async function createPoll(data: {
  title: string
  description?: string
  scoring_rule: Poll['scoring_rule']
  question_order: Poll['question_order']
  show_answer_distribution?: boolean
}): Promise<Poll> {
  const res = await apiClient.post<Poll>('/polls', data)
  return res.data
}

export async function updatePoll(id: string, data: Partial<Poll>): Promise<Poll> {
  const res = await apiClient.put<Poll>(`/polls/${id}`, data)
  return res.data
}

export async function getQuestions(pollId: string): Promise<Question[]> {
  const res = await apiClient.get<Question[]>(`/polls/${pollId}/questions`)
  return res.data ?? []
}

export async function createQuestion(
  pollId: string,
  data: Omit<Question, 'id' | 'poll_id'>,
): Promise<Question> {
  const res = await apiClient.post<Question>(`/polls/${pollId}/questions`, data)
  return res.data
}

export async function updateQuestion(
  pollId: string,
  questionId: string,
  data: Partial<Question>,
): Promise<Question> {
  const res = await apiClient.put<Question>(`/polls/${pollId}/questions/${questionId}`, data)
  return res.data
}

export async function deleteQuestion(pollId: string, questionId: string): Promise<void> {
  await apiClient.delete(`/polls/${pollId}/questions/${questionId}`)
}

export async function reorderQuestions(
  pollId: string,
  order: Array<{ id: string; position: number }>,
): Promise<void> {
  await apiClient.patch(`/polls/${pollId}/questions/reorder`, order)
}

export interface RoomInfo {
  room_code: string
  poll_id: string
  session_id: string
  status: string
  participants: number
  show_answer_distribution: boolean
}

export async function getRoomInfo(code: string): Promise<RoomInfo> {
  const res = await apiClient.get<RoomInfo>(`/rooms/${code}`)
  return res.data
}

export async function getRoomParticipants(code: string): Promise<Participant[]> {
  const res = await apiClient.get<Participant[]>(`/rooms/${code}/participants`)
  return res.data ?? []
}

export async function changeRoomState(
  code: string,
  action: 'start' | 'end' | 'next_question' | 'end_question',
): Promise<void> {
  await apiClient.patch(`/rooms/${code}/state`, { action })
}

export async function getSessions(): Promise<SessionSummary[]> {
  const res = await apiClient.get<SessionSummary[]>('/sessions')
  return res.data ?? []
}

export async function getSession(id: string): Promise<SessionDetail> {
  const res = await apiClient.get<SessionDetail>(`/sessions/${id}`)
  return res.data
}

// Public endpoint — no JWT required. Uses plain axios to avoid 401 redirect.
export async function getParticipantSessionHistory(sessionToken: string): Promise<ParticipantHistorySummary> {
  const res = await axios.get<ParticipantHistorySummary>(
    `${BASE_URL}/sessions/by-token?session_token=${encodeURIComponent(sessionToken)}`,
  )
  return res.data
}
