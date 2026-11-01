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
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterContextProvider,
} from '@tanstack/react-router'
import type { Row } from '@tanstack/react-table'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useRef, useState } from 'react'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { ChannelsProvider } from '@/features/channels/components/channels-provider'
import { DataTableRowActions } from '@/features/channels/components/data-table-row-actions'
import { ChannelMutateDrawer } from '@/features/channels/components/drawers/channel-mutate-drawer'
import type { Channel } from '@/features/channels/types'
import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  ChannelSSOSection,
  type ChannelSSOSectionHandle,
} from '../../components/drawers/sections/channel-sso-section'
import { getChannelSSOConfig, saveChannelSSOConfig } from '../api'

vi.mock('../api', () => ({
  getChannelSSOConfig: vi.fn(),
  saveChannelSSOConfig: vi.fn(),
}))

const channel: Channel = {
  id: 7,
  type: 1,
  key: 'sk-test',
  status: 1,
  name: 'Test Channel',
  created_time: 0,
  test_time: 0,
  response_time: 0,
  other: '',
  balance: 0,
  balance_updated_time: 0,
  models: '',
  group: 'default',
  used_quota: 0,
  other_info: '',
  remark: '',
  max_input_tokens: 0,
  channel_info: {
    is_multi_key: false,
    multi_key_size: 0,
    multi_key_polling_index: 0,
    multi_key_mode: 'random',
  },
  settings: '{}',
}

const originalAuth = useAuthStore.getState().auth

function setRole(role: number) {
  useAuthStore.setState({
    auth: { ...originalAuth, user: { id: 1, username: 'tester', role } },
  })
}

function renderRowActions() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <DataTableRowActions row={{ original: channel } as Row<Channel>} />
      </ChannelsProvider>
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  useAuthStore.setState({ auth: originalAuth })
  vi.restoreAllMocks()
})

it('does not show the Single sign-on row action for a non-super-admin', async () => {
  setRole(ROLE.ADMIN)
  renderRowActions()
  await userEvent.click(screen.getByRole('button', { name: 'Open menu' }))
  expect(
    screen.queryByRole('menuitem', { name: 'Single sign-on' })
  ).not.toBeInTheDocument()
})

it('opens the SSO transition page synchronously when a super admin clicks the row action', async () => {
  setRole(ROLE.SUPER_ADMIN)
  const openSpy = vi.spyOn(window, 'open').mockReturnValue(null)
  renderRowActions()
  await userEvent.click(screen.getByRole('button', { name: 'Open menu' }))
  await userEvent.click(
    await screen.findByRole('menuitem', { name: 'Single sign-on' })
  )
  expect(openSpy).toHaveBeenCalledWith('/channels/sso/7', '_blank', 'noopener')
})

function SSOSectionHarness(props: { channelId: number }) {
  const ref = useRef<ChannelSSOSectionHandle>(null)
  return (
    <>
      <ChannelSSOSection
        ref={ref}
        channelId={props.channelId}
        isEditing
        open
        defaultBaseUrl='https://upstream.example.com/v1'
      />
      <button
        type='button'
        onClick={() => void ref.current?.saveIfChanged(props.channelId)}
      >
        Save
      </button>
    </>
  )
}

beforeEach(() => {
  vi.mocked(getChannelSSOConfig).mockReset()
  vi.mocked(saveChannelSSOConfig).mockReset()
})

it('leaves the password field empty when a password is already saved, and keeps it by sending an empty password', async () => {
  vi.mocked(getChannelSSOConfig).mockResolvedValue({
    success: true,
    data: {
      configured: true,
      base_url: 'https://upstream.example.com',
      username: 'admin',
      entry_path: '/dashboard',
      has_password: true,
    },
  })
  vi.mocked(saveChannelSSOConfig).mockResolvedValue({ success: true })

  render(<SSOSectionHarness channelId={7} />)

  const passwordInput = await screen.findByLabelText('Password')
  expect(passwordInput).toHaveValue('')
  // The password field must be the shared PasswordInput, not a bare
  // <input type="password">, per the component-reuse rule.
  expect(
    screen.getByRole('button', { name: 'Toggle password visibility' })
  ).toBeInTheDocument()

  const usernameInput = screen.getByLabelText('Username')
  await userEvent.clear(usernameInput)
  await userEvent.type(usernameInput, 'root')

  await userEvent.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() =>
    expect(saveChannelSSOConfig).toHaveBeenCalledWith(7, {
      base_url: 'https://upstream.example.com',
      username: 'root',
      password: '',
      entry_path: '/dashboard',
    })
  )
})

it('shows the "not enabled" message together with read-only fields and a disabled clear button', async () => {
  vi.mocked(getChannelSSOConfig).mockResolvedValue({
    success: false,
    message: 'Channel SSO is not enabled on this server',
    message_key: 'Channel SSO is not enabled on this server',
  })

  render(<SSOSectionHarness channelId={7} />)

  expect(
    await screen.findByText('Channel SSO is not enabled on this server')
  ).toBeInTheDocument()

  const baseUrlInput = screen.getByLabelText('Upstream Console URL')
  const usernameInput = screen.getByLabelText('Username')
  const passwordInput = screen.getByLabelText('Password')
  const entryPathInput = screen.getByLabelText('Entry Path')
  for (const field of [
    baseUrlInput,
    usernameInput,
    passwordInput,
    entryPathInput,
  ]) {
    expect(field).toBeVisible()
    expect(field).toHaveAttribute('readonly')
  }

  expect(
    screen.getByRole('button', { name: 'Clear SSO configuration' })
  ).toBeDisabled()
})

function MultiIdSaveHarness(props: { ids: number[] }) {
  const ref = useRef<ChannelSSOSectionHandle>(null)
  return (
    <>
      <ChannelSSOSection
        ref={ref}
        channelId={null}
        isEditing={false}
        open
        defaultBaseUrl='https://upstream.example.com/v1'
      />
      <button
        type='button'
        onClick={() => {
          for (const id of props.ids) {
            void ref.current?.saveIfChanged(id)
          }
        }}
      >
        Save all
      </button>
    </>
  )
}

it('saves the same SSO config to every id when a creation makes more than one channel', async () => {
  vi.mocked(saveChannelSSOConfig).mockResolvedValue({ success: true })
  render(<MultiIdSaveHarness ids={[501, 502]} />)

  const usernameInput = await screen.findByLabelText('Username')
  await userEvent.type(usernameInput, 'upstream-admin')

  await userEvent.click(screen.getByRole('button', { name: 'Save all' }))

  await waitFor(() => {
    expect(saveChannelSSOConfig).toHaveBeenCalledWith(
      501,
      expect.objectContaining({ username: 'upstream-admin' })
    )
    expect(saveChannelSSOConfig).toHaveBeenCalledWith(
      502,
      expect.objectContaining({ username: 'upstream-admin' })
    )
  })
})

function NotifyUnsavableHarness() {
  const ref = useRef<ChannelSSOSectionHandle>(null)
  return (
    <>
      <ChannelSSOSection
        ref={ref}
        channelId={null}
        isEditing={false}
        open
        defaultBaseUrl=''
      />
      <button type='button' onClick={() => ref.current?.notifyUnsavable()}>
        Notify
      </button>
    </>
  )
}

it('does not warn about unsaved SSO settings when nothing in the section changed', async () => {
  const errorToast = vi.spyOn(toast, 'error')
  render(<NotifyUnsavableHarness />)
  await userEvent.click(screen.getByRole('button', { name: 'Notify' }))
  expect(errorToast).not.toHaveBeenCalled()
})

it('warns that SSO settings were not saved when the created channel got no id back', async () => {
  const errorToast = vi.spyOn(toast, 'error')
  render(<NotifyUnsavableHarness />)
  const usernameInput = await screen.findByLabelText('Username')
  await userEvent.type(usernameInput, 'admin')
  await userEvent.click(screen.getByRole('button', { name: 'Notify' }))
  expect(errorToast).toHaveBeenCalledWith(
    'Single sign-on settings were not saved; edit the channel to set them'
  )
})

function CreateDrawerHarness() {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { retry: false },
          mutations: { retry: false },
        },
      })
  )
  const [router] = useState(() =>
    createRouter({
      routeTree: createRootRoute(),
      history: createMemoryHistory({ initialEntries: ['/'] }),
    })
  )
  return (
    <QueryClientProvider client={client}>
      <RouterContextProvider router={router}>
        <ChannelsProvider>
          <ChannelMutateDrawer open onOpenChange={() => {}} currentRow={null} />
        </ChannelsProvider>
      </RouterContextProvider>
    </QueryClientProvider>
  )
}

it('saves the SSO fields entered while creating a channel once the server returns the new id', async () => {
  setRole(ROLE.SUPER_ADMIN)
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/default_base_urls') {
      return { data: { success: true, data: {} } }
    }
    if (url === '/api/task_plugin_options') {
      return { data: { success: true, data: [] } }
    }
    if (url === '/api/channel/models') {
      return { data: { success: true, data: [{ id: 'deepseek-chat' }] } }
    }
    if (url === '/api/group/') {
      return { data: { success: true, data: ['default'] } }
    }
    if (url === '/api/prefill_group') {
      return { data: { success: true, data: [] } }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, message: '', data: { ids: [501] } },
  })
  vi.mocked(saveChannelSSOConfig).mockResolvedValue({ success: true })

  render(<CreateDrawerHarness />)

  // DeepSeek is a built-in provider that does not require Base URL at
  // creation time, keeping this test focused on the SSO wiring.
  await userEvent.click(screen.getByRole('option', { name: /^DeepSeek / }))
  fireEvent.change(screen.getByLabelText('API Key *'), {
    target: { value: 'sk-test-key' },
  })
  await userEvent.click(
    screen.getByRole('button', { name: 'Fill Related Models' })
  )
  fireEvent.change(screen.getByLabelText('Upstream Console URL'), {
    target: { value: 'https://upstream.example.com' },
  })
  fireEvent.change(screen.getByLabelText('Username'), {
    target: { value: 'upstream-admin' },
  })

  await userEvent.click(screen.getByRole('button', { name: 'Create Channel' }))

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/channel',
      expect.anything(),
      expect.anything()
    )
  )
  await waitFor(() =>
    expect(saveChannelSSOConfig).toHaveBeenCalledWith(
      501,
      expect.objectContaining({
        base_url: 'https://upstream.example.com',
        username: 'upstream-admin',
      })
    )
  )
})
