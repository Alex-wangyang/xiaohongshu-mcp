package xiaohongshu

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type fixtureFeedDetailPage struct {
	ctx    context.Context
	calls  []string
	failAt string
	raw    string
}

func (p *fixtureFeedDetailPage) step(stage string) error {
	p.calls = append(p.calls, stage)
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if p.failAt == stage {
		return errors.New("PRIVATE_URL_TOKEN_AND_NOTE")
	}
	return nil
}

func (p *fixtureFeedDetailPage) navigate(_ string) error  { return p.step("navigate") }
func (p *fixtureFeedDetailPage) waitReady(_ string) error { return p.step("ready") }
func (p *fixtureFeedDetailPage) extract(_ string) (string, error) {
	return p.raw, p.step("extract")
}

func captureDetailReadLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger := logrus.StandardLogger()
	previousOutput, previousFormatter := logger.Out, logger.Formatter
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{DisableTimestamp: true})
	t.Cleanup(func() {
		logger.SetOutput(previousOutput)
		logger.SetFormatter(previousFormatter)
	})
	return &buf
}

func TestBoundedFeedDetailReadPreservesRequestedDataWithoutLoggingIt(t *testing.T) {
	logs := captureDetailReadLog(t)
	p := &fixtureFeedDetailPage{raw: `{"note":{"noteId":"PRIVATE_NOTE_ID","xsecToken":"PRIVATE_TOKEN","title":"PRIVATE_TITLE","desc":"","type":"normal","user":{"userId":"PRIVATE_USER"}},"comments":{"list":[],"hasMore":true}}`}
	var readContext context.Context
	result, err := runBoundedFeedDetail(context.Background(), "PRIVATE_NOTE_ID", "PRIVATE_TOKEN", func(ctx context.Context) feedDetailReadPage {
		readContext, p.ctx = ctx, ctx
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.InDelta(t, 60, time.Until(deadline).Seconds(), 1)
		return p
	})
	require.NoError(t, err)
	require.Equal(t, []string{"navigate", "ready", "extract"}, p.calls)
	require.Equal(t, "PRIVATE_TITLE", result.Note.Title)
	require.Equal(t, "PRIVATE_TOKEN", result.Note.XsecToken)
	require.True(t, result.Comments.HasMore)
	require.ErrorIs(t, readContext.Err(), context.Canceled)
	require.NotContains(t, logs.String(), "PRIVATE_")
	require.Contains(t, logs.String(), `"operation":"get_feed_detail"`)
}

func TestBoundedFeedDetailReadStopsAtFirstFailureAndRedactsEveryStage(t *testing.T) {
	for index, stage := range []string{"navigate", "ready", "extract"} {
		t.Run(stage, func(t *testing.T) {
			logs := captureDetailReadLog(t)
			p := &fixtureFeedDetailPage{failAt: stage}
			result, err := runBoundedFeedDetail(context.Background(), "PRIVATE_NOTE_ID", "PRIVATE_TOKEN", func(ctx context.Context) feedDetailReadPage {
				p.ctx = ctx
				return p
			})
			require.Nil(t, result)
			require.Error(t, err)
			require.Len(t, p.calls, index+1)
			require.NotContains(t, err.Error()+logs.String(), "PRIVATE_")
			require.Nil(t, errors.Unwrap(err))
			require.Contains(t, err.Error(), "operation=get_feed_detail")
		})
	}
}

func TestBoundedFeedDetailReadRejectsWrongOrMalformedNoteSafely(t *testing.T) {
	for _, raw := range []string{
		`null`, `{}`, `{"note":{"noteId":"OTHER_PRIVATE_ID"}}`,
		`{"note":{"noteId":"PRIVATE_ID"}}`,
		`{"note":{"noteId":"PRIVATE_ID"},"comments":{"list":[]}}`,
		`{"note":{"noteId":"PRIVATE_ID","xsecToken":"PRIVATE_TOKEN","title":"","desc":"","type":"normal","user":{"userId":"PRIVATE_USER"}}}`,
		`{"note":{"noteId":"PRIVATE_ID","xsecToken":"PRIVATE_TOKEN","title":"","desc":"","type":"normal","user":{"userId":"PRIVATE_USER"}},"comments":{"list":null}}`,
		`{"note":{"noteId":"PRIVATE_ID","time":"PRIVATE_INVALID_VALUE"}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			logs := captureDetailReadLog(t)
			p := &fixtureFeedDetailPage{raw: raw}
			result, err := runBoundedFeedDetail(context.Background(), "PRIVATE_ID", "PRIVATE_TOKEN", func(ctx context.Context) feedDetailReadPage {
				p.ctx = ctx
				return p
			})
			require.Nil(t, result)
			require.Error(t, err)
			require.Contains(t, err.Error(), "stage=decode")
			require.False(t, strings.Contains(err.Error()+logs.String(), "PRIVATE_"))
		})
	}
}

func TestBoundedFeedDetailReadPreservesParentDeadlineAndCancellation(t *testing.T) {
	captureDetailReadLog(t)
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	deadline, _ := parent.Deadline()
	p := &fixtureFeedDetailPage{}
	_, err := runBoundedFeedDetail(parent, "PRIVATE_ID", "PRIVATE_TOKEN", func(ctx context.Context) feedDetailReadPage {
		actual, _ := ctx.Deadline()
		require.Equal(t, deadline, actual)
		p.ctx = ctx
		cancel()
		return p
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "category=canceled")
	require.Equal(t, []string{"navigate"}, p.calls)
}
