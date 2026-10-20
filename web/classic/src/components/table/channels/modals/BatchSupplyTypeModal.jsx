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
import { Modal, Radio, RadioGroup, Typography } from '@douyinfe/semi-ui';

const BatchSupplyTypeModal = ({
  showBatchSetSupplyType,
  setShowBatchSetSupplyType,
  batchSetChannelSupplyType,
  batchSupplyTypeValue,
  setBatchSupplyTypeValue,
  selectedChannels,
  t,
}) => {
  return (
    <Modal
      title={t('批量设置供应方式')}
      visible={showBatchSetSupplyType}
      onOk={batchSetChannelSupplyType}
      onCancel={() => setShowBatchSetSupplyType(false)}
      maskClosable={false}
      centered={true}
      size='small'
      className='!rounded-lg'
    >
      <div className='mb-4'>
        <Typography.Text>{t('请选择要设置的供应方式')}</Typography.Text>
      </div>
      <RadioGroup
        type='button'
        value={batchSupplyTypeValue}
        onChange={(e) => setBatchSupplyTypeValue(e.target.value)}
      >
        <Radio value='key'>{t('Key')}</Radio>
        <Radio value='pool'>{t('号池供应商')}</Radio>
      </RadioGroup>
      <div className='mt-4'>
        <Typography.Text type='secondary'>
          {t('已选择 ${count} 个渠道').replace(
            '${count}',
            selectedChannels.length,
          )}
        </Typography.Text>
      </div>
    </Modal>
  );
};

export default BatchSupplyTypeModal;
