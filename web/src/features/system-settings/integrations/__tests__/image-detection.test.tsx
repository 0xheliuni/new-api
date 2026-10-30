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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { ImageDetectionSection } from '../image-detection-section'

type ImageDetectionDefaults = Parameters<
  typeof ImageDetectionSection
>[0]['defaultValues']

const savedSettings: ImageDetectionDefaults = {
  'image_detection_setting.global_enabled': true,
  'image_detection_setting.global_sample_rate': 1,
  'image_detection_setting.scheduled_enabled': false,
  'image_detection_setting.scheduled_cron': '0 */2 * * *',
  'image_detection_setting.scheduled_batch_limit': 200,
  'image_detection_setting.channel_scope': 'all',
  'image_detection_setting.selected_channel_ids': [],
  'image_detection_setting.check_resolution': true,
  'image_detection_setting.check_c2pa': true,
  'image_detection_setting.check_synth_id': true,
  'image_detection_setting.synth_id_force': true,
  // The options API never returns the saved key, so the default is always empty.
  'image_detection_setting.provenance_api_key': '',
  'image_detection_setting.synth_id_rate_per_minute': 50,
  'image_detection_setting.synth_id_rate_per_day': 500,
  'image_detection_setting.c2pa_tool_path': '',
  'image_detection_setting.c2pa_timeout_seconds': 3,
}

let client: QueryClient

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
})

afterEach(() => {
  cleanup()
  client.clear()
})

function show(defaultValues: ImageDetectionDefaults) {
  render(
    <QueryClientProvider client={client}>
      <ImageDetectionSection defaultValues={defaultValues} />
    </QueryClientProvider>
  )
}

describe('image detection settings', () => {
  it('shows the saved force content provenance choice', () => {
    show(savedSettings)

    expect(
      screen.getByRole('switch', { name: 'Force content provenance check' })
    ).toBeChecked()
  })

  it('disables force content provenance while the content provenance check is off', () => {
    show({
      ...savedSettings,
      'image_detection_setting.check_synth_id': false,
    })

    expect(
      screen.getByRole('switch', { name: 'Content provenance check' })
    ).not.toBeChecked()
    expect(
      screen.getByRole('switch', { name: 'Force content provenance check' })
    ).toHaveAttribute('aria-disabled', 'true')
  })

  it('takes the OpenAI key as a password and never shows a saved one', () => {
    show(savedSettings)

    const keyField = screen.getByLabelText('OpenAI API key')
    expect(keyField).toHaveAttribute('type', 'password')
    expect(keyField).toHaveValue('')
  })

  it('tells which C2PA credentials let an image skip content provenance', () => {
    show(savedSettings)

    expect(
      screen.getByRole('switch', { name: 'C2PA metadata check' })
    ).toHaveAccessibleDescription(
      'Only a trusted OpenAI signature that records AI generation counts as a pass; every other image goes on to the content provenance check'
    )
  })
})
