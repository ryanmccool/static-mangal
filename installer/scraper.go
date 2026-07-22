package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ryanmccool/static-mangal/filesystem"
	"github.com/ryanmccool/static-mangal/where"
)

type Scraper struct {
	Name        string
	URL         string
	SHA256      string
	Description string
	Contents    string
}

func (s *Scraper) Path() string {
	filename := fmt.Sprintf("%s.lua", s.Name)
	return filepath.Join(where.Sources(), filename)
}

func (s *Scraper) GithubURL() string {
	return s.URL
}

func (s *Scraper) download() error {
	if s.Contents != "" {
		return nil
	}
	if s.URL == "" || s.SHA256 == "" {
		return fmt.Errorf("scraper URL and digest must be set")
	}

	contents, err := get(s.URL)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(contents)
	if hex.EncodeToString(digest[:]) != s.SHA256 {
		return fmt.Errorf("scraper %s digest verification failed", s.Name)
	}
	s.Contents = string(contents)
	return nil
}

func (s *Scraper) Install() error {
	err := s.download()

	if err != nil {
		return err
	}

	return filesystem.Api().WriteFile(s.Path(), []byte(s.Contents), os.ModePerm)
}
