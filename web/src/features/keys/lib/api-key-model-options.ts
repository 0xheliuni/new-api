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

import type { Option } from '@/components/multi-select'

export type GroupModels = {
  group: string
  models: string[]
}

/**
 * Model Limits choices for a key, in routing order: a model is served by the
 * first of the key's groups that offers it, and with several groups each
 * choice names that group. Selected models that none of the groups offer stay
 * listed and are marked so they can be removed.
 */
export function buildApiKeyModelOptions(
  t: TFunction,
  groupModels: GroupModels[],
  selected: string[]
): Option[] {
  const namesGroup = groupModels.length > 1
  const seen = new Set<string>()
  const options: Option[] = []
  for (const entry of groupModels) {
    for (const model of entry.models) {
      if (seen.has(model)) continue
      seen.add(model)
      options.push(
        namesGroup
          ? {
              label: model,
              value: model,
              hint: t('Served by {{group}}', { group: entry.group }),
            }
          : { label: model, value: model }
      )
    }
  }
  for (const model of selected) {
    if (seen.has(model)) continue
    seen.add(model)
    options.push({
      label: model,
      value: model,
      hint: t('Not available in the selected groups'),
    })
  }
  return options
}
