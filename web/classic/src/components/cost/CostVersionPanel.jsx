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

import React, { useCallback, useEffect, useState } from 'react';
import {
  Button,
  DatePicker,
  Input,
  InputNumber,
  Modal,
  Radio,
  RadioGroup,
  Select,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconEdit, IconPlus } from '@douyinfe/semi-icons';
import CardTable from '../common/ui/CardTable';
import { API, showError, showSuccess } from '../../helpers';
import { timestamp2string } from '../../helpers/utils';

const { Text } = Typography;

/** 后端自动写入的价格记录备注（英文原文作键翻译）。 */
const SYSTEM_VERSION_NOTES = [
  'auto from channel update',
  'set on the cost page',
  'migrated from channel settings',
];

/** 去掉尾随 0 的计价数值（2.50 → 2.5）。 */
const trimNumber = (value) => {
  const n = Number(value);
  if (!Number.isFinite(n)) return '-';
  return String(Number(n.toFixed(4)));
};

/**
 * 版本的计价展示。discount 模式把冻结汇率一并写出来 —— 同一个「0.8」在不同结算
 * 汇率下代表的成本不同，只显示折扣看不出区别。
 *
 * 空 cost_mode 等同 ratio（见 model.ChannelCostVersion.CostMode 注释）：迁移回填的
 * 初始版本直接抄渠道设置，从未显式选过计价方式的渠道在这里就是空串。按 !== 'discount'
 * 判定，否则一个正在按 ¥2.5/$1 计价的渠道会被写成「未填写」。
 */
const describeVersion = (v, t) => {
  if (v.cost_mode === 'discount') {
    return Number(v.cost_discount) > 0
      ? t('{{d}} 折扣 × ¥{{rate}}', {
          d: trimNumber(v.cost_discount),
          rate: Number(v.exchange_rate || 0).toFixed(2),
        })
      : t('未填写');
  }
  return Number(v.cost_ratio) > 0
    ? t('¥{{r}}/$1', { r: trimNumber(v.cost_ratio) })
    : t('未填写');
};

/**
 * 新增或修改一条价格记录。生效时间决定这条价管哪些日志：初始价没有生效时间，覆盖
 * 最早一条带日期记录之前的全部日志；修改时初始价不能改成带日期的记录，反之亦然。
 *
 * 结算汇率随记录冻结，只在 discount 模式下参与计价（ratio 模式的倍率本身就是
 * 人民币计价，不需要汇率），因此只在折扣模式下要求填写。
 */
const VersionForm = ({
  channelId,
  version,
  hasInitial,
  initialSelected,
  defaultExchangeRate,
  t,
  onDone,
  onCancel,
}) => {
  const editing = Boolean(version);
  const [timing, setTiming] = useState(() => {
    if (version)
      return Number(version.effective_from) === 0 ? 'initial' : 'dated';
    return initialSelected && !hasInitial ? 'initial' : 'dated';
  });
  const [effectiveFrom, setEffectiveFrom] = useState(
    version && Number(version.effective_from) > 0
      ? new Date(Number(version.effective_from) * 1000)
      : null,
  );
  const [mode, setMode] = useState(
    version?.cost_mode === 'discount' ? 'discount' : 'ratio',
  );
  const [value, setValue] = useState(() => {
    if (!version) return '';
    return version.cost_mode === 'discount'
      ? Number(version.cost_discount)
      : Number(version.cost_ratio);
  });
  const [rate, setRate] = useState(() => {
    if (version?.cost_mode === 'discount') return Number(version.exchange_rate);
    return Number(defaultExchangeRate) > 0 ? Number(defaultExchangeRate) : '';
  });
  const [note, setNote] = useState(version?.note || '');
  const [submitting, setSubmitting] = useState(false);

  const parsedValue = Number(value);
  const parsedRate = Number(rate);
  // 要求 > 0 而不是 >= 0：后端拒绝 0 倍率/0 折扣（那样的记录下所有日志都无法
  // 定价），客户端先挡住，省一次往返。
  const valueValid = value !== '' && value !== null && parsedValue > 0;
  const rateValid =
    mode !== 'discount' || (rate !== '' && rate !== null && parsedRate > 0);
  const isValid =
    (timing === 'initial' || Boolean(effectiveFrom)) && valueValid && rateValid;

  const submit = async () => {
    if (!isValid) return;
    setSubmitting(true);
    try {
      const body = {
        effective_from:
          timing === 'initial'
            ? 0
            : Math.floor(new Date(effectiveFrom).getTime() / 1000),
        cost_mode: mode,
        cost_ratio: mode === 'ratio' ? parsedValue : 0,
        cost_discount: mode === 'discount' ? parsedValue : 0,
        exchange_rate: mode === 'discount' ? parsedRate : 0,
        note: (note || '').trim(),
      };
      const res = editing
        ? await API.put(`/api/cost/versions/${version.id}`, body)
        : await API.post(`/api/cost/channels/${channelId}/versions`, body);
      if (res.data.success) {
        showSuccess(editing ? t('已修改价格记录') : t('已补录历史价'));
        onDone();
      } else {
        // 业务失败走 HTTP 200，拦截器不会提示；后端的话术是可操作的
        // （「该生效时间已存在记录」之类），照原样透出。
        showError(res.data.message);
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      className='mb-2 rounded-lg p-2'
      style={{ border: '1px solid var(--semi-color-border)' }}
      role='group'
      aria-label={editing ? t('修改价格记录') : t('补录历史价')}
    >
      <div className='grid grid-cols-1 sm:grid-cols-2 gap-2'>
        <div className='sm:col-span-2'>
          <Text size='small' type='tertiary'>
            {t('该价格自何时起生效')}
          </Text>
          {/* 每个控件都要自带 aria-label：上面的 <Text> 只是视觉标签，既没有 id
              关联也不会被读屏软件念出来，否则整张表单在读屏下都是无名字段。 */}
          {!editing && (
            <div>
              <RadioGroup
                type='button'
                size='small'
                value={timing}
                onChange={(e) => setTiming(e.target.value)}
                aria-label={t('该价格自何时起生效')}
              >
                <Radio value='initial' disabled={hasInitial}>
                  {t('初始价格')}
                </Radio>
                <Radio value='dated'>{t('指定时间')}</Radio>
              </RadioGroup>
            </div>
          )}
          {timing === 'dated' ? (
            <DatePicker
              className='w-full'
              type='dateTime'
              size='small'
              value={effectiveFrom}
              onChange={setEffectiveFrom}
              placeholder={t('选择生效时间')}
              aria-label={t('该价格自何时起生效')}
            />
          ) : (
            <div>
              <Text size='small'>
                {editing ? t('初始价格') : ''}
                {t('适用于最早一条带日期记录之前的全部日志。')}
              </Text>
            </div>
          )}
          {!editing && hasInitial && (
            <div>
              <Text type='tertiary' size='small'>
                {t('该渠道已有初始价格，请在列表中直接修改。')}
              </Text>
            </div>
          )}
        </div>
        <div>
          <Text size='small' type='tertiary'>
            {t('计价方式')}
          </Text>
          <Select
            className='w-full'
            size='small'
            value={mode}
            optionList={[
              { label: t('成本倍率（人民币:美元）'), value: 'ratio' },
              { label: t('成本折扣'), value: 'discount' },
            ]}
            onChange={setMode}
            aria-label={t('计价方式')}
          />
        </div>
        <div>
          <Text size='small' type='tertiary'>
            {mode === 'discount' ? t('成本折扣') : t('成本倍率（人民币:美元）')}
          </Text>
          <InputNumber
            className='w-full'
            size='small'
            min={0}
            step={0.01}
            value={value}
            onChange={setValue}
            aria-label={
              mode === 'discount' ? t('成本折扣') : t('成本倍率（人民币:美元）')
            }
          />
        </div>
        {mode === 'discount' && (
          <div>
            <Text size='small' type='tertiary'>
              {t('结算汇率')}
            </Text>
            <InputNumber
              className='w-full'
              size='small'
              min={0}
              step={0.1}
              value={rate}
              onChange={setRate}
              aria-label={t('结算汇率')}
            />
          </div>
        )}
        <div className='sm:col-span-2'>
          <Text size='small' type='tertiary'>
            {t('备注')}
          </Text>
          <Input
            className='w-full'
            size='small'
            value={note}
            onChange={setNote}
            placeholder={t('选填')}
            aria-label={t('备注')}
          />
        </div>
      </div>

      {mode === 'discount' && (
        <div className='pt-1'>
          <Text type='tertiary' size='small'>
            {t(
              '结算汇率随该版本一起冻结，此后不随站点汇率变动，历史成本因此不会漂移。',
            )}
          </Text>
        </div>
      )}

      <div className='flex justify-end gap-2 pt-2'>
        <Button
          size='small'
          type='tertiary'
          onClick={onCancel}
          disabled={submitting}
        >
          {t('取消')}
        </Button>
        <Button
          size='small'
          type='primary'
          onClick={submit}
          loading={submitting}
          disabled={!isValid}
        >
          {t('保存')}
        </Button>
      </div>
    </div>
  );
};

/**
 * 渠道价格记录：上方「当前价」保存的是从现在起生效的价，这里补录、修改和删除记录
 * —— 成本按日志所属时段的记录分段计算，填错的价在这里改正。
 *
 * 生效时间为 0 的是初始价格，覆盖最早一条带日期记录之前的全部日志。它可以修改，
 * 也可以删除：删掉后由最早那条往前覆盖，之后还能重新设置；后端只拒绝删除渠道仅剩
 * 的最后一条。
 */
const CostVersionPanel = ({
  channelId,
  visible,
  exchangeRate,
  t,
  onChanged,
}) => {
  const [versions, setVersions] = useState([]);
  const [loading, setLoading] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  // null：没有打开表单；{ kind: 'add', initial }：补录（initial 预选初始价）；
  // { kind: 'edit', version }：修改某条记录。
  const [form, setForm] = useState(null);
  const [deletingId, setDeletingId] = useState(null);
  const hasInitial = versions.some((v) => Number(v.effective_from) === 0);

  const loadVersions = useCallback(async () => {
    if (!channelId) return;
    setLoading(true);
    try {
      const res = await API.get(`/api/cost/channels/${channelId}/versions`);
      if (res.data.success) {
        setVersions(res.data.data || []);
        setLoadFailed(false);
      } else {
        showError(res.data.message);
        setLoadFailed(true);
      }
    } catch (e) {
      // 拦截器已经提示过了，这里只记状态：空列表要读作"该渠道没有历史价"，
      // 与"没读到"是两件事，不能混为一谈。
      setLoadFailed(true);
    } finally {
      setLoading(false);
    }
  }, [channelId]);

  // 每次打开都重新拉取：弹窗关闭期间可能刚保存过当前价（后端会自动追加一条）。
  useEffect(() => {
    if (!visible) {
      setForm(null);
      return;
    }
    loadVersions();
  }, [visible, loadVersions]);

  const deleteVersion = async (id) => {
    setDeletingId(id);
    try {
      const res = await API.delete(`/api/cost/versions/${id}`);
      if (res.data.success) {
        showSuccess(t('已删除该版本'));
        loadVersions();
        onChanged && onChanged();
      } else {
        showError(res.data.message);
      }
    } finally {
      setDeletingId(null);
    }
  };

  const confirmDelete = (row) =>
    Modal.confirm({
      title: t('删除这条价格记录？'),
      content: t('它覆盖时段内的日志将改按相邻记录的价格计算。'),
      okButtonProps: { type: 'danger' },
      onOk: () => deleteVersion(row.id),
    });

  const formDone = () => {
    setForm(null);
    loadVersions();
    onChanged && onChanged();
  };

  const columns = [
    {
      key: 'effective_from',
      title: t('生效时间'),
      render: (_, row) =>
        Number(row.effective_from) === 0 ? (
          <Text type='tertiary'>{t('初始价格')}</Text>
        ) : (
          timestamp2string(row.effective_from)
        ),
    },
    {
      key: 'pricing',
      title: t('计价'),
      align: 'right',
      render: (_, row) => describeVersion(row, t),
    },
    {
      key: 'note',
      title: t('备注'),
      // 只翻译系统写入的几条备注；管理员自己填的备注原样显示。
      render: (_, row) => {
        if (!row.note) return '-';
        return SYSTEM_VERSION_NOTES.includes(row.note) ? t(row.note) : row.note;
      },
    },
    {
      key: 'action',
      title: '',
      align: 'right',
      render: (_, row) => (
        <div className='flex justify-end'>
          <Button
            icon={<IconEdit />}
            theme='borderless'
            size='small'
            aria-label={t('修改')}
            onClick={() => setForm({ kind: 'edit', version: row })}
          />
          <Button
            icon={<IconDelete />}
            type='danger'
            theme='borderless'
            size='small'
            aria-label={t('删除')}
            loading={deletingId === row.id}
            onClick={() => confirmDelete(row)}
          />
        </div>
      ),
    },
  ];

  return (
    <div>
      <div className='flex items-center justify-between pb-1'>
        <Text strong size='small'>
          {t('价格历史')}
        </Text>
        {!form && (
          <Button
            icon={<IconPlus />}
            theme='borderless'
            size='small'
            onClick={() => setForm({ kind: 'add', initial: false })}
          >
            {t('补录历史价')}
          </Button>
        )}
      </div>

      {form && (
        <VersionForm
          // 换一条记录编辑时重建表单，不沿用上一条的输入。
          key={form.kind === 'edit' ? `edit-${form.version.id}` : 'add'}
          channelId={channelId}
          version={form.kind === 'edit' ? form.version : undefined}
          hasInitial={hasInitial}
          initialSelected={form.kind === 'add' && form.initial}
          defaultExchangeRate={exchangeRate}
          t={t}
          onDone={formDone}
          onCancel={() => setForm(null)}
        />
      )}

      {!loadFailed &&
        !loading &&
        !hasInitial &&
        versions.length > 0 &&
        !form && (
          <div className='flex items-center justify-between gap-2 pb-1'>
            <Text type='tertiary' size='small'>
              {t('未设置初始价格：早于最早一条记录的日志按最早那条计价。')}
            </Text>
            <Button
              theme='borderless'
              size='small'
              onClick={() => setForm({ kind: 'add', initial: true })}
            >
              {t('设置初始价格')}
            </Button>
          </div>
        )}

      {loadFailed ? (
        <Text type='danger' size='small'>
          {t('价格历史加载失败')}
        </Text>
      ) : (
        <CardTable
          columns={columns}
          dataSource={versions}
          loading={loading}
          rowKey={(row) => `v-${row.id}`}
          hidePagination
          size='small'
        />
      )}
    </div>
  );
};

export default CostVersionPanel;
