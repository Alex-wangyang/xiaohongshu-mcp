package xiaohongshu

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func searchSnapshotValue(value interface{}, underscore bool) map[string]interface{} {
	key := "value"
	if underscore {
		key = "_value"
	}
	return map[string]interface{}{key: value}
}

func executeSearchReadySnapshotJS(t *testing.T, state interface{}, keyword string) string {
	t.Helper()
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	script := `const state = JSON.parse(process.argv[1]);
const keyword = process.argv[2];
globalThis.window = {__INITIAL_STATE__: state};
const result = (` + searchReadySnapshotJS + `)(keyword);
process.stdout.write(JSON.stringify(result));`
	output, err := exec.Command("node", "-e", script, string(encoded), keyword).CombinedOutput()
	require.NoError(t, err, string(output))
	var result string
	require.NoError(t, json.Unmarshal(output, &result))
	return result
}

func TestSearchReadySnapshotJSFixtures(t *testing.T) {
	keyword := "上海民宿"
	validFeeds := []interface{}{map[string]interface{}{
		"id": "PRIVATE_FEED_ID", "xsecToken": "PRIVATE_XSEC_TOKEN",
	}}
	tests := []struct {
		name     string
		state    interface{}
		expected string
	}{
		{
			name: "loading_placeholder_empty",
			state: map[string]interface{}{"search": map[string]interface{}{
				"state":       searchSnapshotValue("loading", false),
				"searchValue": searchSnapshotValue("", false),
				"feeds":       searchSnapshotValue([]interface{}{}, false),
			}},
			expected: "",
		},
		{
			name: "success_empty",
			state: map[string]interface{}{"search": map[string]interface{}{
				"state":       map[string]interface{}{"value": "success", "_value": "loading"},
				"searchValue": map[string]interface{}{"value": keyword, "_value": "OTHER_PRIVATE_KEYWORD"},
				"feeds":       map[string]interface{}{"value": []interface{}{}, "_value": validFeeds},
			}},
			expected: "[]",
		},
		{
			name: "wrong_keyword",
			state: map[string]interface{}{"search": map[string]interface{}{
				"state":       map[string]interface{}{"value": "success", "_value": "success"},
				"searchValue": map[string]interface{}{"value": "OTHER_PRIVATE_KEYWORD", "_value": keyword},
				"feeds":       searchSnapshotValue(validFeeds, false),
			}},
			expected: "",
		},
		{
			name: "underscore_values",
			state: map[string]interface{}{"search": map[string]interface{}{
				"state":       searchSnapshotValue("success", true),
				"searchValue": searchSnapshotValue(keyword, true),
				"feeds":       searchSnapshotValue([]interface{}{}, true),
			}},
			expected: "[]",
		},
		{
			name: "malformed",
			state: map[string]interface{}{"search": map[string]interface{}{
				"state":       searchSnapshotValue("success", false),
				"searchValue": searchSnapshotValue(keyword, false),
				"feeds":       map[string]interface{}{"value": map[string]interface{}{"items": validFeeds}, "_value": validFeeds},
			}},
			expected: "",
		},
		{
			name: "success_valid",
			state: map[string]interface{}{"search": map[string]interface{}{
				"state":       searchSnapshotValue("success", false),
				"searchValue": searchSnapshotValue(keyword, false),
				"feeds":       searchSnapshotValue(validFeeds, false),
			}},
			expected: `[{"id":"PRIVATE_FEED_ID","xsecToken":"PRIVATE_XSEC_TOKEN"}]`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := executeSearchReadySnapshotJS(t, test.state, keyword)
			require.Equal(t, test.expected, result)
		})
	}
}

type fixtureSearchReadPage struct {
	values []string
	calls  int
}

func (p *fixtureSearchReadPage) readReadySnapshot(_ string) (string, error) {
	index := p.calls
	if index >= len(p.values) {
		index = len(p.values) - 1
	}
	p.calls++
	return p.values[index], nil
}

func TestSearchReadyWaitsForHydratedSnapshotWithoutReextracting(t *testing.T) {
	page := &fixtureSearchReadPage{values: []string{"", "[]"}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result, err := awaitSearchReady(ctx, page, "上海民宿")
	require.NoError(t, err)
	require.Equal(t, "[]", result)
	require.Equal(t, 2, page.calls)
}

func TestSearchReadyTimeoutAndCancellationStayRedacted(t *testing.T) {
	for _, test := range []struct {
		name     string
		parent   func() (context.Context, context.CancelFunc)
		expected readFailureCategory
	}{
		{
			name: "timeout",
			parent: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			expected: readFailureTimeout,
		},
		{
			name: "cancellation",
			parent: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
			expected: readFailureCanceled,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := logrus.StandardLogger()
			previousOutput, previousFormatter := logger.Out, logger.Formatter
			logger.SetOutput(&logs)
			logger.SetFormatter(&logrus.JSONFormatter{DisableTimestamp: true})
			t.Cleanup(func() {
				logger.SetOutput(previousOutput)
				logger.SetFormatter(previousFormatter)
			})

			parent, cancel := test.parent()
			defer cancel()
			session := testBrowserReadSession(readOperationSearchFeeds, parent)
			page := &fixtureSearchReadPage{values: []string{""}}
			err := session.run(readStageWaitInitialState, func(_ *rod.Page) error {
				_, waitErr := awaitSearchReady(session.ctx, page, "PRIVATE_KEYWORD")
				return waitErr
			})

			var readErr *browserReadError
			require.ErrorAs(t, err, &readErr)
			require.Equal(t, test.expected, readErr.category)
			require.NotContains(t, err.Error()+logs.String(), "PRIVATE_")
			require.Nil(t, stderrors.Unwrap(err))
			require.True(t, strings.Contains(logs.String(), `"stage":"wait_initial_state"`))
		})
	}
}
