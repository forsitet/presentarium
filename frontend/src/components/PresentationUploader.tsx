import { useCallback, useRef, useState } from 'react'
import {
  uploadPresentation,
  waitForPresentationReady,
} from '../api/presentations'
import type { Presentation } from '../types'

interface PresentationUploaderProps {
  /** Called once the uploaded presentation reaches status="ready". */
  onUploaded: (presentation: Presentation) => void
  /** Optional cancel / close button (shown in modal contexts). */
  onCancel?: () => void
}

type Phase = 'idle' | 'uploading' | 'processing' | 'failed'

/**
 * Drag/drop or click-to-select .pptx uploader. Handles the full lifecycle:
 *
 *   1. idle       — waiting for user to pick a file
 *   2. uploading  — POSTing multipart/form-data, showing upload progress
 *   3. processing — polling GET /api/presentations/{id} until ready
 *   4. failed     — shows error + "Попробовать снова"
 *
 * Calls onUploaded with the final ready Presentation and resets to idle.
 */
export function PresentationUploader({ onUploaded, onCancel }: PresentationUploaderProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [phase, setPhase] = useState<Phase>('idle')
  const [dragActive, setDragActive] = useState(false)
  const [file, setFile] = useState<File | null>(null)
  const [title, setTitle] = useState('')
  const [uploadProgress, setUploadProgress] = useState(0)
  const [processingMsg, setProcessingMsg] = useState('Обработка презентации…')
  const [error, setError] = useState<string | null>(null)

  const reset = () => {
    setPhase('idle')
    setFile(null)
    setTitle('')
    setUploadProgress(0)
    setProcessingMsg('Обработка презентации…')
    setError(null)
    if (inputRef.current) inputRef.current.value = ''
  }

  const validate = (f: File): string | null => {
    const name = f.name.toLowerCase()
    if (!name.endsWith('.pptx')) {
      return 'Поддерживаются только файлы .pptx'
    }
    const maxBytes = 100 * 1024 * 1024 // 100 MB
    if (f.size > maxBytes) {
      return 'Файл слишком большой (максимум 100 МБ)'
    }
    return null
  }

  const pickFile = (f: File) => {
    const err = validate(f)
    if (err) {
      setError(err)
      setPhase('failed')
      return
    }
    setError(null)
    setFile(f)
    // Default the title from the filename (without extension).
    if (!title) {
      const base = f.name.replace(/\.pptx$/i, '')
      setTitle(base)
    }
  }

  const onDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setDragActive(false)
    const f = e.dataTransfer.files?.[0]
    if (f) pickFile(f)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [title])

  const handleUpload = async () => {
    if (!file) return
    setError(null)
    setPhase('uploading')
    setUploadProgress(0)
    try {
      const created = await uploadPresentation(file, title, (frac) => {
        setUploadProgress(frac)
      })
      setPhase('processing')
      setProcessingMsg('Презентация загружена, конвертируем слайды…')
      const ready = await waitForPresentationReady(created.id, (p) => {
        if (p.slide_count > 0) {
          setProcessingMsg(`Обработано слайдов: ${p.slide_count}`)
        }
      })
      onUploaded(ready)
      reset()
    } catch (err: unknown) {
      const axiosErr = err as { response?: { data?: { error?: string } }; message?: string }
      const msg =
        axiosErr?.response?.data?.error ||
        axiosErr?.message ||
        'Не удалось загрузить презентацию'
      setError(msg)
      setPhase('failed')
    }
  }

  // Failed state — show error + retry.
  if (phase === 'failed') {
    return (
      <div className="bg-white rounded-xl border border-red-200 p-5">
        <div className="flex items-start gap-3">
          <div className="w-10 h-10 rounded-full bg-red-50 flex items-center justify-center text-red-500 text-xl shrink-0">
            ⚠
          </div>
          <div className="flex-1">
            <h3 className="font-semibold text-gray-900">Ошибка загрузки</h3>
            <p className="mt-1 text-sm text-red-700">{error}</p>
            <div className="mt-4 flex gap-2">
              <button
                onClick={reset}
                className="px-4 py-2 text-sm rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 transition-colors font-medium"
              >
                Попробовать снова
              </button>
              {onCancel && (
                <button
                  onClick={onCancel}
                  className="px-4 py-2 text-sm rounded-lg border border-gray-200 text-gray-700 hover:bg-gray-50 transition-colors"
                >
                  Отмена
                </button>
              )}
            </div>
          </div>
        </div>
      </div>
    )
  }

  // Uploading / processing state — single spinner + progress bar.
  if (phase === 'uploading' || phase === 'processing') {
    const percent =
      phase === 'uploading' ? Math.round(uploadProgress * 100) : null
    return (
      <div className="bg-white rounded-xl border border-gray-100 p-5">
        <div className="flex items-center gap-3">
          <div className="w-5 h-5 rounded-full border-2 border-indigo-200 border-t-indigo-600 animate-spin shrink-0" />
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium text-gray-900 truncate">
              {file?.name}
            </p>
            <p className="text-xs text-gray-500 mt-0.5">
              {phase === 'uploading' ? `Загрузка… ${percent}%` : processingMsg}
            </p>
          </div>
        </div>
        <div className="mt-3 h-1.5 bg-gray-100 rounded-full overflow-hidden">
          <div
            className="h-full bg-indigo-500 transition-all duration-200"
            style={{
              width:
                phase === 'uploading'
                  ? `${percent}%`
                  : '100%',
            }}
          />
        </div>
      </div>
    )
  }

  // Idle — drop zone + optional selected file preview.
  return (
    <div>
      {!file && (
        <div
          onDragEnter={(e) => {
            e.preventDefault()
            setDragActive(true)
          }}
          onDragOver={(e) => {
            e.preventDefault()
          }}
          onDragLeave={(e) => {
            e.preventDefault()
            setDragActive(false)
          }}
          onDrop={onDrop}
          onClick={() => inputRef.current?.click()}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              inputRef.current?.click()
            }
          }}
          className={`border-2 border-dashed rounded-xl p-8 text-center cursor-pointer transition-colors ${
            dragActive
              ? 'border-indigo-400 bg-indigo-50'
              : 'border-gray-300 bg-gray-50 hover:border-indigo-300 hover:bg-indigo-50/50'
          }`}
        >
          <div className="w-12 h-12 mx-auto mb-3 rounded-full bg-white flex items-center justify-center text-2xl shadow-sm">
            📽
          </div>
          <p className="text-sm font-medium text-gray-700">
            Перетащите .pptx сюда или нажмите, чтобы выбрать
          </p>
          <p className="text-xs text-gray-400 mt-1">
            Максимум 100 МБ · конвертация может занять до минуты
          </p>
          <input
            ref={inputRef}
            type="file"
            accept=".pptx,application/vnd.openxmlformats-officedocument.presentationml.presentation"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) pickFile(f)
            }}
          />
        </div>
      )}

      {file && (
        <div className="bg-white rounded-xl border border-gray-100 p-4">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg bg-indigo-50 text-indigo-600 flex items-center justify-center text-xl shrink-0">
              📽
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-gray-900 truncate">
                {file.name}
              </p>
              <p className="text-xs text-gray-500 mt-0.5">
                {(file.size / 1024 / 1024).toFixed(1)} МБ
              </p>
            </div>
            <button
              onClick={() => setFile(null)}
              className="text-gray-400 hover:text-gray-600 text-sm"
              title="Убрать файл"
            >
              ✕
            </button>
          </div>

          <div className="mt-3">
            <label className="block text-xs font-medium text-gray-500 mb-1">
              Название (необязательно)
            </label>
            <input
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Например: Введение в React"
              className="w-full px-3 py-2 text-sm border border-gray-200 rounded-lg focus:outline-none focus:border-indigo-400 focus:ring-1 focus:ring-indigo-200"
            />
          </div>

          <div className="mt-4 flex gap-2 justify-end">
            {onCancel && (
              <button
                onClick={onCancel}
                className="px-4 py-2 text-sm rounded-lg border border-gray-200 text-gray-700 hover:bg-gray-50 transition-colors"
              >
                Отмена
              </button>
            )}
            <button
              onClick={handleUpload}
              className="px-4 py-2 text-sm rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 transition-colors font-medium"
            >
              Загрузить
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
