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

import { apiKeySchema, type ApiKey } from '../../types'
import {
  getApiKeyFormDefaultValues,
  getApiKeyFormSchema,
  getApiKeyGroupMode,
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
} from '../api-key-form'

const t = ((key: string, options?: Record<string, unknown>) => {
  if (options?.max !== undefined) {
    return key.replace('{{max}}', String(options.max))
  }
  return key
}) as TFunction

const baseApiKey: ApiKey = {
  id: 1,
  name: 'test',
  key: 'sk-test',
  status: 1,
  remain_quota: 0,
  used_quota: 0,
  unlimited_quota: true,
  expired_time: -1,
  created_time: 1,
  accessed_time: 0,
  group: 'auto',
  auto_groups: null,
  cross_group_retry: true,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: '',
}

describe('API key group mode form mapping', () => {
  test('treats legacy token responses without auto_groups as inheritance', () => {
    const legacyApiKey: Record<string, unknown> = { ...baseApiKey }
    delete legacyApiKey.auto_groups

    expect(apiKeySchema.parse(legacyApiKey).auto_groups).toBe(null)
  })

  test.each([
    ['', null, 'user'],
    ['vip', null, 'single'],
    ['auto', null, 'auto'],
    ['auto', [], 'auto'],
    ['auto', ['vip', 'default'], 'multiple'],
  ] as const)(
    'maps group %j with list %j to the %s mode',
    (group, autoGroups, mode) => {
      expect(
        getApiKeyGroupMode({
          group,
          auto_groups: autoGroups === null ? null : [...autoGroups],
        })
      ).toBe(mode)
    }
  )

  test('creates a key that inherits the global Auto order when Auto is the default', () => {
    const payload = transformFormDataToPayload(getApiKeyFormDefaultValues(true))

    expect(payload.group).toBe('auto')
    expect(payload.auto_groups).toEqual([])
    expect(payload.cross_group_retry).toBe(true)
  })

  test('creates a key that follows the user group otherwise', () => {
    const payload = transformFormDataToPayload(
      getApiKeyFormDefaultValues(false)
    )

    expect(payload.group).toBe('')
    expect(payload.auto_groups).toEqual([])
    expect(payload.cross_group_retry).toBe(false)
  })

  test('submits multiple groups as an ordered Auto list without needing the Auto group', () => {
    const payload = transformFormDataToPayload({
      ...getApiKeyFormDefaultValues(false),
      group_mode: 'multiple',
      auto_groups: ['vip', 'default'],
      cross_group_retry: false,
    })

    expect(payload.group).toBe('auto')
    expect(payload.auto_groups).toEqual(['vip', 'default'])
    expect(payload.cross_group_retry).toBe(false)
  })

  test('submits a single group without the list or retry kept from other modes', () => {
    const payload = transformFormDataToPayload({
      ...getApiKeyFormDefaultValues(true),
      group_mode: 'single',
      group: 'vip',
      auto_groups: ['default'],
      cross_group_retry: true,
    })

    expect(payload.group).toBe('vip')
    expect(payload.auto_groups).toEqual([])
    expect(payload.cross_group_retry).toBe(false)
  })

  test('opens an ordered list key in the multiple mode, filtered by current groups and limit', () => {
    const defaults = transformApiKeyToFormDefaults(
      { ...baseApiKey, auto_groups: ['revoked', 'vip', 'default'] },
      ['default', 'vip'],
      2
    )

    expect(defaults.group_mode).toBe('multiple')
    expect(defaults.group).toBe('')
    expect(defaults.auto_groups).toEqual(['vip', 'default'])
  })

  test('opens a single group key with that group selected', () => {
    const defaults = transformApiKeyToFormDefaults(
      { ...baseApiKey, group: 'vip', auto_groups: null, cross_group_retry: false },
      ['default', 'vip']
    )

    expect(defaults.group_mode).toBe('single')
    expect(defaults.group).toBe('vip')
  })

  test('keeps a fully revoked list in the multiple mode and rejects it until a group is added', () => {
    const defaults = transformApiKeyToFormDefaults(
      { ...baseApiKey, auto_groups: ['revoked'] },
      ['default'],
      2
    )

    expect(defaults.group_mode).toBe('multiple')
    expect(defaults.auto_groups).toEqual([])
    const result = getApiKeyFormSchema(t, 2).safeParse(defaults)
    expect(result.success).toBe(false)
    if (result.success) return
    expect(result.error.issues[0]?.path).toEqual(['auto_groups'])
    expect(result.error.issues[0]?.message).toBe('Select at least one group')
  })

  test('requires a group in the single mode', () => {
    const result = getApiKeyFormSchema(t).safeParse({
      ...getApiKeyFormDefaultValues(false),
      name: 'single',
      group_mode: 'single',
      group: '',
    })

    expect(result.success).toBe(false)
    if (result.success) return
    expect(result.error.issues[0]?.path).toEqual(['group'])
    expect(result.error.issues[0]?.message).toBe('Select a group')
  })

  test('rejects lists over the configured limit', () => {
    const result = getApiKeyFormSchema(t, 1).safeParse({
      ...getApiKeyFormDefaultValues(false),
      name: 'limited',
      group_mode: 'multiple',
      auto_groups: ['default', 'vip'],
    })

    expect(result.success).toBe(false)
    if (result.success) return
    expect(result.error.issues[0]?.path[0]).toBe('auto_groups')
    expect(result.error.issues[0]?.message).toBe('Select at most 1 groups')
  })

  test('rejects duplicate groups in a list', () => {
    const result = getApiKeyFormSchema(t).safeParse({
      ...getApiKeyFormDefaultValues(false),
      name: 'duplicate',
      group_mode: 'multiple',
      auto_groups: ['vip', 'vip'],
    })

    expect(result.success).toBe(false)
    if (result.success) return
    expect(result.error.issues[0]?.message).toBe(
      'Groups must not contain duplicates'
    )
  })
})
