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
import { getDashboardChartColors } from '@/features/dashboard/lib/charts'

import {
  foldChannelStack,
  formatBucketLabel,
  formatBucketTooltip,
  sampleBucketTicks,
} from './lib'
import type { CostGranularity, CostStackPoint, CostTrendPoint } from './types'

type Datum = Record<string, unknown>

export interface CostTooltipLine {
  key: string
  value: string | number
  datum?: Datum
}

interface CostChartInput {
  granularity: CostGranularity
  /** Convert a CNY amount (computed with the query rate) into the display currency. */
  toDisplay: (cny: number) => number
  /** Format an amount already in the display currency. */
  formatMoney: (value: number) => string
}

/**
 * Bottom time axis shared by both charts. Hour ranges carry up to ~49 buckets;
 * blanking the labels between sampled ticks keeps the axis readable without
 * dropping points.
 */
function timeAxis(buckets: string[], granularity: CostGranularity) {
  const ticks = sampleBucketTicks(buckets)
  return {
    orient: 'bottom',
    label: {
      formatMethod: (value: unknown) => {
        const bucket = String(value)
        return ticks.has(bucket) ? formatBucketLabel(bucket, granularity) : ''
      },
    },
  }
}

/**
 * Revenue / cost / profit over time, styled like the dashboard's Model Call
 * Analytics trend: smooth lines over a light area fill, no point markers, and
 * a legend that isolates one series on click.
 */
export function buildCostTrendSpec(
  input: CostChartInput & {
    points: CostTrendPoint[]
    labels: { revenue: string; cost: string; profit: string }
  }
) {
  const values: Array<{ date: string; series: string; value: number }> = []
  for (const point of input.points) {
    values.push(
      {
        date: point.date,
        series: input.labels.revenue,
        value: input.toDisplay(point.revenue_cny),
      },
      {
        date: point.date,
        series: input.labels.cost,
        value: input.toDisplay(point.cost_cny),
      },
      {
        date: point.date,
        series: input.labels.profit,
        value: input.toDisplay(point.profit_cny),
      }
    )
  }
  const palette = getDashboardChartColors(3)
  // The dashboard palette's first two slots are both blue, which would make
  // revenue and cost hard to tell apart; take its blue, orange and green.
  const range = [palette[0], palette[2], palette[3]]
  const seriesLine = {
    key: (datum: Datum) => String(datum?.series),
    value: (datum: Datum) => input.formatMoney(Number(datum?.value) || 0),
  }
  const title = {
    value: (datum: Datum) =>
      formatBucketTooltip(String(datum?.date), input.granularity),
  }

  return {
    type: 'area',
    data: [{ id: 'costTrend', values }],
    xField: 'date',
    yField: 'value',
    seriesField: 'series',
    stack: false,
    legends: { visible: true, selectMode: 'single' },
    color: {
      type: 'ordinal',
      domain: [input.labels.revenue, input.labels.cost, input.labels.profit],
      range,
    },
    area: { style: { fillOpacity: 0.08, curveType: 'monotone' } },
    line: { style: { lineWidth: 2, curveType: 'monotone' } },
    point: { visible: false },
    crosshair: { xField: { visible: true } },
    axes: [
      timeAxis(
        input.points.map((p) => p.date),
        input.granularity
      ),
      {
        orient: 'left',
        label: {
          formatMethod: (value: unknown) =>
            input.formatMoney(Number(value) || 0),
        },
      },
    ],
    // Without an explicit content pattern VChart falls back to the raw
    // measure value, which renders full float precision.
    tooltip: {
      dimension: { title, content: [seriesLine] },
      mark: { title, content: [seriesLine] },
    },
    background: { fill: 'transparent' },
    animation: true,
  }
}

/**
 * Dimension tooltip lines for the channel stack, like the dashboard's quota
 * distribution: the bucket total first, then channels by amount, "Other" last.
 * Values arrive as numbers and leave formatted.
 */
export function stackTooltipLines(
  lines: CostTooltipLine[],
  options: {
    otherLabel: string
    totalLabel: string
    formatMoney: (value: number) => string
  }
): CostTooltipLine[] {
  const channels = lines
    .filter((line) => line.key !== options.otherLabel)
    .sort((a, b) => (Number(b.value) || 0) - (Number(a.value) || 0))
  const other = lines.filter((line) => line.key === options.otherLabel)
  const total = lines.reduce((sum, line) => sum + (Number(line.value) || 0), 0)
  return [
    { key: options.totalLabel, value: options.formatMoney(total) },
    ...[...channels, ...other].map((line) => ({
      ...line,
      value: options.formatMoney(Number(line.value) || 0),
    })),
  ]
}

/**
 * Cost per channel over time as stacked bars. Channels beyond the top 8 fold
 * into "Other" so the dashboard palette never repeats a color.
 */
export function buildCostStackSpec(
  input: CostChartInput & {
    points: CostStackPoint[]
    otherLabel: string
    totalLabel: string
    /** Stroke matching the surface, drawing a 2px gap between stacked segments. */
    gapStroke: string
  }
) {
  const folded = foldChannelStack(input.points, input.otherLabel, 8)
  const values = folded.data.map((d) => ({
    ...d,
    cost_display: input.toDisplay(d.cost_cny),
  }))
  const buckets = [...new Set(folded.data.map((d) => d.date))].sort()
  const title = {
    value: (datum: Datum) =>
      formatBucketTooltip(String(datum?.date), input.granularity),
  }

  return {
    type: 'bar',
    data: [{ id: 'costStack', values }],
    xField: 'date',
    yField: 'cost_display',
    seriesField: 'channel',
    stack: true,
    legends: { visible: true },
    color: {
      type: 'ordinal',
      domain: folded.domain,
      range: getDashboardChartColors(folded.domain.length),
    },
    bar: {
      style: { stroke: input.gapStroke, lineWidth: 2 },
      state: { hover: { stroke: input.gapStroke, lineWidth: 2 } },
    },
    axes: [
      timeAxis(buckets, input.granularity),
      {
        orient: 'left',
        label: {
          formatMethod: (value: unknown) =>
            input.formatMoney(Number(value) || 0),
        },
      },
    ],
    tooltip: {
      mark: {
        title,
        content: [
          {
            key: (datum: Datum) => String(datum?.channel),
            value: (datum: Datum) =>
              input.formatMoney(Number(datum?.cost_display) || 0),
          },
        ],
      },
      dimension: {
        title,
        content: [
          {
            key: (datum: Datum) => String(datum?.channel),
            value: (datum: Datum) => Number(datum?.cost_display) || 0,
          },
        ],
        updateContent: (lines: CostTooltipLine[]) =>
          stackTooltipLines(lines, {
            otherLabel: input.otherLabel,
            totalLabel: input.totalLabel,
            formatMoney: input.formatMoney,
          }),
      },
    },
    background: { fill: 'transparent' },
    animation: true,
  }
}
