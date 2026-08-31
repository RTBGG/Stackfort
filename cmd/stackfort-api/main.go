// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/RTBGG/stackfort/internal/accountprovisioning"
	"github.com/RTBGG/stackfort/internal/acmeaccounts"
	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/backupworkspace"
	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/cacheworkspace"
	certificateapp "github.com/RTBGG/stackfort/internal/certificates"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/databaseworkspace"
	"github.com/RTBGG/stackfort/internal/domainlifecycle"
	"github.com/RTBGG/stackfort/internal/fileworkspace"
	"github.com/RTBGG/stackfort/internal/httpapi"
	"github.com/RTBGG/stackfort/internal/jobworkspace"
	"github.com/RTBGG/stackfort/internal/logworkspace"
	"github.com/RTBGG/stackfort/internal/operations"
	"github.com/RTBGG/stackfort/internal/phpmyadminbroker"
	"github.com/RTBGG/stackfort/internal/phpworkspace"
	"github.com/RTBGG/stackfort/internal/secretstore"
	"github.com/RTBGG/stackfort/internal/store"
)

const (
	defaultAddress   = "127.0.0.1:8080"
	defaultStatePath = "/var/lib/stackfort/stackfort.db"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if len(os.Args) > 1 {
		commandCtx, cancelCommand := context.WithTimeout(context.Background(), 30*time.Second)
		err := runCommand(commandCtx, os.Args[1:], os.Stdout)
		cancelCommand()
		if err != nil {
			logger.Error("stackfort-api command failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := run(logger); err != nil {
		logger.Error("stackfort-api stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) (returnErr error) {
	address := os.Getenv("STACKFORT_API_ADDRESS")
	if address == "" {
		address = defaultAddress
	}

	databasePath, err := panelStatePath()
	if err != nil {
		return err
	}
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	state, err := store.Open(startupCtx, databasePath)
	cancelStartup()
	if err != nil {
		return fmt.Errorf("open panel state: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, state.Close())
	}()
	masterKey, err := secretstore.LoadOrCreateMasterKey(masterKeyPath(databasePath))
	if err != nil {
		return fmt.Errorf("load host master key: %w", err)
	}
	repository, err := core.NewRepositoryWithMasterKey(state, masterKey[:])
	clear(masterKey[:])
	if err != nil {
		return fmt.Errorf("initialize control-plane repository: %w", err)
	}
	recoveryCtx, cancelRecovery := context.WithTimeout(context.Background(), 30*time.Second)
	recoveredOperations, err := repository.RecoverExpiredOperations(recoveryCtx)
	cancelRecovery()
	if err != nil {
		return fmt.Errorf("recover expired operations: %w", err)
	}
	if recoveredOperations > 0 {
		logger.Info("recovered expired operations", "count", recoveredOperations)
	}
	domainService, err := domainlifecycle.New(repository)
	if err != nil {
		return fmt.Errorf("initialize domain lifecycle service: %w", err)
	}
	accountProvisioningService, err := accountprovisioning.New(repository)
	if err != nil {
		return fmt.Errorf("initialize account provisioning service: %w", err)
	}
	acmeAccountService, err := acmeaccounts.New(repository)
	if err != nil {
		return fmt.Errorf("initialize ACME account service: %w", err)
	}
	certificateService, err := certificateapp.New(repository)
	if err != nil {
		return fmt.Errorf("initialize TLS certificate service: %w", err)
	}
	databaseWorkspaceService, err := databaseworkspace.New(repository)
	if err != nil {
		return fmt.Errorf("initialize database workspace service: %w", err)
	}
	jobWorkspaceService, err := jobworkspace.New(repository)
	if err != nil {
		return fmt.Errorf("initialize scheduled job workspace service: %w", err)
	}
	var hostCapabilityClient *agentclient.Client
	var phpWorkspaceService *phpworkspace.Service
	var fileWorkspaceService *fileworkspace.Service
	var backupWorkspaceService *backupworkspace.Service
	var logWorkspaceService *logworkspace.Service
	var cacheWorkspaceService *cacheworkspace.Service
	var phpMyAdminBrokerListener net.Listener
	var phpMyAdminBrokerServer *http.Server
	if runtime.GOOS == "linux" {
		hostCapabilityClient, err = agentclient.New(agentprotocol.DefaultSocketPath)
		if err != nil {
			return fmt.Errorf("initialize local agent client: %w", err)
		}
		defer hostCapabilityClient.Close()
		phpWorkspaceService, err = phpworkspace.New(repository, hostCapabilityClient)
		if err != nil {
			return fmt.Errorf("initialize PHP workspace service: %w", err)
		}
		fileWorkspaceService, err = fileworkspace.New(repository, hostCapabilityClient)
		if err != nil {
			return fmt.Errorf("initialize file workspace service: %w", err)
		}
		backupWorkspaceService, err = backupworkspace.New(repository, hostCapabilityClient)
		if err != nil {
			return fmt.Errorf("initialize backup workspace service: %w", err)
		}
		logWorkspaceService, err = logworkspace.New(repository, hostCapabilityClient)
		if err != nil {
			return fmt.Errorf("initialize log workspace service: %w", err)
		}
		cacheWorkspaceService, err = cacheworkspace.New(repository, hostCapabilityClient)
		if err != nil {
			return fmt.Errorf("initialize cache workspace service: %w", err)
		}
		brokerKey, keyErr := phpmyadminbroker.LoadSharedKey(phpmyadminbroker.DefaultKeyPath)
		if keyErr != nil {
			return fmt.Errorf("load phpMyAdmin broker key: %w", keyErr)
		}
		brokerHandler, handlerErr := phpmyadminbroker.New(repository, brokerKey[:])
		clear(brokerKey[:])
		if handlerErr != nil {
			return fmt.Errorf("initialize phpMyAdmin broker: %w", handlerErr)
		}
		phpMyAdminBrokerListener, err = net.Listen("tcp4", phpmyadminbroker.DefaultAddress)
		if err != nil {
			return fmt.Errorf("listen for phpMyAdmin broker: %w", err)
		}
		defer phpMyAdminBrokerListener.Close()
		phpMyAdminBrokerServer = &http.Server{
			Handler: brokerHandler, ReadHeaderTimeout: 2 * time.Second,
			ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second,
			IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8 << 10,
		}
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()

	server := &http.Server{
		Handler: httpapi.NewWithServices(logger, state, httpapi.Services{
			Bootstrap: repository, Authentication: repository, Authorization: repository,
			PlatformAuthorization: repository, HostCapabilities: hostCapabilityClient,
			MultiFactor: repository, Sessions: repository, Domains: domainService,
			ACMEAccounts: acmeAccountService, TLSCertificates: certificateService,
			AdminConsole: repository, AccountProvisioning: accountProvisioningService,
			SelfService:       repository,
			PHPWorkspace:      phpWorkspaceService,
			DatabaseWorkspace: databaseWorkspaceService,
			FileWorkspace:     fileWorkspaceService,
			BackupWorkspace:   backupWorkspaceService,
			LogWorkspace:      logWorkspaceService,
			CacheWorkspace:    cacheWorkspaceService,
			ScheduledJobs:     jobWorkspaceService,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	acmeHandler, err := operations.NewACMEAccountRegistrationHandler(repository, operations.RFC8555Registrar{})
	if err != nil {
		return fmt.Errorf("initialize ACME account operation handler: %w", err)
	}
	handlers := map[string]operations.Handler{
		operations.ACMEAccountRegistrationKind: acmeHandler,
	}
	if hostCapabilityClient != nil {
		accountHandler, handlerErr := operations.NewHostingAccountReconcileHandler(repository, hostCapabilityClient)
		if handlerErr != nil {
			return fmt.Errorf("initialize hosting account operation handler: %w", handlerErr)
		}
		handlers[operations.HostingAccountReconcileKind] = accountHandler
		nginxHandler, handlerErr := operations.NewNGINXActivationHandler(repository, hostCapabilityClient)
		if handlerErr != nil {
			return fmt.Errorf("initialize NGINX operation handler: %w", handlerErr)
		}
		domainHandler, handlerErr := operations.NewDomainLifecycleHandler(repository, hostCapabilityClient)
		if handlerErr != nil {
			return fmt.Errorf("initialize domain operation handler: %w", handlerErr)
		}
		handlers[operations.NGINXActivationKind] = nginxHandler
		handlers[operations.DomainLifecycleKind] = domainHandler
		certificateHandler, handlerErr := operations.NewTLSCertificateLifecycleHandler(
			repository, hostCapabilityClient, operations.RFC8555Issuer{},
		)
		if handlerErr != nil {
			return fmt.Errorf("initialize TLS certificate operation handler: %w", handlerErr)
		}
		handlers[operations.TLSCertificateLifecycleKind] = certificateHandler
		databaseHandler, handlerErr := operations.NewDatabaseLifecycleHandler(repository, hostCapabilityClient)
		if handlerErr != nil {
			return fmt.Errorf("initialize database lifecycle operation handler: %w", handlerErr)
		}
		handlers[operations.DatabaseLifecycleKind] = databaseHandler
		jobHandler, handlerErr := operations.NewScheduledJobLifecycleHandler(repository, hostCapabilityClient)
		if handlerErr != nil {
			return fmt.Errorf("initialize scheduled job lifecycle operation handler: %w", handlerErr)
		}
		handlers[operations.ScheduledJobLifecycleKind] = jobHandler
		cachePurgeHandler, handlerErr := operations.NewCachePurgeHandler(repository, hostCapabilityClient)
		if handlerErr != nil {
			return fmt.Errorf("initialize cache purge operation handler: %w", handlerErr)
		}
		handlers[operations.CachePurgeKind] = cachePurgeHandler
		ociImageHandler, handlerErr := operations.NewOCIImagePrepareHandler(repository, hostCapabilityClient)
		if handlerErr != nil {
			return fmt.Errorf("initialize OCI image operation handler: %w", handlerErr)
		}
		handlers[operations.OCIImagePrepareKind] = ociImageHandler
	}
	runner, err := operations.NewRunner(repository, handlers, operations.RunnerOptions{})
	if err != nil {
		return fmt.Errorf("initialize operation runner: %w", err)
	}
	go runOperationWorker(ctx, logger, runner)
	if hostCapabilityClient != nil {
		go runAccountProvisioningScheduler(ctx, logger, accountProvisioningService)
		go runTLSCertificateScheduler(ctx, logger, certificateService)
	}

	serverErrors := make(chan error, 2)
	go func() {
		logger.Info("stackfort-api listening", "address", listener.Addr().String(), "build", buildinfo.Current())
		serverErrors <- server.Serve(listener)
	}()
	if phpMyAdminBrokerServer != nil {
		go func() {
			logger.Info("phpMyAdmin credential broker listening", "address", phpMyAdminBrokerListener.Addr().String())
			serverErrors <- phpMyAdminBrokerServer.Serve(phpMyAdminBrokerListener)
		}()
	}

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var brokerShutdownError error
		if phpMyAdminBrokerServer != nil {
			brokerShutdownError = phpMyAdminBrokerServer.Shutdown(shutdownCtx)
		}
		return errors.Join(server.Shutdown(shutdownCtx), brokerShutdownError)
	}
}

func runOperationWorker(ctx context.Context, logger *slog.Logger, runner *operations.Runner) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		err := runner.RunOnce(ctx)
		delay := 25 * time.Millisecond
		switch {
		case err == nil:
		case errors.Is(err, core.ErrNoOperationAvailable):
			delay = 500 * time.Millisecond
		case errors.Is(err, context.Canceled):
			return
		default:
			var runError *operations.RunError
			if errors.As(err, &runError) {
				logger.Warn("operation attempt finished with a stable failure", "operationId", runError.OperationID, "code", runError.Code)
			} else {
				logger.Error("operation worker iteration failed", "error", err)
				delay = time.Second
			}
		}
		timer.Reset(delay)
	}
}

func runAccountProvisioningScheduler(
	ctx context.Context,
	logger *slog.Logger,
	service *accountprovisioning.Service,
) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		queued, err := service.QueuePending(ctx, 100)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("account provisioning scheduler iteration failed", "error", err)
		} else if queued > 0 {
			logger.Info("ensured pending account provisioning work", "accounts", queued)
		}
		timer.Reset(time.Minute)
	}
}

func runTLSCertificateScheduler(
	ctx context.Context,
	logger *slog.Logger,
	service *certificateapp.Service,
) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		queued, err := service.QueueAutomaticWork(ctx, 100)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("TLS certificate scheduler iteration failed", "error", err)
		} else if queued > 0 {
			logger.Info("queued automatic TLS certificate work", "count", queued)
		}
		timer.Reset(time.Minute)
	}
}

func panelStatePath() (string, error) {
	if configured := os.Getenv("STACKFORT_STATE_PATH"); configured != "" {
		return configured, nil
	}
	if runtime.GOOS != "windows" {
		return defaultStatePath, nil
	}

	configurationDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve development state directory: %w", err)
	}
	return filepath.Join(configurationDirectory, "Stackfort", "stackfort.db"), nil
}

func masterKeyPath(databasePath string) string {
	if configured := os.Getenv("STACKFORT_MASTER_KEY_PATH"); configured != "" {
		return configured
	}
	return filepath.Join(filepath.Dir(databasePath), "master.key")
}
