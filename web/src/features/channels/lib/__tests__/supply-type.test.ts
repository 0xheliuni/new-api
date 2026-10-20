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
import { QueryClient } from '@tanstack/react-query'
import { describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { channelSchema } from '../../types'
import { handleBatchSetSupplyType } from '../channel-actions'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../channel-form'

function storedChannel(supplyType: string | null | undefined) {
  return channelSchema.parse({
    id: 9,
    name: 'Supplier',
    type: 1,
    key: '',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    models: 'gpt-5',
    group: 'default',
    supply_type: supplyType,
  })
}

const validForm = {
  ...CHANNEL_FORM_DEFAULT_VALUES,
  name: 'Supplier',
  key: 'sk-test',
  models: 'gpt-5',
}

describe('channel supply type', () => {
  test('new channel form defaults to key', () => {
    expect(CHANNEL_FORM_DEFAULT_VALUES.supply_type).toBe('key')
  })

  test('editing a pool supplier channel loads pool', () => {
    expect(transformChannelToFormDefaults(storedChannel('pool')).supply_type).toBe(
      'pool'
    )
  })

  test('editing a legacy channel without supply type loads key', () => {
    expect(transformChannelToFormDefaults(storedChannel(null)).supply_type).toBe(
      'key'
    )
    expect(transformChannelToFormDefaults(storedChannel('')).supply_type).toBe(
      'key'
    )
  })

  test('create and update payloads carry the selected supply type', () => {
    const form = channelFormSchema.parse({ ...validForm, supply_type: 'pool' })
    expect(transformFormDataToCreatePayload(form).channel.supply_type).toBe(
      'pool'
    )
    expect(transformFormDataToUpdatePayload(form, 9).supply_type).toBe('pool')
  })

  test('form rejects an unknown supply type', () => {
    const result = channelFormSchema.safeParse({
      ...validForm,
      supply_type: 'vendor',
    })
    expect(result.success).toBe(false)
  })
})

describe('batch set channel supply type', () => {
  test('posts the selected ids and type, then refreshes the list and runs onSuccess', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true, data: 2 } })
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const onSuccess = vi.fn()

    await handleBatchSetSupplyType([3, 5], 'pool', queryClient, onSuccess)

    expect(post).toHaveBeenCalledWith(
      '/api/channel/batch/supply_type',
      { ids: [3, 5], supply_type: 'pool' },
      expect.anything()
    )
    expect(invalidate).toHaveBeenCalled()
    expect(onSuccess).toHaveBeenCalledOnce()
  })

  test('does not call the API when no channel is selected', async () => {
    const post = vi.spyOn(api, 'post')
    const onSuccess = vi.fn()

    await handleBatchSetSupplyType([], 'key', undefined, onSuccess)

    expect(post).not.toHaveBeenCalled()
    expect(onSuccess).not.toHaveBeenCalled()
  })

  test('keeps the dialog open when the server rejects the request', async () => {
    vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: false, message: 'Invalid parameters' },
    })
    const onSuccess = vi.fn()

    await handleBatchSetSupplyType([3], 'key', undefined, onSuccess)

    expect(onSuccess).not.toHaveBeenCalled()
  })
})
