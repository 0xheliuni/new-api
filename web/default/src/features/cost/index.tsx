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
import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { dateToUnixTimestamp, getRollingDateRange } from '@/lib/time'
import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getCostOverview } from './api'
import { CostCharts } from './components/cost-charts'
import { CostDimensionTable } from './components/cost-dimension-table'
import { CostFilter, type CostFilterValue } from './components/cost-filter'
import { CostKpiCards } from './components/cost-kpi-cards'
import type { CostDimension } from './types'

const route = getRouteApi('/_authenticated/cost/')

export function CostAccounting() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()

  const defaultRange = useMemo(() => getRollingDateRange(7), [])
  const start = search.start ?? dateToUnixTimestamp(defaultRange.start)
  const end = search.end ?? dateToUnixTimestamp(defaultRange.end)
  const tab: CostDimension = search.tab ?? 'users'
  const page = search.p ?? 1

  const handleRangeChange = (next: CostFilterValue) => {
    navigate({
      search: (prev) => ({ ...prev, start: next.start, end: next.end, p: undefined }),
    })
  }

  const handleTabChange = (nextTab: string) => {
    navigate({
      search: (prev) => ({
        ...prev,
        tab: nextTab as CostDimension,
        p: undefined,
      }),
    })
  }

  const handlePageChange = (nextPage: number) => {
    navigate({
      search: (prev) => ({ ...prev, p: nextPage <= 1 ? undefined : nextPage }),
    })
  }

  const overviewQuery = useQuery({
    queryKey: ['cost', 'overview', start, end],
    queryFn: () =>
      getCostOverview({ start_timestamp: start, end_timestamp: end }),
  })

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Cost Accounting')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <CostFilter value={{ start, end }} onChange={handleRangeChange} />
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          <CostKpiCards
            overview={overviewQuery.data}
            loading={overviewQuery.isLoading}
          />

          <CostCharts
            overview={overviewQuery.data}
            loading={overviewQuery.isLoading}
          />

          <Tabs value={tab} onValueChange={handleTabChange}>
            <TabsList>
              <TabsTrigger value='users'>{t('Users')}</TabsTrigger>
              <TabsTrigger value='models'>{t('Models')}</TabsTrigger>
              <TabsTrigger value='channels'>{t('Channels')}</TabsTrigger>
            </TabsList>

            <TabsContent value='users' className='mt-3'>
              <CostDimensionTable
                dim='users'
                start={start}
                end={end}
                page={tab === 'users' ? page : 1}
                onPageChange={handlePageChange}
              />
            </TabsContent>
            <TabsContent value='models' className='mt-3'>
              <CostDimensionTable
                dim='models'
                start={start}
                end={end}
                page={tab === 'models' ? page : 1}
                onPageChange={handlePageChange}
              />
            </TabsContent>
            <TabsContent value='channels' className='mt-3'>
              <CostDimensionTable
                dim='channels'
                start={start}
                end={end}
                page={tab === 'channels' ? page : 1}
                onPageChange={handlePageChange}
              />
            </TabsContent>
          </Tabs>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
