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
import type { CostStackPoint } from './types'

/**
 * Fixed-currency formatters for the cost accounting page. Unlike
 * `lib/currency.ts`, these always render USD / CNY regardless of the
 * admin-configured display currency — the cost report mixes both
 * currencies intentionally (list price in USD, settled cost in CNY).
 */
export function formatUsd(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '-'
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    currencyDisplay: 'narrowSymbol',
    maximumFractionDigits: 2,
  }).format(value)
}

export function formatCny(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '-'
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'CNY',
    currencyDisplay: 'narrowSymbol',
    maximumFractionDigits: 2,
  }).format(value)
}

/** `profit_rate` from the API is a fraction (e.g. 0.452 = 45.2%). */
export function formatRate(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '-'
  return `${(value * 100).toFixed(1)}%`
}

const THEME_CHART_COLOR_VARIABLES = [
  '--chart-1',
  '--chart-2',
  '--chart-3',
  '--chart-4',
  '--chart-5',
] as const

/** Read the app's chart color tokens so cost charts follow the active theme/preset. */
export function getThemeChartColors(): string[] {
  if (typeof document === 'undefined') return []
  const bodyStyle = window.getComputedStyle(document.body)
  const rootStyle = window.getComputedStyle(document.documentElement)
  return THEME_CHART_COLOR_VARIABLES.map((name) =>
    (bodyStyle.getPropertyValue(name) || rootStyle.getPropertyValue(name)).trim()
  ).filter(Boolean)
}

/** Cycle the theme chart colors to cover `count` series, in stable order. */
export function cycleThemeChartColors(count: number): string[] {
  const colors = getThemeChartColors()
  const fallback = [
    '#5B8FF9',
    '#5AD8A6',
    '#F6BD16',
    '#E8684A',
    '#6DC8EC',
    '#9270CA',
    '#FF9D4D',
    '#269A99',
  ]
  const source = colors.length > 0 ? colors : fallback
  return Array.from(
    { length: count },
    (_, index) => source[index % source.length]
  )
}

/** Read the app's `--background` token so stacked bars can render a themed gap stroke. */
export function getThemeBackgroundColor(): string {
  if (typeof document === 'undefined') return '#ffffff'
  const bodyStyle = window.getComputedStyle(document.body)
  const rootStyle = window.getComputedStyle(document.documentElement)
  return (
    (
      bodyStyle.getPropertyValue('--background') ||
      rootStyle.getPropertyValue('--background')
    ).trim() || '#ffffff'
  )
}

export interface FoldedStackPoint {
  date: string
  channel: string
  cost_cny: number
}

export interface FoldedStackResult {
  data: FoldedStackPoint[]
  /** Series names in fixed first-seen order; "Other" (if present) is last. */
  domain: string[]
}

/**
 * Fold a per-channel cost stack down to at most `maxSeries` named channels,
 * bucketing the remainder into `otherLabel`. Series order is first-seen so
 * color assignment stays stable across re-renders / theme switches.
 */
export function foldChannelStack(
  points: CostStackPoint[],
  otherLabel: string,
  maxSeries = 8
): FoldedStackResult {
  if (points.length === 0) return { data: [], domain: [] }

  const totals = new Map<string, number>()
  const firstSeenOrder: string[] = []
  for (const point of points) {
    if (!totals.has(point.channel_name)) firstSeenOrder.push(point.channel_name)
    totals.set(
      point.channel_name,
      (totals.get(point.channel_name) || 0) + (Number(point.cost_cny) || 0)
    )
  }

  const shouldFold = firstSeenOrder.length > maxSeries
  let keep: Set<string>
  if (shouldFold) {
    const ranked = Array.from(totals.entries()).sort((a, b) => b[1] - a[1])
    keep = new Set(ranked.slice(0, maxSeries).map(([name]) => name))
  } else {
    keep = new Set(firstSeenOrder)
  }

  const domain = firstSeenOrder.filter((name) => keep.has(name))
  if (shouldFold) domain.push(otherLabel)

  const byDate = new Map<string, Map<string, number>>()
  for (const point of points) {
    const key = keep.has(point.channel_name) ? point.channel_name : otherLabel
    if (!byDate.has(point.date)) byDate.set(point.date, new Map())
    const bucket = byDate.get(point.date)!
    bucket.set(key, (bucket.get(key) || 0) + (Number(point.cost_cny) || 0))
  }

  const dates = Array.from(byDate.keys()).sort()
  const data: FoldedStackPoint[] = []
  for (const date of dates) {
    const bucket = byDate.get(date)!
    for (const channel of domain) {
      data.push({
        date,
        channel,
        cost_cny: Number((bucket.get(channel) || 0).toFixed(2)),
      })
    }
  }

  return { data, domain }
}
