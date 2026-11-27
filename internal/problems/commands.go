package problems

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/command"
)

// Register adds the problem commands. dataDir is the folder that holds library.db.
func Register(r *command.Registry, dataDir string) { RegisterService(r, ForDir(dataDir)) }

// RegisterService adds the problem commands for s.
func RegisterService(r *command.Registry, s *Service) {
	r.Add(
		command.Define("problems.status", "Show whether the problem log is on, where it is, and the latest problem the person has not been told about. Reads local files only.", false,
			func(context.Context, *command.Env, struct{}) (StatusResult, error) { return s.status(), nil }),
		command.Define("problems.log", "Write an error from the interface to the problem log. At most ten a minute are kept. Nothing is sent anywhere.", false,
			func(_ context.Context, _ *command.Env, p LogParams) (FrontendResult, error) {
				return s.LogFrontend(p.Message, p.Stack, p.Where), nil
			}),
		command.Define("problems.report", "Build the text of a problem report: version, system, the latest new problem and recent log lines, with private content removed. Nothing is sent: the person reads, edits and chooses what to do with it.", false,
			func(_ context.Context, _ *command.Env, p ReportParams) (ReportResult, error) {
				return ReportResult{Text: s.Report(ReportOptions{Note: p.Note, Lines: p.Lines})}, nil
			}),
		command.Define("problems.issue_url", "Make the address of a pre-filled new issue on Glossa's public GitHub page holding the report text, cut to fit a web address if it is long. It only builds the address: the app opens it in the browser and the person submits it.", false,
			func(_ context.Context, _ *command.Env, p IssueParams) (IssueResult, error) {
				link, cut := IssueURL(p.Text)
				return IssueResult{URL: link, Shortened: cut, Limit: MaxIssueURL}, nil
			}),
		command.Define("problems.save", "Save report text to a .txt file the person chose.", false,
			func(_ context.Context, _ *command.Env, p SaveParams) (SaveResult, error) { return saveReport(p) }),
		command.Define("problems.dismiss", "Mark a problem as seen, so its notice is not shown again.", false,
			func(_ context.Context, _ *command.Env, p DismissParams) (struct{}, error) {
				s.Dismiss(p.ID)
				return struct{}{}, nil
			}),
		command.Define("problems.settings", "Show or change the problem log: on by default. Turning it off stops writing and deletes the log.", false,
			func(_ context.Context, _ *command.Env, p SettingsParams) (StatusResult, error) {
				if p.Enabled != nil {
					if err := s.SetEnabled(*p.Enabled); err != nil {
						return StatusResult{}, command.Errorf("failed", "could not change the problem log: %v", err)
					}
				}
				return s.status(), nil
			}),
	)
}

type StatusResult struct {
	Enabled bool     `json:"enabled" desc:"Glossa keeps a problem log on this computer"`
	Folder  string   `json:"folder" desc:"The folder that holds the log"`
	Pending *Problem `json:"pending,omitempty" desc:"The latest problem the person has not been told about"`
}

type LogParams struct {
	Message string `json:"message" desc:"The error's message"`
	Stack   string `json:"stack,omitempty" desc:"Its stack, if there is one"`
	Where   string `json:"where,omitempty" desc:"Where it came from, such as a script name and line"`
}

type ReportParams struct {
	Note  string `json:"note,omitempty" desc:"What the person was doing"`
	Lines int    `json:"lines,omitempty" desc:"How many recent log lines (default 200)"`
}

type ReportResult struct {
	Text string `json:"text"`
}

type IssueParams struct {
	Text string `json:"text" desc:"The report text, as the person has edited it"`
}

type IssueResult struct {
	URL       string `json:"url"`
	Shortened bool   `json:"shortened" desc:"The text was cut to fit; copy the full text to paste it into the issue"`
	Limit     int    `json:"limit"`
}

type SaveParams struct {
	Path string `json:"path" desc:"A .txt file"`
	Text string `json:"text"`
}

type SaveResult struct {
	Path string `json:"path"`
}

type DismissParams struct {
	ID string `json:"id"`
}

type SettingsParams struct {
	Enabled *bool `json:"enabled,omitempty" desc:"false stops the log and deletes it; true starts it again"`
}

func (s *Service) status() StatusResult {
	r := StatusResult{Enabled: s.Enabled(), Folder: s.logDir}
	if p, ok := s.Pending(); ok && r.Enabled {
		r.Pending = &p
	}
	return r
}

func saveReport(p SaveParams) (SaveResult, error) {
	if !strings.EqualFold(filepath.Ext(p.Path), ".txt") {
		return SaveResult{}, command.Errorf("bad_params", "a report is saved as a .txt file")
	}
	if err := os.WriteFile(p.Path, []byte(p.Text), 0o644); err != nil {
		return SaveResult{}, command.Errorf("failed", "could not save the report: %v", err)
	}
	return SaveResult{Path: p.Path}, nil
}
