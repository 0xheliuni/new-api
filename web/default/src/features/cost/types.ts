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
export interface CostMoney {
  revenue_usd: number
  revenue_cny: number
  list_usd: number
  cost_cny: number
  profit_cny: number
  profit_rate: number
  refund_usd: number
  prompt_tokens: number
  completion_tokens: number
  request_count: number
}

export interface CostBreakdownRow extends CostMoney {
  username?: string
  model_name?: string
  channel_id?: number
  channel_name?: string
}

export interface CostDimensionRow extends CostMoney {
  user_id?: number
  username?: string
  model_name?: string
  channel_id?: number
  channel_name?: string
  cost_ratio?: number
  priced: boolean
  user_count?: number
  breakdown?: CostBreakdownRow[]
  breakdown_truncated?: number
}

export interface CostTrendPoint {
  date: string
  revenue_cny: number
  cost_cny: number
  profit_cny: number
}

export interface CostStackPoint {
  date: string
  channel_id: number
  channel_name: string
  cost_cny: number
}

export interface CostOverview {
  totals: CostMoney
  unpriced_channel_count: number
  exchange_rate: number
  trend: CostTrendPoint[]
  cost_stack: CostStackPoint[]
}

export interface CostPage {
  items: CostDimensionRow[]
  total: number
  page: number
  page_size: number
  summary: CostMoney
}

export type CostDimension = 'users' | 'models' | 'channels'

export interface CostQueryParams {
  start_timestamp: number
  end_timestamp: number
  p?: number
  page_size?: number
  username?: string
  channel?: number
  model_name?: string
  exchange_rate?: number
}
