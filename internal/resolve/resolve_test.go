package resolve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
)

// fakeIndex knows formulae and casks by name.
type fakeIndex struct{ formulae, casks map[string]bool }

func (f fakeIndex) Lookup(_ context.Context, name string, gui bool) (config.InstallSpec, bool) {
	switch {
	case f.casks[name] && (gui || !f.formulae[name]):
		return config.InstallSpec{Cask: name}, true
	case f.formulae[name]:
		return config.InstallSpec{Brew: name}, true
	}

	return config.InstallSpec{}, false
}

func app(id string, debian config.InstallSpec) config.App {
	return config.App{ID: id, Install: map[string]config.InstallSpec{"debian": debian}}
}

func TestResolve(t *testing.T) {
	cat, err := catalog.Load()

	if err != nil {
		t.Fatal(err)
	}

	idx := fakeIndex{
		formulae: map[string]bool{"htop": true, "fd": true, "docker": true, "bat": true},
		casks:    map[string]bool{"docker": true, "obsidian": true},
	}
	r := &Resolver{PlatformKey: "macos", Catalog: cat, Index: idx}
	cases := []struct {
		app    config.App
		want   config.InstallSpec
		source string
	}{
		{app("htop", config.InstallSpec{Apt: "htop"}), config.InstallSpec{Brew: "htop"}, ""},
		{app("fd", config.InstallSpec{Apt: "fd-find"}), config.InstallSpec{Brew: "fd"}, SourceRename},
		{app("obsidian", config.InstallSpec{Snap: "obsidian"}), config.InstallSpec{Cask: "obsidian"}, SourceSameName},
		{app("docker-cli", config.InstallSpec{Apt: "docker"}), config.InstallSpec{Brew: "docker"}, SourceSameName},
	}

	for _, c := range cases {
		got, source, ok := r.Resolve(context.Background(), c.app)

		if !ok || got != c.want {
			t.Errorf("%s: got %+v %v", c.app.ID, got, ok)
		}

		if c.source != "" && source != c.source {
			t.Errorf("%s: source %q, want %q", c.app.ID, source, c.source)
		}
	}

	if _, _, ok := r.Resolve(context.Background(), app("nothing", config.InstallSpec{Apt: "nothing-here"})); ok {
		t.Error("resolved an unknown package")
	}
}

func TestResolvePrefersCatalog(t *testing.T) {
	cat, _ := catalog.Load()
	r := &Resolver{PlatformKey: "macos", Catalog: cat, Index: fakeIndex{}}
	spec, source, ok := r.Resolve(context.Background(), app("google-chrome", config.InstallSpec{Apt: "google-chrome-stable"}))

	if !ok || source != SourceCatalog || spec.Cask != "google-chrome" {
		t.Errorf("got %+v %s %v", spec, source, ok)
	}
}

func TestRepologyNamesAndCache(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)

		if r.Header.Get("User-Agent") == "" {
			t.Error("no user agent")
		}

		w.Write([]byte(`[{"repo":"ubuntu_24_04","binname":"foo-tools"},{"repo":"homebrew","srcname":"foo"}]`))
	}))
	defer srv.Close()

	rp := &Repology{BaseURL: srv.URL + "/", CacheDir: t.TempDir(), Client: srv.Client()}

	if got := rp.Names(context.Background(), "foo-tools", "macos"); len(got) != 1 || got[0] != "foo" {
		t.Errorf("names = %v", got)
	}

	rp.Names(context.Background(), "foo-tools", "debian")

	if hits.Load() != 1 {
		t.Errorf("cache not used: %d requests", hits.Load())
	}

	r := &Resolver{PlatformKey: "macos", Index: fakeIndex{formulae: map[string]bool{"foo": true}}, Repology: rp}
	spec, source, ok := r.Resolve(context.Background(), app("foo-tools", config.InstallSpec{Apt: "foo-tools"}))

	if !ok || source != SourceRepology || spec.Brew != "foo" {
		t.Errorf("got %+v %s %v", spec, source, ok)
	}
}

func TestParseBrewInfo(t *testing.T) {
	f, c := parseBrewInfo(`{"formulae":[{"name":"docker"}],"casks":[{"token":"docker"}]}`)

	if f != "docker" || c != "docker" {
		t.Errorf("parsed %q %q", f, c)
	}
}
