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
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { formatNumber } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import type {
  CostBreakdownRow,
  CostDimension,
  CostDimensionRow,
  CostMoney,
} from '../types'
import { formatCny, formatRate, formatUsd } from '../lib'
import { EditRatioDialog } from './edit-ratio-dialog'

const PAGE_SIZE = 20
const NON_CHANNEL_COLSPAN = 11
const CHANNEL_COLSPAN = 10

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

function ProfitCell({ value }: { value: number }) {
  return (
    <MoneyCell className={value >= 0 ? 'text-success' : 'text-destructive'}>
      {formatCny(value)}
    </MoneyCell>
  )
}

type IdentityField = 'username' | 'model_name' | 'channel'

function getBreakdownIdentityFields(
  dim: CostDimension,
  breakdown: CostBreakdownRow[]
): IdentityField[] {
  const fields: IdentityField[] = []
  if (dim !== 'users' && breakdown.some((row) => row.username != null)) {
    fields.push('username')
  }
  if (dim !== 'models' && breakdown.some((row) => row.model_name != null)) {
    fields.push('model_name')
  }
  if (
    dim !== 'channels' &&
    breakdown.some((row) => row.channel_name != null || row.channel_id != null)
  ) {
    fields.push('channel')
  }
  return fields
}

function BreakdownTable({
  dim,
  breakdown,
  truncated,
}: {
  dim: CostDimension
  breakdown: CostBreakdownRow[]
  truncated?: number
}) {
  const { t } = useTranslation()
  const identityFields = getBreakdownIdentityFields(dim, breakdown)
  const identityHeaders: Record<IdentityField, string> = {
    username: t('Username'),
    model_name: t('Model'),
    channel: t('Channel'),
  }

  return (
    <div className='bg-muted/30 flex flex-col gap-1.5 p-2'>
      <StaticDataTable<CostBreakdownRow>
        columns={[
          ...identityFields.map((field) => ({
            id: field,
            header: identityHeaders[field],
            cell: (row: CostBreakdownRow) =>
              field === 'username'
                ? row.username
                : field === 'model_name'
                  ? row.model_name
                  : row.channel_name
                    ? `${row.channel_name} (#${row.channel_id})`
                    : row.channel_id != null
                      ? `#${row.channel_id}`
                      : '-',
          })),
          {
            id: 'revenue_usd',
            header: t('Revenue $'),
            className: 'text-right',
            cellClassName: 'text-right tabular-nums',
            cell: (row: CostBreakdownRow) => formatUsd(row.revenue_usd),
          },
          {
            id: 'cost_cny',
            header: t('Cost ¥'),
            className: 'text-right',
            cellClassName: 'text-right tabular-nums',
            cell: (row: CostBreakdownRow) => formatCny(row.cost_cny),
          },
          {
            id: 'profit_cny',
            header: t('Profit ¥'),
            className: 'text-right',
            cellClassName: (row: CostBreakdownRow) =>
              cn(
                'text-right tabular-nums',
                row.profit_cny >= 0 ? 'text-success' : 'text-destructive'
              ),
            cell: (row: CostBreakdownRow) => formatCny(row.profit_cny),
          },
          {
            id: 'profit_rate',
            header: t('Rate'),
            className: 'text-right',
            cellClassName: 'text-right tabular-nums',
            cell: (row: CostBreakdownRow) => formatRate(row.profit_rate),
          },
        ]}
        data={breakdown}
        tableClassName='text-xs'
      />
      {Boolean(truncated) && (
        <p className='text-muted-foreground px-1 text-xs'>
          {t('… and {{n}} more rows omitted', { n: truncated })}
        </p>
      )}
    </div>
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
  return (
    <div className='flex flex-col'>
      <span>{row.channel_name || t('Unnamed channel')}</span>
      <span className='text-muted-foreground text-xs'>#{row.channel_id}</span>
    </div>
  )
}

function RatioCell({ row }: { row: CostDimensionRow }) {
  const { t } = useTranslation()
  if (row.channel_id == null) return null
  return (
    <div className='flex items-center justify-end gap-1.5'>
      {row.priced ? (
        <span className='tabular-nums'>{row.cost_ratio}</span>
      ) : (
        <Badge className='bg-warning/10 text-warning border-transparent'>
          {t('Not set')}
        </Badge>
      )}
      <EditRatioDialog
        channelId={row.channel_id}
        channelName={row.channel_name || `#${row.channel_id}`}
        currentRatio={row.cost_ratio ?? 0}
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
  const [expanded, setExpanded] = useState<Set<string | number>>(new Set())

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

  const toggleExpanded = (key: string | number) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
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
              <TableHead className='text-right'>{t('Revenue $')}</TableHead>
              {dim === 'channels' ? (
                <TableHead className='text-right'>{t('List $')}</TableHead>
              ) : (
                <TableHead className='text-right'>{t('Revenue ¥')}</TableHead>
              )}
              <TableHead className='text-right'>{t('Cost ¥')}</TableHead>
              <TableHead className='text-right'>{t('Profit ¥')}</TableHead>
              <TableHead className='text-right'>{t('Rate')}</TableHead>
              {dim !== 'channels' && (
                <>
                  <TableHead className='text-right'>
                    {t('Input Tokens')}
                  </TableHead>
                  <TableHead className='text-right'>
                    {t('Output Tokens')}
                  </TableHead>
                </>
              )}
              {dim === 'channels' && (
                <TableHead className='text-right'>{t('Users')}</TableHead>
              )}
              <TableHead className='text-right'>{t('Requests')}</TableHead>
              {dim !== 'channels' && (
                <TableHead className='text-right'>{t('Refund $')}</TableHead>
              )}
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              Array.from({ length: 5 }).map((_, index) => (
                <TableRow key={`skeleton-${index}`}>
                  <TableCell
                    colSpan={dim === 'channels' ? CHANNEL_COLSPAN : NON_CHANNEL_COLSPAN}
                  >
                    <Skeleton className='h-6 w-full' />
                  </TableCell>
                </TableRow>
              ))
            ) : items.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={dim === 'channels' ? CHANNEL_COLSPAN : NON_CHANNEL_COLSPAN}
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
                          <RatioCell row={row} />
                        </TableCell>
                      )}
                      <MoneyCell>{formatUsd(row.revenue_usd)}</MoneyCell>
                      {dim === 'channels' ? (
                        <MoneyCell>{formatUsd(row.list_usd)}</MoneyCell>
                      ) : (
                        <MoneyCell>{formatCny(row.revenue_cny)}</MoneyCell>
                      )}
                      <MoneyCell>{formatCny(row.cost_cny)}</MoneyCell>
                      <ProfitCell value={row.profit_cny} />
                      <MoneyCell>{formatRate(row.profit_rate)}</MoneyCell>
                      {dim !== 'channels' && (
                        <>
                          <MoneyCell>
                            {formatNumber(row.prompt_tokens)}
                          </MoneyCell>
                          <MoneyCell>
                            {formatNumber(row.completion_tokens)}
                          </MoneyCell>
                        </>
                      )}
                      {dim === 'channels' && (
                        <MoneyCell>{formatNumber(row.user_count)}</MoneyCell>
                      )}
                      <MoneyCell>{formatNumber(row.request_count)}</MoneyCell>
                      {dim !== 'channels' && (
                        <MoneyCell>{formatUsd(row.refund_usd)}</MoneyCell>
                      )}
                    </TableRow>
                    {hasBreakdown && isExpanded && (
                      <TableRow>
                        <TableCell
                          colSpan={
                            dim === 'channels' ? CHANNEL_COLSPAN : NON_CHANNEL_COLSPAN
                          }
                          className='p-0'
                        >
                          <BreakdownTable
                            dim={dim}
                            breakdown={row.breakdown ?? []}
                            truncated={row.breakdown_truncated}
                          />
                        </TableCell>
                      </TableRow>
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
                <MoneyCell className='font-semibold'>
                  {formatUsd(summary.revenue_usd)}
                </MoneyCell>
                {dim === 'channels' ? (
                  <MoneyCell className='font-semibold'>
                    {formatUsd(summary.list_usd)}
                  </MoneyCell>
                ) : (
                  <MoneyCell className='font-semibold'>
                    {formatCny(summary.revenue_cny)}
                  </MoneyCell>
                )}
                <MoneyCell className='font-semibold'>
                  {formatCny(summary.cost_cny)}
                </MoneyCell>
                <ProfitCell value={summary.profit_cny} />
                <MoneyCell className='font-semibold'>
                  {formatRate(summary.profit_rate)}
                </MoneyCell>
                {dim !== 'channels' && (
                  <>
                    <MoneyCell className='font-semibold'>
                      {formatNumber(summary.prompt_tokens)}
                    </MoneyCell>
                    <MoneyCell className='font-semibold'>
                      {formatNumber(summary.completion_tokens)}
                    </MoneyCell>
                  </>
                )}
                {dim === 'channels' && <TableCell />}
                <MoneyCell className='font-semibold'>
                  {formatNumber(summary.request_count)}
                </MoneyCell>
                {dim !== 'channels' && (
                  <MoneyCell className='font-semibold'>
                    {formatUsd(summary.refund_usd)}
                  </MoneyCell>
                )}
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
