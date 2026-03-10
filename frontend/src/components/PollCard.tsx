import type { Poll } from '../types'

interface PollCardProps {
  poll: Poll
  onEdit: (id: string) => void
  onLaunch: (id: string) => void
  onDelete: (id: string) => void
  onCopy: (id: string) => void
}

export function PollCard({ poll, onEdit, onLaunch, onDelete, onCopy }: PollCardProps) {
  const date = new Date(poll.created_at).toLocaleDateString('ru-RU', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  })

  return (
    <div className="bg-white rounded-xl shadow-sm border border-gray-100 p-5 flex flex-col gap-3 hover:shadow-md transition-shadow">
      <div className="flex-1">
        <h3 className="text-lg font-semibold text-gray-900 leading-snug">{poll.title}</h3>
        {poll.description && (
          <p className="mt-1 text-sm text-gray-500 line-clamp-2">{poll.description}</p>
        )}
        <p className="mt-2 text-xs text-gray-400">{date}</p>
      </div>

      <div className="flex flex-wrap gap-2 pt-1 border-t border-gray-100">
        <button
          onClick={() => onEdit(poll.id)}
          className="flex-1 min-w-[80px] px-3 py-1.5 text-sm rounded-lg border border-gray-200 text-gray-700 hover:bg-gray-50 transition-colors"
        >
          Редактировать
        </button>
        <button
          onClick={() => onLaunch(poll.id)}
          className="flex-1 min-w-[80px] px-3 py-1.5 text-sm rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 transition-colors font-medium"
        >
          Запустить
        </button>
        <button
          onClick={() => onCopy(poll.id)}
          className="px-3 py-1.5 text-sm rounded-lg border border-gray-200 text-gray-700 hover:bg-gray-50 transition-colors"
          title="Копировать"
        >
          Копировать
        </button>
        <button
          onClick={() => onDelete(poll.id)}
          className="px-3 py-1.5 text-sm rounded-lg border border-red-200 text-red-600 hover:bg-red-50 transition-colors"
          title="Удалить"
        >
          Удалить
        </button>
      </div>
    </div>
  )
}
