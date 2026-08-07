package anilist

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	api "github.com/ryanmccool/static-mangal/anilist"
	"github.com/ryanmccool/static-mangal/key"
	"github.com/ryanmccool/static-mangal/source"
	"github.com/spf13/viper"
)

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

type offlineHTTPClient struct {
	responseBody string
	requestBody  []byte
	body         *trackingBody
}

func (c *offlineHTTPClient) Do(request *http.Request) (*http.Response, error) {
	var err error
	c.requestBody, err = io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	c.body = &trackingBody{
		Reader: bytes.NewBufferString(c.responseBody),
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       c.body,
		Request:    request,
	}, nil
}

func TestLoginClosesResponseBodyAndDoesNotLogCredentials(t *testing.T) {
	const (
		clientID = "offline-client-id"
		secret   = "offline-client-secret"
		code     = "offline-oauth-code"
	)
	previous := map[string]any{
		key.AnilistID:     viper.Get(key.AnilistID),
		key.AnilistSecret: viper.Get(key.AnilistSecret),
		key.AnilistCode:   viper.Get(key.AnilistCode),
	}
	t.Cleanup(func() {
		for name, value := range previous {
			viper.Set(name, value)
		}
	})
	viper.Set(key.AnilistID, clientID)
	viper.Set(key.AnilistSecret, secret)
	viper.Set(key.AnilistCode, code)

	client := &offlineHTTPClient{responseBody: `{"access_token":"offline-access-token"}`}
	previousClient := loginClient
	previousLog := infoLog
	t.Cleanup(func() {
		loginClient = previousClient
		infoLog = previousLog
	})
	loginClient = client
	var messages []string
	infoLog = func(args ...interface{}) { messages = append(messages, fmt.Sprint(args...)) }

	if err := New().login(); err != nil {
		t.Fatal(err)
	}
	body := client.body
	if body == nil {
		t.Fatal("offline login client did not return a response body")
	}
	if !body.closed {
		t.Fatal("login response body was not closed")
	}
	logged := strings.Join(messages, "\n")
	for _, value := range []string{clientID, secret, code, string(client.requestBody)} {
		if strings.Contains(logged, value) {
			t.Fatalf("login log contains sensitive request data %q: %s", value, logged)
		}
	}
}

func TestMarkReadClosesResponseBodyAndDoesNotLogRequestBody(t *testing.T) {
	const token = "offline-mark-token"
	client := &offlineHTTPClient{responseBody: `{"data":{"SaveMediaListEntry":{"ID":42}}}`}
	previousClient := markClient
	previousFind := findClosestManga
	previousLog := infoLog
	t.Cleanup(func() {
		markClient = previousClient
		findClosestManga = previousFind
		infoLog = previousLog
	})
	markClient = client
	findClosestManga = func(string) (*api.Manga, error) { return &api.Manga{ID: 42}, nil }
	var messages []string
	infoLog = func(args ...interface{}) { messages = append(messages, fmt.Sprint(args...)) }

	integration := New()
	integration.token = token
	chapter := &source.Chapter{
		Index: 7,
		Manga: &source.Manga{Name: "offline manga"},
	}
	if err := integration.MarkRead(chapter); err != nil {
		t.Fatal(err)
	}
	body := client.body
	if body == nil {
		t.Fatal("offline mark client did not return a response body")
	}
	if !body.closed {
		t.Fatal("mark response body was not closed")
	}
	if len(client.requestBody) == 0 {
		t.Fatal("mark request body was not sent")
	}
	logged := strings.Join(messages, "\n")
	for _, value := range []string{token, string(client.requestBody)} {
		if strings.Contains(logged, value) {
			t.Fatalf("mark log contains request data %q: %s", value, logged)
		}
	}
}
