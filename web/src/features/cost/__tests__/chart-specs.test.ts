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
import { describe, expect, test } from 'vitest'

import {
  buildCostStackSpec,
  buildCostTrendSpec,
  stackTooltipLines,
} from '../chart-specs'
import type { CostStackPoint } from '../types'

const money = (value: number) => `$${value.toFixed(2)}`
const identity = (cny: number) => cny

describe('cost chart specs', () => {
  test('the trend draws revenue, cost and profit as smooth areas in the dashboard blue, orange and green', () => {
    const spec = buildCostTrendSpec({
      points: [
        {
          date: '2026-11-12 09',
          revenue_cny: 30,
          cost_cny: 20,
          profit_cny: 10,
        },
      ],
      granularity: 'hour',
      labels: { revenue: 'Revenue', cost: 'Cost', profit: 'Profit' },
      toDisplay: identity,
      formatMoney: money,
    })

    expect(spec.type).toBe('area')
    expect(spec.area.style).toEqual({
      fillOpacity: 0.08,
      curveType: 'monotone',
    })
    expect(spec.line.style).toEqual({ lineWidth: 2, curveType: 'monotone' })
    expect(spec.point.visible).toBe(false)
    expect(spec.legends.selectMode).toBe('single')
    expect(spec.color.domain).toEqual(['Revenue', 'Cost', 'Profit'])
    expect(spec.color.range).toEqual(['#1664FF', '#FF8A00', '#3CC780'])
  })

  test('the channel stack gives eight channels plus Other a color each', () => {
    const points: CostStackPoint[] = Array.from({ length: 10 }, (_, i) => ({
      date: '2026-11-12',
      channel_id: i + 1,
      channel_name: `ch-${i + 1}`,
      cost_cny: 10 - i,
    }))
    const spec = buildCostStackSpec({
      points,
      granularity: 'day',
      otherLabel: 'Other',
      totalLabel: 'Total:',
      gapStroke: '#ffffff',
      toDisplay: identity,
      formatMoney: money,
    })

    expect(spec.color.domain).toHaveLength(9)
    expect(new Set(spec.color.range.slice(0, 9)).size).toBe(9)
  })

  test('the stack tooltip lists the bucket total first, channels by amount and Other last', () => {
    const lines = stackTooltipLines(
      [
        { key: 'Other', value: 5 },
        { key: 'small', value: 1 },
        { key: 'large', value: 9 },
      ],
      { otherLabel: 'Other', totalLabel: 'Total:', formatMoney: money }
    )

    expect(lines.map((line) => [line.key, line.value])).toEqual([
      ['Total:', '$15.00'],
      ['large', '$9.00'],
      ['small', '$1.00'],
      ['Other', '$5.00'],
    ])
  })
})
