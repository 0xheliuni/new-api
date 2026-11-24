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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'

import { DEFAULT_GROUP } from '../constants'
import type { ApiKey, ApiKeyFormData } from '../types'

// ============================================================================
// Group Modes
// ============================================================================

/**
 * How a key picks its group: the owner's group, one group, its own ordered
 * group list, or the global Auto order. An ordered list is stored as the
 * `auto` group with `auto_groups` and does not need the Auto group itself.
 */
export const API_KEY_GROUP_MODES = [
  'user',
  'single',
  'multiple',
  'auto',
] as const

export type ApiKeyGroupMode = (typeof API_KEY_GROUP_MODES)[number]

export function getApiKeyGroupMode(
  apiKey: Pick<ApiKey, 'group' | 'auto_groups'>
): ApiKeyGroupMode {
  const group = apiKey.group?.trim() || ''
  if (group === '') return 'user'
  if (group !== 'auto') return 'single'
  return (apiKey.auto_groups ?? []).length > 0 ? 'multiple' : 'auto'
}

// ============================================================================
// Form Schema
// ============================================================================

export function getApiKeyFormSchema(t: TFunction, maxGroups = 5) {
  const groupLimit = Number.isInteger(maxGroups) && maxGroups > 0 ? maxGroups : 5

  return z
    .object({
      name: z.string().min(1, t('Please enter a name')),
      remain_quota_dollars: z.number().optional(),
      expired_time: z.date().optional(),
      unlimited_quota: z.boolean(),
      model_limits: z.array(z.string()),
      allow_ips: z.string().optional(),
      group_mode: z.enum(API_KEY_GROUP_MODES),
      group: z.string().optional(),
      auto_groups: z.array(z.string()),
      cross_group_retry: z.boolean().optional(),
      tokenCount: z.number().min(1).optional(),
    })
    .superRefine((data, ctx) => {
      if (data.group_mode === 'single' && !data.group) {
        ctx.addIssue({
          code: 'custom',
          path: ['group'],
          message: t('Select a group'),
        })
      }

      if (data.group_mode === 'multiple') {
        if (data.auto_groups.length === 0) {
          ctx.addIssue({
            code: 'custom',
            path: ['auto_groups'],
            message: t('Select at least one group'),
          })
        }

        if (data.auto_groups.length > groupLimit) {
          ctx.addIssue({
            code: 'custom',
            path: ['auto_groups'],
            message: t('Select at most {{max}} groups', {
              max: groupLimit,
            }),
          })
        }

        if (new Set(data.auto_groups).size !== data.auto_groups.length) {
          ctx.addIssue({
            code: 'custom',
            path: ['auto_groups'],
            message: t('Groups must not contain duplicates'),
          })
        }
      }

      if (data.unlimited_quota) {
        return
      }

      if (
        data.remain_quota_dollars === undefined ||
        data.remain_quota_dollars < 0
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['remain_quota_dollars'],
          message: t('Quota must be zero or greater'),
        })
      }
    })
}

export type ApiKeyFormValues = z.infer<ReturnType<typeof getApiKeyFormSchema>>

// ============================================================================
// Form Defaults
// ============================================================================

export const API_KEY_FORM_DEFAULT_VALUES: ApiKeyFormValues = {
  name: '',
  remain_quota_dollars: 10,
  expired_time: undefined,
  unlimited_quota: true,
  model_limits: [],
  allow_ips: '',
  group_mode: 'user',
  group: DEFAULT_GROUP,
  auto_groups: [],
  cross_group_retry: true,
  tokenCount: 1,
}

export function getApiKeyFormDefaultValues(
  defaultUseAutoGroup: boolean
): ApiKeyFormValues {
  return {
    ...API_KEY_FORM_DEFAULT_VALUES,
    group_mode: defaultUseAutoGroup ? 'auto' : 'user',
    auto_groups: [],
  }
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
export function transformFormDataToPayload(
  data: ApiKeyFormValues
): ApiKeyFormData {
  const routesAcrossGroups =
    data.group_mode === 'multiple' || data.group_mode === 'auto'
  let group = ''
  if (routesAcrossGroups) {
    group = 'auto'
  } else if (data.group_mode === 'single') {
    group = data.group || ''
  }

  return {
    name: data.name,
    remain_quota: data.unlimited_quota
      ? 0
      : parseQuotaFromDollars(data.remain_quota_dollars || 0),
    expired_time: data.expired_time
      ? Math.floor(data.expired_time.getTime() / 1000)
      : -1,
    unlimited_quota: data.unlimited_quota,
    model_limits_enabled: data.model_limits.length > 0,
    model_limits: data.model_limits.join(','),
    allow_ips: data.allow_ips || '',
    group,
    auto_groups: data.group_mode === 'multiple' ? data.auto_groups : [],
    cross_group_retry: routesAcrossGroups ? !!data.cross_group_retry : false,
  }
}

/**
 * Transform API key data to form defaults
 */
export function transformApiKeyToFormDefaults(
  apiKey: ApiKey,
  availableGroups: string[] = [],
  maxGroups = 5
): ApiKeyFormValues {
  const groupMode = getApiKeyGroupMode(apiKey)
  const availableSet = new Set(availableGroups)
  const autoGroups = (apiKey.auto_groups ?? [])
    .filter((group) => availableSet.has(group))
    .slice(0, Math.max(0, maxGroups))

  return {
    name: apiKey.name,
    remain_quota_dollars: apiKey.unlimited_quota
      ? 0
      : quotaUnitsToDollars(apiKey.remain_quota),
    expired_time:
      apiKey.expired_time > 0
        ? new Date(apiKey.expired_time * 1000)
        : undefined,
    unlimited_quota: apiKey.unlimited_quota,
    model_limits: apiKey.model_limits
      ? apiKey.model_limits.split(',').filter(Boolean)
      : [],
    allow_ips: apiKey.allow_ips || '',
    group_mode: groupMode,
    group: groupMode === 'single' ? apiKey.group || '' : DEFAULT_GROUP,
    auto_groups: autoGroups,
    cross_group_retry: !!apiKey.cross_group_retry,
    tokenCount: 1,
  }
}
