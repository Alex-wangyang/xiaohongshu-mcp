package xiaohongshu

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFeedListHydrationAndEmptySnapshot(t *testing.T) {
	calls := 0
	got, err := awaitFeedList(context.Background(), func() (string, error) {
		calls++
		if calls == 1 {
			return "", nil
		}
		return "[]", nil
	})
	if err != nil || got != "[]" || calls != 2 {
		t.Fatalf("got %q, %v after %d calls", got, err, calls)
	}
}

func TestFeedListCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	_, err := awaitFeedList(ctx, func() (string, error) { cancel(); return "", nil })
	if !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatalf("cancellation not respected: %v", err)
	}
}

func TestFeedListBrowserFailure(t *testing.T) {
	want := errors.New("browser failure")
	_, err := awaitFeedList(context.Background(), func() (string, error) { return "", want })
	if !errors.Is(err, want) {
		t.Fatalf("lost browser error: %v", err)
	}
}
