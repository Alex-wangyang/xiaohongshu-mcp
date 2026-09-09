package xiaohongshu

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
	xhserrors "github.com/xpzouying/xiaohongshu-mcp/errors"
)

const browserReadTimeout = 60 * time.Second

type readOperation string

const (
	readOperationListFeeds   readOperation = "list_feeds"
	readOperationSearchFeeds readOperation = "search_feeds"
	readOperationUserProfile readOperation = "user_profile"
	readOperationFeedDetail  readOperation = "get_feed_detail"
)

type readStage string

const (
	readStagePrepareFilters     readStage = "prepare_filters"
	readStageNavigate           readStage = "navigate"
	readStageWaitDOMStable      readStage = "wait_dom_stable"
	readStageSettle             readStage = "settle"
	readStageWaitStable         readStage = "wait_stable"
	readStageWaitInitialState   readStage = "wait_initial_state"
	readStageOpenFilters        readStage = "open_filters"
	readStageApplyFilters       readStage = "apply_filters"
	readStageWaitFilteredStable readStage = "wait_filtered_stable"
	readStageWaitFilteredState  readStage = "wait_filtered_state"
	readStageExtract            readStage = "extract"
	readStageExtractProfile     readStage = "extract_profile"
	readStageExtractNotes       readStage = "extract_notes"
	readStageDecode             readStage = "decode"
)

type readFailureCategory string

const (
	readFailureNone     readFailureCategory = "none"
	readFailureTimeout  readFailureCategory = "timeout"
	readFailureCanceled readFailureCategory = "canceled"
	readFailureInput    readFailureCategory = "input"
	readFailureEmpty    readFailureCategory = "empty"
	readFailureData     readFailureCategory = "data"
	readFailureBrowser  readFailureCategory = "browser"
	readFailurePanic    readFailureCategory = "panic"
)

type invalidReadInputError struct {
	cause error
}

func (e *invalidReadInputError) Error() string { return "invalid browser read input" }
func (e *invalidReadInputError) Unwrap() error { return e.cause }

type browserReadError struct {
	operation readOperation
	stage     readStage
	category  readFailureCategory
	elapsedMS int64
}

func (e *browserReadError) Error() string {
	return fmt.Sprintf(
		"browser read failed: operation=%s stage=%s category=%s elapsed_ms=%d",
		e.operation,
		e.stage,
		e.category,
		e.elapsedMS,
	)
}

func (e *browserReadError) Is(target error) bool {
	return e.category == readFailureEmpty && target == xhserrors.ErrNoFeeds
}

type browserReadAction struct {
	operation readOperation
	page      *rod.Page
}

func newBrowserReadAction(operation readOperation, page *rod.Page) browserReadAction {
	return browserReadAction{operation: operation, page: page}
}

func (a browserReadAction) begin(parent context.Context) *browserReadSession {
	return newBrowserReadSession(a.operation, a.page, parent)
}

type browserReadSession struct {
	operation readOperation
	page      *rod.Page
	ctx       context.Context
	cancel    context.CancelFunc
}

func newBrowserReadSession(operation readOperation, page *rod.Page, parent context.Context) *browserReadSession {
	ctx, cancel := newBrowserReadContext(parent)
	return &browserReadSession{
		operation: operation,
		page:      page.Context(ctx),
		ctx:       ctx,
		cancel:    cancel,
	}
}

func newBrowserReadContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, browserReadTimeout)
}

func (s *browserReadSession) close() {
	s.cancel()
}

func (s *browserReadSession) run(stage readStage, fn func(*rod.Page) error) (err error) {
	startedAt := time.Now()
	logReadStage(s.operation, stage, "started", readFailureNone, 0)

	defer func() {
		if recovered := recover(); recovered != nil {
			logReadStage(s.operation, stage, "failed", readFailurePanic, time.Since(startedAt))
			panic(recovered)
		}
	}()

	cause := fn(s.page)
	elapsed := time.Since(startedAt)
	if cause == nil {
		logReadStage(s.operation, stage, "succeeded", readFailureNone, elapsed)
		return nil
	}

	category := classifyReadFailure(s.ctx, cause)
	logReadStage(s.operation, stage, "failed", category, elapsed)
	return &browserReadError{
		operation: s.operation,
		stage:     stage,
		category:  category,
		elapsedMS: elapsed.Milliseconds(),
	}
}

func classifyReadFailure(ctx context.Context, cause error) readFailureCategory {
	if stderrors.Is(ctx.Err(), context.DeadlineExceeded) || stderrors.Is(cause, context.DeadlineExceeded) {
		return readFailureTimeout
	}
	if stderrors.Is(ctx.Err(), context.Canceled) || stderrors.Is(cause, context.Canceled) {
		return readFailureCanceled
	}

	var inputErr *invalidReadInputError
	if stderrors.As(cause, &inputErr) {
		return readFailureInput
	}
	if stderrors.Is(cause, xhserrors.ErrNoFeeds) {
		return readFailureEmpty
	}

	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if stderrors.As(cause, &syntaxErr) || stderrors.As(cause, &typeErr) {
		return readFailureData
	}
	return readFailureBrowser
}

func logReadStage(operation readOperation, stage readStage, status string, category readFailureCategory, elapsed time.Duration) {
	logrus.WithFields(logrus.Fields{
		"operation":  operation,
		"stage":      stage,
		"status":     status,
		"category":   category,
		"elapsed_ms": elapsed.Milliseconds(),
	}).Info("browser read stage")
}
