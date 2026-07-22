package installer

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"github.com/ryanmccool/static-mangal/key"
	"github.com/ryanmccool/static-mangal/network"
	"github.com/spf13/viper"
)

const manifestPublicKey = "MCowBQYDK2VwAyEAlCOsw5qSw/VQD71hMjTeaVhAZdx6FJ6knG+6IE5TImE="

type manifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Scrapers      []manifestEntry `json:"scrapers"`
}

type manifestEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func verifiedManifest() (*manifest, string, error) {
	owner := viper.GetString(key.InstallerUser)
	repo := viper.GetString(key.InstallerRepo)
	ref := viper.GetString(key.InstallerBranch)
	if owner == "" || repo == "" || ref == "" {
		return nil, "", fmt.Errorf("scraper registry owner, repository, and immutable revision must be configured")
	}

	baseURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", owner, repo, ref)
	payload, err := get(baseURL + "/manifest.json")
	if err != nil {
		return nil, "", err
	}
	signature, err := get(baseURL + "/manifest.sig")
	if err != nil {
		return nil, "", err
	}

	encodedKey, err := base64.StdEncoding.DecodeString(manifestPublicKey)
	if err != nil {
		return nil, "", fmt.Errorf("decode scraper manifest public key: %w", err)
	}
	keyValue, err := x509.ParsePKIXPublicKey(encodedKey)
	if err != nil {
		return nil, "", fmt.Errorf("parse scraper manifest public key: %w", err)
	}
	publicKey, ok := keyValue.(ed25519.PublicKey)
	if !ok || !ed25519.Verify(publicKey, payload, signature) {
		return nil, "", fmt.Errorf("scraper manifest signature verification failed")
	}

	var result manifest
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, "", fmt.Errorf("decode scraper manifest: %w", err)
	}
	if result.SchemaVersion != 1 {
		return nil, "", fmt.Errorf("unsupported scraper manifest schema version %d", result.SchemaVersion)
	}
	return &result, baseURL, nil
}

func get(url string) ([]byte, error) {
	response, err := network.Client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 10<<20))
}

func (entry manifestEntry) valid() bool {
	if entry.Name == "" || filepath.Base(entry.Path) != entry.Name+".lua" || filepath.Dir(entry.Path) != "scrapers" {
		return false
	}
	if len(entry.SHA256) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(entry.SHA256)
	return err == nil
}
