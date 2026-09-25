package tts

import (
	"testing"

	"qiuqiu/internal/relationship"
)

// bandStates 是每个情绪档的代表落点（与 relationship.updateAffect 的事件
// 动力学对齐：进球 / 进球余温 / VAR / 进球被吹 / 基线）。
var bandStates = map[affectBand]relationship.AffectState{
	bandExcited:  {Valence: 0.8, Arousal: 1, Tension: 0.2, Confidence: 0.9, Engagement: 0.8},
	bandPlayful:  {Valence: 0.7, Arousal: 0.5, Tension: 0.2, Confidence: 0.7, Engagement: 0.7},
	bandTense:    {Valence: 0, Arousal: 0.3, Tension: 0.7, Confidence: 0.1, Engagement: 0.6},
	bandDeflated: {Valence: -1, Arousal: 0.1, Tension: 0.1, Confidence: 0.9, Engagement: 0.4},
	bandCalm:     {Valence: 0, Arousal: 0.2, Tension: 0.1, Confidence: 0.6, Engagement: 0.4},
}

func TestClassifyAffectLandsOnCuratedBands(t *testing.T) {
	for band, state := range bandStates {
		if got := classifyAffect(state); got != band {
			t.Fatalf("classifyAffect(%+v) = %v, want %v", state, got, band)
		}
	}
	// 边界值本身落档（闭式下界）。
	boundaries := []struct {
		state relationship.AffectState
		band  affectBand
	}{
		{relationship.AffectState{Valence: 0.3, Arousal: 0.65}, bandExcited},
		{relationship.AffectState{Valence: 0.3, Arousal: 0.64}, bandPlayful},
		{relationship.AffectState{Tension: 0.55}, bandTense},
		{relationship.AffectState{Valence: -0.3}, bandDeflated},
		{relationship.AffectState{Valence: 0.25, Arousal: 0.35}, bandPlayful},
		{relationship.AffectState{Valence: 0.24, Arousal: 0.35}, bandCalm},
	}
	for _, boundary := range boundaries {
		if got := classifyAffect(boundary.state); got != boundary.band {
			t.Fatalf("classifyAffect(%+v) = %v, want %v", boundary.state, got, boundary.band)
		}
	}
}

// instructionGolden 是指令全表的快照：任何改表都必须自觉同步这里，防止
// 口吻与紧凑尾巴无意识漂移。
var instructionGolden = map[affectBand]map[relationship.CommunicationAct]string{
	bandExcited: {
		relationship.ActReact:       "这一句是即时的情绪爆发，兴奋到声线发亮，像刚看到进球一样喊出来，但收住不破音。",
		relationship.ActOpinion:     "这一句带着高涨的情绪下判断，语气笃定上扬，兴奋感从字缝里透出来。",
		relationship.ActRecall:      "这一句回味刚才的高光，兴奋还没退，像忍不住又提了一遍进球。",
		relationship.ActTease:       "这一句兴奋里带调侃，笑意压不住，打趣但点到为止。",
		relationship.ActRepair:      "这一句情绪仍高但态度诚恳，先接住对方，再把自己的话说清。",
		relationship.ActAsk:         "这一句兴奋中抛出问题，好奇又急切，像怕错过下一秒。",
		relationship.ActAnalyze:     "这一句在兴奋里保持条理，语速快但重点字咬清，不糊成一片。",
		relationship.ActChat:        "这一句兴奋地闲聊，像话痨朋友看球时搭话，轻快自然。",
		relationship.ActAcknowledge: "这一句用高涨的情绪应和对方，短促有力，像击掌一下。",
		relationship.ActDisagree:    "这一句情绪高涨地提出不同看法，直率但不呛人，像朋友间抬杠。",
	},
	bandPlayful: {
		relationship.ActReact:       "这一句轻快雀跃，像比分舒心时的随口一乐，嘴角带笑。",
		relationship.ActOpinion:     "这一句带着好心情亮观点，笃定里透着得意，不要太正经。",
		relationship.ActRecall:      "这一句温温地提起共同记忆，像翻到好笑的合照，笑意自然。",
		relationship.ActTease:       "这一句是熟人间的调侃，揶揄但亲昵，尾音可以上挑。",
		relationship.ActRepair:      "这一句放软语气修补关系，诚恳里带一点缓和的笑意。",
		relationship.ActAsk:         "这一句好奇地发问，语气轻快，像随口一问不强求答案。",
		relationship.ActAnalyze:     "这一句轻松地拆解局面，条理还在但不端着，像聊球不说教。",
		relationship.ActChat:        "这一句放松闲聊，像朋友看球间隙的碎碎念，自然随意。",
		relationship.ActAcknowledge: "这一句轻快地应和，简短带笑，像点着头嗯了一声。",
		relationship.ActDisagree:    "这一句笑着提出不同看法，软顶回去，不带火气。",
	},
	bandTense: {
		relationship.ActReact:       "这一句紧张而专注，声音略收紧，像悬念没落地前不敢大声。",
		relationship.ActOpinion:     "这一句压着紧张给判断，谨慎笃定，不把话说满。",
		relationship.ActRecall:      "这一句在紧张里短暂抽离去回忆，语气放轻，很快回到当下。",
		relationship.ActTease:       "这一句紧张里强行调侃，像用玩笑给自己松绑，笑得不太稳。",
		relationship.ActRepair:      "这一句收起紧张诚恳回应对方，语速放慢，把态度摆正。",
		relationship.ActAsk:         "这一句紧张地确认信息，急切但克制，像等结果时的小声追问。",
		relationship.ActAnalyze:     "这一句在紧张中保持冷静拆解，语速稍慢，重点字咬清。",
		relationship.ActChat:        "这一句紧张里勉强闲聊，心不在悬念上，回答简短。",
		relationship.ActAcknowledge: "这一句紧张地应和，短促轻声，像怕惊动什么。",
		relationship.ActDisagree:    "这一句压低声音提出异议，紧张但立场清楚。",
	},
	bandDeflated: {
		relationship.ActReact:       "这一句有失落感，像到手的进球被吹掉，声音略低但克制，不刻意哭腔。",
		relationship.ActOpinion:     "这一句压着失落给判断，语气平静偏冷，失望藏在字底下。",
		relationship.ActRecall:      "这一句回忆刚才的美好时带着怅然，像承认快乐短暂。",
		relationship.ActTease:       "这一句失落里强撑调侃，幽默但底色发苦，不硬凹。",
		relationship.ActRepair:      "这一句放下失落诚恳回应，把注意力还给对方，语气放暖。",
		relationship.ActAsk:         "这一句失落中低声求证，像还抱着最后一丝希望。",
		relationship.ActAnalyze:     "这一句收拾情绪冷静分析，语速放慢，失望不带走条理。",
		relationship.ActChat:        "这一句情绪低落地闲聊，话变少变短，但不冷淡对方。",
		relationship.ActAcknowledge: "这一句低落地应和，轻声短促，像叹了口气再点头。",
		relationship.ActDisagree:    "这一句失落里温和地表达不同看法，不争辩，像泄了气。",
	},
	bandCalm: {
		relationship.ActReact:       "",
		relationship.ActOpinion:     "这一句平静地亮出观点，语气平和笃定，不说教。",
		relationship.ActRecall:      "这一句温声提起共同的记忆，像老朋友对坐闲谈。",
		relationship.ActTease:       "这一句轻轻调侃，笑意浅浅，亲昵不越界。",
		relationship.ActRepair:      "这一句诚恳放慢，认真回应对方的情绪，像在修复关系。",
		relationship.ActAsk:         "这一句平静地提问，好奇但克制，给对方留足空间。",
		relationship.ActAnalyze:     "这一句条理清晰地分析，语速平稳，重点清楚，不念稿。",
		relationship.ActChat:        "这一句自然的日常闲聊，像熟人随口说话，不要播音腔。",
		relationship.ActAcknowledge: "这一句平静地应和，简短温和，让对方知道听到了。",
		relationship.ActDisagree:    "这一句平静地表达不同看法，对事不对人，语气松。",
	},
}

func TestInstructionForSnapshotLocksFullTable(t *testing.T) {
	acts := []relationship.CommunicationAct{
		relationship.ActReact, relationship.ActOpinion, relationship.ActRecall,
		relationship.ActTease, relationship.ActRepair, relationship.ActAsk,
		relationship.ActAnalyze, relationship.ActChat, relationship.ActAcknowledge,
		relationship.ActDisagree,
	}
	for band, state := range bandStates {
		for _, act := range acts {
			got := InstructionFor(state, act, 20)
			if want := instructionGolden[band][act]; got != want {
				t.Fatalf("InstructionFor(%v, %v) =\n%q\nwant\n%q", band, act, got, want)
			}
			// 纯函数：同输入同输出。
			if again := InstructionFor(state, act, 20); again != got {
				t.Fatalf("InstructionFor(%v, %v) is not deterministic", band, act)
			}
		}
	}
}

func TestInstructionForShortUtteranceAppendsCompactDirection(t *testing.T) {
	acts := []relationship.CommunicationAct{
		relationship.ActReact, relationship.ActOpinion, relationship.ActRecall,
		relationship.ActTease, relationship.ActRepair, relationship.ActAsk,
		relationship.ActAnalyze, relationship.ActChat, relationship.ActAcknowledge,
		relationship.ActDisagree,
	}
	for band, state := range bandStates {
		for _, act := range acts {
			got := InstructionFor(state, act, 8)
			if want := instructionGolden[band][act] + shortUtteranceDirection; got != want {
				t.Fatalf("short InstructionFor(%v, %v) =\n%q\nwant\n%q", band, act, got, want)
			}
		}
	}
	// 紧凑尾巴的原文单独锁死。
	want := instructionGolden[bandExcited][relationship.ActReact] +
		"反应一闪而过：口型短促紧凑，一口气带过，不拖长音、不加语气词。"
	if got := InstructionFor(bandStates[bandExcited], relationship.ActReact, 10); got != want {
		t.Fatalf("compact direction drifted: %q", got)
	}
	// 阈值边界：0 视为长度未知不加尾巴，阈值内加、阈值外加。
	state := bandStates[bandCalm]
	if got := InstructionFor(state, relationship.ActReact, 0); got != instructionGolden[bandCalm][relationship.ActReact] {
		t.Fatalf("utterLen 0 must skip the compact tail, got %q", got)
	}
	if got := InstructionFor(state, relationship.ActReact, compactUtteranceRunes); got == instructionGolden[bandCalm][relationship.ActReact] {
		t.Fatalf("utterLen at threshold must append the compact tail")
	}
	if got := InstructionFor(state, relationship.ActReact, compactUtteranceRunes+1); got != instructionGolden[bandCalm][relationship.ActReact] {
		t.Fatalf("utterLen above threshold must not append the compact tail, got %q", got)
	}
}

func TestInstructionForUnknownActFallsBackToReact(t *testing.T) {
	state := bandStates[bandTense]
	if got, want := InstructionFor(state, relationship.ActSilence, 20), instructionGolden[bandTense][relationship.ActReact]; got != want {
		t.Fatalf("unknown act fallback = %q, want react cell %q", got, want)
	}
	if got, want := InstructionFor(state, "no_such_act", 20), instructionGolden[bandTense][relationship.ActReact]; got != want {
		t.Fatalf("unknown act fallback = %q, want react cell %q", got, want)
	}
}
