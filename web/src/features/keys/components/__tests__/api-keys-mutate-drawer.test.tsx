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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'

import type { ApiKey } from '../../types'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { api } = await import('@/lib/api')
const { ApiKeysProvider } = await import('../api-keys-provider')
const { ApiKeysMutateDrawer } = await import('../api-keys-mutate-drawer')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = {
  get: ApiMethod
  post: ApiMethod
  put: ApiMethod
}
type RenderedDrawer = {
  queryClient: InstanceType<typeof QueryClient>
}
type GroupsData = Record<string, { desc: string; ratio: number | string }>
type DrawerFixture = {
  groups: GroupsData
  defaultUseAutoGroup: boolean
  apiKey?: ApiKey
}

const groupsWithAuto: GroupsData = {
  auto: { desc: 'Automatic routing', ratio: 'auto' },
  default: { desc: 'Standard access', ratio: 1 },
  vip: { desc: 'Priority access', ratio: 2 },
}
const groupsWithoutAuto: GroupsData = {
  default: { desc: 'Standard access', ratio: 1 },
  vip: { desc: 'Priority access', ratio: 2 },
}
const autoGroupsResponse = {
  success: true,
  data: { groups: ['vip', 'default'], max_count: 3 },
}

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalPost = apiClient.post
const originalPut = apiClient.put
let renderedDrawer: RenderedDrawer | null = null

function installApiFixtures(
  fixture: DrawerFixture,
  savedPayloads: Array<Record<string, unknown>>
) {
  apiClient.get = async (url) => {
    switch (url) {
      case '/api/status':
        return {
          data: {
            data: { default_use_auto_group: fixture.defaultUseAutoGroup },
          },
        }
      case '/api/user/models':
        return { data: { success: true, data: [] } }
      case '/api/user/self/groups':
        return { data: { success: true, data: fixture.groups } }
      case '/api/token/auto-groups':
        return { data: autoGroupsResponse }
      case '/api/token/1':
        return { data: { success: true, data: fixture.apiKey } }
      default:
        throw new Error(`Unexpected GET ${url}`)
    }
  }
  const save: ApiMethod = async (url, data) => {
    expect(url).toBe('/api/token/')
    expect(data && typeof data === 'object').toBeTruthy()
    savedPayloads.push(data as Record<string, unknown>)
    return { data: { success: true, data: {} } }
  }
  apiClient.post = save
  apiClient.put = save
}

async function renderDrawer(fixture: DrawerFixture): Promise<void> {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const freshAt = Date.now() + 60_000
  queryClient.setQueryData(
    ['status'],
    { default_use_auto_group: fixture.defaultUseAutoGroup },
    { updatedAt: freshAt }
  )
  queryClient.setQueryData(
    ['user-models'],
    { success: true, data: [] },
    { updatedAt: freshAt }
  )
  queryClient.setQueryData(
    ['user-groups'],
    { success: true, data: fixture.groups },
    { updatedAt: freshAt }
  )
  queryClient.setQueryData(['token-auto-groups'], autoGroupsResponse, {
    updatedAt: freshAt,
  })
  renderedDrawer = { queryClient }

  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <ApiKeysProvider>
          <ApiKeysMutateDrawer
            open
            onOpenChange={() => undefined}
            currentRow={fixture.apiKey}
          />
        </ApiKeysProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
  await waitFor(
    () => {
      const saveButton = findButton('Save changes', false)
      expect(saveButton).toBeEnabled()
    },
    { timeout: 1500 }
  )
}

function findButton(text: string, required: true): HTMLButtonElement
function findButton(text: string, required: false): HTMLButtonElement | null
function findButton(text: string, required = true): HTMLButtonElement | null {
  const button = screen
    .queryAllByRole<HTMLButtonElement>('button')
    .find((candidate) => candidate.textContent?.includes(text))
  if (required && !button) {
    throw new Error(`Expected button containing "${text}"`)
  }
  return button ?? null
}

function getControlByLabel(labelText: 'Name' | 'Quantity'): HTMLInputElement
function getControlByLabel(labelText: 'Single group'): HTMLButtonElement
function getControlByLabel(
  labelText: 'Group' | 'Group order' | 'Auto group order'
): HTMLElement
function getControlByLabel(labelText: string): HTMLElement {
  const label = [...document.querySelectorAll<HTMLLabelElement>('label')].find(
    (candidate) => candidate.textContent?.trim() === labelText
  )
  if (!label) {
    throw new Error(`Expected label "${labelText}"`)
  }

  const control =
    label.control ??
    label
      .closest('[data-slot="form-item"]')
      ?.querySelector<HTMLElement>(
        '[data-slot="form-control"], input, textarea, button[role="combobox"], [role="group"]'
      )
  if (!control) {
    throw new Error(`Expected control for label "${labelText}"`)
  }
  return control
}

function groupModeButton(name: string): HTMLElement {
  return within(getControlByLabel('Group')).getByRole('button', { name })
}

function changeInput(input: HTMLInputElement, value: string): void {
  fireEvent.input(input, { target: { value } })
}

function selectComboboxOption(
  trigger: HTMLButtonElement,
  optionDescription: string
): void {
  fireEvent.click(trigger)
  const option = [
    ...document.querySelectorAll<HTMLElement>('[data-slot="command-item"]'),
  ].find((candidate) => candidate.textContent?.includes(optionDescription))
  if (!option) {
    throw new Error(`Expected option containing "${optionDescription}"`)
  }
  fireEvent.click(option)
}

function addGroupToOrder(
  labelText: 'Group order' | 'Auto group order',
  optionDescription: string
): void {
  const addGroupTrigger =
    getControlByLabel(labelText).querySelector<HTMLButtonElement>(
      'button[role="combobox"]'
    )
  if (!addGroupTrigger) {
    throw new Error(`Expected ${labelText} combobox`)
  }
  selectComboboxOption(addGroupTrigger, optionDescription)
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  apiClient.put = originalPut
  localStorage.clear()
  if (renderedDrawer) {
    renderedDrawer.queryClient.clear()
    renderedDrawer = null
  }
})

describe('API keys mutate drawer group modes', () => {
  test('inherits the root Auto order and sends an empty override for every batch-created key', async () => {
    const savedPayloads: Array<Record<string, unknown>> = []
    const fixture = { groups: groupsWithAuto, defaultUseAutoGroup: true }
    installApiFixtures(fixture, savedPayloads)
    await renderDrawer(fixture)

    expect(groupModeButton('Auto')).toHaveAttribute('aria-pressed', 'true')
    expect(
      document.body.textContent?.includes(
        'Using the complete global Auto order (2 groups)'
      )
    ).toBe(true)
    expect(
      [
        ...document.querySelectorAll('[data-slot="global-auto-order-name"]'),
      ].map((item) => item.textContent)
    ).toEqual(['vip', 'default'])
    expect(findButton('Restore global Auto', true).disabled).toBe(true)

    changeInput(getControlByLabel('Name'), 'batch')
    changeInput(getControlByLabel('Quantity'), '2')
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(savedPayloads).toHaveLength(2))

    expect(savedPayloads[0]?.name).toBe('batch')
    for (const payload of savedPayloads) {
      expect(payload.group).toBe('auto')
      expect(payload.auto_groups).toEqual([])
      expect(payload.cross_group_retry).toBe(true)
    }
  })

  test('customizing the Auto order switches to multiple groups and keeps the list across mode changes', async () => {
    const savedPayloads: Array<Record<string, unknown>> = []
    const fixture = { groups: groupsWithAuto, defaultUseAutoGroup: true }
    installApiFixtures(fixture, savedPayloads)
    await renderDrawer(fixture)

    addGroupToOrder('Auto group order', 'Priority access')

    expect(groupModeButton('Multiple groups')).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(
      document.querySelector('button[aria-label="Remove vip"]')
    ).toBeTruthy()
    expect(document.body.textContent?.includes('1 / 3 groups selected')).toBe(
      true
    )
    expect(findButton('Restore global Auto', false)).toBe(null)

    fireEvent.click(groupModeButton('Single group'))
    selectComboboxOption(getControlByLabel('Single group'), 'Standard access')
    expect(document.querySelector('button[aria-label="Remove vip"]')).toBe(null)
    fireEvent.click(groupModeButton('Multiple groups'))
    expect(
      document.querySelector('button[aria-label="Remove vip"]')
    ).toBeTruthy()

    changeInput(getControlByLabel('Name'), 'custom')
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(savedPayloads).toHaveLength(1))
    expect(savedPayloads[0]?.group).toBe('auto')
    expect(savedPayloads[0]?.auto_groups).toEqual(['vip'])
  })

  test('sends the selected single group instead of the default Auto group', async () => {
    const savedPayloads: Array<Record<string, unknown>> = []
    const fixture = { groups: groupsWithAuto, defaultUseAutoGroup: true }
    installApiFixtures(fixture, savedPayloads)
    await renderDrawer(fixture)

    fireEvent.click(groupModeButton('Single group'))
    selectComboboxOption(getControlByLabel('Single group'), 'Priority access')
    changeInput(getControlByLabel('Name'), 'ordinary')
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(savedPayloads).toHaveLength(1))

    expect(savedPayloads[0]?.group).toBe('vip')
    expect(savedPayloads[0]?.auto_groups).toEqual([])
    expect(savedPayloads[0]?.cross_group_retry).toBe(false)
  })

  test('offers multiple groups without the Auto group and saves the ordered list', async () => {
    const savedPayloads: Array<Record<string, unknown>> = []
    const fixture = { groups: groupsWithoutAuto, defaultUseAutoGroup: false }
    installApiFixtures(fixture, savedPayloads)
    await renderDrawer(fixture)

    expect(
      within(getControlByLabel('Group')).queryByRole('button', {
        name: 'Auto',
      })
    ).toBe(null)
    fireEvent.click(groupModeButton('Multiple groups'))
    addGroupToOrder('Group order', 'Priority access')
    addGroupToOrder('Group order', 'Standard access')
    changeInput(getControlByLabel('Name'), 'multi')
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(savedPayloads).toHaveLength(1))

    expect(savedPayloads[0]?.group).toBe('auto')
    expect(savedPayloads[0]?.auto_groups).toEqual(['vip', 'default'])
    expect(savedPayloads[0]?.cross_group_retry).toBe(true)
  })

  test('keeps an ordered list key unchanged when it is edited without the Auto group', async () => {
    const savedPayloads: Array<Record<string, unknown>> = []
    const fixture: DrawerFixture = {
      groups: groupsWithoutAuto,
      defaultUseAutoGroup: false,
      apiKey: {
        id: 1,
        name: 'ordered',
        key: 'sk-****',
        status: 1,
        remain_quota: 0,
        used_quota: 0,
        unlimited_quota: true,
        expired_time: -1,
        created_time: 1,
        accessed_time: 0,
        group: 'auto',
        auto_groups: ['vip', 'default'],
        cross_group_retry: true,
        model_limits_enabled: false,
        model_limits: '',
        allow_ips: '',
      },
    }
    installApiFixtures(fixture, savedPayloads)
    await renderDrawer(fixture)

    expect(groupModeButton('Multiple groups')).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(savedPayloads).toHaveLength(1))

    expect(savedPayloads[0]?.group).toBe('auto')
    expect(savedPayloads[0]?.auto_groups).toEqual(['vip', 'default'])
    expect(savedPayloads[0]?.cross_group_retry).toBe(true)
  })
})
