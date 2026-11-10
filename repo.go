package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const workflowsDir = ".github/workflows"

// repository is where workflow files are read from and written to: a local clone or a remote GitHub repository.
// Workflow paths are slash-separated and relative to the repository root.
type repository interface {
	String() string
	IsRemote() bool
	WorkflowPaths() ([]string, error)
	// ReadWorkflow returns the content of a workflow, or an error wrapping fs.ErrNotExist if there is none.
	ReadWorkflow(path string) ([]byte, error)
	WriteWorkflow(path string, content []byte, commitMsg string) error
	// ResolveWorkflow turns a workflow given on the command line (a path or a bare file name) into a path.
	ResolveWorkflow(arg string) string
}

var remoteRepoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)

func isRemoteRepoName(s string) bool {
	return remoteRepoPattern.MatchString(s) && !strings.HasSuffix(s, "/.") && !strings.HasSuffix(s, "/..")
}

// openRepo opens a local directory (the enclosing git repository's root, if there is one) or an OWNER/REPO.
// getAPI is only called for remote repositories.
func openRepo(arg string, getAPI func() (githubAPI, error)) (repository, error) {
	if info, err := os.Stat(arg); err == nil && info.IsDir() {
		dir, err := filepath.Abs(arg)
		if err != nil {
			return nil, err
		}
		if root := findGitRoot(dir); root != "" {
			return localRepo{root: root}, nil
		}
		if !isRemoteRepoName(arg) {
			return localRepo{root: dir}, nil
		}
	}
	if !isRemoteRepoName(arg) {
		return nil, fmt.Errorf("%s is neither a local directory nor a repository in OWNER/REPO format", arg)
	}
	api, err := getAPI()
	if err != nil {
		return nil, err
	}
	if err := api.CheckRepo(arg); errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("repository %s not found, or the token can't access it", arg)
	} else if err != nil {
		return nil, err
	}
	return &remoteRepo{name: arg, api: api, shas: make(map[string]string)}, nil
}

// findGitRoot returns the closest directory at or above dir that contains .git, or "" if there is none.
func findGitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func isYAMLFile(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}

type localRepo struct {
	root string
}

func (r localRepo) String() string { return r.root }
func (r localRepo) IsRemote() bool { return false }

func (r localRepo) WorkflowPaths() ([]string, error) {
	entries, err := os.ReadDir(r.abs(workflowsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && isYAMLFile(e.Name()) {
			paths = append(paths, path.Join(workflowsDir, e.Name()))
		}
	}
	return paths, nil
}

func (r localRepo) abs(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(r.root, filepath.FromSlash(p))
}

func (r localRepo) ReadWorkflow(p string) ([]byte, error) {
	return os.ReadFile(r.abs(p))
}

func (r localRepo) WriteWorkflow(p string, content []byte, _ string) error {
	file := r.abs(p)
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	return os.WriteFile(file, content, info.Mode().Perm())
}

// ResolveWorkflow looks for arg relative to the current directory, then to the repository root, then in
// .github/workflows.
func (r localRepo) ResolveWorkflow(arg string) string {
	for _, candidate := range []string{arg, r.abs(arg), r.abs(path.Join(workflowsDir, filepath.ToSlash(arg)))} {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return candidate
		}
		if rel, err := filepath.Rel(r.root, abs); err == nil && filepath.IsLocal(rel) {
			return filepath.ToSlash(rel)
		}
		return abs
	}
	return arg
}

type remoteRepo struct {
	name string
	api  githubAPI

	mu   sync.Mutex
	shas map[string]string // path -> blob SHA of the content last read, needed to update the file
}

func (r *remoteRepo) String() string { return r.name }
func (r *remoteRepo) IsRemote() bool { return true }

func (r *remoteRepo) WorkflowPaths() ([]string, error) {
	files, err := r.api.ListDir(r.name, workflowsDir)
	if errors.Is(err, errNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, f := range files {
		if isYAMLFile(f) {
			paths = append(paths, f)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (r *remoteRepo) ReadWorkflow(p string) ([]byte, error) {
	content, sha, err := r.api.GetFile(r.name, p)
	if errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("%s in %s: %w", p, r.name, fs.ErrNotExist)
	}
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.shas[p] = sha
	r.mu.Unlock()
	return content, nil
}

func (r *remoteRepo) WriteWorkflow(p string, content []byte, commitMsg string) error {
	r.mu.Lock()
	sha, ok := r.shas[p]
	r.mu.Unlock()
	if !ok {
		if _, err := r.ReadWorkflow(p); err != nil {
			return err
		}
		r.mu.Lock()
		sha = r.shas[p]
		r.mu.Unlock()
	}
	return r.api.PutFile(r.name, p, commitMsg, content, sha)
}

func (r *remoteRepo) ResolveWorkflow(arg string) string {
	p := strings.TrimPrefix(filepath.ToSlash(arg), "./")
	if !strings.Contains(p, "/") {
		return path.Join(workflowsDir, p)
	}
	return p
}
