workspace {
    name "QiuQiu Architecture"
    description "C4 facts for the AI football companion"

    model {
        viewer = person "球迷 / 观众" "观看比赛并进行语音对话"
        operator = person "运营 / 导播台" "校正和发布比赛事实"
        qiuqiu = softwareSystem "球球 AI 足球陪看系统" "实时语音、比赛上下文和 Live2D 陪看体验"
        match = softwareSystem "比赛数据供应商" "提供赛程、比分和比赛事件" "External"
        ai = softwareSystem "ASR / LLM / TTS 供应商" "提供语音识别、文本生成和语音合成" "External"

        viewer -> qiuqiu "看球、说话、接收回应"
        operator -> qiuqiu "校正或发布事实"
        qiuqiu -> match "获取比赛事实 / 事件"
        qiuqiu -> ai "调用语音与生成能力"

        qiuqiuContainers = container qiuqiu "Flutter Client" "Live2D、VAD、音频采集和播放" "Flutter"
        gateway = container qiuqiu "Go API + WebSocket" "会话、鉴权和实时通道" "Go"
        pipeline = container qiuqiu "Agent Runtime" "逻辑职责：Voice Session、Scheduler、Companion Agent、Director、Delivery；当前由 Go server 编排" "Go"
        redis = container qiuqiu "Redis" "短期会话、协调和冷却状态；非事实源" "Redis"
        postgres = container qiuqiu "PostgreSQL" "事实账本和审计记录" "PostgreSQL"

        qiuqiuContainers -> gateway "双向音频和事件" "WebSocket"
        gateway -> pipeline "转交语音、事件和会话上下文" "in-process"
        pipeline -> ai "识别、生成和合成" "HTTPS / WebSocket"
        gateway -> redis "会话缓存" "Redis protocol"
        pipeline -> postgres "事实、关系、Trace、Outbox" "SQL"
        match -> gateway "比赛事件进入统一接入（经 Source Manager / Fact Ledger）" "HTTP / polling"
        operator -> gateway "事实校正和发布" "HTTPS"
    }

    views {
        systemContext qiuqiu "01-context" {
            include viewer
            include operator
            include qiuqiu
            include match
            include ai
            autolayout lr
        }
        container qiuqiu "02-containers-mvp" {
            include *
            autolayout lr
        }
        systemLandscape "00-overview" {
            include viewer
            include operator
            include qiuqiu
            include match
            include ai
            autolayout lr
        }
        deployment qiuqiu "MVP" "06-deployment-mvp" {
            include *
            autolayout lr
        }
        deployment qiuqiu "Production-planned" "07-deployment-production" {
            include *
            autolayout lr
        }
    }
}
