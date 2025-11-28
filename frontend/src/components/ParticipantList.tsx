import type { Participant } from '../types'

interface Props {
  participants: Participant[]
}

export function ParticipantList({ participants }: Props) {
  if (participants.length === 0) {
    return (
      <div className="text-center py-8 text-gray-400">
        <div className="text-4xl mb-3">👥</div>
        <p className="text-lg">Ожидаем участников...</p>
        <p className="text-sm mt-1">Поделитесь кодом или QR-кодом</p>
      </div>
    )
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-3">
        <span className="text-sm text-gray-400 uppercase tracking-wide">Участники</span>
        <span className="bg-indigo-600 text-white text-xs font-bold px-2 py-0.5 rounded-full">
          {participants.length}
        </span>
      </div>
      <ul className="space-y-2 max-h-80 overflow-y-auto pr-1">
        {participants.map((p) => (
          <li
            key={p.id}
            className="flex items-center gap-3 bg-gray-700 rounded-lg px-3 py-2 animate-fade-in"
          >
            <div className="w-8 h-8 rounded-full bg-indigo-500 flex items-center justify-center text-white font-bold text-sm flex-shrink-0">
              {p.name.charAt(0).toUpperCase()}
            </div>
            <span className="text-white font-medium truncate">{p.name}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
