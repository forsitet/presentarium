import type { Question } from '../types'

interface QuestionPreviewProps {
  question: Question
}

const OPTION_BG = [
  'bg-emerald-500',
  'bg-blue-500',
  'bg-orange-500',
  'bg-red-500',
  'bg-purple-500',
  'bg-teal-500',
]

export function QuestionPreview({ question }: QuestionPreviewProps) {
  const hasChoices = ['single_choice', 'multiple_choice', 'image_choice'].includes(question.type)

  return (
    <div className="bg-gray-900 rounded-xl p-5 text-white">
      {/* Badges */}
      <div className="flex items-center gap-2 mb-4">
        <span className="px-2 py-0.5 text-xs font-medium rounded-full bg-white/20">
          {question.time_limit_seconds}c
        </span>
        <span className="px-2 py-0.5 text-xs font-medium rounded-full bg-white/20">
          {question.points} pts
        </span>
      </div>

      {/* Question text */}
      <p className="text-base font-semibold mb-4 leading-snug">
        {question.text || 'Текст вопроса...'}
      </p>

      {/* Choice options */}
      {hasChoices && question.options && question.options.length > 0 && (
        <div className="grid grid-cols-2 gap-2">
          {question.options.map((opt, i) => (
            <div
              key={i}
              className={`${OPTION_BG[i] ?? 'bg-gray-600'} rounded-lg px-3 py-2.5 text-sm font-medium text-white truncate`}
            >
              {opt.text || `Вариант ${i + 1}`}
            </div>
          ))}
        </div>
      )}

      {/* Open text */}
      {question.type === 'open_text' && (
        <div className="rounded-lg border border-white/20 px-3 py-2.5 text-sm text-white/50">
          Введите ответ...
        </div>
      )}

      {/* Word cloud */}
      {question.type === 'word_cloud' && (
        <div className="rounded-lg border border-white/20 px-3 py-2.5 text-sm text-white/50">
          Введите слово или фразу...
        </div>
      )}

      {/* Brainstorm */}
      {question.type === 'brainstorm' && (
        <div className="rounded-lg border border-white/20 px-3 py-2.5 text-sm text-white/50">
          + Добавить идею
        </div>
      )}
    </div>
  )
}
