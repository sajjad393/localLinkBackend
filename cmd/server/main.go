package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"locallink/backend/internal/discovery"
	"locallink/backend/internal/httpapi"
	"locallink/backend/internal/store"
)

func main() {

	if len(os.Args) > 1 && os.Args[1] == "--reset-admin" {
		if err := resetAdminCredentials(); err != nil {
			log.Fatal(err)
		}
		log.Println("admin credentials reset successfully")
		return
	}

	addr := getenv("LOCALLINK_ADDR", ":8080")
	dbPath := getenv("LOCALLINK_DB", "data/locallink.db")

	if err := validateProductionSecrets(); err != nil {
		log.Fatal(err)
	}

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	api, err := httpapi.NewWithError(st)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.Health)
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if err := st.DB().PingContext(r.Context()); err != nil {
			writeRuntimeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "ready": false})
			return
		}
		writeRuntimeJSON(w, http.StatusOK, map[string]any{"ok": true, "ready": true})
	})
	mux.HandleFunc("GET /api/v1/status", api.Status)
	mux.HandleFunc("GET /api/v1/pairing/info", api.PairingInfo)
	mux.HandleFunc("POST /api/v1/auth/register", api.RegisterAccount)
	mux.HandleFunc("POST /api/v1/auth/login", api.LoginAccount)
	mux.HandleFunc("GET /api/v1/auth/me", api.AuthMe)
	mux.HandleFunc("POST /api/v1/auth/logout", api.LogoutAccount)
	mux.HandleFunc("POST /api/v1/admin/login", api.AdminLogin)
	mux.HandleFunc("GET /admin", api.AdminWeb)
	mux.HandleFunc("GET /admin/", api.AdminWeb)
	mux.Handle("POST /api/v1/account/transfer/start", api.WithIdempotency("account-transfer-start", http.HandlerFunc(api.StartAccountTransfer)))
	mux.Handle("POST /api/v1/account/transfer/cancel", api.WithIdempotency("account-transfer-cancel", http.HandlerFunc(api.CancelAccountTransfer)))
	mux.Handle("POST /api/v1/account/transfer/complete", api.WithIdempotency("account-transfer-complete", http.HandlerFunc(api.CompleteAccountTransfer)))
	mux.Handle("POST /api/v1/account/recovery/request", api.WithIdempotency("account-recovery-request", http.HandlerFunc(api.CreateAccountRecovery)))
	mux.HandleFunc("POST /api/v1/account/recovery/status", api.AccountRecoveryStatus)
	mux.Handle("POST /api/v1/account/recovery/complete", api.WithIdempotency("account-recovery-complete", http.HandlerFunc(api.CompleteAccountRecovery)))
	mux.HandleFunc("GET /api/v1/account/recovery-code/status", api.RecoveryCodeStatus)
	mux.HandleFunc("GET /api/v1/account/restore/manifest", api.GetAccountRestoreManifest)
	mux.HandleFunc("GET /api/v1/account/restore/data", api.GetAccountRestoreData)
	mux.HandleFunc("GET /api/v1/account/crypto-backup", api.GetCryptoBackup)
	mux.HandleFunc("PUT /api/v1/account/crypto-backup", api.PutCryptoBackup)
	mux.HandleFunc("DELETE /api/v1/account/crypto-backup", api.DeleteCryptoBackup)
	mux.Handle("POST /api/v1/account/recovery-code/rotate", api.WithIdempotency("recovery-code-rotate", http.HandlerFunc(api.RotateRecoveryCode)))
	mux.HandleFunc("POST /api/v1/account/recovery-code/disable", api.DisableRecoveryCode)
	mux.HandleFunc("GET /api/v1/profile", api.GetProfile)
	mux.HandleFunc("PUT /api/v1/profile", api.UpdateProfile)
	mux.HandleFunc("POST /api/v1/profile/avatar", api.UploadProfileAvatar)
	mux.HandleFunc("GET /api/v1/profile/avatar", api.GetProfileAvatar)
	mux.HandleFunc("GET /api/v1/directory/phone", api.DirectoryByPhone)
	mux.HandleFunc("GET /api/v1/directory/name", api.DirectoryByName)
	mux.HandleFunc("GET /api/v1/directory/user/{id}", api.DirectoryByUserID)
	mux.HandleFunc("POST /api/v1/devices", api.RegisterDevice)
	mux.HandleFunc("GET /api/v1/devices", api.ListDevices)
	mux.HandleFunc("GET /api/v1/account/devices", api.ListAccountDevices)
	mux.HandleFunc("POST /api/v1/account/devices/revoke", api.RevokeAccountDevice)
	mux.HandleFunc("GET /api/v1/device-keys", api.IdentityKeys)
	mux.HandleFunc("GET /api/v1/device-key/history", api.IdentityKeyHistory)
	mux.HandleFunc("GET /api/v1/device-keys/history", api.AllIdentityKeyHistory)
	mux.HandleFunc("PUT /api/v1/device-key", api.PutIdentityKey)
	mux.Handle("POST /api/v1/device-key/rotate", api.WithIdempotency("identity-key-rotate", http.HandlerFunc(api.RotateIdentityKey)))
	mux.HandleFunc("GET /api/v1/calls", api.ListCalls)
	mux.Handle("POST /api/v1/calls/reconcile", api.WithIdempotency("call-reconcile", http.HandlerFunc(api.ReconcileCall)))
	mux.HandleFunc("GET /api/v1/admin/summary", api.AdminSummary)
	mux.HandleFunc("GET /api/v1/admin/users", api.AdminUsers)
	mux.HandleFunc("POST /api/v1/admin/user/status", api.AdminSetUserStatus)
	mux.HandleFunc("GET /api/v1/admin/user/network-policy", api.AdminGetUserNetworkPolicy)
	mux.HandleFunc("POST /api/v1/admin/user/network-policy", api.AdminUpdateUserNetworkPolicy)
	mux.HandleFunc("GET /api/v1/admin/sessions", api.AdminSessions)
	mux.HandleFunc("POST /api/v1/admin/session/revoke", api.AdminRevokeSession)
	mux.HandleFunc("GET /api/v1/admin/admin-sessions", api.AdminOwnSessions)
	mux.HandleFunc("POST /api/v1/admin/admin-session/revoke", api.AdminRevokeAdminSession)
	mux.HandleFunc("GET /api/v1/admin/transfers", api.AdminTransfers)
	mux.HandleFunc("GET /api/v1/admin/network", api.AdminNetwork)
	mux.HandleFunc("GET /api/v1/admin/settings", api.AdminGetSettings)
	mux.HandleFunc("POST /api/v1/admin/settings", api.AdminUpdateSettings)
	mux.HandleFunc("GET /api/v1/admin/privacy", api.AdminPrivacyPolicy)
	mux.HandleFunc("GET /api/v1/admin/devices", api.AdminDevices)
	mux.HandleFunc("GET /api/v1/admin/groups", api.AdminGroups)
	// Conversation review is owner-only and audited; see admin_conversations.go.
	mux.HandleFunc("GET /api/v1/admin/conversations", api.AdminConversations)
	mux.HandleFunc("GET /api/v1/admin/conversation", api.AdminConversation)
	mux.HandleFunc("GET /api/v1/admin/group-conversation", api.AdminGroupConversation)
	mux.HandleFunc("GET /api/v1/admin/calls", api.AdminCalls)
	mux.HandleFunc("GET /api/v1/admin/logs", api.AdminLogs)
	mux.HandleFunc("GET /api/v1/admin/security-events", api.AdminSecurityEvents)
	mux.HandleFunc("GET /api/v1/admin/database/integrity", api.DatabaseIntegrity)
	mux.HandleFunc("GET /api/v1/admin/production-config", api.ProductionConfig)
	mux.HandleFunc("GET /api/v1/admin/runtime-diagnostics", api.RuntimeDiagnostics)
	mux.HandleFunc("GET /api/v1/admin/analytics", api.AdminAnalytics)
	mux.HandleFunc("GET /api/v1/admin/health", api.AdminHealth)
	mux.HandleFunc("GET /api/v1/admin/security-summary", api.AdminSecuritySummary)
	mux.HandleFunc("GET /api/v1/admin/activity", api.AdminActivity)
	mux.HandleFunc("GET /api/v1/admin/recovery-requests", api.AdminRecoveryRequests)
	mux.Handle("POST /api/v1/admin/recovery-requests/approve", api.WithIdempotency("admin-recovery-approve", http.HandlerFunc(api.AdminApproveRecovery)))
	mux.Handle("POST /api/v1/admin/recovery-requests/reissue", api.WithIdempotency("admin-recovery-reissue", http.HandlerFunc(api.AdminReissueRecovery)))
	mux.Handle("POST /api/v1/admin/recovery-requests/reject", api.WithIdempotency("admin-recovery-reject", http.HandlerFunc(api.AdminRejectRecovery)))
	mux.HandleFunc("POST /api/v1/admin/device/rename", api.AdminRenameDevice)
	mux.HandleFunc("POST /api/v1/admin/device/revoke", api.AdminRevokeDevice)
	mux.HandleFunc("GET /api/v1/admin/device/network-policy", api.AdminGetDeviceNetworkPolicy)
	mux.HandleFunc("POST /api/v1/admin/device/network-policy", api.AdminUpdateDeviceNetworkPolicy)
	mux.HandleFunc("GET /api/v1/device/network-policy", api.DeviceGetNetworkPolicy)
	mux.HandleFunc("POST /api/v1/admin/backup", api.AdminBackup)
	mux.HandleFunc("POST /api/v1/admin/logout", api.AdminLogout)
	mux.HandleFunc("GET /api/v1/groups", api.ListGroups)
	mux.Handle("POST /api/v1/groups", api.WithIdempotency("group-create", http.HandlerFunc(api.CreateGroup)))
	mux.HandleFunc("GET /api/v1/groups/members", api.GroupMembers)
	mux.HandleFunc("POST /api/v1/groups/members", api.AddGroupMember)
	mux.HandleFunc("DELETE /api/v1/groups/members", api.RemoveGroupMember)
	mux.HandleFunc("GET /api/v1/group-messages", api.ListGroupMessages)
	mux.HandleFunc("POST /api/v1/group-messages", api.CreateGroupMessage)
	mux.HandleFunc("POST /api/v1/messages", api.CreateMessage)
	mux.HandleFunc("GET /api/v1/messages", api.ListMessages)
	mux.HandleFunc("GET /api/v1/sync", api.Sync)
	mux.HandleFunc("POST /api/v1/messages/ack", api.MessageAck)
	mux.HandleFunc("POST /api/v1/files", api.UploadFile)
	mux.HandleFunc("GET /api/v1/files/{id}", api.FileMetadata)
	mux.HandleFunc("GET /api/v1/files/{id}/content", api.DownloadFile)
	mux.HandleFunc("GET /api/v1/files/{id}/thumbnail", api.DownloadThumbnail)
	mux.HandleFunc("GET /ws", api.WebSocket)

	// Apply a defensive generic request-body cap to JSON/form requests.
	// Multipart upload handlers enforce their own larger file-specific limits.
	limitedHandler := limitRequestBody(mux)
	metrics := newRuntimeMetrics()
	api.SetRuntimeDiagnosticsProvider(metrics.snapshot)

	srv := &http.Server{
		Addr:              addr,
		Handler:           requestLogging(metrics, requestRecovery(metrics, limitedHandler)),
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	if err := api.CleanupSecurityArtifacts(); err != nil {
		log.Printf("security cleanup error: %v", err)
	}
	if err := api.CleanupProductionArtifacts(); err != nil {
		log.Printf("production artifact cleanup error: %v", err)
	}

	orphanStop := make(chan struct{})
	go func() {
		if removed, err := api.CleanupOrphanFiles(24 * time.Hour); err != nil {
			log.Printf("attachment cleanup error: %v", err)
		} else if removed > 0 {
			log.Printf("attachment cleanup removed %d orphaned files", removed)
		}
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := api.CleanupProductionArtifacts(); err != nil {
					log.Printf("production artifact cleanup error: %v", err)
				}
				if err := api.CleanupSecurityArtifacts(); err != nil {
					log.Printf("security cleanup error: %v", err)
				}
				if removed, err := api.CleanupOrphanFiles(24 * time.Hour); err != nil {
					log.Printf("attachment cleanup error: %v", err)
				} else if removed > 0 {
					log.Printf("attachment cleanup removed %d orphaned files", removed)
				}
			case <-orphanStop:
				return
			}
		}
	}()

	callStop := make(chan struct{})
	go func() {
		if calls, err := api.CleanupStaleCalls(60*time.Second, 60*time.Second); err != nil {
			log.Printf("call cleanup error: %v", err)
		} else if calls > 0 {
			log.Printf("call cleanup ended %d stale calls", calls)
		}
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if calls, err := api.CleanupStaleCalls(60*time.Second, 60*time.Second); err != nil {
					log.Printf("call cleanup error: %v", err)
				} else if calls > 0 {
					log.Printf("call cleanup ended %d stale calls", calls)
				}
			case <-callStop:
				return
			}
		}
	}()

	stopDiscovery, err := discovery.Start(addr, getenv("LOCALLINK_NAME", "LocalLink Server"), 8080)
	if err != nil {
		log.Printf("LAN discovery disabled: %v", err)
	} else {
		defer stopDiscovery()
	}

	certFile := getenv("LOCALLINK_TLS_CERT", "")
	keyFile := getenv("LOCALLINK_TLS_KEY", "")
	requireTLS := strings.EqualFold(strings.TrimSpace(getenv("LOCALLINK_REQUIRE_TLS", "true")), "true")
	if !requireTLS {
		envName := strings.ToLower(strings.TrimSpace(getenv("LOCALLINK_ENV", "production")))
		if envName != "development" && envName != "test" {
			log.Printf("SECURITY WARNING: LOCALLINK_REQUIRE_TLS is disabled outside development/test")
		}
	}
	if (certFile == "") != (keyFile == "") {
		log.Fatal("LOCALLINK_TLS_CERT and LOCALLINK_TLS_KEY must be set together")
	}
	if requireTLS && (certFile == "" || keyFile == "") {
		log.Fatal("LOCALLINK_REQUIRE_TLS=true requires LOCALLINK_TLS_CERT and LOCALLINK_TLS_KEY")
	}

	go func() {
		if certFile != "" && keyFile != "" {
			log.Printf("LocalLink server listening with TLS on %s", addr)
			if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
				log.Printf("server error: %v", err)
			}
			return
		}
		log.Printf("LocalLink server listening without TLS on %s (development mode)", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	close(orphanStop)
	close(callStop)
	log.Printf("runtime summary requests=%d 2xx=%d 4xx=%d 5xx=%d panics=%d", metrics.requests.Load(), metrics.successes.Load(), metrics.clientErrs.Load(), metrics.serverErrs.Load(), metrics.panics.Load())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func resetAdminCredentials() error {
	envName := strings.ToLower(strings.TrimSpace(getenv("LOCALLINK_ENV", "production")))
	if envName != "development" && envName != "test" {
		return fmt.Errorf("--reset-admin is allowed only in development/test")
	}

	dbPath := getenv("LOCALLINK_DB", "data/locallink.db")

	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	api, err := httpapi.NewWithError(st)
	if err != nil {
		return err
	}

	return api.ResetAdminCredentials()
}

func validateProductionSecrets() error {
	envName := strings.ToLower(strings.TrimSpace(getenv("LOCALLINK_ENV", "production")))
	if envName == "development" || envName == "test" {
		return nil
	}
	if user := strings.TrimSpace(os.Getenv("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME")); user != "" && len(user) > 128 {
		return fmt.Errorf("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME is too long")
	}
	password := os.Getenv("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD")
	if strings.TrimSpace(os.Getenv("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME")) != "" || password != "" {
		if strings.TrimSpace(os.Getenv("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME")) == "" || password == "" {
			return fmt.Errorf("admin bootstrap username and password must be configured together")
		}
		if len(password) < 16 {
			return fmt.Errorf("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD must be at least 16 characters outside development/test")
		}
		if password == "change-this-to-a-strong-password" {
			return fmt.Errorf("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD must not use the example placeholder")
		}
	}
	pairing := strings.TrimSpace(os.Getenv("LOCALLINK_PAIRING_CODE"))
	if pairing == "" {
		return fmt.Errorf("LOCALLINK_PAIRING_CODE is required outside development/test")
	}
	if len(pairing) < 16 {
		return fmt.Errorf("LOCALLINK_PAIRING_CODE must be at least 16 characters outside development/test")
	}
	if strings.EqualFold(pairing, "change-this-to-a-strong-password") {
		return fmt.Errorf("LOCALLINK_PAIRING_CODE must not use an example placeholder")
	}
	requireTLS := strings.EqualFold(strings.TrimSpace(getenv("LOCALLINK_REQUIRE_TLS", "true")), "true")
	if !requireTLS {
		return fmt.Errorf("LOCALLINK_REQUIRE_TLS must remain true in production")
	}
	certFile := strings.TrimSpace(os.Getenv("LOCALLINK_TLS_CERT"))
	keyFile := strings.TrimSpace(os.Getenv("LOCALLINK_TLS_KEY"))
	if (certFile == "") != (keyFile == "") {
		return fmt.Errorf("LOCALLINK_TLS_CERT and LOCALLINK_TLS_KEY must be set together in production")
	}
	if certFile == "" || keyFile == "" {
		return fmt.Errorf("production TLS requires LOCALLINK_TLS_CERT and LOCALLINK_TLS_KEY")
	}
	return nil
}

const maxGenericRequestBody = 2 << 20

func hasWebSocketUpgrade(r *http.Request) bool {
	for _, value := range r.Header.Values("Upgrade") {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "websocket") {
				return true
			}
		}
	}
	return false
}

func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead && !hasWebSocketUpgrade(r) {
			contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
			if contentType != "multipart/form-data" {
				r.Body = http.MaxBytesReader(w, r.Body, maxGenericRequestBody)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
