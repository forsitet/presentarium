import { useState, useCallback, useEffect, useRef } from 'react'
import type { Question, QuestionOption } from '../types'

interface QuestionEditorPanelProps {
  question: Question
  pollId: string
  scoringRule?: string
  onSave: (updated: Question) => void
  onDelete: () => void
}

const OPTION_COLORS = [
  'bg-emerald-500',
  'bg-blue-500',
  'bg-orange-500',
  'bg-red-500',
  'bg-purple-500',
  'bg-teal-500',
]

const HAS_OPTIONS: Array<Question['type']> = ['single_choice', 'multiple_choice', 'image_choice']

const TYPE_LABELS: Record<Question['type'], string> = {
  single_choice: '\u041E\u0434\u0438\u043D\u043E\u0447\u043D\u044B\u0439 \u0432\u044B\u0431\u043E\u0440',
  multiple_choice: '\u041C\u043D\u043E\u0436\u0435\u0441\u0442\u0432\u0435\u043D\u043D\u044B\u0439 \u0432\u044B\u0431\u043E\u0440',
  open_text: '\u041E\u0442\u043A\u0440\u044B\u0442\u044B\u0439 \u0442\u0435\u043A\u0441\u0442',
  image_choice: '\u0412\u044B\u0431\u043E\u0440 \u0441 \u043A\u0430\u0440\u0442\u0438\u043D\u043A\u0430\u043C\u0438',
  word_cloud: '\u041E\u0431\u043B\u0430\u043A\u043E \u0441\u043B\u043E\u0432',
  brainstorm: '\u041C\u043E\u0437\u0433\u043E\u0432\u043E\u0439 \u0448\u0442\u0443\u0440\u043C',
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value))
}

export function QuestionEditorPanel(props: QuestionEditorPanelProps) {
  const { question, onSave, onDelete } = props

  const [text, setText] = useState(question.text)
  const [timeLimit, setTimeLimit] = useState(question.time_limit_seconds)
  const [points, setPoints] = useState(question.points)
  const [options, setOptions] = useState<QuestionOption[]>(question.options ?? [])
  const [saved, setSaved] = useState(false)
  const savedTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  // Reset local state when the selected question changes
  useEffect(() => {
    setText(question.text)
    setTimeLimit(question.time_limit_seconds)
    setPoints(question.points)
    setOptions(question.options ?? [])
    setSaved(false)
  }, [question.id, question.text, question.time_limit_seconds, question.points, question.options])

  const showSaved = useCallback(() => {
    setSaved(true)
    if (savedTimerRef.current) clearTimeout(savedTimerRef.current)
    savedTimerRef.current = setTimeout(() => setSaved(false), 2000)
  }, [])

  useEffect(() => {
    return () => {
      if (savedTimerRef.current) clearTimeout(savedTimerRef.current)
    }
  }, [])

  const buildQuestion = useCallback(
    (overrides: Partial<{ text: string; timeLimit: number; points: number; options: QuestionOption[] }> = {}): Question => {
      const finalText = overrides.text ?? text
      const finalTimeLimit = overrides.timeLimit ?? timeLimit
      const finalPoints = overrides.points ?? points
      const finalOptions = overrides.options ?? options
      return {
        ...question,
        text: finalText,
        time_limit_seconds: finalTimeLimit,
        points: finalPoints,
        options: HAS_OPTIONS.includes(question.type) ? finalOptions : undefined,
      }
    },
    [question, text, timeLimit, points, options],
  )

  const handleSave = useCallback(
    (overrides: Partial<{ text: string; timeLimit: number; points: number; options: QuestionOption[] }> = {}) => {
      onSave(buildQuestion(overrides))
      showSaved()
    },
    [buildQuestion, onSave, showSaved],
  )

  const handleTextBlur = () => {
    handleSave({ text })
  }

  const handleTimeLimitBlur = () => {
    const clamped = clamp(timeLimit, 5, 300)
    setTimeLimit(clamped)
    handleSave({ timeLimit: clamped })
  }

  const handlePointsBlur = () => {
    const clamped = clamp(points, 0, 10000)
    setPoints(clamped)
    handleSave({ points: clamped })
  }

  // Options management
  const updateOption = (index: number, field: keyof QuestionOption, value: string | boolean) => {
    const next = options.map((opt, i) => {
      if (i !== index) {
        if (field === 'is_correct' && value === true && question.type === 'single_choice') {
          return { ...opt, is_correct: false }
        }
        return opt
      }
      return { ...opt, [field]: value }
    })
    setOptions(next)
  }

  const handleOptionBlur = () => {
    handleSave({ options })
  }

  const handleCorrectToggle = (index: number) => {
    let next: QuestionOption[]
    if (question.type === 'single_choice') {
      next = options.map((opt, i) => ({
        ...opt,
        is_correct: i === index,
      }))
    } else {
      next = options.map((opt, i) =>
        i === index ? { ...opt, is_correct: !opt.is_correct } : opt,
      )
    }
    setOptions(next)
    handleSave({ options: next })
  }

  const addOption = () => {
    if (options.length >= 6) return
    const next = [...options, { text: '', is_correct: false }]
    setOptions(next)
  }

  const removeOption = (index: number) => {
    if (options.length <= 2) return
    const next = options.filter((_, i) => i !== index)
    setOptions(next)
    handleSave({ options: next })
  }

  const hasOptions = HAS_OPTIONS.includes(question.type)

  return (
    <div className="bg-white rounded-xl border border-gray-200 p-5">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <span className="px-2.5 py-1 text-xs font-medium rounded-full bg-indigo-100 text-indigo-700">
            {TYPE_LABELS[question.type]}
          </span>
          {saved && (
            <span className="text-xs text-emerald-600 font-medium">
              Сохранено
            </span>
          )}
        </div>
        <button
          onClick={onDelete}
          className="text-sm text-red-500 hover:text-red-700 transition-colors"
        >
          Удалить вопрос
        </button>
      </div>

      {/* Question text */}
      <div className="mb-4">
        <label htmlFor="q-text" className="block text-sm font-medium text-gray-700 mb-1">
          Текст вопроса
        </label>
        <textarea
          id="q-text"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onBlur={handleTextBlur}
          maxLength={500}
          rows={3}
          placeholder="Введите вопрос..."
          className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent resize-none"
        />
        <div className="text-xs text-gray-400 text-right mt-1">
          {text.length}/500
        </div>
      </div>

      {/* Time limit and points */}
      <div className={`grid ${props.scoringRule !== 'none' ? 'grid-cols-2' : 'grid-cols-1'} gap-4 mb-5`}>
        <div>
          <label htmlFor="q-time" className="block text-sm font-medium text-gray-700 mb-1">
            Время (сек)
          </label>
          <input
            id="q-time"
            type="number"
            min={5}
            max={300}
            value={timeLimit}
            onChange={(e) => setTimeLimit(Number(e.target.value))}
            onBlur={handleTimeLimitBlur}
            className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
          />
        </div>
        {props.scoringRule !== 'none' && (
          <div>
            <label htmlFor="q-points" className="block text-sm font-medium text-gray-700 mb-1">
              Баллы
            </label>
            <input
              id="q-points"
              type="number"
              min={0}
              max={10000}
              value={points}
              onChange={(e) => setPoints(Number(e.target.value))}
              onBlur={handlePointsBlur}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            />
          </div>
        )}
      </div>

      {/* Options section */}
      {hasOptions && (
        <div>
          <h3 className="text-sm font-medium text-gray-700 mb-2">
            Варианты ответов
          </h3>
          <div className="space-y-2">
            {options.map((opt, index) => (
              <div key={index} className="flex items-center gap-2">
                <div
                  className={`w-2 h-8 rounded-full flex-shrink-0 ${OPTION_COLORS[index] ?? 'bg-gray-400'}`}
                />
                <input
                  type="text"
                  value={opt.text}
                  onChange={(e) => updateOption(index, 'text', e.target.value)}
                  onBlur={handleOptionBlur}
                  placeholder={`Вариант ${index + 1}`}
                  className="flex-1 px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                />
                <button
                  type="button"
                  onClick={() => handleCorrectToggle(index)}
                  title={opt.is_correct ? 'Правильный ответ' : 'Отметить как правильный'}
                  className={`flex-shrink-0 w-8 h-8 rounded-lg flex items-center justify-center transition-colors border ${
                    opt.is_correct
                      ? 'bg-emerald-100 border-emerald-300 text-emerald-700'
                      : 'bg-gray-50 border-gray-200 text-gray-400 hover:bg-gray-100'
                  }`}
                >
                  {question.type === 'single_choice' ? (
                    <span className="text-xs">{opt.is_correct ? '\u25C9' : '\u25CB'}</span>
                  ) : (
                    <span className="text-xs">{opt.is_correct ? '\u2713' : ''}</span>
                  )}
                </button>
                <button
                  type="button"
                  onClick={() => removeOption(index)}
                  disabled={options.length <= 2}
                  className="flex-shrink-0 w-8 h-8 rounded-lg flex items-center justify-center text-gray-400 hover:text-red-500 hover:bg-red-50 transition-colors disabled:opacity-30 disabled:pointer-events-none"
                  title="Удалить вариант"
                >
                  &times;
                </button>
              </div>
            ))}
          </div>
          {options.length < 6 && (
            <button
              type="button"
              onClick={addOption}
              className="mt-3 text-sm text-indigo-600 hover:text-indigo-800 font-medium transition-colors"
            >
              + Добавить вариант
            </button>
          )}
          {question.type === 'single_choice' && !options.some((o) => o.is_correct) && (
            <p className="mt-2 text-xs text-amber-600">
              Отметьте один правильный ответ
            </p>
          )}
          {question.type === 'multiple_choice' && !options.some((o) => o.is_correct) && (
            <p className="mt-2 text-xs text-amber-600">
              Отметьте хотя бы один правильный ответ
            </p>
          )}
        </div>
      )}

      {/* Info for types without options */}
      {!hasOptions && (
        <div className="rounded-lg bg-gray-50 border border-gray-200 p-4 text-sm text-gray-500">
          {question.type === 'open_text' && 'Участники введут свободный текстовый ответ.'}
          {question.type === 'word_cloud' && 'Участники предложат слова, которые сформируют облако.'}
          {question.type === 'brainstorm' && 'Участники будут добавлять идеи в общий список.'}
        </div>
      )}
    </div>
  )
}
