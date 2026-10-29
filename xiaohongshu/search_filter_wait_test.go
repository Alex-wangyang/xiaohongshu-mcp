package xiaohongshu

import (
	"github.com/go-rod/rod/lib/proto"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSearchFilterCompletionAllowsEmptyAndUnchangedResults(t *testing.T) {
	for _, name := range []string{"empty", "unchanged"} {
		t.Run(name, func(t *testing.T) {
			s := &searchRequestCompletion{keyword: "test"}
			s.sent(&proto.NetworkRequestWillBeSent{RequestID: "matching", Request: &proto.NetworkRequest{URL: "https://edith.xiaohongshu.com" + searchNotesPath, Method: "POST", PostData: `{"keyword":"test"}`}})
			s.response(&proto.NetworkResponseReceived{RequestID: "matching", Response: &proto.NetworkResponse{Status: 200}})
			require.True(t, s.finished(&proto.NetworkLoadingFinished{RequestID: "matching"}))
			require.NoError(t, s.result())
			// Readiness accepts both legal snapshots after request completion; no
			// note-ID change is required to reach the existing snapshot poller.
			feeds := []interface{}{}
			if name == "unchanged" {
				feeds = append(feeds, map[string]interface{}{"id": "same"})
			}
			require.NotEmpty(t, executeSearchReadySnapshotJS(t, map[string]interface{}{"search": map[string]interface{}{"state": map[string]interface{}{"value": "success"}, "searchValue": map[string]interface{}{"value": "test"}, "feeds": map[string]interface{}{"value": feeds}}}, "test"))
		})
	}
}

func TestSearchFilterCompletionRejectsUnrelatedAndFailedRequests(t *testing.T) {
	s := &searchRequestCompletion{keyword: "test"}
	s.sent(&proto.NetworkRequestWillBeSent{RequestID: "other", Request: &proto.NetworkRequest{URL: "https://edith.xiaohongshu.com" + searchNotesPath, Method: "POST", PostData: `{"keyword":"other"}`}})
	require.False(t, s.finished(&proto.NetworkLoadingFinished{RequestID: "other"}))
	require.Error(t, s.result())
	s.sent(&proto.NetworkRequestWillBeSent{RequestID: "matching", Request: &proto.NetworkRequest{URL: "https://edith.xiaohongshu.com" + searchNotesPath, Method: "POST", PostData: `{"keyword":"test"}`}})
	require.False(t, s.finished(&proto.NetworkLoadingFinished{RequestID: "other"}))
	s.response(&proto.NetworkResponseReceived{RequestID: "matching", Response: &proto.NetworkResponse{Status: 403}})
	require.True(t, s.finished(&proto.NetworkLoadingFinished{RequestID: "matching"}))
	require.Error(t, s.result())
	s.status = 200
	s.complete = false
	require.True(t, s.failure(&proto.NetworkLoadingFailed{RequestID: "matching"}))
	require.Error(t, s.result())
}
