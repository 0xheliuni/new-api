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
// ============================================================================
// Channel SSO Types
// ============================================================================

/** GET /api/channel/:id/sso */
export type ChannelSSOConfig = {
  configured: boolean
  base_url: string
  username: string
  entry_path: string
  has_password: boolean
}

/** PUT /api/channel/:id/sso body */
export type SaveChannelSSOConfigBody = {
  base_url: string
  username: string
  password: string
  entry_path: string
  clear?: boolean
}

/** POST /api/channel/:id/sso/check data */
export type ChannelSSOCheckResult = {
  session_id: string
  site_name: string
  username: string
  role: number
  reused: boolean
}

/** POST /api/channel/sso/:sid/validate data */
export type ChannelSSOValidateResult = {
  username: string
  role: number
}

/** POST /api/channel/sso/:sid/open data */
export type ChannelSSOOpenResult = {
  enter_url: string
}
