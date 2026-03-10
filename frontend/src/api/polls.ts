import { apiClient } from './client'
import type { Poll } from '../types'

export async function getPolls(): Promise<Poll[]> {
  const res = await apiClient.get<Poll[]>('/polls')
  return res.data
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
