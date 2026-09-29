import { useCallback, useState } from 'react';
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  DatePicker,
  Drawer,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import dayjs, { type Dayjs } from 'dayjs';
import { useOperator } from '../api/operator';
import { fmtDateTime } from '../api/format';
import { useAsync } from '../api/useAsync';
import ConsolePageShell from '../components/ConsolePageShell';
import {
  knowledgeApi,
  knowledgeStatusColors,
  knowledgeStatusLabels,
  validateKnowledgeConfidence,
} from '../api/knowledge';
import type { KnowledgeEntry, KnowledgeList, KnowledgeStatus, KnowledgeUpdate } from '../api/knowledge';

const { Paragraph, Text } = Typography;

// 知识策展台（knowledge-curation-console 7.3）：条目列表（检索 / 生效窗口 /
// 「待复查」过滤器）+ 单条编辑抽屉（FastGPT 表单形态：字段表单化 + 即时校验；
// 列表交互照 Coze 资源列表：筛选栏 + 服务端分页表格 + 行内编辑入口）。
// 保存即生效（后端 executeOperatorWrite + 审计），无草稿流（qiuqiu 运营
// 一两人，直接保存 + 审计即足够，见 proposal Non-goals）。

interface KnowledgeFormValues {
  topics: string[];
  answer: string;
  source?: string;
  confidence: number;
  effectiveAt: Dayjs;
}

export default function Knowledge() {
  const { message: messageApi } = AntApp.useApp();
  const { loading: operatorLoading, isDirector } = useOperator();
  const [form] = Form.useForm<KnowledgeFormValues>();

  // 服务端过滤/分页单源：query 一次拿全量参数，useAsync 依赖数组只收标量。
  const [q, setQ] = useState('');
  const [status, setStatus] = useState<KnowledgeStatus | ''>('');
  const [dueReview, setDueReview] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const list = useAsync<KnowledgeList>(
    () =>
      knowledgeApi.list({
        q: q || undefined,
        status: status || undefined,
        due: dueReview ? 'review' : undefined,
        page,
        pageSize,
      }),
    [q, status, dueReview, page, pageSize],
  );

  const [editing, setEditing] = useState<KnowledgeEntry | null>(null);
  const [saving, setSaving] = useState(false);

  const openEditor = useCallback(
    (entry: KnowledgeEntry) => {
      setEditing(entry);
      form.setFieldsValue({
        topics: entry.topics,
        answer: entry.answer,
        source: entry.source,
        confidence: entry.confidence,
        effectiveAt: dayjs(entry.effectiveAt),
      });
    },
    [form],
  );

  const closeEditor = useCallback(() => {
    setEditing(null);
    form.resetFields();
  }, [form]);

  const saveEntry = useCallback(
    async (values: KnowledgeFormValues) => {
      if (!editing) return;
      setSaving(true);
      try {
        const payload: KnowledgeUpdate = {
          topics: values.topics.map((topic) => topic.trim()).filter(Boolean),
          answer: values.answer.trim(),
          source: (values.source ?? '').trim(),
          confidence: values.confidence,
          effectiveAt: values.effectiveAt.format('YYYY-MM-DD'),
        };
        await knowledgeApi.update(editing.id, payload);
        messageApi.success(`已保存并生效：${editing.id}`);
        closeEditor();
        await list.reload();
      } catch (err) {
        messageApi.error(err instanceof Error ? err.message : String(err));
      } finally {
        setSaving(false);
      }
    },
    [editing, closeEditor, list, messageApi],
  );

  const columns: ColumnsType<KnowledgeEntry> = [
    { title: '条目 id', dataIndex: 'id', key: 'id', width: 180 },
    {
      title: '匹配关键词',
      dataIndex: 'topics',
      key: 'topics',
      width: 220,
      render: (topics: string[]) => (
        <Space size={4} wrap>
          {topics.map((topic) => (
            <Tag key={topic} color="blue">
              {topic}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '答案原文（逐字锚）',
      dataIndex: 'answer',
      key: 'answer',
      render: (answer: string) => (
        <Paragraph ellipsis={{ rows: 2, expandable: true, symbol: '展开' }} style={{ marginBottom: 0 }}>
          {answer}
        </Paragraph>
      ),
    },
    {
      title: '出处',
      dataIndex: 'source',
      key: 'source',
      width: 180,
      render: (source: string) => (
        <Text type="secondary" ellipsis style={{ maxWidth: 160 }}>
          {source || '—'}
        </Text>
      ),
    },
    {
      title: '确信度',
      dataIndex: 'confidence',
      key: 'confidence',
      width: 90,
      render: (confidence: number) => confidence.toFixed(2),
    },
    {
      title: '生效窗口',
      dataIndex: 'effectiveAt',
      key: 'effectiveAt',
      width: 110,
      render: (effectiveAt: string) => (effectiveAt ? effectiveAt.slice(0, 10) : '—'),
    },
    {
      title: '状态',
      key: 'status',
      width: 150,
      render: (_, record) => (
        <Space size={4} wrap>
          <Tag color={knowledgeStatusColors[record.status]}>
            {knowledgeStatusLabels[record.status]}
          </Tag>
          {record.dueReview ? <Tag color="red">待复查</Tag> : null}
        </Space>
      ),
    },
    {
      title: '更新',
      key: 'updatedAt',
      width: 170,
      render: (_, record) => (
        <Text type="secondary" style={{ fontSize: 12 }}>
          {fmtDateTime(record.updatedAt)}
          {record.createdBy ? ` · ${record.createdBy}` : ''}
        </Text>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 80,
      render: (_, record) => (
        <Button type="link" size="small" onClick={() => openEditor(record)} data-entry-id={record.id}>
          编辑
        </Button>
      ),
    },
  ];

  return (
    <ConsolePageShell
      title="知识条目"
      subtitle={
        <Text type="secondary" style={{ fontSize: 12 }}>
          第二事实域（ADR-0017）：保存即生效，检索照旧逐字拼装
        </Text>
      }
      error={list.error}
      errorTitle="知识条目加载失败"
      loading={list.loading || operatorLoading}
      onReload={() => void list.reload()}
    >
      {!operatorLoading && !isDirector ? (
        <Alert
          type="warning"
          showIcon
          message="当前令牌为审计（auditor）角色，条目为只读"
          style={{ marginBottom: 16 }}
        />
      ) : null}
      <Card>
        <Space size={8} wrap style={{ marginBottom: 16 }}>
          <Input.Search
            allowClear
            placeholder="搜索关键词 / 答案 / 出处"
            style={{ width: 260 }}
            onSearch={(value) => {
              setQ(value.trim());
              setPage(1);
            }}
          />
          <Select<KnowledgeStatus | ''>
            allowClear
            placeholder="生效状态"
            style={{ width: 130 }}
            value={status || undefined}
            onChange={(value) => {
              setStatus(value ?? '');
              setPage(1);
            }}
            options={[
              { value: 'active', label: '生效中' },
              { value: 'pending', label: '待生效' },
            ]}
          />
          <Button
            type={dueReview ? 'primary' : 'default'}
            danger={dueReview}
            onClick={() => {
              setDueReview((current) => !current);
              setPage(1);
            }}
          >
            待复查
          </Button>
        </Space>
        <Table<KnowledgeEntry>
          size="small"
          rowKey="id"
          columns={columns}
          dataSource={list.data?.entries ?? []}
          locale={{ emptyText: '暂无知识条目' }}
          pagination={{
            current: page,
            pageSize,
            total: list.data?.total ?? 0,
            showSizeChanger: true,
            onChange: (nextPage, nextSize) => {
              setPage(nextSize !== pageSize ? 1 : nextPage);
              setPageSize(nextSize);
            },
          }}
        />
      </Card>
      <Drawer
        title={editing ? `编辑知识条目 · ${editing.id}` : '编辑知识条目'}
        width={520}
        open={Boolean(editing)}
        onClose={closeEditor}
        destroyOnHidden
        footer={
          isDirector ? (
            <Space style={{ float: 'right' }}>
              <Button onClick={closeEditor}>取消</Button>
              <Button type="primary" loading={saving} onClick={() => form.submit()}>
                保存并生效
              </Button>
            </Space>
          ) : null
        }
      >
        {editing ? (
          <>
            {!isDirector ? (
              <Alert
                type="warning"
                showIcon
                message="审计角色只读，不能保存修改"
                style={{ marginBottom: 16 }}
              />
            ) : null}
            <Form<KnowledgeFormValues>
              form={form}
              layout="vertical"
              onFinish={(values) => void saveEntry(values)}
            >
              <Form.Item
                name="topics"
                label="匹配关键词（topics）"
                rules={[{ required: true, message: '请至少填写一个匹配关键词' }]}
              >
                <Select
                  mode="tags"
                  open={false}
                  tokenSeparators={[',', '，']}
                  placeholder="回车或逗号分隔，如：越位 / offside"
                />
              </Form.Item>
              <Form.Item
                name="answer"
                label="答案原文"
                extra="逐字拼装的答案锚，运行时原样回答——写成球球口吻的口语"
                rules={[
                  { required: true, message: '请填写答案原文' },
                  { whitespace: true, message: '答案原文不能为空白' },
                ]}
              >
                <Input.TextArea rows={4} placeholder="策展时就写成球球口吻的口语" />
              </Form.Item>
              <Form.Item name="source" label="出处（source）">
                <Input placeholder="如：IFAB Laws of the Game, Law 11" />
              </Form.Item>
              <Form.Item
                name="confidence"
                label="确信度（confidence）"
                extra="低于 0.6 的条目不出答案——「不知道」好过「不太对」"
                rules={[
                  { required: true, message: '请填写确信度' },
                  { validator: (_rule, value: number | null) => {
                      const problem = validateKnowledgeConfidence(typeof value === 'number' ? value : null);
                      return problem ? Promise.reject(new Error(problem)) : Promise.resolve();
                    } },
                ]}
              >
                <InputNumber min={0} max={1} step={0.05} style={{ width: 160 }} />
              </Form.Item>
              <Form.Item
                name="effectiveAt"
                label="生效日期（effective_at）"
                extra="早于最近一次转会窗关闭（7 月 / 1 月）的条目会进「待复查」"
                rules={[{ required: true, message: '请选择生效日期' }]}
              >
                <DatePicker style={{ width: 200 }} format="YYYY-MM-DD" />
              </Form.Item>
            </Form>
          </>
        ) : null}
      </Drawer>
    </ConsolePageShell>
  );
}
