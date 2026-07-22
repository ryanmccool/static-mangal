package mangadex

import (
	"strings"
	"testing"

	"github.com/ryanmccool/static-mangal/key"
	"github.com/ryanmccool/static-mangal/source"
	"github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
)

func TestMangadex_ChapterIndexes(t *testing.T) {
	previousLanguage := viper.GetString(key.MangadexLanguage)
	viper.Set(key.MangadexLanguage, "en")
	t.Cleanup(func() {
		viper.Set(key.MangadexLanguage, previousLanguage)
	})

	manga := &source.Manga{
		ID:  "296cbc31-af1a-4b5b-a34b-fee2b4cad542",
		URL: "https://mangadex.org/title/296cbc31-af1a-4b5b-a34b-fee2b4cad542",
	}
	chapters, err := New().ChaptersOf(manga)

	convey.Convey("Given Oshi no Ko's English MangaDex chapters", t, func() {
		convey.So(err, convey.ShouldBeNil)
		convey.So(chapters, convey.ShouldNotBeEmpty)
		convey.So(strings.HasPrefix(chapters[0].Name, "Chapter 1"), convey.ShouldBeTrue)

		convey.Convey("Then the chapters use contiguous one-based indexes", func() {
			for index, chapter := range chapters {
				convey.So(chapter.Index, convey.ShouldEqual, index+1)
			}
		})
	})
}
