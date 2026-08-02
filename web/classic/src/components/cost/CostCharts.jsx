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

import React, { useEffect, useMemo } from 'react';
import { Card, Empty } from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import { initVChartSemiTheme } from '@visactor/vchart-semi-theme';
import { CHART_CONFIG, CARD_PROPS } from '../../constants/dashboard.constants';

const MAX_STACK_SERIES = 8;

// 固定配色，与 web/default 端保持一致的语义：收入蓝、成本橙、利润绿
const TREND_COLORS = {
  revenue: '#3b82f6',
  cost: '#f59e0b',
  profit: '#10b981',
};

const STACK_COLORS = [
  '#3b82f6',
  '#10b981',
  '#f59e0b',
  '#ef4444',
  '#8b5cf6',
  '#06b6d4',
  '#ec4899',
  '#6366f1',
  '#94a3b8', // 其他
];

// 将 trend 数据（宽表：每行含 revenue_cny/cost_cny/profit_cny）转换为 VChart 需要的长表
function buildTrendData(trend, seriesNames) {
  const rows = [];
  (trend || []).forEach((point) => {
    rows.push({
      date: point.date,
      series: seriesNames.revenue,
      value: Number(point.revenue_cny) || 0,
    });
    rows.push({
      date: point.date,
      series: seriesNames.cost,
      value: Number(point.cost_cny) || 0,
    });
    rows.push({
      date: point.date,
      series: seriesNames.profit,
      value: Number(point.profit_cny) || 0,
    });
  });
  return rows;
}

// 按渠道折叠成本堆叠数据：超过 MAX_STACK_SERIES 个渠道时，按总成本排序保留前 N 个，其余归入"其他"
function foldChannelStack(points, otherLabel, maxSeries = MAX_STACK_SERIES) {
  if (!points || points.length === 0) return { data: [], domain: [] };

  const totals = new Map();
  const firstSeenOrder = [];
  points.forEach((point) => {
    const name = point.channel_name;
    if (!totals.has(name)) firstSeenOrder.push(name);
    totals.set(name, (totals.get(name) || 0) + (Number(point.cost_cny) || 0));
  });

  const shouldFold = firstSeenOrder.length > maxSeries;
  let keep;
  if (shouldFold) {
    const ranked = Array.from(totals.entries()).sort((a, b) => b[1] - a[1]);
    keep = new Set(ranked.slice(0, maxSeries).map(([name]) => name));
  } else {
    keep = new Set(firstSeenOrder);
  }

  const domain = firstSeenOrder.filter((name) => keep.has(name));
  if (shouldFold) domain.push(otherLabel);

  const byDate = new Map();
  points.forEach((point) => {
    const key = keep.has(point.channel_name) ? point.channel_name : otherLabel;
    if (!byDate.has(point.date)) byDate.set(point.date, new Map());
    const bucket = byDate.get(point.date);
    bucket.set(key, (bucket.get(key) || 0) + (Number(point.cost_cny) || 0));
  });

  const dates = Array.from(byDate.keys()).sort();
  const data = [];
  dates.forEach((date) => {
    const bucket = byDate.get(date);
    domain.forEach((channel) => {
      data.push({
        date,
        channel,
        cost_cny: Number((bucket.get(channel) || 0).toFixed(2)),
      });
    });
  });

  return { data, domain };
}

const CostCharts = ({ trend, costStack, t }) => {
  // 与看板保持一致的 VChart 主题初始化
  useEffect(() => {
    initVChartSemiTheme({ isWatchingThemeSwitch: true });
  }, []);

  const seriesNames = useMemo(
    () => ({
      revenue: t('收入'),
      cost: t('成本'),
      profit: t('利润'),
    }),
    [t],
  );

  const trendData = useMemo(
    () => buildTrendData(trend, seriesNames),
    [trend, seriesNames],
  );

  const { data: stackData, domain: stackDomain } = useMemo(
    () => foldChannelStack(costStack, t('其他')),
    [costStack, t],
  );

  const trendSpec = useMemo(
    () => ({
      type: 'line',
      data: [{ id: 'trend', values: trendData }],
      xField: 'date',
      yField: 'value',
      seriesField: 'series',
      legends: { visible: true },
      title: {
        visible: true,
        text: t('收支趋势（¥）'),
      },
      axes: [
        {
          orient: 'left',
          label: { formatMethod: (v) => Number(v).toFixed(0) },
        },
      ],
      line: { style: { lineWidth: 2 } },
      point: { visible: false },
      tooltip: {
        mark: {
          content: [
            {
              key: (datum) => datum['series'],
              value: (datum) => Number(datum['value'] || 0).toFixed(4),
            },
          ],
        },
      },
      color: {
        specified: {
          [seriesNames.revenue]: TREND_COLORS.revenue,
          [seriesNames.cost]: TREND_COLORS.cost,
          [seriesNames.profit]: TREND_COLORS.profit,
        },
      },
    }),
    [trendData, seriesNames, t],
  );

  const stackSpec = useMemo(
    () => ({
      type: 'bar',
      data: [{ id: 'costStack', values: stackData }],
      xField: 'date',
      yField: 'cost_cny',
      seriesField: 'channel',
      stack: true,
      legends: { visible: true },
      title: {
        visible: true,
        text: t('渠道成本堆叠（¥）'),
      },
      bar: {
        state: { hover: { stroke: '#000', lineWidth: 1 } },
      },
      tooltip: {
        mark: {
          content: [
            {
              key: (datum) => datum['channel'],
              value: (datum) => Number(datum['cost_cny'] || 0).toFixed(4),
            },
          ],
        },
      },
      color: {
        type: 'ordinal',
        domain: stackDomain,
        range: STACK_COLORS,
      },
    }),
    [stackData, stackDomain, t],
  );

  return (
    <div className='grid grid-cols-1 lg:grid-cols-2 gap-4 mb-4'>
      <Card {...CARD_PROPS} className='!rounded-2xl' bodyStyle={{ padding: 8 }}>
        <div className='h-72'>
          {trendData.length > 0 ? (
            <VChart spec={trendSpec} option={CHART_CONFIG} />
          ) : (
            <div className='flex h-full items-center justify-center'>
              <Empty title={t('暂无数据')} />
            </div>
          )}
        </div>
      </Card>
      <Card {...CARD_PROPS} className='!rounded-2xl' bodyStyle={{ padding: 8 }}>
        <div className='h-72'>
          {stackData.length > 0 ? (
            <VChart spec={stackSpec} option={CHART_CONFIG} />
          ) : (
            <div className='flex h-full items-center justify-center'>
              <Empty title={t('暂无数据')} />
            </div>
          )}
        </div>
      </Card>
    </div>
  );
};

export default CostCharts;
