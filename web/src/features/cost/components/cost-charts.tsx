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
import { VChart } from '@visactor/react-vchart'
import { BarChart3, LineChart } from 'lucide-react'
import { type ReactNode, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import { buildCostStackSpec, buildCostTrendSpec } from '../chart-specs'
import { getThemeBackgroundColor, useCostCurrency } from '../lib'
import type { CostGranularity, CostOverview } from '../types'

interface CostChartsProps {
  overview?: CostOverview
  loading?: boolean
}

/**
 * Card shell matching the dashboard's chart cards (Model Call Analytics, Quota
 * Distribution): icon badge, title and total in the header, then the chart.
 */
function ChartCard(props: {
  icon: ReactNode
  tone: IconBadgeTone
  title: string
  summary: string
  isEmpty: boolean
  emptyText: string
  children: ReactNode
}) {
  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex w-full items-center gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
        <IconBadge tone={props.tone} size='sm'>
          {props.icon}
        </IconBadge>
        <div className='text-sm font-semibold'>{props.title}</div>
        {props.summary && (
          <span className='text-muted-foreground text-xs tabular-nums'>
            {props.summary}
          </span>
        )}
      </div>
      <div className='h-[300px] p-1.5 sm:h-96 sm:p-2'>
        {props.isEmpty ? (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {props.emptyText}
          </div>
        ) : (
          props.children
        )}
      </div>
    </div>
  )
}

export function CostCharts({ overview, loading }: CostChartsProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const currency = useCostCurrency()

  const revenueLabel = t('Revenue')
  const costLabel = t('Upstream Cost')
  const profitLabel = t('Profit')
  const otherLabel = t('Other')
  const totalLabel = t('Total:')

  const granularity: CostGranularity = overview?.granularity ?? 'day'
  // Trend/stack points carry CNY only (revenue_cny = revenue_usd × query rate,
  // cost_cny = list × channel ratio). Convert each once into the display
  // currency so the axis agrees with the table.
  const queryRate = overview?.exchange_rate ?? 0
  const toDisplay = useCallback(
    (cny: number) => currency.fromCny(cny, queryRate),
    [currency, queryRate]
  )

  const trendSpec = useMemo(
    () =>
      buildCostTrendSpec({
        points: overview?.trend ?? [],
        granularity,
        labels: { revenue: revenueLabel, cost: costLabel, profit: profitLabel },
        toDisplay,
        formatMoney: currency.format,
      }),
    [
      overview?.trend,
      granularity,
      revenueLabel,
      costLabel,
      profitLabel,
      toDisplay,
      currency,
    ]
  )

  // Read on every render: the themed background changes with the theme, and
  // the stack spec below rebuilds when it does.
  const gapStroke = getThemeBackgroundColor()
  const stackSpec = useMemo(
    () =>
      buildCostStackSpec({
        points: overview?.cost_stack ?? [],
        granularity,
        otherLabel,
        totalLabel,
        gapStroke,
        toDisplay,
        formatMoney: currency.format,
      }),
    [
      overview?.cost_stack,
      granularity,
      otherLabel,
      totalLabel,
      gapStroke,
      toDisplay,
      currency,
    ]
  )

  const chartKey = [
    resolvedTheme,
    loading ? 'loading' : 'ready',
    granularity,
    currency.symbol,
    overview?.trend?.length ?? 0,
    overview?.cost_stack?.length ?? 0,
  ].join('-')

  const chartsReady = themeReady && !loading
  const trendEmpty = chartsReady && !(overview?.trend?.length ?? 0)
  const stackEmpty = chartsReady && !(overview?.cost_stack?.length ?? 0)
  const emptyText = t('No data available')
  const totals = !loading && overview ? overview.totals : undefined

  return (
    <div className='flex flex-col gap-4'>
      <ChartCard
        icon={<LineChart />}
        tone='chart-4'
        title={t('Revenue / Cost / Profit Trend')}
        summary={
          totals
            ? `${t('Total profit:')} ${currency.format(toDisplay(totals.profit_cny))}`
            : ''
        }
        isEmpty={trendEmpty}
        emptyText={emptyText}
      >
        {chartsReady && (
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
      </ChartCard>

      <ChartCard
        icon={<BarChart3 />}
        tone='chart-2'
        title={t('Cost by Channel')}
        summary={
          totals
            ? `${totalLabel} ${currency.format(toDisplay(totals.cost_cny))}`
            : ''
        }
        isEmpty={stackEmpty}
        emptyText={emptyText}
      >
        {chartsReady && (
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
      </ChartCard>
    </div>
  )
}
