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
import { api } from '@/lib/api'
import type {
  ChannelCostVersion,
  ChannelCostVersionInput,
  CostDimension,
  CostOverview,
  CostPage,
  CostQueryParams,
} from './types'

export async function getCostOverview(
  params: CostQueryParams
): Promise<CostOverview> {
  const res = await api.get('/api/cost/overview', { params })
  return res.data.data
}

export async function getCostByDimension(
  dim: CostDimension,
  params: CostQueryParams
): Promise<CostPage> {
  const res = await api.get(`/api/cost/${dim}`, { params })
  return res.data.data
}

/** Newest first (effective_from desc), matching the backend ordering. */
export async function getChannelCostVersions(
  channelId: number
): Promise<ChannelCostVersion[]> {
  const res = await api.get(`/api/cost/channels/${channelId}/versions`)
  return res.data.data ?? []
}

/**
 * The response interceptor toasts a business failure but still resolves, so
 * mutations have to reject explicitly — otherwise react-query runs onSuccess
 * for a request the server refused (e.g. a duplicate effective_from).
 */
export async function createChannelCostVersion(
  channelId: number,
  body: ChannelCostVersionInput
): Promise<ChannelCostVersion> {
  const res = await api.post(`/api/cost/channels/${channelId}/versions`, body)
  if (!res.data?.success) throw new Error(res.data?.message)
  return res.data.data
}

export async function deleteChannelCostVersion(
  versionId: number
): Promise<void> {
  const res = await api.delete(`/api/cost/versions/${versionId}`)
  if (!res.data?.success) throw new Error(res.data?.message)
}
