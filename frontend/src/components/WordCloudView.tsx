import { useMemo, useRef, useState, useLayoutEffect } from 'react'

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

// A small palette of near-identical indigo shades. Popularity is communicated
// by font size; the colour variation just keeps neighbouring same-size phrases
// from blurring into one another.
const PALETTE = ['#3b5bb8', '#4f46e5', '#4338ca', '#4c5bc6', '#6366f1']

// stableHash gives each phrase a deterministic colour index — same phrase
// always keeps the same shade as new submissions arrive.
function stableHash(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) | 0
  }
  return Math.abs(h)
}

interface LaidOutWord {
  text: string
  count: number
  fontSize: number
  color: string
}

/**
 * Compute font size + colour for each phrase. Sizes are sqrt-scaled between
 * `min` and `max` so the difference between count=1 and count=2 is visible
 * but doesn't crowd out the rest of the cloud at high counts.
 */
function layout(words: WordEntry[], min: number, max: number): LaidOutWord[] {
  if (words.length === 0) return []
  const counts = words.map((w) => w.count)
  const lo = Math.min(...counts)
  const hi = Math.max(...counts)
  const range = Math.max(1, hi - lo)

  return words
    .slice()
    .sort((a, b) => b.count - a.count) // largest first → React layout puts them near the centre
    .map((w) => {
      // sqrt-scale so a single popular phrase doesn't dominate the whole box.
      const t = Math.sqrt((w.count - lo) / range) // 0..1
      const fontSize = min + (max - min) * t
      return {
        text: w.text,
        count: w.count,
        fontSize,
        color: PALETTE[stableHash(w.text) % PALETTE.length],
      }
    })
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
  const [fontRange, setFontRange] = useState<[number, number]>(
    fullScreen ? [22, 88] : [16, 56],
  )

  // Pick a font-size range that scales with the actual cloud area so phrases
  // fill the box on a 4K screen but don't overflow on a phone.
  useLayoutEffect(() => {
    if (!containerRef.current) return
    const measure = () => {
      const el = containerRef.current
      if (!el) return
      const { width, height } = el.getBoundingClientRect()
      if (width <= 0 || height <= 0) return
      // Heuristic: the largest phrase should be roughly 1/4 of the smaller
      // dimension of the box. Clamp to keep readability everywhere.
      const small = Math.min(width, height)
      const max = Math.max(28, Math.min(140, Math.floor(small / 4)))
      const min = Math.max(14, Math.floor(max / 4))
      setFontRange([min, max])
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(containerRef.current)
    return () => ro.disconnect()
  }, [fullScreen])

  const visibleWords = useMemo(
    () =>
      words
        .filter((w) => !hiddenWords.has(w.text) && w.text.trim().length > 0),
    [words, hiddenWords],
  )

  const laidOut = useMemo(
    () => layout(visibleWords, fontRange[0], fontRange[1]),
    [visibleWords, fontRange],
  )

  const cloudHeight = fullScreen ? 'flex-1 min-h-[300px]' : 'h-64'

  return (
    <div className={`flex flex-col gap-4 ${fullScreen ? 'h-full' : ''}`}>
      {/* Word cloud — Mentimeter-style: light surface, single hue family,
          horizontal-only, packed via flex-wrap so multi-word phrases like
          "искусственный интеллект" stay on one line. */}
      <div
        ref={containerRef}
        className={`${cloudHeight} w-full rounded-xl bg-slate-50 border border-slate-200 overflow-hidden`}
      >
        {words.length === 0 ? (
          <div className="h-full flex items-center justify-center text-slate-500 text-sm">
            Ждём ответов участников...
          </div>
        ) : laidOut.length === 0 ? (
          <div className="h-full flex items-center justify-center text-slate-500 text-sm">
            Все слова скрыты
          </div>
        ) : (
          <div className="h-full w-full flex flex-wrap items-center justify-center content-center gap-x-6 gap-y-2 px-6 py-4 overflow-hidden">
            {laidOut.map((w) => (
              <span
                key={w.text}
                className="leading-tight font-bold whitespace-nowrap transition-all duration-500"
                style={{
                  fontSize: `${w.fontSize}px`,
                  color: w.color,
                }}
                title={`${w.text} · ${w.count}`}
              >
                {w.text}
              </span>
            ))}
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
