import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor, cleanup } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { App as AntApp } from 'antd';
import Knowledge from './Knowledge';
import { validateKnowledgeConfidence, validateKnowledgeProbability, validateKnowledgeTurns } from '../api/knowledge';
import type { KnowledgeEntry, KnowledgeList } from '../api/knowledge';

// 知识策展台组件测试（knowledge-curation-console 7.3）：列表渲染（生效状态
// tag + 待复查标记）、过滤器（待复查开关走服务端参数）、编辑抽屉表单校验
// （必填 + 确信度范围）与保存即生效的客户端形状、auditor 只读降级。
// jsdom 缺 antd Table 依赖的浏览器 API，先补桩。

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
(globalThis as unknown as { ResizeObserver?: unknown }).ResizeObserver ??= ResizeObserverStub;
if (!window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
}

const { knowledgeApiMocks, operatorState } = vi.hoisted(() => ({
  knowledgeApiMocks: { list: vi.fn(), update: vi.fn(), create: vi.fn(), get: vi.fn() },
  operatorState: { operator: null as null | { name: string; scopes: string[] } },
}));

vi.mock('../api/knowledge', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/knowledge')>();
  return { ...actual, knowledgeApi: knowledgeApiMocks };
});

vi.mock('../api/operator', () => ({
  useOperator: () => ({
    operator: operatorState.operator,
    loading: false,
    isDirector: Boolean(operatorState.operator?.scopes.includes('operator:match:write')),
  }),
}));

function entry(overrides: Partial<KnowledgeEntry>): KnowledgeEntry {
  return {
    id: 'rule-offside',
    topics: ['越位', 'offside'],
    answer: '传球一瞬间比对方最后一名防守球员更靠近球门线就算越位。',
    source: 'IFAB Law 11',
    confidence: 0.95,
    effectiveAt: '2026-07-01T00:00:00Z',
    status: 'active',
    dueReview: false,
    priority: 0,
    inclusionGroup: '',
    stickyTurns: 0,
    cooldownTurns: 0,
    probability: 1,
    createdBy: 'seed',
    createdAt: '2026-09-30T08:00:00Z',
    updatedAt: '2026-09-30T08:00:00Z',
    ...overrides,
  };
}

function listPayload(entries: KnowledgeEntry[]): KnowledgeList {
  return { entries, total: entries.length, page: 1, pageSize: 20 };
}

function renderPage() {
  return render(
    <AntApp>
      <Knowledge />
    </AntApp>,
  );
}

beforeEach(() => {
  operatorState.operator = { name: '阿琴', scopes: ['operator:match:write'] };
  knowledgeApiMocks.list.mockResolvedValue(listPayload([]));
  knowledgeApiMocks.update.mockResolvedValue({ entry: entry({}) });
  knowledgeApiMocks.create.mockResolvedValue({ entry: entry({ id: 'rule-stoppage' }) });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('Knowledge 策展台', () => {
  it('列表渲染条目：生效状态 tag、待复查标记与字段', async () => {
    knowledgeApiMocks.list.mockResolvedValue(
      listPayload([
        entry({ id: 'rule-offside', status: 'active', dueReview: false }),
        entry({
          id: 'player-stale',
          topics: ['旧档案'],
          answer: '转会窗前的旧档案。',
          status: 'active',
          dueReview: true,
        }),
        entry({
          id: 'rule-future',
          topics: ['新规'],
          answer: '下赛季才生效的新规。',
          status: 'pending',
          dueReview: false,
        }),
      ]),
    );
    renderPage();
    await waitFor(() => expect(screen.getByText('rule-offside')).toBeInTheDocument());
    expect(screen.getByText('rule-future')).toBeInTheDocument();
    // 生效二态 tag（api/knowledge.ts 的共用标签）。
    expect(screen.getAllByText('生效中')).toHaveLength(2);
    expect(screen.getByText('待生效')).toBeInTheDocument();
    // 转会窗复查标记只落在过期条目上（另一个「待复查」是过滤开关按钮）。
    expect(screen.getAllByText('待复查')).toHaveLength(2);
    expect(screen.getByText(/转会窗前的旧档案/)).toBeInTheDocument();
  });

  it('待复查开关走服务端过滤参数并刷新列表', async () => {
    renderPage();
    await waitFor(() => expect(knowledgeApiMocks.list).toHaveBeenCalled());
    const dueToggle = screen.getByRole('button', { name: '待复查' });
    fireEvent.click(dueToggle);
    await waitFor(() =>
      expect(knowledgeApiMocks.list).toHaveBeenLastCalledWith(
        expect.objectContaining({ due: 'review', page: 1 }),
      ),
    );
    // 再点一次关掉：参数回到无 due。
    fireEvent.click(screen.getByRole('button', { name: '待复查' }));
    await waitFor(() =>
      expect(knowledgeApiMocks.list).toHaveBeenLastCalledWith(
        expect.objectContaining({ due: undefined, page: 1 }),
      ),
    );
  });

  it('生效状态过滤：选择「待生效」即带 status 参数', async () => {
    renderPage();
    await waitFor(() => expect(knowledgeApiMocks.list).toHaveBeenCalled());
    fireEvent.mouseDown(screen.getByText('生效状态'));
    await waitFor(() => expect(screen.getByText('待生效')).toBeInTheDocument());
    fireEvent.click(screen.getByText('待生效'));
    await waitFor(() =>
      expect(knowledgeApiMocks.list).toHaveBeenLastCalledWith(
        expect.objectContaining({ status: 'pending' }),
      ),
    );
  });

  it('表单校验：答案空白拦在客户端，不发请求（确信度口径见纯函数测试）', async () => {
    knowledgeApiMocks.list.mockResolvedValue(listPayload([entry({ answer: '原始答案。' })]));
    renderPage();
    await waitFor(() => expect(screen.getByText('rule-offside')).toBeInTheDocument());
    fireEvent.click(screen.getByText('编辑'));
    // 抽屉表单预填：页面上唯一的 TextArea 就是答案原文（Drawer 挂 body）。
    await waitFor(() => {
      const textarea = document.body.querySelector('textarea');
      expect(textarea).not.toBeNull();
      expect((textarea as HTMLTextAreaElement).value).toBe('原始答案。');
    });
    fireEvent.change(document.body.querySelector('textarea')!, { target: { value: '   ' } });
    fireEvent.click(screen.getByRole('button', { name: '保存并生效' }));
    await waitFor(() => expect(screen.getByText('答案原文不能为空白')).toBeInTheDocument());
    expect(knowledgeApiMocks.update).not.toHaveBeenCalled();
  });

  it('确信度校验口径（api/knowledge.ts 纯函数）：空值与越界都拒绝', () => {
    expect(validateKnowledgeConfidence(null)).toBe('请填写确信度');
    expect(validateKnowledgeConfidence(Number.NaN)).toBe('请填写确信度');
    expect(validateKnowledgeConfidence(-0.1)).toBe('确信度必须在 0 到 1 之间');
    expect(validateKnowledgeConfidence(1.01)).toBe('确信度必须在 0 到 1 之间');
    expect(validateKnowledgeConfidence(0)).toBeUndefined();
    expect(validateKnowledgeConfidence(0.6)).toBeUndefined();
    expect(validateKnowledgeConfidence(1)).toBeUndefined();
  });

  it('参数学校验口径（knowledge-worldinfo）：概率越界拒绝、0 合法；轮数负数与非整数拒绝', () => {
    expect(validateKnowledgeProbability(null)).toBe('请填写触发概率');
    expect(validateKnowledgeProbability(-0.1)).toBe('触发概率必须在 0 到 1 之间');
    expect(validateKnowledgeProbability(1.5)).toBe('触发概率必须在 0 到 1 之间');
    expect(validateKnowledgeProbability(0)).toBeUndefined();
    expect(validateKnowledgeProbability(0.3)).toBeUndefined();
    expect(validateKnowledgeTurns(null)).toBe('请填写轮数');
    expect(validateKnowledgeTurns(-1)).toBe('轮数必须是非负整数');
    expect(validateKnowledgeTurns(1.5)).toBe('轮数必须是非负整数');
    expect(validateKnowledgeTurns(0)).toBeUndefined();
    expect(validateKnowledgeTurns(3)).toBeUndefined();
  });

  it('保存即生效：有效提交按契约形状 PUT（日期格式化 + 答案去空白 + 参数学默认值）并刷新', async () => {
    knowledgeApiMocks.list.mockResolvedValue(listPayload([entry({})]));
    renderPage();
    await waitFor(() => expect(screen.getByText('rule-offside')).toBeInTheDocument());
    fireEvent.click(screen.getByText('编辑'));
    await waitFor(() => expect(document.body.querySelector('textarea')).not.toBeNull());
    // 答案输入前后带空白：提交形状按保存纪律去空白。
    fireEvent.change(document.body.querySelector('textarea')!, { target: { value: '  原始答案。  ' } });
    fireEvent.click(screen.getByRole('button', { name: '保存并生效' }));
    await waitFor(() => expect(knowledgeApiMocks.update).toHaveBeenCalled());
    expect(knowledgeApiMocks.update).toHaveBeenCalledWith(
      'rule-offside',
      expect.objectContaining({
        answer: '原始答案。',
        confidence: 0.95,
        effectiveAt: '2026-07-01',
        topics: ['越位', 'offside'],
        source: 'IFAB Law 11',
        // 参数学五字段随保存透传（未改过=默认值，现状行为）。
        priority: 0,
        inclusionGroup: '',
        stickyTurns: 0,
        cooldownTurns: 0,
        probability: 1,
      }),
    );
    // 保存成功后列表重取（保存即生效回显）。
    await waitFor(() => expect(knowledgeApiMocks.list).toHaveBeenCalledTimes(2));
  });

  it('参数学五字段：编辑预填服务端值、保存按原样透传（knowledge-worldinfo）', async () => {
    knowledgeApiMocks.list.mockResolvedValue(
      listPayload([entry({ priority: 7, inclusionGroup: '规则组', stickyTurns: 2, cooldownTurns: 5, probability: 0.35 })]),
    );
    renderPage();
    await waitFor(() => expect(screen.getByText('rule-offside')).toBeInTheDocument());
    fireEvent.click(screen.getByText('编辑'));
    // 预填断言：抽屉里按 label 找到四个数字输入与一个文本输入的现值。
    await waitFor(() => {
      const spinbuttons = document.body.querySelectorAll('input[role="spinbutton"]');
      expect(spinbuttons.length).toBeGreaterThanOrEqual(5);
      expect((spinbuttons[0] as HTMLInputElement).value).toBe('0.95'); // 确信度
      expect((spinbuttons[1] as HTMLInputElement).value).toBe('7'); // priority
      expect((spinbuttons[2] as HTMLInputElement).value).toBe('2'); // sticky
      expect((spinbuttons[3] as HTMLInputElement).value).toBe('5'); // cooldown
      expect((spinbuttons[4] as HTMLInputElement).value).toBe('0.35'); // probability
    });
    const groupInput = document.body.querySelector('input[placeholder="如：越位规则组"]') as HTMLInputElement;
    expect(groupInput.value).toBe('规则组');
    // 保存：参数学五字段原样进 PUT payload。
    fireEvent.click(screen.getByRole('button', { name: '保存并生效' }));
    await waitFor(() => expect(knowledgeApiMocks.update).toHaveBeenCalled());
    expect(knowledgeApiMocks.update).toHaveBeenCalledWith(
      'rule-offside',
      expect.objectContaining({
        priority: 7,
        inclusionGroup: '规则组',
        stickyTurns: 2,
        cooldownTurns: 5,
        probability: 0.35,
      }),
    );
  });

  it('auditor 只读：无保存按钮，抽屉出只读警示', async () => {
    operatorState.operator = { name: '小阅', scopes: ['operator:trace:read'] };
    knowledgeApiMocks.list.mockResolvedValue(listPayload([entry({})]));
    renderPage();
    await waitFor(() => expect(screen.getByText('rule-offside')).toBeInTheDocument());
    expect(screen.getByText(/审计（auditor）角色，条目为只读/)).toBeInTheDocument();
    fireEvent.click(screen.getByText('编辑'));
    await waitFor(() => expect(screen.getByText('审计角色只读，不能保存修改')).toBeInTheDocument());
    expect(screen.queryByRole('button', { name: '保存并生效' })).not.toBeInTheDocument();
  });

  it('新建条目：非法 id 被 slug 校验拦在客户端，不发请求', async () => {
    renderPage();
    await waitFor(() => expect(knowledgeApiMocks.list).toHaveBeenCalled());
    fireEvent.click(screen.getByTestId('knowledge-create'));
    await waitFor(() => expect(screen.getByText('新建知识条目')).toBeInTheDocument());
    fireEvent.change(screen.getByPlaceholderText('如：rule-stoppage'), {
      target: { value: 'Rule_Offside' },
    });
    fireEvent.click(screen.getByRole('button', { name: '新建并生效' }));
    await waitFor(() =>
      expect(screen.getByText('id 只能是小写字母、数字和连字符（2-64 字符）')).toBeInTheDocument(),
    );
    expect(knowledgeApiMocks.create).not.toHaveBeenCalled();
  });

  it('新建条目：合法提交按契约形状 POST（id + 去空白字段）并刷新列表', async () => {
    renderPage();
    await waitFor(() => expect(knowledgeApiMocks.list).toHaveBeenCalled());
    fireEvent.click(screen.getByTestId('knowledge-create'));
    await waitFor(() => expect(screen.getByPlaceholderText('如：rule-stoppage')).toBeInTheDocument());

    fireEvent.change(screen.getByPlaceholderText('如：rule-stoppage'), {
      target: { value: ' rule-stoppage ' },
    });
    // topics：tags 模式的搜索输入（placeholder 不是 input 属性,按类名查）,
    // 逗号触发 tokenSeparators 成签。
    const searchInput = document.body.querySelector(
      '.ant-drawer-body input.ant-select-input',
    ) as HTMLInputElement;
    fireEvent.change(searchInput, { target: { value: '补时,' } });
    fireEvent.change(document.body.querySelector('textarea')!, {
      target: { value: '  新条目答案。  ' },
    });
    fireEvent.change(screen.getByPlaceholderText(/IFAB/), { target: { value: 'IFAB Law 7' } });
    const confidenceInput = document.body.querySelector('input[role="spinbutton"]') as HTMLInputElement;
    fireEvent.change(confidenceInput, { target: { value: '0.9' } });
    fireEvent.blur(confidenceInput);
    // 测试环境无 ConfigProvider，antd 默认英文 locale，DatePicker placeholder 是 en 口径。
    const dateInput = screen.getByPlaceholderText('Select date');
    fireEvent.change(dateInput, { target: { value: '2026-08-01' } });
    fireEvent.keyDown(dateInput, { key: 'Enter', keyCode: 13, which: 13 });

    fireEvent.click(screen.getByRole('button', { name: '新建并生效' }));
    await waitFor(() => expect(knowledgeApiMocks.create).toHaveBeenCalled());
    expect(knowledgeApiMocks.create).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 'rule-stoppage',
        answer: '新条目答案。',
        topics: ['补时'],
        source: 'IFAB Law 7',
        confidence: 0.9,
        effectiveAt: '2026-08-01',
        // 新建态参数学默认值=现状行为（表单 initialValues）。
        priority: 0,
        inclusionGroup: '',
        stickyTurns: 0,
        cooldownTurns: 0,
        probability: 1,
      }),
    );
    // 新建成功后列表重取。
    await waitFor(() => expect(knowledgeApiMocks.list).toHaveBeenCalledTimes(2));
  });

  it('新建条目：同 id 撞车时把 409 的服务端错误透出', async () => {
    knowledgeApiMocks.create.mockRejectedValue(new Error('knowledge entry id already exists'));
    renderPage();
    await waitFor(() => expect(knowledgeApiMocks.list).toHaveBeenCalled());
    fireEvent.click(screen.getByTestId('knowledge-create'));
    await waitFor(() => expect(screen.getByPlaceholderText('如：rule-stoppage')).toBeInTheDocument());

    fireEvent.change(screen.getByPlaceholderText('如：rule-stoppage'), {
      target: { value: 'rule-stoppage' },
    });
    const searchInput = document.body.querySelector(
      '.ant-drawer-body input.ant-select-input',
    ) as HTMLInputElement;
    fireEvent.change(searchInput, { target: { value: '补时,' } });
    fireEvent.change(document.body.querySelector('textarea')!, { target: { value: '答案。' } });
    const confidenceInput = document.body.querySelector('input[role="spinbutton"]') as HTMLInputElement;
    fireEvent.change(confidenceInput, { target: { value: '0.9' } });
    fireEvent.blur(confidenceInput);
    const dateInput = screen.getByPlaceholderText('Select date');
    fireEvent.change(dateInput, { target: { value: '2026-08-01' } });
    fireEvent.keyDown(dateInput, { key: 'Enter', keyCode: 13, which: 13 });
    fireEvent.click(screen.getByRole('button', { name: '新建并生效' }));
    await waitFor(() =>
      expect(screen.getAllByText('knowledge entry id already exists').length).toBeGreaterThan(0),
    );
  });
});
