package mangadex

import (
	"fmt"
	"github.com/darylhjd/mangodex"
	"github.com/ryanmccool/static-mangal/key"
	"github.com/ryanmccool/static-mangal/source"
	"github.com/spf13/viper"
	"net/url"
	"strconv"
)

func (m *Mangadex) ChaptersOf(manga *source.Manga) ([]*source.Chapter, error) {
	if cached, ok := m.cache.chapters.Get(manga.URL).Get(); ok {
		for _, chapter := range cached {
			chapter.Manga = manga
		}

		return cached, nil
	}

	params := url.Values{}
	params.Set("limit", strconv.Itoa(500))
	ratings := []string{mangodex.Safe, mangodex.Suggestive}
	for _, rating := range ratings {
		params.Add("contentRating[]", rating)
	}

	if viper.GetBool(key.MangadexNSFW) {
		params.Add("contentRating[]", mangodex.Porn)
		params.Add("contentRating[]", mangodex.Erotica)
	}

	// scanlation group for the chapter
	params.Add("includes[]", mangodex.ScanlationGroupRel)
	params.Set("order[chapter]", "asc")

	var (
		chapters   []*source.Chapter
		currOffset = 0
	)

	language := viper.GetString(key.MangadexLanguage)

	for {
		params.Set("offset", strconv.Itoa(currOffset))
		list, err := m.client.Chapter.GetMangaChapters(manga.ID, params)
		if err != nil {
			return nil, err
		}

		for _, chapter := range list.Data {
			// Skip external chapters. Their pages cannot be downloaded.
			if chapter.Attributes.ExternalURL != nil && !viper.GetBool(key.MangadexShowUnavailableChapters) {
				continue
			}

			// skip chapters that are not in the current language
			if language != "any" && chapter.Attributes.TranslatedLanguage != language {
				continue
			}

			name := chapter.GetTitle()
			if name == "" {
				name = fmt.Sprintf("Chapter %s", chapter.GetChapterNum())
			} else {
				name = fmt.Sprintf("Chapter %s - %s", chapter.GetChapterNum(), name)
			}

			var volume string
			if chapter.Attributes.Volume != nil {
				volume = fmt.Sprintf("Vol.%s", *chapter.Attributes.Volume)
			}
			chapters = append(chapters, &source.Chapter{
				Name:   name,
				Index:  uint16(len(chapters) + 1),
				ID:     chapter.ID,
				URL:    fmt.Sprintf("https://mangadex.org/chapter/%s", chapter.ID),
				Manga:  manga,
				Volume: volume,
			})
		}
		currOffset += len(list.Data)
		if len(list.Data) == 0 || currOffset >= list.Total {
			break
		}

	}

	manga.Chapters = chapters
	_ = m.cache.chapters.Set(manga.URL, chapters)
	return chapters, nil
}
