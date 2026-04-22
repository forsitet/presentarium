import { apiClient } from './client'
import type { Presentation, PresentationDetail } from '../types'

/** List the current user's presentations (most recent first). */
export async function listPresentations(): Promise<Presentation[]> {
  const res = await apiClient.get<Presentation[]>('/presentations')
  return res.data ?? []
}

/** Fetch a single presentation by ID, including its slide URLs. */
export async function getPresentation(id: string): Promise<PresentationDetail> {
  const res = await apiClient.get<PresentationDetail>(`/presentations/${id}`)
  return res.data
}

/** Delete a presentation. Succeeds with 204; axios returns undefined data. */
export async function deletePresentation(id: string): Promise<void> {
  await apiClient.delete(`/presentations/${id}`)
}

/**
 * Upload a .pptx file. The server responds 202 Accepted with a Presentation
 * row in status="processing" — conversion happens asynchronously in a worker
 * goroutine. Call waitForPresentationReady(id) next to block until conversion
 * finishes (or fails).
 *
 * `onUploadProgress` is invoked with a value in [0, 1] so the caller can draw
 * a progress bar during the network transfer.
 */
export async function uploadPresentation(
  file: File,
  title?: string,
  onUploadProgress?: (fraction: number) => void,
): Promise<Presentation> {
  const form = new FormData()
  form.append('file', file)
  if (title && title.trim()) {
    form.append('title', title.trim())
  }

  const res = await apiClient.post<Presentation>('/presentations', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
    onUploadProgress: (e) => {
      if (!onUploadProgress) return
      const total = e.total ?? 0
      if (total > 0) {
        onUploadProgress(Math.min(1, e.loaded / total))
      }
    },
  })
  return res.data
}

/**
 * Poll GET /api/presentations/{id} until its status leaves "processing".
 * Resolves with the ready PresentationDetail, or rejects if the server
 * reports status="failed" or the timeout expires.
 *
 * @param id            Presentation ID returned by uploadPresentation.
 * @param onProgress    Optional callback receiving each intermediate
 *                      Presentation row (e.g. to show "Обработка… N слайдов").
 * @param timeoutMs     Maximum wait in ms (default 3 minutes).
 * @param intervalMs    Poll interval in ms (default 1 second).
 */
export async function waitForPresentationReady(
  id: string,
  onProgress?: (p: Presentation) => void,
  timeoutMs = 3 * 60 * 1000,
  intervalMs = 1000,
): Promise<PresentationDetail> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const p = await getPresentation(id)
    onProgress?.(p)
    if (p.status === 'ready') {
      return p
    }
    if (p.status === 'failed') {
      throw new Error(p.error_message || 'Не удалось обработать презентацию')
    }
    await new Promise((resolve) => setTimeout(resolve, intervalMs))
  }
  throw new Error('Превышено время ожидания обработки презентации')
}
