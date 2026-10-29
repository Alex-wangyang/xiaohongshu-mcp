package xiaohongshu

import (
	"encoding/json"
	"fmt"

	"github.com/go-rod/rod"
)

// profileReadySnapshotJS returns only a request-matched, fully hydrated
// profile snapshot. Both the requested profile path and active tab must match
// before the selected feed group is returned.
const profileReadySnapshotJS = ` (userID, requestedTab) => {
	const state = window.__INITIAL_STATE__?.user;
	const readValue = value => value?.value !== undefined ? value.value : value?._value;
	const readUserPageData = value => {
		if (value === null || typeof value !== "object") return value;
		if (value.value !== undefined) return value.value;
		if (value._value !== undefined) return value._value;
		return value;
	};
	const tabIndexes = {note: 0, fav: 1, liked: 2};
	const tabIndex = tabIndexes[requestedTab];
	const requestedPath = "/user/profile/" + userID;
	const userFetchingStatus = readValue(state?.userFetchingStatus);
	const userNoteFetchingStatus = readValue(state?.userNoteFetchingStatus);
	const isFetchingNotes = readValue(state?.isFetchingNotes);
	const userPageData = readUserPageData(state?.userPageData);
	const notes = readValue(state?.notes);
	const activeTab = readValue(state?.activeTab);
	const activeIndex = activeTab?.index;
	const query = typeof activeTab?.query === "string" ? activeTab.query : "";
	if (window.location?.pathname !== requestedPath ||
		tabIndex === undefined ||
		userFetchingStatus !== "resolved" ||
		!Array.isArray(userNoteFetchingStatus) || userNoteFetchingStatus[tabIndex] !== "resolved" ||
		!Array.isArray(isFetchingNotes) || isFetchingNotes[tabIndex] !== false ||
		!userPageData || typeof userPageData !== "object" || Array.isArray(userPageData) ||
		!userPageData.basicInfo || typeof userPageData.basicInfo.nickname !== "string" ||
		userPageData.basicInfo.nickname.length === 0 ||
		!Array.isArray(userPageData.interactions) ||
		!Array.isArray(notes) || !Array.isArray(notes[tabIndex]) ||
		activeIndex !== tabIndex || (query !== "" && query !== requestedTab)) {
		return "";
	}
	return JSON.stringify({
		basicInfo: userPageData.basicInfo,
		interactions: userPageData.interactions,
		index: activeIndex,
		query,
		feeds: notes[tabIndex],
	});
}`

type rodProfileReadPage struct {
	page *rod.Page
	tab  ProfileTab
}

// readReadySnapshot implements the existing bounded search read interface so
// the same parent-context poller is reused without changing its semantics.
func (p rodProfileReadPage) readReadySnapshot(userID string) (string, error) {
	result, err := p.page.Eval(profileReadySnapshotJS, userID, string(p.tab))
	if err != nil {
		return "", err
	}
	return result.Value.String(), nil
}

type profileReadySnapshot struct {
	BasicInfo    UserBasicInfo      `json:"basicInfo"`
	Interactions []UserInteractions `json:"interactions"`
	Index        int                `json:"index"`
	Query        string             `json:"query"`
	Feeds        []Feed             `json:"feeds"`
}

func profileTabIndex(tab ProfileTab) (int, error) {
	switch tab {
	case TabNotes:
		return 0, nil
	case TabFavorites:
		return 1, nil
	case TabLiked:
		return 2, nil
	default:
		return 0, fmt.Errorf("unknown profile tab")
	}
}

func decodeProfileReadySnapshot(raw string, tab ProfileTab) (*UserProfileResponse, error) {
	parsedTab, err := ParseProfileTab(string(tab))
	if err != nil {
		return nil, fmt.Errorf("profile snapshot is incomplete")
	}
	expectedIndex, err := profileTabIndex(parsedTab)
	if err != nil {
		return nil, fmt.Errorf("profile snapshot is incomplete")
	}

	var snapshot profileReadySnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal profile snapshot: %w", err)
	}
	if snapshot.BasicInfo.Nickname == "" || snapshot.Interactions == nil || snapshot.Index != expectedIndex ||
		(snapshot.Query != "" && ProfileTab(snapshot.Query) != parsedTab) || snapshot.Feeds == nil {
		return nil, fmt.Errorf("profile snapshot is incomplete")
	}

	response := &UserProfileResponse{
		UserBasicInfo: snapshot.BasicInfo,
		Interactions:  snapshot.Interactions,
		Feeds:         append([]Feed{}, snapshot.Feeds...),
	}
	return response, nil
}
