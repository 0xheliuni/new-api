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

import React, { useEffect, useRef, useState } from 'react';
import { Button, Col, Form, Row, Spin, Banner } from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../helpers';
import { useTranslation } from 'react-i18next';

const DEFAULT_INPUTS = {
  'image_detection_setting.global_enabled': false,
  'image_detection_setting.global_sample_rate': 1.0,
  'image_detection_setting.scheduled_enabled': false,
  'image_detection_setting.scheduled_cron': '0 */2 * * *',
  'image_detection_setting.scheduled_batch_limit': 200,
  'image_detection_setting.channel_scope': 'all',
  'image_detection_setting.selected_channel_ids': '[]',
  'image_detection_setting.check_resolution': true,
  'image_detection_setting.check_c2pa': true,
  'image_detection_setting.check_synth_id': true,
  'image_detection_setting.synth_id_force': false,
  // Never returned by the options API; the field only takes a new key.
  'image_detection_setting.provenance_api_key': '',
  'image_detection_setting.synth_id_rate_per_minute': 50,
  'image_detection_setting.synth_id_rate_per_day': 500,
  'image_detection_setting.c2pa_tool_path': '',
  'image_detection_setting.c2pa_timeout_seconds': 3,
};

const BOOL_KEYS = new Set([
  'image_detection_setting.global_enabled',
  'image_detection_setting.scheduled_enabled',
  'image_detection_setting.check_resolution',
  'image_detection_setting.check_c2pa',
  'image_detection_setting.check_synth_id',
  'image_detection_setting.synth_id_force',
]);

export default function ImageDetectionSetting() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({ ...DEFAULT_INPUTS });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState({ ...DEFAULT_INPUTS });
  const [installing, setInstalling] = useState(false);
  const [installMsg, setInstallMsg] = useState({ type: '', text: '' });

  const installC2PA = async () => {
    setInstalling(true);
    setInstallMsg({ type: '', text: '' });
    try {
      const res = await API.post('/api/image-detection/install-c2pa');
      const { success, message, data } = res.data;
      if (success && data?.path) {
        setInstallMsg({
          type: 'success',
          text: t('已安装到 {{path}}', { path: data.path }),
        });
        setInputs((prev) => ({
          ...prev,
          'image_detection_setting.c2pa_tool_path': data.path,
        }));
        showSuccess(t('安装成功'));
      } else {
        setInstallMsg({ type: 'error', text: message || t('安装失败') });
        showError(message || t('安装失败'));
      }
    } catch (err) {
      const msg = err?.message || t('安装失败');
      setInstallMsg({ type: 'error', text: msg });
      showError(msg);
    } finally {
      setInstalling(false);
    }
  };

  const parseValue = (key, value) => {
    if (BOOL_KEYS.has(key)) return value === 'true';
    if (key === 'image_detection_setting.global_sample_rate')
      return parseFloat(value);
    if (
      key === 'image_detection_setting.scheduled_batch_limit' ||
      key === 'image_detection_setting.synth_id_rate_per_minute' ||
      key === 'image_detection_setting.synth_id_rate_per_day' ||
      key === 'image_detection_setting.c2pa_timeout_seconds'
    ) {
      return parseInt(value, 10);
    }
    return value;
  };

  const getOptions = async () => {
    setLoading(true);
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (success) {
      const newInputs = { ...DEFAULT_INPUTS };
      data.forEach((item) => {
        if (item.key in newInputs) {
          newInputs[item.key] = parseValue(item.key, item.value);
        }
      });
      setInputs(newInputs);
      setInputsRow(structuredClone(newInputs));
      refForm.current.setValues(newInputs);
    } else {
      showError(message);
    }
    setLoading(false);
  };

  useEffect(() => {
    getOptions();
  }, []);

  function onSubmit() {
    // A blank key field keeps the saved key; only a newly entered one is sent.
    const updateArray = compareObjects(inputs, inputsRow).filter(
      (item) =>
        item.key !== 'image_detection_setting.provenance_api_key' ||
        inputs[item.key] !== '',
    );
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      const value = String(inputs[item.key]);
      return API.put('/api/option/', { key: item.key, value });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        setInputsRow(structuredClone(inputs));
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  const channelScopeOptions = [
    { value: 'all', label: t('全部渠道') },
    { value: 'selected', label: t('指定渠道') },
  ];

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('全局检测')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'image_detection_setting.global_enabled'}
                  label={t('启用全局图片检测')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.global_enabled': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('全局采样率')}
                  step={0.01}
                  min={0}
                  max={1}
                  field={'image_detection_setting.global_sample_rate'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.global_sample_rate': value,
                    })
                  }
                />
              </Col>
            </Row>
          </Form.Section>

          <Form.Section text={t('定时抽样')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'image_detection_setting.scheduled_enabled'}
                  label={t('启用定时检测')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.scheduled_enabled': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  label={t('定时检测 Cron 表达式')}
                  field={'image_detection_setting.scheduled_cron'}
                  placeholder='0 */2 * * *'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.scheduled_cron': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('每次处理条数上限')}
                  step={1}
                  min={1}
                  field={'image_detection_setting.scheduled_batch_limit'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.scheduled_batch_limit':
                        parseInt(value),
                    })
                  }
                />
              </Col>
            </Row>
          </Form.Section>

          <Form.Section text={t('渠道范围')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Select
                  label={t('渠道范围')}
                  field={'image_detection_setting.channel_scope'}
                  optionList={channelScopeOptions}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.channel_scope': value,
                    })
                  }
                />
              </Col>
              {inputs['image_detection_setting.channel_scope'] ===
                'selected' && (
                <Col xs={24} sm={12} md={12} lg={12} xl={12}>
                  <Form.Input
                    label={t('指定渠道 ID（JSON 数组）')}
                    field={'image_detection_setting.selected_channel_ids'}
                    placeholder='[1, 2, 3]'
                    onChange={(value) =>
                      setInputs({
                        ...inputs,
                        'image_detection_setting.selected_channel_ids': value,
                      })
                    }
                  />
                </Col>
              )}
            </Row>
          </Form.Section>

          <Form.Section text={t('检测项')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'image_detection_setting.check_resolution'}
                  label={t('分辨率校验')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.check_resolution': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'image_detection_setting.check_c2pa'}
                  label={t('C2PA 检测')}
                  extraText={t(
                    '仅认可由 OpenAI 签发、记录了 AI 生成且通过可信校验的签名，其余图片继续执行内容溯源',
                  )}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.check_c2pa': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'image_detection_setting.check_synth_id'}
                  label={t('内容溯源')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.check_synth_id': value,
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={24} md={16} lg={16} xl={16}>
                <Form.Switch
                  field={'image_detection_setting.synth_id_force'}
                  label={t('强制内容溯源')}
                  extraText={t(
                    '即使元数据（C2PA）已检测到 OpenAI 信号，也执行内容溯源',
                  )}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  disabled={!inputs['image_detection_setting.check_synth_id']}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.synth_id_force': value,
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('C2PA 超时时间(秒)')}
                  step={1}
                  min={1}
                  suffix={t('秒')}
                  field={'image_detection_setting.c2pa_timeout_seconds'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.c2pa_timeout_seconds':
                        parseInt(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={24} md={16} lg={16} xl={16}>
                <Form.Input
                  label={t('c2patool 路径')}
                  field={'image_detection_setting.c2pa_tool_path'}
                  placeholder={t('留空则使用 PATH 中的 c2patool')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.c2pa_tool_path': value,
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col span={24}>
                <Button
                  size='default'
                  loading={installing}
                  onClick={installC2PA}
                >
                  {t('下载并安装 c2patool')}
                </Button>
                {installMsg.text && (
                  <Banner
                    type={installMsg.type === 'success' ? 'success' : 'danger'}
                    description={installMsg.text}
                    fullMode={false}
                    style={{ marginTop: 8 }}
                  />
                )}
                <p
                  style={{
                    color: 'var(--semi-color-text-2)',
                    fontSize: 12,
                    marginTop: 8,
                  }}
                >
                  {t(
                    '从 GitHub 下载当前平台的 v0.28.2 二进制并写入容器内 /usr/local/bin，自动回填上方路径。',
                  )}
                </p>
              </Col>
            </Row>
          </Form.Section>

          <Form.Section text={t('内容溯源')}>
            <Row gutter={16}>
              <Col xs={24} sm={24} md={16} lg={16} xl={16}>
                <Form.Input
                  field={'image_detection_setting.provenance_api_key'}
                  label={t('OpenAI 密钥')}
                  type='password'
                  autoComplete='new-password'
                  placeholder={t('敏感信息不会发送到前端显示')}
                  extraText={t(
                    '用于内容溯源。留空则保留已保存的密钥；未保存时使用环境变量 OPENAI_API_KEY_DETECTION 或 OPENAI_API_KEY。',
                  )}
                  disabled={!inputs['image_detection_setting.check_synth_id']}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.provenance_api_key':
                        value.trim(),
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('每分钟最大调用次数')}
                  step={1}
                  min={1}
                  field={'image_detection_setting.synth_id_rate_per_minute'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.synth_id_rate_per_minute':
                        parseInt(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('每日最大调用次数')}
                  step={1}
                  min={1}
                  field={'image_detection_setting.synth_id_rate_per_day'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_detection_setting.synth_id_rate_per_day':
                        parseInt(value),
                    })
                  }
                />
              </Col>
            </Row>
          </Form.Section>

          <Row>
            <Button size='default' onClick={onSubmit}>
              {t('保存图片检测设置')}
            </Button>
          </Row>
        </Form>
      </Spin>
    </>
  );
}
