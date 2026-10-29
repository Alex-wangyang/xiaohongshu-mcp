package xiaohongshu

import (
	"bytes"
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	xhserrors "github.com/xpzouying/xiaohongshu-mcp/errors"
)

func TestReadActionsDeferBrowserWorkUntilRequestContextExists(t *testing.T) {
	require.NotPanics(t, func() {
		search := NewSearchAction(nil)
		feeds := NewFeedsListAction(nil)
		profile := NewUserProfileAction(nil)

		require.Equal(t, readOperationSearchFeeds, search.read.operation)
		require.Equal(t, readOperationListFeeds, feeds.read.operation)
		require.Equal(t, readOperationUserProfile, profile.read.operation)
	})
}

func TestReadDeadlineContract(t *testing.T) {
	operations := []readOperation{
		readOperationSearchFeeds,
		readOperationListFeeds,
		readOperationUserProfile,
	}

	for _, operation := range operations {
		t.Run(string(operation), func(t *testing.T) {
			ctx, cancel := newBrowserReadContext(context.Background())
			defer cancel()

			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			remaining := time.Until(deadline)
			require.Greater(t, remaining, 59*time.Second)
			require.LessOrEqual(t, remaining, 60*time.Second)
		})
	}
}

func TestReadDeadlineDoesNotExtendShorterParent(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelParent()
	parentDeadline, ok := parent.Deadline()
	require.True(t, ok)

	readContext, cancelRead := newBrowserReadContext(parent)
	defer cancelRead()
	readDeadline, ok := readContext.Deadline()
	require.True(t, ok)
	require.Equal(t, parentDeadline, readDeadline)
}

func TestReadContextPreservesParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	readContext, cancelRead := newBrowserReadContext(parent)
	defer cancelRead()

	cancelParent()
	select {
	case <-readContext.Done():
	case <-time.After(time.Second):
		t.Fatal("read context did not inherit parent cancellation")
	}
	require.ErrorIs(t, readContext.Err(), context.Canceled)
}

func TestReadStageFailureIsFixedCategoryAndDoesNotLeakCause(t *testing.T) {
	sensitiveCause := stderrors.New("PRIVATE_KEYWORD_AND_URL")

	var logBuffer bytes.Buffer
	logger := logrus.StandardLogger()
	previousOutput := logger.Out
	previousFormatter := logger.Formatter
	previousLevel := logger.Level
	logger.SetOutput(&logBuffer)
	logger.SetFormatter(&logrus.JSONFormatter{DisableTimestamp: true})
	logger.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logger.SetOutput(previousOutput)
		logger.SetFormatter(previousFormatter)
		logger.SetLevel(previousLevel)
	})

	session := testBrowserReadSession(readOperationSearchFeeds, context.Background())
	err := session.run(readStageNavigate, func(_ *rod.Page) error {
		return sensitiveCause
	})

	var readErr *browserReadError
	require.ErrorAs(t, err, &readErr)
	require.Equal(t, readFailureBrowser, readErr.category)
	require.Equal(t, readOperationSearchFeeds, readErr.operation)
	require.Equal(t, readStageNavigate, readErr.stage)
	require.NotErrorIs(t, err, sensitiveCause)
	require.Nil(t, stderrors.Unwrap(err))
	require.Regexp(t, `^browser read failed: operation=search_feeds stage=navigate category=browser elapsed_ms=[0-9]+$`, err.Error())
	require.NotContains(t, err.Error(), sensitiveCause.Error())
	require.NotContains(t, logBuffer.String(), sensitiveCause.Error())
	require.Contains(t, logBuffer.String(), `"status":"started"`)
	require.Contains(t, logBuffer.String(), `"status":"failed"`)
	require.Contains(t, logBuffer.String(), `"category":"browser"`)
}

func TestReadStageClassifiesDeadlineAndCancellation(t *testing.T) {
	tests := []struct {
		name     string
		parent   func() (context.Context, context.CancelFunc)
		cause    error
		expected readFailureCategory
	}{
		{
			name: "deadline",
			parent: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			cause:    context.DeadlineExceeded,
			expected: readFailureTimeout,
		},
		{
			name: "cancellation",
			parent: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
			cause:    context.Canceled,
			expected: readFailureCanceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent, cancelParent := tt.parent()
			defer cancelParent()
			session := testBrowserReadSession(readOperationUserProfile, parent)

			err := session.run(readStageWaitStable, func(_ *rod.Page) error {
				return tt.cause
			})

			var readErr *browserReadError
			require.ErrorAs(t, err, &readErr)
			require.Equal(t, tt.expected, readErr.category)
		})
	}
}

func TestReadStagePreservesSafeNoFeedsSentinel(t *testing.T) {
	session := testBrowserReadSession(readOperationListFeeds, context.Background())
	err := session.run(readStageExtract, func(_ *rod.Page) error {
		return xhserrors.ErrNoFeeds
	})

	require.ErrorIs(t, err, xhserrors.ErrNoFeeds)
	require.Nil(t, stderrors.Unwrap(err))
	require.NotContains(t, err.Error(), xhserrors.ErrNoFeeds.Error())
}

func TestReadStageLogsSafeTerminalBeforeUnexpectedPanic(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := logrus.StandardLogger()
	previousOutput := logger.Out
	previousFormatter := logger.Formatter
	logger.SetOutput(&logBuffer)
	logger.SetFormatter(&logrus.JSONFormatter{DisableTimestamp: true})
	t.Cleanup(func() {
		logger.SetOutput(previousOutput)
		logger.SetFormatter(previousFormatter)
	})

	session := testBrowserReadSession(readOperationUserProfile, context.Background())
	require.PanicsWithValue(t, "PRIVATE_PANIC_PAYLOAD", func() {
		_ = session.run(readStageExtract, func(_ *rod.Page) error {
			panic("PRIVATE_PANIC_PAYLOAD")
		})
	})

	require.NotContains(t, logBuffer.String(), "PRIVATE_PANIC_PAYLOAD")
	require.True(t, strings.Contains(logBuffer.String(), `"category":"panic"`))
}

func testBrowserReadSession(operation readOperation, ctx context.Context) *browserReadSession {
	return &browserReadSession{
		operation: operation,
		ctx:       ctx,
		cancel:    func() {},
	}
}
