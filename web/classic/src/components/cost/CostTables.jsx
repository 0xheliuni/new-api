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
import { Tabs, TabPane, Tag, Button, Modal, InputNumber, Typography } from '@douyinfe/semi-ui';
import { IconEdit } from '@douyinfe/semi-icons';
import CardTable from '../common/ui/CardTable';
import { API, showError, showSuccess } from '../../helpers';

const { Text } = Typography;

const money = (v, digits = 4) => Number(v || 0).toFixed(digits);

const profitStyle = (v) => ({
  color:
    Number(v || 0) >= 0
      ? 'var(--semi-color-success)'
      : 'var(--semi-color-danger)',
});

const getBreakdownLabel = (row) => {
  const parts = [];
  if (row.username) parts.push(row.username);
  if (row.model_name) parts.push(row.model_name);
  if (row.channel_name) parts.push(row.channel_name);
  return parts.join(' / ') || '-';
};

// ========== 通用金额列 ==========
const buildMoneyColumns = (t) => [
  {
    key: 'revenue',
    title: t('收入'),
    align: 'right',
    render: (_, row) => (
      <span>
        ${money(row.revenue_usd)} / ¥{money(row.revenue_cny)}
      </span>
    ),
  },
  {
    key: 'list_usd',
    title: t('刊例价'),
    align: 'right',
    render: (_, row) => <span>${money(row.list_usd)}</span>,
  },
  {
    key: 'cost_cny',
    title: t('成本'),
    align: 'right',
    render: (_, row) => <span>¥{money(row.cost_cny)}</span>,
  },
  {
    key: 'profit_cny',
    title: t('利润'),
    align: 'right',
    render: (_, row) => (
      <span style={profitStyle(row.profit_cny)}>¥{money(row.profit_cny)}</span>
    ),
  },
  {
    key: 'profit_rate',
    title: t('利润率'),
    align: 'right',
    render: (_, row) => (
      <span style={profitStyle(row.profit_rate)}>
        {(Number(row.profit_rate || 0) * 100).toFixed(2)}%
      </span>
    ),
  },
  {
    key: 'refund_usd',
    title: t('退款'),
    align: 'right',
    render: (_, row) =>
      Number(row.refund_usd || 0) > 0 ? (
        <span style={{ color: 'var(--semi-color-danger)' }}>
          -${money(row.refund_usd)}
        </span>
      ) : (
        <span>-</span>
      ),
  },
  {
    key: 'request_count',
    title: t('请求数'),
    align: 'right',
    render: (_, row) => <span>{Number(row.request_count || 0)}</span>,
  },
  {
    key: 'prompt_tokens',
    title: t('输入 tokens'),
    align: 'right',
    render: (_, row) => <span>{Number(row.prompt_tokens || 0)}</span>,
  },
  {
    key: 'completion_tokens',
    title: t('输出 tokens'),
    align: 'right',
    render: (_, row) => <span>{Number(row.completion_tokens || 0)}</span>,
  },
];

// ========== 展开行（用户/模型/渠道两两交叉明细）==========
const renderBreakdown = (t) => (record) => {
  const rows = record.breakdown || [];
  if (rows.length === 0) {
    return <Text type='tertiary'>{t('暂无数据')}</Text>;
  }
  const columns = [
    { key: 'label', title: t('明细'), render: (_, row) => getBreakdownLabel(row) },
    {
      key: 'revenue_cny',
      title: t('收入'),
      align: 'right',
      render: (_, row) => `¥${money(row.revenue_cny)}`,
    },
    {
      key: 'cost_cny',
      title: t('成本'),
      align: 'right',
      render: (_, row) => `¥${money(row.cost_cny)}`,
    },
    {
      key: 'profit_cny',
      title: t('利润'),
      align: 'right',
      render: (_, row) => (
        <span style={profitStyle(row.profit_cny)}>¥{money(row.profit_cny)}</span>
      ),
    },
    {
      key: 'request_count',
      title: t('请求数'),
      align: 'right',
      render: (_, row) => Number(row.request_count || 0),
    },
  ];
  return (
    <div className='pl-4'>
      <CardTable
        columns={columns}
        dataSource={rows}
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
}) => {
  const [ratioModal, setRatioModal] = useState({
    visible: false,
    channelId: null,
    channelName: '',
    value: 0,
  });
  const [submitting, setSubmitting] = useState(false);

  const openRatioModal = (record) => {
    setRatioModal({
      visible: true,
      channelId: record.channel_id,
      channelName: record.channel_name,
      value: record.cost_ratio || 0,
    });
  };

  const closeRatioModal = () => {
    if (submitting) return;
    setRatioModal((s) => ({ ...s, visible: false }));
  };

  const isValidRatio =
    ratioModal.value !== null &&
    ratioModal.value !== undefined &&
    ratioModal.value !== '' &&
    !Number.isNaN(Number(ratioModal.value)) &&
    Number(ratioModal.value) >= 0;

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
      settingObj.cost_ratio = Number(ratioModal.value);

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
      };
    }
    if (activeTab === 'models') {
      return {
        key: 'model_name',
        title: t('模型'),
        dataIndex: 'model_name',
        fixed: 'left',
      };
    }
    return {
      key: 'channel_name',
      title: t('渠道'),
      dataIndex: 'channel_name',
      fixed: 'left',
      render: (_, row) => `${row.channel_name}（#${row.channel_id}）`,
    };
  })();

  const channelExtraColumns =
    activeTab === 'channels'
      ? [
          {
            key: 'user_count',
            title: t('用户数'),
            align: 'right',
            render: (_, row) => Number(row.user_count || 0),
          },
          {
            key: 'cost_ratio',
            title: t('成本倍率（人民币:美元）'),
            align: 'right',
            render: (_, row) => (
              <div className='flex items-center justify-end gap-1'>
                {row.priced ? (
                  <span>{row.cost_ratio}</span>
                ) : (
                  <Tag color='yellow' shape='circle'>
                    {t('未填写')}
                  </Tag>
                )}
                <Button
                  icon={<IconEdit />}
                  type='tertiary'
                  theme='borderless'
                  size='small'
                  onClick={() => openRatioModal(row)}
                />
              </div>
            ),
          },
        ]
      : [];

  const columns = [dimensionColumn, ...channelExtraColumns, ...buildMoneyColumns(t)];

  const items = pageData?.items || [];
  const total = pageData?.total || 0;

  const rowKey = (record) => {
    if (activeTab === 'users') return `u-${record.user_id}`;
    if (activeTab === 'models') return `m-${record.model_name}`;
    return `c-${record.channel_id}`;
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
          expandedRowRender={renderBreakdown(t)}
          rowExpandable={(record) => (record.breakdown || []).length > 0}
        />
      </div>

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
            {t('成本倍率（人民币:美元）')}
          </div>
        }
      >
        <div className='mb-2'>
          <Text type='secondary'>{ratioModal.channelName}</Text>
        </div>
        <InputNumber
          className='w-full'
          min={0}
          step={0.01}
          value={ratioModal.value}
          onChange={(value) => setRatioModal((s) => ({ ...s, value }))}
        />
        <div className='pt-2'>
          <Text type='tertiary' size='small'>
            {isValidRatio
              ? t('倍率 {{r}} → 上游每消耗刊例 $1 记成本 ¥{{cny}}', {
                  r: ratioModal.value,
                  cny: Number(ratioModal.value).toFixed(2),
                })
              : t('请输入不小于 0 的倍率')}
          </Text>
        </div>
      </Modal>
    </>
  );
};

export default CostTables;
