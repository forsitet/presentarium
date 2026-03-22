import { Component, type ReactNode } from 'react'
import ReactWordcloud from 'react-wordcloud'

interface WordEntry {
  text: string
  count: number
}

interface WordCloudViewProps {
  words: WordEntry[]
  hiddenWords?: Set<string>
  onHideWord?: (word: string) => void
  showModerationPanel?: boolean
}

const WC_OPTIONS = {
  fontSizes: [14, 64] as [number, number],
  rotations: 0,
  fontFamily: 'Inter, system-ui, sans-serif',
  deterministic: true,
  padding: 4,
  colors: ['#818cf8', '#a78bfa', '#f472b6', '#fb923c', '#34d399', '#60a5fa', '#facc15'],
  enableTooltip: true,
  tooltipOptions: {},
}

/* ---------- Error Boundary for react-wordcloud ---------- */

interface EBProps {
  fallback: ReactNode
  children: ReactNode
}
interface EBState {
  hasError: boolean
}

class WordCloudErrorBoundary extends Component<EBProps, EBState> {
  state: EBState = { hasError: false }

  static getDerivedStateFromError(): EBState {
    return { hasError: true }
  }

  render() {
    if (this.state.hasError) return this.props.fallback
    return this.props.children
  }
}

/* ---------- Chip-based fallback when cloud crashes ---------- */

function WordChipsFallback({ words }: { words: { text: string; value: number }[] }) {
  return (
    <div className="h-full flex flex-wrap items-center justify-center gap-2 p-4 overflow-y-auto">
      {words.map((w) => (
        <span
          key={w.text}
          className="px-3 py-1 rounded-full text-sm font-medium bg-indigo-600/30 text-indigo-200 border border-indigo-500/40"
          style={{ fontSize: `${Math.min(12 + w.value * 2, 24)}px` }}
        >
          {w.text}
          <span className="ml-1 opacity-60 text-xs">{w.value}</span>
        </span>
      ))}
    </div>
  )
}

/* ---------- Main component ---------- */

export function WordCloudView({
  words,
  hiddenWords = new Set(),
  onHideWord,
  showModerationPanel = false,
}: WordCloudViewProps) {
  const visibleWords = words
    .filter((w) => !hiddenWords.has(w.text))
    .map((w) => ({ text: w.text, value: Math.max(w.count, 1) })) // ensure positive values
    .filter((w) => w.text.trim().length > 0) // filter out empty strings

  if (words.length === 0) {
    return (
      <div className="flex items-center justify-center h-48 text-gray-500 text-sm">
        Ждём ответов участников...
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {/* Word cloud visualization */}
      <div className="h-64 w-full bg-gray-900 rounded-xl overflow-hidden">
        {visibleWords.length > 0 ? (
          <WordCloudErrorBoundary fallback={<WordChipsFallback words={visibleWords} />}>
            <ReactWordcloud
              words={visibleWords}
              options={WC_OPTIONS}
              size={[560, 256]}
            />
          </WordCloudErrorBoundary>
        ) : (
          <div className="h-full flex items-center justify-center text-gray-500 text-sm">
            Все слова скрыты
          </div>
        )}
      </div>

      {/* Word list with moderation (hide/show) buttons */}
      {showModerationPanel && words.length > 0 && (
        <div>
          <p className="text-gray-400 text-xs uppercase tracking-wide mb-2">
            Слова ({words.length})
          </p>
          <div className="flex flex-wrap gap-2 max-h-36 overflow-y-auto">
            {words
              .slice()
              .sort((a, b) => b.count - a.count)
              .map((w) => {
                const isHidden = hiddenWords.has(w.text)
                return (
                  <div
                    key={w.text}
                    className={`flex items-center gap-1 px-2 py-1 rounded-full text-xs border transition-colors ${
                      isHidden
                        ? 'border-gray-700 text-gray-600 bg-gray-800'
                        : 'border-gray-600 text-gray-300 bg-gray-700'
                    }`}
                  >
                    <span>{w.text}</span>
                    <span className="text-gray-500 font-mono ml-1">{w.count}</span>
                    {onHideWord && (
                      <button
                        onClick={() => onHideWord(w.text)}
                        className={`ml-1 transition-colors leading-none ${
                          isHidden
                            ? 'text-gray-500 hover:text-green-400'
                            : 'text-gray-400 hover:text-red-400'
                        }`}
                        title={isHidden ? 'Показать слово' : 'Скрыть слово'}
                      >
                        {isHidden ? '👁' : '✕'}
                      </button>
                    )}
                  </div>
                )
              })}
          </div>
        </div>
      )}
    </div>
  )
}
