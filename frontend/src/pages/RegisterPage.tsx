import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import { useNavigate, Link } from 'react-router-dom'
import { AuthLayout } from '../components/AuthLayout'
import { apiClient } from '../api/client'
import { useAuthStore } from '../stores/authStore'

const registerSchema = z.object({
  name: z.string().min(1, 'Введите имя').max(100, 'Имя не более 100 символов'),
  email: z.string().email('Введите корректный email'),
  password: z.string().min(8, 'Пароль минимум 8 символов'),
})

type RegisterFormData = z.infer<typeof registerSchema>

export function RegisterPage() {
  const navigate = useNavigate()
  const login = useAuthStore((s) => s.login)
  const [serverError, setServerError] = useState('')

  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<RegisterFormData>()

  const onSubmit = async (data: RegisterFormData) => {
    setServerError('')
    const result = registerSchema.safeParse(data)
    if (!result.success) {
      result.error.errors.forEach((err) => {
        const field = err.path[0] as keyof RegisterFormData
        setError(field, { message: err.message })
      })
      return
    }

    try {
      const res = await apiClient.post('/auth/register', result.data)
      const { access_token, user } = res.data
      login(user, access_token)
      navigate('/dashboard')
    } catch (err: unknown) {
      const status = (err as { response?: { status: number } }).response?.status
      if (status === 409) {
        setError('email', { message: 'Аккаунт с этим email уже существует' })
      } else if (status === 400) {
        const msg = (err as { response?: { data?: { error?: string } } }).response?.data?.error
        setServerError(msg || 'Ошибка валидации данных')
      } else {
        setServerError('Произошла ошибка. Попробуйте ещё раз.')
      }
    }
  }

  return (
    <AuthLayout title="Регистрация">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-5" noValidate>
        {serverError && (
          <div className="rounded-lg bg-red-50 border border-red-200 px-4 py-3 text-sm text-red-700">
            {serverError}
          </div>
        )}

        <div>
          <label htmlFor="name" className="block text-sm font-medium text-gray-700 mb-1">
            Имя
          </label>
          <input
            id="name"
            type="text"
            autoComplete="name"
            {...register('name')}
            className="w-full rounded-lg border border-gray-300 px-4 py-2.5 text-sm outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-200 transition"
            placeholder="Иван Иванов"
          />
          {errors.name && (
            <p className="mt-1 text-xs text-red-600">{errors.name.message}</p>
          )}
        </div>

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
            autoComplete="new-password"
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
          {isSubmitting ? 'Регистрируемся...' : 'Зарегистрироваться'}
        </button>

        <p className="text-center text-sm text-gray-500">
          Уже есть аккаунт?{' '}
          <Link to="/login" className="text-indigo-600 hover:underline font-medium">
            Войти
          </Link>
        </p>
      </form>
    </AuthLayout>
  )
}
