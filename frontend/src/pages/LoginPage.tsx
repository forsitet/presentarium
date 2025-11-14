import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import { useNavigate, Link } from 'react-router-dom'
import { AuthLayout } from '../components/AuthLayout'
import { apiClient } from '../api/client'
import { useAuthStore } from '../stores/authStore'

const loginSchema = z.object({
  email: z.string().email('Введите корректный email'),
  password: z.string().min(8, 'Пароль минимум 8 символов'),
})

type LoginFormData = z.infer<typeof loginSchema>

export function LoginPage() {
  const navigate = useNavigate()
  const login = useAuthStore((s) => s.login)
  const [serverError, setServerError] = useState('')

  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<LoginFormData>()

  const onSubmit = async (data: LoginFormData) => {
    setServerError('')
    const result = loginSchema.safeParse(data)
    if (!result.success) {
      result.error.errors.forEach((err) => {
        const field = err.path[0] as keyof LoginFormData
        setError(field, { message: err.message })
      })
      return
    }

    try {
      const res = await apiClient.post('/auth/login', result.data)
      const { access_token, user } = res.data
      login(user, access_token)
      navigate('/dashboard')
    } catch (err: unknown) {
      const status = (err as { response?: { status: number } }).response?.status
      if (status === 401) {
        setServerError('Неверный email или пароль')
      } else if (status === 429) {
        setServerError('Слишком много попыток. Попробуйте позже.')
      } else {
        setServerError('Произошла ошибка. Попробуйте ещё раз.')
      }
    }
  }

  return (
    <AuthLayout title="Войти">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-5" noValidate>
        {serverError && (
          <div className="rounded-lg bg-red-50 border border-red-200 px-4 py-3 text-sm text-red-700">
            {serverError}
          </div>
        )}

        <div>
          <label htmlFor="email" className="block text-sm font-medium text-gray-700 mb-1">
            Email
          </label>
          <input
            id="email"
            type="email"
            autoComplete="email"
            {...register('email')}
            className="w-full rounded-lg border border-gray-300 px-4 py-2.5 text-sm outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-200 transition"
            placeholder="you@example.com"
          />
          {errors.email && (
            <p className="mt-1 text-xs text-red-600">{errors.email.message}</p>
          )}
        </div>

        <div>
          <label htmlFor="password" className="block text-sm font-medium text-gray-700 mb-1">
            Пароль
          </label>
          <input
            id="password"
            type="password"
            autoComplete="current-password"
            {...register('password')}
            className="w-full rounded-lg border border-gray-300 px-4 py-2.5 text-sm outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-200 transition"
            placeholder="••••••••"
          />
          {errors.password && (
            <p className="mt-1 text-xs text-red-600">{errors.password.message}</p>
          )}
        </div>

        <button
          type="submit"
          disabled={isSubmitting}
          className="w-full rounded-lg bg-indigo-600 px-4 py-2.5 text-sm font-semibold text-white hover:bg-indigo-700 disabled:opacity-60 disabled:cursor-not-allowed transition"
        >
          {isSubmitting ? 'Входим...' : 'Войти'}
        </button>

        <p className="text-center text-sm text-gray-500">
          Нет аккаунта?{' '}
          <Link to="/register" className="text-indigo-600 hover:underline font-medium">
            Зарегистрироваться
          </Link>
        </p>
      </form>
    </AuthLayout>
  )
}
