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
import { resolveDualMoney } from '../lib'

// List $1177.17, channel cost ratio 3.4 (¥ per $1 list), user discount 0.65,
// query rate 6.8. The backend returns revenue in USD and cost in CNY:
//   revenue_usd = 1177.17 × 0.65 = 765.16
//   cost_cny    = 1177.17 × 3.4  = 4002.39 (CNY, not USD)
const rate = 6.8

describe('resolveDualMoney', () => {
  test('CNY cost under USD display is converted, not relabelled', () => {
    const cost = resolveDualMoney(4002.39, 'cny', 'usd', rate)
    expect(cost.primary).toContain('$')
    expect(cost.primary).toContain('588.59')
    expect(cost.secondary).toContain('4,002.39')
  })

  test('USD revenue under CNY display is converted by the query rate', () => {
    const revenue = resolveDualMoney(765.16, 'usd', 'cny', rate)
    expect(revenue.primary).toContain('5,203.09')
    expect(revenue.secondary).toContain('765.16')
  })

  test('amount already in the primary currency is shown unchanged', () => {
    expect(resolveDualMoney(765.16, 'usd', 'usd', rate).primary).toContain(
      '765.16'
    )
    expect(resolveDualMoney(4002.39, 'cny', 'cny', rate).primary).toContain(
      '4,002.39'
    )
  })

  test('zero query rate does not relabel CNY cost as USD', () => {
    expect(resolveDualMoney(4002.39, 'cny', 'usd', 0).primary).not.toContain(
      '4,002'
    )
  })
})
