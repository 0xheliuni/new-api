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

import { CHANNEL_TYPE_ALI, CHANNEL_TYPE_TASK_PLUGIN } from '../../constants'
import { channelSchema } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../channel-form'
import { supportsTaskPluginConfig } from '../channel-plugin-extensions'

const relayConfig = { relay: { submit: { path: '/v1/aigc/video/generate' } } }

function relayForm(overrides: Record<string, unknown> = {}) {
  return channelFormSchema.parse({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Relay',
    type: CHANNEL_TYPE_TASK_PLUGIN,
    task_plugin_key: 'alibaba',
    base_url: 'https://relay.example',
    key: 'relay-key',
    models: 'wan3.0-video',
    task_plugin_config: JSON.stringify(relayConfig),
    ...overrides,
  })
}

function settingOf(form: ReturnType<typeof relayForm>) {
  return JSON.parse(transformFormDataToCreatePayload(form).channel.setting ?? '{}')
}

describe('task plugin configuration', () => {
  test('is offered for Alibaba task plugin channels and Ali channels only', () => {
    expect(supportsTaskPluginConfig(CHANNEL_TYPE_TASK_PLUGIN, 'alibaba')).toBe(true)
    expect(supportsTaskPluginConfig(CHANNEL_TYPE_ALI)).toBe(true)
    expect(supportsTaskPluginConfig(CHANNEL_TYPE_TASK_PLUGIN, 'kling')).toBe(false)
    expect(supportsTaskPluginConfig(1)).toBe(false)
  })

  test('rejects a configuration that is not a JSON object', () => {
    const base = { ...CHANNEL_FORM_DEFAULT_VALUES, name: 'Relay', type: CHANNEL_TYPE_ALI, key: 'k', models: 'wan3.0-video' }
    expect(channelFormSchema.safeParse({ ...base, task_plugin_config: '[1]' }).success).toBe(false)
    expect(channelFormSchema.safeParse({ ...base, task_plugin_config: '{"relay":' }).success).toBe(false)
    expect(channelFormSchema.safeParse({ ...base, task_plugin_config: '' }).success).toBe(true)
  })

  test.each([
    { kind: 'an OpenAI channel', overrides: { type: 1, task_plugin_key: '' } },
    {
      kind: 'a task plugin channel bound to another plugin',
      overrides: { task_plugin_key: 'kling' },
    },
  ])(
    'accepts invalid hidden relay text on $kind and leaves it out of the setting',
    ({ overrides }) => {
      const form = relayForm({ ...overrides, task_plugin_config: '{"relay":' })
      expect(settingOf(form)).not.toHaveProperty('task_plugin_config')
    }
  )

  test('serializes the configuration into the setting of an eligible channel', () => {
    expect(settingOf(relayForm()).task_plugin_config).toEqual(relayConfig)
  })

  test('omits the configuration when it is empty or the channel cannot use it', () => {
    expect(settingOf(relayForm({ task_plugin_config: '' }))).not.toHaveProperty('task_plugin_config')
    expect(settingOf(relayForm({ task_plugin_key: 'kling' }))).not.toHaveProperty('task_plugin_config')
  })

  test('loads a stored configuration as formatted JSON', () => {
    const channel = channelSchema.parse({
      id: 12,
      name: 'Relay',
      type: CHANNEL_TYPE_TASK_PLUGIN,
      key: '',
      status: 1,
      created_time: 1,
      test_time: 0,
      response_time: 0,
      balance_updated_time: 0,
      models: 'wan3.0-video',
      group: 'default',
      base_url: 'https://relay.example',
      setting: JSON.stringify({ task_plugin_key: 'alibaba', task_plugin_config: relayConfig }),
    })
    expect(transformChannelToFormDefaults(channel).task_plugin_config).toBe(JSON.stringify(relayConfig, null, 2))
  })
})
