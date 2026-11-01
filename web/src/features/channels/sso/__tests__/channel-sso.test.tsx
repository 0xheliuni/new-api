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
import type { Row } from '@tanstack/react-table'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useRef } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { ChannelsProvider } from '@/features/channels/components/channels-provider'
import { DataTableRowActions } from '@/features/channels/components/data-table-row-actions'
import type { Channel } from '@/features/channels/types'
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
