package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/xpzouying/xiaohongshu-mcp/errors"
)

type FeedsListAction struct {
	read browserReadAction
}

func NewFeedsListAction(page *rod.Page) *FeedsListAction {
	return &FeedsListAction{read: newBrowserReadAction(readOperationListFeeds, page)}
}

// GetFeedsList 获取页面的 Feed 列表数据
func (f *FeedsListAction) GetFeedsList(ctx context.Context) ([]Feed, error) {
	session := f.read.begin(ctx)
	defer session.close()

	if err := session.run(readStageNavigate, func(page *rod.Page) error {
		return page.Navigate("https://www.xiaohongshu.com")
	}); err != nil {
		return nil, err
	}
	if err := session.run(readStageWaitDOMStable, func(page *rod.Page) error {
		return page.WaitDOMStable(time.Second, 0)
	}); err != nil {
		return nil, err
	}
	if err := session.run(readStageSettle, func(page *rod.Page) error {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-page.GetContext().Done():
			return page.GetContext().Err()
		}
	}); err != nil {
		return nil, err
	}

	var result string
	if err := session.run(readStageExtract, func(page *rod.Page) error {
		remote, err := page.Eval(`() => {
		if (window.__INITIAL_STATE__ &&
		    window.__INITIAL_STATE__.feed &&
		    window.__INITIAL_STATE__.feed.feeds) {
			const feeds = window.__INITIAL_STATE__.feed.feeds;
			const feedsData = feeds.value !== undefined ? feeds.value : feeds._value;
			if (feedsData) {
				return JSON.stringify(feedsData);
			}
		}
		return "";
	}`)
		if err != nil {
			return err
		}
		result = remote.Value.String()
		if result == "" {
			return errors.ErrNoFeeds
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var feeds []Feed
	if err := session.run(readStageDecode, func(_ *rod.Page) error {
		if err := json.Unmarshal([]byte(result), &feeds); err != nil {
			return fmt.Errorf("failed to unmarshal feeds: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return feeds, nil
}
