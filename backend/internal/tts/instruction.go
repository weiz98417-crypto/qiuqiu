package tts

import (
	"qiuqiu/internal/relationship"
)

// 情绪→语音表演指令映射（deep module）：把回合既有的 Affect State 与沟通
// 动作折算成一条自然语言表演指令，经 VoiceOpts.Instruction 走供应商风格
// 通道。纯函数、零 LLM、零随机——同一输入永远得到同一指令，全表由快照
// 单测锁定；改表必须自觉过测试，防止口吻漂移。
//
// 情绪分档阈值与 relationship.updateAffect 的事件动力学对齐：进球落点进
// 激动档、VAR 落点进紧张档、进球被吹落点进遗憾档、基线落平静档，余温
// 衰减窗进轻快档。

// compactUtteranceRunes 是短反应阈值（runes）：微反应短语池 ≤10 字，
// 超过它一律按完整语句给指令。
const compactUtteranceRunes = 12

// shortUtteranceDirection 是短反应的紧凑尾巴：短句不需要铺陈，只需要
// 一闪而过的念法。
const shortUtteranceDirection = "反应一闪而过：口型短促紧凑，一口气带过，不拖长音、不加语气词。"

// shortReactionActs 是短反应类动作集合：紧凑尾巴只服务微反应式的短句
// （react/acknowledge/chat 类）。repair/opinion 等承载完整语义的动作即使
// 措辞很短也走全句——「一口气带过、不加语气词」会压掉诚恳修补与观点
// 表达的完整口吻，其余动作同理（尾巴按动作类施加，不按长度一刀切）。
var shortReactionActs = map[relationship.CommunicationAct]bool{
	relationship.ActReact:       true,
	relationship.ActChat:        true,
	relationship.ActAcknowledge: true,
}

// affectBand 是情绪档：AffectState 连续数值的确定性离散化。
type affectBand string

const (
	bandExcited  affectBand = "excited"
	bandPlayful  affectBand = "playful"
	bandTense    affectBand = "tense"
	bandDeflated affectBand = "deflated"
	bandCalm     affectBand = "calm"
)

// classifyAffect 按固定顺序分档：激动 → 紧张 → 遗憾 → 轻快 → 平静。
// 阈值只有这一处，调档即改这里（快照测试会拦住无意识的漂移）。
func classifyAffect(affect relationship.AffectState) affectBand {
	switch {
	case affect.Arousal >= 0.65 && affect.Valence >= 0.3:
		return bandExcited
	case affect.Tension >= 0.55:
		return bandTense
	case affect.Valence <= -0.3:
		return bandDeflated
	case affect.Valence >= 0.25 && affect.Arousal >= 0.35:
		return bandPlayful
	default:
		return bandCalm
	}
}

// instructionTable 是情绪档 × 沟通动作的指令模板表（策展于 repo）。
// 平静×react 显式为空串：中性场景不加情绪指令，交给基础人格与既有数值
// 档（style/energy/speed）方向，避免同义指令堆叠。
var instructionTable = map[affectBand]map[relationship.CommunicationAct]string{
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

// InstructionFor 把情绪状态 × 沟通动作 × 语句长度折算成表演指令。未知
// 动作回落 react 的中性姿态；平静×react 返回空串（调用方直接省略该段，
// 不占指令通道）；紧凑尾巴只对短反应类动作（react/acknowledge/chat 类）
// 的短句追加——微反应要一闪而过；repair/opinion 等完整语义动作走全句，
// 不因措辞短被压成急促念白。
func InstructionFor(affect relationship.AffectState, act relationship.CommunicationAct, utterLen int) string {
	band := classifyAffect(affect)
	cell, ok := instructionTable[band][act]
	if !ok {
		cell = instructionTable[band][relationship.ActReact]
	}
	if utterLen > 0 && utterLen <= compactUtteranceRunes && shortReactionActs[act] {
		return cell + shortUtteranceDirection
	}
	return cell
}
