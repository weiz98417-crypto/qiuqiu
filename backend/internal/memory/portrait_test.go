package memory

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/privacy"
)

func TestResolvePortraitLayersEditsTombstonesAndUserSlots(t *testing.T) {
	updated := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	entries := []PortraitEntry{
		{ID: "prof-1", Topic: "basic_info", SubTopic: "favorite_team", Content: "皇马", UpdatedAt: updated, Source: PortraitSourceSynthesis},
		{ID: "prof-2", Topic: "basic_info", SubTopic: "favorite_player", Content: "佩德里", UpdatedAt: updated, Source: PortraitSourceSynthesis},
	}
	overlays := []PortraitOverlay{
		{Topic: "basic_info", SubTopic: "favorite_team", Content: "巴萨", UpdatedAt: updated.Add(time.Hour)},
		{Topic: "basic_info", SubTopic: "favorite_player", Deleted: true, UpdatedAt: updated.Add(time.Hour)},
		{Topic: "preferences", SubTopic: "reply_style", Content: "简短直接", UpdatedAt: updated.Add(2 * time.Hour)},
	}
	merged := ResolvePortrait(entries, overlays)
	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want the edited team slot and the user-created style slot", merged)
	}
	if merged[0].Topic != "basic_info" || merged[0].SubTopic != "favorite_team" {
		t.Fatalf("first slot = %+v, want the ordered basic_info/favorite_team edit", merged[0])
	}
	if merged[0].Content != "巴萨" || merged[0].Source != PortraitSourceUser {
		t.Fatalf("edited slot = %+v, want user-authored content override", merged[0])
	}
	if merged[0].UpdatedAt != updated.Add(time.Hour) {
		t.Fatalf("edited slot timestamp = %v, want the overlay time", merged[0].UpdatedAt)
	}
	if merged[1].SubTopic != "reply_style" || merged[1].Content != "简短直接" {
		t.Fatalf("second slot = %+v, want the user-created slot appended", merged[1])
	}
}

// TestQueuePortraitTombstoneIsHonoredOnNextTurn is the seam-level delete eval:
// a user forget writes a local tombstone and the assembled portrait — the
// exact read behind prompt injection — drops the slot immediately, with and
// without a reachable Memobase.
func TestQueuePortraitTombstoneIsHonoredOnNextTurn(t *testing.T) {
	ctx := context.Background()
	overlays := NewMemoryPortraitOverlays()
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil, WithPortraitOverlays(overlays))

	if _, err := queue.SetPortraitEntry(ctx, "user-1", "basic_info", "favorite_player", "佩德里", ""); err != nil {
		t.Fatalf("SetPortraitEntry: %v", err)
	}
	portrait, err := queue.Portrait(ctx, "user-1")
	if err != nil {
		t.Fatalf("Portrait after edit: %v", err)
	}
	if !strings.Contains(portrait.Block, "佩德里") {
		t.Fatalf("portrait block = %q, want the user edit", portrait.Block)
	}
	if len(portrait.Entries) != 1 || portrait.Entries[0].Source != PortraitSourceUser {
		t.Fatalf("portrait entries = %+v, want one user-authored slot", portrait.Entries)
	}

	if err := queue.ForgetPortraitEntry(ctx, "user-1", "basic_info", "favorite_player", ""); err != nil {
		t.Fatalf("ForgetPortraitEntry: %v", err)
	}
	if portrait, err = queue.Portrait(ctx, "user-1"); err == nil && portrait.Block != "" {
		t.Fatalf("portrait after forget = %+v, want the empty degraded portrait", portrait)
	}
	entries, _ := queue.PortraitEntries(ctx, "user-1")
	if len(entries) != 0 {
		t.Fatalf("portrait entries after forget = %+v, want none", entries)
	}
}

func TestQueuePortraitLayersOverlayOverSynthesisAndForwardsMutations(t *testing.T) {
	server, requestsFn := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/profile/"):
			return http.StatusOK, memobaseOK(`{"profiles":[{"id":"prof-team","content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-09-01T03:04:05Z"}]}`)
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/"):
			return http.StatusOK, memobaseOK(`{}`)
		default:
			return http.StatusOK, memobaseOK(`null`)
		}
	})
	defer server.Close()

	ctx := context.Background()
	overlays := NewMemoryPortraitOverlays()
	queue := NewQueue(NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"}), nil, nil, WithPortraitOverlays(overlays))

	if _, err := queue.SetPortraitEntry(ctx, "user-1", "basic_info", "favorite_team", "巴萨", "prof-team"); err != nil {
		t.Fatalf("SetPortraitEntry: %v", err)
	}
	portrait, err := queue.Portrait(ctx, "user-1")
	if err != nil {
		t.Fatalf("Portrait: %v", err)
	}
	if !strings.Contains(portrait.Block, "巴萨") || strings.Contains(portrait.Block, "皇马") {
		t.Fatalf("portrait block = %q, want the user edit to replace synthesis", portrait.Block)
	}
	var put *recordedRequest
	for _, request := range requestsFn() {
		if request.method == http.MethodPut && strings.HasSuffix(request.path, "/users/profile/"+url.PathEscape("qiuqiu-user-1")+"/prof-team") {
			request := request
			put = &request
		}
	}
	if put == nil {
		t.Fatalf("requests = %+v, want the forwarded profile PUT", requestsFn())
	}
	if !strings.Contains(string(put.body), "巴萨") {
		t.Fatalf("PUT body = %s, want the edited content", put.body)
	}

	if err := queue.ForgetPortraitEntry(ctx, "user-1", "basic_info", "favorite_team", ""); err != nil {
		t.Fatalf("ForgetPortraitEntry: %v", err)
	}
	if portrait, err = queue.Portrait(ctx, "user-1"); err != nil || portrait.Block != "" {
		t.Fatalf("portrait after forget = %+v err=%v, want the empty block", portrait, err)
	}
	forwardedDelete := false
	for _, request := range requestsFn() {
		if request.method == http.MethodDelete && strings.HasSuffix(request.path, "/prof-team") {
			forwardedDelete = true
		}
	}
	if !forwardedDelete {
		t.Fatalf("requests = %+v, want the forwarded profile DELETE", requestsFn())
	}
}

func TestQueuePortraitMutationsNeedAnOverlayStore(t *testing.T) {
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)
	if _, err := queue.SetPortraitEntry(context.Background(), "user-1", "basic_info", "favorite_team", "巴萨", ""); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("SetPortraitEntry without overlays = %v, want ErrNotSupported", err)
	}
	if err := queue.ForgetPortrait(context.Background(), "user-1"); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("ForgetPortrait without overlays = %v, want ErrNotSupported", err)
	}
}

// failingListOverlays simulates the overlay store failing on its List half
// so the whole-portrait forget hits a partial-delete state.
type failingListOverlays struct {
	MemoryPortraitOverlays
	listErr error
}

func (f *failingListOverlays) List(context.Context, string) ([]PortraitOverlay, error) {
	return nil, f.listErr
}

func TestForgetPortraitSurfacesAdapterFailureInsteadOfHalfDelete(t *testing.T) {
	server, _ := stubMemobaseServer(func(recordedRequest) (int, []byte) {
		return http.StatusInternalServerError, []byte("memobase down")
	})
	defer server.Close()
	ctx := context.Background()
	overlays := NewMemoryPortraitOverlays()
	queue := NewQueue(NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"}), nil, nil, WithPortraitOverlays(overlays))
	if _, err := queue.SetPortraitEntry(ctx, "user-1", "basic_info", "favorite_player", "佩德里", ""); err != nil {
		t.Fatalf("SetPortraitEntry: %v", err)
	}
	if err := queue.ForgetPortrait(ctx, "user-1"); err == nil {
		t.Fatal("ForgetPortrait with an unreachable adapter must error, not report a half-done privacy delete as success")
	}
	remaining, err := overlays.List(ctx, "user-1")
	if err != nil || len(remaining) != 1 || remaining[0].Deleted {
		t.Fatalf("overlays after failed forget = %+v err=%v, want the entry untouched", remaining, err)
	}
}

func TestForgetPortraitSurfacesOverlayListFailure(t *testing.T) {
	listErr := errors.New("overlay list unavailable")
	overlays := &failingListOverlays{MemoryPortraitOverlays: *NewMemoryPortraitOverlays(), listErr: listErr}
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil, WithPortraitOverlays(overlays))
	if err := queue.ForgetPortrait(context.Background(), "user-1"); !errors.Is(err, listErr) {
		t.Fatalf("ForgetPortrait List failure = %v, want %v (the half-deleted portrait must not report success)", err, listErr)
	}
}

func TestForgetPortraitStillSucceedsForDevLocalOnlyAndHealthyAdapter(t *testing.T) {
	server, requestsFn := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/profile/"):
			return http.StatusOK, memobaseOK(`{"profiles":[{"id":"prof-team","content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-09-01T03:04:05Z"}]}`)
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/"):
			return http.StatusOK, memobaseOK(`{}`)
		default:
			return http.StatusOK, memobaseOK(`null`)
		}
	})
	defer server.Close()

	ctx := context.Background()
	// Unconfigured Memobase (dev): only the local half exists, so the delete
	// must keep succeeding.
	localOnly := NewMemoryPortraitOverlays()
	devQueue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil, WithPortraitOverlays(localOnly))
	if _, err := devQueue.SetPortraitEntry(ctx, "user-1", "basic_info", "favorite_player", "佩德里", ""); err != nil {
		t.Fatalf("SetPortraitEntry: %v", err)
	}
	if err := devQueue.ForgetPortrait(ctx, "user-1"); err != nil {
		t.Fatalf("ForgetPortrait without Memobase = %v, want the local-only delete to succeed", err)
	}
	remaining, _ := localOnly.List(ctx, "user-1")
	if len(remaining) != 1 || !remaining[0].Deleted {
		t.Fatalf("overlays after dev forget = %+v, want the tombstoned slot", remaining)
	}

	// Healthy Memobase: the synthesis slot is tombstoned and forwarded too.
	overlays := NewMemoryPortraitOverlays()
	queue := NewQueue(NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"}), nil, nil, WithPortraitOverlays(overlays))
	if err := queue.ForgetPortrait(ctx, "user-2"); err != nil {
		t.Fatalf("ForgetPortrait with a healthy adapter: %v", err)
	}
	forwardedDelete := false
	for _, request := range requestsFn() {
		if request.method == http.MethodDelete && strings.HasSuffix(request.path, "/prof-team") {
			forwardedDelete = true
		}
	}
	if !forwardedDelete {
		t.Fatalf("requests = %+v, want the forwarded synthesis DELETE", requestsFn())
	}
}

// hiddenOverlays simulates the privacy lifecycle at the store seam: Check
// reports an active user tombstone so the whole portrait must disappear.
type hiddenOverlays struct {
	MemoryPortraitOverlays
}

func (h *hiddenOverlays) Check(context.Context, string) error {
	return privacy.ErrDataDeleted
}

func TestQueuePortraitVanishesWhilePrivacyTombstoneIsActive(t *testing.T) {
	server, _ := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/profile/"):
			return http.StatusOK, memobaseOK(`{"profiles":[{"id":"prof-1","content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-09-01T03:04:05Z"}]}`)
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/"):
			return http.StatusOK, memobaseOK(`{}`)
		default:
			return http.StatusOK, memobaseOK(`null`)
		}
	})
	defer server.Close()

	queue := NewQueue(NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"}), nil, nil, WithPortraitOverlays(&hiddenOverlays{*NewMemoryPortraitOverlays()}))
	portrait, err := queue.Portrait(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Portrait under a privacy tombstone must degrade, not fail: %v", err)
	}
	if portrait.Block != "" || len(portrait.Entries) != 0 {
		t.Fatalf("portrait under a privacy tombstone = %+v, want empty", portrait)
	}
}

func TestMemobasePortraitEntriesCarryRemoteIDs(t *testing.T) {
	server, _ := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/profile/"):
			return http.StatusOK, memobaseOK(`{"profiles":[{"id":"prof-team","content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-09-01T03:04:05Z"}]}`)
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/"):
			return http.StatusOK, memobaseOK(`{}`)
		default:
			return http.StatusOK, memobaseOK(`null`)
		}
	})
	defer server.Close()
	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	portrait, err := adapter.Portrait(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Portrait: %v", err)
	}
	if len(portrait.Entries) != 1 || portrait.Entries[0].ID != "prof-team" {
		t.Fatalf("portrait entries = %+v, want the remote profile id", portrait.Entries)
	}
	if portrait.Entries[0].Source != PortraitSourceSynthesis {
		t.Fatalf("entry source = %q, want %q", portrait.Entries[0].Source, PortraitSourceSynthesis)
	}
}

func TestMemobaseUpdateAndDeleteProfileEntryIssueSDKRequests(t *testing.T) {
	server, requestsFn := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/"):
			return http.StatusOK, memobaseOK(`{}`)
		default:
			return http.StatusOK, memobaseOK(`null`)
		}
	})
	defer server.Close()
	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	if err := adapter.UpdateProfileEntry(context.Background(), "user-1", "prof-1", "basic_info", "favorite_team", "巴萨"); err != nil {
		t.Fatalf("UpdateProfileEntry: %v", err)
	}
	if err := adapter.DeleteProfileEntry(context.Background(), "user-1", "prof-1"); err != nil {
		t.Fatalf("DeleteProfileEntry: %v", err)
	}
	requests := requestsFn()
	if len(requests) != 3 { // ensureUser get, forwarded PUT, forwarded DELETE (user cached)
		t.Fatalf("requests = %d (%+v), want ensureUser, put and delete", len(requests), requests)
	}
	if requests[1].method != http.MethodPut || !strings.HasSuffix(requests[1].path, "/prof-1") {
		t.Fatalf("update request = %s %s, want PUT on the profile slot", requests[1].method, requests[1].path)
	}
	if !strings.Contains(string(requests[1].body), `"sub_topic":"favorite_team"`) {
		t.Fatalf("update body = %s, want the topic attributes", requests[1].body)
	}
	if requests[2].method != http.MethodDelete || !strings.HasSuffix(requests[2].path, "/prof-1") {
		t.Fatalf("delete request = %s %s, want DELETE on the profile slot", requests[2].method, requests[2].path)
	}
}

func TestMemoryPortraitOverlaysRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryPortraitOverlays()
	if err := store.Check(ctx, "user-1"); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if _, err := store.Put(ctx, "user-1", "basic_info", "favorite_team", "巴萨"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete(ctx, "user-1", "preferences", "reply_style"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	overlays, err := store.List(ctx, "user-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(overlays) != 2 || overlays[0].SubTopic != "favorite_team" || !overlays[1].Deleted {
		t.Fatalf("overlays = %+v, want the edit and the tombstone in key order", overlays)
	}
}
