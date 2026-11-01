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
import { api, type ApiRequestConfig } from '@/lib/api'

import type {
  ChannelSSOCheckResult,
  ChannelSSOConfig,
  ChannelSSOOpenResult,
  ChannelSSOValidateResult,
  SaveChannelSSOConfigBody,
} from './types'

/**
 * Web console responses for these endpoints carry `message_key` (the English
 * source text, also used verbatim as an i18n key) alongside the translated
 * `message`. Callers that need to special-case a specific server error (for
 * example "Channel SSO is not enabled on this server") should compare against
 * `message_key` rather than the already-localized `message`.
 */
export type ChannelSSOApiResponse<T> = {
  success: boolean
  message?: string
  message_key?: string
  data?: T
}

const channelActionConfig = (
  config: ApiRequestConfig = {}
): ApiRequestConfig => ({
  ...config,
  skipBusinessError: true,
  skipErrorHandler: true,
})

/**
 * Whether Channel SSO is enabled on this server (requires super admin).
 */
export async function getChannelSSOStatus(
  signal?: AbortSignal
): Promise<ChannelSSOApiResponse<{ enabled: boolean }>> {
  const res = await api.get(
    '/api/channel/sso/status',
    channelActionConfig({ signal })
  )
  return res.data
}

/**
 * Get the saved Channel SSO configuration (requires super admin).
 */
export async function getChannelSSOConfig(
  id: number,
  signal?: AbortSignal
): Promise<ChannelSSOApiResponse<ChannelSSOConfig>> {
  const res = await api.get(
    `/api/channel/${id}/sso`,
    channelActionConfig({ signal })
  )
  return res.data
}

/**
 * Save (or clear, with `clear: true`) the Channel SSO configuration.
 */
export async function saveChannelSSOConfig(
  id: number,
  body: SaveChannelSSOConfigBody
): Promise<ChannelSSOApiResponse<null>> {
  const res = await api.put(
    `/api/channel/${id}/sso`,
    body,
    channelActionConfig()
  )
  return res.data
}

/**
 * Start (or resume) an upstream SSO session for the channel (super admin only).
 */
export async function checkChannelSSO(
  id: number,
  signal?: AbortSignal
): Promise<ChannelSSOApiResponse<ChannelSSOCheckResult>> {
  const res = await api.post(
    `/api/channel/${id}/sso/check`,
    undefined,
    channelActionConfig({ signal })
  )
  return res.data
}

/**
 * Re-validate a pending SSO session before the transition page offers to
 * enter the upstream console.
 */
export async function validateChannelSSO(
  sid: string,
  signal?: AbortSignal
): Promise<ChannelSSOApiResponse<ChannelSSOValidateResult>> {
  const res = await api.post(
    `/api/channel/sso/${sid}/validate`,
    undefined,
    channelActionConfig({ signal })
  )
  return res.data
}

/**
 * Consume the validated session and get the one-time upstream entry URL.
 */
export async function openChannelSSO(
  sid: string,
  signal?: AbortSignal
): Promise<ChannelSSOApiResponse<ChannelSSOOpenResult>> {
  const res = await api.post(
    `/api/channel/sso/${sid}/open`,
    undefined,
    channelActionConfig({ signal })
  )
  return res.data
}
