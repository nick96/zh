package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	dotenv "github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var (
	// DefaultBaseURL is the base URL to build API endpoint URLs from.
	//
	// This can be configured via the command line .
	DefaultBaseURL string = "https://api.zenhub.com"

	// AuthenticationHeader is the header used to put the authentication
	// token in.
	AuthenticationHeader string = "X-Authentication-Token"

	// ZenHubTokenEnvVar is the environment variable to retrieve ZenHub
	// token from.
	ZenHubTokenEnvVar string = "ZENHUB_TOKEN"

	// ZenHubWorkspaceIDEnvVar is the environment variable to set the
	// default ZenHub workspace.
	ZenHubWorkspaceIDEnvVar string = "ZENHUB_WORKSPACE_ID"

	// ZenHubRepositoryIDEnvVar is the environment variable to set the
	// default ZenHub repository.
	ZenHubRepositoryIDEnvVar string = "ZENHUB_REPOSITORY_ID"

	// ZenHubLogLevelEnvVar is the environment variable to set the log
	// level.
	ZenHubLogLevelEnvVar string = "ZENHUB_LOG_LEVEL"
)

// MoveIssueRequest is the request body of a request to move an issue.
type MoveIssueRequest struct {
	PipelineID string `json:"pipeline_id"`
	Position   string `json:"position"`
}

// AuthenticationTransport is a custom transport that adds the ZenHub token to
// the `AuthenticationHeader`.
type AuthenticationTransport struct {
	transport           http.RoundTripper
	authenticationToken string
}

// RoundTrip adds the `AuthenticationHeader` to the request and calls the
// wrapped `transport`.
func (t *AuthenticationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add(AuthenticationHeader, t.authenticationToken)
	return t.transport.RoundTrip(req)
}

// GetZenHubToken gets the ZenHub token.
//
// Order of precedence is:
//
// 1. ZENHUB_TOKEN environment variable
func GetZenHubToken() (string, error) {
	envVar := strings.TrimSpace(os.Getenv(ZenHubTokenEnvVar))
	if envVar != "" {
		return envVar, nil
	}
	return "", fmt.Errorf("expected environment variable %s", ZenHubTokenEnvVar)
}

// ErrorFromStatusCode converts the given status code into a more informative
// error message.
func ErrorFromStatusCode(statusCode int) error {
	switch statusCode {
	case 401:
		return fmt.Errorf("authentication token is not valid. Check that %s is set correctly", ZenHubTokenEnvVar)
	case 403:
		return fmt.Errorf("ZenHub API request limit reached. Please try again later")
	case 404:
		return fmt.Errorf("endpoint not found. This most likely is a bug in zh, please report it")
	case 200:
		return nil
	default:
		return fmt.Errorf("unknown status code %d. This most likely is a bug in zh, please report it", statusCode)
	}
}

// MoveIssueCommand moves issues between pipelines.
func MoveIssueCommand(ctx *cli.Context) error {
	if ctx.Args().Len() != 2 {
		return fmt.Errorf("expected exactly two argument, the issue ID and the pipeline ID. Received %d", ctx.Args().Len())
	}

	issueID, err := strconv.Atoi(ctx.Args().First())
	if err != nil {
		return fmt.Errorf("expected issue ID to be an int, got %s", ctx.Args().First())
	}

	pipelineID := ctx.Args().Get(1)

	token, err := GetZenHubToken()
	if err != nil {
		return err
	}

	client := http.Client{
		Transport: &AuthenticationTransport{
			transport:           http.DefaultTransport,
			authenticationToken: token,
		},
	}

	workspaceID := ctx.String("workspace-id")
	if workspaceID == "" {
		return fmt.Errorf("invalid workpace-id value of %s", workspaceID)
	}

	repositoryID := ctx.Uint("repository-id")
	if repositoryID == 0 {
		return fmt.Errorf("invalid repository-id value of %d", repositoryID)
	}

	url := fmt.Sprintf("%s/p2/workspaces/%s/repositories/%d/issues/%d/moves",
		ctx.String("base-url"),
		workspaceID,
		repositoryID,
		issueID,
	)
	request := MoveIssueRequest{
		PipelineID: pipelineID,
		Position:   "bottom",
	}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to convert move issue request %v to JSON: %w", request, err)
	}

	logrus.WithFields(logrus.Fields{
		"url":  url,
		"body": string(body),
	}).Debug("Sending move issue request")
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to move issue between pipelines: %w", err)
	}

	if err := ErrorFromStatusCode(resp.StatusCode); err != nil {
		return fmt.Errorf("failed to move issue between pipelines: %w", err)
	}

	fmt.Printf("Successfully moved issue %d to pipelines %s\n", issueID, pipelineID)

	return nil
}

func main() {
	if err := dotenv.Load(); err != nil {
		logrus.WithField("error", err).Warn("failed to load .env file in working directory")
	}

	if level := os.Getenv(ZenHubLogLevelEnvVar); level != "" {
		logrusLevel, err := logrus.ParseLevel(level)
		if err != nil {
			logrus.WithField("value", level).Warn("Invalid logrus level '%s' specified by %s", level, ZenHubLogLevelEnvVar)
		} else {
			logrus.SetLevel(logrusLevel)
		}
	}

	defaultWorkspaceID := os.Getenv(ZenHubWorkspaceIDEnvVar)

	defaultRepositoryID := uint(0)
	if repoIDEnv := os.Getenv(ZenHubRepositoryIDEnvVar); strings.TrimSpace(repoIDEnv) != "" {
		repoID, err := strconv.Atoi(repoIDEnv)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"error": err,
				"name":  ZenHubRepositoryIDEnvVar,
				"value": repoIDEnv,
			}).Fatal("invalid value for default repository ID")
		}
		defaultRepositoryID = uint(repoID)
	}

	app := cli.App{
		Name:  "zh",
		Usage: "Control ZenHub from the command line!",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "base-url",
				Value: DefaultBaseURL,
				Usage: "Base URL to build API endpoints from.",
			},
			&cli.StringFlag{
				Name:    "workspace-id",
				Aliases: []string{"w"},
				Usage:   "ID of the target workspace.",
				Value:   defaultWorkspaceID,
			},
			&cli.UintFlag{
				Name:    "repository-id",
				Aliases: []string{"r"},
				Usage:   "ID of the target repository.",
				Value:   defaultRepositoryID,
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "issue",
				Usage: "Work with issues",
				Subcommands: []*cli.Command{
					{
						Name:   "mv",
						Usage:  "Move an issue between pipelines",
						Action: MoveIssueCommand,
					},
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		logrus.WithFields(logrus.Fields{"error": err}).Fatal("Failed to run app")
	}
}
