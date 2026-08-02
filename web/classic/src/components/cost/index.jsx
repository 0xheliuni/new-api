/*
Copyright (C) 2025 QuantumNous

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

import React from 'react';
import { Card, Form, Button, Banner } from '@douyinfe/semi-ui';
import CardPro from '../common/ui/CardPro';
import CostCharts from './CostCharts';
import CostTables from './CostTables';
import { useCostData } from './useCostData';
import { createCardProPagination } from '../../helpers/utils';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import { DATE_RANGE_PRESETS } from '../../constants/console.constants';

const profitStyle = (v) => ({
  color:
    Number(v || 0) >= 0
      ? 'var(--semi-color-success)'
      : 'var(--semi-color-danger)',
});

const CostAccounting = () => {
  const costData = useCostData();
  const {
    t,
    dateRange,
    applyDateRange,
    activeTab,
    setActiveTab,
    activePage,
    pageSize,
    handlePageChange,
    handlePageSizeChange,
    overview,
    overviewLoading,
    pageData,
    tableLoading,
    refresh,
  } = costData;
  const isMobile = useIsMobile();

  const totals = overview?.totals || {};
  const unpricedCount = overview?.unpriced_channel_count || 0;

  const statsArea = (
    <div className='flex flex-col gap-3 mb-2'>
      {unpricedCount > 0 && (
        <Banner
          type='warning'
          description={t('{{count}} 个渠道未填成本倍率，其成本按 0 计', {
            count: unpricedCount,
          })}
          className='!rounded-lg'
          closeIcon={null}
        />
      )}
      <div className='grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4'>
        <Card bordered className='!rounded-2xl'>
          <div className='text-xs text-gray-500 mb-1'>{t('收入')}</div>
          <div className='text-lg font-semibold'>
            ${Number(totals.revenue_usd || 0).toFixed(2)}
          </div>
          <div className='text-xs text-gray-500'>
            ¥{Number(totals.revenue_cny || 0).toFixed(2)}
          </div>
          {Number(totals.refund_usd || 0) > 0 && (
            <div
              className='text-xs mt-1'
              style={{ color: 'var(--semi-color-danger)' }}
            >
              {t('退款')} -${Number(totals.refund_usd || 0).toFixed(2)}
            </div>
          )}
        </Card>
        <Card bordered className='!rounded-2xl'>
          <div className='text-xs text-gray-500 mb-1'>{t('成本')}</div>
          <div className='text-lg font-semibold'>
            ¥{Number(totals.cost_cny || 0).toFixed(2)}
          </div>
        </Card>
        <Card bordered className='!rounded-2xl'>
          <div className='text-xs text-gray-500 mb-1'>{t('利润')}</div>
          <div
            className='text-lg font-semibold'
            style={profitStyle(totals.profit_cny)}
          >
            ¥{Number(totals.profit_cny || 0).toFixed(2)}
          </div>
        </Card>
        <Card bordered className='!rounded-2xl'>
          <div className='text-xs text-gray-500 mb-1'>{t('利润率')}</div>
          <div
            className='text-lg font-semibold'
            style={profitStyle(totals.profit_rate)}
          >
            {(Number(totals.profit_rate || 0) * 100).toFixed(2)}%
          </div>
        </Card>
      </div>
    </div>
  );

  const searchArea = (
    <Form
      initValues={{ dateRange }}
      onSubmit={(values) => applyDateRange(values.dateRange)}
      allowEmpty={true}
      layout='vertical'
      trigger='change'
      stopValidateWithError={false}
    >
      <div className='flex flex-col sm:flex-row gap-2 items-start sm:items-end'>
        <Form.DatePicker
          field='dateRange'
          type='dateTimeRange'
          className='w-full sm:w-auto'
          placeholder={[t('开始时间'), t('结束时间')]}
          presets={DATE_RANGE_PRESETS.map((preset) => ({
            text: t(preset.text),
            start: preset.start(),
            end: preset.end(),
          }))}
          showClear
          pure
          size='small'
        />
        <Button type='tertiary' htmlType='submit' loading={overviewLoading} size='small'>
          {t('查询')}
        </Button>
      </div>
    </Form>
  );

  return (
    <CardPro
      type='type2'
      statsArea={statsArea}
      searchArea={searchArea}
      paginationArea={createCardProPagination({
        currentPage: activePage,
        pageSize,
        total: pageData?.total || 0,
        onPageChange: handlePageChange,
        onPageSizeChange: handlePageSizeChange,
        isMobile,
        t,
      })}
      t={t}
    >
      <CostCharts trend={overview?.trend} costStack={overview?.cost_stack} t={t} />
      <CostTables
        t={t}
        activeTab={activeTab}
        setActiveTab={setActiveTab}
        pageData={pageData}
        tableLoading={tableLoading}
        activePage={activePage}
        pageSize={pageSize}
        handlePageChange={handlePageChange}
        handlePageSizeChange={handlePageSizeChange}
        onRatioUpdated={refresh}
      />
    </CardPro>
  );
};

export default CostAccounting;
