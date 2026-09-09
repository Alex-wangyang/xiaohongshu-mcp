package xiaohongshu

import (
	"context"
	"time"

	"github.com/go-rod/rod"
)

// searchReadySnapshotJS returns the request-matched feeds snapshot only after
// the search request has succeeded. Returning the serialized feeds from the
// same predicate avoids a readiness check followed by a stale extraction.
const searchReadySnapshotJS = `keyword => {
	const search = window.__INITIAL_STATE__?.search;
	const readValue = value => value?.value !== undefined ? value.value : value?._value;
	const state = readValue(search?.state);
	const searchValue = readValue(search?.searchValue);
	const feeds = readValue(search?.feeds);
	if (state !== "success" || searchValue !== keyword || !Array.isArray(feeds)) {
		return "";
	}
	return JSON.stringify(feeds);
}`

type searchReadPage interface {
	readReadySnapshot(keyword string) (string, error)
}

type rodSearchReadPage struct {
	page *rod.Page
}

func (p rodSearchReadPage) readReadySnapshot(keyword string) (string, error) {
	result, err := p.page.Eval(searchReadySnapshotJS, keyword)
	if err != nil {
		return "", err
	}
	return result.Value.String(), nil
}

// awaitSearchReady polls the browser's request-specific snapshot with the
// existing parent-bounded read context. Each successful value is extracted by
// the same JS predicate that establishes readiness.
func awaitSearchReady(ctx context.Context, page searchReadPage, keyword string) (string, error) {
	const pollInterval = 100 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		result, err := page.readReadySnapshot(keyword)
		if err != nil {
			return "", err
		}
		if result != "" {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			default:
				return result, nil
			}
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return "", ctx.Err()
		case <-timer.C:
		}
	}
}
