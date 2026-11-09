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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { Form, FormField } from '@/components/ui/form'
import { createServerError } from '@/lib/server-error-message'

import { previewTaskPluginConfig } from '../../api'
import { RELAY_CONFIG_EXAMPLE } from '../../constants'
import type { ChannelFormValues } from '../../lib/channel-form'
import { ChannelRelayConfigSection } from '../drawers/sections/channel-relay-config-section'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  previewTaskPluginConfig: vi.fn(),
}))

const exampleText = JSON.stringify(RELAY_CONFIG_EXAMPLE, null, 2)
const previewResult = {
  submit: {
    url: 'https://relay.example/v1/aigc/video/generate',
    method: 'POST',
  },
  errors: {},
}

function Harness(props: { initial?: string; disabled?: boolean }) {
  const [queryClient] = useState(() => new QueryClient())
  const form = useForm<ChannelFormValues>({
    defaultValues: {
      task_plugin_config: props.initial ?? '',
    } as ChannelFormValues,
  })
  return (
    <QueryClientProvider client={queryClient}>
      <Form {...form}>
        <FormField
          control={form.control}
          name='task_plugin_config'
          render={({ field }) => (
            <ChannelRelayConfigSection
              field={field}
              disabled={props.disabled}
              channelType={61}
              taskPluginKey='alibaba'
              baseUrl='https://relay.example'
              model='wan3.0-video'
            />
          )}
        />
      </Form>
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.mocked(previewTaskPluginConfig).mockReset()
})

describe('ChannelRelayConfigSection', () => {
  test('insert example fills the editor with the relay example', async () => {
    render(<Harness />)
    await userEvent.click(
      screen.getByRole('button', { name: 'Insert example' })
    )
    expect(
      screen.getByRole('textbox', { name: 'Relay configuration' })
    ).toHaveValue(exampleText)
  })

  test('preview stays disabled while the configuration is empty', () => {
    render(<Harness />)
    expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled()
  })

  test('running the preview sends the parsed configuration and shows the translated request', async () => {
    vi.mocked(previewTaskPluginConfig).mockResolvedValue({
      submit: {
        url: 'https://relay.example/v1/aigc/video/generate',
        method: 'POST',
      },
      submit_result: { task_id: 'relay-task-1' },
      errors: {},
    })
    render(<Harness initial={exampleText} />)
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }))
    await userEvent.click(
      await screen.findByRole('button', { name: 'Run preview' })
    )

    await waitFor(() => expect(previewTaskPluginConfig).toHaveBeenCalled())
    expect(vi.mocked(previewTaskPluginConfig).mock.calls[0][0]).toMatchObject({
      type: 61,
      task_plugin_key: 'alibaba',
      base_url: 'https://relay.example',
      model: 'wan3.0-video',
      entry: 'openai_video',
      config: RELAY_CONFIG_EXAMPLE,
    })
    expect(
      await screen.findByDisplayValue(
        /relay\.example\/v1\/aigc\/video\/generate/
      )
    ).toBeInTheDocument()
  })

  test('step errors returned by the preview are shown', async () => {
    vi.mocked(previewTaskPluginConfig).mockResolvedValue({
      errors: { submit_result: 'relay returned a failure' },
    })
    render(<Harness initial={exampleText} />)
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }))
    await userEvent.click(
      await screen.findByRole('button', { name: 'Run preview' })
    )
    expect(
      await screen.findByText('Task ID: relay returned a failure')
    ).toBeInTheDocument()
  })

  test('invalid sample JSON is reported without calling the server', async () => {
    render(<Harness initial={exampleText} />)
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }))
    fireEvent.input(
      await screen.findByRole('textbox', { name: 'Sample client request' }),
      { target: { value: '{' } }
    )
    await userEvent.click(screen.getByRole('button', { name: 'Run preview' }))
    expect(
      await screen.findByText('Invalid JSON: Sample client request')
    ).toBeInTheDocument()
    expect(previewTaskPluginConfig).not.toHaveBeenCalled()
  })

  test('closing the dialog discards the last result but keeps the sample inputs', async () => {
    vi.mocked(previewTaskPluginConfig).mockResolvedValue(previewResult)
    const sampleRequest = '{"model":"wan3.0-video","prompt":"A dog"}'
    const user = userEvent.setup()
    render(<Harness initial={exampleText} />)
    await user.click(screen.getByRole('button', { name: 'Preview' }))
    fireEvent.input(
      await screen.findByRole('textbox', { name: 'Sample client request' }),
      { target: { value: sampleRequest } }
    )
    await user.click(screen.getByRole('button', { name: 'Run preview' }))
    expect(
      await screen.findByRole('textbox', { name: 'Submit request' })
    ).toBeInTheDocument()

    await user.keyboard('{Escape}')
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    await user.click(screen.getByRole('button', { name: 'Preview' }))

    expect(
      await screen.findByRole('textbox', { name: 'Sample client request' })
    ).toHaveValue(sampleRequest)
    expect(
      screen.queryByRole('textbox', { name: 'Submit request' })
    ).not.toBeInTheDocument()
  })

  test('an input error after a successful run hides the previous result', async () => {
    vi.mocked(previewTaskPluginConfig).mockResolvedValue(previewResult)
    const user = userEvent.setup()
    render(<Harness initial={exampleText} />)
    await user.click(screen.getByRole('button', { name: 'Preview' }))
    await user.click(await screen.findByRole('button', { name: 'Run preview' }))
    expect(
      await screen.findByRole('textbox', { name: 'Submit request' })
    ).toBeInTheDocument()

    fireEvent.input(
      screen.getByRole('textbox', { name: 'Sample client request' }),
      { target: { value: '{' } }
    )
    await user.click(screen.getByRole('button', { name: 'Run preview' }))

    expect(
      await screen.findByText('Invalid JSON: Sample client request')
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('textbox', { name: 'Submit request' })
    ).not.toBeInTheDocument()
    expect(previewTaskPluginConfig).toHaveBeenCalledOnce()
  })

  test('a rejected preview request shows the translated server message inline', async () => {
    const reason = 'relay.submit.taskId must be a dot-separated response path'
    vi.mocked(previewTaskPluginConfig).mockRejectedValue(
      createServerError({
        success: false,
        message_key: 'Invalid task plugin configuration: {{error}}',
        message_params: { error: reason },
      })
    )
    const user = userEvent.setup()
    render(<Harness initial={exampleText} />)
    await user.click(screen.getByRole('button', { name: 'Preview' }))
    await user.click(await screen.findByRole('button', { name: 'Run preview' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      `Invalid task plugin configuration: ${reason}`
    )
  })

  test('a locked section disables inserting the example and editing', () => {
    render(<Harness initial={exampleText} disabled />)
    expect(
      screen.getByRole('button', { name: 'Insert example' })
    ).toBeDisabled()
    expect(
      screen.getByRole('textbox', { name: 'Relay configuration' })
    ).toBeDisabled()
  })

  test('choosing the Alibaba native entry refills the sample request and previews that entry', async () => {
    vi.mocked(previewTaskPluginConfig).mockResolvedValue({ errors: {} })
    const user = userEvent.setup()
    render(<Harness initial={exampleText} />)
    await user.click(screen.getByRole('button', { name: 'Preview' }))
    await user.click(
      await screen.findByRole('combobox', { name: 'Client entry' })
    )
    await user.click(
      await screen.findByRole('option', {
        name: 'Alibaba native (/ali/api/v1/...)',
      })
    )
    await user.click(screen.getByRole('button', { name: 'Run preview' }))

    await waitFor(() => expect(previewTaskPluginConfig).toHaveBeenCalled())
    expect(vi.mocked(previewTaskPluginConfig).mock.calls[0][0]).toMatchObject({
      entry: '/ali/api/v1/services/aigc/video-generation/video-synthesis',
      request: {
        model: 'wan3.0-video',
        input: { prompt: 'A cat running on the grass' },
        parameters: { resolution: '720P', duration: 5 },
      },
    })
  })
})
