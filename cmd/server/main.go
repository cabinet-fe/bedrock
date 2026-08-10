package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	aihandler "bedrock/internal/ai/handler"
	airepo "bedrock/internal/ai/repository"
	aiservice "bedrock/internal/ai/service"
	authhandler "bedrock/internal/auth/handler"
	authmiddleware "bedrock/internal/auth/middleware"
	authrepo "bedrock/internal/auth/repository"
	authservice "bedrock/internal/auth/service"
	cicdhandler "bedrock/internal/cicd/handler"
	cicdrepo "bedrock/internal/cicd/repository"
	cicdservice "bedrock/internal/cicd/service"
	dashboardhandler "bedrock/internal/dashboard/handler"
	dashboardrepo "bedrock/internal/dashboard/repository"
	dashboardservice "bedrock/internal/dashboard/service"
	"bedrock/internal/engine"
	"bedrock/internal/middleware"
	opshandler "bedrock/internal/ops/handler"
	opsrepo "bedrock/internal/ops/repository"
	opsservice "bedrock/internal/ops/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	projecthandler "bedrock/internal/project/handler"
	projectrepo "bedrock/internal/project/repository"
	projectservice "bedrock/internal/project/service"
	rbachandler "bedrock/internal/rbac/handler"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
	resourcehandler "bedrock/internal/resource/handler"
	resourcerepo "bedrock/internal/resource/repository"
	resourceservice "bedrock/internal/resource/service"
	storagerepo "bedrock/internal/storage/repository"
	storageservice "bedrock/internal/storage/service"
	systemhandler "bedrock/internal/system/handler"
	systemmw "bedrock/internal/system/middleware"
	systemrepo "bedrock/internal/system/repository"
	systemservice "bedrock/internal/system/service"
	"bedrock/internal/ws"
)

var version = "dev"

func main() {
	startedAt := time.Now().UTC()
	configPath := flag.String("config", "config.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	var logger *zap.Logger
	if gin.Mode() == gin.ReleaseMode {
		logger, _ = zap.NewProduction()
	} else {
		logger, _ = zap.NewDevelopment()
	}
	defer logger.Sync()
	logger.Info("Bedrock server",
		zap.String("version", version),
		zap.String("db_driver", cfg.Database.Driver),
	)
	logger.Info("database driver change does not migrate data; 2.0 supports fresh install only")
	logger.Info("build scripts and AI CLIs execute as the same OS user as Bedrock (no OS/container sandbox isolation)")

	if err := pkg.InitEncryption(cfg.Encryption.Key); err != nil {
		logger.Fatal("Failed to init encryption", zap.Error(err))
	}

	gdb, err := db.Open(&cfg.Database)
	if err != nil {
		logger.Fatal("Failed to open database", zap.Error(err))
	}

	agentWorkDir := cfg.Build.WorkspaceDir
	if agentWorkDir == "" {
		agentWorkDir = "./data/workspace"
	}
	agentArtifactDir := cfg.Build.ArtifactDir
	if agentArtifactDir == "" {
		agentArtifactDir = "./data/artifacts"
	}
	agentCleanup, err := migration.PrepareAgentPersistentWorkspaceCleanup(gdb, agentWorkDir, agentArtifactDir)
	if err != nil {
		logger.Fatal("Failed to prepare legacy Agent workspace cleanup", zap.Error(err))
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver(cfg.Database.Driver)); err != nil {
		logger.Fatal("Failed to apply migrations", zap.Error(err))
	}
	if err := agentCleanup.Finalize(); err != nil {
		logger.Fatal("Failed to finalize legacy Agent workspace cleanup", zap.Error(err))
	}
	if err := seed.EnsureSuperAdmin(gdb, cfg.Admin); err != nil {
		logger.Fatal("Failed to seed super-admin", zap.Error(err))
	}
	if err := seed.EnsureRBACResources(gdb); err != nil {
		logger.Fatal("Failed to seed RBAC resources", zap.Error(err))
	}

	userRepo := authrepo.NewUserRepository(gdb)
	roleRepo := rbacrepo.NewRoleRepository(gdb)
	resourceRepo := rbacrepo.NewResourceRepository(gdb)
	menuGroupRepo := rbacrepo.NewMenuGroupRepository(gdb)
	dictRepo := systemrepo.NewDictionaryRepository(gdb)
	logRepo := systemrepo.NewOperationLogRepository(gdb)

	permSvc := rbacservice.NewPermissionService(roleRepo, resourceRepo, menuGroupRepo)
	roleSvc := rbacservice.NewRoleService(roleRepo, resourceRepo)
	resourceSvc := rbacservice.NewResourceService(resourceRepo, menuGroupRepo)
	menuGroupSvc := rbacservice.NewMenuGroupService(menuGroupRepo)
	userSvc := systemservice.NewUserService(userRepo, roleSvc)
	dictSvc := systemservice.NewDictionaryService(dictRepo)
	auditSvc := systemservice.NewAuditService(logRepo)

	authSvc, err := authservice.NewAuthService(cfg, userRepo, permSvc)
	if err != nil {
		logger.Fatal("Failed to init auth service", zap.Error(err))
	}

	authHandler := authhandler.NewAuthHandler(authSvc)
	userHandler := systemhandler.NewUserHandler(userSvc, permSvc)
	roleHandler := rbachandler.NewRoleHandler(roleSvc, permSvc)
	resourceHandler := rbachandler.NewResourceHandler(resourceSvc, menuGroupSvc, permSvc)
	dictHandler := systemhandler.NewDictionaryHandler(dictSvc, permSvc)
	logHandler := systemhandler.NewOperationLogHandler(auditSvc, permSvc)

	credRepo := resourcerepo.NewCredentialRepository(gdb)
	repoRepo := resourcerepo.NewRepositoryRepository(gdb)
	serverRepo := resourcerepo.NewServerRepository(gdb)
	cliRepo := resourcerepo.NewCLIRepository(gdb)
	patRepo := resourcerepo.NewPATRepository(gdb)
	jobRepo := cicdrepo.NewBuildJobRepository(gdb)
	runRepo := cicdrepo.NewBuildRunRepository(gdb)
	deliveryRepo := cicdrepo.NewWebhookDeliveryRepository(gdb)
	scriptJobRepo := cicdrepo.NewScriptJobRepository(gdb)
	scriptRunRepo := cicdrepo.NewScriptRunRepository(gdb)
	scriptDeliveryRepo := cicdrepo.NewScriptWebhookDeliveryRepository(gdb)
	pipelineRepo := cicdrepo.NewBuildPipelineRepository(gdb)
	pipelineRunRepo := cicdrepo.NewPipelineRunRepository(gdb)
	pipelineDeliveryRepo := cicdrepo.NewPipelineWebhookDeliveryRepository(gdb)

	credSvc := resourceservice.NewCredentialService(credRepo)
	repoSvc := resourceservice.NewRepositoryService(repoRepo, credSvc)
	serverSvc := resourceservice.NewServerService(serverRepo, credSvc)
	cliSvc := resourceservice.NewCLIService(cliRepo, auditSvc)
	patSvc := resourceservice.NewPATService(patRepo, auditSvc)
	projectRepo := projectrepo.NewProjectRepository(gdb)
	jobSvc := cicdservice.NewBuildJobService(jobRepo, repoRepo, projectRepo)
	jobSvc.SetWorkspaceDir(cfg.Build.WorkspaceDir)
	runSvc := cicdservice.NewBuildRunService(runRepo, jobRepo)
	webhookSvc := cicdservice.NewWebhookService(jobRepo, deliveryRepo, runSvc)
	scriptJobSvc := cicdservice.NewScriptJobService(scriptJobRepo, projectRepo)
	scriptJobSvc.SetWorkspaceDir(cfg.Build.WorkspaceDir)
	scriptRunSvc := cicdservice.NewScriptRunService(scriptRunRepo, scriptJobRepo)
	scriptWebhookSvc := cicdservice.NewScriptWebhookService(scriptJobRepo, scriptDeliveryRepo, scriptRunSvc)
	pipelineSvc := cicdservice.NewBuildPipelineService(pipelineRepo, jobRepo, scriptJobRepo, projectRepo)

	dashboardRepo := dashboardrepo.NewDashboardRepository(gdb)
	dashboardSvc := dashboardservice.NewDashboardService(
		dashboardRepo, version, startedAt,
		[]string{cfg.Build.WorkspaceDir, cfg.Build.ArtifactDir, cfg.Build.LogDir, cfg.Build.CacheDir},
	)
	dashboardHandler := dashboardhandler.NewDashboardHandler(dashboardSvc, permSvc)

	opsRepo := opsrepo.NewOpsRepository(gdb)
	processSvc := opsservice.NewProcessService()
	devEnvSvc := opsservice.NewDevEnvironmentService(opsRepo, auditSvc)
	opsHandler := opshandler.NewOpsHandler(processSvc, devEnvSvc, permSvc)

	storageRepo := storagerepo.NewStorageRepository(gdb)
	storageSvc, err := storageservice.NewStorageService(storageRepo, cfg.Storage.Root, storageservice.Limits{
		AttachmentMaxBytes: cfg.Storage.AttachmentMaxBytes,
		DocImportMaxBytes:  cfg.Storage.DocImportMaxBytes,
	})
	if err != nil {
		logger.Fatal("Failed to init storage service", zap.Error(err))
	}
	projectSvc := projectservice.NewProjectService(projectRepo, storageSvc)
	projectHandler := projecthandler.NewProjectHandler(projectSvc, permSvc)

	aiRepo := airepo.NewAIRepository(gdb)
	skillsRoot := filepath.Join(cfg.Storage.Root, "skills")
	skillSvc := aiservice.NewSkillService(aiRepo, storageSvc, skillsRoot, auditSvc)

	hub := ws.NewHub()
	runSvc.SetHub(hub)
	scriptRunSvc.SetHub(hub)
	notifRepo := systemrepo.NewNotificationRepository(gdb)
	notifSvc := systemservice.NewNotificationService(notifRepo, hub)
	notifHandler := systemhandler.NewNotificationHandler(notifSvc)

	agentSvc := aiservice.NewAgentService(aiRepo, cliSvc, skillSvc, hub, logger, agentWorkDir, agentArtifactDir, cfg.Build.LogDir, auditSvc)
	agentSvc.SetDocDraftWriter(projectSvc)
	agentSvc.SetRepoCheckoutDeps(repoRepo, resourceservice.NewCredentialSecretResolver(credSvc))
	agentSvc.SetTerminalNotifier(notifSvc)
	docsBridge := aiservice.NewDocsBridge(agentSvc)
	projectSvc.SetDocsAIBridge(docsBridge)
	aiHandler := aihandler.NewHandler(agentSvc, skillSvc, permSvc)

	pipeline := engine.NewPipeline(
		runRepo, jobRepo, repoRepo, serverRepo,
		resourceservice.NewCredentialSecretResolver(credSvc),
		hub, logger,
		cfg.Build.WorkspaceDir, cfg.Build.ArtifactDir, cfg.Build.LogDir, cfg.Build.CacheDir,
	)
	pipeline.SetAgentEventHook(agentSvc)
	pipeline.SetTerminalNotifier(notifSvc)
	sched := engine.NewScheduler(cfg.Build.MaxConcurrent, pipeline, runRepo, logger)
	runSvc.SetScheduler(sched)
	cronSched := engine.NewCronScheduler(jobRepo, runRepo, runSvc, sched, logger)
	jobSvc.SetCron(cronSched)

	scriptPipeline := engine.NewScriptPipeline(scriptRunRepo, scriptJobRepo, hub, logger, cfg.Build.WorkspaceDir, cfg.Build.LogDir)
	scriptSched := engine.NewScriptScheduler(cfg.Build.MaxConcurrent, scriptPipeline, scriptRunRepo, logger)
	scriptRunSvc.SetScheduler(scriptSched)
	scriptCronSched := engine.NewScriptCronScheduler(scriptJobRepo, scriptRunRepo, scriptRunSvc, scriptSched, logger)
	scriptJobSvc.SetCron(scriptCronSched)

	pipelineOrch := cicdservice.NewPipelineOrchestrator(
		pipelineRepo, pipelineRunRepo, jobRepo, scriptJobRepo, runSvc, scriptRunSvc, agentSvc, logger,
	)
	pipelineOrch.SetHub(hub)
	pipeline.SetBuildRunTerminalHook(pipelineOrch)
	runSvc.SetTerminalHook(pipelineOrch)
	scriptPipeline.SetTerminalHook(pipelineOrch)
	scriptRunSvc.SetTerminalHook(pipelineOrch)
	agentSvc.SetTerminalHook(pipelineOrch)
	pipelineSvc.SetAgentExists(func(id uint) bool {
		_, err := agentSvc.GetAgent(id)
		return err == nil
	})
	pipelineWebhookSvc := cicdservice.NewPipelineWebhookService(pipelineRepo, pipelineDeliveryRepo, pipelineOrch)
	pipelineCronSched := cicdservice.NewPipelineCronScheduler(pipelineRepo, pipelineRunRepo, pipelineOrch, logger)
	pipelineSvc.SetCron(pipelineCronSched)

	credHandler := resourcehandler.NewCredentialHandler(credSvc, permSvc)
	repoHandler := resourcehandler.NewRepositoryHandler(repoSvc, permSvc)
	serverHandler := resourcehandler.NewServerHandler(serverSvc, permSvc)
	cliHandler := resourcehandler.NewCLIHandler(cliSvc, permSvc)
	tokenHandler := resourcehandler.NewTokenHandler(patSvc, permSvc)
	jobHandler := cicdhandler.NewBuildJobHandler(jobSvc, runSvc, permSvc)
	runHandler := cicdhandler.NewBuildRunHandler(runSvc, permSvc)
	scriptJobHandler := cicdhandler.NewScriptJobHandler(scriptJobSvc, scriptRunSvc, permSvc)
	scriptRunHandler := cicdhandler.NewScriptRunHandler(scriptRunSvc, permSvc)
	webhookHandler := cicdhandler.NewWebhookHandler(webhookSvc)
	webhookHandler.SetScriptWebhook(scriptWebhookSvc)
	webhookHandler.SetPipelineWebhook(pipelineWebhookSvc)
	buildPipelineHandler := cicdhandler.NewBuildPipelineHandler(pipelineSvc, pipelineOrch, permSvc)
	pipelineRunHandler := cicdhandler.NewPipelineRunHandler(pipelineOrch, permSvc)

	r := gin.Default()
	r.Use(gzip.Gzip(gzip.DefaultCompression))
	corsCfg := middleware.DefaultCORSConfig()
	r.Use(middleware.CORSGin(corsCfg))

	api := r.Group("/api/v1")
	api.Use(systemmw.AuditWrite(auditSvc))
	authMW := authmiddleware.AuthWithPAT(authSvc, patSvc)
	authHandler.RegisterRoutes(api, authMW)
	userHandler.RegisterRoutes(api, authMW)
	roleHandler.RegisterRoutes(api, authMW)
	resourceHandler.RegisterRoutes(api, authMW)
	dictHandler.RegisterRoutes(api, authMW)
	logHandler.RegisterRoutes(api, authMW)
	credHandler.RegisterRoutes(api, authMW)
	repoHandler.RegisterRoutes(api, authMW)
	serverHandler.RegisterRoutes(api, authMW)
	cliHandler.RegisterRoutes(api, authMW)
	tokenHandler.RegisterRoutes(api, authMW)
	jobHandler.RegisterRoutes(api, authMW)
	runHandler.RegisterRoutes(api, authMW)
	scriptJobHandler.RegisterRoutes(api, authMW)
	scriptRunHandler.RegisterRoutes(api, authMW)
	buildPipelineHandler.RegisterRoutes(api, authMW)
	pipelineRunHandler.RegisterRoutes(api, authMW)
	webhookHandler.RegisterRoutes(api)
	dashboardHandler.RegisterRoutes(api, authMW)
	opsHandler.RegisterRoutes(api, authMW)
	projectHandler.RegisterRoutes(api, authMW)
	aiHandler.RegisterRoutes(api, authMW)
	notifHandler.RegisterRoutes(api, authMW)

	api.GET("/health", func(c *gin.Context) {
		pkg.Success(c, gin.H{
			"status":  "ok",
			"version": version,
			"driver":  cfg.Database.Driver,
		})
	})

	wsHandler := cicdhandler.NewWSHandler(authSvc, permSvc, runSvc, hub, corsCfg)
	wsHandler.SetScriptRuns(scriptRunSvc)
	wsHandler.RegisterRoutes(r)
	aiWSHandler := aihandler.NewWSHandler(authSvc, permSvc, agentSvc, hub, corsCfg)
	aiWSHandler.RegisterRoutes(r)
	notifWSHandler := systemhandler.NewNotificationWSHandler(authSvc, hub, corsCfg)
	notifWSHandler.RegisterRoutes(r)
	dashboardWSHandler := dashboardhandler.NewDashboardWSHandler(authSvc, patSvc, permSvc, hub, corsCfg)
	dashboardWSHandler.RegisterRoutes(r)

	statusBroadcasterCtx, cancelStatusBroadcaster := context.WithCancel(context.Background())
	dashboardSvc.StartStatusBroadcaster(statusBroadcasterCtx, hub, 3*time.Second)

	serveSPA(r, cfg.Encryption.Key)

	for _, dir := range []string{cfg.Build.WorkspaceDir, cfg.Build.ArtifactDir, cfg.Build.LogDir, cfg.Build.CacheDir} {
		if dir != "" {
			_ = os.MkdirAll(dir, 0o755)
		}
	}

	sched.Start()
	scriptSched.Start()
	devEnvSvc.Start()
	agentSvc.Start()
	if err := sched.RecoverOnStartup(); err != nil {
		logger.Error("scheduler recovery failed", zap.Error(err))
	}
	if err := scriptSched.RecoverOnStartup(); err != nil {
		logger.Error("script scheduler recovery failed", zap.Error(err))
	}
	if err := devEnvSvc.RecoverOnStartup(); err != nil {
		logger.Error("dev environment scheduler recovery failed", zap.Error(err))
	}
	if err := agentSvc.RecoverOnStartup(); err != nil {
		logger.Error("agent run recovery failed", zap.Error(err))
	}
	if err := cronSched.Start(); err != nil {
		logger.Error("cron start failed", zap.Error(err))
	}
	if err := scriptCronSched.Start(); err != nil {
		logger.Error("script cron start failed", zap.Error(err))
	}
	if _, err := pipelineRunRepo.MarkRunningInterrupted(); err != nil {
		logger.Error("pipeline run recovery failed", zap.Error(err))
	}
	if err := pipelineCronSched.Start(); err != nil {
		logger.Error("pipeline cron start failed", zap.Error(err))
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	go func() {
		logger.Info("listening", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down...")

	cancelStatusBroadcaster()
	cronSched.Stop()
	scriptCronSched.Stop()
	pipelineCronSched.Stop()
	sched.Shutdown()
	scriptSched.Shutdown()
	devEnvSvc.Shutdown()
	agentSvc.Shutdown()
	hub.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("HTTP server forced shutdown", zap.Error(err))
	}
	if sqlDB, err := gdb.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
