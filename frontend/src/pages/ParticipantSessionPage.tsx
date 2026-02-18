import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { socket } from '../ws/socket'

type RoomStatus = 'waiting' | 'active' | 'showing_question' | 'showing_results' | 'finished'

export function ParticipantSessionPage() {
  const { code } = useParams<{ code: string }>()
  const navigate = useNavigate()
  const [status, setStatus] = useState<RoomStatus>('waiting')

  useEffect(() => {
    if (!code) {
      navigate('/join')
      return
    }

    // If no session_token stored, the socket was never connected — redirect back to join
    const sessionToken = localStorage.getItem(`session_token_${code}`)
    if (!sessionToken) {
      navigate(`/join/${code}`)
      return
    }

    // Restore connection on page refresh — session_token is read from localStorage automatically
    socket.connect(code, undefined, undefined)

    const onStateChanged = (data: unknown) => {
      const d = data as { status?: RoomStatus }
      if (d?.status) setStatus(d.status)
    }

    const onSessionEnd = () => {
      setStatus('finished')
    }

    socket.on('room_state_changed', onStateChanged)
    socket.on('session_end', onSessionEnd)

    return () => {
      socket.off('room_state_changed', onStateChanged)
      socket.off('session_end', onSessionEnd)
    }
  }, [code, navigate])

  if (status === 'finished') {
    return (
      <div className="min-h-screen bg-gradient-to-br from-indigo-900 via-purple-900 to-pink-900 flex items-center justify-center p-4">
        <div className="text-center text-white">
          <div className="text-6xl mb-4">🏁</div>
          <h1 className="text-4xl font-bold mb-4">Опрос завершён!</h1>
          <p className="text-indigo-200 text-lg mb-8">Спасибо за участие</p>
          <button
            onClick={() => navigate('/join')}
            className="bg-white text-indigo-700 px-8 py-3 rounded-xl font-semibold text-lg hover:bg-indigo-50 transition-colors"
          >
            На главную
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-indigo-900 via-purple-900 to-pink-900 flex items-center justify-center p-4">
      <div className="text-center text-white">
        {/* Animated waiting indicator */}
        <div className="relative w-24 h-24 mx-auto mb-8">
          <div className="absolute inset-0 rounded-full border-4 border-indigo-400/30" />
          <div className="absolute inset-0 rounded-full border-4 border-t-white border-r-transparent border-b-transparent border-l-transparent animate-spin" />
          <div
            className="absolute inset-2 rounded-full border-4 border-t-transparent border-r-purple-400 border-b-transparent border-l-transparent animate-spin"
            style={{ animationDirection: 'reverse', animationDuration: '1.5s' }}
          />
        </div>

        <h1 className="text-4xl font-bold mb-3">Ждём начала...</h1>
        <p className="text-indigo-200 text-lg">Опрос скоро начнётся. Будьте готовы!</p>

        <div className="mt-8 flex justify-center gap-2">
          {[0, 1, 2].map((i) => (
            <div
              key={i}
              className="w-2 h-2 rounded-full bg-indigo-300 animate-bounce"
              style={{ animationDelay: `${i * 0.15}s` }}
            />
          ))}
        </div>

        <div className="mt-12 text-indigo-300/60 text-sm">
          Комната:{' '}
          <span className="font-mono font-bold tracking-widest text-indigo-200">{code}</span>
        </div>
      </div>
    </div>
  )
}
