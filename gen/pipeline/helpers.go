package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/buildkite/go-pipeline"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// secretEnvVar is a helper for specifying the values to pull from the aws-sm
// plugin.
// Don't construct this value directly, add a constant (var in SCREAMING_CASE)
// and refer to that.
type secretEnvVar struct {
	SecretID string `json:"secret-id"`
}

// NotifyGitHubCommitStatus is a helper to specify the name of a check to
// report back to GitHub.
// See also: https://buildkite.com/docs/pipelines/source-control/github#customizing-commit-statuses
// Usage:
//
//	RemainingFields: map[string]any{
//		"notify": []any{
//			NotifyGitHubCommitStatus{Context: "My Context"},
//		},
//	}
type NotifyGitHubCommitStatus struct {
	Context string
}

func (n NotifyGitHubCommitStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"github_commit_status": map[string]string{
			"context": n.Context,
		},
	})
}

type TestSuite struct {
	Name     string
	Required bool
	Timeout  time.Duration

	JUnitPattern *string
}

func (suite *TestSuite) junitPattern() string {
	if suite.JUnitPattern != nil {
		return *suite.JUnitPattern
	}
	return fmt.Sprintf("work/%s-tests-*.xml", strings.ToLower(suite.Name))
}

func (suite *TestSuite) ToStep() pipeline.Step {
	prettyName := fmt.Sprintf("%s Tests", cases.Title(language.English, cases.NoLower).String(suite.Name))

	return &pipeline.GroupStep{
		Group: &prettyName,
		Key:   strings.ToLower(suite.Name),
		Steps: pipeline.Steps{
			&pipeline.CommandStep{
				Key:     strings.ToLower(suite.Name) + "-run",
				Label:   "Run " + prettyName,
				Command: "./ci/scripts/run-in-nix-docker.sh task ci:configure ci:test:" + strings.ToLower(suite.Name),
				RemainingFields: map[string]any{
					"soft_fail":          !suite.Required,
					"timeout_in_minutes": int(suite.Timeout.Minutes()),
					"agents":             AgentsLarge,
					"notify": []any{
						NotifyGitHubCommitStatus{Context: prettyName},
					},
				},
				Plugins: pipeline.Plugins{
					secretEnvVars(
						GITHUB_API_TOKEN, // Required to clone private GH repos (Flux Shims, buildkite slack).
						REDPANDA_LICENSE,
						REDPANDA_LICENSE2,
						SLACK_VBOT_TOKEN, // Used to notify us of build failures in slack.
					),
					// Inform us about failures on main.
					notifySlack(fmt.Sprintf(":cloud: %s Job Failed", prettyName)),
				},
			},
			// BuildKite's go SDK's WaitStep seems to filter itself out if no
			// values are specified. Explicitly setting wait: null, as per
			// BuildKite's docs seems to fix the issue.
			// https://buildkite.com/docs/pipelines/configure/step-types/wait-step
			&pipeline.WaitStep{Contents: map[string]any{"wait": nil, "continue_on_failure": true}},
			&pipeline.CommandStep{
				Key:   strings.ToLower(suite.Name) + "-parse",
				Label: fmt.Sprintf("Parse and annotate %s results", prettyName),
				RemainingFields: map[string]any{
					"allow_dependency_failure": true,
					"agents":                   AgentsPipeLineUploader,
					// If a build fails due to a timeout, killed machine, etc,
					// the Junit files might not get uploaded or the may be
					// corrupted. If so, we don't want to fail the build.
					// That's the job of the actual test step.
					"soft_fail": true,
				},
				Plugins: pipeline.Plugins{
					parseJunit(suite.Name, suite.junitPattern()),
				},
			},
		},
	}
}

func secretEnvVars(vars ...secretEnvVar) *pipeline.Plugin {
	return &pipeline.Plugin{
		Source: "seek-oss/aws-sm#v2.3.2",
		Config: map[string]any{
			"json-to-env": vars,
		},
	}
}

func parseJunit(context, artifactGlob string) *pipeline.Plugin {
	// https://github.com/buildkite-plugins/junit-annotate-buildkite-plugin
	return &pipeline.Plugin{
		Source: "junit-annotate#v2.4.1",
		Config: map[string]any{
			"artifacts":      artifactGlob,
			"report-slowest": 10,
			// BuildKite annotations are associated with a "context" which
			// allows them to be updated. Each instance of this plugin must
			// utilize a different context otherwise the default context of
			// "junit" will be updated with the context of the last run.
			"context": context,
		},
	}
}

func notifySlack(message string) *pipeline.Plugin {
	return &pipeline.Plugin{
		Source: "https://$GITHUB_API_TOKEN@github.com/redpanda-data/step-slack-notify-buildkite-plugin.git#main",
		Config: map[string]any{
			"message":                  message,
			"channel_name":             "kubernetes-tests",
			"slack_token_env_var_name": "SLACK_VBOT_TOKEN",
			"conditions": map[string]any{
				"failed":   true,
				"branches": []string{"main"},
			},
		},
	}
}
