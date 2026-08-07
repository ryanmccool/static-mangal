package key

import "testing"

func TestIsSensitive(t *testing.T) {
	for _, name := range []string{AnilistSecret, AnilistCode} {
		if !IsSensitive(name) {
			t.Fatalf("IsSensitive(%q) = false, want true", name)
		}
	}

	for _, name := range []string{AnilistID, AnilistEnable, AnilistLinkOnMangaSelect, "anilist.secret.extra"} {
		if IsSensitive(name) {
			t.Fatalf("IsSensitive(%q) = true, want false", name)
		}
	}
}
