import { useState, useCallback } from 'react'
import { apiClient } from '../api/client'

/**
 * Converts a DOM element that contains an SVG (e.g. a Recharts container)
 * into a base64-encoded PNG string using the browser's Canvas API.
 * Works without any external library because Recharts uses inline SVG styles.
 */
async function svgContainerToBase64(container: Element): Promise<string> {
  const svg = container.querySelector('svg')
  if (!svg) return ''

  const svgData = new XMLSerializer().serializeToString(svg)
  const svgBlob = new Blob([svgData], { type: 'image/svg+xml;charset=utf-8' })
  const url = URL.createObjectURL(svgBlob)

  return new Promise<string>((resolve) => {
    const img = new Image()
    img.onload = () => {
      const canvas = document.createElement('canvas')
      canvas.width = img.width || 560
      canvas.height = img.height || 256
      const ctx = canvas.getContext('2d')
      if (ctx) {
        ctx.fillStyle = '#111827' // match dark bg (gray-900)
        ctx.fillRect(0, 0, canvas.width, canvas.height)
        ctx.drawImage(img, 0, 0)
      }
      URL.revokeObjectURL(url)
      // Remove data:image/png;base64, prefix
      resolve(canvas.toDataURL('image/png').replace(/^data:image\/png;base64,/, ''))
    }
    img.onerror = () => {
      URL.revokeObjectURL(url)
      resolve('')
    }
    img.src = url
  })
}

/** Triggers a file download from a Blob in the browser. */
function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

export type ExportStatus = 'idle' | 'loading' | 'success' | 'error'

export function useExport(sessionId: string | undefined) {
  const [csvStatus, setCsvStatus] = useState<ExportStatus>('idle')
  const [pdfStatus, setPdfStatus] = useState<ExportStatus>('idle')
  const [errorMessage, setErrorMessage] = useState<string | null>(null)

  const dismissError = useCallback(() => setErrorMessage(null), [])

  const exportCsv = useCallback(async () => {
    if (!sessionId) return
    setCsvStatus('loading')
    setErrorMessage(null)
    try {
      const response = await apiClient.get(`/sessions/${sessionId}/export/csv`, {
        responseType: 'blob',
      })
      const contentDisposition = response.headers['content-disposition'] as string | undefined
      let filename = `session_${sessionId}.csv`
      if (contentDisposition) {
        const match = contentDisposition.match(/filename=(.+)/)
        if (match) filename = match[1].replace(/"/g, '')
      }
      downloadBlob(response.data as Blob, filename)
      setCsvStatus('success')
      setTimeout(() => setCsvStatus('idle'), 2000)
    } catch {
      setCsvStatus('error')
      setErrorMessage('Не удалось скачать CSV. Попробуйте ещё раз.')
      setTimeout(() => setCsvStatus('idle'), 3000)
    }
  }, [sessionId])

  const exportPdf = useCallback(async (chartContainerSelector = '[data-chart-index]') => {
    if (!sessionId) return
    setPdfStatus('loading')
    setErrorMessage(null)
    try {
      // Capture all chart containers as base64 images
      const chartContainers = Array.from(document.querySelectorAll(chartContainerSelector))
      const charts: { question_index: number; image: string }[] = []

      for (const container of chartContainers) {
        const indexAttr = container.getAttribute('data-chart-index')
        if (indexAttr === null) continue
        const questionIndex = parseInt(indexAttr, 10)
        const image = await svgContainerToBase64(container)
        if (image) {
          charts.push({ question_index: questionIndex, image })
        }
      }

      const response = await apiClient.post(
        `/sessions/${sessionId}/export/pdf`,
        { charts },
        { responseType: 'blob' },
      )

      const contentDisposition = response.headers['content-disposition'] as string | undefined
      let filename = `session_${sessionId}.pdf`
      if (contentDisposition) {
        const match = contentDisposition.match(/filename=(.+)/)
        if (match) filename = match[1].replace(/"/g, '')
      }
      downloadBlob(response.data as Blob, filename)
      setPdfStatus('success')
      setTimeout(() => setPdfStatus('idle'), 2000)
    } catch {
      setPdfStatus('error')
      setErrorMessage('Не удалось сгенерировать PDF. Попробуйте ещё раз.')
      setTimeout(() => setPdfStatus('idle'), 3000)
    }
  }, [sessionId])

  return { exportCsv, exportPdf, csvStatus, pdfStatus, errorMessage, dismissError }
}
