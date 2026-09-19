import { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, App as AntApp, Button, Card, Col, Input, Row, Segmented, Select, Space, Tag, Typography } from 'antd';
import { useParams } from 'react-router-dom';
import { ApiError } from '../api/client';
import BehaviorBar from '../director/BehaviorBar';
import DraftCard from '../director/DraftCard';
import FactTimeline from '../director/FactTimeline';
import VoiceDraft from '../director/VoiceDraft';
import {
  type DirectorConflict,
  type DirectorEventRow,
  type MatchClockState,
  type RosterPlayer,
  correctEvent,
  factTransition,
  loadClock,
  loadConfig,
  loadEvents,

  patchClock,
  publishEvent,
  resolveConflict,
} from '../director/api';
import { createDraft, formatClock, toEventPayload, updateDraft, type Draft } from '../director/event-model';
import {
  draftFromEvent,
  elapsedClockSeconds,
  formFromDraft,
  mergedDraftForSubmit,
  type DraftFormState,
} from '../director/draft-form';

const { Text } = Typography;

interface TeamRoster {
  players: RosterPlayer[];
  active: boolean;
}

// 实战导演页（新版，ADR-0011）：/console/match/:matchId/director。
// 老页面 operator.html#live 的四步迁移：事件模型 → 草稿卡 → 语音流 →
// 事实时间线；请求形状与 operator-control evals 断言逐字一致。
export default function DirectorLive() {
  const { matchId = '' } = useParams<{ matchId: string }>();
  const { message: messageApi } = AntApp.useApp();

  const [homeTeam, setHomeTeam] = useState('');
  const [awayTeam, setAwayTeam] = useState('');
  const [rosters, setRosters] = useState<{ home: TeamRoster; away: TeamRoster }>({
    home: { players: [], active: true },
    away: { players: [], active: false },
  });
  const [side, setSide] = useState<'home' | 'away'>('home');
  const [playerSearch, setPlayerSearch] = useState('');
  const [clock, setClock] = useState<MatchClockState>({ period: 'pre_match', elapsedSeconds: 0, running: false, version: 0 });
  const [confirmedScore, setConfirmedScore] = useState({ home: 0, away: 0 });
  const [draft, setDraft] = useState<Draft>(() => createDraft());
  const [form, setForm] = useState<DraftFormState>({
    occurredClock: '00:00',
    intensity: '3',
    factStatus: 'confirmed',
    action: '',
    mode: 'auto',
    description: '',
    proactive: '',
    correctionReason: '',
    scoreHome: '',
    scoreAway: '',
    mainPlayer: '',
  });
  const [events, setEvents] = useState<DirectorEventRow[]>([]);
  const [conflicts, setConflicts] = useState<DirectorConflict[]>([]);
  const [correctingId, setCorrectingId] = useState('');
  const [busy, setBusy] = useState(false);

  const [, setClockTick] = useState(0);
  useEffect(() => {
    if (!clock.running) return undefined;
    const timer = setInterval(() => setClockTick((value) => value + 1), 1000);
    return () => clearInterval(timer);
  }, [clock.running]);

  const currentClockElapsed = useCallback(
    () => elapsedClockSeconds(clock, Date.now()),
    [clock],
  );

  const refreshTimeline = useCallback(async () => {
    const data = await loadEvents(matchId);
    setEvents(data.events || []);
    setConflicts(data.conflicts || []);
  }, [matchId]);

  const refreshAll = useCallback(async () => {
    const [configData, clockData] = await Promise.all([
      loadConfig(matchId),
      loadClock(matchId).catch(() => null),
    ]);
    // GET /config 的响应是 { config, snapshot } 两层。
    const config = ((configData as Record<string, unknown>).config ?? configData) as Record<string, unknown>;
    const snapshot = (configData.snapshot ?? {}) as { score?: { home: number; away: number }; period?: string };
    setHomeTeam(String(config.homeTeam ?? ''));
    setAwayTeam(String(config.awayTeam ?? ''));
    setRosters({
      home: { players: (config.homePlayers as RosterPlayer[]) ?? [], active: config.homeActive !== false },
      away: { players: (config.awayPlayers as RosterPlayer[]) ?? [], active: config.awayActive === true },
    });
    if (snapshot.score) setConfirmedScore({ home: Number(snapshot.score.home || 0), away: Number(snapshot.score.away || 0) });
    if (clockData?.clock) {
      setClock({
        period: clockData.clock.period || 'pre_match',
        elapsedSeconds: Number(clockData.clock.elapsedSeconds || 0),
        running: Boolean(clockData.clock.running),
        anchorAt: clockData.clock.anchorAt ?? null,
        version: Number(clockData.clock.version || 0),
      });
    }
    await refreshTimeline();
  }, [matchId, refreshTimeline]);

  useEffect(() => {
    if (!matchId) return;
    refreshAll().catch((err) => messageApi.error(`导演台加载失败：${err instanceof Error ? err.message : String(err)}`));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [matchId]);

  const applyDraftAction = useCallback((action: Parameters<typeof updateDraft>[1]) => {
    setDraft((current) => updateDraft(current, action));
  }, []);

  // —— 表单回填：只在草稿被整体替换（选行为/清空/拉回更正）时重置表单 ——
  // 不能挂 [draft] 逐次同步：用户正在编辑的描述/话术会被草稿里的旧值清掉
  // （老页面 renderCurrentDraft({ preserveForm: true }) 防的就是这个）。
  // 「何时回填」的 preserve 语义留在组件；「如何映射」在 draft-form 纯 module。
  const syncFormFromDraft = useCallback((source: Draft) => {
    setForm(formFromDraft(source));
  }, []);

  const applyPreset = useCallback(
    (eventType: string) => {
      let next = updateDraft(draft, {
        type: 'select_event',
        eventType,
        period: clock.period,
        elapsedSeconds: currentClockElapsed(),
        clockVersion: Number(clock.version || 0),
      });
      next = updateDraft(next, { type: 'set_field', field: 'source', value: 'operator' });
      if (eventType === 'score_correction') {
        next = updateDraft(next, { type: 'set_field', field: 'scoreBefore', value: { ...confirmedScore } });
        next = updateDraft(next, { type: 'set_field', field: 'scoreOverride', value: { ...confirmedScore } });
      }
      setDraft(next);
      syncFormFromDraft(next);
    },
    [applyDraftAction, clock.period, clock.version, confirmedScore, currentClockElapsed, draft, syncFormFromDraft],
  );

  const roster = side === 'away' ? rosters.away.players : rosters.home.players;
  const filteredRoster = useMemo(() => {
    const keyword = playerSearch.trim();
    if (!keyword) return roster;
    return roster.filter((player) => player.name.includes(keyword) || player.number === keyword);
  }, [playerSearch, roster]);

  // 球球处理切换即时写回草稿：人工话术输入框随 deliveryMode 渲染。
  const handleFormChange = (patch: Partial<DraftFormState>) => {
    setForm((current) => ({ ...current, ...patch }));
    if (patch.mode !== undefined) {
      applyDraftAction({ type: 'set_field', field: 'deliveryMode', value: patch.mode });
    }
  };

  const pickPlayer = (player: RosterPlayer) => {
    applyDraftAction({
      type: 'select_player',
      name: player.name,
      teamId: side,
      teamName: side === 'home' ? homeTeam : awayTeam,
    });
    setForm((current) => ({ ...current, mainPlayer: player.name }));
  };

  // 服务端时钟归一化（响应形状见 match-clock golden）。
  const applyClockState = useCallback((raw: {
    period?: string; elapsedSeconds?: number; running?: boolean; anchorAt?: string | null; version?: number;
  }) => {
    setClock({
      period: raw.period || 'pre_match',
      elapsedSeconds: Number(raw.elapsedSeconds || 0),
      running: Boolean(raw.running),
      anchorAt: raw.anchorAt ?? null,
      version: Number(raw.version || 0),
    });
  }, []);

  const clockAction = async (action: string, fields: Record<string, unknown> = {}) => {
    setBusy(true);
    try {
      const data = await patchClock(matchId, { action, expectedVersion: Number(clock.version || 0), ...fields });
      if (data.clock) applyClockState(data.clock);
    } catch (err) {
      // 409 版本冲突 = 另一端已校准时钟（双轨并存期的常态）：自动重读恢复，
      // 不让一次冲突变成连环失效（旧页语义，parity-checklist 22）。
      if (err instanceof ApiError && err.status === 409) {
        try {
          const fresh = await loadClock(matchId);
          if (fresh.clock) applyClockState(fresh.clock);
          messageApi.warning('时钟已被另一端校准，已自动同步');
        } catch (reloadErr) {
          messageApi.error(reloadErr instanceof Error ? reloadErr.message : String(reloadErr));
        }
        return;
      }
      messageApi.error(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const clearDraft = () => {
    const next = createDraft({
      matchId,
      occurredPeriod: clock.period,
      occurredSeconds: currentClockElapsed(),
      capturedClockVersion: Number(clock.version || 0),
    });
    setDraft(next);
    syncFormFromDraft(next);
    setCorrectingId('');
  };

  const submitDraft = async (factStatus: 'confirmed' | 'pending') => {
    // 表单→草稿合并（含 captureDraftFromForm 静默丢失防护）在纯 module。
    const working = mergedDraftForSubmit({ draft, form, factStatus, fallbackSide: side, homeTeam, awayTeam });
    setBusy(true);
    try {
      const payload = toEventPayload(working, { score: confirmedScore, homeTeam, awayTeam });
      const publishesRelatedFact = ['var_result', 'goal_cancelled'].includes(working.eventType || '');
      const result = correctingId && !publishesRelatedFact
        ? await correctEvent(matchId, correctingId, payload)
        : await publishEvent(matchId, payload);
      if (result.snapshot?.score) {
        setConfirmedScore({ home: Number(result.snapshot.score.home || 0), away: Number(result.snapshot.score.away || 0) });
      }
      messageApi.success(factStatus === 'pending' ? '已暂存候选' : '已确认并发送给球球');
      setCorrectingId('');
      clearDraft();
      await refreshTimeline();
    } catch (err) {
      messageApi.error(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const loadEventIntoDraft = (event: DirectorEventRow) => {
    setCorrectingId(event.id || '');
    // 事件行→草稿装载（__quiet__ 解码、provisional→pending）在纯 module。
    const loaded = draftFromEvent({ event, matchId, clock });
    setDraft(loaded);
    syncFormFromDraft(loaded);
    messageApi.info('事件已拉回草稿，修改后确认会以更正发布');
  };

  const transition = async (event: DirectorEventRow, action: string) => {
    if (!event.factId) return;
    setBusy(true);
    try {
      await factTransition(matchId, event.factId, action);
      messageApi.success(`事实已${action === 'confirm' ? '确认' : '撤销'}`);
      await refreshTimeline();
    } catch (err) {
      messageApi.error(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const resolve = async (conflict: DirectorConflict, chosenFactId: string) => {
    setBusy(true);
    try {
      // 老页面请求形状：selectedFactIds 数组（保留已采用 + 新选候选），
      // chosenFactId 为本次裁决的候选。
      const kept = (conflict.members || [])
        .filter((member) => member.role === 'accepted' && member.factId !== chosenFactId)
        .map((member) => member.factId);
      await resolveConflict(matchId, conflict.id, {
        selectedFactIds: [...kept, chosenFactId],
        reason: '导演裁决采用该事实',
      });
      messageApi.success('事实选择已生效');
      await refreshTimeline();
    } catch (err) {
      messageApi.error(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const rosterSideTeam = side === 'home' ? homeTeam : awayTeam;

  return (
    <div>
      {!matchId ? <Alert type="warning" showIcon message="缺少比赛 ID" style={{ marginBottom: 12 }} /> : null}

      {/* 比分 + 主时钟条 */}
      <Card data-testid="director-scorebar" style={{ border: '1px solid #253142', marginBottom: 12 }} styles={{ body: { padding: 12 } }}>
        <Row align="middle" gutter={12}>
          <Col span={7} style={{ textAlign: 'right' }}>
            <Text strong style={{ fontSize: 18 }}>{homeTeam || '主队'}</Text>
          </Col>
          <Col span={4} style={{ textAlign: 'center' }}>
            <Space size={4} align="baseline">
              <span style={{ fontSize: 26, fontWeight: 700 }}>{confirmedScore.home}</span>
              <Text type="secondary">-</Text>
              <span style={{ fontSize: 26, fontWeight: 700 }}>{confirmedScore.away}</span>
            </Space>
          </Col>
          <Col span={7}>
            <Text strong style={{ fontSize: 18 }}>{awayTeam || '客队'}</Text>
          </Col>
          <Col span={6} style={{ textAlign: 'right' }}>
            <Space wrap size={4}>
              <Tag data-testid="director-clock">{clock.running ? '▶' : '⏸'} {formatClock(currentClockElapsed())}</Tag>
              <Select
                size="small"
                aria-label="比赛阶段"
                value={clock.period || 'pre_match'}
                style={{ minWidth: 90 }}
                onChange={(value) => clockAction('set', { period: value })}
                options={['pre_match', 'first_half', 'halftime', 'second_half', 'fulltime'].map((value) => ({ value, label: value }))}
              />
              <Button size="small" disabled={busy} onClick={() => clockAction(clock.running ? 'pause' : 'start')}>
                {clock.running ? '暂停' : '开始'}
              </Button>
              <Button size="small" disabled={busy} onClick={() => clockAction('adjust', { deltaSeconds: -10 })}>
                -10秒
              </Button>
              <Button size="small" disabled={busy} onClick={() => clockAction('adjust', { deltaSeconds: 10 })}>
                +10秒
              </Button>
              <Tag>版本 {clock.version}</Tag>
            </Space>
          </Col>
        </Row>
      </Card>

      <Row gutter={[12, 12]}>
        <Col span={7}>
          <Space direction="vertical" style={{ width: '100%' }} size={12}>
            <Card title="球员选择" style={{ border: '1px solid #253142' }} styles={{ body: { padding: 12 } }}>
              <Space direction="vertical" style={{ width: '100%' }} size={8}>
                <Segmented
                  value={side}
                  onChange={(value) => setSide(value as 'home' | 'away')}
                  options={[
                    { value: 'home', label: `主队 ${homeTeam}` },
                    { value: 'away', label: `客队 ${awayTeam}` },
                  ]}
                />
                <Input
                  aria-label="搜索球员"
                  placeholder="搜索号码或姓名"
                  value={playerSearch}
                  onChange={(event) => setPlayerSearch(event.target.value)}
                  allowClear
                />
                <Space wrap size={4}>
                  {filteredRoster.map((player) => (
                    <Button
                      key={`${player.number}-${player.name}`}
                      size="small"
                      type={form.mainPlayer === player.name ? 'primary' : 'default'}
                      disabled={busy}
                      data-testid={`roster-${player.name}`}
                      onClick={() => pickPlayer(player)}
                    >
                      {player.number ? `${player.number} ` : ''}
                      {player.name}
                    </Button>
                  ))}
                  {!filteredRoster.length ? <Text type="secondary">名单为空：先在赛前配置保存阵容</Text> : null}
                </Space>
              </Space>
            </Card>
            <BehaviorBar onSelectEvent={applyPreset} disabled={busy} />
          </Space>
        </Col>

        <Col span={9}>
          <Space direction="vertical" style={{ width: '100%' }} size={12}>
            <DraftCard
              draft={draft}
              form={form}
              onFormChange={handleFormChange}
              homeTeam={homeTeam}
              awayTeam={awayTeam}
              confirmedScore={confirmedScore}
              correctingId={correctingId}
              busy={busy}
              onSubmit={submitDraft}
              onClear={clearDraft}
            />
            <VoiceDraft
              matchId={matchId}
              draft={draft}
              clock={{
                period: clock.period,
                elapsedSeconds: currentClockElapsed(),
                capturedClockVersion: Number(clock.version || 0),
              }}
              busy={busy}
              onDraftApplied={(next) => setDraft(next)}
              onPublished={(snapshot) => {
                if (snapshot?.score) {
                  setConfirmedScore({ home: Number(snapshot.score.home || 0), away: Number(snapshot.score.away || 0) });
                }
                clearDraft();
                void refreshTimeline();
              }}
            />
          </Space>
        </Col>

        <Col span={8}>
          <FactTimeline
            events={events}
            conflicts={conflicts}
            busy={busy}
            onCorrect={loadEventIntoDraft}
            onFactTransition={transition}
            onResolveConflict={resolve}
          />
          <Card style={{ border: '1px solid #253142', marginTop: 12 }} styles={{ body: { padding: 12 } }}>
            <Space wrap size={4}>
              <Tag>侧：{side === 'home' ? rosterSideTeam : awayTeam}</Tag>
              <Text type="secondary">matchId: {matchId}</Text>
              <Button size="small" onClick={() => void refreshAll()}>
                刷新比赛状态
              </Button>
            </Space>
          </Card>
        </Col>
      </Row>
    </div>
  );
}
