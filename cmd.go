package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"

	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/spf13/cobra"
)

const defaultCommitMsg = "chore(ci):update actions"

const tokenNotProvidedMsg = `GitHub connection token not provided.
You might not be able to make the changes to remote repositories, and you will hit API rate limits quickly.
You can provide it using the GH_TOKEN/GITHUB_TOKEN environment variables, the --github-token option,
or by logging in with "gh auth login".`

// app holds the global options and dependencies shared by all commands.
type app struct {
	stdout, stderr io.Writer
	color          styler // for stdout
	errColor       styler // for stderr
	log            *logger

	verbose   int
	repoArg   string
	token     string
	majorOnly bool

	// resolveToken returns the token to use (flagToken when set) and the GitHub host it is for.
	resolveToken func(flagToken string) (token, host string)
	newAPI       func(token, host string) (githubAPI, error)

	apiOnce sync.Once
	api     githubAPI
	apiErr  error
}

func newApp(stdout, stderr io.Writer, color, errColor bool) *app {
	return &app{
		stdout:       stdout,
		stderr:       stderr,
		color:        styler(color),
		errColor:     styler(errColor),
		log:          &logger{w: stderr, level: levelWarn, color: styler(errColor)},
		resolveToken: resolveToken,
		newAPI:       newGitHubAPI,
	}
}

// resolveToken uses, in order: --github-token, GH_TOKEN/GITHUB_TOKEN, gh's config and `gh auth token`.
func resolveToken(flagToken string) (string, string) {
	host, _ := auth.DefaultHost()
	if flagToken != "" {
		return flagToken, host
	}
	token, _ := auth.TokenForHost(host)
	return token, host
}

// github returns the API client, creating it on first use so that local-only commands never need a token.
func (a *app) github() (githubAPI, error) {
	a.apiOnce.Do(func() {
		token, host := a.resolveToken(a.token)
		if token == "" {
			fmt.Fprintln(a.stderr, a.errColor.style(tokenNotProvidedMsg, yellow))
		}
		a.api, a.apiErr = a.newAPI(token, host)
	})
	return a.api, a.apiErr
}

func (a *app) execute(args []string) error {
	root := a.newRootCmd()
	root.SetArgs(args)
	return root.Execute()
}

type updateOptions struct {
	update    bool
	commitMsg string
}

func addUpdateFlags(cmd *cobra.Command, opts *updateOptions) {
	cmd.Flags().BoolVarP(&opts.update, "update", "u", false,
		"Update actions in workflows (remote repos: commit the changes, local repos: rewrite the files)")
	cmd.Flags().StringVar(&opts.commitMsg, "commit-msg", defaultCommitMsg, "Commit message, only used for remote repos")
}

func (a *app) newRootCmd() *cobra.Command {
	var rootOpts, updateOpts updateOptions
	root := &cobra.Command{
		Use:   "actions-update",
		Short: "Find and update outdated actions in GitHub Actions workflows",
		Long: `Find and update outdated actions in GitHub Actions workflows, in a local clone or a remote repository.

Without a command, update-actions runs.`,
		Annotations:   map[string]string{cobra.CommandDisplayNameAnnotation: "gh actions-update"},
		Version:       buildVersion(),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(*cobra.Command, []string) {
			switch {
			case a.verbose >= 2:
				a.log.level = levelDebug
			case a.verbose == 1:
				a.log.level = levelInfo
			}
		},
		RunE: func(*cobra.Command, []string) error { return a.runUpdateActions(rootOpts) },
	}
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	root.SetVersionTemplate("gh actions-update {{.Version}}\n")
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w\nRun '%s --help' for usage", err, c.CommandPath())
	})

	flags := root.PersistentFlags()
	flags.CountVarP(&a.verbose, "verbose", "v", "Increase verbosity: -v shows progress, -vv debug details")
	flags.StringVar(&a.repoArg, "repo", ".", "Repository to analyze, a local directory or OWNER/REPO")
	flags.StringVar(&a.token, "github-token", "",
		`GitHub token to use. Defaults to GH_TOKEN or GITHUB_TOKEN, then to "gh auth token"`)
	flags.BoolVarP(&a.majorOnly, "major-only", "m", false,
		"Update major versions only, e.g., v1.2.3 will not be upgraded to v1.2.4 but to v2")
	addUpdateFlags(root, &rootOpts)

	updateCmd := &cobra.Command{
		Use:   "update-actions",
		Short: "Show actions that have updates in repository workflows, and optionally update them",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return a.runUpdateActions(updateOpts) },
	}
	addUpdateFlags(updateCmd, &updateOpts)

	listActionsCmd := &cobra.Command{
		Use:   "list-actions WORKFLOW",
		Short: "List actions in a workflow",
		Long: `List actions in a workflow.

WORKFLOW is a path to the workflow file, or its file name in .github/workflows.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error { return a.runListActions(args[0]) },
	}

	listWorkflowsCmd := &cobra.Command{
		Use:   "list-workflows",
		Short: "List workflows in repository",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return a.runListWorkflows() },
	}

	root.AddCommand(updateCmd, listActionsCmd, listWorkflowsCmd)
	return root
}

func (a *app) openRepo() (repository, error) {
	repo, err := openRepo(a.repoArg, a.github)
	if err != nil {
		return nil, err
	}
	fmt.Fprintln(a.stderr, a.errColor.style("GitHub Actions CLI, scanning repo in "+repo.String(), green, bold))
	return repo, nil
}

// loadWorkflows reads and parses all workflows of a repository, skipping those that can't be read or parsed.
func (a *app) loadWorkflows(repo repository) ([]workflow, error) {
	paths, err := repo.WorkflowPaths()
	if err != nil {
		return nil, err
	}
	var workflows []workflow
	for _, p := range paths {
		content, err := repo.ReadWorkflow(p)
		if errors.Is(err, fs.ErrNotExist) {
			a.log.Warnf("Workflow %s not found in %s", p, repo)
			continue
		}
		if err != nil {
			return nil, err
		}
		name, uses, err := parseWorkflow(content)
		if err != nil {
			a.log.Warnf("Skipping %s, it is not a valid workflow: %v", p, err)
			continue
		}
		if name == "" {
			name = p
		}
		workflows = append(workflows, workflow{Path: p, Name: name, Uses: uses, Content: content})
	}
	return workflows, nil
}

func (a *app) runListWorkflows() error {
	repo, err := a.openRepo()
	if err != nil {
		return err
	}
	workflows, err := a.loadWorkflows(repo)
	if err != nil {
		return err
	}
	for _, wf := range workflows {
		fmt.Fprintf(a.stdout, "%s - %s\n", wf.Path, wf.Name)
	}
	return nil
}

func (a *app) runListActions(arg string) error {
	repo, err := a.openRepo()
	if err != nil {
		return err
	}
	p := repo.ResolveWorkflow(arg)
	content, err := repo.ReadWorkflow(p)
	if errors.Is(err, fs.ErrNotExist) {
		paths, _ := repo.WorkflowPaths()
		return fmt.Errorf("workflow %s not found in %s, possible values: %s", arg, repo, strings.Join(paths, ", "))
	}
	if err != nil {
		return err
	}
	_, uses, err := parseWorkflow(content)
	if err != nil {
		return fmt.Errorf("%s is not a valid workflow: %w", p, err)
	}
	for _, u := range uses {
		fmt.Fprintln(a.stdout, u)
	}
	return nil
}

func (a *app) runUpdateActions(opts updateOptions) error {
	repo, err := a.openRepo()
	if err != nil {
		return err
	}
	workflows, err := a.loadWorkflows(repo)
	if err != nil {
		return err
	}
	if len(workflows) == 0 {
		fmt.Fprintf(a.stderr, "No workflows found in %s\n", repo)
		return nil
	}
	a.log.Infof("Found %d workflows in %s", len(workflows), repo)

	var refs, names []string
	seenRefs, seenNames := make(map[string]bool), make(map[string]bool)
	for _, wf := range workflows {
		for _, u := range wf.Uses {
			if !isVersionedAction(u) || seenRefs[u] {
				continue
			}
			seenRefs[u] = true
			refs = append(refs, u)
			if name, _, _ := strings.Cut(u, "@"); !seenNames[name] {
				seenNames[name] = true
				names = append(names, name)
			}
		}
	}
	a.log.Infof("Found %d actions in workflows: %s", len(names), strings.Join(names, ", "))

	gh, err := a.github()
	if err != nil {
		return err
	}
	latest, err := newUpdater(gh, a.majorOnly, a.log).latestVersions(refs)
	if err != nil {
		return err
	}

	versions := make(map[string][]actionVersion, len(workflows))
	for _, wf := range workflows {
		for _, u := range wf.Uses {
			if !isVersionedAction(u) {
				continue
			}
			name, current, _ := strings.Cut(u, "@")
			versions[wf.Path] = append(versions[wf.Path], actionVersion{Name: name, Current: current, Latest: latest[u]})
		}
	}
	a.printVersions(workflows, versions)

	if !opts.update {
		return nil
	}
	updated := 0
	for _, wf := range workflows {
		newContent := []byte(applyUpdates(string(wf.Content), versions[wf.Path]))
		if bytes.Equal(newContent, wf.Content) {
			continue
		}
		if err := repo.WriteWorkflow(wf.Path, newContent, opts.commitMsg); err != nil {
			return fmt.Errorf("updating %s: %w", wf.Path, err)
		}
		updated++
		if repo.IsRemote() {
			fmt.Fprintln(a.stdout, a.color.style(fmt.Sprintf("Committed changes to workflow in %s:%s", repo, wf.Path), cyan))
		} else {
			fmt.Fprintln(a.stdout, a.color.style("Updated workflow in "+wf.Path, cyan))
		}
	}
	if updated == 0 {
		fmt.Fprintln(a.stdout, "All actions are up to date, nothing to update")
	}
	return nil
}

func (a *app) printVersions(workflows []workflow, versions map[string][]actionVersion) {
	nameWidth, versionWidth := 0, 0
	for _, actions := range versions {
		for _, v := range actions {
			nameWidth = max(nameWidth, len(v.Name))
			versionWidth = max(versionWidth, len(v.Current))
		}
	}
	for _, wf := range workflows {
		fmt.Fprintln(a.stdout, a.color.style(wf.Path+" (", brightBlue)+a.color.style(wf.Name, brightCyan)+
			a.color.style("):", brightBlue))
		for _, v := range versions[wf.Path] {
			line := fmt.Sprintf("\t%-*s %*s", nameWidth+5, v.Name, versionWidth+2, v.Current)
			if v.Latest != "" {
				color := cyan
				if major(v.Latest) != major(v.Current) {
					color = red
				}
				line += " ==> " + a.color.style(v.Latest, color)
			}
			fmt.Fprintln(a.stdout, line)
		}
	}
}

func major(version string) string {
	m, _, _ := strings.Cut(version, ".")
	return m
}
