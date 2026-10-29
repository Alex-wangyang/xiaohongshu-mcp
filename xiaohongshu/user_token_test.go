package xiaohongshu

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUserProfileTokenPreservedSeparatelyFromNoteToken(t *testing.T) {
	raw := []byte(`{"id":"fixture-note","xsecToken":"fixture-note-token","noteCard":{"user":{"userId":"fixture-author","xsecToken":"fixture-author-token"}}}`)
	var feed Feed
	require.NoError(t, json.Unmarshal(raw, &feed))
	require.Equal(t, "fixture-note-token", feed.XsecToken)
	require.Equal(t, "fixture-author-token", feed.NoteCard.User.XsecToken)
	encoded, err := json.Marshal(feed)
	require.NoError(t, err)
	var roundTrip Feed
	require.NoError(t, json.Unmarshal(encoded, &roundTrip))
	require.Equal(t, feed.NoteCard.User.XsecToken, roundTrip.NoteCard.User.XsecToken)
}

func TestUserProfileMissingTokenRemainsAbsent(t *testing.T) {
	encoded, err := json.Marshal(FeedUser{User: User{UserID: "fixture-author"}})
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.NotContains(t, fields, "xsecToken")
}

func TestUserProfileTokenDoesNotExpandUnrelatedUserPayloads(t *testing.T) {
	var user User
	require.NoError(t, json.Unmarshal([]byte(`{"userId":"fixture-author","xsecToken":"fixture-author-token"}`), &user))
	encoded, err := json.Marshal(user)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "xsecToken")
}
