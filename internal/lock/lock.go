// Package lock holds installed app versions per platform, as nodes record them.
package lock

type Entry struct {
	Manager     string `yaml:"manager,omitempty"`
	Version     string `yaml:"version,omitempty"`
	Commit      string `yaml:"commit,omitempty"`
	Unsupported bool   `yaml:"unsupported,omitempty"`
}

type Lock struct {
	// Apps maps an app id to its entry per platform key (debian, macos, …).
	Apps map[string]map[string]Entry `yaml:"apps"`
}

func New() *Lock {
	return &Lock{Apps: map[string]map[string]Entry{}}
}

func (l *Lock) Get(id, platform string) (Entry, bool) {
	e, ok := l.Apps[id][platform]

	return e, ok
}

func (l *Lock) Set(id, platform string, e Entry) {
	if l.Apps[id] == nil {
		l.Apps[id] = map[string]Entry{}
	}

	l.Apps[id][platform] = e
}

func (l *Lock) Clone() *Lock {
	c := New()

	for id, byPlatform := range l.Apps {
		for p, e := range byPlatform {
			c.Set(id, p, e)
		}
	}

	return c
}

