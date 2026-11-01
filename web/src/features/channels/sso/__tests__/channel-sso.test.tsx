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
import { StrictMode, useRef, useState } from 'react'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

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
import {
  checkChannelSSO,
  getChannelSSOConfig,
  getChannelSSOStatus,
  openChannelSSO,
  saveChannelSSOConfig,
  validateChannelSSO,
} from '../api'
import { SSOTransitionPage } from '../sso-transition-page'

vi.mock('../api', () => ({
  getChannelSSOStatus: vi.fn(),
  getChannelSSOConfig: vi.fn(),
  saveChannelSSOConfig: vi.fn(),
  checkChannelSSO: vi.fn(),
  validateChannelSSO: vi.fn(),
  openChannelSSO: vi.fn(),
}))

const { mockUseSecureVerification, defaultSecureVerification } = vi.hoisted(
  () => {
    const base = {
      requestVerification: vi.fn(),
      requestLoginVerification: vi.fn(),
      cancel: vi.fn(),
      isActive: false,
      dialogProps: {
        state: { phase: 'idle' as const },
        passkeyDomains: null,
        onCancel: vi.fn(),
        onRetry: vi.fn(),
        onInputChange: vi.fn(),
        onVerify: vi.fn(),
      },
    }
    return {
      defaultSecureVerification: base,
      mockUseSecureVerification: vi.fn(() => base),
    }
  }
)

vi.mock('@/features/auth/secure-verification', () => ({
  useSecureVerification: mockUseSecureVerification,
  SecureVerificationDialog: () => null,
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
  expect(openSpy).toHaveBeenCalledWith('/sso/channel/7', '_blank', 'noopener')
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
  vi.mocked(getChannelSSOStatus).mockReset()
  vi.mocked(getChannelSSOStatus).mockResolvedValue({
    success: true,
    data: { enabled: true },
  })
  vi.mocked(saveChannelSSOConfig).mockReset()
  vi.mocked(checkChannelSSO).mockReset()
  vi.mocked(validateChannelSSO).mockReset()
  vi.mocked(openChannelSSO).mockReset()
  mockUseSecureVerification.mockReset()
  mockUseSecureVerification.mockReturnValue(defaultSecureVerification)
  defaultSecureVerification.requestVerification.mockReset()
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

it('loads the saved settings into the editor under React StrictMode', async () => {
  vi.mocked(getChannelSSOConfig).mockResolvedValue({
    success: true,
    data: {
      configured: true,
      base_url: 'https://upstream.example.com',
      username: 'admin',
      entry_path: '/console',
      has_password: true,
    },
  })

  render(
    <StrictMode>
      <SSOSectionHarness channelId={7} />
    </StrictMode>
  )

  await waitFor(() =>
    expect(screen.getByLabelText('Username')).toHaveValue('admin')
  )
  expect(screen.getByLabelText('Entry Path')).toHaveValue('/console')
  expect(screen.getByLabelText('Username')).not.toHaveAttribute('readonly')
})

it('shows the "not enabled" message while creating a channel under React StrictMode', async () => {
  vi.mocked(getChannelSSOStatus).mockResolvedValue({
    success: true,
    data: { enabled: false },
  })

  render(
    <StrictMode>
      <ChannelSSOSection
        channelId={null}
        isEditing={false}
        open
        defaultBaseUrl='https://upstream.example.com/v1'
      />
    </StrictMode>
  )

  expect(
    await screen.findByText('Channel SSO is not enabled on this server')
  ).toBeInTheDocument()
})

it('shows the "not enabled" message with read-only fields while creating a channel', async () => {
  vi.mocked(getChannelSSOStatus).mockResolvedValue({
    success: true,
    data: { enabled: false },
  })

  render(
    <ChannelSSOSection
      channelId={null}
      isEditing={false}
      open
      defaultBaseUrl='https://upstream.example.com/v1'
    />
  )

  expect(
    await screen.findByText('Channel SSO is not enabled on this server')
  ).toBeInTheDocument()
  for (const label of [
    'Upstream Console URL',
    'Username',
    'Password',
    'Entry Path',
  ]) {
    expect(screen.getByLabelText(label)).toHaveAttribute('readonly')
  }
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

function TransitionPageHarness(props: { channelId: number }) {
  const [router] = useState(() =>
    createRouter({
      routeTree: createRootRoute(),
      history: createMemoryHistory({
        initialEntries: [`/sso/channel/${props.channelId}`],
      }),
    })
  )
  return (
    <RouterContextProvider router={router}>
      <SSOTransitionPage channelId={props.channelId} />
    </RouterContextProvider>
  )
}

function renderTransitionPage(channelId = 7, strict = false) {
  const element = <TransitionPageHarness channelId={channelId} />
  return render(strict ? <StrictMode>{element}</StrictMode> : element)
}

/**
 * jsdom's `window.location.replace` cannot be re-spied with `vi.spyOn` across
 * tests in the same file ("Cannot redefine property"), so stub it directly.
 */
function stubLocationReplace() {
  const replaceFn = vi.fn()
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: { ...window.location, replace: replaceFn },
  })
  return replaceFn
}

const checkSuccess = {
  success: true,
  data: {
    session_id: 'sid-1',
    site_name: 'Upstream Console',
    username: 'admin',
    role: ROLE.SUPER_ADMIN,
    reused: false,
  },
}

const validateSuccess = {
  success: true,
  data: { username: 'admin', role: ROLE.SUPER_ADMIN },
}

const openSuccess = {
  success: true,
  data: { enter_url: 'https://sso.example.com/__sso/enter?ticket=abc' },
}

describe('channel SSO transition page', () => {
  afterEach(() => {
    cleanup()
  })

  it('completes verify, login, and open in order and replaces location with enter_url', async () => {
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
    vi.mocked(checkChannelSSO).mockResolvedValue(checkSuccess)
    vi.mocked(validateChannelSSO).mockResolvedValue(validateSuccess)
    vi.mocked(openChannelSSO).mockResolvedValue(openSuccess)
    const replaceSpy = stubLocationReplace()

    renderTransitionPage()

    await waitFor(() =>
      expect(checkChannelSSO).toHaveBeenCalledWith(7, expect.anything())
    )
    await waitFor(() =>
      expect(validateChannelSSO).toHaveBeenCalledWith(
        'sid-1',
        expect.anything()
      )
    )
    await waitFor(() =>
      expect(openChannelSSO).toHaveBeenCalledWith('sid-1', expect.anything())
    )
    expect(screen.getByText('Super Admin · admin')).toBeInTheDocument()
    expect(screen.getByText('Signed in again')).toBeInTheDocument()
    expect(screen.getByText('Entering…')).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: 'Upstream Console' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('img', { name: 'Signed in to the upstream site' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('img', { name: 'Sign-in failed' })
    ).not.toBeInTheDocument()
    expect(defaultSecureVerification.requestVerification).not.toHaveBeenCalled()

    await waitFor(
      () =>
        expect(replaceSpy).toHaveBeenCalledWith(
          'https://sso.example.com/__sso/enter?ticket=abc'
        ),
      { timeout: 2000 }
    )
  })

  it('shows the upstream message, pending later steps, and Retry when the check reports a required step-up', async () => {
    vi.mocked(getChannelSSOConfig).mockResolvedValue({ success: false })
    vi.mocked(checkChannelSSO).mockResolvedValue({
      success: false,
      message:
        'Upstream requires two-step or human verification; automatic login is not possible',
      message_key:
        'Upstream requires two-step or human verification; automatic login is not possible',
    })

    renderTransitionPage()

    expect(
      await screen.findByText(
        'Upstream requires two-step or human verification; automatic login is not possible'
      )
    ).toBeInTheDocument()
    expect(
      screen.getByRole('img', { name: 'Sign-in failed' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('img', { name: 'Signed in to the upstream site' })
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Back to channels' })
    ).toBeInTheDocument()
    expect(validateChannelSSO).not.toHaveBeenCalled()
    expect(openChannelSSO).not.toHaveBeenCalled()
  })

  it('shows an Edit channel link when the check reports the channel has no SSO configuration', async () => {
    vi.mocked(getChannelSSOConfig).mockResolvedValue({ success: false })
    vi.mocked(checkChannelSSO).mockResolvedValue({
      success: false,
      message: 'Channel SSO is not configured',
      message_key: 'Channel SSO is not configured',
    })

    renderTransitionPage()

    expect(
      await screen.findByText('Channel SSO is not configured')
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /Edit channel/ })
    ).toBeInTheDocument()
  })

  it('restarts from verify when Retry is clicked', async () => {
    vi.mocked(getChannelSSOConfig).mockResolvedValue({ success: false })
    vi.mocked(checkChannelSSO)
      .mockResolvedValueOnce({
        success: false,
        message: 'Upstream site is unreachable',
        message_key: 'Upstream site is unreachable',
      })
      .mockResolvedValueOnce(checkSuccess)
    vi.mocked(validateChannelSSO).mockResolvedValue(validateSuccess)
    vi.mocked(openChannelSSO).mockResolvedValue(openSuccess)
    stubLocationReplace()

    renderTransitionPage()

    await screen.findByText('Upstream site is unreachable')
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => expect(checkChannelSSO).toHaveBeenCalledTimes(2))
    expect(checkChannelSSO).toHaveBeenNthCalledWith(2, 7, expect.anything())
    await screen.findByText('Super Admin · admin')
  })

  it('shows "Reused existing session" as the login detail when the check result reports reused=true', async () => {
    vi.mocked(getChannelSSOConfig).mockResolvedValue({ success: false })
    vi.mocked(checkChannelSSO).mockResolvedValue({
      success: true,
      data: { ...checkSuccess.data, reused: true },
    })
    vi.mocked(validateChannelSSO).mockResolvedValue(validateSuccess)
    vi.mocked(openChannelSSO).mockResolvedValue(openSuccess)
    stubLocationReplace()

    renderTransitionPage()

    expect(
      await screen.findByText('Reused existing session')
    ).toBeInTheDocument()
  })

  it('starts the flow exactly once even under React StrictMode', async () => {
    vi.mocked(getChannelSSOConfig).mockResolvedValue({ success: false })
    vi.mocked(checkChannelSSO).mockResolvedValue(checkSuccess)
    vi.mocked(validateChannelSSO).mockResolvedValue(validateSuccess)
    vi.mocked(openChannelSSO).mockResolvedValue(openSuccess)
    stubLocationReplace()

    renderTransitionPage(7, true)

    await screen.findByText('Super Admin · admin')
    expect(checkChannelSSO).toHaveBeenCalledTimes(1)
  })
})
