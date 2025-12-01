import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { socket } from '../ws/socket'

const schema = z.object({
  code: z
    .string()
    .min(1, 'Введите код комнаты')
    .regex(/^\d{6}$/, 'Код должен содержать 6 цифр'),
  name: z
    .string()
    .min(2, 'Минимум 2 символа')
    .max(30, 'Максимум 30 символов'),
})

type FormData = z.infer<typeof schema>

export function JoinPage() {
  const { code: urlCode } = useParams<{ code?: string }>()
  const navigate = useNavigate()
  const [error, setError] = useState('')
  const [connecting, setConnecting] = useState(false)
  const settledRef = useRef(false)

  const {
    register,
    handleSubmit,
    formState: { errors },
    setValue,
  } = useForm<FormData>({
    resolver: zodResolver(schema),
    defaultValues: { code: urlCode || '', name: '' },
  })

  useEffect(() => {
    if (urlCode) setValue('code', urlCode)
  }, [urlCode, setValue])

  const onSubmit = (data: FormData) => {
    setError('')
    setConnecting(true)
    settledRef.current = false

    let timer: ReturnType<typeof setTimeout>

    const settle = (fn: () => void) => {
      if (settledRef.current) return
      settledRef.current = true
      clearTimeout(timer)
      socket.off('connected', onConnected)
      socket.off('error', onWsError)
      fn()
    }

    const onConnected = () => {
      settle(() => navigate(`/session/${data.code}`))
    }

    const onWsError = (errData: unknown) => {
      settle(() => {
        const msg =
          (errData as { message?: string })?.message ||
          'Не удалось подключиться к комнате'
        setError(msg)
        setConnecting(false)
        socket.disconnect()
      })
    }

    socket.on('connected', onConnected)
    socket.on('error', onWsError)

    timer = setTimeout(() => {
      settle(() => {
        setError('Комната не найдена или сервер недоступен')
        setConnecting(false)
        socket.disconnect()
      })
    }, 5000)

    socket.connect(data.code, undefined, data.name)
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-indigo-900 via-purple-900 to-pink-900 flex items-center justify-center p-4">
      <div className="bg-white rounded-2xl shadow-2xl p-5 sm:p-8 w-full max-w-md">
        <div className="text-center mb-6 sm:mb-8">
          <div className="text-4xl sm:text-5xl mb-3">🎯</div>
          <h1 className="text-2xl sm:text-3xl font-bold text-gray-900 mb-1">Войти в опрос</h1>
          <p className="text-gray-500">Введите код комнаты и своё имя</p>
        </div>

        {error && (
          <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded-lg mb-6 text-sm">
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit(onSubmit)} className="space-y-5">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Код комнаты
            </label>
            <input
              {...register('code')}
              placeholder="123456"
              maxLength={6}
              inputMode="numeric"
              className="w-full px-4 py-3 border border-gray-300 rounded-lg text-center text-xl sm:text-3xl font-mono tracking-widest focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent disabled:bg-gray-100"
              disabled={!!urlCode || connecting}
            />
            {errors.code && (
              <p className="text-red-600 text-sm mt-1">{errors.code.message}</p>
            )}
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Ваше имя
            </label>
            <input
              {...register('name')}
              placeholder="Введите имя"
              maxLength={30}
              autoFocus={!!urlCode}
              className="w-full px-4 py-3 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent disabled:bg-gray-100"
              disabled={connecting}
            />
            {errors.name && (
              <p className="text-red-600 text-sm mt-1">{errors.name.message}</p>
            )}
          </div>

          <div className="text-center">
            <Link
              to="/my-results"
              className="text-sm text-indigo-600 hover:text-indigo-800 underline"
            >
              Моя история результатов
            </Link>
          </div>

          <button
            type="submit"
            disabled={connecting}
            className="w-full bg-indigo-600 text-white py-3 rounded-lg font-semibold text-lg hover:bg-indigo-700 active:bg-indigo-800 disabled:opacity-60 disabled:cursor-not-allowed transition-colors"
          >
            {connecting ? (
              <span className="flex items-center justify-center gap-2">
                <svg
                  className="animate-spin h-5 w-5"
                  fill="none"
                  viewBox="0 0 24 24"
                >
                  <circle
                    className="opacity-25"
                    cx="12"
                    cy="12"
                    r="10"
                    stroke="currentColor"
                    strokeWidth="4"
                  />
                  <path
                    className="opacity-75"
                    fill="currentColor"
                    d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
                  />
                </svg>
                Подключение...
              </span>
            ) : (
              'Войти'
            )}
          </button>
        </form>
      </div>
    </div>
  )
}
