package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// MR is an open merge request (GitLab) or pull request (GitHub, Gitea) on a
// forge.
type MR struct {
	URL          string    `json:"url"`
	Project      string    `json:"project"` // owner/repo path on the forge
	Ref          string    `json:"ref"`     // !123 or #123
	Title        string    `json:"title"`
	SourceBranch string    `json:"source_branch"`
	Draft        bool      `json:"draft"`
	Status       string    `json:"status"` // short merge status, may be empty
	Updated      time.Time `json:"updated"`
}

// Forge is one forge a profile's merge requests are fetched from, through the
// forge's own CLI so reserva never handles tokens.
type Forge struct {
	Kind   string   `toml:"kind"`  // gitlab (glab), github (gh) or gitea (tea)
	Host   string   `toml:"host"`  // e.g. gitlab.com
	Login  string   `toml:"login"` // tea login name; GLAB_CONFIG_DIR / GH_CONFIG_DIR for glab / gh
	Scopes []string `toml:"scopes"`
}

var forgeScopes = []string{"author", "assignee", "reviewer"}

// validate checks a forge entry and fills in defaults.
func (f *Forge) validate() error {
	switch f.Kind {
	case "gitlab", "github", "gitea":
	default:
		return fmt.Errorf("forge kind %q is not supported (use gitlab, github or gitea)", f.Kind)
	}
	if f.Host == "" {
		return fmt.Errorf("%s forge has no host", f.Kind)
	}
	if len(f.Scopes) == 0 {
		f.Scopes = []string{"author", "assignee"}
	}
	for _, s := range f.Scopes {
		known := false
		for _, k := range forgeScopes {
			known = known || s == k
		}
		if !known {
			return fmt.Errorf("%s forge: unknown scope %q (use author, assignee or reviewer)", f.Host, s)
		}
	}
	return nil
}

// fetchOpenMRs returns the open merge requests of every forge of a profile,
// newest first. MRs from forges that worked are returned even when another
// one failed.
func fetchOpenMRs(forges []Forge) ([]*MR, error) {
	seen := map[string]bool{}
	var out []*MR
	var errs []error
	for _, f := range forges {
		var mrs []*MR
		var err error
		switch f.Kind {
		case "gitlab":
			mrs, err = fetchGitLab(f)
		case "github":
			mrs, err = fetchGitHub(f)
		case "gitea":
			mrs, err = fetchGitea(f)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.Host, err))
		}
		for _, mr := range mrs {
			if !seen[mr.URL] {
				seen[mr.URL] = true
				out = append(out, mr)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, errors.Join(errs...)
}

// runForgeCLI runs a forge CLI and returns its stdout. It runs from the home
// directory so the CLI does not pick up the repository reserva was started in.
func runForgeCLI(env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir, _ = os.UserHomeDir()
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%s not found on PATH", name)
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%s: %s", name, lastLine(msg))
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

func lastLine(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// decodeStream decodes one or more concatenated JSON values, which is what
// paginated CLI output looks like.
func decodeStream[T any](data []byte) ([]T, error) {
	var out []T
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var v T
		if err := dec.Decode(&v); err == io.EOF {
			return out, nil
		} else if err != nil {
			return out, err
		}
		out = append(out, v)
	}
}

// decodeArrays decodes one or more concatenated JSON arrays into one slice.
func decodeArrays[T any](data []byte) ([]T, error) {
	pages, err := decodeStream[[]T](data)
	var out []T
	for _, p := range pages {
		out = append(out, p...)
	}
	return out, err
}

type gitlabMR struct {
	WebURL       string    `json:"web_url"`
	IID          int       `json:"iid"`
	Title        string    `json:"title"`
	SourceBranch string    `json:"source_branch"`
	Draft        bool      `json:"draft"`
	MergeStatus  string    `json:"detailed_merge_status"`
	HasConflicts bool      `json:"has_conflicts"`
	UpdatedAt    time.Time `json:"updated_at"`
	References   struct {
		Full string `json:"full"` // group/project!123
	} `json:"references"`
}

func fetchGitLab(f Forge) ([]*MR, error) {
	var env []string
	if f.Login != "" {
		env = append(env, "GLAB_CONFIG_DIR="+expandHome(f.Login))
	}
	glab := func(endpoint string) ([]byte, error) {
		return runForgeCLI(env, "glab", "api", "--hostname", f.Host, "--paginate", endpoint)
	}
	var out []*MR
	for _, scope := range f.Scopes {
		q := "merge_requests?state=opened&per_page=100&"
		switch scope {
		case "author":
			q += "scope=created_by_me"
		case "assignee":
			q += "scope=assigned_to_me"
		case "reviewer":
			data, err := runForgeCLI(env, "glab", "api", "--hostname", f.Host, "user")
			if err != nil {
				return out, err
			}
			var u struct {
				Username string `json:"username"`
			}
			if err := json.Unmarshal(data, &u); err != nil {
				return out, err
			}
			q += "scope=all&reviewer_username=" + url.QueryEscape(u.Username)
		}
		data, err := glab(q)
		if err != nil {
			return out, err
		}
		mrs, err := decodeArrays[gitlabMR](data)
		if err != nil {
			return out, fmt.Errorf("glab: %w", err)
		}
		for _, m := range mrs {
			project, _, _ := strings.Cut(m.References.Full, "!")
			out = append(out, &MR{
				URL:          m.WebURL,
				Project:      project,
				Ref:          fmt.Sprintf("!%d", m.IID),
				Title:        m.Title,
				SourceBranch: m.SourceBranch,
				Draft:        m.Draft,
				Status:       gitlabStatus(m),
				Updated:      m.UpdatedAt,
			})
		}
	}
	return out, nil
}

// gitlabStatus shortens detailed_merge_status to what is worth showing.
func gitlabStatus(m gitlabMR) string {
	if m.HasConflicts {
		return "conflicts"
	}
	switch m.MergeStatus {
	case "mergeable":
		return "mergeable"
	case "not_approved", "requested_changes":
		return "needs approval"
	case "conflict", "need_rebase":
		return "needs rebase"
	case "ci_must_pass", "ci_still_running":
		return "ci"
	case "discussions_not_resolved":
		return "discussions"
	}
	return ""
}

type githubPR struct {
	URL            string    `json:"url"`
	Number         int       `json:"number"`
	Title          string    `json:"title"`
	HeadRefName    string    `json:"headRefName"`
	IsDraft        bool      `json:"isDraft"`
	Mergeable      string    `json:"mergeable"`      // MERGEABLE, CONFLICTING or UNKNOWN
	ReviewDecision string    `json:"reviewDecision"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED or empty
	UpdatedAt      time.Time `json:"updatedAt"`
	Repository     struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

type githubPage struct {
	Data struct {
		Search struct {
			Nodes []githubPR `json:"nodes"`
		} `json:"search"`
	} `json:"data"`
}

// githubQuery searches pull requests across every repository in one request
// per page; gh api --paginate drives $endCursor.
const githubQuery = `query($q: String!, $endCursor: String) {
  search(query: $q, type: ISSUE, first: 100, after: $endCursor) {
    pageInfo { hasNextPage endCursor }
    nodes { ... on PullRequest {
      url number title headRefName isDraft mergeable reviewDecision updatedAt
      repository { nameWithOwner }
      commits(last: 1) { nodes { commit { statusCheckRollup { state } } } }
    } }
  }
}`

func fetchGitHub(f Forge) ([]*MR, error) {
	var env []string
	if f.Login != "" {
		env = append(env, "GH_CONFIG_DIR="+expandHome(f.Login))
	}
	var out []*MR
	for _, scope := range f.Scopes {
		qualifier := map[string]string{"author": "author:@me", "assignee": "assignee:@me", "reviewer": "review-requested:@me"}[scope]
		data, err := runForgeCLI(env, "gh", "api", "graphql", "--hostname", f.Host, "--paginate",
			"-f", "query="+githubQuery, "-f", "q=is:pr is:open archived:false "+qualifier)
		if err != nil {
			return out, err
		}
		pages, err := decodeStream[githubPage](data)
		if err != nil {
			return out, fmt.Errorf("gh: %w", err)
		}
		for _, page := range pages {
			for _, p := range page.Data.Search.Nodes {
				out = append(out, &MR{
					URL:          p.URL,
					Project:      p.Repository.NameWithOwner,
					Ref:          fmt.Sprintf("#%d", p.Number),
					Title:        p.Title,
					SourceBranch: p.HeadRefName,
					Draft:        p.IsDraft,
					Status:       githubStatus(p),
					Updated:      p.UpdatedAt,
				})
			}
		}
	}
	return out, nil
}

// githubStatus reduces mergeability, checks and review decision to one word.
func githubStatus(p githubPR) string {
	if p.Mergeable == "CONFLICTING" {
		return "conflicts"
	}
	if n := p.Commits.Nodes; len(n) > 0 && n[0].Commit.StatusCheckRollup != nil {
		switch n[0].Commit.StatusCheckRollup.State {
		case "PENDING", "EXPECTED", "FAILURE", "ERROR":
			return "ci"
		}
	}
	switch p.ReviewDecision {
	case "CHANGES_REQUESTED", "REVIEW_REQUIRED":
		return "needs approval"
	}
	if p.Mergeable == "MERGEABLE" {
		return "mergeable"
	}
	return ""
}

type giteaIssue struct {
	Number     int `json:"number"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

type giteaPull struct {
	HTMLURL   string    `json:"html_url"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Draft     bool      `json:"draft"`
	Mergeable bool      `json:"mergeable"`
	UpdatedAt time.Time `json:"updated_at"`
	Head      struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

func fetchGitea(f Forge) ([]*MR, error) {
	login := f.Login
	if login == "" {
		login = f.Host
	}
	tea := func(endpoint string) ([]byte, error) {
		return runForgeCLI(nil, "tea", "api", "--login", login, endpoint)
	}
	// The issue search spans every repository but leaves out the source
	// branch, so each hit is looked up as a pull request afterwards.
	const limit = 50
	seen := map[string]bool{}
	var out []*MR
	for _, scope := range f.Scopes {
		param := map[string]string{"author": "created", "assignee": "assigned", "reviewer": "review_requested"}[scope]
		for page := 1; ; page++ {
			data, err := tea(fmt.Sprintf("repos/issues/search?type=pulls&state=open&%s=true&limit=%d&page=%d", param, limit, page))
			if err != nil {
				return out, err
			}
			var hits []giteaIssue
			if err := json.Unmarshal(data, &hits); err != nil {
				return out, fmt.Errorf("tea: %w", err)
			}
			for _, h := range hits {
				key := fmt.Sprintf("%s#%d", h.Repository.FullName, h.Number)
				if seen[key] {
					continue
				}
				seen[key] = true
				data, err := tea(fmt.Sprintf("repos/%s/pulls/%d", h.Repository.FullName, h.Number))
				if err != nil {
					return out, err
				}
				var p giteaPull
				if err := json.Unmarshal(data, &p); err != nil {
					return out, fmt.Errorf("tea: %w", err)
				}
				status := ""
				if p.Mergeable {
					status = "mergeable"
				}
				out = append(out, &MR{
					URL:          p.HTMLURL,
					Project:      h.Repository.FullName,
					Ref:          fmt.Sprintf("#%d", p.Number),
					Title:        p.Title,
					SourceBranch: p.Head.Ref,
					Draft:        p.Draft,
					Status:       status,
					Updated:      p.UpdatedAt,
				})
			}
			if len(hits) < limit {
				break
			}
		}
	}
	return out, nil
}

// remoteProjects caches the forge project path of each repository directory,
// read from its origin remote.
var remoteProjects sync.Map

// remoteProject returns the owner/repo path of dir's origin remote, or "".
func remoteProject(dir string) string {
	if v, ok := remoteProjects.Load(dir); ok {
		return v.(string)
	}
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	p := ""
	if err == nil {
		p = projectFromRemote(strings.TrimSpace(string(out)))
	}
	remoteProjects.Store(dir, p)
	return p
}

// projectFromRemote extracts the project path from a git remote URL:
// git@host:group/repo.git, ssh://git@host:22/group/repo.git or
// https://host/group/repo.git all give group/repo.
func projectFromRemote(remote string) string {
	var path string
	if u, err := url.Parse(remote); err == nil && u.Scheme != "" && u.Host != "" {
		path = u.Path
	} else if _, after, ok := strings.Cut(remote, ":"); ok {
		path = after
	} else {
		return ""
	}
	path = strings.Trim(path, "/")
	return strings.TrimSuffix(path, ".git")
}

// applyMRs attaches open MRs to the sessions they belong to: those that
// recorded a link to the MR, and those on the MR's source branch in its
// project.
func applyMRs(groups []*Group, mrs []*MR) {
	byURL := map[string]*MR{}
	for _, mr := range mrs {
		byURL[mr.URL] = mr
	}
	for _, g := range groups {
		for _, s := range g.Sessions {
			s.MRs = nil
			for _, u := range s.PRLinks {
				if mr := byURL[u]; mr != nil {
					s.MRs = append(s.MRs, mr)
				}
			}
			if g.Project != "" && s.Branch != "" {
				for _, mr := range mrs {
					if mr.SourceBranch == s.Branch && strings.EqualFold(mr.Project, g.Project) && !hasMR(s.MRs, mr) {
						s.MRs = append(s.MRs, mr)
					}
				}
			}
			sort.Slice(s.MRs, func(i, j int) bool { return s.MRs[i].Updated.After(s.MRs[j].Updated) })
		}
	}
}

func hasMR(list []*MR, mr *MR) bool {
	for _, x := range list {
		if x == mr {
			return true
		}
	}
	return false
}

// openURL opens a link in the default browser.
func openURL(u string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, u)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// mrCache is the last fetched set of open MRs per profile, so they show up
// at startup before the forges have answered.
type mrCache struct {
	path     string
	Profiles map[string][]*MR `json:"profiles"`
}

func defaultMRCachePath() string {
	return filepath.Join(filepath.Dir(defaultStatePath()), "mrs.json")
}

func loadMRCache(path string) *mrCache {
	c := &mrCache{path: path, Profiles: map[string][]*MR{}}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, c)
	}
	if c.Profiles == nil {
		c.Profiles = map[string][]*MR{}
	}
	return c
}

func (c *mrCache) save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}
