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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { UsersAutoCreateDrawer } from '../users-auto-create-drawer'
import { UsersCredentialsDialog } from '../users-credentials-dialog'
import { UsersProvider, useUsers } from '../users-provider'

const clients: QueryClient[] = []

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

function AutoCreateFlow() {
  const { open, setOpen } = useUsers()

  return (
    <>
      <button type='button' onClick={() => setOpen('auto-create')}>
        Open auto create
      </button>
      <UsersAutoCreateDrawer
        open={open === 'auto-create'}
        onOpenChange={(isOpen) => !isOpen && setOpen(null)}
      />
      <UsersCredentialsDialog
        open={open === 'credentials'}
        onOpenChange={(isOpen) => !isOpen && setOpen(null)}
      />
    </>
  )
}

it('shows the created account in the credentials dialog after auto create succeeds', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/user/auto/preview') {
      return {
        data: {
          success: true,
          data: {
            username: 'User-abcd',
            password: 'secret-pass',
            group: 'default',
            quota: 0,
          },
        },
      }
    }
    return { data: { success: true, data: [] } }
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: {} } })
  const successToast = vi.spyOn(toast, 'success')
  const errorToast = vi.spyOn(toast, 'error')
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)

  render(
    <QueryClientProvider client={client}>
      <UsersProvider>
        <AutoCreateFlow />
      </UsersProvider>
    </QueryClientProvider>
  )

  await user.click(screen.getByRole('button', { name: 'Open auto create' }))
  await screen.findByDisplayValue('User-abcd')
  await user.click(screen.getByRole('button', { name: 'Confirm and create' }))

  await waitFor(() => expect(post).toHaveBeenCalledOnce())
  await waitFor(() => {
    expect(errorToast).not.toHaveBeenCalled()
    expect(successToast).toHaveBeenCalledWith('User created')
  })
  const dialog = await screen.findByRole('dialog', { name: 'User created' })
  expect(within(dialog).getByText('User-abcd')).toBeInTheDocument()
  expect(within(dialog).getByText('secret-pass')).toBeInTheDocument()
})
