package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli"
	cmdcommon "github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

const batchCompletionGrace = 2 * time.Second

var (
	dlPath   string
	fileName string
	proxyURL string

	dlFlags = []cli.Flag{
		cli.StringFlag{
			Name:        "file-name, o",
			Usage:       "explicitly set the name of file (determined automatically if not specified)",
			Destination: &fileName,
		},
		cli.StringFlag{
			Name:        "download-path, l",
			Usage:       "set the path where downloaded file should be saved (default: $WARPDL_DEFAULT_DL_DIR or current directory)",
			Value:       "",
			Destination: &dlPath,
		},
		cli.BoolFlag{
			Name:  "overwrite, y",
			Usage: "overwrite existing file at destination path",
		},
		cli.StringFlag{
			Name:        "proxy",
			Usage:       "proxy server URL (http://host:port, https://host:port, socks5://host:port)",
			EnvVar:      "WARPDL_PROXY",
			Destination: &proxyURL,
		},
		cli.BoolFlag{
			Name:   "no-work-steal",
			Usage:  "disable work stealing (fast parts taking over slow part ranges)",
			EnvVar: "WARPDL_NO_WORK_STEAL",
		},
		cli.StringFlag{
			Name:   "priority",
			Usage:  "queue priority: high, normal, low (default: normal)",
			Value:  "normal",
			EnvVar: "WARPDL_PRIORITY",
		},
		cli.StringFlag{
			Name:  "input-file, i",
			Usage: "read URLs from input file (one URL per line, # for comments)",
		},
		cli.StringFlag{
			Name:  "ssh-key",
			Usage: "path to SSH private key file for SFTP downloads (default: ~/.ssh/id_ed25519 or ~/.ssh/id_rsa)",
		},
		cli.StringFlag{
			Name:  "start-at",
			Usage: "schedule download to start at a specific time (format: YYYY-MM-DD HH:MM)",
		},
		cli.StringFlag{
			Name:  "start-in",
			Usage: "schedule download to start after a relative duration (e.g., 2h, 30m, 1h30m); mutually exclusive with --start-at",
		},
		cli.StringFlag{
			Name:  "schedule",
			Usage: "cron expression for recurring download schedule (e.g., \"0 2 * * *\" = daily 2 AM)",
		},
	}
)

// parsePriority converts a priority string to the corresponding integer value.
// Returns 1 (normal) for invalid values.
func parsePriority(s string) int {
	switch strings.ToLower(s) {
	case "high":
		return 2
	case "low":
		return 0
	default:
		return 1 // normal
	}
}

// resolveDownloadPath determines the download directory path based on priority:
// 1. CLI flag (-l) - highest priority
// 2. Environment variable (WARPDL_DEFAULT_DL_DIR) - medium priority
// 3. Current working directory - fallback
// Returns the validated absolute path or an error if the path is invalid.
func resolveDownloadPath(cliPath string) (string, error) {
	var selectedPath string

	// Priority 1: CLI flag
	if cliPath != "" {
		selectedPath = cliPath
	} else {
		// Priority 2: Environment variable
		envPath := os.Getenv(common.DefaultDlDirEnv)
		if envPath != "" {
			selectedPath = envPath
		} else {
			// Priority 3: Current working directory
			cwd, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("failed to get current directory: %w", err)
			}
			selectedPath = cwd
		}
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(selectedPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	// Validate the directory
	if err := warplib.ValidateDownloadDirectory(absPath); err != nil {
		return "", fmt.Errorf("invalid download directory: %w", err)
	}

	return absPath, nil
}

func buildDownloadOpts(
	ctx *cli.Context,
	headers warplib.Headers,
	startAtValue, scheduleValue string,
) *warpcli.DownloadOpts {
	return &warpcli.DownloadOpts{
		ForceParts:          forceParts,
		MaxConnections:      int32(maxConns),
		MaxSegments:         int32(maxParts),
		Headers:             headers,
		Overwrite:           ctx.Bool("overwrite"),
		Proxy:               proxyURL,
		Timeout:             timeout,
		MaxRetries:          maxRetries,
		RetryDelay:          retryDelay,
		SpeedLimit:          ctx.String("speed-limit"),
		DisableWorkStealing: ctx.Bool("no-work-steal"),
		Priority:            parsePriority(ctx.String("priority")),
		SSHKeyPath:          ctx.String("ssh-key"),
		StartAt:             startAtValue,
		Schedule:            scheduleValue,
	}
}

func download(ctx *cli.Context) (err error) {
	inputFile := ctx.String("input-file")
	url := ctx.Args().First()

	// Check if we have any URLs to download
	if inputFile == "" && url == "" {
		if ctx.Command.Name == "" {
			return cmdcommon.Help(ctx)
		}
		return cmdcommon.PrintErrWithCmdHelp(
			ctx,
			errors.New("no url provided (use URL argument or -i/--input-file)"),
		)
	}

	if url == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	if err := validateTransferLimits(); err != nil {
		return cmdcommon.PrintErrWithCmdHelp(ctx, err)
	}

	client, err := getClient()
	if err != nil {
		return cmdcommon.PrintRuntimeErr(ctx, "download", "new_client", err)
	}
	defer client.Close()
	client.CheckVersionMismatch(currentBuildArgs.Version)

	// Handle batch download if input file is provided
	if inputFile != "" {
		return downloadBatchFromFile(ctx, client, inputFile)
	}

	fmt.Println(">> Initiating a WARP download << ")
	url = strings.TrimSpace(url)

	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}
	// Parse and append cookie flags
	cookies := ctx.StringSlice("cookie")
	headers, err = AppendCookieHeader(headers, cookies)
	if err != nil {
		return cmdcommon.PrintRuntimeErr(ctx, "download", "parse_cookies", err)
	}
	dlPath, err = resolveDownloadPath(dlPath)
	if err != nil {
		return cmdcommon.PrintRuntimeErr(ctx, "download", "resolve_path", err)
	}
	if proxyURL != "" {
		if _, err := warplib.ParseProxyURL(proxyURL); err != nil {
			return cmdcommon.PrintRuntimeErr(ctx, "download", "invalid_proxy", err)
		}
	}
	// Validate mutual exclusion: --start-at and --start-in are mutually exclusive
	startAtValue := ctx.String("start-at")
	startInValue := ctx.String("start-in")
	if err := validateStartAtStartInExclusion(startAtValue, startInValue); err != nil {
		return cmdcommon.PrintErrWithCmdHelp(ctx, err)
	}
	// Validate --start-in flag: parse duration and resolve to absolute time
	if startInValue != "" {
		resolvedAt, err := parseStartIn(startInValue)
		if err != nil {
			return cmdcommon.PrintErrWithCmdHelp(ctx, err)
		}
		// --start-in resolved to absolute time; set startAtValue
		startAtValue = resolvedAt.Format(startAtLayout)
	}
	// Validate --start-at flag
	if startAtValue != "" {
		if _, err := parseStartAt(startAtValue); err != nil {
			return cmdcommon.PrintErrWithCmdHelp(ctx, err)
		}
		var warning string
		startAtValue, warning = validateStartAt(startAtValue)
		if warning != "" {
			fmt.Println(warning)
		}
	}
	// Validate --schedule flag (T066)
	scheduleValue := ctx.String("schedule")
	if scheduleValue != "" {
		if err := validateSchedule(scheduleValue); err != nil {
			return cmdcommon.PrintErrWithCmdHelp(ctx, err)
		}
		// Warn if no occurrence in next year
		if !hasOccurrenceWithinYear(scheduleValue, time.Now()) {
			fmt.Printf("warning: cron expression %q has no occurrence in the next year\n", scheduleValue)
		}
	}
	d, err := client.Download(
		url,
		fileName,
		dlPath,
		buildDownloadOpts(ctx, headers, startAtValue, scheduleValue),
	)
	if err != nil {
		if isAuthRequiredError(err) {
			// The daemon's plugin extractor called getAccessToken() on
			// an unauthenticated account and triggerFlowAndAwait timed
			// out without a resolver. We don't know the plugin id from
			// this error alone (common.AuthLoginResult doesn't carry
			// PluginID either), so point the user at the generic
			// recovery command. Once the daemon starts broadcasting
			// UPDATE_AUTH_REQUIRED during downloads this whole branch
			// becomes the degraded-path fallback.
			fmt.Fprintln(os.Stderr, "This download requires authentication.")
			fmt.Fprintln(os.Stderr, "Run `warp auth login <plugin-id>` for the plugin that matches this URL and retry.")
			fmt.Fprintln(os.Stderr, "Use `warp ext list` to see installed plugins and their IDs.")
		}
		return cmdcommon.PrintRuntimeErr(ctx, "info", "download", err)
	}
	txt := fmt.Sprintf(`
Download Info
Name`+"\t\t"+`: %s
Size`+"\t\t"+`: %s
Save Location`+"\t"+`: %s/
Max Connections`+"\t"+`: %d
`,
		d.FileName,
		d.ContentLength.String(),
		d.DownloadDirectory,
		d.MaxConnections,
	)
	if d.MaxSegments != 0 {
		txt += fmt.Sprintf("Max Segments\t: %d\n", d.MaxSegments)
	}
	fmt.Println(txt)

	// A scheduled transfer has no live progress stream until its trigger
	// time. Attaching here would block the CLI for minutes, hours, or forever
	// for a recurring schedule, despite successful registration.
	if startAtValue != "" || scheduleValue != "" {
		fmt.Printf("Scheduled download %s.\n", d.DownloadId)
		fmt.Println("Use 'warpdl list' to check its next trigger.")
		return nil
	}

	if ctx.Bool("background") {
		fmt.Printf("Started download %s in background.\n", d.DownloadId)
		fmt.Printf("Use 'warpdl attach %s' to view progress.\n", d.DownloadId)
		fmt.Println("Use 'warpdl list' to check status.")
		return nil
	}

	RegisterHandlers(client, int64(d.ContentLength))
	registerAuthRequiredHandler(client)
	return client.Listen()
}

// authRequiredHandler adapts the warpcli Handler interface to the
// HandleAuthRequired orchestrator. It decodes the pushed
// AuthLoginResult and — if either UserCode (device flow) or
// AuthorizeURL (PKCE flow) is populated — invokes the orchestrator.
//
// KNOWN GAP: as of Task 18 the daemon does NOT push
// UPDATE_AUTH_REQUIRED during a download. The downloadHandler RPC in
// internal/api/download.go runs elEngine.Extract synchronously; the
// plugin's getAccessToken blocks in triggerFlowAndAwait until
// ErrFlowTimeout (5 min) then surfaces as a regular RPC error.
// Implementing the push requires:
//   - plumbing a *server.Pool (or an equivalent notifier) into the
//     auth provider's flow-trigger path
//   - broadcasting UPDATE_AUTH_REQUIRED on a newly-allocated uid
//     before blocking on Await
//   - extending common.AuthLoginResult with PluginID + Account so the
//     CLI knows which plugin to prompt for
//
// Registering the handler now is cheap wiring that makes the CLI
// ready for the future push — today it simply never fires.
type authRequiredHandler struct {
	client authRPC
	out    io.Writer
}

// Handle satisfies warpcli.Handler. It tolerates malformed payloads
// gracefully (prints to stderr, returns nil so the listener keeps
// serving download progress updates).
//
// HandleAuthRequired can issue follow-up RPCs. Run the interactive flow in a
// goroutine so this handler returns promptly and the listener can continue
// dispatching download-progress updates.
func (h *authRequiredHandler) Handle(m json.RawMessage) error {
	var res common.AuthLoginResult
	if err := json.Unmarshal(m, &res); err != nil {
		fmt.Fprintf(os.Stderr, "auth_required push: invalid payload: %v\n", err)
		return nil
	}
	// The pushed payload lacks a PluginID field today
	// (common.AuthLoginResult predates Task 18's push wiring). Pass
	// an empty string; HandleAuthRequired falls back to a generic
	// prompt label.
	//
	// context.Background() gives HandleAuthRequired its own
	// authLoginTimeout budget — the outer download listener has no
	// timeout to inherit.
	go func() {
		if err := HandleAuthRequired(context.Background(), h.out, h.client, "", "default", &res, false); err != nil {
			fmt.Fprintf(os.Stderr, "auth flow failed: %v\n", err)
		}
	}()
	return nil
}

// registerAuthRequiredHandler installs a handler that reacts to
// UPDATE_AUTH_REQUIRED pushes during a download. Safe to call even if
// the daemon never pushes — the handler simply never fires.
func registerAuthRequiredHandler(client *warpcli.Client) {
	client.AddHandler(common.UPDATE_AUTH_REQUIRED, &authRequiredHandler{
		client: client,
		out:    os.Stdout,
	})
}

// downloadBatchFromFile handles batch download from an input file.
// It reads URLs from the file, combines with any direct URL arguments,
// and downloads them all using the batch download logic.
func downloadBatchFromFile(ctx *cli.Context, client *warpcli.Client, inputFile string) error {
	fmt.Println(">> Initiating WARP batch download << ")
	if fileName != "" {
		return cmdcommon.PrintErrWithCmdHelp(
			ctx,
			errors.New("--file-name cannot be used with --input-file because each URL requires a distinct output name"),
		)
	}

	// Resolve download path
	resolvedPath, err := resolveDownloadPath(dlPath)
	if err != nil {
		return cmdcommon.PrintRuntimeErr(ctx, "download", "resolve_path", err)
	}

	// Build headers
	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}

	// Parse and append cookie flags
	cookies := ctx.StringSlice("cookie")
	headers, err = AppendCookieHeader(headers, cookies)
	if err != nil {
		return cmdcommon.PrintRuntimeErr(ctx, "download", "parse_cookies", err)
	}

	// Validate proxy if provided
	if proxyURL != "" {
		if _, err := warplib.ParseProxyURL(proxyURL); err != nil {
			return cmdcommon.PrintRuntimeErr(ctx, "download", "invalid_proxy", err)
		}
	}

	startAtValue := ctx.String("start-at")
	startInValue := ctx.String("start-in")
	if err := validateStartAtStartInExclusion(startAtValue, startInValue); err != nil {
		return cmdcommon.PrintErrWithCmdHelp(ctx, err)
	}
	if startInValue != "" {
		resolvedAt, err := parseStartIn(startInValue)
		if err != nil {
			return cmdcommon.PrintErrWithCmdHelp(ctx, err)
		}
		startAtValue = resolvedAt.Format(startAtLayout)
	}
	if startAtValue != "" {
		if _, err := parseStartAt(startAtValue); err != nil {
			return cmdcommon.PrintErrWithCmdHelp(ctx, err)
		}
		var warning string
		startAtValue, warning = validateStartAt(startAtValue)
		if warning != "" {
			fmt.Println(warning)
		}
	}
	scheduleValue := ctx.String("schedule")
	if scheduleValue != "" {
		if err := validateSchedule(scheduleValue); err != nil {
			return cmdcommon.PrintErrWithCmdHelp(ctx, err)
		}
		if !hasOccurrenceWithinYear(scheduleValue, time.Now()) {
			fmt.Printf("warning: cron expression %q has no occurrence in the next year\n", scheduleValue)
		}
	}

	// Build download options
	opts := &BatchDownloadOpts{
		DownloadDir:  resolvedPath,
		DownloadOpts: buildDownloadOpts(ctx, headers, startAtValue, scheduleValue),
	}

	// Collect direct URLs from positional arguments
	directURLs := ctx.Args()

	fmt.Printf("Input file: %s\n", inputFile)
	if len(directURLs) > 0 {
		fmt.Printf("Additional URLs: %d\n", len(directURLs))
	}

	// Perform batch download
	result, err := DownloadBatch(client, inputFile, directURLs, opts)
	if err != nil {
		return cmdcommon.PrintRuntimeErr(ctx, "download", "batch_download", err)
	}

	// Scheduled batch submissions are accepted work, but they cannot be
	// attached until their future trigger. Return the submission summary
	// immediately just as the single-download path does.
	scheduled := startAtValue != "" || scheduleValue != ""
	if !ctx.Bool("background") && !scheduled {
		_ = client.Close()
		waitForBatchSubmissions(result)
	}
	if scheduled {
		fmt.Println("Scheduled batch submissions; use 'warpdl list' to check their next triggers.")
	}

	// Print summary using BatchResult's String() method
	fmt.Println()
	fmt.Print(result.String())

	if result.HasErrors() {
		return cmdcommon.PrintRuntimeErr(
			ctx,
			"download",
			"batch",
			fmt.Errorf("%d of %d downloads failed", result.Failed, result.Total),
		)
	}
	return nil
}

func waitForBatchSubmissions(result *BatchResult) {
	for _, submission := range result.Submissions {
		if err := waitForBatchSubmission(submission); err != nil {
			result.ConvertSuccessToError(submission.URL, err)
		}
	}
}

func waitForBatchSubmission(submission BatchSubmission) error {
	client, err := getClient()
	if err != nil {
		return err
	}
	client.CheckVersionMismatch(currentBuildArgs.Version)

	_, err = client.AttachDownload(submission.DownloadID)
	if err != nil {
		_ = client.Close()
		if waitForBatchCompletionGrace(submission) {
			return nil
		}
		return err
	}

	terminal := &batchTerminalState{}
	registerBatchWaitHandlers(client, terminal)
	err = client.Listen()
	if terminal.failed != nil {
		return terminal.failed
	}
	if terminal.completed {
		return nil
	}
	if terminal.stopped {
		return fmt.Errorf("download %s stopped before completion", submission.DownloadID)
	}
	if err != nil && !waitForBatchCompletionGrace(submission) {
		return err
	}
	if !waitForBatchCompletionGrace(submission) {
		return fmt.Errorf("download %s did not complete", submission.DownloadID)
	}
	return nil
}

func waitForBatchCompletionGrace(submission BatchSubmission) bool {
	deadline := time.Now().Add(batchCompletionGrace)
	for {
		if isBatchSubmissionCompleteInManager(submission) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func isBatchSubmissionCompleteInManager(submission BatchSubmission) bool {
	client, err := getClient()
	if err != nil {
		return false
	}
	defer client.Close()

	resp, err := client.List(&warpcli.ListOpts{
		ShowCompleted: true,
		ShowPending:   true,
	})
	if err != nil {
		return false
	}

	for _, item := range resp.Items {
		if item == nil || item.Hash != submission.DownloadID {
			continue
		}
		total := int64(item.TotalSize)
		if total <= 0 {
			return false
		}
		// Parts becomes nil only in Manager's MAIN_HASH completion handler,
		// after integrity/checksum validation and destination sync. A matching
		// byte count alone is not an authoritative success state.
		return item.Parts == nil && int64(item.Downloaded) == total
	}

	return false
}

type batchTerminalState struct {
	completed bool
	stopped   bool
	failed    error
}

func registerBatchWaitHandlers(client *warpcli.Client, terminal *batchTerminalState) {
	registerDownloadErrorHandler(client, &terminal.failed)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler("", func(dr *common.DownloadingResponse) error {
			if dr.Hash != warplib.MAIN_HASH {
				return nil
			}
			switch dr.Action {
			case common.DownloadComplete:
				terminal.completed = true
				client.Disconnect()
			case common.DownloadStopped:
				terminal.stopped = true
				client.Disconnect()
			}
			return nil
		}),
	)
}
