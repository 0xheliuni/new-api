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
import type { TFunction } from 'i18next'
import { describe, expect, test } from 'vitest'

import { buildApiKeyModelOptions } from '../api-key-model-options'

const t = ((key: string, options?: Record<string, unknown>) =>
  options?.group === undefined
    ? key
    : key.replace('{{group}}', String(options.group))) as TFunction

describe('API key model limit options', () => {
  test('lists one group without naming it', () => {
    expect(
      buildApiKeyModelOptions(t, [{ group: 'vip', models: ['gpt-4o'] }], [])
    ).toEqual([{ label: 'gpt-4o', value: 'gpt-4o' }])
  })

  test('names the first group in routing order that serves each model', () => {
    const options = buildApiKeyModelOptions(
      t,
      [
        { group: 'claude', models: ['claude-sonnet', 'shared'] },
        { group: 'gpt', models: ['shared', 'gpt-4o'] },
      ],
      []
    )

    expect(options).toEqual([
      { label: 'claude-sonnet', value: 'claude-sonnet', hint: 'Served by claude' },
      { label: 'shared', value: 'shared', hint: 'Served by claude' },
      { label: 'gpt-4o', value: 'gpt-4o', hint: 'Served by gpt' },
    ])
  })

  test('keeps a selected model the groups no longer serve and marks it', () => {
    const options = buildApiKeyModelOptions(
      t,
      [{ group: 'vip', models: ['gpt-4o'] }],
      ['gpt-4o', 'retired-model']
    )

    expect(options).toEqual([
      { label: 'gpt-4o', value: 'gpt-4o' },
      {
        label: 'retired-model',
        value: 'retired-model',
        hint: 'Not available in the selected groups',
      },
    ])
  })
})
