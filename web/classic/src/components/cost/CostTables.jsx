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

import React, { useState } from 'react';
import {
  Tabs,
  TabPane,
  Tag,
  Button,
  Modal,
  InputNumber,
  Select,
  Typography,
} from '@douyinfe/semi-ui';
import { IconEdit } from '@douyinfe/semi-icons';
import CardTable from '../common/ui/CardTable';
import { API, showError, showSuccess } from '../../helpers';
import { mergeBreakdown, MERGE_VIEW_OPTIONS } from './costMerge';

const { Text } = Typography;

const money = (v, digits = 4) => Number(v || 0).toFixed(digits);

const profitStyle = (v) => ({
  color:
    Number(v || 0) >= 0
      ? 'var(--semi-color-success)'
      : 'var(--semi-color-danger)',
});

const percent = (v, digits = 2) => `${(Number(v || 0) * 100).toFixed(digits)}%`;

const getBreakdownLabel = (row) => {
  const parts = [];
  if (row.username) parts.push(row.username);
  if (row.model_name) parts.push(row.model_name);
  if (row.channel_name) {
    parts.push(
      row.channel_id ? `${row.channel_name}（#${row.channel_id}）` : row.channel_name,
    );
  }
  return parts.join(' / ') || '-';
};

// ========== 布局对齐用的显式列宽 ==========
// 父表 身份列 + 操作列 的宽度预算 == 子表 两个身份列 的宽度预算，
// 令两张表在“指标列”开始前占用相同的总宽度，配合下方共用的指标列宽定义，
// 使 成本/收入/利润/刊例/请求数/利润率/总tokens/成功率/缓存率/TTFT 上下对齐。
const IDENTITY_COLUMN_WIDTH = 200;
const ACTION_COLUMN_WIDTH = 140;
const BREAKDOWN_IDENTITY_COLUMN_WIDTH = 170; // (200 + 140) / 2

// ========== 统一的指标列（顶层行与展开明细行共用） ==========
const buildMetricColumns = (t, openTokensModal) => [
  {
    key: 'cost_cny',
    title: t('成本'),
    align: 'right',
    width: 110,
    render: (_, row) => <span>¥{money(row.cost_cny)}</span>,
  },
  {
    key: 'revenue',
    title: t('收入'),
    align: 'right',
    width: 120,
    render: (_, row) => (
      <div>
        <div>${money(row.revenue_usd)}</div>
        <div className='text-xs text-gray-500'>¥{money(row.revenue_cny)}</div>
      </div>
    ),
  },
  {
    key: 'profit_cny',
    title: t('利润'),
    align: 'right',
    width: 110,
    render: (_, row) => (
      <span style={profitStyle(row.profit_cny)}>¥{money(row.profit_cny)}</span>
    ),
  },
  {
    key: 'list_usd',
    title: t('刊例价'),
    align: 'right',
    width: 100,
    render: (_, row) => <span>${money(row.list_usd)}</span>,
  },
  {
    key: 'request_count',
    title: t('请求数'),
    align: 'right',
    width: 90,
    render: (_, row) => <span>{Number(row.request_count || 0)}</span>,
  },
  {
    key: 'profit_rate',
    title: t('利润率'),
    align: 'right',
    width: 90,
    render: (_, row) => (
      <span style={profitStyle(row.profit_rate)}>{percent(row.profit_rate)}</span>
    ),
  },
  {
    key: 'total_tokens',
    title: t('总tokens'),
    align: 'right',
    width: 110,
    render: (_, row) => (
      <Button
        theme='borderless'
        type='tertiary'
        size='small'
        onClick={() => openTokensModal(row)}
      >
        {Number(row.total_tokens || 0)}
      </Button>
    ),
  },
  {
    key: 'success_rate',
    title: t('成功率'),
    align: 'right',
    width: 90,
    render: (_, row) => <span>{percent(row.success_rate ?? 1)}</span>,
  },
  {
    key: 'cache_rate',
    title: t('缓存率'),
    align: 'right',
    width: 90,
    render: (_, row) => <span>{percent(row.cache_rate)}</span>,
  },
  {
    key: 'avg_ttft_ms',
    title: t('平均TTFT'),
    align: 'right',
    width: 110,
    render: (_, row) =>
      Number(row.frt_count || 0) === 0 ? (
        <span>-</span>
      ) : (
        <span>{Number(row.avg_ttft_ms || 0).toFixed(0)} ms</span>
      ),
  },
];

// 每个维度 Tab 下，展开明细行固定展示的两个身份字段（顺序与 costMerge.js 的
// MERGE_VIEW_OPTIONS 对应：字段若不在当前查看方式的 keyFields 里，视为“已合并”，
// 显示为“—”，但列本身始终保留，避免切换查看方式时布局跳动）。
const BREAKDOWN_IDENTITY_FIELDS = {
  users: ['channel', 'model_name'],
  models: ['username', 'channel'],
  channels: ['username', 'model_name'],
};

// 身份字段 -> 判断其是否被当前 keyFields 保留所用的代表性行字段
const IDENTITY_FIELD_SIGNATURE = {
  channel: 'channel_id',
  model_name: 'model_name',
  username: 'username',
};

const isIdentityFieldCollapsed = (field, mode, keyFields) =>
  mode !== 'detail' && !(keyFields || []).includes(IDENTITY_FIELD_SIGNATURE[field]);

const renderIdentityFieldValue = (t, field, row) => {
  if (field === 'username') return row.username || '-';
  if (field === 'model_name') return row.model_name || '-';
  // field === 'channel'
  if (!row.channel_id) return t('未知渠道');
  return row.channel_name ? `${row.channel_name}（#${row.channel_id}）` : `#${row.channel_id}`;
};

const IDENTITY_FIELD_LABEL = {
  channel: '渠道',
  model_name: '模型',
  username: '用户',
};

const CostTables = ({
  t,
  activeTab,
  setActiveTab,
  pageData,
  tableLoading,
  activePage,
  pageSize,
  handlePageChange,
  handlePageSizeChange,
  onRatioUpdated,
  onFilterByChannel,
  exchangeRate,
}) => {
  const [ratioModal, setRatioModal] = useState({
    visible: false,
    channelId: null,
    channelName: '',
    mode: 'ratio',
    ratioValue: 0,
    discountValue: 0,
  });
  const [submitting, setSubmitting] = useState(false);
  const [tokensModal, setTokensModal] = useState({ visible: false, row: null });
  const [mergeModes, setMergeModes] = useState({});

  const openRatioModal = (record) => {
    setRatioModal({
      visible: true,
      channelId: record.channel_id,
      channelName: record.channel_name,
      mode: record.cost_mode === 'discount' ? 'discount' : 'ratio',
      ratioValue: record.cost_ratio || 0,
      discountValue: record.cost_discount || 0,
    });
  };

  const closeRatioModal = () => {
    if (submitting) return;
    setRatioModal((s) => ({ ...s, visible: false }));
  };

  const openTokensModal = (row) => setTokensModal({ visible: true, row });
  const closeTokensModal = () => setTokensModal({ visible: false, row: null });

  const activeRatioValue =
    ratioModal.mode === 'discount' ? ratioModal.discountValue : ratioModal.ratioValue;
  const isValidRatio =
    activeRatioValue !== null &&
    activeRatioValue !== undefined &&
    activeRatioValue !== '' &&
    !Number.isNaN(Number(activeRatioValue)) &&
    Number(activeRatioValue) >= 0;

  const submitRatio = async () => {
    if (!isValidRatio) return;
    setSubmitting(true);
    try {
      const channelRes = await API.get(`/api/channel/${ratioModal.channelId}`);
      const { success, message, data } = channelRes.data;
      if (!success) {
        showError(message);
        return;
      }

      let settingObj = {};
      if (data.setting) {
        try {
          const parsed = JSON.parse(data.setting);
          if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
            settingObj = parsed;
          }
        } catch (e) {
          settingObj = {};
        }
      }
      settingObj.cost_mode = ratioModal.mode;
      settingObj.cost_ratio = Number(ratioModal.ratioValue) || 0;
      settingObj.cost_discount = Number(ratioModal.discountValue) || 0;

      const updateRes = await API.put('/api/channel/', {
        id: ratioModal.channelId,
        setting: JSON.stringify(settingObj),
      });
      if (updateRes.data.success) {
        showSuccess(t('操作成功完成！'));
        setRatioModal((s) => ({ ...s, visible: false }));
        onRatioUpdated && onRatioUpdated();
      } else {
        showError(updateRes.data.message);
      }
    } finally {
      setSubmitting(false);
    }
  };

  const dimensionColumn = (() => {
    if (activeTab === 'users') {
      return {
        key: 'username',
        title: t('用户'),
        dataIndex: 'username',
        fixed: 'left',
        width: IDENTITY_COLUMN_WIDTH,
      };
    }
    if (activeTab === 'models') {
      return {
        key: 'model_name',
        title: t('模型'),
        dataIndex: 'model_name',
        fixed: 'left',
        width: IDENTITY_COLUMN_WIDTH,
      };
    }
    return {
      key: 'channel_name',
      title: t('渠道'),
      fixed: 'left',
      width: IDENTITY_COLUMN_WIDTH,
      render: (_, row) => (
        <span className='flex items-center gap-1'>
          <span>
            {row.channel_id
              ? `${row.channel_name}（#${row.channel_id}）`
              : `${t('未知渠道')}（#0）`}
          </span>
          {row.is_aggregator && (
            <Tag color='purple' shape='circle' size='small'>
              {t('聚合渠道')}
            </Tag>
          )}
        </span>
      ),
    };
  })();

  const channelExtraColumns =
    activeTab === 'channels'
      ? [
          {
            key: 'user_count',
            title: t('用户数'),
            align: 'right',
            width: 90,
            render: (_, row) => Number(row.user_count || 0),
          },
          {
            key: 'ratio',
            title: t('倍率'),
            align: 'right',
            width: 160,
            render: (_, row) => (
              <div className='flex items-center justify-end gap-1'>
                {row.priced ? (
                  row.cost_mode === 'discount' ? (
                    <span>
                      {t('{{d}} 折 (≈¥{{cny}}/$1)', {
                        d: (Number(row.cost_discount || 0) * 10).toFixed(1),
                        cny: Number(row.effective_ratio || 0).toFixed(2),
                      })}
                    </span>
                  ) : (
                    <span>{row.cost_ratio}</span>
                  )
                ) : (
                  <Tag color='yellow' shape='circle'>
                    {t('未填写')}
                  </Tag>
                )}
                {Boolean(row.channel_id) && (
                  <Button
                    icon={<IconEdit />}
                    type='tertiary'
                    theme='borderless'
                    size='small'
                    onClick={() => openRatioModal(row)}
                  />
                )}
              </div>
            ),
          },
        ]
      : [];

  const rowKey = (record) => {
    if (activeTab === 'users') return `u-${record.user_id}`;
    if (activeTab === 'models') return `m-${record.model_name}`;
    return `c-${record.channel_id}`;
  };

  const getMergeMode = (rKey) => mergeModes[rKey] || 'detail';
  const setMergeMode = (rKey, mode) =>
    setMergeModes((prev) => ({ ...prev, [rKey]: mode }));

  // 「操作」列：紧凑的按行「查看方式」Select，仅在该行有 breakdown 明细时展示。
  const actionColumn = {
    key: 'action',
    title: t('操作'),
    width: ACTION_COLUMN_WIDTH,
    render: (_, row) => {
      if (!(row.breakdown || []).length) return null;
      const rKey = rowKey(row);
      const options = MERGE_VIEW_OPTIONS[activeTab] || MERGE_VIEW_OPTIONS.users;
      return (
        <Select
          size='small'
          value={getMergeMode(rKey)}
          optionList={options.map((o) => ({
            label: t(o.labelKey),
            value: o.value,
          }))}
          onChange={(value) => setMergeMode(rKey, value)}
          style={{ width: '100%' }}
        />
      );
    },
  };

  const columns = [
    dimensionColumn,
    ...channelExtraColumns,
    ...buildMetricColumns(t, openTokensModal),
    actionColumn,
  ];

  const items = pageData?.items || [];
  const total = pageData?.total || 0;

  // ========== 展开行：合并明细 + （渠道维度）子供应商配置 ==========
  // 查看方式 Select 已上移至父行「操作」列，此处仅渲染合并后的明细子表。
  const renderBreakdown = (record) => {
    const rows = record.breakdown || [];
    if (rows.length === 0) {
      return <Text type='tertiary'>{t('暂无数据')}</Text>;
    }

    const rKey = rowKey(record);
    const options = MERGE_VIEW_OPTIONS[activeTab] || MERGE_VIEW_OPTIONS.users;
    const mode = getMergeMode(rKey);
    const activeOption = options.find((o) => o.value === mode) || options[0];
    const mergedRows = mergeBreakdown(rows, activeOption.keyFields);

    // 固定展示当前维度的两个身份字段（顺序固定），合并掉的字段显示“—”，
    // 保证列宽/布局不随查看方式切换而跳动。
    const [fieldA, fieldB] = BREAKDOWN_IDENTITY_FIELDS[activeTab];
    const identityColumns = [fieldA, fieldB].map((field) => ({
      key: `identity-${field}`,
      title: t(IDENTITY_FIELD_LABEL[field]),
      width: BREAKDOWN_IDENTITY_COLUMN_WIDTH,
      render: (_, row) =>
        isIdentityFieldCollapsed(field, mode, activeOption.keyFields) ? (
          <Text type='tertiary'>—</Text>
        ) : (
          renderIdentityFieldValue(t, field, row)
        ),
    }));

    // 「只看该渠道」放在渠道身份列旁的一个窄尾列里，供应商维度（channels）本身
    // 就是按渠道分组，不需要该动作。
    const channelActionColumn =
      activeTab !== 'channels'
        ? [
            {
              key: 'action',
              title: '',
              width: ACTION_COLUMN_WIDTH,
              render: (_, row) =>
                !isIdentityFieldCollapsed('channel', mode, activeOption.keyFields) &&
                row.channel_id ? (
                  <Button
                    size='small'
                    type='tertiary'
                    theme='borderless'
                    onClick={() =>
                      onFilterByChannel && onFilterByChannel(row.channel_id)
                    }
                  >
                    {t('只看该渠道')}
                  </Button>
                ) : null,
            },
          ]
        : [];

    const breakdownColumns = [
      ...identityColumns,
      ...buildMetricColumns(t, openTokensModal),
      ...channelActionColumn,
    ];

    return (
      <div className='pl-4'>
        {activeTab === 'channels' && (record.sub_suppliers || []).length > 0 && (
          <div className='mb-3'>
            <Text className='text-sm font-medium'>{t('子供应商')}</Text>
            <CardTable
              columns={[
                {
                  key: 'name',
                  title: t('名称'),
                  render: (_, row) => row.name || '-',
                },
                {
                  key: 'cost_ratio',
                  title: t('倍率'),
                  align: 'right',
                  render: (_, row) => row.cost_ratio,
                },
              ]}
              dataSource={record.sub_suppliers}
              rowKey={(row, idx) => `sub-${idx}`}
              hidePagination
              size='small'
            />
            <div className='pt-1'>
              <Text type='tertiary' size='small'>
                {t('报表成本按渠道级计价计算')}
              </Text>
            </div>
          </div>
        )}

        <CardTable
          columns={breakdownColumns}
          dataSource={mergedRows}
          rowKey={(row, idx) => `${getBreakdownLabel(row)}-${idx}`}
          hidePagination
          size='small'
        />
        {record.breakdown_truncated > 0 && (
          <div className='pt-2'>
            <Text type='tertiary' size='small'>
              {t('还有 {{count}} 项未展示', { count: record.breakdown_truncated })}
            </Text>
          </div>
        )}
      </div>
    );
  };

  return (
    <>
      <Tabs type='line' activeKey={activeTab} onChange={setActiveTab}>
        <TabPane tab={t('用户维度')} itemKey='users' />
        <TabPane tab={t('模型维度')} itemKey='models' />
        <TabPane tab={t('供应商维度')} itemKey='channels' />
      </Tabs>

      <div className='mt-2'>
        <CardTable
          columns={columns}
          dataSource={items}
          loading={tableLoading}
          rowKey={rowKey}
          scroll={{ x: 'max-content' }}
          hidePagination
          pagination={{
            currentPage: activePage,
            pageSize,
            total,
            pageSizeOpts: [10, 20, 50, 100],
            showSizeChanger: true,
            onPageSizeChange: handlePageSizeChange,
            onPageChange: handlePageChange,
          }}
          expandedRowRender={renderBreakdown}
          rowExpandable={(record) => (record.breakdown || []).length > 0}
        />
      </div>

      <Modal
        centered
        visible={tokensModal.visible}
        onCancel={closeTokensModal}
        footer={
          <Button type='primary' onClick={closeTokensModal}>
            {t('关闭')}
          </Button>
        }
        title={t('总tokens')}
      >
        <div className='space-y-2'>
          {[
            ['输入 tokens', tokensModal.row?.prompt_tokens],
            ['输出 tokens', tokensModal.row?.completion_tokens],
            ['缓存读取 tokens', tokensModal.row?.cache_read_tokens],
            ['缓存创建 tokens', tokensModal.row?.cache_creation_tokens],
          ].map(([labelKey, value]) => (
            <div key={labelKey} className='flex items-center justify-between'>
              <Text type='tertiary'>{t(labelKey)}</Text>
              <Text className='tabular-nums'>{Number(value || 0)}</Text>
            </div>
          ))}
        </div>
      </Modal>

      <Modal
        centered
        visible={ratioModal.visible}
        onCancel={closeRatioModal}
        onOk={submitRatio}
        confirmLoading={submitting}
        okButtonProps={{ disabled: !isValidRatio }}
        title={
          <div className='flex items-center'>
            <IconEdit className='mr-2' />
            {t('计价方式')}
          </div>
        }
      >
        <div className='mb-2'>
          <Text type='secondary'>{ratioModal.channelName}</Text>
        </div>

        <Select
          className='w-full mb-2'
          value={ratioModal.mode}
          optionList={[
            { label: t('成本倍率（人民币:美元）'), value: 'ratio' },
            { label: t('成本折扣'), value: 'discount' },
          ]}
          onChange={(value) => setRatioModal((s) => ({ ...s, mode: value }))}
        />

        {ratioModal.mode === 'discount' ? (
          <InputNumber
            className='w-full'
            min={0}
            step={0.01}
            value={ratioModal.discountValue}
            onChange={(value) =>
              setRatioModal((s) => ({ ...s, discountValue: value }))
            }
          />
        ) : (
          <InputNumber
            className='w-full'
            min={0}
            step={0.01}
            value={ratioModal.ratioValue}
            onChange={(value) => setRatioModal((s) => ({ ...s, ratioValue: value }))}
          />
        )}

        <div className='pt-2'>
          <Text type='tertiary' size='small'>
            {!isValidRatio
              ? t('请输入不小于 0 的倍率')
              : ratioModal.mode === 'discount'
                ? t('折扣 {{d}} × 汇率 {{rate}} → 刊例 $1 记成本 ¥{{cny}}', {
                    d: ratioModal.discountValue,
                    rate: exchangeRate,
                    cny: (
                      Number(ratioModal.discountValue) * Number(exchangeRate || 0)
                    ).toFixed(2),
                  })
                : t('倍率 {{r}} → 上游每消耗刊例 $1 记成本 ¥{{cny}}', {
                    r: ratioModal.ratioValue,
                    cny: Number(ratioModal.ratioValue).toFixed(2),
                  })}
          </Text>
        </div>
      </Modal>
    </>
  );
};

export default CostTables;
