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
import { Fragment, type ReactNode, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { formatNumber } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { StaticDataTable } from '@/components/data-table'
import { getCostByDimension } from '../api'
import { DEFAULT_EXCHANGE_RATE } from './cost-filter'
import type {
  CostBreakdownRow,
  CostChannelSubSupplier,
  CostDimension,
  CostDimensionRow,
  CostMoney,
} from '../types'
import {
  formatAvgTtft,
  formatCny,
  formatDiscountLabel,
  formatRate,
  formatUsd,
  mergeBreakdown,
  type CostBreakdownGroupBy,
} from '../lib'
import { EditRatioDialog } from './edit-ratio-dialog'
import { TokensDetailDialog } from './tokens-detail-dialog'

const PAGE_SIZE = 20
const route = getRouteApi('/_authenticated/cost/')

interface CostDimensionTableProps {
  dim: CostDimension
  start: number
  end: number
  page: number
  onPageChange: (page: number) => void
  username?: string
  channel?: number
  modelName?: string
  exchangeRate?: number
}

function rowKey(dim: CostDimension, row: CostDimensionRow, index: number) {
  if (dim === 'users') return row.user_id ?? `u-${index}`
  if (dim === 'models') return row.model_name ?? `m-${index}`
  return row.channel_id ?? `c-${index}`
}

function MoneyCell({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <TableCell className={cn('text-right tabular-nums', className)}>
      {children}
    </TableCell>
  )
}

/**
 * Metric column definitions shared verbatim between the top-level dimension
 * rows and their breakdown sub-rows, so both render the exact same set of
 * columns in the exact same order (v2 requirement).
 */
interface MetricColumnSpec {
  id: string
  header: ReactNode
  cellClassName?: string | ((row: CostMoney) => string | undefined)
  cell: (row: CostMoney) => ReactNode
}

function renderRevenueCell(row: CostMoney) {
  return (
    <div className='flex flex-col items-end leading-tight'>
      <span>{formatUsd(row.revenue_usd)}</span>
      <span className='text-muted-foreground text-[11px]'>
        {formatCny(row.revenue_cny)}
      </span>
    </div>
  )
}

function buildMetricColumns(
  t: ReturnType<typeof useTranslation>['t']
): MetricColumnSpec[] {
  return [
    { id: 'cost_cny', header: t('Cost ¥'), cell: (row) => formatCny(row.cost_cny) },
    { id: 'revenue', header: t('Revenue'), cell: renderRevenueCell },
    {
      id: 'profit_cny',
      header: t('Profit ¥'),
      cellClassName: (row) =>
        row.profit_cny >= 0 ? 'text-success' : 'text-destructive',
      cell: (row) => formatCny(row.profit_cny),
    },
    { id: 'list_usd', header: t('List $'), cell: (row) => formatUsd(row.list_usd) },
    {
      id: 'request_count',
      header: t('Requests'),
      cell: (row) => formatNumber(row.request_count),
    },
    { id: 'profit_rate', header: t('Rate'), cell: (row) => formatRate(row.profit_rate) },
    {
      id: 'total_tokens',
      header: t('Total Tokens'),
      cell: (row) => (
        <TokensDetailDialog
          promptTokens={row.prompt_tokens}
          completionTokens={row.completion_tokens}
          cacheReadTokens={row.cache_read_tokens}
          cacheCreationTokens={row.cache_creation_tokens}
          totalTokens={row.total_tokens}
        />
      ),
    },
    {
      id: 'success_rate',
      header: t('Success rate'),
      cell: (row) => formatRate(row.success_rate),
    },
    {
      id: 'cache_rate',
      header: t('Cache Rate'),
      cell: (row) => formatRate(row.cache_rate),
    },
    {
      id: 'avg_ttft_ms',
      header: t('Average TTFT'),
      cell: (row) => formatAvgTtft(row),
    },
  ]
}

type IdentityField = CostBreakdownGroupBy
type ViewMode = 'detail' | IdentityField

const IDENTITY_MERGE_LABEL_KEY: Record<IdentityField, string> = {
  username: 'Merge users',
  model_name: 'Merge models',
  channel: 'Merge channels',
}

/**
 * Fixed per-dim order for the two breakdown identity columns (v3: always
 * both columns, "—" for whichever is collapsed by the merge view — this
 * keeps sub-row column widths stable whether the parent row is in detail or
 * merged mode).
 */
const BREAKDOWN_IDENTITY_ORDER: Record<CostDimension, [IdentityField, IdentityField]> = {
  users: ['channel', 'model_name'],
  models: ['username', 'channel'],
  channels: ['username', 'model_name'],
}

/**
 * Fixed per-dim merge options (each collapses one identity field, grouping
 * by the other). Order matches the product spec: users -> Merge
 * models/Merge channels; models -> Merge users/Merge channels; channels ->
 * Merge users/Merge models.
 */
const MERGE_OPTIONS_BY_DIM: Record<
  CostDimension,
  { groupBy: IdentityField; collapsed: IdentityField }[]
> = {
  users: [
    { groupBy: 'channel', collapsed: 'model_name' },
    { groupBy: 'model_name', collapsed: 'channel' },
  ],
  models: [
    { groupBy: 'channel', collapsed: 'username' },
    { groupBy: 'username', collapsed: 'channel' },
  ],
  channels: [
    { groupBy: 'model_name', collapsed: 'username' },
    { groupBy: 'username', collapsed: 'model_name' },
  ],
}

/** Per-row compact merge-view Select, rendered in the parent row's Actions column. */
function MergeViewSelect({
  dim,
  viewMode,
  onViewModeChange,
}: {
  dim: CostDimension
  viewMode: ViewMode
  onViewModeChange: (mode: ViewMode) => void
}) {
  const { t } = useTranslation()
  const mergeOptions = MERGE_OPTIONS_BY_DIM[dim]
  return (
    <Select
      value={viewMode}
      onValueChange={(value) => onViewModeChange(value as ViewMode)}
      items={[
        { value: 'detail', label: t('Detail') },
        ...mergeOptions.map((option) => ({
          value: option.groupBy,
          label: t(IDENTITY_MERGE_LABEL_KEY[option.collapsed]),
        })),
      ]}
    >
      <SelectTrigger size='sm' className='h-7 w-28 text-xs'>
        <SelectValue />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectGroup>
          <SelectItem value='detail'>{t('Detail')}</SelectItem>
          {mergeOptions.map((option) => (
            <SelectItem key={option.groupBy} value={option.groupBy}>
              {t(IDENTITY_MERGE_LABEL_KEY[option.collapsed])}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

/** `true` when the merge view has collapsed this identity field away. */
function isFieldCollapsed(field: IdentityField, viewMode: ViewMode): boolean {
  return viewMode !== 'detail' && field !== viewMode
}

function BreakdownIdentityValue({
  field,
  row,
  collapsed,
}: {
  field: IdentityField
  row: CostBreakdownRow
  collapsed: boolean
}) {
  const { t } = useTranslation()
  if (collapsed) return <span className='text-muted-foreground'>—</span>
  if (field === 'username') return <>{row.username || '-'}</>
  if (field === 'model_name') return <>{row.model_name || '-'}</>
  // channel_id falsy (undefined/0) => no channel selected on the underlying
  // logs, not a real channel.
  if (!row.channel_id) return <>{t('Unknown channel')}</>
  return <>{row.channel_name ? `${row.channel_name} (#${row.channel_id})` : `#${row.channel_id}`}</>
}

/**
 * Both identity columns for a dim, always rendered (task v3): the two
 * "other" dimensions per dim (e.g. users dim -> channel, model). Rendered as
 * a fixed 2-column grid inside one <td> so widths stay put across
 * detail/merged views.
 */
function BreakdownIdentityCell({
  dim,
  row,
  viewMode,
}: {
  dim: CostDimension
  row: CostBreakdownRow
  viewMode: ViewMode
}) {
  const { t } = useTranslation()
  const [fieldA, fieldB] = BREAKDOWN_IDENTITY_ORDER[dim]
  const identityLabels: Record<IdentityField, string> = {
    username: t('Username'),
    model_name: t('Model'),
    channel: t('Channel'),
  }
  return (
    <div className='grid grid-cols-2 gap-3'>
      {[fieldA, fieldB].map((field) => (
        <div key={field} className='flex flex-col gap-0.5'>
          <span className='text-muted-foreground text-[11px]'>
            {identityLabels[field]}
          </span>
          <BreakdownIdentityValue
            field={field}
            row={row}
            collapsed={isFieldCollapsed(field, viewMode)}
          />
        </div>
      ))}
    </div>
  )
}

/**
 * Breakdown sub-rows for an expanded parent row, rendered as additional
 * <tr>s in the SAME table as the parent so metric columns line up exactly
 * (v3 requirement). The leading identity+ratio columns are merged into one
 * cell holding both breakdown identity fields; the Actions column carries
 * the "view channel only" action when applicable.
 */
function BreakdownRows({
  dim,
  row,
  metricColumns,
  viewMode,
  onViewChannel,
  totalColSpan,
}: {
  dim: CostDimension
  row: CostDimensionRow
  metricColumns: MetricColumnSpec[]
  viewMode: ViewMode
  onViewChannel: (channelId: number) => void
  totalColSpan: number
}) {
  const { t } = useTranslation()
  const rawBreakdown = row.breakdown ?? []
  const breakdown =
    viewMode === 'detail' ? rawBreakdown : mergeBreakdown(rawBreakdown, viewMode)
  // Reclaim the Ratio column's width for channels-dim breakdown rows — it
  // has no per-row ratio editor, so folding it into the identity cell gives
  // the two identity fields more room without breaking column alignment.
  const identityColSpan = 1 + (dim === 'channels' ? 1 : 0)
  // Breakdown rows for the "channels" dim never carry a `channel` identity
  // field (the parent row already is the channel), so there's no "view
  // channel only" action to show there.
  const channelActionAvailable = dim !== 'channels'

  return (
    <>
      {dim === 'channels' && row.sub_suppliers && row.sub_suppliers.length > 0 && (
        <TableRow className='hover:bg-transparent'>
          <TableCell colSpan={totalColSpan} className='bg-muted/30 p-0'>
            <div className='flex flex-col gap-1.5 p-2'>
              <p className='text-muted-foreground text-xs'>
                {t('Report cost uses the channel-level pricing')}
              </p>
              <StaticDataTable<CostChannelSubSupplier>
                columns={[
                  {
                    id: 'name',
                    header: t('Sub-supplier'),
                    cell: (supplier) => supplier.name,
                  },
                  {
                    id: 'cost_ratio',
                    header: t('Ratio'),
                    className: 'text-right',
                    cellClassName: 'text-right tabular-nums',
                    cell: (supplier) => supplier.cost_ratio ?? '-',
                  },
                ]}
                data={row.sub_suppliers}
                tableClassName='text-xs'
              />
            </div>
          </TableCell>
        </TableRow>
      )}

      {breakdown.map((bRow, index) => (
        <TableRow key={index} className='bg-muted/20 hover:bg-muted/30'>
          <TableCell />
          <TableCell colSpan={identityColSpan}>
            <BreakdownIdentityCell dim={dim} row={bRow} viewMode={viewMode} />
          </TableCell>
          {metricColumns.map((col) => (
            <MoneyCell
              key={col.id}
              className={
                typeof col.cellClassName === 'function'
                  ? col.cellClassName(bRow)
                  : col.cellClassName
              }
            >
              {col.cell(bRow)}
            </MoneyCell>
          ))}
          <TableCell>
            {channelActionAvailable &&
              !isFieldCollapsed('channel', viewMode) &&
              Boolean(bRow.channel_id) && (
                <Button
                  type='button'
                  variant='link'
                  size='xs'
                  className='h-auto p-0 text-xs'
                  onClick={() => onViewChannel(bRow.channel_id!)}
                >
                  {t('View channel only')}
                </Button>
              )}
          </TableCell>
        </TableRow>
      ))}

      {Boolean(row.breakdown_truncated) && (
        <TableRow className='hover:bg-transparent'>
          <TableCell
            colSpan={totalColSpan}
            className='text-muted-foreground p-2 text-xs'
          >
            {t('… and {{n}} more rows omitted', { n: row.breakdown_truncated })}
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

function DimensionCell({
  dim,
  row,
}: {
  dim: CostDimension
  row: CostDimensionRow
}) {
  const { t } = useTranslation()
  if (dim === 'users') return <>{row.username || '-'}</>
  if (dim === 'models') return <>{row.model_name || '-'}</>
  // channel_id is omitted (omitempty) by the backend when it's 0, i.e. "no
  // channel selected" on the underlying logs — not a real, unpriced channel.
  // Render it distinctly instead of "Unnamed channel #undefined".
  const isUnknownChannel = !row.channel_id
  return (
    <div className='flex flex-col gap-0.5'>
      <div className='flex items-center gap-1.5'>
        <span>
          {isUnknownChannel
            ? t('Unknown channel')
            : row.channel_name || t('Unnamed channel')}
        </span>
        {row.is_aggregator && (
          <Badge className='bg-info/10 text-info border-transparent'>
            {t('Aggregator')}
          </Badge>
        )}
      </div>
      <span className='text-muted-foreground text-xs'>
        #{isUnknownChannel ? 0 : row.channel_id}
      </span>
    </div>
  )
}

function RatioCell({
  row,
  exchangeRate,
}: {
  row: CostDimensionRow
  exchangeRate: number
}) {
  const { t } = useTranslation()
  // Falsy (undefined or 0) channel_id means "no channel selected" on the
  // underlying logs, not a real channel — no ratio to edit for it.
  if (!row.channel_id) return null
  const mode = row.cost_mode === 'discount' ? 'discount' : 'ratio'
  return (
    <div className='flex items-center justify-end gap-1.5'>
      {!row.priced ? (
        <Badge className='bg-warning/10 text-warning border-transparent'>
          {t('Not set')}
        </Badge>
      ) : mode === 'discount' ? (
        <span className='tabular-nums'>
          {formatDiscountLabel(row.cost_discount, t)} (≈¥
          {(row.effective_ratio ?? 0).toFixed(2)}/$1)
        </span>
      ) : (
        <span className='tabular-nums'>{row.cost_ratio}</span>
      )}
      <EditRatioDialog
        channelId={row.channel_id}
        channelName={row.channel_name || `#${row.channel_id}`}
        currentRatio={row.cost_ratio ?? 0}
        currentMode={row.cost_mode}
        currentDiscount={row.cost_discount ?? 0}
        exchangeRate={exchangeRate}
      />
    </div>
  )
}

function summaryLabel(dim: CostDimension, t: (key: string) => string) {
  if (dim === 'users') return t('All Users')
  if (dim === 'models') return t('All Models')
  return t('All Channels')
}

export function CostDimensionTable({
  dim,
  start,
  end,
  page,
  onPageChange,
  username,
  channel,
  modelName,
  exchangeRate,
}: CostDimensionTableProps) {
  const { t } = useTranslation()
  const navigate = route.useNavigate()
  const [expanded, setExpanded] = useState<Set<string | number>>(new Set())
  const [viewModeByRow, setViewModeByRow] = useState<
    Map<string | number, ViewMode>
  >(new Map())

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'cost',
      dim,
      start,
      end,
      page,
      username,
      channel,
      modelName,
      exchangeRate,
    ],
    queryFn: () =>
      getCostByDimension(dim, {
        start_timestamp: start,
        end_timestamp: end,
        p: page,
        page_size: PAGE_SIZE,
        username,
        channel,
        model_name: modelName,
        exchange_rate: exchangeRate,
      }),
    placeholderData: keepPreviousData,
  })

  const items = data?.items ?? []
  const total = data?.total ?? 0
  const summary: CostMoney | undefined = data?.summary
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const metricColumns = buildMetricColumns(t)
  // expand toggle + identity + (ratio, channels only) + metrics + actions
  const totalColSpan = 3 + (dim === 'channels' ? 1 : 0) + metricColumns.length

  const toggleExpanded = (key: string | number) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const getViewMode = (key: string | number): ViewMode =>
    viewModeByRow.get(key) ?? 'detail'

  const setViewMode = (key: string | number, mode: ViewMode) => {
    setViewModeByRow((prev) => {
      const next = new Map(prev)
      next.set(key, mode)
      return next
    })
  }

  const handleViewChannel = (channelId: number) => {
    navigate({
      search: (prev) => ({
        ...prev,
        channel: channelId,
        username: undefined,
        model_name: undefined,
        tab: 'channels',
        p: undefined,
      }),
    })
  }

  const identityHeader =
    dim === 'users' ? t('Username') : dim === 'models' ? t('Model') : t('Channel')

  return (
    <div className='flex flex-col gap-2'>
      <div className='overflow-x-auto rounded-lg border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='w-8' />
              <TableHead>{identityHeader}</TableHead>
              {dim === 'channels' && (
                <TableHead className='text-right'>{t('Ratio')}</TableHead>
              )}
              {metricColumns.map((col) => (
                <TableHead key={col.id} className='text-right'>
                  {col.header}
                </TableHead>
              ))}
              <TableHead className='w-28'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              Array.from({ length: 5 }).map((_, index) => (
                <TableRow key={`skeleton-${index}`}>
                  <TableCell colSpan={totalColSpan}>
                    <Skeleton className='h-6 w-full' />
                  </TableCell>
                </TableRow>
              ))
            ) : items.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={totalColSpan}
                  className='text-muted-foreground h-24 text-center'
                >
                  {t('No data available')}
                </TableCell>
              </TableRow>
            ) : (
              items.map((row, index) => {
                const key = rowKey(dim, row, index)
                const hasBreakdown = Boolean(row.breakdown?.length)
                const isExpanded = expanded.has(key)
                return (
                  <Fragment key={key}>
                    <TableRow>
                      <TableCell>
                        {hasBreakdown && (
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            className='size-6'
                            aria-expanded={isExpanded}
                            onClick={() => toggleExpanded(key)}
                          >
                            {isExpanded ? (
                              <ChevronDown className='size-3.5' />
                            ) : (
                              <ChevronRight className='size-3.5' />
                            )}
                          </Button>
                        )}
                      </TableCell>
                      <TableCell>
                        <DimensionCell dim={dim} row={row} />
                      </TableCell>
                      {dim === 'channels' && (
                        <TableCell className='text-right'>
                          <RatioCell
                            row={row}
                            exchangeRate={exchangeRate ?? DEFAULT_EXCHANGE_RATE}
                          />
                        </TableCell>
                      )}
                      {metricColumns.map((col) => (
                        <MoneyCell
                          key={col.id}
                          className={
                            typeof col.cellClassName === 'function'
                              ? col.cellClassName(row)
                              : col.cellClassName
                          }
                        >
                          {col.cell(row)}
                        </MoneyCell>
                      ))}
                      <TableCell>
                        {hasBreakdown && (
                          <MergeViewSelect
                            dim={dim}
                            viewMode={getViewMode(key)}
                            onViewModeChange={(mode) => setViewMode(key, mode)}
                          />
                        )}
                      </TableCell>
                    </TableRow>
                    {hasBreakdown && isExpanded && (
                      <BreakdownRows
                        dim={dim}
                        row={row}
                        metricColumns={metricColumns}
                        viewMode={getViewMode(key)}
                        onViewChannel={handleViewChannel}
                        totalColSpan={totalColSpan}
                      />
                    )}
                  </Fragment>
                )
              })
            )}
          </TableBody>
          {summary && items.length > 0 && (
            <TableFooter>
              <TableRow>
                <TableCell />
                <TableCell className='font-semibold'>
                  {summaryLabel(dim, t)}
                </TableCell>
                {dim === 'channels' && <TableCell />}
                {metricColumns.map((col) => (
                  <MoneyCell
                    key={col.id}
                    className={cn(
                      'font-semibold',
                      typeof col.cellClassName === 'function'
                        ? col.cellClassName(summary)
                        : col.cellClassName
                    )}
                  >
                    {col.cell(summary)}
                  </MoneyCell>
                ))}
                <TableCell />
              </TableRow>
            </TableFooter>
          )}
        </Table>
      </div>

      <div
        className={cn(
          'flex items-center justify-between px-1 transition-opacity',
          isFetching && !isLoading && 'opacity-60'
        )}
      >
        <span className='text-muted-foreground text-xs'>
          {t('Total:')} {formatNumber(total)}
        </span>
        <div className='flex items-center gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={page <= 1}
            onClick={() => onPageChange(page - 1)}
          >
            {t('Previous')}
          </Button>
          <span className='text-xs tabular-nums'>
            {t('Page {{page}} of {{total}}', { page, total: totalPages })}
          </span>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={page >= totalPages}
            onClick={() => onPageChange(page + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      </div>
    </div>
  )
}
