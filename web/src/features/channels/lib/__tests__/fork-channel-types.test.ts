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
  CHANNEL_TYPE_ADVANCED_CUSTOM,
  CHANNEL_TYPE_AIGC_VIDEO,
  CHANNEL_TYPE_NEW_API,
  CHANNEL_TYPE_SEEDANCE_3RD,
  CHANNEL_TYPE_SUB2API,
  CHANNEL_TYPE_VOLC_PASSTHROUGH,
} from '../../constants'
import { channelSchema } from '../../types'
import {
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from '../channel-form'
import { getChannelTypeIcon } from '../channel-utils'

function settingsAfterSave(
  type: number,
  settings: Record<string, unknown>
): Record<string, unknown> {
  const channel = channelSchema.parse({
    id: 9,
    name: 'Channel',
    type,
    key: '',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    models: 'seedance',
    group: 'default',
    settings: JSON.stringify(settings),
  })
  const payload = transformFormDataToUpdatePayload(
    transformChannelToFormDefaults(channel),
    channel.id
  )
  return JSON.parse(payload.settings ?? '{}') as Record<string, unknown>
}

describe('channel types added by this fork', () => {
  test.each([
    [CHANNEL_TYPE_ADVANCED_CUSTOM, 'NewAPI'],
    [CHANNEL_TYPE_SUB2API, 'Sub2API'],
    [CHANNEL_TYPE_NEW_API, 'NewAPI'],
    [CHANNEL_TYPE_AIGC_VIDEO, 'OpenAI'],
    [CHANNEL_TYPE_SEEDANCE_3RD, 'Doubao'],
    [CHANNEL_TYPE_VOLC_PASSTHROUGH, 'Volcengine'],
  ])('channel type %i uses the %s icon', (type, icon) => {
    expect(getChannelTypeIcon(type)).toBe(icon)
  })

  test('saving a Volc passthrough channel keeps its control-plane settings', () => {
    const stored = {
      byteplus_access_key: 'ak',
      byteplus_secret_key: 'sk',
      byteplus_project_name: 'project',
      volc_sign_region: 'cn-beijing',
      volc_openapi_endpoint: 'https://open.volcengineapi.com',
      volc_asset_quota_limit: 200,
    }

    expect(
      settingsAfterSave(CHANNEL_TYPE_VOLC_PASSTHROUGH, stored)
    ).toMatchObject(stored)
  })

  test('saving a Seedance third-party channel keeps its asset and task id settings', () => {
    const stored = {
      seedance3rd_asset_enabled: true,
      expose_upstream_task_id: true,
    }

    expect(settingsAfterSave(CHANNEL_TYPE_SEEDANCE_3RD, stored)).toMatchObject(
      stored
    )
  })

  test('saving a New API channel does not add Volc passthrough settings', () => {
    const settings = settingsAfterSave(CHANNEL_TYPE_NEW_API, {})

    expect(settings).not.toHaveProperty('volc_sign_region')
    expect(settings).not.toHaveProperty('byteplus_access_key')
  })

  test('saving a Sub2API channel does not add Seedance third-party settings', () => {
    const settings = settingsAfterSave(CHANNEL_TYPE_SUB2API, {})

    expect(settings).not.toHaveProperty('seedance3rd_asset_enabled')
    expect(settings).not.toHaveProperty('expose_upstream_task_id')
  })
})
