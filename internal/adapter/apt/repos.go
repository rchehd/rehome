package apt

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const sourcesDir = "/etc/apt/sources.list.d"

type Repo struct {
	Name     string
	File     string
	URIs     []string
	Keyrings []string
}

type Repos []Repo

var signedBy = regexp.MustCompile(`signed-by=([^\]\s]+)`)

// ListRepos reads third-party sources; the distribution's own sources are skipped.
func ListRepos() (Repos, error) {
	entries, err := os.ReadDir(sourcesDir)

	if err != nil {
		return nil, err
	}

	var repos Repos

	for _, e := range entries {
		name := e.Name()
		ext := filepath.Ext(name)

		if e.IsDir() || (ext != ".list" && ext != ".sources") || strings.HasPrefix(name, "ubuntu.") || strings.HasPrefix(name, "debian.") {
			continue
		}

		repo, err := parseRepo(filepath.Join(sourcesDir, name))

		if err != nil {
			return nil, err
		}

		if len(repo.URIs) > 0 {
			repos = append(repos, repo)
		}
	}

	return repos, nil
}

// NameFor returns the repo whose URIs serve the archive URI, or "" for official/unknown origins.
func (rs Repos) NameFor(uri string) string {
	if uri == "" || uri == dpkgOnly || IsOfficial(uri) {
		return ""
	}

	uri = strings.TrimSuffix(uri, "/")

	for _, r := range rs {
		for _, u := range r.URIs {
			if strings.HasPrefix(uri, strings.TrimSuffix(u, "/")) {
				return r.Name
			}
		}
	}

	return ""
}

func parseRepo(path string) (Repo, error) {
	data, err := os.ReadFile(path)

	if err != nil {
		return Repo{}, err
	}

	repo, err := parseRepoText(string(data), filepath.Ext(path) == ".sources")
	base := filepath.Base(path)
	repo.Name = strings.TrimSuffix(base, filepath.Ext(base))
	repo.File = path

	return repo, err
}

func parseRepoText(text string, deb822 bool) (Repo, error) {
	var repo Repo
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if deb822 {
			if v, ok := strings.CutPrefix(line, "URIs:"); ok {
				repo.URIs = append(repo.URIs, strings.Fields(v)...)
			}

			if v, ok := strings.CutPrefix(line, "Signed-By:"); ok && strings.TrimSpace(v) != "" {
				repo.Keyrings = append(repo.Keyrings, strings.TrimSpace(v))
			}

			continue
		}

		if !strings.HasPrefix(line, "deb ") {
			continue
		}

		if m := signedBy.FindStringSubmatch(line); m != nil {
			repo.Keyrings = append(repo.Keyrings, m[1])
		}

		for _, field := range strings.Fields(strings.TrimPrefix(line, "deb ")) {
			if strings.Contains(field, "://") {
				repo.URIs = append(repo.URIs, field)

				break
			}
		}
	}

	return repo, sc.Err()
}
