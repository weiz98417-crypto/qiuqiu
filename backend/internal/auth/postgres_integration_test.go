package auth_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/privacy"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresAnonymousSessionKeepsIdentityForTheSameActiveDevice(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	migrations.Close()
	store, err := auth.OpenPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open auth store: %v", err)
	}
	t.Cleanup(store.Close)
	manager, err := auth.NewManager(store, "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "device_postgres_" + time.Now().UTC().Format("20060102150405.000000000")

	first, err := manager.IssueAnonymous(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.IssueAnonymous(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Claims.Subject != first.Claims.Subject {
		t.Fatalf("same device user id changed from %q to %q", first.Claims.Subject, second.Claims.Subject)
	}
	if second.Claims.SessionID == first.Claims.SessionID {
		t.Fatal("same device should receive a new PostgreSQL-backed session")
	}
}

func TestPostgresAnonymousSessionClaimsOneIdentityDuringConcurrentIssue(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	migrations.Close()
	store, err := auth.OpenPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open auth store: %v", err)
	}
	t.Cleanup(store.Close)
	manager, err := auth.NewManager(store, "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "device_postgres_concurrent_" + time.Now().UTC().Format("20060102150405.000000000")
	const count = 16
	start := make(chan struct{})
	results := make(chan auth.Session, count)
	errorsFound := make(chan error, count)
	var wait sync.WaitGroup
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			session, issueErr := manager.IssueAnonymous(ctx, deviceID)
			if issueErr != nil {
				errorsFound <- issueErr
				return
			}
			results <- session
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsFound)
	for issueErr := range errorsFound {
		t.Fatal(issueErr)
	}
	userID := ""
	for session := range results {
		if userID == "" {
			userID = session.Claims.Subject
		}
		if session.Claims.Subject != userID {
			t.Fatalf("concurrent PostgreSQL issue split identity: %q != %q", session.Claims.Subject, userID)
		}
	}
}

func TestPostgresAnonymousIssueDoesNotRecreateDeletedUser(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	migrations.Close()
	authStore, err := auth.OpenPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open auth store: %v", err)
	}
	t.Cleanup(authStore.Close)
	privacyStore, err := privacy.OpenPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open privacy store: %v", err)
	}
	t.Cleanup(privacyStore.Close)
	manager, err := auth.NewManager(authStore, "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "device_postgres_delete_race_" + time.Now().UTC().Format("20060102150405.000000000")
	first, err := manager.IssueAnonymous(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	oldUserID := first.Claims.Subject
	cleanupPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanupPool.Close)
	var freshUserID string
	t.Cleanup(func() {
		_, _ = cleanupPool.Exec(context.Background(), `
			DELETE FROM anonymous_device_identities WHERE device_id = $1;
			DELETE FROM user_sessions WHERE user_id = $2 OR user_id = $3;
			DELETE FROM privacy_tombstones WHERE user_id = $2
		`, deviceID, oldUserID, freshUserID)
	})

	start := make(chan struct{})
	var issued auth.Session
	var issueErr, deletionErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		issued, issueErr = manager.IssueAnonymous(ctx, deviceID)
	}()
	go func() {
		defer wait.Done()
		<-start
		if _, deletionErr = privacyStore.RequestDeletion(ctx, oldUserID, "concurrent_user_request"); deletionErr == nil {
			deletionErr = privacyStore.ProcessDeletion(ctx, oldUserID)
		}
	}()
	close(start)
	wait.Wait()
	if deletionErr != nil {
		t.Fatalf("concurrent deletion: %v", deletionErr)
	}
	if issueErr != nil && !errors.Is(issueErr, auth.ErrIdentityUnavailable) {
		t.Fatalf("concurrent issue: %v", issueErr)
	}
	if issueErr == nil && issued.Claims.Subject == oldUserID {
		if _, authenticateErr := manager.Authenticate(ctx, issued.AccessToken); authenticateErr == nil {
			t.Fatal("session for the deleted user remained queryable")
		}
	}
	fresh, err := manager.IssueAnonymous(ctx, deviceID)
	if err != nil {
		t.Fatalf("issue fresh anonymous identity: %v", err)
	}
	freshUserID = fresh.Claims.Subject
	if freshUserID == oldUserID {
		t.Fatal("deleted anonymous user id was reissued")
	}
}
