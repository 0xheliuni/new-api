/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo } from 'react'
import { VChart } from '@visactor/react-vchart'
import { LineChart, BarChart3 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'
import type { CostOverview } from '../types'
import {
  cycleThemeChartColors,
  foldChannelStack,
  formatCny,
  getThemeBackgroundColor,
} from '../lib'

interface CostChartsProps {
  overview?: CostOverview
  loading?: boolean
}

export function CostCharts({ overview, loading }: CostChartsProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()

  const revenueLabel = t('Revenue')
  const costLabel = t('Cost')
  const profitLabel = t('Profit')
  const otherLabel = t('Other')

  const trendSpec = useMemo(() => {
    const values: Array<{ date: string; series: string; value: number }> = []
    for (const point of overview?.trend ?? []) {
      values.push({
        date: point.date,
        series: revenueLabel,
        value: point.revenue_cny,
      })
      values.push({ date: point.date, series: costLabel, value: point.cost_cny })
      values.push({
        date: point.date,
        series: profitLabel,
        value: point.profit_cny,
      })
    }
    const domain = [revenueLabel, costLabel, profitLabel]
    const range = cycleThemeChartColors(domain.length)

    return {
      type: 'line',
      data: [{ id: 'costTrend', values }],
      xField: 'date',
      yField: 'value',
      seriesField: 'series',
      legends: { visible: true },
      color: { type: 'ordinal', domain, range },
      line: { style: { lineWidth: 2 } },
      point: { visible: false },
      crosshair: { xField: { visible: true } },
      tooltip: {
        mark: {
          content: [
            {
              key: (datum: Record<string, unknown>) => String(datum?.series),
              value: (datum: Record<string, unknown>) =>
                formatCny(Number(datum?.value) || 0),
            },
          ],
        },
      },
      background: { fill: 'transparent' },
      animation: true,
    }
  }, [overview?.trend, revenueLabel, costLabel, profitLabel])

  const stackSpec = useMemo(() => {
    const folded = foldChannelStack(overview?.cost_stack ?? [], otherLabel, 8)
    const range = cycleThemeChartColors(folded.domain.length)
    const gapStroke = getThemeBackgroundColor()

    return {
      type: 'bar',
      data: [{ id: 'costStack', values: folded.data }],
      xField: 'date',
      yField: 'cost_cny',
      seriesField: 'channel',
      stack: true,
      legends: { visible: true },
      color: { type: 'ordinal', domain: folded.domain, range },
      bar: {
        style: { stroke: gapStroke, lineWidth: 2 },
        state: { hover: { stroke: gapStroke, lineWidth: 2 } },
      },
      tooltip: {
        mark: {
          content: [
            {
              key: (datum: Record<string, unknown>) => String(datum?.channel),
              value: (datum: Record<string, unknown>) =>
                formatCny(Number(datum?.cost_cny) || 0),
            },
          ],
        },
      },
      background: { fill: 'transparent' },
      animation: true,
    }
  }, [overview?.cost_stack, otherLabel])

  const chartKey = [
    resolvedTheme,
    loading ? 'loading' : 'ready',
    overview?.trend.length ?? 0,
    overview?.cost_stack.length ?? 0,
  ].join('-')

  return (
    <div className='grid gap-4 lg:grid-cols-2'>
      <div className='overflow-hidden rounded-lg border'>
        <div className='flex items-center gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
          <LineChart className='text-muted-foreground/60 size-4' />
          <div className='text-sm font-semibold'>{t('Revenue / Cost / Profit Trend')}</div>
        </div>
        <div className='h-[280px] p-1.5 sm:h-80 sm:p-2'>
          {themeReady && !loading && (
            <VChart
              key={`trend-${chartKey}`}
              spec={{
                ...trendSpec,
                theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                background: 'transparent',
              }}
              option={VCHART_OPTION}
            />
          )}
        </div>
      </div>

      <div className='overflow-hidden rounded-lg border'>
        <div className='flex items-center gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
          <BarChart3 className='text-muted-foreground/60 size-4' />
          <div className='text-sm font-semibold'>{t('Cost by Channel')}</div>
        </div>
        <div className='h-[280px] p-1.5 sm:h-80 sm:p-2'>
          {themeReady && !loading && (
            <VChart
              key={`stack-${chartKey}`}
              spec={{
                ...stackSpec,
                theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                background: 'transparent',
              }}
              option={VCHART_OPTION}
            />
          )}
        </div>
      </div>
    </div>
  )
}
