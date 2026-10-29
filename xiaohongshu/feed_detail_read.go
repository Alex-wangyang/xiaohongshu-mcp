package xiaohongshu

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/go-rod/rod"
)

// The ordinary detail read never scrolls or clicks to load extra comments.
// Use only the requested note's ready state; network/DOM-wide idle is not
// required for a page which continues polling in the background.
type feedDetailReadPage interface {
	navigate(string) error
	waitReady(string) error
	extract(string) (string, error)
}

type rodFeedDetailReadPage struct{ page *rod.Page }

func (p rodFeedDetailReadPage) navigate(url string) error { return p.page.Navigate(url) }

func (p rodFeedDetailReadPage) waitReady(feedID string) error {
	return p.page.Wait(rod.Eval(`(id) => {
		const detail = window.__INITIAL_STATE__?.note?.noteDetailMap?.[id];
		const note = detail?.note;
		return note?.noteId === id &&
			typeof note.title === 'string' && typeof note.desc === 'string' &&
			typeof note.type === 'string' &&
			typeof note.xsecToken === 'string' && note.xsecToken.length > 0 &&
			typeof note.user?.userId === 'string' && note.user.userId.length > 0 &&
			Array.isArray(detail.comments?.list);
	}`, feedID))
}

func (p rodFeedDetailReadPage) extract(feedID string) (string, error) {
	result, err := p.page.Eval(`(id) => JSON.stringify(window.__INITIAL_STATE__?.note?.noteDetailMap?.[id] ?? null)`, feedID)
	if err != nil {
		return "", err
	}
	return result.Value.String(), nil
}

func (f *FeedDetailAction) getBoundedFeedDetail(ctx context.Context, feedID, xsecToken string) (*FeedDetailResponse, error) {
	return runBoundedFeedDetail(ctx, feedID, xsecToken, func(readCtx context.Context) feedDetailReadPage {
		return rodFeedDetailReadPage{page: f.page.Context(readCtx)}
	})
}

func runBoundedFeedDetail(parent context.Context, feedID, xsecToken string, newPage func(context.Context) feedDetailReadPage) (*FeedDetailResponse, error) {
	ctx, cancel := newBrowserReadContext(parent)
	defer cancel()
	session := &browserReadSession{operation: readOperationFeedDetail, ctx: ctx}
	page := newPage(ctx)
	if err := session.run(readStageNavigate, func(_ *rod.Page) error {
		return page.navigate(makeFeedDetailURL(feedID, xsecToken))
	}); err != nil {
		return nil, err
	}
	if err := session.run(readStageWaitInitialState, func(_ *rod.Page) error {
		return page.waitReady(feedID)
	}); err != nil {
		return nil, err
	}
	var raw string
	if err := session.run(readStageExtract, func(_ *rod.Page) error {
		var err error
		raw, err = page.extract(feedID)
		return err
	}); err != nil {
		return nil, err
	}
	var result FeedDetailResponse
	if err := session.run(readStageDecode, func(_ *rod.Page) error {
		// Detect missing fields separately from Go's valid empty/zero values.
		// A note ID can be hydrated before its content and initial comments.
		var contract struct {
			Note *struct {
				NoteID    string  `json:"noteId"`
				XsecToken string  `json:"xsecToken"`
				Title     *string `json:"title"`
				Desc      *string `json:"desc"`
				Type      *string `json:"type"`
				User      *struct {
					UserID string `json:"userId"`
				} `json:"user"`
			} `json:"note"`
			Comments *struct {
				List *[]json.RawMessage `json:"list"`
			} `json:"comments"`
		}
		if err := json.Unmarshal([]byte(raw), &contract); err != nil {
			return err
		}
		if contract.Note == nil || contract.Note.NoteID != feedID || feedID == "" ||
			contract.Note.XsecToken == "" || contract.Note.Title == nil ||
			contract.Note.Desc == nil || contract.Note.Type == nil ||
			contract.Note.User == nil || contract.Note.User.UserID == "" ||
			contract.Comments == nil || contract.Comments.List == nil {
			return errors.New("requested note is not ready")
		}
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return err
		}
		if result.Note.NoteID == "" || result.Note.NoteID != feedID {
			return errors.New("requested note is missing")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &result, nil
}
