package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-rod/rod"
)

type UserProfileAction struct {
	read browserReadAction
}

func NewUserProfileAction(page *rod.Page) *UserProfileAction {
	return &UserProfileAction{read: newBrowserReadAction(readOperationUserProfile, page)}
}

// UserProfile 获取用户基本信息及帖子
func (u *UserProfileAction) UserProfile(ctx context.Context, userID, xsecToken string) (*UserProfileResponse, error) {
	session := u.read.begin(ctx)
	defer session.close()

	searchURL := makeUserProfileURL(userID, xsecToken)
	if err := session.run(readStageNavigate, func(page *rod.Page) error {
		return page.Navigate(searchURL)
	}); err != nil {
		return nil, err
	}

	var snapshot string
	if err := session.run(readStageWaitInitialState, func(page *rod.Page) error {
		var err error
		snapshot, err = awaitSearchReady(session.ctx, rodProfileReadPage{page: page}, userID)
		return err
	}); err != nil {
		return nil, err
	}

	var response *UserProfileResponse
	if err := session.run(readStageDecode, func(_ *rod.Page) error {
		var err error
		response, err = decodeProfileReadySnapshot(snapshot)
		return err
	}); err != nil {
		return nil, err
	}
	return response, nil
}

// extractUserProfileData 从页面中提取用户资料数据的通用方法
func (u *UserProfileAction) extractUserProfileData(session *browserReadSession) (*UserProfileResponse, error) {
	if err := session.run(readStageWaitInitialState, func(page *rod.Page) error {
		return page.Wait(rod.Eval(`() => window.__INITIAL_STATE__ !== undefined`))
	}); err != nil {
		return nil, err
	}

	var userDataResult string
	if err := session.run(readStageExtractProfile, func(page *rod.Page) error {
		remote, err := page.Eval(`() => {
		if (window.__INITIAL_STATE__ &&
		    window.__INITIAL_STATE__.user &&
		    window.__INITIAL_STATE__.user.userPageData) {
			const userPageData = window.__INITIAL_STATE__.user.userPageData;
			const data = userPageData.value !== undefined ? userPageData.value : userPageData._value;
			if (data) {
				return JSON.stringify(data);
			}
		}
		return "";
	}`)
		if err != nil {
			return err
		}
		userDataResult = remote.Value.String()
		if userDataResult == "" {
			return fmt.Errorf("profile data is missing")
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// 2. 获取用户帖子：window.__INITIAL_STATE__.user.notes.value
	var notesResult string
	if err := session.run(readStageExtractNotes, func(page *rod.Page) error {
		remote, err := page.Eval(`() => {
		if (window.__INITIAL_STATE__ &&
		    window.__INITIAL_STATE__.user &&
		    window.__INITIAL_STATE__.user.notes) {
			const notes = window.__INITIAL_STATE__.user.notes;
			// 优先使用 value（getter），如果不存在则使用 _value（内部字段）
			const data = notes.value !== undefined ? notes.value : notes._value;
			if (data) {
				return JSON.stringify(data);
			}
		}
		return "";
	}`)
		if err != nil {
			return err
		}
		notesResult = remote.Value.String()
		if notesResult == "" {
			return fmt.Errorf("profile notes are missing")
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// 解析用户信息
	var userPageData struct {
		Interactions []UserInteractions `json:"interactions"`
		BasicInfo    UserBasicInfo      `json:"basicInfo"`
	}
	var notesFeeds [][]Feed
	if err := session.run(readStageDecode, func(_ *rod.Page) error {
		if err := json.Unmarshal([]byte(userDataResult), &userPageData); err != nil {
			return fmt.Errorf("failed to unmarshal userPageData: %w", err)
		}
		if err := json.Unmarshal([]byte(notesResult), &notesFeeds); err != nil {
			return fmt.Errorf("failed to unmarshal notes: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// 组装响应
	response := &UserProfileResponse{
		UserBasicInfo: userPageData.BasicInfo,
		Interactions:  userPageData.Interactions,
	}

	// 添加用户帖子（展平双重数组）
	for _, feeds := range notesFeeds {
		if len(feeds) != 0 {
			response.Feeds = append(response.Feeds, feeds...)
		}
	}

	return response, nil
}

func makeUserProfileURL(userID, xsecToken string) string {
	return fmt.Sprintf("https://www.xiaohongshu.com/user/profile/%s?xsec_token=%s&xsec_source=pc_note", userID, xsecToken)
}

func (u *UserProfileAction) GetMyProfileViaSidebar(ctx context.Context) (*UserProfileResponse, error) {
	session := u.read.begin(ctx)
	defer session.close()

	// 创建导航动作
	navigate := NewNavigate(session.page)

	// 通过侧边栏导航到个人主页
	if err := session.run(readStageNavigate, func(_ *rod.Page) error {
		return navigate.ToProfilePage(session.ctx)
	}); err != nil {
		return nil, err
	}

	// 等待页面加载完成并获取 __INITIAL_STATE__
	if err := session.run(readStageWaitStable, func(page *rod.Page) error {
		return page.WaitStable(time.Second)
	}); err != nil {
		return nil, err
	}

	return u.extractUserProfileData(session)
}
