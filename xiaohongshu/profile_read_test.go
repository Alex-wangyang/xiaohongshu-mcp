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

func profileSnapshotValue(value interface{}, underscore bool) map[string]interface{} {
	key := "value"
	if underscore {
		key = "_value"
	}
	return map[string]interface{}{key: value}
}

func executeProfileReadySnapshotJS(t *testing.T, state interface{}, pathname, userID string, tab ProfileTab) string {
	t.Helper()
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	script := `const state = JSON.parse(process.argv[1]);
const pathname = process.argv[2];
const userID = process.argv[3];
const tab = process.argv[4];
globalThis.window = {location: {pathname}, __INITIAL_STATE__: state};
const result = (` + profileReadySnapshotJS + `)(userID, tab);
process.stdout.write(JSON.stringify(result));`
	output, err := exec.Command("node", "-e", script, string(encoded), pathname, userID, string(tab)).CombinedOutput()
	require.NoError(t, err, string(output))
	var result string
	require.NoError(t, json.Unmarshal(output, &result))
	return result
}

func profileReadyFixture(underscore bool, notes interface{}) map[string]interface{} {
	return profileReadyFixtureForTab(underscore, notes, 0, string(TabNotes))
}

func profileReadyFixtureForTab(underscore bool, notes interface{}, activeIndex int, query string) map[string]interface{} {
	return map[string]interface{}{"user": map[string]interface{}{
		"userFetchingStatus":     profileSnapshotValue("resolved", underscore),
		"userNoteFetchingStatus": profileSnapshotValue([]interface{}{"resolved", "resolved", "resolved"}, underscore),
		"isFetchingNotes":        profileSnapshotValue([]interface{}{false, false, false}, underscore),
		"activeTab": profileSnapshotValue(map[string]interface{}{
			"index": activeIndex,
			"query": query,
		}, underscore),
		"userPageData": profileSnapshotValue(map[string]interface{}{
			"basicInfo":    map[string]interface{}{"nickname": "PRIVATE_NICKNAME", "gender": 1},
			"interactions": []interface{}{},
		}, underscore),
		"notes": profileSnapshotValue(notes, underscore),
	}}
}

func profileReadyDirectPageDataFixture(notes interface{}) map[string]interface{} {
	state := profileReadyFixture(false, notes)
	state["user"].(map[string]interface{})["userPageData"] = map[string]interface{}{
		"result":       "success",
		"basicInfo":    map[string]interface{}{"nickname": "PRIVATE_NICKNAME", "gender": 1},
		"interactions": []interface{}{},
		"tags":         []interface{}{},
		"tabPublic":    map[string]interface{}{},
		"extraInfo":    map[string]interface{}{},
	}
	return state
}

func TestProfileReadySnapshotJSFixtures(t *testing.T) {
	const userID = "PRIVATE_USER_ID"
	const pathname = "/user/profile/" + userID
	validFeed := map[string]interface{}{"id": "PRIVATE_FEED_ID", "xsecToken": "PRIVATE_NOTE_TOKEN"}
	tests := []struct {
		name      string
		state     interface{}
		path      string
		wantReady bool
		wantNick  string
	}{
		{
			name:      "wrong_path",
			state:     profileReadyFixture(false, []interface{}{[]interface{}{}}),
			path:      "/user/profile/OTHER_PRIVATE_ID",
			wantReady: false,
		},
		{
			name: "loading_default_data",
			state: map[string]interface{}{"user": map[string]interface{}{
				"userFetchingStatus":     profileSnapshotValue("loading", false),
				"userNoteFetchingStatus": profileSnapshotValue([]interface{}{nil}, false),
				"isFetchingNotes":        profileSnapshotValue([]interface{}{true}, false),
				"userPageData": profileSnapshotValue(map[string]interface{}{
					"basicInfo": map[string]interface{}{"nickname": ""}, "interactions": []interface{}{},
				}, false),
				"notes": profileSnapshotValue([]interface{}{[]interface{}{}}, false),
			}},
			path:      pathname,
			wantReady: false,
		},
		{
			name: "notes_loading",
			state: func() interface{} {
				state := profileReadyFixture(false, []interface{}{[]interface{}{}})
				state["user"].(map[string]interface{})["userNoteFetchingStatus"] = profileSnapshotValue([]interface{}{"loading"}, false)
				state["user"].(map[string]interface{})["isFetchingNotes"] = profileSnapshotValue([]interface{}{true}, false)
				return state
			}(),
			path:      pathname,
			wantReady: false,
		},
		{
			name: "missing_fields",
			state: map[string]interface{}{"user": map[string]interface{}{
				"userFetchingStatus":     profileSnapshotValue("resolved", false),
				"userNoteFetchingStatus": profileSnapshotValue([]interface{}{"resolved"}, false),
				"isFetchingNotes":        profileSnapshotValue([]interface{}{false}, false),
				"userPageData": profileSnapshotValue(map[string]interface{}{
					"basicInfo": map[string]interface{}{"gender": 1}, "interactions": []interface{}{},
				}, false),
				"notes": profileSnapshotValue([]interface{}{[]interface{}{}}, false),
			}},
			path:      pathname,
			wantReady: false,
		},
		{
			name:      "valid_empty_first_group",
			state:     profileReadyFixture(false, []interface{}{[]interface{}{}}),
			path:      pathname,
			wantReady: true,
		},
		{
			name:      "direct_user_page_data",
			state:     profileReadyDirectPageDataFixture([]interface{}{[]interface{}{}}),
			path:      pathname,
			wantReady: true,
		},
		{
			name:      "valid_normal",
			state:     profileReadyFixture(false, []interface{}{[]interface{}{validFeed}, []interface{}{}}),
			path:      pathname,
			wantReady: true,
		},
		{
			name:      "underscore_values",
			state:     profileReadyFixture(true, []interface{}{[]interface{}{}}),
			path:      pathname,
			wantReady: true,
		},
		{
			name: "underscore_value_wins_over_direct_data",
			state: func() interface{} {
				state := profileReadyDirectPageDataFixture([]interface{}{[]interface{}{}})
				state["user"].(map[string]interface{})["userPageData"] = map[string]interface{}{
					"_value": map[string]interface{}{
						"basicInfo":    map[string]interface{}{"nickname": "PRIVATE_WRAPPED_NICKNAME"},
						"interactions": []interface{}{},
					},
					"basicInfo":    map[string]interface{}{"nickname": "PRIVATE_RAW_NICKNAME"},
					"interactions": []interface{}{},
				}
				return state
			}(),
			path:      pathname,
			wantReady: true,
			wantNick:  "PRIVATE_WRAPPED_NICKNAME",
		},
		{
			name: "value_precedence_over_stale_underscore",
			state: func() interface{} {
				state := profileReadyFixture(true, []interface{}{[]interface{}{validFeed}})
				user := state["user"].(map[string]interface{})
				user["userFetchingStatus"] = map[string]interface{}{"value": "loading", "_value": "resolved"}
				user["userNoteFetchingStatus"] = map[string]interface{}{"value": []interface{}{nil}, "_value": []interface{}{"resolved"}}
				user["isFetchingNotes"] = map[string]interface{}{"value": []interface{}{true}, "_value": []interface{}{false}}
				return state
			}(),
			path:      pathname,
			wantReady: false,
		},
		{
			name: "null_value_does_not_fallback_to_raw",
			state: func() interface{} {
				state := profileReadyDirectPageDataFixture([]interface{}{[]interface{}{}})
				state["user"].(map[string]interface{})["userPageData"] = map[string]interface{}{
					"value":        nil,
					"_value":       map[string]interface{}{"basicInfo": map[string]interface{}{"nickname": "PRIVATE_STALE_NICKNAME"}, "interactions": []interface{}{}},
					"basicInfo":    map[string]interface{}{"nickname": "PRIVATE_NICKNAME"},
					"interactions": []interface{}{},
				}
				return state
			}(),
			path:      pathname,
			wantReady: false,
		},
		{
			name: "malformed_value_does_not_fallback_to_raw",
			state: func() interface{} {
				state := profileReadyDirectPageDataFixture([]interface{}{[]interface{}{}})
				state["user"].(map[string]interface{})["userPageData"] = map[string]interface{}{
					"value":        map[string]interface{}{"unexpected": true},
					"_value":       map[string]interface{}{"basicInfo": map[string]interface{}{"nickname": "PRIVATE_STALE_NICKNAME"}, "interactions": []interface{}{}},
					"basicInfo":    map[string]interface{}{"nickname": "PRIVATE_NICKNAME"},
					"interactions": []interface{}{},
				}
				return state
			}(),
			path:      pathname,
			wantReady: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := executeProfileReadySnapshotJS(t, test.state, test.path, userID, TabNotes)
			if !test.wantReady {
				require.Empty(t, result)
				return
			}
			var snapshot profileReadySnapshot
			require.NoError(t, json.Unmarshal([]byte(result), &snapshot))
			require.NotEmpty(t, snapshot.BasicInfo.Nickname)
			require.NotNil(t, snapshot.Interactions)
			require.NotNil(t, snapshot.Feeds)
			require.Equal(t, 0, snapshot.Index)
			if test.wantNick != "" {
				require.Equal(t, test.wantNick, snapshot.BasicInfo.Nickname)
			}
		})
	}
}

func TestUserProfileTokenDoesNotEnterProfileReadinessSnapshot(t *testing.T) {
	const userID = "PRIVATE_USER_ID"
	result := executeProfileReadySnapshotJS(t, profileReadyFixture(false, []interface{}{[]interface{}{}}), "/user/profile/"+userID, userID, TabNotes)
	require.NotContains(t, result, "PRIVATE_NOTE_TOKEN")
	require.Contains(t, makeUserProfileURL(userID, "PRIVATE_PROFILE_TOKEN", TabNotes), "xsec_token=PRIVATE_PROFILE_TOKEN")
}

func TestProfileReadySnapshotSelectsOnlyRequestedTab(t *testing.T) {
	const userID = "PRIVATE_USER_ID"
	feeds := []interface{}{
		[]interface{}{map[string]interface{}{"id": "PRIVATE_NOTE_FEED"}},
		[]interface{}{map[string]interface{}{"id": "PRIVATE_FAVORITE_FEED"}},
		[]interface{}{map[string]interface{}{"id": "PRIVATE_LIKED_FEED"}},
	}
	for _, test := range []struct {
		tab      ProfileTab
		index    int
		query    string
		wantID   string
		otherIDs []string
	}{
		{TabFavorites, 1, "fav", "PRIVATE_FAVORITE_FEED", []string{"PRIVATE_NOTE_FEED", "PRIVATE_LIKED_FEED"}},
		{TabLiked, 2, "liked", "PRIVATE_LIKED_FEED", []string{"PRIVATE_NOTE_FEED", "PRIVATE_FAVORITE_FEED"}},
	} {
		raw := executeProfileReadySnapshotJS(t, profileReadyFixtureForTab(false, feeds, test.index, test.query), "/user/profile/"+userID, userID, test.tab)
		require.NotEmpty(t, raw)
		var snapshot profileReadySnapshot
		require.NoError(t, json.Unmarshal([]byte(raw), &snapshot))
		require.Equal(t, test.index, snapshot.Index)
		require.Equal(t, test.query, snapshot.Query)
		require.Len(t, snapshot.Feeds, 1)
		require.Equal(t, test.wantID, snapshot.Feeds[0].ID)
		for _, otherID := range test.otherIDs {
			require.NotContains(t, raw, otherID)
		}
		result, err := decodeProfileReadySnapshot(raw, test.tab)
		require.NoError(t, err)
		require.Equal(t, test.wantID, result.Feeds[0].ID)
	}
}

func TestProfileReadySnapshotRejectsUnresolvedOrWrongRequestedTab(t *testing.T) {
	const userID = "PRIVATE_USER_ID"
	feeds := []interface{}{[]interface{}{}, []interface{}{}, []interface{}{}}
	unresolved := profileReadyFixtureForTab(false, feeds, 1, "fav")
	unresolved["user"].(map[string]interface{})["userNoteFetchingStatus"] = profileSnapshotValue([]interface{}{"resolved", "loading", "resolved"}, false)
	unresolved["user"].(map[string]interface{})["isFetchingNotes"] = profileSnapshotValue([]interface{}{false, true, false}, false)
	require.Empty(t, executeProfileReadySnapshotJS(t, unresolved, "/user/profile/"+userID, userID, TabFavorites))

	wrongTab := profileReadyFixtureForTab(false, feeds, 2, "liked")
	require.Empty(t, executeProfileReadySnapshotJS(t, wrongTab, "/user/profile/"+userID, userID, TabFavorites))
}

func TestProfileReadyDecodeKeepsOnlySelectedGroupAndEmptyFeedsNonNil(t *testing.T) {
	result, err := decodeProfileReadySnapshot(`{"basicInfo":{"nickname":"PRIVATE_NICKNAME"},"interactions":[],"index":1,"query":"fav","feeds":[{"id":"PRIVATE_FEED_ID"}]}`, TabFavorites)
	require.NoError(t, err)
	require.NotNil(t, result.Feeds)
	require.Len(t, result.Feeds, 1)
	require.Equal(t, "PRIVATE_FEED_ID", result.Feeds[0].ID)

	result, err = decodeProfileReadySnapshot(`{"basicInfo":{"nickname":"PRIVATE_NICKNAME"},"interactions":[],"index":0,"query":"note","feeds":[]}`, TabNotes)
	require.NoError(t, err)
	require.NotNil(t, result.Feeds)
	require.Empty(t, result.Feeds)

	for _, test := range []struct {
		raw string
		tab ProfileTab
	}{
		{`{"basicInfo":{"nickname":"PRIVATE_NICKNAME"},"interactions":[],"index":1,"query":"fav","feeds":null}`, TabFavorites},
		{"", TabNotes},
		{`{"basicInfo":{}}`, TabNotes},
		{`{"basicInfo":{"nickname":"PRIVATE_NICKNAME"},"interactions":null,"index":0,"query":"note","feeds":[]}`, TabNotes},
		{`{"basicInfo":{"nickname":"PRIVATE_NICKNAME"},"interactions":[],"index":2,"query":"liked","feeds":[]}`, TabFavorites},
		{`{"basicInfo":{"nickname":"PRIVATE_NICKNAME"},"interactions":[],"index":1,"query":"liked","feeds":[]}`, TabFavorites},
	} {
		_, err := decodeProfileReadySnapshot(test.raw, test.tab)
		require.Error(t, err)
	}
}

type fixtureProfileReadyPage struct {
	values []string
	calls  int
	err    error
}

func (p *fixtureProfileReadyPage) readReadySnapshot(_ string) (string, error) {
	if p.err != nil {
		p.calls++
		return "", p.err
	}
	index := p.calls
	if index >= len(p.values) {
		index = len(p.values) - 1
	}
	p.calls++
	return p.values[index], nil
}

func TestProfileReadyBrowserErrorIsRedacted(t *testing.T) {
	var logs bytes.Buffer
	logger := logrus.StandardLogger()
	previousOutput, previousFormatter := logger.Out, logger.Formatter
	logger.SetOutput(&logs)
	logger.SetFormatter(&logrus.JSONFormatter{DisableTimestamp: true})
	t.Cleanup(func() {
		logger.SetOutput(previousOutput)
		logger.SetFormatter(previousFormatter)
	})

	session := testBrowserReadSession(readOperationUserProfile, context.Background())
	page := &fixtureProfileReadyPage{err: stderrors.New("PRIVATE_USER_ID PRIVATE_PROFILE_TOKEN")}
	err := session.run(readStageWaitInitialState, func(_ *rod.Page) error {
		_, waitErr := awaitSearchReady(session.ctx, page, "PRIVATE_USER_ID")
		return waitErr
	})

	var readErr *browserReadError
	require.ErrorAs(t, err, &readErr)
	require.Equal(t, readFailureBrowser, readErr.category)
	require.NotContains(t, err.Error()+logs.String(), "PRIVATE_")
	require.Nil(t, stderrors.Unwrap(err))
}

func TestProfileReadyTimeoutAndCancellationStayRedacted(t *testing.T) {
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
			session := testBrowserReadSession(readOperationUserProfile, parent)
			page := &fixtureProfileReadyPage{values: []string{""}}
			err := session.run(readStageWaitInitialState, func(_ *rod.Page) error {
				_, waitErr := awaitSearchReady(session.ctx, page, "PRIVATE_USER_ID")
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
