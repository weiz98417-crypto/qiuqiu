package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/auth"
	"qiuqiu/internal/ambient"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/datasource"
	"qiuqiu/internal/directordraft"
	"qiuqiu/internal/embedding"
	"qiuqiu/internal/knowledge"
	"qiuqiu/internal/proactive"
	"qiuqiu/internal/structured"
	"qiuqiu/internal/interaction"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/observation"
	"qiuqiu/internal/operatorauth"
	"qiuqiu/internal/operatorwrite"
	"qiuqiu/internal/privacy"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/router"
	"qiuqiu/internal/tts"
	"qiuqiu/internal/ws"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

type speechRecognizer interface {
	Transcribe(ctx context.Context, audio []byte, hints []string) (*asr.Result, error)
}

// speechSynthesizer 收敛为 tts.Synthesizer seam 的别名（ADR-0012 修订）：
// 供应商知识与表现选项都走 seam，本包不再自定义合成接口。
type speechSynthesizer = tts.Synthesizer

type todayFixturesClient interface {
	GetTodayFixturesContext(context.Context) ([]datasource.Fixture, error)
}

type scheduleFixturesClient interface {
	todayFixturesClient
	GetFixturesContext(context.Context, time.Time, time.Time, string) ([]datasource.Fixture, error)
}

type apiSportsScheduleReader struct {
	client todayFixturesClient
}

func (reader apiSportsScheduleReader) TodayFixtures(ctx context.Context) ([]companion.ScheduleMatch, error) {
	fixtures, err := reader.client.GetTodayFixturesContext(ctx)
	if err != nil {
		return nil, err
	}
	matches := make([]companion.ScheduleMatch, 0, len(fixtures))
	for _, fixture := range fixtures {
		matches = append(matches, companion.ScheduleMatch{
			HomeTeam: fixture.HomeTeam,
			AwayTeam: fixture.AwayTeam,
			Status:   fixture.Status,
		})
	}
	return matches, nil
}

func (reader apiSportsScheduleReader) Search(ctx context.Context, request companion.ScheduleSearchRequest) (companion.ScheduleSearchResult, error) {
	client, ok := reader.client.(scheduleFixturesClient)
	if !ok {
		return companion.ScheduleSearchResult{}, fmt.Errorf("schedule date-range search is unavailable")
	}
	fixtures, err := client.GetFixturesContext(ctx, request.From, request.To, request.Timezone)
	if err != nil {
		return companion.ScheduleSearchResult{}, err
	}
	result := companion.ScheduleSearchResult{
		Fixtures:  make([]companion.ScheduleMatch, 0, len(fixtures)),
		Source:    "api-sports",
		FetchedAt: time.Now().UTC(),
		Freshness: "fresh",
	}
	for _, fixture := range fixtures {
		if competition := strings.TrimSpace(request.Competition); competition != "" &&
			!strings.Contains(strings.ToLower(fixture.Competition), strings.ToLower(competition)) {
			continue
		}
		var homeScore, awayScore *int
		if fixture.HomeGoalKnown && fixture.AwayGoalKnown {
			home, away := fixture.HomeGoal, fixture.AwayGoal
			homeScore, awayScore = &home, &away
		}
		result.Fixtures = append(result.Fixtures, companion.ScheduleMatch{
			FixtureID:   strconv.Itoa(fixture.ID),
			HomeTeam:    fixture.HomeTeam,
			AwayTeam:    fixture.AwayTeam,
			Competition: fixture.Competition,
			KickoffAt:   fixture.KickoffAt,
			Status:      fixture.Status,
			HomeScore:   homeScore,
			AwayScore:   awayScore,
			Source:      fixture.Source,
			Freshness:   fixture.Freshness,
		})
	}
	return result, nil
}

type voiceSessionResult struct {
	Text           string
	Reply          string
	Trace          companion.Trace
	Presentation   relationship.PresentationPlan
	ScheduleLookup *companion.ScheduleLookup
	AudioData      []byte
	AudioMIME      string
	ASRError       string
	TTSError       string
}

func qiuqiuReplyData(text, traceID, source, eventID, deliveryKey string, presentation relationship.PresentationPlan) map[string]interface{} {
	data := map[string]interface{}{
		"text":    text,
		"traceId": traceID,
		"source":  source,
	}
	if eventID != "" {
		data["eventId"] = eventID
	}
	if deliveryKey != "" {
		data["deliveryKey"] = deliveryKey
	}
	if presentation.Expression != "" || presentation.Motion != "" || presentation.VoiceStyle != "" {
		data["presentation"] = presentation
	}
	return data
}

type demoStateResetter struct {
	traces        companion.DemoResetter
	relationships relationship.MatchResetter
	observations  observation.MatchResetter
}

func (resetter demoStateResetter) Reset(matchID string) error {
	if resetter.traces != nil {
		if err := resetter.traces.Reset(matchID); err != nil {
			return err
		}
	}
	if resetter.relationships != nil {
		if err := resetter.relationships.ResetMatch(matchID); err != nil {
			return err
		}
	}
	if resetter.observations != nil {
		return resetter.observations.ResetMatch(context.Background(), matchID)
	}
	return nil
}

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	privacy.SetRetentionDays(cfg.PrivacyRetentionDays)

	// AI clients
	llmClient := newTextLLMClient(cfg)
	ttsClient := configuredSpeechSynthesizer(cfg)
	asrClient := asr.NewClient(cfg.MiMoAPIKey).WithBaseURL(cfg.MiMoBaseURL).WithModel("mimo-v2.5-asr")
	directorDrafts := directordraft.NewService(asrClient, directordraft.NewLLMExtractor(structured.NewClient(cfg.MiMoBaseURL, cfg.MiMoAPIKey, cfg.MiMoModel)))
	// 用户轮次信号去重器（server-residual-polish 1.3：原包级 global，改为
	// main() 构造后经 watchDeps 显式注入）。
	submittedUserSignals := newSignalDeduper(userSignalDedupeTTL, userSignalDedupeMaxEntries)

	var sessionStore auth.Store = auth.NewMemoryStore()
	var sessionStoreCloser func()
	if cfg.DatabaseURL != "" {
		postgresSessionStore, err := auth.OpenPostgresStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres session store: %v", err)
		}
		sessionStore = postgresSessionStore
		sessionStoreCloser = postgresSessionStore.Close
	}
	sessionManager, err := auth.NewManager(sessionStore, cfg.SessionSigningKey)
	if err != nil {
		log.Fatalf("session manager: %v", err)
	}
	if sessionStoreCloser != nil {
		defer sessionStoreCloser()
	}
	hub := ws.NewHub(cfg).WithSessionAuthenticator(sessionManager)
	storeOptions := []matchstate.StoreOption{matchstate.WithFactLedgerPublicReads(cfg.FactLedgerPublicReads)}
	var matchStore matchstate.Repository = matchstate.NewStore(storeOptions...)
	if cfg.DatabaseURL != "" {
		postgresStore, err := matchstate.OpenPostgresStore(context.Background(), cfg.DatabaseURL, "migrations", storeOptions...)
		if err != nil {
			log.Fatalf("postgres match store: %v", err)
		}
		defer postgresStore.Close()
		matchStore = postgresStore
		log.Printf("match store: postgresql")
	} else {
		log.Printf("match store: memory")
	}
	if registrar, ok := matchStore.(matchstate.FactProjectionAuditRegistrar); ok {
		var projectionAuditMu sync.Mutex
		projectionAuditSignatures := make(map[string]string)
		registrar.SetFactProjectionAuditObserver(func(audit matchstate.FactProjectionAudit) {
			differences, _ := json.Marshal(audit.Differences)
			differenceHash := sha256.Sum256(differences)
			signature := fmt.Sprintf(
				"%s|%d-%d|%d-%d|%s|%x",
				strings.Join(audit.Mismatches, ","),
				audit.LegacyScore.Home, audit.LegacyScore.Away,
				audit.ProjectedScore.Home, audit.ProjectedScore.Away,
				audit.Error,
				differenceHash,
			)
			projectionAuditMu.Lock()
			if projectionAuditSignatures[audit.MatchID] == signature {
				projectionAuditMu.Unlock()
				return
			}
			projectionAuditSignatures[audit.MatchID] = signature
			projectionAuditMu.Unlock()
			log.Printf(
				"fact projection shadow mismatch match=%q fields=%v legacy=%d-%d projected=%d-%d difference_hash=%x error=%q",
				audit.MatchID, audit.Mismatches,
				audit.LegacyScore.Home, audit.LegacyScore.Away,
				audit.ProjectedScore.Home, audit.ProjectedScore.Away,
				differenceHash,
				audit.Error,
			)
		})
	}
	outboxCtx, outboxCancel := context.WithCancel(context.Background())
	defer outboxCancel()
	outboxRunner, _ := matchStore.(matchstate.OutboxRunner)
	var privacyStore privacy.Store = privacy.NewMemoryStore()
	var privacyStoreCloser func()
	if cfg.DatabaseURL != "" {
		postgresPrivacyStore, err := privacy.OpenPostgresStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres privacy store: %v", err)
		}
		privacyStore = postgresPrivacyStore
		privacyStoreCloser = postgresPrivacyStore.Close
	}
	if privacyStoreCloser != nil {
		defer privacyStoreCloser()
	}
	privacyService := privacy.NewService(privacyStore)
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	go runPrivacyCleanup(cleanupCtx, privacyService)
	operatorWrites := operatorwrite.NewMemoryService()
	if cfg.DatabaseURL != "" {
		postgresOperatorWrites, err := operatorwrite.OpenPostgresService(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres operator idempotency store: %v", err)
		}
		operatorWrites = postgresOperatorWrites
	}
	defer operatorWrites.Close()
	// ADR-0008 operator identity: personal tokens live in the operators table
	// (migration 042) with an in-process store when no database exists.
	// Bootstrap seeds the first director from QIUQIU_BOOTSTRAP_OPERATOR
	// ("name:token") when the table is empty; revocation = row deletion.
	var operatorStore operatorauth.Directory = operatorauth.NewMemoryStore()
	if cfg.DatabaseURL != "" {
		postgresOperators, err := operatorauth.OpenPostgresStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres operator store: %v", err)
		}
		defer postgresOperators.Close()
		operatorStore = postgresOperators
	}
	if err := operatorauth.Bootstrap(context.Background(), operatorStore, os.Getenv("QIUQIU_BOOTSTRAP_OPERATOR"), log.Printf); err != nil {
		log.Printf("operator bootstrap skipped: %v", err)
	}
	authz := newOperatorAuthz(cfg, operatorStore).withJWTSecret(cfg.JWTSecret)
	// ADR-0010: the JWT secret is required once any password account exists.
	// Startup only warns (legacy token-only deployments are valid); the login
	// route enforces it per request.
	if passwordChecker, ok := operatorStore.(operatorauth.PasswordAccounts); ok {
		if passwordChecker.HasPasswordAccounts(context.Background()) && strings.TrimSpace(cfg.JWTSecret) == "" {
			log.Printf("WARNING: operator password accounts exist but QIUQIU_JWT_SECRET is not set — console login will refuse until it is configured")
		}
	}
	var sportsClient datasource.EventsClient
	if cfg.APISportsAPIKey != "" {
		sportsClient = datasource.NewClient(cfg.APISportsAPIKey).WithBaseURL(cfg.APISportsBaseURL)
	}
	sourceManager := datasource.NewManager(context.Background(), matchStore, sportsClient, datasource.ManagerConfig{})
	defer sourceManager.Close()
	companionTools := companion.NewRepositoryMemoryTools(matchStore)
	var traceReader companion.TraceReader = companionTools
	var demoResetter companion.DemoResetter = companionTools
	if cfg.DatabaseURL != "" {
		traceWriter, err := companion.OpenPostgresTraceWriter(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres trace writer: %v", err)
		}
		defer traceWriter.Close()
		traceReader = traceWriter
		companionTools.WithTurnReader(traceWriter)
		demoResetter = traceWriter
		companionTools.WithTraceWriter(traceWriter)
	}
	var observationCoordinator observation.Coordinator
	if cfg.PendingObservationCoordination {
		observationCoordinator = observation.NewMemoryCoordinator()
		if cfg.DatabaseURL != "" {
			postgresObservationCoordinator, err := observation.OpenPostgresCoordinator(context.Background(), cfg.DatabaseURL)
			if err != nil {
				log.Fatalf("postgres observation coordinator: %v", err)
			}
			defer postgresObservationCoordinator.Close()
			observationCoordinator = postgresObservationCoordinator
		}
		go runObservationExpiry(cleanupCtx, observationCoordinator)
	}
	// 气氛旁路（ambient-audio-observation）：AMBIENT_AED_URL 留空即停用；
	// 事件只作 observation store 的 kind=ambient 旁证（最低权重档），永不作
	// Match Fact（ADR-0002）。sidecar 不可达时旁路静默失败，主链路无感。
	var ambientSidecar *ambientRelay
	if strings.TrimSpace(cfg.AmbientAEDURL) != "" {
		ambientSidecar = newAmbientRelay(
			ambient.NewClient(cfg.AmbientAEDURL).WithTimeout(cfg.AmbientAEDTimeout()),
			observationCoordinator,
		)
	}
	// 意图注册表漂移断言（openspec/changes/intent-registry）：启动即校验
	// 注册表镜像与 router 硬编码事实一致，不一致 fail-fast。
	if err := companion.ValidateIntentRegistry(); err != nil {
		log.Fatalf("intent registry validation: %v", err)
	}
	// 提醒簿（openspec/changes/proactive-scheduler，ADR-0015）：有库走
	// Postgres（migration 045），无库走内存实现（测试/裸跑降级）。
	var reminderStore proactive.Store = proactive.NewMemoryStore()
	if cfg.DatabaseURL != "" {
		postgresReminders, err := proactive.OpenPostgresStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres reminder store: %v", err)
		}
		defer postgresReminders.Close()
		reminderStore = postgresReminders
	}
	// 语义记忆嵌入端点（第二波）：留空 = 停用向量路与知识向量检索。
	var knowledgeEmbedder knowledge.Embedder
	if cfg.EmbeddingBaseURL != "" {
		knowledgeEmbedder = embedding.NewClient(cfg.EmbeddingBaseURL, cfg.EmbeddingModel, "ollama")
	}
	var knowledgeLibrary *knowledge.Library
	if cfg.KnowledgeDir != "" {
		library, err := knowledge.Load(cfg.KnowledgeDir, knowledgeEmbedder)
		if err != nil {
			log.Fatalf("knowledge library: %v", err)
		}
		knowledgeLibrary = library
	}
	// 订阅簿（openspec/changes/season-subscription）：有库走 Postgres
	//（migration 047），无库走内存实现。
	var subscriptionStore proactive.SubscriptionStore = proactive.NewMemorySubscriptionStore()
	if cfg.DatabaseURL != "" {
		postgresSubs, err := proactive.OpenPostgresSubscriptionStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres subscription store: %v", err)
		}
		defer postgresSubs.Close()
		subscriptionStore = postgresSubs
	}
	// 人格互动规范（openspec/changes/character-settings）：三入口一状态。
	var characterSettings *relationship.CharacterSettings
	if cfg.DatabaseURL != "" {
		postgresSettings, err := relationship.OpenPostgresCharacterSettingStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres character settings: %v", err)
		}
		defer postgresSettings.Close()
		characterSettings, err = relationship.NewCharacterSettings(postgresSettings)
		if err != nil {
			log.Fatalf("character settings: %v", err)
		}
	} else {
		characterSettings, _ = relationship.NewCharacterSettings(relationship.NewMemoryCharacterSettingStore())
	}
	companionAgent := companion.NewAgent(companionTools).WithReminders(reminderStore).WithSubscriptions(subscriptionStore).WithKnowledge(knowledgeLibrary)
	interactionLedger := interaction.Ledger(interaction.NewMemoryLedger())
	var interactionLedgerCloser func()
	if cfg.DatabaseURL != "" {
		postgresInteractionLedger, err := interaction.OpenPostgresLedger(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres interaction ledger: %v", err)
		}
		interactionLedger = postgresInteractionLedger
		interactionLedgerCloser = postgresInteractionLedger.Close
	}
	if interactionLedgerCloser != nil {
		defer interactionLedgerCloser()
	}
	companionAgent.WithInteractionLedger(interactionLedger)
	var scheduleReaderSource companion.ScheduleReader
	if reader, ok := sportsClient.(scheduleFixturesClient); ok {
		scheduleReaderSource = apiSportsScheduleReader{client: reader}
		companionAgent.WithScheduleReader(scheduleReaderSource)
	} else if reader, ok := sportsClient.(todayFixturesClient); ok {
		scheduleReaderSource = apiSportsScheduleReader{client: reader}
		companionAgent.WithScheduleReader(scheduleReaderSource)
	}
	// 订阅展开节拍（Q17）：每日合并扫描未来 14 天，新赛程增量补提醒。
	go runSubscriptionExpansion(outboxCtx, subscriptionStore, reminderStore, scheduleReaderSource)
	if observationCoordinator != nil {
		companionAgent.WithObservationCoordinator(observationCoordinator)
		companionAgent.WithObservationReconcileWindow(func(matchID, eventType string) time.Duration {
			return sourceManager.ObservationReconcileWindow(matchID, observation.DefaultReconcileWindow(eventType))
		})
	}
	var relationshipRepository relationship.StateRepository = relationship.NewMemoryRepository()
	if cfg.DatabaseURL != "" {
		postgresRelationshipRepository, err := relationship.OpenPostgresRepository(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("relationship repository: %v", err)
		}
		defer postgresRelationshipRepository.Close()
		relationshipRepository = postgresRelationshipRepository
	}
	companionAgent.WithDirector(relationship.NewDirector(relationshipRepository))
	relationshipResetter, _ := relationshipRepository.(relationship.MatchResetter)
	observationResetter, _ := observationCoordinator.(observation.MatchResetter)
	demoResetter = demoStateResetter{traces: demoResetter, relationships: relationshipResetter, observations: observationResetter}
	// ADR-0006 memory seam: async observations queue into Memobase with the
	// local audit/backlog tables; without a database the queue still runs and
	// simply degrades to Ledger-only recall. The open-thread ledger (C2), the
	// user talkativeness preference and the portrait override layer (C3
	// 球球懂我) stay local Postgres tables.
	memoryCtx, memoryCancel := context.WithCancel(context.Background())
	defer memoryCancel()
	memobaseAdapter := memory.NewMemobase(memory.MemobaseConfig{
		BaseURL: cfg.MemobaseURL,
		Token:   cfg.MemobaseToken,
		Timeout: cfg.MemobaseExtractionTimeout(),
	})
	var memoryQueue *memory.Queue
	var memoryPreferenceStore *memory.PostgresRecords
	// 语义记忆双路（openspec/changes/semantic-memory）：EMBEDDING_BASE_URL
	// 留空即整体停用（行为=现状 contains 单路）；配置后 Observe 异步嵌写、
	// Recall 双路合并，任何 embedding 故障弃权。
	var vectorOptions []memory.QueueOption
	if cfg.EmbeddingBaseURL != "" && knowledgeEmbedder != nil {
		if cfg.RecallDecayDays > 0 {
			vectorOptions = append(vectorOptions, memory.WithRecallDecay(float64(cfg.RecallDecayDays)))
		}
		if cfg.DatabaseURL != "" {
			vectorStore, err := memory.OpenPostgresVectorStore(context.Background(), cfg.DatabaseURL)
			if err != nil {
				log.Fatalf("postgres vector store: %v", err)
			}
			defer vectorStore.Close()
			vectorOptions = append(vectorOptions, memory.WithVectorRecall(vectorStore, knowledgeEmbedder))
		} else {
			vectorOptions = append(vectorOptions, memory.WithVectorRecall(memory.NewMemoryVectorStore(), knowledgeEmbedder))
		}
	}
	if cfg.DatabaseURL != "" {
		memoryRecords, err := memory.OpenRecords(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres memory records: %v", err)
		}
		defer memoryRecords.Close()
		memoryThreads, err := memory.OpenThreadStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres memory threads: %v", err)
		}
		defer memoryThreads.Close()
		queueOptions := []memory.QueueOption{memory.WithReflections(memoryRecords), memory.WithThreads(memoryThreads), memory.WithPortraitOverlays(memoryRecords)}
		// 画像冲突操作集（portrait-maintenance 阶段一）：Reflection 把新主
		// 张并入权威层前先经 structured seam 判定 ADD/UPDATE/DELETE/NOOP。
		// 未配判定模型（CI/evals）即盲 ADD——冲突知识集中在操作集一处，是
		// 加深而不是旁路。
		if cfg.MiMoAPIKey != "" {
			queueOptions = append(queueOptions, memory.WithPortraitOps(memory.NewLLMPortraitOpDecider(structured.NewClient(cfg.MiMoBaseURL, cfg.MiMoAPIKey, cfg.MiMoModel))))
		}
		queueOptions = append(queueOptions, vectorOptions...)
		memoryQueue = memory.NewQueue(memobaseAdapter, memoryRecords, memoryRecords, queueOptions...)
		memoryPreferenceStore = memoryRecords
	} else {
		// No database: the C3 portrait overlay layer lives in-process so the
		// 球球懂我 page still edits real state for the running server.
		queueOptions := append([]memory.QueueOption{memory.WithPortraitOverlays(memory.NewMemoryPortraitOverlays())}, vectorOptions...)
		if cfg.MiMoAPIKey != "" {
			queueOptions = append(queueOptions, memory.WithPortraitOps(memory.NewLLMPortraitOpDecider(structured.NewClient(cfg.MiMoBaseURL, cfg.MiMoAPIKey, cfg.MiMoModel))))
		}
		memoryQueue = memory.NewQueue(memobaseAdapter, nil, nil, queueOptions...)
	}
	companionAgent.WithMemories(memoryQueue)
	go memoryQueue.Run(memoryCtx)
	go runReflectionBeat(memoryCtx, memoryQueue, 15*time.Minute)
	if llmClient != nil {
		companionAgent.WithRealizer(companion.NewLLMReplyRealizer(llmClient), cfg.CompanionRealizerTimeout())
	}
	// ADR-0009 intent router: keyword-miss turns route through mimo-v2.5 when
	// a key is configured; unset (CI/evals) keeps the legacy behaviour.
	companionAgent.WithRouter(router.NewClient(router.Config{
		BaseURL: cfg.RouterBaseURL,
		APIKey:  cfg.RouterAPIKey,
		Model:   cfg.RouterModel,
		Timeout: time.Duration(cfg.RouterTimeoutMS) * time.Millisecond,
	}))
	if registrar, ok := matchStore.(matchstate.EventObserverRegistrar); ok && observationCoordinator != nil {
		registrar.SetEventObserver(func(event matchstate.MatchEvent) error {
			if event.EventType == "match_end" || event.EventType == "fulltime" {
				memoryQueue.NotifyMatchEnded(event.MatchID)
				scheduleFulltimeReviewReminders(memoryCtx, reminderStore, memoryQueue, matchStore, event)
			}
			observationCtx, observationCancel := context.WithTimeout(cleanupCtx, 5*time.Second)
			defer observationCancel()
			_, err := companionAgent.HandleObservationFactChanged(observationCtx, event, time.Now().UTC())
			return err
		})
	}
	if outboxRunner != nil {
		go outboxRunner.RunOutbox(outboxCtx)
	}

	// 提醒簿清理节拍：过期待递翻 suppressed 并转记忆素材（Q13：错过开球
	// 的提醒不再补发，成为下次聊天可引用的共同事实）。到点投递由连接内
	// 低频检查与连接时补递承担（socket 归连接所有）。
	go proactive.SweepLoop(outboxCtx, reminderStore, func(ctx context.Context, reminder proactive.Reminder) {
		if memoryQueue == nil {
			return
		}
		_ = memoryQueue.Observe(ctx, memory.Moment{
			UserID:     reminder.UserID,
			Kind:       memory.MomentUserFact,
			Content:    fmt.Sprintf("开球前没等到你：%s 对 %s 的赛前提醒错过了。", reminder.HomeTeam, reminder.AwayTeam),
			Importance: 0.6,
			OccurredAt: time.Now().UTC(),
		})
	}, time.Minute)

	mux := http.NewServeMux()
	watchSessions := conversation.NewWatchSessionRegistry(context.Background(), conversation.Config{})
	defer watchSessions.Close()
	var deliveryStore *conversation.PostgresDeliveryStore
	if cfg.DatabaseURL != "" {
		var err error
		deliveryStore, err = conversation.OpenPostgresDeliveryStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres delivery ledger: %v", err)
		}
		defer deliveryStore.Close()
		watchSessions.WithStore(deliveryStore)
	}
	mux.HandleFunc("/health", hub.HandleHealth)
	mux.HandleFunc("/api/sessions/", handleSessionAPI(sessionManager, cfg))
	mux.HandleFunc("/api/me/", handlePrivacyAPI(sessionManager, cfg, privacyService))
	mux.HandleFunc("/api/me/character", handleCharacterAPI(sessionManager, cfg, characterSettings, interactionLedger))
	// C3 球球懂我: the user-facing portrait page (read/edit/forget) on the
	// privacy API's transport (session bearer auth, account-scoped).
	mux.HandleFunc("/api/me/portrait", handlePortraitAPI(sessionManager, cfg, memoryQueue))
	mux.HandleFunc("/api/matches/catalog", handleMatchCatalog(matchStore, cfg))
	mux.HandleFunc("/api/matches/", handleMatchAPIWithOperatorAuth(matchStore, traceReader, demoResetter, cfg, llmClient, sourceManager, directorDrafts, interactionLedger, authz, operatorWrites))
	// ADR-0008 operations console API: the three-tier IA data surface
	// (overview → match → user) behind operator auth + scopes.
	mux.HandleFunc("/api/console/", handleConsoleAPI(consoleAPI{
		cfg:           cfg,
		authz:         authz,
		matches:       matchStore,
		traces:        traceReader,
		ledger:        interactionLedger,
		sessions:      watchSessions,
		memories:      memoryQueue,
		operators:     operatorStore,
		preferences:   memoryPreferenceStore,
		writes:        operatorWrites,
		interruptions: sharedInterruptions,
		jwtSecret:     cfg.JWTSecret,
	}))
	fs := http.StripPrefix("/live2d-assets/", http.FileServer(http.Dir("../client/assets/live2d")))
	mux.HandleFunc("/live2d-assets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.URL.Path == "/live2d-assets/operator-live-state.js" {
			noCache(w)
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(200)
			return
		}
		fs.ServeHTTP(w, r)
	})
	mux.HandleFunc("/live2d.html", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		http.ServeFile(w, r, "../client/assets/live2d/live2d.html")
	})
	// operator.html 已退役（ADR-0013）：实时运营面收敛到 /console 新导播台，
	// 旧页与其服务端点一并删除；/api/matches/* 冻结形状由迁移后的 28 条
	// operator-control evals 继续守护。
	registerDevelopmentPages(mux, cfg.Environment, "../client/assets/live2d")
	webApp := http.FileServer(http.Dir(resolveWebAppDir()))
	// ADR-0008 operations console (built from ../console/dist), hash-routed
	// SPA: /console/ serves index.html, /console redirects to keep the slash.
	consoleApp := http.FileServer(http.Dir(resolveConsoleDir()))
	mux.HandleFunc("/console", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/console" {
			http.Redirect(w, r, "/console/", http.StatusPermanentRedirect)
			return
		}
		consoleApp.ServeHTTP(w, r)
	})
	mux.Handle("/console/", http.StripPrefix("/console/", consoleApp))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app.html" {
			http.Redirect(w, r, "/", http.StatusPermanentRedirect)
			return
		}
		if r.URL.Path == "/" || filepath.Ext(r.URL.Path) == ".html" {
			noCache(w)
		}
		webApp.ServeHTTP(w, r)
	})
	// /ws/match/ 实时会话引擎（原 675 行内联闭包，见 watchconnection.go）。
	mux.HandleFunc("/ws/match/", handleWatchConnection(watchDeps{
		hub: hub, matchStore: matchStore, traceReader: traceReader, watchSessions: watchSessions,
		agent: companionAgent, tts: ttsClient, asr: asrClient, cfg: cfg, memories: memoryQueue,
		reminders: reminderStore,
		submittedSignals: submittedUserSignals,
		ambient:          ambientSidecar,
	}))

	addr := ":" + cfg.Port
	log.Printf("qiuqiu server starting on %s", addr)
	if err := newHTTPServer(addr, mux).ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func configuredSpeechSynthesizer(cfg *config.Config) speechSynthesizer {
	if cfg == nil {
		return nil
	}
	if cfg.MiMoAPIKey != "" {
		return tts.NewClient(cfg.MiMoAPIKey).WithBaseURL(cfg.MiMoBaseURL).WithModel("mimo-v2.5-tts").WithVoice(cfg.MiMoVoice)
	}
	if strings.EqualFold(cfg.Environment, "development") && strings.TrimSpace(os.Getenv("QIUQIU_RUNTIME_TTS")) == "1" {
		return tts.NewMockClient([]byte("qiuqiu-runtime-eval-audio"))
	}
	return nil
}

func observationFollowUpsForUser(responses []companion.ObservationResponse, userID string, now time.Time) []companion.ObservationResponse {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	selected := make([]companion.ObservationResponse, 0, len(responses))
	for _, response := range responses {
		if response.Resolution.UserID != userID {
			continue
		}
		if !response.Resolution.FollowUpDeadline.IsZero() && now.After(response.Resolution.FollowUpDeadline) {
			continue
		}
		selected = append(selected, response)
	}
	return selected
}

func registerDevelopmentPages(mux *http.ServeMux, environment, assetsDir string) {
	if strings.EqualFold(strings.TrimSpace(environment), "production") {
		return
	}
	for _, name := range []string{"test-expressions.html", "director-prototype.html"} {
		path := "/" + name
		file := filepath.Join(assetsDir, name)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			noCache(w)
			http.ServeFile(w, r, file)
		})
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
}

func runPrivacyCleanup(ctx context.Context, service *privacy.Service) {
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := service.CleanupExpired(cleanupCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("privacy cleanup error: %v", err)
		}
	}
	cleanup()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}

func runObservationExpiry(ctx context.Context, coordinator observation.Coordinator) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if _, err := coordinator.Expire(ctx, now.UTC()); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("observation expiry error: %v", err)
			}
		}
	}
}

// runReflectionBeat is the ADR-0006 reflection: after each ended match and on
// an idle ticker it flushes pending extractions, refreshes the user portrait
// and persists an audit record citing the ledger sequences observed since the
// previous beat. The same ticker expires stale open threads (C2): expired is
// distinct from addressed, and every expiry is written to the audit table by
// the thread store. Failures are logged; reflection never touches replies.
func runReflectionBeat(ctx context.Context, memoryQueue *memory.Queue, idleInterval time.Duration) {
	if memoryQueue == nil {
		return
	}
	lastIdle := time.Now()
	expireThreads := func() {
		expireCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		expired, err := memoryQueue.ExpireStaleThreads(expireCtx, time.Now().UTC(), memory.DefaultThreadTTL)
		if err != nil {
			if !errors.Is(err, memory.ErrNotSupported) {
				log.Printf("memory thread expiry error: %v", err)
			}
			return
		}
		if len(expired) > 0 {
			log.Printf("memory: expired %d stale open thread(s)", len(expired))
		}
	}
	reflect := func(userID, matchID, trigger string) {
		reflectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if _, err := memoryQueue.ReflectNow(reflectCtx, userID, matchID, trigger); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("memory reflection (%s) error for user %q: %v", trigger, userID, err)
		}
	}
	expireThreads()
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expireThreads()
			users := memoryQueue.ActiveUsers()
			if len(users) == 0 {
				continue
			}
			if ended := memoryQueue.TakeMatchEnds(); len(ended) > 0 {
				for _, userID := range users {
					// reflection-attribution：审计标签用用户实际看过的那场
					// 终场（多场取最近互动的），没看任何一场则空标签——冲洗
					// 与画像刷新照常，不把别人的比赛挂到用户头上。
					reflect(userID, memoryQueue.RecentEndedMatchFor(userID, ended), "post_match")
				}
				lastIdle = time.Now()
				continue
			}
			if time.Since(lastIdle) < idleInterval {
				continue
			}
			lastIdle = time.Now()
			for _, userID := range users {
				reflect(userID, "", "idle")
			}
		}
	}
}

// matchFactRetractedMessage lets a client remove a proactive reply that was
// generated from a fact which is no longer part of the public match record.
func matchFactRetractedMessage(event matchstate.MatchEvent) map[string]interface{} {
	return map[string]interface{}{
		"type": "match_fact_retracted",
		"data": map[string]string{
			"eventId": event.ID,
			"factId":  event.FactID,
		},
	}
}

func clientSnapshot(snapshot matchstate.Snapshot) matchstate.Snapshot {
	snapshot.Integrity = matchstate.MatchIntegrity{}
	return snapshot
}

func operatorAuditEvents(events, publicEvents []matchstate.MatchEvent) []matchstate.MatchEvent {
	effectiveByID := make(map[string]matchstate.Score, len(publicEvents))
	for _, event := range publicEvents {
		effectiveByID[event.ID] = event.Score
	}
	for index := range events {
		events[index].ReportedScore = nil
		events[index].EffectiveScoreAfter = nil
		reported := new(matchstate.Score)
		*reported = events[index].Score
		events[index].ReportedScore = reported
		if effective, exists := effectiveByID[events[index].ID]; exists {
			effectiveAfter := new(matchstate.Score)
			*effectiveAfter = effective
			events[index].EffectiveScoreAfter = effectiveAfter
		}
	}
	return events
}


func handleTranscribedVoiceSessionWithSignalIDOptions(ctx context.Context, agent *companion.Agent, synthesizer speechSynthesizer, matchID, userID, text, provider string, now time.Time, signalID string, options voiceSessionOptions) (voiceSessionResult, error) {
	text = strings.TrimSpace(text)
	voiceMeta := &companion.VoiceTraceMetadata{
		ASRStatus:   "ok",
		ASRText:     text,
		ASRProvider: strings.TrimSpace(provider),
	}
	return completeVoiceSessionWithOptions(
		ctx,
		agent,
		synthesizer,
		matchID,
		userID,
		now,
		signalID,
		voiceSessionResult{Text: text},
		voiceMeta,
		options,
	)
}

type voiceSessionOptions struct {
	ProgressiveSchedule bool
	Timezone            string
	FactRefresh         string
	Talkativeness       string
	// Settings 是用户显式设置的粘性覆盖（settings-in-policy，ADR-0018）。
	Settings *relationship.PreferenceOverrides
}

func completeVoiceSession(ctx context.Context, agent *companion.Agent, synthesizer speechSynthesizer, matchID, userID string, now time.Time, signalID string, result voiceSessionResult, voiceMeta *companion.VoiceTraceMetadata) (voiceSessionResult, error) {
	return completeVoiceSessionWithOptions(ctx, agent, synthesizer, matchID, userID, now, signalID, result, voiceMeta, voiceSessionOptions{})
}

func completeVoiceSessionWithOptions(ctx context.Context, agent *companion.Agent, synthesizer speechSynthesizer, matchID, userID string, now time.Time, signalID string, result voiceSessionResult, voiceMeta *companion.VoiceTraceMetadata, options voiceSessionOptions) (voiceSessionResult, error) {
	if result.Text == "" {
		if result.ASRError == "" {
			result.ASRError = "empty voice input"
		}
		return result, fmt.Errorf("no usable user text")
	}
	response, err := agent.HandleMessage(ctx, companion.MessageRequest{
		SignalID:            signalID,
		FactRefresh:         options.FactRefresh,
		MatchID:             matchID,
		UserID:              userID,
		Text:                result.Text,
		Timezone:            strings.TrimSpace(options.Timezone),
		Talkativeness:       options.Talkativeness,
		ProgressiveSchedule: options.ProgressiveSchedule,
		Now:                 now,
		Voice:               nonEmptyVoiceMeta(voiceMeta),
	})
	if err != nil {
		return result, err
	}
	result.Reply = response.Reply
	result.Trace = response.Trace
	result.Presentation = response.Presentation
	result.ScheduleLookup = response.ScheduleLookup
	if synthesizer != nil {
		ttsResult, err := synthesizeReply(ctx, synthesizer, result.Reply, result.Presentation, turnActs(result.Trace))
		if err != nil {
			result.TTSError = err.Error()
			result.Trace.Voice = ensureVoiceMeta(result.Trace.Voice)
			result.Trace.Voice.TTSStatus = "failed"
			result.Trace.Voice.TTSError = result.TTSError
			_ = agent.RecordMediaDelivery(ctx, interaction.Event{ID: "tts:" + userID + ":" + matchID + ":" + result.Trace.ID + ":failed", Kind: interaction.KindMediaDelivery, UserID: userID, MatchID: matchID, TraceID: result.Trace.ID, DeliveryKey: result.Trace.ID, DeliveryState: "failed", Source: "tts", CreatedAt: time.Now().UTC()})
		} else {
			result.AudioData = ttsResult.AudioData
			result.AudioMIME = ttsResult.MimeType
			result.Trace.Voice = ensureVoiceMeta(result.Trace.Voice)
			result.Trace.Voice.TTSStatus = "ok"
			result.Trace.Voice.TTSMime = result.AudioMIME
			result.Trace.Voice.TTSByteCount = len(result.AudioData)
			// 延迟分解：tts_synthesized（voice-transport-upgrade 1.1，操作台
			// HTTP 语音路径；WS 路径的合成在投递服务内，由 audio_delivered 覆盖）。
			log.Printf("voice latency event: user=%q match=%q signal=%q stage=%q elapsed_ms=%d",
				userID, matchID, signalID, "tts_synthesized", time.Since(now).Milliseconds())
			_ = agent.RecordMediaDelivery(ctx, interaction.Event{ID: "tts:" + userID + ":" + matchID + ":" + result.Trace.ID + ":audio_ready", Kind: interaction.KindMediaDelivery, UserID: userID, MatchID: matchID, TraceID: result.Trace.ID, DeliveryKey: result.Trace.ID, MediaType: result.AudioMIME, DeliveryState: "audio_ready", Source: "tts", CreatedAt: time.Now().UTC()})
		}
	}
	return result, nil
}

func handleVoiceTurnWithFactRefresh(snapshot func() matchstate.Snapshot, generate func(text, audio, factRefresh string) (voiceSessionResult, error), suppressFollowUp func(string) bool, text, audio string) (voiceSessionResult, error) {
	before := latestCriticalFactRevisionKey(snapshot())
	result, err := generate(text, audio, "")
	if err != nil {
		return result, err
	}
	after := latestCriticalFactRevisionKey(snapshot())
	if before == after || result.Text == "" {
		return result, nil
	}
	refreshed, refreshErr := generate(result.Text, "", after)
	if refreshErr != nil {
		return result, nil
	}
	if result.Trace.Observation != nil {
		if suppressFollowUp == nil || !suppressFollowUp(result.Trace.Observation.ID) {
			return result, nil
		}
		refreshed.Trace.Observation = result.Trace.Observation
		refreshed.Trace.ToolCalls = append(refreshed.Trace.ToolCalls, companion.ToolCall{
			Name: "observation.follow_up",
			Args: map[string]string{"status": "suppressed_in_band", "observationId": result.Trace.Observation.ID},
		})
	}
	return refreshed, nil
}

func latestCriticalFactRevisionKey(snapshot matchstate.Snapshot) string {
	for _, event := range snapshot.KeyEvents {
		if proactiveUrgency(event.EventType) != conversation.UrgencyCritical {
			continue
		}
		if event.ID != "" {
			return matchstate.DeliveryKey(event)
		}
		return fmt.Sprintf("%s:%s:%s:%d:%s", event.EventType, event.Clock, event.UpdatedAt, event.FactRevision, event.FactStatus)
	}
	return ""
}

func replyContextActive(ctx context.Context) bool {
	return ctx != nil && ctx.Err() == nil
}

func proactiveUrgency(eventType string) conversation.Urgency {
	switch eventType {
	case "goal", "red_card", "penalty", "penalty_awarded", "var_check", "var_result", "goal_cancelled", "halftime", "fulltime", "match_end":
		return conversation.UrgencyCritical
	default:
		return conversation.UrgencyNormal
	}
}

func proactiveTTL(eventType string) time.Duration {
	if proactiveUrgency(eventType) == conversation.UrgencyCritical {
		return 45 * time.Second
	}
	return 12 * time.Second
}

func proactiveSchedule(decision relationship.Decision, eventType string) (conversation.Urgency, time.Duration) {
	urgency := proactiveUrgency(eventType)
	ttl := proactiveTTL(eventType)
	if decision.Speech == nil {
		return urgency, ttl
	}
	if decision.Speech.Delivery.Urgency == "critical" {
		urgency = conversation.UrgencyCritical
	} else if decision.Speech.Delivery.Urgency == "normal" {
		urgency = conversation.UrgencyNormal
	}
	if decision.Speech.Delivery.TTLSeconds > 0 {
		ttl = time.Duration(decision.Speech.Delivery.TTLSeconds) * time.Second
	}
	return urgency, ttl
}

func recordPlaybackStatus(ctx context.Context, reader companion.TraceReader, agent *companion.Agent, matchID, traceID, userID, status string) error {
	trace, err := reader.GetTrace(ctx, matchID, traceID)
	if err != nil {
		return err
	}
	if userID == "" || trace.UserID != userID {
		return fmt.Errorf("playback trace owner mismatch")
	}
	return agent.RecordMediaDelivery(ctx, interaction.Event{
		ID:   "playback:" + userID + ":" + matchID + ":" + traceID + ":" + status,
		Kind: interaction.KindPlaybackResult, UserID: userID, MatchID: matchID,
		SignalID: "delivery:" + traceID + ":" + status, TraceID: traceID, DeliveryKey: traceID, PlaybackState: status,
		Source: "client_playback", CreatedAt: time.Now().UTC(),
	})
}

func recordDisplayedReply(ctx context.Context, reader companion.TraceReader, agent *companion.Agent, matchID, traceID, userID string, now time.Time) error {
	trace, err := reader.GetTrace(ctx, matchID, traceID)
	if err != nil {
		return err
	}
	if userID == "" || trace.UserID != userID {
		return fmt.Errorf("displayed reply owner mismatch")
	}
	purpose := "user_reply"
	if trace.Input == "first_meeting" {
		purpose = "first_meeting"
	} else if trace.ObservationResolution != nil {
		purpose = "observation_resolution"
	} else if trace.Reason == companion.ReasonOperatorEventProactive || trace.Reason == companion.ReasonRelationshipMatchReaction {
		purpose = "match_reaction"
	}
	decisionID := ""
	var usedMemoryIDs []string
	if trace.RelationshipDecision != nil {
		decisionID = trace.RelationshipDecision.ID
		usedMemoryIDs = trace.RelationshipDecision.UsedMemoryIDs
	}
	_, err = agent.Plan(ctx, companion.TurnInput{Kind: companion.TurnKindDelivery, Delivery: &companion.DeliveryInput{
		SignalID: "delivery:" + traceID + ":text", TraceID: traceID, UserID: userID, MatchID: matchID,
		DecisionID: decisionID, State: "text_delivered", Purpose: purpose,
		UsedMemoryIDs: usedMemoryIDs, Now: now,
	}})
	if err != nil {
		return err
	}
	if trace.ObservationResolution != nil {
		return agent.MarkObservationResolutionDelivered(ctx, trace.ObservationResolution.DeliveryKey, now)
	}
	return nil
}

func ensureVoiceMeta(meta *companion.VoiceTraceMetadata) *companion.VoiceTraceMetadata {
	if meta != nil {
		return meta
	}
	return &companion.VoiceTraceMetadata{}
}

func nonEmptyVoiceMeta(meta *companion.VoiceTraceMetadata) *companion.VoiceTraceMetadata {
	if meta == nil {
		return nil
	}
	if meta.ASRStatus == "" && meta.ASRText == "" && meta.ASRError == "" && meta.TTSStatus == "" && meta.TTSError == "" {
		return nil
	}
	return meta
}

func applyCORS(w http.ResponseWriter, r *http.Request, cfg *config.Config) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		if !cfg.OriginAllowedForHost(origin, r.Host) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return false
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "DELETE, GET, OPTIONS, PATCH, POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
	return true
}

func isDemoMatchID(matchID string) bool {
	matchID = strings.TrimSpace(matchID)
	return matchID == "test" || matchID == "operator-config-e2e" || strings.HasPrefix(matchID, "demo-")
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("writeJSON error: %v", err)
	}
}

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func resolveWebAppDir() string {
	candidates := []string{
		strings.TrimSpace(os.Getenv("QIUQIU_WEB_DIR")),
		"../client/build/web",
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "../client/build/web"
}

// resolveConsoleDir locates the built operations console (ADR-0008): the
// Vite build output at ../console/dist, overridable via QIUQIU_CONSOLE_DIR.
func resolveConsoleDir() string {
	candidates := []string{
		strings.TrimSpace(os.Getenv("QIUQIU_CONSOLE_DIR")),
		"../console/dist",
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "../console/dist"
}

func playbackTraceStatus(state string) string {
	switch state {
	case "started", "ended":
		return "ok"
	case "skipped":
		return "skipped"
	case "blocked":
		return "blocked:autoplay"
	case "interrupted":
		return "interrupted"
	default:
		return "failed:" + state
	}
}

func deliveryStateForPlayback(state string) conversation.DeliveryState {
	switch state {
	case "started":
		return conversation.DeliveryAudioStarted
	case "ended", "completed":
		return conversation.DeliveryCompleted
	case "interrupted":
		return conversation.DeliveryInterrupted
	case "skipped", "blocked":
		return conversation.DeliverySkipped
	default:
		return conversation.DeliveryFailed
	}
}

func terminalPlaybackState(state string) bool {
	switch state {
	case "ended", "completed", "interrupted", "skipped", "blocked":
		return true
	default:
		return false
	}
}

func newTextLLMClient(cfg *config.Config) *llm.Client {
	if cfg == nil || strings.TrimSpace(cfg.MiMoAPIKey) == "" {
		return nil
	}
	return llm.NewClient(cfg.MiMoBaseURL, cfg.MiMoAPIKey, cfg.MiMoModel)
}

func eventFromVoiceDraft(result directordraft.Result, currentScore matchstate.Score) (matchstate.MatchEvent, error) {
	if !result.Ready {
		return matchstate.MatchEvent{}, errors.New("语音内容无法确认完整赛事事件，请补充球队、球员或行为后重试")
	}
	draft := result.Draft
	event := matchstate.MatchEvent{
		Source:            "operator_voice",
		ProviderName:      "director-voice",
		EventType:         draft.EventType,
		Period:            draft.OccurredPeriod,
		Clock:             fmt.Sprintf("%02d:%02d", draft.OccurredSeconds/60, draft.OccurredSeconds%60),
		TeamID:            draft.TeamID,
		TeamName:          draft.TeamName,
		Score:             currentScore,
		Intensity:         3,
		Confirmed:         true,
		FactStatus:        matchstate.FactStatusConfirmed,
		Description:       draft.Description,
		RecommendedAction: recommendedActionForVoiceEvent(draft.EventType),
		Tags:              []string{fmt.Sprintf("clockVersion=%d", draft.CapturedClockVersion), "input=operator_voice"},
	}
	for _, participant := range draft.Participants {
		event.Participants = append(event.Participants, matchstate.Participant{
			Role: participant.Role, Name: participant.Name, TeamID: participant.TeamID, TeamName: participant.TeamName,
		})
	}
	primaryRole := primaryRoleForVoiceEvent(draft.EventType)
	for _, participant := range event.Participants {
		if participant.Role == primaryRole {
			event.PlayerName = participant.Name
			break
		}
	}
	if event.EventType == "goal" {
		switch event.TeamID {
		case "home":
			event.Score.Home++
		case "away":
			event.Score.Away++
		default:
			return matchstate.MatchEvent{}, errors.New("进球事件缺少进球队伍")
		}
	}
	return event, nil
}

func primaryRoleForVoiceEvent(eventType string) string {
	switch eventType {
	case "goal":
		return "scorer"
	case "shot", "miss":
		return "shooter"
	case "save":
		return "keeper"
	case "foul", "yellow_card", "red_card":
		return "offender"
	case "substitution":
		return "sub_on"
	case "injury":
		return "injured"
	default:
		return ""
	}
}

func recommendedActionForVoiceEvent(eventType string) string {
	switch eventType {
	case "goal":
		return "celebrate"
	case "shot", "pressure":
		return "focus"
	case "save":
		return "surprise"
	case "miss":
		return "miss"
	case "foul", "yellow_card":
		return "complain"
	case "red_card":
		return "angry"
	case "injury":
		return "comfort"
	default:
		return "analysis"
	}
}

func markProactiveMode(event *matchstate.MatchEvent) {
	event.Tags = removeTagPrefix(event.Tags, "proactive=")
	if event.EventType == "score_correction" {
		event.ProactiveText = ""
		event.Tags = append(event.Tags, "proactive=quiet")
		return
	}
	if event.ProactiveText == "__quiet__" {
		event.ProactiveText = ""
		event.Tags = append(event.Tags, "proactive=quiet")
		return
	}
	if strings.TrimSpace(event.ProactiveText) == "" {
		event.ProactiveText = companion.FallbackProactiveText(*event)
		event.Tags = append(event.Tags, "proactive=auto")
		return
	}
	event.Tags = append(event.Tags, "proactive=manual")
}

func applyRequestedFactStatus(event *matchstate.MatchEvent) {
	if event == nil || event.FactStatus != "" {
		return
	}
	if event.Confirmed {
		event.FactStatus = matchstate.FactStatusConfirmed
	}
}

func removeTagPrefix(tags []string, prefix string) []string {
	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		if strings.HasPrefix(tag, prefix) {
			continue
		}
		filtered = append(filtered, tag)
	}
	return filtered
}

func hasEventTag(event matchstate.MatchEvent, tag string) bool {
	for _, candidate := range event.Tags {
		if candidate == tag {
			return true
		}
	}
	return false
}

func str(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func sendJSON(conn *websocket.Conn, msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("sendJSON marshal error: %v", err)
		return
	}
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("sendJSON write error: %v", err)
	}
}

type wsWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *wsWriter) SendJSON(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("ws marshal error: %v", err)
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := w.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("ws write error: %v", err)
		return err
	}
	return nil
}

func (w *wsWriter) SendBinary(data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := w.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		log.Printf("ws binary write error: %v", err)
	}
}

func (w *wsWriter) SendAudio(meta interface{}, data []byte) error {
	encoded, err := json.Marshal(meta)
	if err != nil {
		log.Printf("ws audio metadata marshal error: %v", err)
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := w.conn.WriteMessage(websocket.TextMessage, encoded); err != nil {
		log.Printf("ws audio metadata write error: %v", err)
		return err
	}
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := w.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		log.Printf("ws audio write error: %v", err)
		return err
	}
	return nil
}

func (w *wsWriter) Ping() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return w.conn.WriteMessage(websocket.PingMessage, nil)
}

// pcmToWav wraps raw PCM 16bit 16kHz mono in a WAV header.
func pcmToWav(pcm []byte) []byte {
	return asr.PCM16ToWAV(pcm)
}
