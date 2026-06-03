package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/suhaanthayyil/entire-sem/internal/gitutil"
	"github.com/suhaanthayyil/entire-sem/internal/sem"
)

type Options struct {
	Version string
	Env     EntireEnv
	Stdout  io.Writer
	Stderr  io.Writer
}

func Execute(version string, args []string) error {
	return Run(context.Background(), Options{
		Version: version,
		Env:     EnvFromOS(),
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}, args)
}

func Run(ctx context.Context, opts Options, args []string) error {
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}

	if len(args) == 0 {
		printHelp(opts.Stdout)
		return nil
	}

	switch args[0] {
	case "diff":
		return runDiff(ctx, opts, args[1:])
	case "commit":
		return runCommit(ctx, opts, args[1:])
	case "checkpoint":
		return runCheckpoint(ctx, opts, args[1:])
	case "analyze":
		return runAnalyze(ctx, opts, args[1:])
	case "doctor":
		return runDoctor(opts)
	case "version", "--version", "-v":
		fmt.Fprintln(opts.Stdout, opts.Version)
		return nil
	case "help", "--help", "-h":
		printHelp(opts.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printHelp(out io.Writer) {
	fmt.Fprintln(out, `entire-sem adds entity-level context to Entire checkpoints.

Usage:
  entire sem commit [rev] [--json] [--repo path]
  entire sem checkpoint <checkpoint-id> [--json] [--repo path]
  entire sem diff --base <rev> --head <rev> [--json] [--repo path] [-- path...]
  entire sem analyze [--base <rev>] [--head <rev>] [--json] [--repo path] [-- path...]
  entire sem doctor
  entire sem version`)
}

func runDoctor(opts Options) error {
	fmt.Fprintf(opts.Stdout, "ENTIRE_CLI_VERSION=%s\n", valueOrUnset(opts.Env.CLIVersion))
	fmt.Fprintf(opts.Stdout, "ENTIRE_REPO_ROOT=%s\n", valueOrUnset(opts.Env.RepoRoot))
	fmt.Fprintf(opts.Stdout, "ENTIRE_PLUGIN_DATA_DIR=%s\n", valueOrUnset(opts.Env.PluginDataDir))
	repo, err := resolveRepo(context.Background(), opts.Env, "")
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.Stdout, "repo_root=%s\n", repo)
	return nil
}

type commonFlags struct {
	Repo string
	JSON bool
}

type parsedCommonFlags struct {
	Flags            commonFlags
	Rest             []string
	PathArgs         []string
	HasPathSeparator bool
}

func parseCommonFlags(args []string) (parsedCommonFlags, error) {
	var parsed parsedCommonFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--json":
			parsed.Flags.JSON = true
		case "--repo":
			i++
			if i >= len(args) {
				return parsed, errors.New("--repo requires a value")
			}
			parsed.Flags.Repo = args[i]
		case "--":
			parsed.HasPathSeparator = true
			parsed.PathArgs = append(parsed.PathArgs, args[i+1:]...)
			return parsed, nil
		default:
			parsed.Rest = append(parsed.Rest, arg)
		}
	}
	return parsed, nil
}

func runCommit(ctx context.Context, opts Options, args []string) error {
	parsed, err := parseCommonFlags(args)
	if err != nil {
		return err
	}
	if parsed.HasPathSeparator {
		return errors.New("commit does not accept path arguments")
	}
	rev := "HEAD"
	if len(parsed.Rest) > 0 {
		rev = parsed.Rest[0]
	}
	if len(parsed.Rest) > 1 {
		return errors.New("commit accepts at most one revision")
	}
	repo, err := resolveRepo(ctx, opts.Env, parsed.Flags.Repo)
	if err != nil {
		return err
	}
	base, err := gitutil.FirstParent(ctx, repo, rev)
	if err != nil {
		return err
	}
	return analyzeAndPrint(ctx, opts.Stdout, repo, base, rev, nil, parsed.Flags.JSON)
}

func runCheckpoint(ctx context.Context, opts Options, args []string) error {
	parsed, err := parseCommonFlags(args)
	if err != nil {
		return err
	}
	if parsed.HasPathSeparator {
		return errors.New("checkpoint does not accept path arguments")
	}
	if len(parsed.Rest) != 1 {
		return errors.New("checkpoint requires exactly one checkpoint ID")
	}
	repo, err := resolveRepo(ctx, opts.Env, parsed.Flags.Repo)
	if err != nil {
		return err
	}
	result, err := sem.AnalyzeCheckpoint(ctx, repo, parsed.Rest[0])
	if err != nil {
		return err
	}
	return printResult(opts.Stdout, result, parsed.Flags.JSON)
}

func runAnalyze(ctx context.Context, opts Options, args []string) error {
	return runDiff(ctx, opts, args)
}

func runDiff(ctx context.Context, opts Options, args []string) error {
	parsed, err := parseCommonFlags(args)
	if err != nil {
		return err
	}

	base := "HEAD~1"
	head := "HEAD"
	var paths []string
	for i := 0; i < len(parsed.Rest); i++ {
		switch parsed.Rest[i] {
		case "--base":
			i++
			if i >= len(parsed.Rest) {
				return errors.New("--base requires a value")
			}
			base = parsed.Rest[i]
		case "--head":
			i++
			if i >= len(parsed.Rest) {
				return errors.New("--head requires a value")
			}
			head = parsed.Rest[i]
		default:
			paths = append(paths, parsed.Rest[i])
		}
	}
	paths = append(paths, parsed.PathArgs...)

	repo, err := resolveRepo(ctx, opts.Env, parsed.Flags.Repo)
	if err != nil {
		return err
	}
	return analyzeAndPrint(ctx, opts.Stdout, repo, base, head, paths, parsed.Flags.JSON)
}

func resolveRepo(ctx context.Context, env EntireEnv, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env.RepoRoot != "" {
		return env.RepoRoot, nil
	}
	return gitutil.RepoRoot(ctx, ".")
}

func analyzeAndPrint(ctx context.Context, out io.Writer, repo, base, head string, paths []string, asJSON bool) error {
	result, err := sem.AnalyzeGitRange(ctx, repo, base, head, paths)
	if err != nil {
		return err
	}
	return printResult(out, result, asJSON)
}

func printResult(out io.Writer, result sem.Result, asJSON bool) error {
	if asJSON {
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(encoded))
		return nil
	}
	sem.WriteText(out, result)
	return nil
}
