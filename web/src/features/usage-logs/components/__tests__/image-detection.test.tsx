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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { DetailsDialog } from '../dialogs/details-dialog'

const queryClients: QueryClient[] = []

function makeLog(other: LogOtherData): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type: 2,
    content: '',
    username: 'user',
    token_name: 'token',
    model_name: 'gpt-image-1',
    quota: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 0,
    is_stream: false,
    channel: 1,
    channel_name: 'channel',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-1',
    upstream_request_id: '',
  }
}

function renderDetails(
  other: LogOtherData,
  options?: { isRoot?: boolean }
): void {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClient.setQueryData(['status'], {}, { updatedAt: Date.now() + 60_000 })
  queryClients.push(queryClient)

  render(
    <QueryClientProvider client={queryClient}>
      <DetailsDialog
        log={makeLog(other)}
        isAdmin={options?.isRoot ?? false}
        isRoot={options?.isRoot ?? false}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

function detectionSummary(): HTMLElement {
  return screen.getByRole('group', { name: 'Detection summary' })
}

// Content provenance could not run because no key is configured, so the image
// was kept for manual verification.
const failedWithoutKey: LogOtherData = {
  detection_status: 'error',
  detection_steps: {
    resolution: 'pass',
    metadata: 'fail',
    content: 'error',
    content_error: 'no_api_key',
    image_available: true,
    duration_ms: 120,
  },
}

class FakeClipboardItem {
  constructor(readonly items: Record<string, Promise<Blob> | Blob>) {}
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  for (const queryClient of queryClients) {
    queryClient.clear()
  }
  queryClients.length = 0
})

describe('usage log image detection', () => {
  test('shows the total detection time', () => {
    renderDetails({
      detection_status: 'checked',
      detection_verdict: 'pass',
      detection_steps: {
        resolution: 'pass',
        metadata: 'pass',
        content: 'skipped',
        duration_ms: 3210,
      },
    })

    expect(
      within(detectionSummary()).getByText('Detection duration')
        .nextElementSibling
    ).toHaveTextContent('3.2s')
  })

  test('omits the detection time when the record has none', () => {
    renderDetails({
      detection_status: 'checked',
      detection_verdict: 'pass',
      detection_steps: {
        resolution: 'pass',
        metadata: 'pass',
        content: 'skipped',
      },
    })

    expect(screen.getByText('Image detection')).toBeInTheDocument()
    expect(screen.queryByText('Detection duration')).toBeNull()
  })

  test('shows the overall result after the detection time in the summary row', () => {
    renderDetails({
      detection_status: 'checked',
      detection_verdict: 'pass',
      detection_steps: {
        resolution: 'pass',
        metadata: 'pass',
        content: 'skipped',
        duration_ms: 3210,
      },
    })

    const summary = within(detectionSummary())
    const duration = summary.getByText('Detection duration')
    const result = summary.getByText('Overall result')
    expect(summary.getByText('Passed')).toBeInTheDocument()
    expect(
      duration.compareDocumentPosition(result) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  test('shows the detection status when there is no overall result', () => {
    renderDetails({
      detection_status: 'error',
      detection_steps: {
        resolution: 'pass',
        metadata: 'pass',
        content: 'error',
        duration_ms: 120,
      },
    })

    expect(
      within(detectionSummary()).getByText('Detection error')
    ).toBeInTheDocument()
  })

  test('shows each step result in pipeline order', () => {
    renderDetails({
      detection_status: 'checked',
      detection_verdict: 'fail',
      detection_steps: {
        resolution: 'pass',
        metadata: 'pass',
        content: 'fail',
        duration_ms: 3210,
      },
    })

    const steps = within(
      screen.getByRole('list', { name: 'Image detection' })
    ).getAllByRole('listitem')
    expect(steps).toHaveLength(3)
    expect(within(steps[0]).getByText('Resolution')).toBeInTheDocument()
    expect(within(steps[0]).getByText('Passed')).toBeInTheDocument()
    expect(within(steps[1]).getByText('Metadata')).toBeInTheDocument()
    expect(within(steps[1]).getByText('Passed')).toBeInTheDocument()
    expect(within(steps[2]).getByText('Content provenance')).toBeInTheDocument()
    expect(within(steps[2]).getByText('Failed')).toBeInTheDocument()
  })

  test('lists each check that did not pass with its reason', () => {
    renderDetails(failedWithoutKey, { isRoot: true })

    const entries = within(
      screen.getByRole('list', { name: 'Detection log' })
    ).getAllByRole('listitem')
    expect(entries).toHaveLength(2)
    expect(within(entries[0]).getByText('Metadata')).toBeInTheDocument()
    expect(
      within(entries[0]).getByText(
        'No C2PA credentials showing OpenAI generation were found'
      )
    ).toBeInTheDocument()
    expect(
      within(entries[1]).getByText('Content provenance')
    ).toBeInTheDocument()
    expect(
      within(entries[1]).getByText(
        'No OpenAI API key is configured, so the official check did not run'
      )
    ).toBeInTheDocument()
  })

  test('shows the raw detail of a failed content provenance call', () => {
    renderDetails({
      detection_status: 'error',
      detection_steps: {
        resolution: 'pass',
        metadata: 'pass',
        content: 'error',
        content_error: 'http_error',
        content_error_detail: 'HTTP 409: conflict',
      },
    })

    const log = within(screen.getByRole('list', { name: 'Detection log' }))
    expect(
      log.getByText('The OpenAI API rejected the request')
    ).toBeInTheDocument()
    expect(log.getByText('HTTP 409: conflict')).toBeInTheDocument()
  })

  test('says how to get access when the organization cannot use the API', () => {
    renderDetails({
      detection_status: 'error',
      detection_steps: {
        resolution: 'pass',
        metadata: 'pass',
        content: 'error',
        content_error: 'no_access',
        content_error_detail: 'HTTP 404: Not found.',
      },
    })

    const log = within(screen.getByRole('list', { name: 'Detection log' }))
    expect(
      log.getByText(
        'This OpenAI organization has no access to the Content Provenance API yet; request access first'
      )
    ).toBeInTheDocument()
  })

  test('explains why the C2PA credentials did not count', () => {
    renderDetails({
      detection_status: 'checked',
      detection_verdict: 'fail',
      detection_steps: {
        resolution: 'pass',
        metadata: 'fail',
        metadata_error: 'invalid_credentials',
        content: 'fail',
      },
    })

    const log = within(screen.getByRole('list', { name: 'Detection log' }))
    expect(
      log.getByText(
        'The image was changed after its C2PA credentials were signed'
      )
    ).toBeInTheDocument()
  })

  test('says a rate-limited official check is waiting for a later run', () => {
    renderDetails({
      detection_status: 'checking',
      detection_steps: {
        resolution: 'pass',
        metadata: 'pass',
        content: 'pending',
        content_error: 'rate_limited',
      },
    })

    const entries = within(
      screen.getByRole('list', { name: 'Detection log' })
    ).getAllByRole('listitem')
    expect(entries).toHaveLength(1)
    expect(
      within(entries[0]).getByText(
        'OpenAI rate limit reached; a later scheduled run checks the image'
      )
    ).toBeInTheDocument()
  })

  test('shows each signal the official check answered', () => {
    renderDetails({
      detection_status: 'checked',
      detection_verdict: 'pass',
      detection_steps: {
        resolution: 'pass',
        metadata: 'fail',
        content: 'pass',
        provenance_c2pa: 'not_detected',
        provenance_c2pa_state: 'not_present',
        provenance_synthid: 'detected',
        provenance_model: 'gpt-image-2',
      },
    })

    const signals = within(
      screen.getByRole('list', { name: 'Provenance signals' })
    ).getAllByRole('listitem')
    expect(signals).toHaveLength(2)
    expect(within(signals[0]).getByText('C2PA')).toBeInTheDocument()
    expect(within(signals[0]).getByText('Not detected')).toBeInTheDocument()
    expect(within(signals[0]).getByText('Not present')).toBeInTheDocument()
    expect(within(signals[1]).getByText('SynthID')).toBeInTheDocument()
    expect(within(signals[1]).getByText('Detected')).toBeInTheDocument()
    expect(screen.getByText('gpt-image-2')).toBeInTheDocument()
  })

  test('omits the signals when the official check has not answered', () => {
    renderDetails({
      detection_status: 'checked',
      detection_verdict: 'pass',
      detection_steps: {
        resolution: 'pass',
        metadata: 'pass',
        content: 'skipped',
      },
    })

    expect(
      screen.queryByRole('list', { name: 'Provenance signals' })
    ).toBeNull()
  })

  test('omits the detection log when every check passed', () => {
    renderDetails({
      detection_status: 'checked',
      detection_verdict: 'pass',
      detection_steps: {
        resolution: 'pass',
        metadata: 'pass',
        content: 'pass',
      },
    })

    expect(screen.queryByRole('list', { name: 'Detection log' })).toBeNull()
  })

  test('offers manual verification to root when content provenance could not run', () => {
    renderDetails(failedWithoutKey, { isRoot: true })

    const panel = within(
      screen.getByRole('group', { name: 'Manual verification' })
    )
    expect(
      panel.getByRole('button', { name: 'Download original' })
    ).toBeInTheDocument()
    expect(
      panel.getByRole('button', { name: 'Copy image' })
    ).toBeInTheDocument()
    expect(
      panel.getByRole('link', { name: 'Open the OpenAI verify page' })
    ).toHaveAttribute('href', 'https://openai.com/verify/')
  })

  test('hides manual verification from users who are not root', () => {
    renderDetails(failedWithoutKey)

    expect(
      screen.queryByRole('group', { name: 'Manual verification' })
    ).toBeNull()
  })

  test('copies the kept image to the clipboard as PNG', async () => {
    const user = userEvent.setup()
    const image = new Blob(['png'], { type: 'image/png' })
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: image })
    const write = vi
      .spyOn(navigator.clipboard, 'write')
      .mockResolvedValue(undefined)
    vi.stubGlobal('ClipboardItem', FakeClipboardItem)
    renderDetails(failedWithoutKey, { isRoot: true })

    await user.click(screen.getByRole('button', { name: 'Copy image' }))

    await waitFor(() => expect(write).toHaveBeenCalledTimes(1))
    expect(get).toHaveBeenCalledWith(
      '/api/image-detection/image',
      expect.objectContaining({
        params: { request_id: 'req-1' },
        responseType: 'blob',
      })
    )
    const [item] = write.mock.calls[0][0] as unknown as FakeClipboardItem[]
    expect(Object.keys(item.items)).toEqual(['image/png'])
    await expect(item.items['image/png']).resolves.toBe(image)
  })

  test('downloads the kept original image', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: new Blob(['png'], { type: 'image/png' }),
    })
    vi.stubGlobal(
      'URL',
      Object.assign(URL, {
        createObjectURL: vi.fn(() => 'blob:image'),
        revokeObjectURL: vi.fn(),
      })
    )
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(() => undefined)
    renderDetails(failedWithoutKey, { isRoot: true })

    await user.click(screen.getByRole('button', { name: 'Download original' }))

    await waitFor(() => expect(click).toHaveBeenCalledTimes(1))
    expect(click.mock.contexts[0]).toHaveAttribute(
      'download',
      'image-detection-req-1.png'
    )
  })
})
