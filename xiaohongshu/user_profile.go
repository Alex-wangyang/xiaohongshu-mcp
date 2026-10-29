package xiaohongshu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/xpzouying/xiaohongshu-mcp/humanize"
)

// ProfileTab 个人主页的子 tab。
type ProfileTab string

const (
	TabNotes     ProfileTab = "note"
	TabFavorites ProfileTab = "fav"
	TabLiked     ProfileTab = "liked"
)

// ParseProfileTab 解析 tab 名，空值默认为「笔记」。
func ParseProfileTab(s string) (ProfileTab, error) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "", "note", "notes", "笔记":
		return TabNotes, nil
	case "fav", "favorites", "favorite", "收藏":
		return TabFavorites, nil
	case "liked", "like", "点赞":
		return TabLiked, nil
	}
	return "", fmt.Errorf("未知的主页 tab %q，可选：note / fav / liked", s)
}

// tabLabel 子 tab 对应的页面文字。
var tabLabel = map[ProfileTab]string{
	TabNotes:     "笔记",
	TabFavorites: "收藏",
	TabLiked:     "点赞",
}

type UserProfileAction struct {
	read browserReadAction
}

func NewUserProfileAction(page *rod.Page) *UserProfileAction {
	return &UserProfileAction{read: newBrowserReadAction(readOperationUserProfile, page)}
}

// UserProfile 获取用户基本信息及指定 tab 下的帖子。
func (u *UserProfileAction) UserProfile(ctx context.Context, userID, xsecToken string, tab ProfileTab) (*UserProfileResponse, error) {
	session := u.read.begin(ctx)
	defer session.close()

	if err := session.run(readStagePrepareFilters, func(_ *rod.Page) error {
		parsed, err := ParseProfileTab(string(tab))
		if err != nil {
			return &invalidReadInputError{cause: err}
		}
		tab = parsed
		return nil
	}); err != nil {
		return nil, err
	}

	searchURL := makeUserProfileURL(userID, xsecToken, tab)
	if err := session.run(readStageNavigate, func(page *rod.Page) error {
		return page.Navigate(searchURL)
	}); err != nil {
		return nil, err
	}

	return readProfileSnapshot(session, userID, tab)
}

func readProfileSnapshot(session *browserReadSession, userID string, tab ProfileTab) (*UserProfileResponse, error) {
	var snapshot string
	if err := session.run(readStageWaitInitialState, func(page *rod.Page) error {
		var err error
		snapshot, err = awaitSearchReady(session.ctx, rodProfileReadPage{page: page, tab: tab}, userID)
		return err
	}); err != nil {
		return nil, err
	}

	var response *UserProfileResponse
	if err := session.run(readStageDecode, func(_ *rod.Page) error {
		var err error
		response, err = decodeProfileReadySnapshot(snapshot, tab)
		return err
	}); err != nil {
		return nil, err
	}
	return response, nil
}

func makeUserProfileURL(userID, xsecToken string, tab ProfileTab) string {
	url := fmt.Sprintf("https://www.xiaohongshu.com/user/profile/%s?xsec_token=%s&xsec_source=pc_note", userID, xsecToken)
	if tab != "" && tab != TabNotes {
		url += fmt.Sprintf("&tab=%s&subTab=note", tab)
	}
	return url
}

func (u *UserProfileAction) GetMyProfileViaSidebar(ctx context.Context, tab ProfileTab) (*UserProfileResponse, error) {
	session := u.read.begin(ctx)
	defer session.close()

	if err := session.run(readStagePrepareFilters, func(_ *rod.Page) error {
		parsed, err := ParseProfileTab(string(tab))
		if err != nil {
			return &invalidReadInputError{cause: err}
		}
		tab = parsed
		return nil
	}); err != nil {
		return nil, err
	}

	if err := session.run(readStageNavigate, func(page *rod.Page) error {
		if err := navigateToProfilePage(page, session.ctx); err != nil {
			return err
		}
		return selectProfileTab(session.ctx, page, tab)
	}); err != nil {
		return nil, err
	}

	var userID string
	if err := session.run(readStageExtractProfile, func(page *rod.Page) error {
		var err error
		userID, err = currentProfileUserID(page)
		return err
	}); err != nil {
		return nil, err
	}

	var snapshot string
	if err := session.run(readStageWaitInitialState, func(page *rod.Page) error {
		var err error
		snapshot, err = awaitSearchReady(session.ctx, rodProfileReadPage{page: page, tab: tab}, userID)
		return err
	}); err != nil {
		return nil, err
	}

	var response *UserProfileResponse
	if err := session.run(readStageDecode, func(_ *rod.Page) error {
		var err error
		response, err = decodeProfileReadySnapshot(snapshot, tab)
		return err
	}); err != nil {
		return nil, err
	}
	return response, nil
}

func navigateToProfilePage(page *rod.Page, ctx context.Context) error {
	if err := page.Navigate("https://www.xiaohongshu.com/explore"); err != nil {
		return err
	}
	if err := page.WaitLoad(); err != nil {
		return err
	}
	if _, err := page.Element(`div#app`); err != nil {
		return err
	}
	if err := page.WaitStable(time.Second); err != nil {
		return err
	}
	profileLink, err := page.Element(`div.main-container li.user.side-bar-component a.link-wrapper span.channel`)
	if err != nil {
		return err
	}
	humanize.Delay(ctx, humanize.BeforeClick)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := profileLink.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return err
	}
	return page.WaitLoad()
}

func currentProfileUserID(page *rod.Page) (string, error) {
	if err := page.Wait(rod.Eval(`() => window.location.pathname.startsWith("/user/profile/")`)); err != nil {
		return "", err
	}
	result, err := page.Eval(`() => window.location.pathname`)
	if err != nil {
		return "", err
	}
	const profilePathPrefix = "/user/profile/"
	pathname := result.Value.String()
	if !strings.HasPrefix(pathname, profilePathPrefix) {
		return "", fmt.Errorf("current page is not a user profile")
	}
	userID := strings.TrimPrefix(pathname, profilePathPrefix)
	if userID == "" || strings.Contains(userID, "/") {
		return "", fmt.Errorf("current profile path is incomplete")
	}
	return userID, nil
}

func selectProfileTab(ctx context.Context, page *rod.Page, tab ProfileTab) error {
	if tab == TabNotes {
		return nil
	}

	label := tabLabel[tab]
	elems, err := page.Elements(`.reds-tab-item.sub-tab-list`)
	if err != nil {
		return err
	}
	for _, elem := range elems {
		text, err := elem.Text()
		if err != nil || strings.TrimSpace(text) != label {
			continue
		}
		humanize.Delay(ctx, humanize.BeforeClick)
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := elem.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return err
		}
		return page.WaitStable(time.Second)
	}
	return fmt.Errorf("requested profile tab is unavailable")
}
