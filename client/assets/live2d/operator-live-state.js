(function attachOperatorLiveState(global) {
  const eventDefinitions = {
    goal: definition('进球', '高频', 'celebrate', 5, [['scorer', '进球者'], ['assist', '助攻者'], ['pre_assist', '策动者']], ['scorer'], 1),
    shot: definition('射门', '高频', 'focus', 3, [['shooter', '射门者'], ['assist', '传球者'], ['blocker', '封堵者'], ['keeper', '门将']], ['shooter']),
    big_chance: definition('绝佳机会', '高频', 'tense', 5, [['attacker', '进攻者'], ['passer', '传球者'], ['defender', '防守者'], ['keeper', '门将']], ['attacker']),
    save: definition('扑救', '高频', 'surprise', 4, [['keeper', '扑救门将'], ['shooter', '射门者']], ['keeper']),
    miss: definition('错失', '高频', 'miss', 4, [['shooter', '错失者'], ['assist', '传球者'], ['keeper', '门将']], ['shooter']),
    foul: definition('犯规', '高频', 'complain', 3, [['offender', '犯规者'], ['fouled', '被犯规者']], ['offender']),
    yellow_card: definition('黄牌', '纪律', 'complain', 3, [['offender', '吃牌者'], ['fouled', '对抗对象']], ['offender']),
    red_card: definition('红牌', '纪律', 'angry', 5, [['offender', '红牌球员'], ['fouled', '对抗对象']], ['offender']),
    penalty: definition('点球', '纪律', 'tense', 5, [['taker', '主罚者'], ['won_by', '造点者'], ['offender', '犯规者'], ['keeper', '门将']], []),
    substitution: definition('换人', '人员 / 战术', 'analysis', 2, [['sub_on', '上场'], ['sub_off', '下场']], ['sub_on', 'sub_off']),
    tactical_shift: definition('战术变化', '人员 / 战术', 'analysis', 3, [['leader', '关键球员']], [], 0, true),
    pressure: definition('持续压迫', '人员 / 战术', 'focus', 4, [['attacker', '压迫发起']], [], 0, true),
    injury: definition('伤停', '人员 / 战术', 'comfort', 3, [['injured', '受伤球员'], ['challenger', '对抗球员']], ['injured']),
    var_check: definition('VAR检查', '特殊', 'tense', 4, [['subject', '被检查球员'], ['affected', '受影响球员']], []),
    var_result: definition('VAR 结果', '特殊', 'analysis', 4, [['subject', '被检查球员'], ['affected', '受影响球员']], []),
    goal_cancelled: definition('进球取消', '特殊', 'analysis', 5, [['scorer', '原进球者'], ['affected', '受影响球员']], [], -1),
    score_correction: definition('比分更正', '特殊', 'analysis', 2, [], [], 0, true),
    operator_note: definition('备注', '特殊', 'analysis', 2, [['player', '相关球员']], [], 0, true),
  };

  function definition(label, group, action, intensity, roles, requiredRoles, scoreDelta = 0, playerOptional = false) {
    return { label, group, action, intensity, roles, requiredRoles, scoreDelta, playerOptional };
  }

  function createDraft(overrides = {}) {
    return {
      matchId: '',
      source: 'operator',
      teamId: null,
      eventType: null,
      occurredPeriod: 'pre_match',
      occurredSeconds: 0,
      capturedClockVersion: 0,
      primaryParticipant: null,
      participants: [],
      description: '',
      intensity: 3,
      recommendedAction: '',
      factStatus: 'confirmed',
      deliveryMode: 'auto',
      proactiveText: '',
      revisionOf: null,
      correctionReason: '',
      scoreOverride: null,
      scoreBefore: null,
      transcript: '',
      inferredFields: [],
      fieldConfidence: {},
      ...overrides,
    };
  }

  function updateDraft(draft, action) {
    const next = cloneDraft(draft);
    switch (action.type) {
      case 'select_team': {
        const teamId = action.teamId || null;
        if (teamId !== next.teamId) {
          next.teamId = teamId;
          next.primaryParticipant = null;
          next.participants = [];
        }
        return next;
      }
      case 'select_player': {
        if (action.teamId && action.teamId !== next.teamId) {
          next.teamId = action.teamId;
          next.participants = [];
        }
        next.primaryParticipant = action.name ? participant('', action.name, next.teamId, action.teamName, true) : null;
        const role = eventDefinitions[next.eventType]?.roles?.[0]?.[0];
        if (role && action.name) setParticipant(next, role, action.name, next.teamId, action.teamName, true);
        return next;
      }
      case 'select_event': {
        const eventType = action.eventType || null;
        const definition = eventDefinitions[eventType];
        next.eventType = definition ? eventType : null;
        next.participants = [];
        next.description = '';
        next.proactiveText = '';
        next.scoreOverride = null;
        next.scoreBefore = null;
        next.recommendedAction = definition?.action || '';
        next.intensity = definition?.intensity || 3;
        next.factStatus = eventType === 'var_check' ? 'provisional' : 'confirmed';
        if (eventType === 'score_correction') {
          next.teamId = null;
          next.primaryParticipant = null;
          next.participants = [];
          next.deliveryMode = 'quiet';
        }
        next.inferredFields = [];
        next.fieldConfidence = {};
        if (action.period) next.occurredPeriod = action.period;
        if (Number.isFinite(action.elapsedSeconds)) next.occurredSeconds = Math.max(0, action.elapsedSeconds);
        if (Number.isFinite(action.clockVersion)) next.capturedClockVersion = action.clockVersion;
        const role = definition?.roles?.[0]?.[0];
        if (role && next.primaryParticipant?.name) {
          setParticipant(next, role, next.primaryParticipant.name, next.teamId, next.primaryParticipant.teamName, true);
        }
        return next;
      }
      case 'set_participant':
        setParticipant(next, action.role, action.name, action.teamId || next.teamId, action.teamName, action.resolved !== false);
        if (eventDefinitions[next.eventType]?.roles?.[0]?.[0] === action.role) {
          next.primaryParticipant = action.name ? participant(action.role, action.name, action.teamId || next.teamId, action.teamName, action.resolved !== false) : null;
        }
        return next;
      case 'set_field':
        if (Object.prototype.hasOwnProperty.call(next, action.field)) next[action.field] = action.value;
        return next;
      case 'sync_clock':
        if (hasDraftContent(next)) return next;
        if (action.period) next.occurredPeriod = action.period;
        if (Number.isFinite(action.elapsedSeconds)) next.occurredSeconds = Math.max(0, action.elapsedSeconds);
        if (Number.isFinite(action.clockVersion)) next.capturedClockVersion = action.clockVersion;
        return next;
      case 'apply_voice':
        return applyVoiceDraft(next, action.draft || {}).draft;
      case 'clear':
        return createDraft({
          matchId: next.matchId,
          occurredPeriod: action.period || next.occurredPeriod,
          occurredSeconds: Number.isFinite(action.elapsedSeconds) ? action.elapsedSeconds : next.occurredSeconds,
          capturedClockVersion: Number.isFinite(action.clockVersion) ? action.clockVersion : next.capturedClockVersion,
          deliveryMode: next.deliveryMode,
        });
      default:
        return next;
    }
  }

  function applyVoiceDraft(draft, incoming) {
    const next = cloneDraft(draft);
    const conflicts = [];
    const scalarFields = ['teamId', 'eventType', 'occurredPeriod', 'occurredSeconds', 'capturedClockVersion', 'description', 'transcript'];
    scalarFields.forEach((field) => mergeField(next, incoming, field, conflicts));
    if (incoming.teamName && !next.teamName) next.teamName = incoming.teamName;
    if (next.eventType && !draft.eventType) {
      const definition = eventDefinitions[next.eventType];
      next.recommendedAction = definition?.action || next.recommendedAction;
      next.intensity = definition?.intensity || next.intensity;
    }
    (incoming.participants || []).forEach((item) => {
      const current = next.participants.find((existing) => existing.role === item.role);
      if (!current) {
        setParticipant(next, item.role, item.name, item.teamId || next.teamId, item.teamName, item.resolved !== false);
      } else if (current.name !== item.name) {
        conflicts.push({ field: `participants.${item.role}`, current: current.name, incoming: item.name, participant: { ...item } });
      }
    });
    next.inferredFields = unique([...(next.inferredFields || []), ...(incoming.inferredFields || [])]);
    next.fieldConfidence = { ...(next.fieldConfidence || {}), ...(incoming.fieldConfidence || {}) };
    const firstRole = eventDefinitions[next.eventType]?.roles?.[0]?.[0];
    const primary = next.participants.find((item) => item.role === firstRole);
    if (primary) next.primaryParticipant = { ...primary };
    return { draft: next, conflicts };
  }

  function applyVoiceConflict(draft, conflict) {
    const next = cloneDraft(draft);
    if (!conflict?.field) return next;
    if (conflict.field.startsWith('participants.') && conflict.participant) {
      const item = conflict.participant;
      setParticipant(next, item.role, item.name, item.teamId || next.teamId, item.teamName, item.resolved !== false);
      const firstRole = eventDefinitions[next.eventType]?.roles?.[0]?.[0];
      if (item.role === firstRole) next.primaryParticipant = { ...item };
    } else if (Object.prototype.hasOwnProperty.call(next, conflict.field)) {
      next[conflict.field] = conflict.incoming;
      if (conflict.field === 'teamId' && conflict.teamName) next.teamName = conflict.teamName;
      if (conflict.field === 'eventType') {
        const definition = eventDefinitions[next.eventType];
        const allowedRoles = new Set((definition?.roles || []).map(([role]) => role));
        next.participants = next.participants.filter((item) => allowedRoles.has(item.role));
        const primaryRole = definition?.roles?.[0]?.[0];
        next.primaryParticipant = next.participants.find((item) => item.role === primaryRole) || null;
        next.recommendedAction = definition?.action || next.recommendedAction;
        next.intensity = definition?.intensity || next.intensity;
      }
    }
    next.inferredFields = unique([...(next.inferredFields || []), conflict.field.split('.')[0]]);
    return next;
  }

  function validateDraft(draft) {
    const errors = [];
    const definition = eventDefinitions[draft.eventType];
    if (!definition) errors.push('请选择比赛行为');
    if (definition && !draft.teamId && !definition.playerOptional) errors.push('请选择球队');
    if (definition) {
      definition.requiredRoles.forEach((role) => {
        const item = draft.participants.find((participant) => participant.role === role && participant.name);
        if (!item) errors.push(`请填写${roleLabel(draft.eventType, role)}`);
        else if (item.resolved === false) errors.push(`请确认球员：${item.name}`);
      });
    }
    if (draft.eventType === 'substitution') {
      const teams = unique(draft.participants.filter((item) => item.name).map((item) => item.teamId).filter(Boolean));
      if (teams.length > 1) errors.push('换上与换下球员必须属于同一球队');
    }
    if (draft.eventType === 'goal') {
      const wrongTeam = draft.participants.find((item) => ['scorer', 'assist', 'pre_assist'].includes(item.role) && item.teamId && item.teamId !== draft.teamId);
      if (wrongTeam) errors.push(`${roleLabel(draft.eventType, wrongTeam.role)}必须属于进球队伍`);
    }
    if (draft.eventType === 'goal_cancelled' && !draft.revisionOf) errors.push('请选择要取消的原进球');
    if (draft.eventType === 'var_result' && !draft.revisionOf) errors.push('请选择 VAR 正在审查的原事实');
    if (!String(draft.description || '').trim()) errors.push('请填写事件描述');
    if ((draft.revisionOf || draft.eventType === 'score_correction') && !String(draft.correctionReason || '').trim()) errors.push('请填写更正或采用原因');
    if (draft.eventType === 'score_correction') {
      const score = draft.scoreOverride;
      if (!score || !Number.isInteger(Number(score.home)) || !Number.isInteger(Number(score.away)) || Number(score.home) < 0 || Number(score.away) < 0) {
        errors.push('请填写有效的目标比分');
      } else if (draft.scoreBefore && Number(score.home) === Number(draft.scoreBefore.home) && Number(score.away) === Number(draft.scoreBefore.away)) {
        errors.push('目标比分必须与当前公开比分不同');
      }
    }
    if (draft.deliveryMode === 'manual' && !String(draft.proactiveText || '').trim()) errors.push('请填写球球主动话术');
    return { ready: errors.length === 0, errors };
  }

  function toEventPayload(draft, context) {
    const validation = validateDraft(draft);
    if (!validation.ready) throw new Error(validation.errors[0]);
    const definition = eventDefinitions[draft.eventType];
    const score = {
      home: Number(context.score?.home || 0),
      away: Number(context.score?.away || 0),
    };
    if (draft.eventType === 'score_correction') {
      score.home = Number(draft.scoreOverride.home);
      score.away = Number(draft.scoreOverride.away);
    }
    if (definition.scoreDelta && draft.teamId) score[draft.teamId] += definition.scoreDelta;
    const primaryRole = definition.roles?.[0]?.[0];
    const primary = draft.participants.find((item) => item.role === primaryRole) || draft.primaryParticipant;
    return {
      source: draft.source || 'operator',
      providerName: draft.source === 'operator_voice' ? 'director-voice' : 'director-console',
      eventType: draft.eventType,
      period: draft.occurredPeriod,
      clock: formatClock(draft.occurredSeconds),
      teamId: draft.teamId || '',
      teamName: draft.teamId === 'away' ? context.awayTeam : draft.teamId === 'home' ? context.homeTeam : '',
      playerName: primary?.name || '',
      participants: draft.participants.filter((item) => item.name).map(({ role, name, teamId, teamName }) => ({ role, name, teamId, teamName })),
      score,
      intensity: Number(draft.intensity || definition.intensity || 3),
      confirmed: draft.factStatus === 'confirmed',
      factStatus: draft.factStatus,
      description: String(draft.description || '').trim(),
      recommendedAction: draft.recommendedAction || definition.action,
      tags: [`clockVersion=${draft.capturedClockVersion}`, `input=${draft.source || 'operator'}`],
      proactiveText: draft.deliveryMode === 'quiet' ? '__quiet__' : draft.deliveryMode === 'manual' ? String(draft.proactiveText || '').trim() : '',
      revisionOf: draft.revisionOf || '',
      evidence: draft.revisionOf || draft.eventType === 'score_correction' ? { correctionReason: String(draft.correctionReason || '').trim() } : {},
    };
  }

  function setParticipant(draft, role, name, teamId, teamName, resolved) {
    if (!role) return;
    draft.participants = draft.participants.filter((item) => item.role !== role);
    if (name) draft.participants.push(participant(role, name, teamId, teamName, resolved));
  }

  function participant(role, name, teamId, teamName, resolved) {
    return { role, name, teamId: teamId || '', teamName: teamName || '', resolved: resolved !== false };
  }

  function mergeField(target, source, field, conflicts) {
    const incoming = source[field];
    if (incoming === undefined || incoming === null || incoming === '') return;
    const current = target[field];
    if (current === undefined || current === null || current === '' || (typeof current === 'number' && current === 0)) {
      target[field] = incoming;
    } else if (current !== incoming) {
      conflicts.push({ field, current, incoming, teamName: field === 'teamId' ? source.teamName : '' });
    }
  }

  function hasDraftContent(draft) {
    return Boolean(
      draft.eventType ||
      draft.primaryParticipant?.name ||
      (draft.participants || []).some((item) => item.name) ||
      String(draft.description || '').trim() ||
      String(draft.proactiveText || '').trim() ||
      String(draft.transcript || '').trim() ||
      draft.revisionOf ||
      String(draft.correctionReason || '').trim() ||
      draft.scoreOverride
    );
  }

  function roleLabel(eventType, role) {
    return eventDefinitions[eventType]?.roles?.find(([key]) => key === role)?.[1] || role;
  }

  function formatClock(seconds) {
    const safe = Math.max(0, Number(seconds || 0));
    return `${String(Math.floor(safe / 60)).padStart(2, '0')}:${String(Math.floor(safe % 60)).padStart(2, '0')}`;
  }

  function cloneDraft(draft) {
    return createDraft({
      ...draft,
      participants: (draft.participants || []).map((item) => ({ ...item })),
      primaryParticipant: draft.primaryParticipant ? { ...draft.primaryParticipant } : null,
      scoreOverride: draft.scoreOverride ? { ...draft.scoreOverride } : null,
      scoreBefore: draft.scoreBefore ? { ...draft.scoreBefore } : null,
      inferredFields: [...(draft.inferredFields || [])],
      fieldConfidence: { ...(draft.fieldConfidence || {}) },
    });
  }

  function unique(values) {
    return [...new Set(values)];
  }

  global.OperatorLiveState = {
    eventDefinitions,
    createDraft,
    updateDraft,
    applyVoiceDraft,
    applyVoiceConflict,
    validateDraft,
    toEventPayload,
    roleLabel,
    formatClock,
    hasDraftContent,
  };
})(globalThis);
