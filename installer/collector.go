package installer

import (
	"fmt"
	"path"
)

// Scrapers gets the signed manifest from the configured, immutable scraper revision.
func Scrapers() ([]*Scraper, error) {
	manifest, baseURL, err := verifiedManifest()
	if err != nil {
		return nil, err
	}

	scrapers := make([]*Scraper, 0, len(manifest.Scrapers))
	for _, entry := range manifest.Scrapers {
		if !entry.valid() {
			return nil, fmt.Errorf("invalid scraper manifest entry %q", entry.Name)
		}

		scrapers = append(scrapers, &Scraper{
			Name:   entry.Name,
			URL:    baseURL + "/" + path.Clean(entry.Path),
			SHA256: entry.SHA256,
		})
	}
	return scrapers, nil
}
