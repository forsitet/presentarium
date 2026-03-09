export interface User {
  id: string
  email: string
  name: string
}

export interface Poll {
  id: string
  title: string
  description?: string
  scoring_rule: 'none' | 'correct_answer' | 'speed_bonus'
  question_order: 'sequential' | 'random'
  created_at: string
  updated_at: string
}

export interface Question {
  id: string
  poll_id: string
  type: 'single_choice' | 'multiple_choice' | 'open_text' | 'image_choice' | 'word_cloud' | 'brainstorm'
  text: string
  options?: QuestionOption[]
  time_limit_seconds: number
  points: number
  position: number
}

export interface QuestionOption {
  text: string
  is_correct: boolean
  image_url?: string
}

export interface Session {
  id: string
  poll_id: string
  room_code: string
  status: 'waiting' | 'active' | 'showing_question' | 'showing_results' | 'finished'
  started_at?: string
  finished_at?: string
}

export interface Participant {
  id: string
  name: string
  session_token: string
  joined_at: string
  total_score: number
}

export interface LeaderboardEntry {
  rank: number
  participant_id: string
  name: string
  score: number
}
