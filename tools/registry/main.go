package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

var (
	namePattern       = regexp.MustCompile(`^@([a-z0-9][a-z0-9-]{0,38})/([a-z0-9][a-z0-9_-]{0,63})$`)
	repositoryPattern = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9-]+)/([A-Za-z0-9._-]+)$`)
)

type entry struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Repository  string    `json:"repository"`
	Subdir      string    `json:"subdir,omitempty"`
	Keywords    []string  `json:"keywords,omitempty"`
	Owners      []string  `json:"owners"`
	PublishedAt time.Time `json:"published_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type index struct {
	Version int     `json:"version"`
	Recipes []entry `json:"recipes"`
}

type recipeFile struct {
	Recipe struct {
		Name        string   `toml:"name"`
		Description string   `toml:"description"`
		Keywords    []string `toml:"keywords"`
	} `toml:"recipe"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: registry <publish|unpublish>")
		os.Exit(2)
	}
	message, err := run(os.Args[1])
	if err != nil {
		message = "❌ **Request rejected:** " + err.Error() + "\n\nEdit the issue to try again."
	}
	if resultFile := os.Getenv("RESULT_FILE"); resultFile != "" {
		os.WriteFile(resultFile, []byte(message+"\n"), 0o644)
	}
	fmt.Println(message)
	if err != nil {
		os.Exit(1)
	}
}

func run(action string) (string, error) {
	fields := parseIssue(os.Getenv("ISSUE_BODY"))
	author := os.Getenv("ISSUE_AUTHOR")
	name := strings.ToLower(strings.TrimSpace(fields["Recipe name"]))
	match := namePattern.FindStringSubmatch(name)
	if match == nil {
		return "", fmt.Errorf("`%s` is not a valid name, use `@scope/name` in lowercase", name)
	}
	scope := match[1]
	file := filepath.Join("recipes", scope, match[2]+".json")
	existing, err := readEntry(file)
	if err != nil {
		return "", err
	}
	if err := authorize(scope, author, existing); err != nil {
		return "", err
	}
	switch action {
	case "publish":
		return publish(name, file, fields, author, existing)
	case "unpublish":
		if existing == nil {
			return "", fmt.Errorf("`%s` is not in the registry", name)
		}
		if err := os.Remove(file); err != nil {
			return "", err
		}
		if err := rebuildIndex(); err != nil {
			return "", err
		}
		return fmt.Sprintf("🗑️ `%s` was removed from the registry.", name), nil
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}
}

func publish(name string, file string, fields map[string]string, author string, existing *entry) (string, error) {
	repository := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(fields["Repository"]), "/"), ".git")
	repositoryMatch := repositoryPattern.FindStringSubmatch(repository)
	if repositoryMatch == nil {
		return "", fmt.Errorf("repository must be a public GitHub URL like `https://github.com/owner/repo`")
	}
	subdir := strings.Trim(path.Clean("/"+strings.TrimSpace(fields["Subdirectory"])), "/")
	manifestPath := path.Join(subdir, "recipe.toml")
	content, err := githubContent(repositoryMatch[1], repositoryMatch[2], manifestPath)
	if err != nil {
		return "", err
	}
	var manifest recipeFile
	if err := toml.Unmarshal(content, &manifest); err != nil {
		return "", fmt.Errorf("`%s` is not valid TOML: %v", manifestPath, err)
	}
	if manifest.Recipe.Name != name {
		return "", fmt.Errorf("`%s` declares the name `%s`, expected `%s`", manifestPath, manifest.Recipe.Name, name)
	}
	description := strings.TrimSpace(fields["Description"])
	if description == "" {
		description = manifest.Recipe.Description
	}
	keywords := manifest.Recipe.Keywords
	if raw := strings.TrimSpace(fields["Keywords"]); raw != "" {
		keywords = nil
		for _, keyword := range strings.Split(raw, ",") {
			if trimmed := strings.ToLower(strings.TrimSpace(keyword)); trimmed != "" {
				keywords = append(keywords, trimmed)
			}
		}
	}
	now := time.Now().UTC()
	updated := entry{Name: name, Description: description, Repository: repository, Subdir: subdir, Keywords: keywords, Owners: []string{strings.ToLower(author)}, PublishedAt: now, UpdatedAt: now}
	verb := "published"
	if existing != nil {
		updated.PublishedAt = existing.PublishedAt
		updated.Owners = existing.Owners
		if !slices.Contains(updated.Owners, strings.ToLower(author)) {
			updated.Owners = append(updated.Owners, strings.ToLower(author))
		}
		verb = "updated"
	}
	if err := writeEntry(file, updated); err != nil {
		return "", err
	}
	if err := rebuildIndex(); err != nil {
		return "", err
	}
	return fmt.Sprintf("✅ `%s` was %s.\n\nInstall it with:\n\n```sh\nnwm server create my-server --recipe %s\n```\n\nVersions come from git tags like `v1.0.0` in %s.", name, verb, name, repository), nil
}

func authorize(scope string, author string, existing *entry) error {
	if strings.EqualFold(scope, author) {
		return nil
	}
	if existing != nil && slices.Contains(existing.Owners, strings.ToLower(author)) {
		return nil
	}
	status, err := githubStatus("https://api.github.com/orgs/" + url.PathEscape(scope) + "/public_members/" + url.PathEscape(author))
	if err != nil {
		return err
	}
	if status == http.StatusNoContent {
		return nil
	}
	return fmt.Errorf("@%s may not publish to the scope `@%s`, use your own login as scope or make your membership in that organization public", author, scope)
}

func parseIssue(body string) map[string]string {
	fields := map[string]string{}
	var current string
	var lines []string
	flush := func() {
		if current == "" {
			return
		}
		value := strings.TrimSpace(strings.Join(lines, "\n"))
		if value == "_No response_" {
			value = ""
		}
		fields[current] = value
	}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "### ") {
			flush()
			current = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			lines = nil
			continue
		}
		lines = append(lines, line)
	}
	flush()
	return fields
}

func githubContent(owner string, repository string, file string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+owner+"/"+repository+"/contents/"+file, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github.raw")
	authenticate(request)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("`%s` was not found in %s/%s, is the repository public and pushed?", file, owner, repository)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reading %s from GitHub: %s", file, response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 1<<20))
}

func githubStatus(address string) (int, error) {
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return 0, err
	}
	authenticate(request)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err
	}
	response.Body.Close()
	return response.StatusCode, nil
}

func authenticate(request *http.Request) {
	request.Header.Set("User-Agent", "nwm-registry")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}

func readEntry(file string) (*entry, error) {
	content, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var loaded entry
	return &loaded, json.Unmarshal(content, &loaded)
}

func writeEntry(file string, value entry) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(content, '\n'), 0o644)
}

func rebuildIndex() error {
	result := index{Version: 1, Recipes: []entry{}}
	err := filepath.WalkDir("recipes", func(file string, directory os.DirEntry, err error) error {
		if err != nil || directory.IsDir() || filepath.Ext(file) != ".json" {
			return err
		}
		loaded, err := readEntry(file)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		result.Recipes = append(result.Recipes, *loaded)
		return nil
	})
	if err != nil {
		return err
	}
	slices.SortFunc(result.Recipes, func(a, b entry) int { return strings.Compare(a.Name, b.Name) })
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("index.json", append(content, '\n'), 0o644)
}
