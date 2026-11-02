// Package mcpserver exposes rehome to AI agents over the Model Context Protocol.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/status"
)

const instructions = `rehome moves a workstation to a new machine. Read "scan" (it does not write unless write=true)
and "status"; fix what "attention" lists with add_install / add_capture / set_app; never guess install commands —
prefer official installers. Installing needs sudo in a person's terminal: call install_plan and show the person its
run_as command instead of trying to install.`

// New builds the server; o carries --config/--lock and where progress logs go (stderr).
func New(o app.Options, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "rehome", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	t := tools{o: o}

	mcp.AddTool(s, &mcp.Tool{Name: "scan", Description: "Detect installed software and settings and compare with the config. Writes the config, settings files and lock only when write is true."}, t.scan)
	mcp.AddTool(s, &mcp.Tool{Name: "status", Description: "Compare the config with this system: missing apps, new apps, changed settings, versions behind the lock."}, t.status)
	mcp.AddTool(s, &mcp.Tool{Name: "attention", Description: "What scan could not handle by itself (too-large settings, unclaimed ~/.config folders, unknown binaries), each with the action to take."}, t.attention)
	mcp.AddTool(s, &mcp.Tool{Name: "install_plan", Description: "What install would do (never installs). Returns the steps and run_as: the command a person runs in a terminal."}, t.installPlan)
	mcp.AddTool(s, &mcp.Tool{Name: "add_install", Description: "Set how an app installs on a platform; creates the app if needed. The config is validated before it is written."}, t.addInstall)
	mcp.AddTool(s, &mcp.Tool{Name: "add_capture", Description: "Add a settings rule so scan copies these files for an app."}, t.addCapture)
	mcp.AddTool(s, &mcp.Tool{Name: "set_app", Description: "Enable or disable an app, skip it on platforms, pin a version or change its group."}, t.setApp)

	return s
}

type tools struct {
	o app.Options
}

type ScanArgs struct {
	Write bool `json:"write,omitempty" jsonschema:"write the config, settings files and lock (default: report only)"`
}

func (t tools) scan(ctx context.Context, _ *mcp.CallToolRequest, a ScanArgs) (*mcp.CallToolResult, app.ScanReport, error) {
	plan, err := app.PrepareScan(ctx, t.o)

	if err != nil {
		return nil, app.ScanReport{}, err
	}

	written := false

	if a.Write && plan.Changed() {
		if err := plan.Write(plan.Result.FileChanges); err != nil {
			return nil, app.ScanReport{}, err
		}

		written = true
	}

	return nil, plan.JSON(written), nil
}

type StatusReport struct {
	Clean bool `json:"clean"`
	*status.Status
}

func (t tools) status(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, StatusReport, error) {
	st, err := app.Status(ctx, t.o)

	if err != nil {
		return nil, StatusReport{}, err
	}

	return nil, StatusReport{Clean: st.Clean(), Status: st}, nil
}

type AttentionReport struct {
	Unknown   []string `json:"unknown_binaries"`
	Attention any      `json:"needs_attention"`
}

func (t tools) attention(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, AttentionReport, error) {
	plan, err := app.PrepareScan(ctx, t.o)

	if err != nil {
		return nil, AttentionReport{}, err
	}

	return nil, AttentionReport{Unknown: plan.Result.Unknown, Attention: plan.Result.Attention}, nil
}

type PlanArgs struct {
	Versions string `json:"versions,omitempty" jsonschema:"latest or locked (default: locked when a lock exists)"`
}

func (t tools) installPlan(ctx context.Context, _ *mcp.CallToolRequest, a PlanArgs) (*mcp.CallToolResult, app.PlanReport, error) {
	in, err := app.LoadInstall(t.o)

	if err != nil {
		return nil, app.PlanReport{}, err
	}

	mode := a.Versions

	if mode == "" {
		mode = app.DefaultMode(in.Lock)
	}

	if mode != install.ModeLatest && mode != install.ModeLocked {
		return nil, app.PlanReport{}, fmt.Errorf("versions must be latest or locked")
	}

	plan, err := app.BuildPlan(ctx, t.o, in, install.Selection{Mode: mode})

	if err != nil {
		return nil, app.PlanReport{}, err
	}

	return nil, app.PlanJSON(plan), nil
}

type InstallArgs struct {
	App      string `json:"app" jsonschema:"app id"`
	Platform string `json:"platform,omitempty" jsonschema:"debian, macos, fedora, arch or any (default: this machine)"`
	Manager  string `json:"manager" jsonschema:"apt, snap, flatpak, npm, pipx, uv_tool, cargo, go or script"`
	Package  string `json:"package" jsonschema:"package name, or the install command for script"`
	Bin      string `json:"bin,omitempty" jsonschema:"binary name or path that proves the install (needed for script)"`
	Repo     string `json:"repo,omitempty" jsonschema:"apt repo name from apt_repos"`
	Classic  bool   `json:"classic,omitempty" jsonschema:"snap classic confinement"`
	Group    string `json:"group,omitempty" jsonschema:"group for a new app"`
}

type Updated struct {
	Updated string `json:"updated"`
}

func (t tools) addInstall(_ context.Context, _ *mcp.CallToolRequest, a InstallArgs) (*mcp.CallToolResult, Updated, error) {
	spec, err := specFor(a)

	if err != nil {
		return nil, Updated{}, err
	}

	if a.Platform == "" {
		p, err := platform.Detect()

		if err != nil {
			return nil, Updated{}, err
		}

		a.Platform = p.Key()
	}

	return t.edit(func(path string) error {
		return config.AddInstall(path, a.App, a.Platform, spec, a.Group)
	})
}

func specFor(a InstallArgs) (config.InstallSpec, error) {
	s := config.InstallSpec{Repo: a.Repo, Classic: a.Classic, Bin: a.Bin}
	fields := map[string]*string{
		"apt": &s.Apt, "snap": &s.Snap, "flatpak": &s.Flatpak, "npm": &s.Npm, "pipx": &s.Pipx,
		"uv_tool": &s.UvTool, "cargo": &s.Cargo, "go": &s.GoPkg, "script": &s.Script,
	}
	field, ok := fields[a.Manager]

	if !ok {
		return s, fmt.Errorf("unknown manager %q", a.Manager)
	}

	*field = a.Package

	return s, nil
}

type CaptureArgs struct {
	App     string   `json:"app" jsonschema:"app id"`
	Base    string   `json:"base" jsonschema:"folder the globs are relative to, e.g. ~/.config/myapp"`
	Include []string `json:"include" jsonschema:"globs to copy; ** spans folders"`
	Exclude []string `json:"exclude,omitempty" jsonschema:"globs to skip"`
	FileKey string   `json:"file_key,omitempty" jsonschema:"linux or macos (default: this machine)"`
}

func (t tools) addCapture(_ context.Context, _ *mcp.CallToolRequest, a CaptureArgs) (*mcp.CallToolResult, Updated, error) {
	if a.FileKey == "" {
		p, err := platform.Detect()

		if err != nil {
			return nil, Updated{}, err
		}

		a.FileKey = p.FileKey()
	}

	rule := config.FileRule{Base: a.Base, Include: a.Include, Exclude: a.Exclude}

	return t.edit(func(path string) error {
		return config.AddCapture(path, a.App, a.FileKey, rule)
	})
}

type SetArgs struct {
	App     string  `json:"app" jsonschema:"app id"`
	Enabled *bool   `json:"enabled,omitempty" jsonschema:"enable or disable the app"`
	Skip    *string `json:"skip,omitempty" jsonschema:"comma-separated platforms to skip; empty removes"`
	Version *string `json:"version,omitempty" jsonschema:"pin a version permanently; empty removes"`
	Group   *string `json:"group,omitempty" jsonschema:"group name"`
}

func (t tools) setApp(_ context.Context, _ *mcp.CallToolRequest, a SetArgs) (*mcp.CallToolResult, Updated, error) {
	fields := map[string]string{}

	if a.Enabled != nil {
		fields["enabled"] = fmt.Sprint(*a.Enabled)
	}

	for key, v := range map[string]*string{"skip": a.Skip, "version": a.Version, "group": a.Group} {
		if v != nil {
			fields[key] = *v
		}
	}

	if len(fields) == 0 {
		return nil, Updated{}, fmt.Errorf("nothing to set")
	}

	return t.edit(func(path string) error {
		return config.SetApp(path, a.App, fields)
	})
}

func (t tools) edit(change func(path string) error) (*mcp.CallToolResult, Updated, error) {
	path, err := app.ConfigPath(t.o)

	if err != nil {
		return nil, Updated{}, err
	}

	if err := change(path); err != nil {
		return nil, Updated{}, err
	}

	return nil, Updated{Updated: path}, nil
}
