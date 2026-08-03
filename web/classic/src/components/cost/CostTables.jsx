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
import { IconEdit, IconChevronDown, IconChevronRight } from '@douyinfe/semi-icons';
import CardTable from '../common/ui/CardTable';
import { API, showError, showSuccess } from '../../helpers';
import { mergeBreakdown, MERGE_VIEW_OPTIONS } from './costMerge';
import {
  deriveCnyFromUsd,
  deriveUsdFromCny,
  formatDualMoney,
  getMoneyPrimaryCurrency,
} from './costFormat';

const { Text } = Typography;

const profitStyle = (v) => ({
  color:
    Number(v || 0) >= 0
      ? 'var(--semi-color-success)'
      : 'var(--semi-color-danger)',
});

const percent = (v, digits = 2) => `${(Number(v || 0) * 100).toFixed(digits)}%`;

// 双币种金额单元格：主行随管理员配置的 quota_display_type，副行为次要货币，
// 字号更小、灰色（与 web/default 端 MoneyDualCell 的展示口径一致）。
const DualMoneyCell = ({ primary, secondary }) => (
  <div className='flex flex-col items-end leading-tight'>
    <span>{primary}</span>
    <Text type='tertiary' size='small'>
      {secondary}
    </Text>
  </div>
);

// ========== 布局对齐用的显式列宽 ==========
// 展开明细行现已直接以普通行的形式插入同一张表的 dataSource（见下方
// buildFlattenedRows），与父行共用同一个 colgroup，天然对齐，不再需要单独的
// 宽度预算表来"凑"两张表的列宽。
const IDENTITY_COLUMN_WIDTH = 220; // 身份列（用户/模型/渠道），展开明细行在此列内缩进展示
const ACTION_COLUMN_WIDTH = 140; // 「操作」列：父行显示「查看方式」，明细行显示「只看该渠道」
const CHANNEL_USER_COUNT_WIDTH = 90; // 供应商维度 用户数列
const CHANNEL_RATIO_WIDTH = 160; // 供应商维度 倍率列

// 每个维度 Tab 下，展开明细行固定展示的两个身份字段（顺序与 costMerge.js 的
// MERGE_VIEW_OPTIONS 对应：字段若不在当前查看方式的 keyFields 里，视为"已合并"，
// 显示为"—"，但字段本身始终保留，避免切换查看方式时布局跳动）。
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

// ========== 统一的指标列（顶层行与展开明细行共用同一套渲染） ==========
const buildMetricColumns = (t, openTokensModal, exchangeRate, primaryCurrency) => [
  {
    key: 'cost_cny',
    title: t('成本'),
    align: 'right',
    width: 130,
    render: (_, row) => {
      if (row.__isNote) return null;
      const { primary, secondary } = formatDualMoney(
        deriveUsdFromCny(row.cost_cny, exchangeRate),
        row.cost_cny,
        primaryCurrency,
      );
      return <DualMoneyCell primary={primary} secondary={secondary} />;
    },
  },
  {
    key: 'revenue',
    title: t('收入'),
    align: 'right',
    width: 130,
    render: (_, row) => {
      if (row.__isNote) return null;
      const { primary, secondary } = formatDualMoney(
        row.revenue_usd,
        row.revenue_cny,
        primaryCurrency,
      );
      return <DualMoneyCell primary={primary} secondary={secondary} />;
    },
  },
  {
    key: 'profit_cny',
    title: t('利润'),
    align: 'right',
    width: 130,
    render: (_, row) => {
      if (row.__isNote) return null;
      const { primary, secondary } = formatDualMoney(
        deriveUsdFromCny(row.profit_cny, exchangeRate),
        row.profit_cny,
        primaryCurrency,
      );
      return (
        <DualMoneyCell
          primary={<span style={profitStyle(row.profit_cny)}>{primary}</span>}
          secondary={secondary}
        />
      );
    },
  },
  {
    key: 'list_usd',
    title: t('刊例价'),
    align: 'right',
    width: 130,
    render: (_, row) => {
      if (row.__isNote) return null;
      const { primary, secondary } = formatDualMoney(
        row.list_usd,
        deriveCnyFromUsd(row.list_usd, exchangeRate),
        primaryCurrency,
      );
      return <DualMoneyCell primary={primary} secondary={secondary} />;
    },
  },
  {
    key: 'request_count',
    title: t('请求数'),
    align: 'right',
    width: 90,
    render: (_, row) =>
      row.__isNote ? null : <span>{Number(row.request_count || 0)}</span>,
  },
  {
    key: 'profit_rate',
    title: t('利润率'),
    align: 'right',
    width: 90,
    render: (_, row) =>
      row.__isNote ? null : (
        <span style={profitStyle(row.profit_rate)}>{percent(row.profit_rate)}</span>
      ),
  },
  {
    key: 'total_tokens',
    title: t('总tokens'),
    align: 'right',
    width: 110,
    render: (_, row) =>
      row.__isNote ? null : (
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
    render: (_, row) => (row.__isNote ? null : <span>{percent(row.success_rate ?? 1)}</span>),
  },
  {
    key: 'cache_rate',
    title: t('缓存率'),
    align: 'right',
    width: 90,
    render: (_, row) => (row.__isNote ? null : <span>{percent(row.cache_rate)}</span>),
  },
  {
    key: 'avg_ttft_ms',
    title: t('平均TTFT'),
    align: 'right',
    width: 110,
    render: (_, row) => {
      if (row.__isNote) return null;
      return Number(row.frt_count || 0) === 0 ? (
        <span>-</span>
      ) : (
        <span>{Number(row.avg_ttft_ms || 0).toFixed(0)} ms</span>
      );
    },
  },
];

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
  const [expandedKeys, setExpandedKeys] = useState(() => new Set());
  const [subSuppliersModal, setSubSuppliersModal] = useState({
    visible: false,
    channelName: '',
    subSuppliers: [],
  });

  const primaryCurrency = getMoneyPrimaryCurrency();

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

  const openSubSuppliersModal = (record) =>
    setSubSuppliersModal({
      visible: true,
      channelName: record.channel_name,
      subSuppliers: record.sub_suppliers || [],
    });
  const closeSubSuppliersModal = () =>
    setSubSuppliersModal((s) => ({ ...s, visible: false }));

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

  const rowKey = (record) => {
    if (activeTab === 'users') return `u-${record.user_id}`;
    if (activeTab === 'models') return `m-${record.model_name}`;
    return `c-${record.channel_id}`;
  };

  const getMergeMode = (rKey) => mergeModes[rKey] || 'detail';
  const setMergeMode = (rKey, mode) =>
    setMergeModes((prev) => ({ ...prev, [rKey]: mode }));

  const toggleExpanded = (rKey) =>
    setExpandedKeys((prev) => {
      const next = new Set(prev);
      if (next.has(rKey)) next.delete(rKey);
      else next.add(rKey);
      return next;
    });

  const dimensionTitle =
    activeTab === 'users' ? t('用户') : activeTab === 'models' ? t('模型') : t('渠道');

  // 父行的身份列内容（不含展开箭头）：三个维度各自的展示逻辑与原实现一致。
  const renderParentIdentityContent = (row) => {
    if (activeTab === 'users') return row.username || '-';
    if (activeTab === 'models') return row.model_name || '-';
    return (
      <span className='flex items-center gap-1 flex-wrap'>
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
        {(row.sub_suppliers || []).length > 0 && (
          <Button
            theme='borderless'
            type='tertiary'
            size='small'
            onClick={() => openSubSuppliersModal(row)}
          >
            {t('子供应商')}
          </Button>
        )}
      </span>
    );
  };

  // 展开明细行的身份列内容：固定展示当前维度的两个身份字段，合并掉的字段显示
  // "—"，缩进展示以区分层级。
  const renderChildIdentityContent = (row) => {
    const [fieldA, fieldB] = BREAKDOWN_IDENTITY_FIELDS[activeTab];
    return (
      <div className='pl-6 flex flex-col gap-0.5 text-xs'>
        {[fieldA, fieldB].map((field) => (
          <div key={field}>
            {isIdentityFieldCollapsed(field, row.__mode, row.__keyFields) ? (
              <Text type='tertiary'>—</Text>
            ) : (
              renderIdentityFieldValue(t, field, row)
            )}
          </div>
        ))}
      </div>
    );
  };

  const dimensionColumn = {
    key: 'dimension',
    title: dimensionTitle,
    fixed: 'left',
    width: IDENTITY_COLUMN_WIDTH,
    render: (_, row) => {
      if (row.__isNote) {
        return (
          <Text type='tertiary' size='small'>
            {row.__noteText}
          </Text>
        );
      }
      if (row.__isChild) {
        return renderChildIdentityContent(row);
      }
      const expandable = (row.breakdown || []).length > 0;
      const rKey = rowKey(row);
      const expanded = expandedKeys.has(rKey);
      return (
        <div className='flex items-center gap-1'>
          {expandable ? (
            <Button
              theme='borderless'
              type='tertiary'
              size='small'
              icon={expanded ? <IconChevronDown /> : <IconChevronRight />}
              onClick={() => toggleExpanded(rKey)}
              style={{ padding: 0, minWidth: 24 }}
            />
          ) : (
            <span style={{ width: 24, display: 'inline-block' }} />
          )}
          <span className='flex-1'>{renderParentIdentityContent(row)}</span>
        </div>
      );
    },
  };

  const channelExtraColumns =
    activeTab === 'channels'
      ? [
          {
            key: 'user_count',
            title: t('用户数'),
            align: 'right',
            width: CHANNEL_USER_COUNT_WIDTH,
            render: (_, row) =>
              row.__isChild ? (
                <Text type='tertiary'>—</Text>
              ) : (
                Number(row.user_count || 0)
              ),
          },
          {
            key: 'ratio',
            title: t('倍率'),
            align: 'right',
            width: CHANNEL_RATIO_WIDTH,
            render: (_, row) => {
              if (row.__isChild) return <Text type='tertiary'>—</Text>;
              return (
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
              );
            },
          },
        ]
      : [];

  // 「操作」列：父行显示紧凑的「查看方式」Select（仅在该行有 breakdown 明细时
  // 展示）；展开出的明细行显示「只看该渠道」（仅在渠道身份字段未被合并、且该
  // 行确有 channel_id 时展示；供应商维度本身已按渠道分组，明细行没有渠道身份
  // 字段，不展示）。
  const actionColumn = {
    key: 'action',
    title: t('操作'),
    width: ACTION_COLUMN_WIDTH,
    render: (_, row) => {
      if (row.__isNote) return null;
      if (row.__isChild) {
        if (activeTab === 'channels') return null;
        if (isIdentityFieldCollapsed('channel', row.__mode, row.__keyFields)) return null;
        if (!row.channel_id) return null;
        return (
          <Button
            size='small'
            type='tertiary'
            theme='borderless'
            onClick={() => onFilterByChannel && onFilterByChannel(row.channel_id)}
          >
            {t('只看该渠道')}
          </Button>
        );
      }
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
    ...buildMetricColumns(t, openTokensModal, exchangeRate, primaryCurrency),
    actionColumn,
  ];

  const items = pageData?.items || [];
  const total = pageData?.total || 0;

  // ========== 展平：把展开行的合并明细直接插入父行之后，成为同一张表的普通
  // 行（打 __isChild 标记）。单表单 colgroup，列宽由列定义唯一决定，父子行
  // 天然对齐，不再需要第二张表 + 宽度预算凑对齐。 ==========
  const buildFlattenedRows = () => {
    const result = [];
    for (const record of items) {
      const rKey = rowKey(record);
      result.push({ ...record, __rowKey: rKey });

      const rawRows = record.breakdown || [];
      if (rawRows.length === 0 || !expandedKeys.has(rKey)) continue;

      const options = MERGE_VIEW_OPTIONS[activeTab] || MERGE_VIEW_OPTIONS.users;
      const mode = getMergeMode(rKey);
      const activeOption = options.find((o) => o.value === mode) || options[0];
      const mergedRows = mergeBreakdown(rawRows, activeOption.keyFields);

      mergedRows.forEach((bRow, idx) => {
        result.push({
          ...bRow,
          __isChild: true,
          __parentKey: rKey,
          __rowKey: `${rKey}::${idx}`,
          __mode: mode,
          __keyFields: activeOption.keyFields,
        });
      });

      if (record.breakdown_truncated > 0) {
        result.push({
          __isChild: true,
          __isNote: true,
          __parentKey: rKey,
          __rowKey: `${rKey}::note`,
          __noteText: t('还有 {{count}} 项未展示', {
            count: record.breakdown_truncated,
          }),
        });
      }
    }
    return result;
  };

  const dataSource = buildFlattenedRows();

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
          dataSource={dataSource}
          loading={tableLoading}
          rowKey={(record) => record.__rowKey}
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

      <Modal
        centered
        visible={subSuppliersModal.visible}
        onCancel={closeSubSuppliersModal}
        footer={
          <Button type='primary' onClick={closeSubSuppliersModal}>
            {t('关闭')}
          </Button>
        }
        title={t('子供应商')}
      >
        <div className='mb-2'>
          <Text type='secondary'>{subSuppliersModal.channelName}</Text>
        </div>
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
          dataSource={subSuppliersModal.subSuppliers}
          rowKey={(row, idx) => `sub-${idx}`}
          hidePagination
          size='small'
        />
        <div className='pt-1'>
          <Text type='tertiary' size='small'>
            {t('报表成本按渠道级计价计算')}
          </Text>
        </div>
      </Modal>
    </>
  );
};

export default CostTables;
