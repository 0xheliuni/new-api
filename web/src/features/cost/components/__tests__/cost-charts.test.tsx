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
import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import type { CostMoney, CostOverview } from '../../types'
import { CostCharts } from '../cost-charts'

const chart = vi.hoisted(() =>
  vi.fn((_props: { spec: { type: string } }) => null)
)
vi.mock('@visactor/react-vchart', () => ({ VChart: chart }))
vi.mock('@visactor/vchart', () => ({
  ThemeManager: { setCurrentTheme: vi.fn() },
}))

// The default display currency is USD at a site rate of 1, so a CNY amount
// shows as cny / query rate: 680 / 6.8 = $100, 340 / 6.8 = $50.
const overview: CostOverview = {
  totals: { profit_cny: 680, cost_cny: 340 } as CostMoney,
  unpriced_channel_count: 0,
  exchange_rate: 6.8,
  granularity: 'hour',
  trend: [
    {
      date: '2026-11-12 09',
      revenue_cny: 1020,
      cost_cny: 340,
      profit_cny: 680,
    },
  ],
  cost_stack: [
    {
      date: '2026-11-12 09',
      channel_id: 3,
      channel_name: 'upstream',
      cost_cny: 340,
    },
  ],
}

afterEach(() => {
  chart.mockClear()
})

describe('cost charts', () => {
  test('the card headers show the profit and cost totals in the display currency', async () => {
    render(<CostCharts overview={overview} />)

    expect(screen.getByText(/^Total profit: \$100(\.00)?$/)).toBeVisible()
    expect(screen.getByText(/^Total: \$50(\.00)?$/)).toBeVisible()
    await waitFor(() => expect(chart).toHaveBeenCalledTimes(2))
    expect(chart.mock.calls.map((call) => call[0].spec.type)).toEqual([
      'area',
      'bar',
    ])
  })
})
