import { useEffect, useState, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { QRCodeSVG } from 'qrcode.react'
import { useAuthStore } from '../stores/authStore'
import { useSessionStore } from '../stores/sessionStore'
import { socket } from '../ws/socket'
import { getRoomParticipants, changeRoomState } from '../api/polls'
import { ParticipantList } from '../components/ParticipantList'
import type { Participant } from '../types'

export function HostSessionPage() {
  const { code } = useParams<{ code: string }>()
  const navigate = useNavigate()
  const { accessToken } = useAuthStore()
  const { participants, setParticipants, addParticipant, removeParticipant, setRoom, reset } =
    useSessionStore()

  const [copied, setCopied] = useState(false)
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const joinUrl = `${window.location.origin}/join/${code}`

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(joinUrl).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [joinUrl])

  const handleStart = useCallback(async () => {
    if (!code || participants.length === 0) return
    setStarting(true)
    try {
      await changeRoomState(code, 'start')
      // State transition will be driven by WS room_state_changed messages (TASK-020)
    } catch {
      setError('Не удалось начать опрос. Попробуйте снова.')
      setStarting(false)
    }
  }, [code, participants.length])

  useEffect(() => {
    if (!code) return

    setRoom(code)

    // Load initial participants
    getRoomParticipants(code)
      .then(setParticipants)
      .catch(() => {
        // room may have 0 participants — that's fine
      })

    // Connect as organizer (with JWT token)
    socket.connect(code, accessToken ?? undefined)

    const handleParticipantJoined = (data: unknown) => {
      const p = data as Participant
      addParticipant(p)
    }

    const handleParticipantLeft = (data: unknown) => {
      const d = data as { id: string }
      removeParticipant(d.id)
    }

    socket.on('participant_joined', handleParticipantJoined)
    socket.on('participant_left', handleParticipantLeft)

    return () => {
      socket.off('participant_joined', handleParticipantJoined)
      socket.off('participant_left', handleParticipantLeft)
      socket.disconnect()
      reset()
    }
  }, [code, accessToken, setRoom, setParticipants, addParticipant, removeParticipant, reset])

  if (!code) {
    return (
      <div className="min-h-screen bg-gray-900 flex items-center justify-center text-white">
        <p>Код комнаты не найден</p>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      {/* Header */}
      <div className="bg-gray-800 border-b border-gray-700 px-6 py-4 flex items-center justify-between">
        <button
          onClick={() => navigate('/dashboard')}
          className="text-gray-400 hover:text-white transition-colors flex items-center gap-2 text-sm"
        >
          ← Дашборд
        </button>
        <span className="text-gray-400 text-sm">Экран ожидания</span>
      </div>

      <div className="max-w-5xl mx-auto px-6 py-10 grid grid-cols-1 lg:grid-cols-2 gap-10">
        {/* Left: room code + QR */}
        <div className="flex flex-col items-center">
          <p className="text-gray-400 text-sm uppercase tracking-widest mb-2">Код комнаты</p>
          <div className="text-8xl font-black tracking-widest text-white mb-6 font-mono">
            {code}
          </div>

          {/* QR Code */}
          <div className="bg-white p-4 rounded-2xl shadow-lg mb-6">
            <QRCodeSVG value={joinUrl} size={200} />
          </div>

          {/* Copy link */}
          <button
            onClick={handleCopy}
            className="flex items-center gap-2 bg-gray-700 hover:bg-gray-600 text-white px-5 py-3 rounded-xl transition-colors text-sm font-medium w-full max-w-xs justify-center"
          >
            {copied ? (
              <>
                <span>✓</span>
                <span>Скопировано!</span>
              </>
            ) : (
              <>
                <span>🔗</span>
                <span>Скопировать ссылку</span>
              </>
            )}
          </button>

          <p className="text-gray-500 text-xs mt-3 text-center break-all max-w-xs">{joinUrl}</p>
        </div>

        {/* Right: participant list + start button */}
        <div className="flex flex-col">
          <div className="bg-gray-800 rounded-2xl p-6 flex-1 border border-gray-700">
            <ParticipantList participants={participants} />
          </div>

          {error && (
            <div className="mt-4 bg-red-900/40 border border-red-700 text-red-300 px-4 py-3 rounded-xl text-sm">
              {error}
            </div>
          )}

          <button
            onClick={handleStart}
            disabled={participants.length === 0 || starting}
            className={`mt-6 w-full py-4 rounded-2xl text-lg font-bold transition-all ${
              participants.length === 0 || starting
                ? 'bg-gray-700 text-gray-500 cursor-not-allowed'
                : 'bg-indigo-600 hover:bg-indigo-500 text-white shadow-lg hover:shadow-indigo-500/30 active:scale-[0.98]'
            }`}
          >
            {starting
              ? 'Запускаем...'
              : participants.length === 0
                ? 'Ждём участников...'
                : `Начать опрос (${participants.length})`}
          </button>
        </div>
      </div>
    </div>
  )
}
