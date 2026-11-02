package app

import (
	"github.com/rchehd/rehome/internal/discover"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/secrets"
)

type ScanReport struct {
	Config    string             `json:"config"`
	Written   bool               `json:"written"`
	Apps      AppsReport         `json:"apps"`
	Unknown   []string           `json:"unknown"`
	Files     FilesReport        `json:"settings_files"`
	Secrets   []secrets.Finding  `json:"redacted_secrets"`
	Attention []discover.Warning `json:"needs_attention"`
	Warnings  []string           `json:"warnings"`
	Lock      LockReport         `json:"lock"`
}

type AppsReport struct {
	Added         []string `json:"added"`
	Updated       []string `json:"updated"`
	Returned      []string `json:"returned"`
	MissingOnScan []string `json:"missing_on_scan"`
	Unchanged     int      `json:"unchanged"`
}

type FilesReport struct {
	New     int `json:"new"`
	Changed int `json:"changed"`
}

type LockReport struct {
	Path     string   `json:"path"`
	Recorded int      `json:"recorded"`
	Changed  []string `json:"changed"`
}

func (sp *ScanPlan) JSON(written bool) ScanReport {
	r := ScanReport{
		Config:  sp.Root + "/rehome.yml",
		Written: written,
		Apps: AppsReport{
			Added: sp.Report.Added, Updated: sp.Report.Updated, Returned: sp.Report.Returned,
			MissingOnScan: sp.Report.MissingOnScan, Unchanged: len(sp.Report.Unchanged),
		},
		Unknown:   sp.Result.Unknown,
		Secrets:   sp.Result.Findings,
		Attention: sp.Result.Attention,
		Warnings:  sp.Result.Warnings,
		Lock:      LockReport{Path: sp.Lock.Path, Recorded: len(sp.Lock.Current), Changed: sp.Lock.Changes},
	}

	for _, c := range sp.Result.FileChanges {
		if c.Old == nil {
			r.Files.New++
		} else {
			r.Files.Changed++
		}
	}

	return r
}

type PlanStep struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Manager   string `json:"manager,omitempty"`
	Package   string `json:"package,omitempty"`
	Pin       string `json:"pin,omitempty"`
	Installed string `json:"installed,omitempty"`
	Upgrade   string `json:"upgrade,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type PlanReport struct {
	Mode     string     `json:"mode"`
	Steps    []PlanStep `json:"steps"`
	Commands []string   `json:"commands_that_will_run"`
	// RunAs is what a person runs in a terminal to apply the plan (sudo needs them).
	RunAs string `json:"run_as"`
}

func PlanJSON(plan *install.Plan) PlanReport {
	r := PlanReport{Mode: plan.Mode, Commands: plan.Commands(), RunAs: "rehome install --yes --versions " + plan.Mode}

	for _, s := range plan.Steps {
		name, pkg := s.Spec.Manager()
		r.Steps = append(r.Steps, PlanStep{
			ID: s.App.ID, Status: s.Status, Manager: name, Package: pkg,
			Pin: label(s.Pin), Installed: s.Installed, Upgrade: label(s.Upgrade), Reason: s.Reason,
		})
	}

	return r
}

func label(e lock.Entry) string {
	if e.Commit != "" {
		return e.Commit
	}

	return e.Version
}
