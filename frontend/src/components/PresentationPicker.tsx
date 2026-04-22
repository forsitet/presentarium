import { useEffect, useState } from 'react'
import { listPresentations } from '../api/presentations'
import { PresentationUploader } from './PresentationUploader'
import type { Presentation } from '../types'

interface PresentationPickerProps {
  /** Called when the host picks a ready presentation. */
  onPick: (p: Presentation) => void
  /** Called when the host closes the modal without picking anything. */
  onClose: () => void
}

/**
 * Modal dialog showing the host's ready presentations plus an inline uploader.
 * Used from HostSessionPage when the host clicks "Показать презентацию".
 */
export function PresentationPicker({ onPick, onClose }: PresentationPickerProps) {
  const [presentations, setPresentations] = useState<Presentation[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showUploader, setShowUploader] = useState(false)

  const load = async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await listPresentations()
      setPresentations(data)
    } catch {
      setError('Не удалось загрузить список презентаций')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const handleUploaded = (p: Presentation) => {
    // Auto-pick once upload+conversion finishes — typical flow is "upload and
    // immediately show". The host can always close and re-pick something else.
    onPick(p)
  }

  const formatDate = (iso: string) =>
    new Date(iso).toLocaleDateString('ru-RU', {
      day: '2-digit',
      month: '2-digit',
      year: 'numeric',
    })

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/40" onClick={onClose} />
      <div className="relative bg-white rounded-2xl shadow-xl w-full max-w-2xl max-h-[85vh] flex flex-col">
        {/* Header */}
        <div className="px-6 py-4 border-b border-gray-100 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-gray-900">
            {showUploader ? 'Загрузить презентацию' : 'Выбрать презентацию'}
          </h2>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-600 text-xl leading-none"
            aria-label="Закрыть"
          >
            ×
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-6 py-4">
          {showUploader ? (
            <PresentationUploader
              onUploaded={handleUploaded}
              onCancel={() => setShowUploader(false)}
            />
          ) : (
            <>
              {loading && (
                <div className="space-y-2 animate-pulse">
                  {[1, 2, 3].map((i) => (
                    <div key={i} className="h-16 bg-gray-100 rounded-lg" />
                  ))}
                </div>
              )}

              {!loading && error && (
                <div className="p-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700">
                  {error}
                </div>
              )}

              {!loading && !error && presentations.length === 0 && (
                <div className="py-10 text-center">
                  <div className="w-14 h-14 mx-auto mb-3 rounded-full bg-indigo-50 flex items-center justify-center text-2xl">
                    📽
                  </div>
                  <p className="text-sm text-gray-600">
                    У вас ещё нет загруженных презентаций
                  </p>
                  <button
                    onClick={() => setShowUploader(true)}
                    className="mt-4 px-4 py-2 text-sm rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 transition-colors font-medium"
                  >
                    Загрузить первую
                  </button>
                </div>
              )}

              {!loading && !error && presentations.length > 0 && (
                <div className="space-y-2">
                  {presentations.map((p) => {
                    const isReady = p.status === 'ready'
                    const isFailed = p.status === 'failed'
                    const isProcessing = p.status === 'processing'
                    return (
                      <button
                        key={p.id}
                        disabled={!isReady}
                        onClick={() => isReady && onPick(p)}
                        className={`w-full text-left rounded-lg border p-3 flex items-center gap-3 transition-colors ${
                          isReady
                            ? 'border-gray-200 hover:border-indigo-300 hover:bg-indigo-50/40 cursor-pointer'
                            : 'border-gray-100 bg-gray-50 cursor-not-allowed'
                        }`}
                      >
                        <div className="w-10 h-10 rounded-lg bg-indigo-50 text-indigo-600 flex items-center justify-center text-xl shrink-0">
                          📽
                        </div>
                        <div className="flex-1 min-w-0">
                          <p className="text-sm font-medium text-gray-900 truncate">
                            {p.title}
                          </p>
                          <p className="text-xs text-gray-500 mt-0.5">
                            {isReady && `${p.slide_count} слайд(ов) · ${formatDate(p.created_at)}`}
                            {isProcessing && 'Обрабатывается…'}
                            {isFailed && (p.error_message || 'Ошибка конвертации')}
                          </p>
                        </div>
                        {isReady && <span className="text-gray-300 text-lg">›</span>}
                        {isProcessing && (
                          <div className="w-4 h-4 rounded-full border-2 border-indigo-200 border-t-indigo-500 animate-spin" />
                        )}
                        {isFailed && <span className="text-red-500 text-sm">⚠</span>}
                      </button>
                    )
                  })}
                </div>
              )}
            </>
          )}
        </div>

        {/* Footer */}
        {!showUploader && (
          <div className="px-6 py-3 border-t border-gray-100 flex justify-between items-center">
            <button
              onClick={() => setShowUploader(true)}
              className="text-sm text-indigo-600 hover:text-indigo-700 font-medium"
            >
              + Загрузить новую
            </button>
            <button
              onClick={onClose}
              className="px-4 py-1.5 text-sm rounded-lg border border-gray-200 text-gray-700 hover:bg-gray-50 transition-colors"
            >
              Отмена
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
