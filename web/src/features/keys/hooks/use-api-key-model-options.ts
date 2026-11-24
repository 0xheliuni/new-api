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
  useQueries,
  useQuery,
  type UseQueryResult,
} from '@tanstack/react-query'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import type { Option } from '@/components/multi-select'
import { getUserModels } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import { buildApiKeyModelOptions } from '../lib/api-key-model-options'

type UserModelsResponse = Awaited<ReturnType<typeof getUserModels>>

type UseApiKeyModelOptionsParams = {
  enabled: boolean
  /** Groups the key routes through, in routing order. */
  groups: string[]
  selected: string[]
}

// A module-level combine keeps the result referentially stable until one of
// the group queries changes.
function combineGroupModels(
  results: UseQueryResult<UserModelsResponse>[]
): string[][] | null {
  if (results.length === 0 || results.some((result) => !result.isSuccess)) {
    return null
  }
  return results.map((result) => result.data?.data ?? [])
}

/**
 * Model Limits choices for the groups a key routes through. Until those
 * groups are known and loaded it lists every model the user can reach.
 */
export function useApiKeyModelOptions(
  params: UseApiKeyModelOptionsParams
): Option[] {
  const { t } = useTranslation()
  const allModels = useQuery({
    queryKey: ['user-models'],
    queryFn: async () => requireServerSuccess(await getUserModels()),
    enabled: params.enabled,
    staleTime: 0,
  })
  const groupModels = useQueries({
    queries: params.groups.map((group) => ({
      queryKey: ['user-models', group],
      queryFn: async () => requireServerSuccess(await getUserModels(group)),
      enabled: params.enabled,
      staleTime: 0,
    })),
    combine: combineGroupModels,
  })
  const allModelNames = allModels.data?.data

  return useMemo(() => {
    if (!groupModels) {
      return (allModelNames ?? []).map((model) => ({
        label: model,
        value: model,
      }))
    }
    return buildApiKeyModelOptions(
      t,
      params.groups.map((group, index) => ({
        group,
        models: groupModels[index] ?? [],
      })),
      params.selected
    )
  }, [t, allModelNames, groupModels, params.groups, params.selected])
}
