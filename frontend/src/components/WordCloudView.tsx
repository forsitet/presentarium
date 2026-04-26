import { Component, type ReactNode, useRef, useState, useLayoutEffect } from 'react'
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
  /** When true, the cloud fills all available vertical space. */
  fullScreen?: boolean
}

// Mentimeter-style cloud: a single indigo hue across all phrases (popularity
// is communicated entirely by font size), a tight layout that fills the box,
// and a strictly horizontal orientation. We pass a few near-identical shades
// of indigo so adjacent same-size phrases can be told apart, but the overall
// impression is monochrome.
const WC_COLORS = ['#3b5bb8', '#4f46e5', '#4338ca', '#4c5bc6']

const WC_OPTIONS = {
  fontSizes: [14, 72] as [number, number],
  rotations: 0,
  rotationAngles: [0, 0] as [number, number],
  fontFamily: 'Inter, system-ui, sans-serif',
  fontWeight: '600' as const,
  deterministic: true,
  padding: 2,
  scale: 'sqrt' as const, // softens the size jump between rare and popular phrases
  spiral: 'archimedean' as const,
  colors: WC_COLORS,
  enableTooltip: true,
  tooltipOptions: {},
  transitionDuration: 600, // smooth re-layout when new answers arrive
}

const WC_OPTIONS_FULL = {
  ...WC_OPTIONS,
  fontSizes: [20, 120] as [number, number],
  padding: 3,
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
          className="px-3 py-1 rounded-full font-semibold bg-indigo-50 text-indigo-700 border border-indigo-200"
          style={{ fontSize: `${Math.min(12 + w.value * 2, 28)}px` }}
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
  fullScreen = false,
}: WordCloudViewProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [cloudSize, setCloudSize] = useState<[number, number]>([560, 256])

  // Measure the container once it mounts (and on resize) so the cloud fills it.
  useLayoutEffect(() => {
    if (!fullScreen || !containerRef.current) return
    const measure = () => {
      if (containerRef.current) {
        const { width, height } = containerRef.current.getBoundingClientRect()
        if (width > 0 && height > 0) {
          setCloudSize([Math.floor(width), Math.floor(height)])
        }
      }
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(containerRef.current)
    return () => ro.disconnect()
  }, [fullScreen])

  const visibleWords = words
    .filter((w) => !hiddenWords.has(w.text))
    .map((w) => ({ text: w.text, value: Math.max(w.count, 1) })) // ensure positive values
    .filter((w) => w.text.trim().length > 0) // filter out empty strings

  if (words.length === 0) {
    return (
      <div
        className={`flex items-center justify-center rounded-xl bg-slate-50 border border-slate-200 text-slate-500 text-sm ${
          fullScreen ? 'h-full' : 'h-48'
        }`}
      >
        Ждём ответов участников...
      </div>
    )
  }

  const cloudHeight = fullScreen ? 'flex-1 min-h-[300px]' : 'h-64'
  const options = fullScreen ? WC_OPTIONS_FULL : WC_OPTIONS
  const size: [number, number] = fullScreen ? cloudSize : [560, 256]

  return (
    <div className={`flex flex-col gap-4 ${fullScreen ? 'h-full' : ''}`}>
      {/* Word cloud visualization — Mentimeter-style: light surface, single
          indigo hue, all phrases horizontal so multi-word answers stay
          readable end-to-end. */}
      <div
        ref={containerRef}
        className={`${cloudHeight} w-full bg-slate-50 border border-slate-200 rounded-xl overflow-hidden`}
      >
        {visibleWords.length > 0 ? (
          <WordCloudErrorBoundary fallback={<WordChipsFallback words={visibleWords} />}>
            <ReactWordcloud
              words={visibleWords}
              options={options}
              size={size}
            />
          </WordCloudErrorBoundary>
        ) : (
          <div className="h-full flex items-center justify-center text-slate-500 text-sm">
            Все слова скрыты
          </div>
        )}
      </div>

      {/* Word list with moderation (hide/show) buttons */}
      {showModerationPanel && words.length > 0 && (
        <div className="flex-shrink-0">
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
