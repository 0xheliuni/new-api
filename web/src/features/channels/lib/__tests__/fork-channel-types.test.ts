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
import { z } from 'zod'

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
import {
  ARK_CN_BASE_URL,
  VIDEO_CHANNEL_SETTINGS_DEFAULTS,
  getVideoChannelDefaultBaseUrl,
  getVideoChannelDefaultModels,
  getVideoChannelKeyPrompt,
  ownsVideoChannelBaseUrl,
  readVideoChannelSettings,
  validateVideoChannelSettings,
  videoChannelSettingsShape,
  writeVideoChannelSettings,
} from '../video-channel-extensions'

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

const videoChannelValidation = z
  .object({
    type: z.number(),
    base_url: z.string().optional(),
    ...videoChannelSettingsShape,
  })
  .superRefine(validateVideoChannelSettings)

function issuePaths(input: Record<string, unknown>): string[] {
  const result = videoChannelValidation.safeParse(input)
  if (result.success) return []
  return result.error.issues.map((issue) => String(issue.path[0]))
}

describe('video channel settings module', () => {
  test('missing settings read as the documented defaults', () => {
    expect(readVideoChannelSettings(null)).toEqual(
      VIDEO_CHANNEL_SETTINGS_DEFAULTS
    )
    expect(VIDEO_CHANNEL_SETTINGS_DEFAULTS).toEqual({
      byteplus_asset_enabled: false,
      byteplus_access_key: '',
      byteplus_secret_key: '',
      byteplus_asset_group_id: '',
      byteplus_project_name: 'default',
      byteplus_region: 'cn-beijing',
      byteplus_moderation_skip: true,
      asset_provider: 'byteplus',
      seedance3rd_asset_enabled: false,
      expose_upstream_task_id: false,
      volc_sign_region: '',
      volc_openapi_endpoint: '',
      volc_asset_quota_limit: 0,
    })
  })

  test('an unknown asset provider reads as the official provider', () => {
    expect(
      readVideoChannelSettings({ asset_provider: 'other' }).asset_provider
    ).toBe('byteplus')
  })

  test.each([
    {
      type: 45,
      stored: {
        byteplus_asset_enabled: true,
        asset_provider: 'cloudwise',
        byteplus_asset_group_id: 'group-1',
        byteplus_moderation_skip: false,
        expose_upstream_task_id: true,
      },
    },
    {
      type: 54,
      stored: {
        byteplus_asset_enabled: true,
        asset_provider: 'byteplus',
        byteplus_access_key: 'ak',
        byteplus_secret_key: 'sk',
        byteplus_project_name: 'project',
        byteplus_region: 'ap-southeast-1',
      },
    },
    {
      type: 65,
      stored: {
        seedance3rd_asset_enabled: true,
        expose_upstream_task_id: true,
      },
    },
    {
      type: 66,
      stored: {
        byteplus_access_key: 'ak',
        byteplus_secret_key: 'sk',
        byteplus_project_name: 'project',
        volc_sign_region: 'cn-beijing',
        volc_openapi_endpoint: 'https://open.example',
        volc_asset_quota_limit: 200,
      },
    },
  ])(
    'type $type settings survive a read and write round trip',
    ({ type, stored }) => {
      const settings: Record<string, unknown> = { ...stored }
      writeVideoChannelSettings(settings, {
        type,
        ...readVideoChannelSettings(stored),
      })
      expect(settings).toMatchObject(stored)
    }
  )

  test('saving as another channel type removes every video channel key', () => {
    const settings: Record<string, unknown> = {
      byteplus_asset_enabled: true,
      asset_provider: 'cloudwise',
      byteplus_access_key: 'ak',
      volc_sign_region: 'cn-beijing',
      volc_asset_quota_limit: 5,
      seedance3rd_asset_enabled: true,
      expose_upstream_task_id: true,
      proxy: 'http://proxy.example',
    }
    writeVideoChannelSettings(settings, { type: 1 })
    expect(settings).toEqual({ proxy: 'http://proxy.example' })
  })

  test('saving a Volc passthrough channel keeps old asset library keys and normalizes its signing keys', () => {
    const settings: Record<string, unknown> = {
      byteplus_asset_enabled: true,
      byteplus_asset_group_id: 'group-1',
    }
    writeVideoChannelSettings(settings, {
      type: 66,
      byteplus_access_key: ' ak ',
      byteplus_secret_key: 'sk',
      byteplus_project_name: '',
      volc_asset_quota_limit: -3,
    })
    expect(settings).toEqual({
      byteplus_asset_enabled: true,
      byteplus_asset_group_id: 'group-1',
      byteplus_access_key: 'ak',
      byteplus_secret_key: 'sk',
      byteplus_project_name: 'default',
      volc_sign_region: '',
      volc_openapi_endpoint: '',
      volc_asset_quota_limit: 0,
    })
  })

  test('the official asset library requires an access key and a secret key', () => {
    expect(
      issuePaths({
        type: 45,
        byteplus_asset_enabled: true,
        asset_provider: 'byteplus',
      })
    ).toEqual(['byteplus_access_key', 'byteplus_secret_key'])
    expect(
      issuePaths({
        type: 54,
        byteplus_asset_enabled: true,
        byteplus_access_key: 'ak',
        byteplus_secret_key: 'sk',
      })
    ).toEqual([])
  })

  test('the Cloudwise asset library requires the channel address', () => {
    expect(
      issuePaths({
        type: 45,
        byteplus_asset_enabled: true,
        asset_provider: 'cloudwise',
        base_url: ' ',
      })
    ).toEqual(['base_url'])
    expect(
      issuePaths({
        type: 45,
        byteplus_asset_enabled: true,
        asset_provider: 'cloudwise',
        base_url: 'https://api.cloudwise.ai/api',
      })
    ).toEqual([])
  })

  test('asset library rules apply only when it is enabled on VolcEngine or DoubaoVideo', () => {
    expect(issuePaths({ type: 45, byteplus_asset_enabled: false })).toEqual([])
    expect(issuePaths({ type: 65, byteplus_asset_enabled: true })).toEqual([])
  })

  test('only a new Volc passthrough channel gets a default address', () => {
    expect(getVideoChannelDefaultBaseUrl(66)).toBe(ARK_CN_BASE_URL)
    expect(getVideoChannelDefaultBaseUrl(45)).toBeUndefined()
  })

  test('OpenAI Video and Seedance third-party channels offer the classic model lists', () => {
    expect(getVideoChannelDefaultModels(65)).toEqual([
      'dreamina-seedance-2-0-260128',
      'dreamina-seedance-2-0-fast-260128',
      'dreamina-seedance-2-0-mini-260615',
      'dreamina-seedance-2-5-260628',
    ])
    const openAIVideoModels = getVideoChannelDefaultModels(64)
    expect(openAIVideoModels).toHaveLength(27)
    expect(openAIVideoModels).toEqual(
      expect.arrayContaining(['Vidu-q2', 'Kling-3.0-Omni', 'JV-3.0-pro'])
    )
    expect(getVideoChannelDefaultModels(1)).toBeUndefined()
  })

  test('only OpenAI Video has a fork key format prompt', () => {
    expect(getVideoChannelKeyPrompt(64)).toBe(
      'Format: SubAppId|SecretId|SecretKey[|Region], or an API key such as sk-xxx'
    )
    expect(getVideoChannelKeyPrompt(65)).toBeUndefined()
  })

  test('the fork renders the address field for VolcEngine and Volc passthrough', () => {
    expect([45, 54, 65, 66, 1].map(ownsVideoChannelBaseUrl)).toEqual([
      true,
      false,
      false,
      true,
      false,
    ])
  })
})
