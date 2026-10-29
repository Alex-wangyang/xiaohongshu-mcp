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

	var result string
	if err := session.run(readStageExtract, func(page *rod.Page) error {
		var err error
		result, err = awaitFeedList(session.ctx, func() (string, error) {
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
				return "", err
			}
			return remote.Value.String(), nil
		})
		return err
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

	return onlyNotes(feeds), nil
}

// awaitFeedList preserves the upstream hydration wait within the caller deadline.
func awaitFeedList(ctx context.Context, read func() (string, error)) (string, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if waitCtx.Err() != nil {
			return "", errors.ErrNoFeeds
		}
		result, err := read()
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if result != "" {
			return result, nil
		}
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", errors.ErrNoFeeds
		case <-timer.C:
		}
	}
}
