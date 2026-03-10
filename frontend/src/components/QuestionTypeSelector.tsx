import { useEffect } from 'react'
import type { Question } from '../types'

type QuestionType = Question['type']

interface QuestionTypeSelectorProps {
  onSelect: (type: QuestionType) => void
  onClose: () => void
}

const QUESTION_TYPES: Array<{
  type: QuestionType
  icon: string
  title: string
  description: string
}> = [
  {
    type: 'single_choice',
    icon: '\u2611\uFE0F',
    title: '\u041E\u0434\u0438\u043D\u043E\u0447\u043D\u044B\u0439 \u0432\u044B\u0431\u043E\u0440',
    description: '\u0423\u0447\u0430\u0441\u0442\u043D\u0438\u043A \u0432\u044B\u0431\u0438\u0440\u0430\u0435\u0442 \u043E\u0434\u0438\u043D \u043F\u0440\u0430\u0432\u0438\u043B\u044C\u043D\u044B\u0439 \u043E\u0442\u0432\u0435\u0442',
  },
  {
    type: 'multiple_choice',
    icon: '\u2705',
    title: '\u041C\u043D\u043E\u0436\u0435\u0441\u0442\u0432\u0435\u043D\u043D\u044B\u0439 \u0432\u044B\u0431\u043E\u0440',
    description: '\u041C\u043E\u0436\u043D\u043E \u0432\u044B\u0431\u0440\u0430\u0442\u044C \u043D\u0435\u0441\u043A\u043E\u043B\u044C\u043A\u043E \u043F\u0440\u0430\u0432\u0438\u043B\u044C\u043D\u044B\u0445 \u043E\u0442\u0432\u0435\u0442\u043E\u0432',
  },
  {
    type: 'open_text',
    icon: '\uD83D\uDCDD',
    title: '\u041E\u0442\u043A\u0440\u044B\u0442\u044B\u0439 \u0442\u0435\u043A\u0441\u0442',
    description: '\u0423\u0447\u0430\u0441\u0442\u043D\u0438\u043A \u043F\u0438\u0448\u0435\u0442 \u0441\u0432\u043E\u0431\u043E\u0434\u043D\u044B\u0439 \u043E\u0442\u0432\u0435\u0442',
  },
  {
    type: 'image_choice',
    icon: '\uD83D\uDDBC\uFE0F',
    title: '\u0412\u044B\u0431\u043E\u0440 \u0441 \u043A\u0430\u0440\u0442\u0438\u043D\u043A\u0430\u043C\u0438',
    description: '\u0412\u0430\u0440\u0438\u0430\u043D\u0442\u044B \u043E\u0442\u0432\u0435\u0442\u043E\u0432 \u0441 \u0438\u0437\u043E\u0431\u0440\u0430\u0436\u0435\u043D\u0438\u044F\u043C\u0438',
  },
  {
    type: 'word_cloud',
    icon: '\u2601\uFE0F',
    title: '\u041E\u0431\u043B\u0430\u043A\u043E \u0441\u043B\u043E\u0432',
    description: '\u0423\u0447\u0430\u0441\u0442\u043D\u0438\u043A\u0438 \u043F\u0440\u0435\u0434\u043B\u0430\u0433\u0430\u044E\u0442 \u0441\u043B\u043E\u0432\u0430 \u0438 \u0444\u0440\u0430\u0437\u044B',
  },
  {
    type: 'brainstorm',
    icon: '\uD83E\uDDE0',
    title: '\u041C\u043E\u0437\u0433\u043E\u0432\u043E\u0439 \u0448\u0442\u0443\u0440\u043C',
    description: '\u0421\u0431\u043E\u0440 \u0438\u0434\u0435\u0439 \u043E\u0442 \u0443\u0447\u0430\u0441\u0442\u043D\u0438\u043A\u043E\u0432',
  },
]

export function QuestionTypeSelector({ onSelect, onClose }: QuestionTypeSelectorProps) {
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onClose])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div
        className="absolute inset-0 bg-black/40"
        onClick={onClose}
      />
      <div className="relative bg-white rounded-2xl shadow-xl p-6 w-full max-w-lg mx-4">
        <div className="flex items-center justify-between mb-5">
          <h2 className="text-lg font-semibold text-gray-900">
            Выберите тип вопроса
          </h2>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-600 transition-colors text-xl leading-none"
            aria-label="Закрыть"
          >
            &times;
          </button>
        </div>
        <div className="grid grid-cols-2 gap-3">
          {QUESTION_TYPES.map((qt) => (
            <button
              key={qt.type}
              onClick={() => onSelect(qt.type)}
              className="flex flex-col items-start gap-1 p-4 rounded-xl border border-gray-200 hover:border-indigo-300 hover:bg-indigo-50/50 transition-colors text-left group"
            >
              <span className="text-2xl mb-1">{qt.icon}</span>
              <span className="text-sm font-medium text-gray-900 group-hover:text-indigo-700">
                {qt.title}
              </span>
              <span className="text-xs text-gray-500 leading-snug">
                {qt.description}
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}
