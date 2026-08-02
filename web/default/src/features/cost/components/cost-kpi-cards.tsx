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
import { DollarSign, PiggyBank, Receipt, TrendingUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatCard } from '@/features/dashboard/components/ui/stat-card'
import type { CostOverview } from '../types'
import { formatCny, formatRate, formatUsd } from '../lib'

interface CostKpiCardsProps {
  overview?: CostOverview
  loading?: boolean
}

export function CostKpiCards({ overview, loading }: CostKpiCardsProps) {
  const { t } = useTranslation()
  const totals = overview?.totals
  const unpricedCount = overview?.unpriced_channel_count ?? 0
  const profitTone =
    totals && totals.profit_cny < 0 ? ('rose' as const) : ('teal' as const)

  return (
    <div className='flex flex-col gap-3'>
      {unpricedCount > 0 && (
        <div className='border-warning/40 bg-warning/10 text-warning-foreground rounded-md border px-3 py-2 text-sm'>
          {t(
            '{{count}} channels have no cost ratio set; their cost is counted as 0',
            { count: unpricedCount }
          )}
        </div>
      )}

      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
        <div className='bg-card rounded-xl border p-3'>
          <StatCard
            title={t('Revenue')}
            value={loading || !totals ? '--' : formatUsd(totals.revenue_usd)}
            description={
              loading || !totals
                ? ''
                : `${formatCny(totals.revenue_cny)} · ${t('Refunds')} -${formatUsd(totals.refund_usd)}`
            }
            icon={DollarSign}
            tone='gray'
            loading={loading}
          />
        </div>
        <div className='bg-card rounded-xl border p-3'>
          <StatCard
            title={t('Cost')}
            value={loading || !totals ? '--' : formatCny(totals.cost_cny)}
            description={
              loading || !totals
                ? ''
                : `${t('List Price')} ${formatUsd(totals.list_usd)} × ${t('per-channel ratios')}`
            }
            icon={Receipt}
            tone='gray'
            loading={loading}
          />
        </div>
        <div className='bg-card rounded-xl border p-3'>
          <StatCard
            title={t('Profit')}
            value={loading || !totals ? '--' : formatCny(totals.profit_cny)}
            description={loading || !totals ? '' : formatRate(totals.profit_rate)}
            icon={PiggyBank}
            tone={profitTone}
            loading={loading}
          />
        </div>
        <div className='bg-card rounded-xl border p-3'>
          <StatCard
            title={t('Profit Rate')}
            value={loading || !totals ? '--' : formatRate(totals.profit_rate)}
            description={
              loading || !totals
                ? ''
                : `${formatCny(totals.profit_cny)} / ${formatCny(totals.revenue_cny)}`
            }
            icon={TrendingUp}
            tone={profitTone}
            loading={loading}
          />
        </div>
      </div>
    </div>
  )
}
