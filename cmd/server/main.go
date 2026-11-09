package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"locallink/backend/internal/discovery"
	"locallink/backend/internal/httpapi"
	"locallink/backend/internal/store"
	"locallink/backend/internal/tlsauto"
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
	// Bundle 222: Forgot password with the recovery code (online + offline sync).
	mux.HandleFunc("POST /api/v1/auth/join-relay", api.JoinRelay222)
	mux.Handle("POST /api/v1/account/recovery/reset", api.WithIdempotency("account-recovery-reset", http.HandlerFunc(api.ResetPasswordWithCode222)))
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
	mux.HandleFunc("DELETE /api/v1/profile/avatar", api.DeleteProfileAvatar)   // Bundle 204
	mux.HandleFunc("GET /api/v1/directory/user/{id}/avatar", api.FriendAvatar) // Bundle 204
	mux.HandleFunc("GET /api/v1/directory/phone", api.DirectoryByPhone)
	mux.HandleFunc("GET /api/v1/directory/name", api.DirectoryByName)
	mux.HandleFunc("GET /api/v1/directory/user/{id}", api.DirectoryByUserID)
	mux.HandleFunc("POST /api/v1/devices", api.RegisterDevice)
	mux.HandleFunc("GET /api/v1/devices", api.ListDevices)
	mux.HandleFunc("GET /api/v1/account/contacts", api.ListAccountContacts)
	mux.HandleFunc("POST /api/v1/account/contacts", api.AddAccountContact)
	mux.HandleFunc("POST /api/v1/users/resolve", api.ResolveUsers218)
	mux.HandleFunc("DELETE /api/v1/account/contacts/{user_id}", api.RemoveAccountContact)
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
	mux.HandleFunc("GET /api/v1/admin/users/{id}/avatar", api.AdminUserAvatar)
	mux.HandleFunc("GET /api/v1/admin/link-map", api.AdminLinkMap)
	mux.HandleFunc("POST /api/v1/admin/user/status", api.AdminSetUserStatus)
	mux.HandleFunc("GET /api/v1/admin/duplicates", api.AdminDuplicates235)
	mux.HandleFunc("POST /api/v1/admin/duplicates/new-owner", api.AdminDuplicateNewOwner235)
	mux.HandleFunc("POST /api/v1/admin/duplicates/dismiss", api.AdminDuplicateDismiss235)
	mux.HandleFunc("POST /api/v1/account/return-note", api.ReturnNote235)
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
	// Owner-only, audit-logged media streaming for the conversation console;
	// it never reuses the user file routes, whose bearer it does not accept.
	mux.HandleFunc("GET /api/v1/admin/conversation-media", api.AdminConversationMedia)
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
	// Bundle 212: offline admin blocks.
	mux.HandleFunc("GET /api/v1/trust", api.Trust)
	mux.HandleFunc("POST /api/v1/admin/offline-cert", api.AdminOfflineCert)
	mux.HandleFunc("POST /api/v1/admin-blocks/sync", api.AdminBlocksSync)
	mux.HandleFunc("POST /api/v1/admin-blocks/mine", api.AdminBlocksMine)
	// Bundle 220: admin actions go to all phones.
	mux.HandleFunc("POST /api/v1/admin-actions/sync", api.AdminActionsSync)
	mux.HandleFunc("POST /api/v1/admin-actions/mine", api.AdminActionsMine)
	mux.HandleFunc("GET /api/v1/admin/actions", api.AdminActionsList)
	mux.HandleFunc("POST /api/v1/admin/actions/{id}/cancel", api.AdminActionCancel)
	// Bundle 221: the Owner gets chats from the phones; delete completely.
	mux.HandleFunc("POST /api/v1/admin/chat-requests", api.AdminChatRequestCreate)
	mux.HandleFunc("GET /api/v1/admin/chat-requests", api.AdminChatRequestList)
	mux.HandleFunc("GET /api/v1/admin/chat-requests/{id}/answer", api.AdminChatRequestAnswer)
	mux.HandleFunc("POST /api/v1/admin/chat-requests/{id}/close", api.AdminChatRequestClose)
	mux.HandleFunc("POST /api/v1/admin/chat-requests/{id}/file", api.AdminChatRequestFile)
	mux.HandleFunc("POST /api/v1/admin/purge", api.AdminPurge)
	mux.HandleFunc("POST /api/v1/admin-actions/answers", api.AdminActionAnswers)
	mux.HandleFunc("POST /api/v1/admin-actions/answers/fetch", api.AdminActionAnswersFetch)
	// Bundle 213: minimum app version.
	mux.HandleFunc("GET /api/v1/version-rule", api.VersionRule)
	mux.HandleFunc("POST /api/v1/version-rule/sync", api.VersionRuleSync)
	mux.HandleFunc("GET /api/v1/admin/version-rule", api.AdminGetVersionRule)
	mux.HandleFunc("POST /api/v1/admin/version-rule", api.AdminSetVersionRule)
	// Bundle 214: app users as admins (crown, no admin login in the app).
	mux.HandleFunc("POST /api/v1/admin/user-admin", api.AdminSetUserAdmin)
	mux.HandleFunc("POST /api/v1/admin/device-session", api.AdminDeviceSession)
	mux.HandleFunc("GET /api/v1/admin-badges", api.AdminBadges)
	mux.HandleFunc("GET /api/v1/groups", api.ListGroups)
	mux.Handle("POST /api/v1/groups", api.WithIdempotency("group-create", http.HandlerFunc(api.CreateGroup)))
	mux.HandleFunc("GET /api/v1/groups/members", api.GroupMembers)
	mux.HandleFunc("POST /api/v1/groups/members", api.AddGroupMember)
	mux.HandleFunc("DELETE /api/v1/groups/members", api.RemoveGroupMember)
	// Bundle G2: admins, info, photo, leave.
	mux.HandleFunc("PATCH /api/v1/groups", api.UpdateGroup)
	mux.HandleFunc("GET /api/v1/groups/info", api.GroupInfo)
	mux.HandleFunc("GET /api/v1/groups/photo", api.GroupPhoto)
	mux.HandleFunc("PUT /api/v1/groups/photo", api.SetGroupPhoto)
	mux.HandleFunc("DELETE /api/v1/groups/photo", api.SetGroupPhoto)
	mux.HandleFunc("POST /api/v1/groups/leave", api.LeaveGroup)
	mux.HandleFunc("POST /api/v1/groups/role", api.SetGroupRole)
	// Bundle G5: receipts per member (Message info).
	mux.HandleFunc("GET /api/v1/groups/receipts", api.GroupReceipts)
	// Bundle G6: invite link.
	mux.HandleFunc("POST /api/v1/groups/invite", api.GroupInvite)
	mux.HandleFunc("GET /api/v1/groups/join", api.GroupJoinPreview)
	mux.HandleFunc("POST /api/v1/groups/join", api.GroupJoin)
	mux.HandleFunc("GET /api/v1/groups/join-requests", api.GroupJoinRequests)
	mux.HandleFunc("DELETE /api/v1/groups/join-requests", api.GroupJoinDecline)
	mux.HandleFunc("GET /join/{token}", api.GroupJoinPage)
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
	limitedHandler := limitRequestBody(api.VersionGate(mux)) // Bundle 213: version gate
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
		// Bundle 221: the server keeps a message only until delivered (+7 days).
		if _, _, err := api.PostmanTick(time.Now()); err != nil {
			log.Printf("postman cleanup error: %v", err)
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
				if _, _, err := api.PostmanTick(time.Now()); err != nil {
					log.Printf("postman cleanup error: %v", err)
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

	// Bundle 243 (audit S3 part A): HTTPS with a self-signed certificate on
	// a second port (8443), next to plain HTTP (8080) during the move.
	// Phones trust the certificate by its hash from the signed pairing proof.
	tlsSrv := startAutoTLS243(api, srv.Handler, dbPath)

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
	if tlsSrv != nil {
		_ = tlsSrv.Shutdown(ctx)
	}
}

// startAutoTLS243 starts HTTPS on LOCALLINK_TLS_ADDR (default :8443) unless
// LOCALLINK_TLS_AUTO=false. Any failure only logs: HTTP keeps working.
func startAutoTLS243(api *httpapi.API, handler http.Handler, dbPath string) *http.Server {
	if strings.EqualFold(strings.TrimSpace(getenv("LOCALLINK_TLS_AUTO", "true")), "false") {
		log.Printf("TLS_AUTO_243 off")
		return nil
	}
	tlsAddr := getenv("LOCALLINK_TLS_ADDR", ":8443")
	port := addrPort243(tlsAddr)
	if port <= 0 {
		log.Printf("TLS_AUTO_243 bad LOCALLINK_TLS_ADDR %q", tlsAddr)
		return nil
	}
	dir := getenv("LOCALLINK_TLS_DIR", filepath.Join(filepath.Dir(dbPath), "tls"))
	certPath, keyPath, sha, err := tlsauto.Ensure(dir)
	if err != nil {
		log.Printf("TLS_AUTO_243 certificate failed: %v", err)
		return nil
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		log.Printf("TLS_AUTO_243 load failed: %v", err)
		return nil
	}
	ln, err := tls.Listen("tcp", tlsAddr, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}})
	if err != nil {
		log.Printf("TLS_AUTO_243 listen failed on %s: %v", tlsAddr, err)
		return nil
	}
	tlsSrv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	api.SetTLS243(port, sha)
	_ = os.Setenv("LOCALLINK_TLS_PORT_ADVERTISE", strconv.Itoa(port))
	log.Printf("TLS_AUTO_243 HTTPS on %s cert_sha256=%s", tlsAddr, sha)
	go func() {
		if err := tlsSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("TLS_AUTO_243 server error: %v", err)
		}
	}()
	return tlsSrv
}

func addrPort243(addr string) int {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return 0
	}
	v, err := strconv.Atoi(addr[i+1:])
	if err != nil || v <= 0 || v > 65535 {
		return 0
	}
	return v
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
