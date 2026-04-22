import { useEffect } from 'react'
import type { WSPresentationSlide } from '../types'

interface SlideViewerProps {
  title: string
  slides: WSPresentationSlide[]
  currentPosition: number // 1-indexed
  /** Host-only controls. When omitted/undefined, viewer is read-only (participant). */
  onPrev?: () => void
  onNext?: () => void
  onClose?: () => void
  onJump?: (position: number) => void
}

/**
 * Full-screen slide viewer used both in HostSessionPage (with prev/next/close
 * controls + thumbnail strip) and ParticipantSessionPage (read-only overlay).
 *
 * Keyboard shortcuts (host mode only):
 *   ← / PageUp   → prev
 *   → / PageDown → next
 *   Esc          → close
 */
export function SlideViewer({
  title,
  slides,
  currentPosition,
  onPrev,
  onNext,
  onClose,
  onJump,
}: SlideViewerProps) {
  const isHost = Boolean(onPrev && onNext && onClose)

  // Keyboard nav for host mode.
  useEffect(() => {
    if (!isHost) return
    const onKey = (e: KeyboardEvent) => {
      // Don't hijack keys when focus is in a text input or textarea.
      const tag = (e.target as HTMLElement)?.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA') return
      if (e.key === 'ArrowLeft' || e.key === 'PageUp') {
        e.preventDefault()
        onPrev?.()
      } else if (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ') {
        e.preventDefault()
        onNext?.()
      } else if (e.key === 'Escape') {
        e.preventDefault()
        onClose?.()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [isHost, onPrev, onNext, onClose])

  const current = slides.find((s) => s.position === currentPosition) ?? slides[0]
  const total = slides.length

  if (!current) {
    return null
  }

  const atStart = currentPosition <= 1
  const atEnd = currentPosition >= total

  return (
    <div className="fixed inset-0 z-40 bg-gray-900 flex flex-col">
      {/* Top bar */}
      <div className="shrink-0 flex items-center justify-between px-4 py-2 bg-black/40 text-white text-sm">
        <div className="min-w-0 flex-1 truncate">
          <span className="font-medium">{title}</span>
          <span className="text-gray-300 ml-3">
            Слайд {currentPosition} из {total}
          </span>
        </div>
        {isHost && (
          <button
            onClick={onClose}
            className="ml-3 px-3 py-1 rounded-md bg-white/10 hover:bg-white/20 transition-colors"
            title="Закрыть (Esc)"
          >
            ✕ Закрыть
          </button>
        )}
      </div>

      {/* Main slide area */}
      <div className="flex-1 flex items-center justify-center p-4 relative min-h-0">
        <img
          key={current.id}
          src={current.image_url}
          alt={`Слайд ${current.position}`}
          className="max-w-full max-h-full object-contain shadow-2xl"
        />

        {/* Prev / next arrows (host only) */}
        {isHost && (
          <>
            <button
              onClick={onPrev}
              disabled={atStart}
              className="absolute left-4 top-1/2 -translate-y-1/2 w-12 h-12 rounded-full bg-white/10 hover:bg-white/25 text-white text-2xl disabled:opacity-30 disabled:cursor-not-allowed transition-colors backdrop-blur"
              title="Предыдущий (←)"
            >
              ‹
            </button>
            <button
              onClick={onNext}
              disabled={atEnd}
              className="absolute right-4 top-1/2 -translate-y-1/2 w-12 h-12 rounded-full bg-white/10 hover:bg-white/25 text-white text-2xl disabled:opacity-30 disabled:cursor-not-allowed transition-colors backdrop-blur"
              title="Следующий (→)"
            >
              ›
            </button>
          </>
        )}
      </div>

      {/* Thumbnail strip (host only) */}
      {isHost && total > 1 && (
        <div className="shrink-0 bg-black/50 px-3 py-2 overflow-x-auto">
          <div className="flex gap-2">
            {slides.map((s) => {
              const active = s.position === currentPosition
              return (
                <button
                  key={s.id}
                  onClick={() => onJump?.(s.position)}
                  className={`relative shrink-0 rounded-md overflow-hidden transition-all ${
                    active
                      ? 'ring-2 ring-indigo-400 scale-[1.04]'
                      : 'ring-1 ring-white/10 hover:ring-white/30'
                  }`}
                  style={{ width: 96, height: 56 }}
                  title={`Слайд ${s.position}`}
                >
                  <img
                    src={s.image_url}
                    alt=""
                    className="w-full h-full object-cover"
                    loading="lazy"
                  />
                  <span
                    className={`absolute bottom-0 right-0 px-1 text-[10px] font-semibold rounded-tl ${
                      active
                        ? 'bg-indigo-500 text-white'
                        : 'bg-black/60 text-white/80'
                    }`}
                  >
                    {s.position}
                  </span>
                </button>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
