package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

const searchNotesPath = "/api/sns/web/v1/search/notes"

// A filter can legitimately return the same notes or no notes. Completion is
// therefore tied to its own network request, never to changing note IDs.
type searchRequestCompletion struct {
	keyword  string
	id       proto.NetworkRequestID
	status   int
	complete bool
	failed   bool
}

func (s *searchRequestCompletion) sent(e *proto.NetworkRequestWillBeSent) {
	if s.id != "" || e.Request == nil || e.Request.Method != "POST" {
		return
	}
	u, err := url.Parse(e.Request.URL)
	if err != nil || u.Path != searchNotesPath {
		return
	}
	var body struct {
		Keyword string `json:"keyword"`
	}
	if json.Unmarshal([]byte(e.Request.PostData), &body) != nil || body.Keyword != s.keyword {
		return
	}
	s.id = e.RequestID
}
func (s *searchRequestCompletion) response(e *proto.NetworkResponseReceived) {
	if s.id != "" && e.RequestID == s.id && e.Response != nil {
		s.status = e.Response.Status
	}
}
func (s *searchRequestCompletion) finished(e *proto.NetworkLoadingFinished) bool {
	if s.id == "" || e.RequestID != s.id {
		return false
	}
	s.complete = true
	return true
}
func (s *searchRequestCompletion) failure(e *proto.NetworkLoadingFailed) bool {
	if s.id == "" || e.RequestID != s.id {
		return false
	}
	s.failed = true
	return true
}
func (s *searchRequestCompletion) result() error {
	if !s.complete || s.failed || s.status < 200 || s.status >= 300 {
		return fmt.Errorf("filtered search request did not complete successfully")
	}
	return nil
}

func awaitFilteredSearchRequest(ctx context.Context, page *rod.Page, keyword string, click func() error) error {
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	p := page.Context(waitCtx)
	state := &searchRequestCompletion{keyword: keyword}
	// Both listeners are armed before clicking. Restrict idle detection to the
	// search API so unrelated page polling cannot keep the operation alive.
	waitIdle := p.WaitRequestIdle(300*time.Millisecond, []string{regexp.QuoteMeta(searchNotesPath)}, nil, nil)
	wait := p.EachEvent(state.sent, state.response, state.finished, state.failure)
	done := make(chan struct{})
	go func() { defer close(done); wait() }()
	if err := click(); err != nil {
		cancel()
		<-done
		waitIdle()
		return err
	}
	<-done
	if err := waitCtx.Err(); err != nil {
		waitIdle()
		return err
	}
	if err := state.result(); err != nil {
		cancel()
		waitIdle()
		return err
	}
	// Allow response handlers to settle, then the caller reads an atomic
	// request-matched successful snapshot (which may contain an empty array).
	waitIdle()
	return waitCtx.Err()
}
