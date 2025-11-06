package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/mickamy/xplain/internal/analyzer"
	"github.com/mickamy/xplain/internal/config"
	"github.com/mickamy/xplain/internal/diff"
	"github.com/mickamy/xplain/internal/model"
	"github.com/mickamy/xplain/internal/parser"
	"github.com/mickamy/xplain/internal/render/html"
	"github.com/mickamy/xplain/internal/render/tui"
	"github.com/mickamy/xplain/internal/runner"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "run":
		err = runCommand(args)
	case "analyze":
		err = analyzeCommand(args)
	case "report":
		err = reportCommand(args)
	case "diff":
		err = diffCommand(args)
	case "version":
		err = versionCommand(args)
	case "help", "-h", "--help":
		usage()
		return
	default:
		_, _ = fmt.Fprintf(os.Stderr, "Unknown command %q\n\n", cmd)
		usage()
		os.Exit(1)
	}

	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`xplain - PostgreSQL EXPLAIN analyzer

Usage:
  xplain <command> [options]

Commands:
  run      Execute EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) for a query
  analyze  Run EXPLAIN and render a report in one step
  report   Render a plan report (TUI or HTML)
  diff     Compare two plans and emit a Markdown summary
  version  Show CLI version information

Use "xplain <command> -h" for command-specific help.`)
}

func applyConfigPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("XPLAIN_CONFIG"))
	}
	return config.Apply(path)
}

func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stdout, "Usage: xplain run --url <url> --sql <file> [--out plan.json]\n\nOptions:\n")
		fs.PrintDefaults()
	}

	envURL := os.Getenv("DATABASE_URL")

	var (
		urlFlag    = fs.String("url", envURL, "PostgreSQL connection string; defaults to $DATABASE_URL")
		sqlPath    = fs.String("sql", "", "Path to the SQL file to EXPLAIN")
		outPath    = fs.String("out", "", "Path to write the resulting JSON (defaults to stdout)")
		timeout    = fs.Duration("timeout", 0, "Optional execution timeout, e.g. 45s")
		configPath = fs.String("config", "", "Path to configuration file (JSON). Falls back to $XPLAIN_CONFIG")
	)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stdout)
			fs.Usage()
			return nil
		}
		return err
	}
	if err := applyConfigPath(*configPath); err != nil {
		return err
	}
	if err := applyConfigPath(*configPath); err != nil {
		return err
	}
	connection := strings.TrimSpace(*urlFlag)
	if connection == "" {
		return fmt.Errorf("--url is required or set $DATABASE_URL")
	}
	if *sqlPath == "" {
		return fmt.Errorf("--sql is required")
	}

	sqlBytes, err := os.ReadFile(*sqlPath)
	if err != nil {
		return fmt.Errorf("read sql file: %w", err)
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, connection, string(sqlBytes), runner.Options{Timeout: *timeout})
	if err != nil {
		return err
	}

	pretty, err := indentJSON(result)
	if err != nil {
		return err
	}

	if *outPath == "" {
		_, err = os.Stdout.Write(pretty)
		return err
	}
	return os.WriteFile(*outPath, pretty, 0o644)
}

func analyzeCommand(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stdout, "Usage: xplain analyze --url <url> (--sql file.sql | --query \"SELECT ...\") [--mode tui|html]\n\nOptions:\n")
		fs.PrintDefaults()
	}

	envURL := os.Getenv("DATABASE_URL")

	var (
		urlFlag    = fs.String("url", envURL, "PostgreSQL connection string; defaults to $DATABASE_URL")
		sqlPath    = fs.String("sql", "", "Path to the SQL file to EXPLAIN")
		inlineSQL  = fs.String("query", "", "Inline SQL string to EXPLAIN")
		mode       = fs.String("mode", "tui", "Output mode: tui or html")
		outPath    = fs.String("out", "", "Output path (stdout if omitted)")
		title      = fs.String("title", "xplain report", "Report title (HTML)")
		color      = fs.Bool("color", true, "Enable ANSI colors for TUI output")
		maxDepth   = fs.Int("max-depth", 0, "Limit tree depth (TUI)")
		warnings   = fs.Bool("warnings", true, "Show warnings (TUI)")
		includeCSS = fs.Bool("css", true, "Include inline styles (HTML)")
		timeout    = fs.Duration("timeout", 0, "Optional execution timeout, e.g. 45s")
		configPath = fs.String("config", "", "Path to configuration file (JSON). Falls back to $XPLAIN_CONFIG")
	)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stdout)
			fs.Usage()
			return nil
		}
		return err
	}
	if err := applyConfigPath(*configPath); err != nil {
		return err
	}

	connection := strings.TrimSpace(*urlFlag)
	if connection == "" {
		return fmt.Errorf("--url is required or set $DATABASE_URL")
	}

	if *sqlPath != "" && *inlineSQL != "" {
		return fmt.Errorf("specify only one of --sql or --query")
	}

	var sqlText string
	if *sqlPath != "" {
		data, err := os.ReadFile(*sqlPath)
		if err != nil {
			return fmt.Errorf("read sql file: %w", err)
		}
		sqlText = string(data)
	} else if *inlineSQL != "" {
		sqlText = *inlineSQL
	} else {
		return fmt.Errorf("--sql or --query is required")
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, connection, sqlText, runner.Options{Timeout: *timeout})
	if err != nil {
		return err
	}

	_, analysis, err := parseAnalysisReader(bytes.NewReader(result))
	if err != nil {
		return err
	}

	switch *mode {
	case "tui":
		target := io.Writer(os.Stdout)
		if *outPath != "" {
			file, err := os.Create(*outPath)
			if err != nil {
				return fmt.Errorf("create output: %w", err)
			}
			defer func() {
				_ = file.Close()
			}()
			target = file
		}
		return tui.Render(target, analysis, tui.Options{
			EnableColor:  *color,
			MaxDepth:     *maxDepth,
			ShowWarnings: *warnings,
		})
	case "html":
		target := io.Writer(os.Stdout)
		if *outPath != "" {
			file, err := os.Create(*outPath)
			if err != nil {
				return fmt.Errorf("create output: %w", err)
			}
			defer func() {
				_ = file.Close()
			}()
			target = file
		}
		return html.Render(target, analysis, html.Options{
			Title:         *title,
			IncludeStyles: *includeCSS,
		})
	default:
		return fmt.Errorf("unknown mode %q (expected tui or html)", *mode)
	}
}

func reportCommand(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stdout, "Usage: xplain report --input plan.json [--mode tui|html] [--out file]\n\nOptions:\n")
		fs.PrintDefaults()
	}

	var (
		input      = fs.String("input", "", "Path to EXPLAIN JSON input")
		output     = fs.String("out", "", "Output path (stdout if omitted)")
		mode       = fs.String("mode", "tui", "Output mode: tui or html")
		title      = fs.String("title", "xplain report", "Report title (HTML)")
		color      = fs.Bool("color", true, "Enable ANSI colors for TUI output")
		maxDepth   = fs.Int("max-depth", 0, "Limit tree depth (TUI)")
		warnings   = fs.Bool("warnings", true, "Show warnings (TUI)")
		includeCSS = fs.Bool("css", true, "Include inline styles (HTML)")
		configPath = fs.String("config", "", "Path to configuration file (JSON). Falls back to $XPLAIN_CONFIG")
	)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stdout)
			fs.Usage()
			return nil
		}
		return err
	}
	if err := applyConfigPath(*configPath); err != nil {
		return err
	}
	if *input == "" {
		return fmt.Errorf("--input is required")
	}

	_, analysis, err := loadAnalysis(*input)
	if err != nil {
		return err
	}

	switch *mode {
	case "tui":
		target := io.Writer(os.Stdout)
		if *output != "" {
			file, err := os.Create(*output)
			if err != nil {
				return fmt.Errorf("create output: %w", err)
			}
			defer func() {
				_ = file.Close()
			}()
			target = file
		}
		return tui.Render(target, analysis, tui.Options{
			EnableColor:  *color,
			MaxDepth:     *maxDepth,
			ShowWarnings: *warnings,
		})
	case "html":
		target := io.Writer(os.Stdout)
		if *output != "" {
			file, err := os.Create(*output)
			if err != nil {
				return fmt.Errorf("create output: %w", err)
			}
			defer func() {
				_ = file.Close()
			}()
			target = file
		}
		return html.Render(target, analysis, html.Options{
			Title:         *title,
			IncludeStyles: *includeCSS,
		})
	default:
		return fmt.Errorf("unknown mode %q (expected tui or html)", *mode)
	}
}

func diffCommand(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stdout, "Usage: xplain diff --base base.json --target target.json [--format md]\n\nOptions:\n")
		fs.PrintDefaults()
	}

	var (
		basePath   = fs.String("base", "", "Path to baseline EXPLAIN JSON")
		targetPath = fs.String("target", "", "Path to target EXPLAIN JSON")
		format     = fs.String("format", "md", "Output format (md)")
		output     = fs.String("out", "", "Output path (stdout if omitted)")
		minDelta   = fs.Float64("min-delta", 0, "Minimum self-time delta in ms to report (default from config)")
		minPct     = fs.Float64("min-percent", 0, "Minimum percent change to report (default from config)")
		maxItems   = fs.Int("limit", 0, "Maximum rows per section (default from config)")
		configPath = fs.String("config", "", "Path to configuration file (JSON). Falls back to $XPLAIN_CONFIG")
	)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stdout)
			fs.Usage()
			return nil
		}
		return err
	}
	if err := applyConfigPath(*configPath); err != nil {
		return err
	}
	if *basePath == "" || *targetPath == "" {
		return fmt.Errorf("--base and --target are required")
	}

	_, baseAnalysis, err := loadAnalysis(*basePath)
	if err != nil {
		return fmt.Errorf("load base: %w", err)
	}
	_, targetAnalysis, err := loadAnalysis(*targetPath)
	if err != nil {
		return fmt.Errorf("load target: %w", err)
	}

	report, err := diff.Compare(baseAnalysis, targetAnalysis, diff.Options{
		MinSelfTimeDeltaMs: *minDelta,
		MinPercentChange:   *minPct,
		MaxItems:           *maxItems,
	})
	if err != nil {
		return err
	}

	switch *format {
	case "md", "markdown":
		content := report.Markdown()
		if *output == "" {
			fmt.Print(content)
			return nil
		}
		return os.WriteFile(*output, []byte(content), 0o644)
	case "json":
		payload, err := report.JSON()
		if err != nil {
			return err
		}
		if *output == "" {
			os.Stdout.Write(payload)
			os.Stdout.WriteString("\n")
			return nil
		}
		return os.WriteFile(*output, payload, 0o644)
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
}

func versionCommand(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	short := fs.Bool("short", false, "Print only the version number")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stdout)
			fs.Usage()
			return nil
		}
		return err
	}

	v, meta := resolveVersion()
	if *short {
		fmt.Println(v)
		return nil
	}
	if meta != "" {
		fmt.Printf("xplain %s (%s)\n", v, meta)
	} else {
		fmt.Printf("xplain %s\n", v)
	}
	return nil
}

func resolveVersion() (string, string) {
	v := strings.TrimSpace(version)
	if v == "" {
		v = "dev"
	}

	var commit, buildTime string
	var dirty bool
	if info, ok := debug.ReadBuildInfo(); ok {
		if (v == "dev" || v == "(devel)") &&
			info.Main.Version != "" &&
			info.Main.Version != "(devel)" &&
			!strings.HasPrefix(info.Main.Version, "v0.0.0-") {
			v = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				commit = setting.Value
			case "vcs.time":
				buildTime = setting.Value
			case "vcs.modified":
				dirty = setting.Value == "true"
			}
		}
	}

	var details []string
	if commit != "" {
		short := commit
		if len(short) > 12 {
			short = short[:12]
		}
		if dirty {
			short += "*"
			dirty = false
		}
		details = append(details, fmt.Sprintf("commit %s", short))
	}
	if buildTime != "" {
		details = append(details, fmt.Sprintf("built %s", buildTime))
	}
	if dirty {
		details = append(details, "modified workspace")
	}

	return v, strings.Join(details, ", ")
}

func loadAnalysis(path string) (*model.Explain, *analyzer.PlanAnalysis, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	return parseAnalysisReader(file)
}

func indentJSON(data []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		return nil, fmt.Errorf("indent json: %w", err)
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func parseAnalysisReader(r io.Reader) (*model.Explain, *analyzer.PlanAnalysis, error) {
	plan, err := parser.ParseJSON(r)
	if err != nil {
		return nil, nil, err
	}

	analysis, err := analyzer.Analyze(plan)
	if err != nil {
		return nil, nil, err
	}
	return plan, analysis, nil
}
