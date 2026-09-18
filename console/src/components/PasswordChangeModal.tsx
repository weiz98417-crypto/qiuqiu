import { useState } from 'react';
import { App as AntApp, Button, Form, Input, Modal } from 'antd';
import { changeSelfPassword } from '../api/client';

interface PasswordChangeModalProps {
  open: boolean;
  // 首登强制改密时不可关闭（直到改密成功）。
  forced?: boolean;
  onClose: () => void;
  onChanged: () => void;
}

// 自助改密弹窗（ADR-0010 task 2.3）：旧密码必填，新密码 ≥10 字符；首登
// 强制改密时作为不可关闭的拦路弹窗使用。
export default function PasswordChangeModal({ open, forced, onClose, onChanged }: PasswordChangeModalProps) {
  const { message: messageApi } = AntApp.useApp();
  const [form] = Form.useForm();
  const [submitting, setSubmitting] = useState(false);

  const submit = async (values: { oldPassword: string; newPassword: string; confirm: string }) => {
    setSubmitting(true);
    try {
      await changeSelfPassword(values.oldPassword, values.newPassword);
      messageApi.success('密码已更新，下次登录请使用新密码');
      form.resetFields();
      onChanged();
      if (!forced) onClose();
    } catch (err) {
      messageApi.error(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title={forced ? '首次登录，请修改临时密码' : '修改密码'}
      open={open}
      onCancel={forced ? undefined : onClose}
      closable={!forced}
      maskClosable={!forced}
      keyboard={!forced}
      footer={null}
      width={420}
    >
      {forced ? (
        <p style={{ color: '#F2C94C' }}>
          当前是导演签发的临时密码，改密完成前无法使用管理台。
        </p>
      ) : null}
      <Form form={form} layout="vertical" onFinish={submit}>
        <Form.Item name="oldPassword" label="当前密码" rules={[{ required: true, message: '请输入当前密码' }]}>
          <Input.Password aria-label="当前密码" autoComplete="current-password" />
        </Form.Item>
        <Form.Item
          name="newPassword"
          label="新密码"
          extra="至少 10 个字符"
          rules={[
            { required: true, message: '请输入新密码' },
            { min: 10, message: '新密码至少 10 个字符' },
          ]}
        >
          <Input.Password aria-label="新密码" autoComplete="new-password" />
        </Form.Item>
        <Form.Item
          name="confirm"
          label="确认新密码"
          dependencies={['newPassword']}
          rules={[
            { required: true, message: '请再次输入新密码' },
            ({ getFieldValue }) => ({
              validator(_, value) {
                if (!value || getFieldValue('newPassword') === value) return Promise.resolve();
                return Promise.reject(new Error('两次输入的新密码不一致'));
              },
            }),
          ]}
        >
          <Input.Password aria-label="确认新密码" autoComplete="new-password" />
        </Form.Item>
        <Button type="primary" htmlType="submit" block loading={submitting}>
          确认修改
        </Button>
      </Form>
    </Modal>
  );
}
