package xiaohongshu

import (
	"encoding/json"
	"fmt"

	"github.com/go-rod/rod"
)

// profileReadySnapshotJS returns only a request-matched, fully hydrated
// profile snapshot. The profile pathname binds the response to the requested
// user before any state fields are accepted.
const profileReadySnapshotJS = `userID => {
	const state = window.__INITIAL_STATE__?.user;
	const readValue = value => value?.value !== undefined ? value.value : value?._value;
	const requestedPath = "/user/profile/" + userID;
	const userFetchingStatus = readValue(state?.userFetchingStatus);
	const userNoteFetchingStatus = readValue(state?.userNoteFetchingStatus);
	const isFetchingNotes = readValue(state?.isFetchingNotes);
	const userPageData = readValue(state?.userPageData);
	const notes = readValue(state?.notes);
	if (window.location?.pathname !== requestedPath ||
		userFetchingStatus !== "resolved" ||
		!Array.isArray(userNoteFetchingStatus) || userNoteFetchingStatus[0] !== "resolved" ||
		!Array.isArray(isFetchingNotes) || isFetchingNotes[0] !== false ||
		!userPageData || typeof userPageData !== "object" || Array.isArray(userPageData) ||
		!userPageData.basicInfo || typeof userPageData.basicInfo.nickname !== "string" ||
		userPageData.basicInfo.nickname.length === 0 ||
		!Array.isArray(userPageData.interactions) ||
		!Array.isArray(notes) || notes.length === 0 || !notes.every(Array.isArray)) {
		return "";
	}
	return JSON.stringify({
		basicInfo: userPageData.basicInfo,
		interactions: userPageData.interactions,
		notes,
	});
}`

type rodProfileReadPage struct {
	page *rod.Page
}

// readReadySnapshot implements the existing bounded search read interface so
// the same parent-context poller is reused without changing its semantics.
func (p rodProfileReadPage) readReadySnapshot(userID string) (string, error) {
	result, err := p.page.Eval(profileReadySnapshotJS, userID)
	if err != nil {
		return "", err
	}
	return result.Value.String(), nil
}

type profileReadySnapshot struct {
	BasicInfo    UserBasicInfo      `json:"basicInfo"`
	Interactions []UserInteractions `json:"interactions"`
	Notes        [][]Feed           `json:"notes"`
}

func decodeProfileReadySnapshot(raw string) (*UserProfileResponse, error) {
	var snapshot profileReadySnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal profile snapshot: %w", err)
	}
	if snapshot.BasicInfo.Nickname == "" || snapshot.Interactions == nil || snapshot.Notes == nil || len(snapshot.Notes) == 0 {
		return nil, fmt.Errorf("profile snapshot is incomplete")
	}

	response := &UserProfileResponse{
		UserBasicInfo: snapshot.BasicInfo,
		Interactions:  snapshot.Interactions,
		Feeds:         make([]Feed, 0),
	}
	for _, feeds := range snapshot.Notes {
		if feeds == nil {
			return nil, fmt.Errorf("profile snapshot is incomplete")
		}
		response.Feeds = append(response.Feeds, feeds...)
	}
	return response, nil
}
