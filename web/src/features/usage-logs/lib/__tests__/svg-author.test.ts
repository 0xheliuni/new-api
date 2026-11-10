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

import type { LogOtherData } from '../../types'
import { hasSvgAuthorCharge } from '../format'

function withAuthorQuota(quota: number): LogOtherData {
  return {
    svg_author: {
      model: 'gpt-6-astra',
      priced_as: 'gpt-6-astra',
      input_tokens: 1000,
      output_tokens: 200,
      reasoning_tokens: 50,
      quota,
    },
  }
}

describe('SVG author charge detection', () => {
  test('is true when the author tokens were charged', () => {
    expect(hasSvgAuthorCharge(withAuthorQuota(3200))).toBe(true)
  })

  test.each([
    { name: 'a missing log', other: null },
    { name: 'a log without an author', other: {} },
    { name: 'an author that charged nothing', other: withAuthorQuota(0) },
  ])('is false for $name', ({ other }) => {
    expect(hasSvgAuthorCharge(other)).toBe(false)
  })
})
