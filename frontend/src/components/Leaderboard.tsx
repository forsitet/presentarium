interface LBEntry {
  rank: number
  name: string
  score: number
}

interface LeaderboardProps {
  entries: LBEntry[]
  title?: string
}

const RANK_STYLES: Record<number, string> = {
  1: 'text-yellow-400',
  2: 'text-gray-300',
  3: 'text-amber-600',
}

const RANK_MEDALS: Record<number, string> = {
  1: '🥇',
  2: '🥈',
  3: '🥉',
}

export function Leaderboard({ entries, title = 'Лидерборд' }: LeaderboardProps) {
  if (!entries.length) {
    return (
      <div className="bg-gray-800 rounded-xl border border-gray-700 p-5">
        <h3 className="text-white font-bold text-lg mb-2">{title}</h3>
        <p className="text-gray-500 text-sm text-center py-4">Нет данных</p>
      </div>
    )
  }

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700 p-5">
      <h3 className="text-white font-bold text-lg mb-4">{title}</h3>
      <div className="space-y-2">
        {entries.map((entry, idx) => (
          <div
            key={entry.rank}
            className="flex items-center gap-3 bg-gray-700/50 rounded-lg px-4 py-2.5 animate-slide-stagger"
            style={{ animationDelay: `${idx * 80}ms` }}
          >
            <span
              className={`text-xl font-black w-7 text-center ${RANK_STYLES[entry.rank] ?? 'text-gray-500'}`}
            >
              {RANK_MEDALS[entry.rank] ?? entry.rank}
            </span>
            <span className="flex-1 text-white font-medium truncate">{entry.name}</span>
            <span className="text-indigo-300 font-bold tabular-nums">{entry.score} pts</span>
          </div>
        ))}
      </div>
    </div>
  )
}
