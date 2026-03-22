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
    <div className="bg-white rounded-xl shadow-sm border border-gray-100 p-5 flex flex-col gap-3 hover:shadow-md transition-shadow overflow-hidden">
      <div className="flex-1 min-w-0">
        <h3 className="text-lg font-semibold text-gray-900 leading-snug truncate">{poll.title}</h3>
        {poll.description && (
          <p className="mt-1 text-sm text-gray-500 line-clamp-2">{poll.description}</p>
        )}
        <p className="mt-2 text-xs text-gray-400">{date}</p>
      </div>

      <div className="flex gap-2 pt-1 border-t border-gray-100">
        <button
          onClick={() => onEdit(poll.id)}
          className="flex-1 px-3 py-1.5 text-sm text-center whitespace-nowrap rounded-lg border border-gray-200 text-gray-700 hover:bg-gray-50 transition-colors"
        >
          Редакт.
        </button>
        <button
          onClick={() => onLaunch(poll.id)}
          className="flex-1 px-3 py-1.5 text-sm text-center whitespace-nowrap rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 transition-colors font-medium"
        >
          Запустить
        </button>
        <button
          onClick={() => onCopy(poll.id)}
          className="px-2 py-1.5 text-sm rounded-lg border border-gray-200 text-gray-500 hover:bg-gray-50 transition-colors"
          title="Копировать"
        >
          <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}><rect x="9" y="9" width="13" height="13" rx="2" /><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" /></svg>
        </button>
        <button
          onClick={() => onDelete(poll.id)}
          className="px-2 py-1.5 text-sm rounded-lg border border-red-200 text-red-500 hover:bg-red-50 transition-colors"
          title="Удалить"
        >
          <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
        </button>
      </div>
    </div>
  )
}
