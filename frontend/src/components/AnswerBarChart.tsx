import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'

interface ChartOption {
  text: string
  is_correct?: boolean
  image_url?: string
}

interface AnswerBarChartProps {
  options: ChartOption[]
  distribution: Record<string, number>
  showCorrect?: boolean
}

const OPTION_COLORS = ['#6366f1', '#f59e0b', '#10b981', '#ef4444', '#8b5cf6', '#06b6d4']

export function AnswerBarChart({ options, distribution, showCorrect = false }: AnswerBarChartProps) {
  const data = options.map((opt, i) => ({
    name: opt.text.length > 22 ? opt.text.slice(0, 22) + '…' : opt.text,
    count: distribution[String(i)] ?? 0,
    isCorrect: opt.is_correct,
    idx: i,
  }))

  const getColor = (entry: { isCorrect?: boolean; idx: number }) => {
    if (showCorrect) {
      if (entry.isCorrect === true) return '#10b981'
      if (entry.isCorrect === false) return '#ef4444'
    }
    return OPTION_COLORS[entry.idx % OPTION_COLORS.length]
  }

  return (
    <ResponsiveContainer width="100%" height={220}>
      <BarChart data={data} margin={{ top: 8, right: 16, left: 0, bottom: 4 }}>
        <XAxis
          dataKey="name"
          tick={{ fill: '#d1d5db', fontSize: 12 }}
          interval={0}
          tickLine={false}
          axisLine={false}
        />
        <YAxis
          allowDecimals={false}
          tick={{ fill: '#9ca3af', fontSize: 11 }}
          axisLine={false}
          tickLine={false}
          width={28}
        />
        <Tooltip
          contentStyle={{
            background: '#1f2937',
            border: 'none',
            borderRadius: '8px',
            color: '#f9fafb',
            fontSize: 13,
          }}
          cursor={{ fill: 'rgba(255,255,255,0.05)' }}
          formatter={(value: number) => [value, 'Ответов']}
        />
        <Bar dataKey="count" radius={[4, 4, 0, 0]} maxBarSize={80}>
          {data.map((entry) => (
            <Cell key={entry.idx} fill={getColor(entry)} />
          ))}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  )
}
