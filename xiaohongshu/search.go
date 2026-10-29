package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/xpzouying/xiaohongshu-mcp/humanize"
)

type SearchResult struct {
	Search struct {
		Feeds FeedsValue `json:"feeds"`
	} `json:"search"`
}

// FilterOption 筛选选项结构体
type FilterOption struct {
	SortBy      string `json:"sort_by,omitempty" jsonschema:"排序依据: 综合|最新|最多点赞|最多评论|最多收藏,默认为'综合'"`
	NoteType    string `json:"note_type,omitempty" jsonschema:"笔记类型: 不限|视频|图文,默认为'不限'"`
	PublishTime string `json:"publish_time,omitempty" jsonschema:"发布时间: 不限|一天内|一周内|半年内,默认为'不限'"`
	SearchScope string `json:"search_scope,omitempty" jsonschema:"搜索范围: 不限|已看过|未看过|已关注,默认为'不限'"`
	Location    string `json:"location,omitempty" jsonschema:"位置距离: 不限|同城|附近,默认为'不限'"`
}

// filterGroup 面板上的一个筛选组：标签是什么、对应入参的哪个字段、允许哪些取值。
//
// 组和选项一律按文本定位，不用序号。面板里同一个选项可能渲染成多个 div.tags
// （数量随视口而变），首项是否重复各组也不一致，下标对不齐。
type filterGroup struct {
	label        string                    // 面板上这一组的标签文本
	defaultValue string                    // 页面初始选中的值，不需要重复点击
	pick         func(FilterOption) string // 从入参里取这一组的值
	allowed      []string                  // 合法取值；在打开页面之前就能挡掉写错的值
}

var filterGroups = []filterGroup{
	{"排序依据", "综合", func(f FilterOption) string { return f.SortBy },
		[]string{"综合", "最新", "最多点赞", "最多评论", "最多收藏"}},
	{"笔记类型", "不限", func(f FilterOption) string { return f.NoteType },
		[]string{"不限", "视频", "图文"}},
	{"发布时间", "不限", func(f FilterOption) string { return f.PublishTime },
		[]string{"不限", "一天内", "一周内", "半年内"}},
	{"搜索范围", "不限", func(f FilterOption) string { return f.SearchScope },
		[]string{"不限", "已看过", "未看过", "已关注"}},
	{"位置距离", "不限", func(f FilterOption) string { return f.Location },
		[]string{"不限", "同城", "附近"}},
}

// pendingFilter 一个待应用的筛选项。
type pendingFilter struct {
	group  string // 组标签
	option string // 选项文本
}

// collectFilters 把入参整理成待应用的筛选项，同一组以最后一个非空值为准。
//
// 校验放在这里是为了在打开浏览器之前就挡掉写错的值——否则要等导航、悬停、
// 在面板里找不到之后才能报错，等于为了说一句"你写错了"先向平台发一次请求。
func collectFilters(filters []FilterOption) ([]pendingFilter, error) {
	selected := make(map[string]string, len(filterGroups))

	for _, f := range filters {
		for _, g := range filterGroups {
			value := g.pick(f)
			if value == "" {
				continue
			}
			if !slices.Contains(g.allowed, value) {
				return nil, fmt.Errorf("%s 不支持 %q，可选：%s",
					g.label, value, strings.Join(g.allowed, "、"))
			}
			selected[g.label] = value
		}
	}

	var pending []pendingFilter
	for _, g := range filterGroups {
		value, ok := selected[g.label]
		if !ok || value == g.defaultValue {
			continue
		}
		pending = append(pending, pendingFilter{group: g.label, option: value})
	}

	return pending, nil
}

func collectSearchFilters(filters []FilterOption) ([]pendingFilter, error) {
	pending, err := collectFilters(filters)
	if err != nil {
		return nil, &invalidReadInputError{cause: err}
	}
	return pending, nil
}

type SearchAction struct {
	read browserReadAction
}

func NewSearchAction(page *rod.Page) *SearchAction {
	return &SearchAction{read: newBrowserReadAction(readOperationSearchFeeds, page)}
}

func (s *SearchAction) Search(ctx context.Context, keyword string, filters ...FilterOption) ([]Feed, error) {
	session := s.read.begin(ctx)
	defer session.close()

	var pending []pendingFilter
	if err := session.run(readStagePrepareFilters, func(_ *rod.Page) error {
		var err error
		pending, err = collectSearchFilters(filters)
		return err
	}); err != nil {
		return nil, err
	}

	searchURL := makeSearchURL(keyword)
	if err := session.run(readStageNavigate, func(page *rod.Page) error {
		if err := session.ctx.Err(); err != nil {
			return err
		}
		// Enable request observation before navigation. Keeping it enabled for
		// this page avoids toggling the domain between serialized filter clicks.
		if len(pending) > 0 {
			if err := (proto.NetworkEnable{}).Call(page); err != nil {
				return err
			}
		}
		return page.Navigate(searchURL)
	}); err != nil {
		return nil, err
	}
	if err := session.run(readStageWaitDOMStable, func(page *rod.Page) error {
		return page.WaitDOMStable(time.Second, 0)
	}); err != nil {
		return nil, err
	}
	if err := session.run(readStageSettle, func(_ *rod.Page) error {
		humanize.Delay(session.ctx, humanize.AfterNavigate)
		return session.ctx.Err()
	}); err != nil {
		return nil, err
	}

	var snapshot string
	if err := session.run(readStageWaitInitialState, func(page *rod.Page) error {
		var err error
		snapshot, err = awaitSearchReady(session.ctx, rodSearchReadPage{page: page}, keyword)
		return err
	}); err != nil {
		return nil, err
	}

	if len(pending) > 0 {

		if err := session.run(readStageOpenFilters, func(page *rod.Page) error {
			filterButton, err := page.Element(`div.filter`)
			if err != nil {
				return err
			}
			if err := session.ctx.Err(); err != nil {
				return err
			}
			if err := humanize.Hover(filterButton); err != nil {
				return err
			}
			humanize.Delay(session.ctx, humanize.BeforeClick)
			if err := session.ctx.Err(); err != nil {
				return err
			}
			return page.Wait(rod.Eval(`() => document.querySelector('div.filter-panel') !== null`))
		}); err != nil {
			return nil, err
		}

		if err := session.run(readStageApplyFilters, func(page *rod.Page) error {
			bounds, err := panelBounds(page)
			if err != nil {
				return err
			}
			if err := session.ctx.Err(); err != nil {
				return err
			}
			if err := humanize.MoveInto(page, bounds); err != nil {
				return fmt.Errorf("移入筛选面板失败: %w", err)
			}
			if err := session.ctx.Err(); err != nil {
				return err
			}

			// 用 ClickNoWait：筛选面板是 hover 浮层，rod 的 WaitInteractable 会误判被遮挡而死等。
			for _, pf := range pending {
				if err := session.ctx.Err(); err != nil {
					return err
				}
				option, err := findFilterOption(page, pf)
				if err != nil {
					return err
				}
				// 每轮重取：点完一项后面板可能重排。
				bounds, err = panelBounds(page)
				if err != nil {
					return err
				}
				humanize.Delay(session.ctx, humanize.BeforeClick)
				if err := session.ctx.Err(); err != nil {
					return err
				}
				if err := awaitFilteredSearchRequest(session.ctx, page, keyword, func() error {
					return humanize.ClickNoWaitInside(option, bounds)
				}); err != nil {
					return err
				}
				if err := session.ctx.Err(); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}

		if err := session.run(readStageWaitFilteredState, func(page *rod.Page) error {
			var err error
			snapshot, err = awaitSearchReady(session.ctx, rodSearchReadPage{page: page}, keyword)
			return err
		}); err != nil {
			return nil, err
		}
	}

	var feeds []Feed
	if err := session.run(readStageDecode, func(_ *rod.Page) error {
		return json.Unmarshal([]byte(snapshot), &feeds)
	}); err != nil {
		return nil, err
	}

	return onlyNotes(feeds), nil
}

// panelBounds 取筛选面板当前占据的矩形。
func panelBounds(page *rod.Page) (humanize.Rect, error) {
	panel, err := page.Element("div.filter-panel")
	if err != nil {
		return humanize.Rect{}, fmt.Errorf("读取筛选面板失败: %w", err)
	}
	shape, err := panel.Shape()
	if err != nil {
		return humanize.Rect{}, fmt.Errorf("读取筛选面板失败: %w", err)
	}
	if len(shape.Quads) == 0 {
		return humanize.Rect{}, fmt.Errorf("筛选面板没有可用区域")
	}
	q := shape.Quads[0]
	return humanize.Rect{Left: q[0], Top: q[1], Right: q[4], Bottom: q[5]}, nil
}

// findFilterOption 在筛选面板里定位一个选项：按标签找到组，再在组内按文本找选项。
//
// 全程不用序号。同一个选项在面板里可能渲染成多个 div.tags（数量随视口而变，
// 且首项是否重复各组不一致），下标对不齐；早前用 div.tags:nth-child(N) 会选错项。
// 同一个文本可能对应多个节点，只取其中可见的那个。
//
// 作用域必须限定在 div.filter-panel 内且只认 div.tags：页面别处存在同文本的
// 可见元素（顶部频道栏的「图文」「视频」、标签「综合」），放宽会点错地方。
func findFilterOption(page *rod.Page, pf pendingFilter) (*rod.Element, error) {
	groups, err := page.Elements("div.filter-panel div.filters")
	if err != nil {
		return nil, fmt.Errorf("读取筛选面板失败: %w", err)
	}

	for _, group := range groups {
		// 组标签是 div.filters 下的直接子 span
		label, err := group.Element(":scope > span")
		if err != nil {
			continue
		}
		text, err := label.Text()
		if err != nil || strings.TrimSpace(text) != pf.group {
			continue
		}

		options, err := group.Elements("div.tags")
		if err != nil {
			return nil, fmt.Errorf("读取「%s」的选项失败: %w", pf.group, err)
		}

		var available []string
		matched := false
		for _, opt := range options {
			t, err := opt.Text()
			if err != nil {
				continue
			}
			t = strings.TrimSpace(t)
			available = append(available, t)
			if t != pf.option {
				continue
			}

			matched = true
			if humanize.Visible(opt) {
				return opt, nil
			}
		}
		if matched {
			return nil, fmt.Errorf("「%s」里的选项「%s」当前不可见", pf.group, pf.option)
		}
		return nil, fmt.Errorf("「%s」里没有选项「%s」，页面上是：%s",
			pf.group, pf.option, strings.Join(available, "、"))
	}

	return nil, fmt.Errorf("筛选面板里没有「%s」这一组", pf.group)
}

func makeSearchURL(keyword string) string {

	values := url.Values{}
	values.Set("keyword", keyword)
	values.Set("source", "web_explore_feed")

	//https://www.xiaohongshu.com/search_result?keyword=%25E7%258E%258B%25E5%25AD%2590&source=web_search_result_notes
	//https://www.xiaohongshu.com/search_result?keyword=%25E7%258E%258B%25E5%25AD%2590&source=web_explore_feed
	return fmt.Sprintf("https://www.xiaohongshu.com/search_result?%s", values.Encode())
}
