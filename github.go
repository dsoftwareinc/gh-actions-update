package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

// errNotFound is returned (wrapped) by githubAPI methods when the requested repository, release, commit or file
// does not exist.
var errNotFound = errors.New("not found")

type release struct {
	Tag       string
	Published time.Time
}

type commit struct {
	SHA  string
	Date time.Time
}

// githubAPI is the subset of the GitHub REST API the tool needs. repo is always in OWNER/REPO form.
type githubAPI interface {
	// CheckRepo returns an error wrapping errNotFound if the repository doesn't exist or isn't accessible.
	CheckRepo(repo string) error
	LatestRelease(repo string) (release, error)
	// Commit resolves ref (a commit SHA, tag or branch) to a commit.
	Commit(repo, ref string) (commit, error)
	// ListDir returns the paths of the files (not subdirectories) in dir on the default branch.
	ListDir(repo, dir string) ([]string, error)
	// GetFile returns the content of a file on the default branch and its blob SHA, needed to update it.
	GetFile(repo, path string) (content []byte, sha string, err error)
	// PutFile commits new content for an existing file to the default branch.
	PutFile(repo, path, message string, content []byte, sha string) error
}

// restClient is implemented by go-gh's *api.RESTClient and by anonymousClient.
type restClient interface {
	Do(method, path string, body io.Reader, response any) error
}

type restAPI struct {
	client restClient
}

const requestTimeout = 30 * time.Second

// newGitHubAPI returns an API client for host. Without a token it falls back to unauthenticated requests to
// github.com, which work for public repositories but are heavily rate limited.
func newGitHubAPI(token, host string) (githubAPI, error) {
	if token == "" {
		return &restAPI{client: anonymousClient{
			http:    &http.Client{Timeout: requestTimeout},
			baseURL: "https://api.github.com/",
		}}, nil
	}
	client, err := api.NewRESTClient(api.ClientOptions{Host: host, AuthToken: token, Timeout: requestTimeout})
	if err != nil {
		return nil, err
	}
	return &restAPI{client: client}, nil
}

func (r *restAPI) CheckRepo(repo string) error {
	var resp struct{}
	return wrapNotFound(r.client.Do(http.MethodGet, repoPath(repo), nil, &resp), http.StatusNotFound)
}

func (r *restAPI) LatestRelease(repo string) (release, error) {
	var resp struct {
		TagName     string     `json:"tag_name"`
		PublishedAt *time.Time `json:"published_at"`
		CreatedAt   time.Time  `json:"created_at"`
	}
	if err := r.client.Do(http.MethodGet, repoPath(repo)+"/releases/latest", nil, &resp); err != nil {
		return release{}, wrapNotFound(err, http.StatusNotFound)
	}
	published := resp.CreatedAt
	if resp.PublishedAt != nil {
		published = *resp.PublishedAt
	}
	return release{Tag: resp.TagName, Published: published}, nil
}

func (r *restAPI) Commit(repo, ref string) (commit, error) {
	var resp struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := r.client.Do(http.MethodGet, repoPath(repo)+"/commits/"+url.PathEscape(ref), nil, &resp); err != nil {
		// GitHub answers 422 "No commit found for SHA" for well-formed SHAs that don't exist.
		return commit{}, wrapNotFound(err, http.StatusNotFound, http.StatusUnprocessableEntity)
	}
	return commit{SHA: resp.SHA, Date: resp.Commit.Committer.Date}, nil
}

func (r *restAPI) ListDir(repo, dir string) ([]string, error) {
	var entries []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if err := r.client.Do(http.MethodGet, repoPath(repo)+"/contents/"+escapePath(dir), nil, &entries); err != nil {
		return nil, wrapNotFound(err, http.StatusNotFound)
	}
	var paths []string
	for _, e := range entries {
		if e.Type == "file" {
			paths = append(paths, e.Path)
		}
	}
	return paths, nil
}

func (r *restAPI) GetFile(repo, path string) ([]byte, string, error) {
	var resp struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		SHA      string `json:"sha"`
	}
	if err := r.client.Do(http.MethodGet, repoPath(repo)+"/contents/"+escapePath(path), nil, &resp); err != nil {
		return nil, "", wrapNotFound(err, http.StatusNotFound)
	}
	if resp.Encoding != "base64" {
		return nil, "", fmt.Errorf("%s: unsupported content encoding %q", path, resp.Encoding)
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return nil, "", fmt.Errorf("%s: decoding content: %w", path, err)
	}
	return content, resp.SHA, nil
}

func (r *restAPI) PutFile(repo, path, message string, content []byte, sha string) error {
	body, err := json.Marshal(map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"sha":     sha,
	})
	if err != nil {
		return err
	}
	return r.client.Do(http.MethodPut, repoPath(repo)+"/contents/"+escapePath(path), bytes.NewReader(body), nil)
}

func repoPath(repo string) string {
	return "repos/" + escapePath(repo)
}

// escapePath escapes each segment of a slash-separated path.
func escapePath(p string) string {
	segments := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

func wrapNotFound(err error, statuses ...int) error {
	var httpErr *api.HTTPError
	if errors.As(err, &httpErr) {
		for _, s := range statuses {
			if httpErr.StatusCode == s {
				return fmt.Errorf("%w: %w", errNotFound, err)
			}
		}
	}
	return err
}

// anonymousClient makes unauthenticated REST requests. go-gh's client refuses to run without a token.
type anonymousClient struct {
	http    *http.Client
	baseURL string
}

func (c anonymousClient) Do(method, path string, body io.Reader, response any) error {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "gh-actions-update")
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return api.HandleHTTPError(resp)
	}
	if response == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(response)
}
