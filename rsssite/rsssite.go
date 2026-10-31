package rsssite

import (
	"log"
	urlPkg "net/url"
	"strings"

	"github.com/dlclark/regexp2"

	"github.com/lingeringdawn/pan-fetcher/request"
	"github.com/mmcdole/gofeed"
)

type MagnetSite interface {
	GetMagnet(item *gofeed.Item) string
	GetMagnetItem(item *gofeed.Item) MagnetItem
}

type MagnetItem struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Magnet      string `json:"magnet"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

func getSite(url string) MagnetSite {
	name := url
	if strings.HasPrefix(url, "http") {
		urlObj, _ := urlPkg.Parse(url)
		name = urlObj.Host
	}
	switch name {
	case "mikanani.me", "mikanime.tv":
		return &Mikanani{}
	case "nyaa.si", "sukebei.nyaa.si":
		return &Nyaa{}
	case "share.dmhy.org":
		return &Dmhy{}
	case "share.acgnx.se", "share.acgnx.net", "www.acgnx.se":
		return &Acgnx{}
	case "acg.rip":
		return &Acgrip{}
	case "rsshub.app":
		return &Acgrip{}
	case "anibt.net":
		return &Anibt{}
	default:
		log.Printf("[error] not support site: [%s]. rss URL: %s\n", name, url)
		return nil
	}
}

func GetFeed(url string) *gofeed.Feed {
	res, err := request.Get(url, nil)
	if err != nil {
		log.Printf("[error] get rss from %s error: %s\n", url, err)
		return nil
	}
	feed, err := gofeed.NewParser().ParseString(res)
	if err != nil {
		log.Printf("[error] parse rss error: %s\n", err)
		return nil
	}
	return feed
}

func GetMagnetItemList(config *RssConfig) []MagnetItem {
	site := getSite(config.Url)
	if site == nil {
		return nil
	}
	feed := GetFeed(config.Url)
	if feed == nil {
		return nil
	}
	var itemList []MagnetItem
	var re *regexp2.Regexp
	if strings.HasPrefix(config.Filter, "/") && strings.HasSuffix(config.Filter, "/") {
		re = regexp2.MustCompile(config.Filter[1:len(config.Filter)-1], 0)
	}
	for _, item := range feed.Items {
		flag := true
		if config.Filter != "" {
			if re != nil {
				flag, _ = re.MatchString(item.Title)
			} else {
				flag = strings.Contains(item.Title, config.Filter)
			}
		}
		if !flag {
			continue
		}
		itemList = append(itemList, site.GetMagnetItem(item))
	}
	return itemList
}

// HasTorrentEnclosure reports whether an item advertises a torrent at all,
// whether or not it could be resolved.
//
// ProcessRSSFeed uses this to tell apart two cases that both end with "no magnet
// for this item":
//
//   - no torrent enclosure at all — a text-only entry, nothing to fetch
//   - a torrent enclosure that failed to resolve — a transient download problem,
//     worth retrying instead of dropping the episode for this cycle
func HasTorrentEnclosure(item *gofeed.Item) bool {
	for _, enclosure := range item.Enclosures {
		if enclosure.Type == "application/x-bittorrent" || strings.HasPrefix(enclosure.URL, "magnet:?") {
			return true
		}
	}
	return false
}

// GetMagnetByEnclosure returns a usable task URL from an item's enclosure.
//
// "Usable" means a magnet, or a .torrent URL the caller may still fetch. A
// torrent endpoint that could not be converted comes back from NormalizeTaskURL
// unchanged, and returning it is worse than returning nothing:
//
//   - 115 is handed an arbitrary URL and builds a task from it, so the same
//     episode lands in the account twice — once from the magnet that worked
//     earlier, once from this failed retry.
//   - The unconverted URL carries no info hash, so dedup cannot relate it to the
//     magnet either. Raw-key fallback gives it a distinct key, and every later
//     run sees something new.
//
// Returning "" leaves the item unrecorded instead, so a transient download
// failure is retried next cycle rather than turned into a duplicate.
func GetMagnetByEnclosure(item *gofeed.Item) string {
	if len(item.Enclosures) == 0 {
		return ""
	}
	// find enclosure by type == "application/x-bittorrent" or url has prefix magnet:?
	for _, enclosure := range item.Enclosures {
		if enclosure.Type != "application/x-bittorrent" && !strings.HasPrefix(enclosure.URL, "magnet:?") {
			continue
		}
		got := NormalizeTaskURL(enclosure.URL, item.Title)
		if got == "" {
			continue
		}
		lower := strings.ToLower(got)
		// A magnet, or a .torrent the caller can still fetch, is usable.
		// Anything else means the conversion failed.
		if strings.HasPrefix(lower, "magnet:") || strings.HasSuffix(lower, ".torrent") {
			return got
		}
		log.Printf("[feed] enclosure could not be converted to a magnet, skipping it: %s", enclosure.URL)
	}
	return ""
}
