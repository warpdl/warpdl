package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli"
	cmdcommon "github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

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

	client, err := getClient()
	if err != nil {
		cmdcommon.PrintRuntimeErr(ctx, "download", "new_client", err)
		return
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
		cmdcommon.PrintRuntimeErr(ctx, "download", "parse_cookies", err)
		return nil
	}
	dlPath, err = resolveDownloadPath(dlPath)
	if err != nil {
		cmdcommon.PrintRuntimeErr(ctx, "download", "resolve_path", err)
		return nil
	}
	if proxyURL != "" {
		if _, err := warplib.ParseProxyURL(proxyURL); err != nil {
			cmdcommon.PrintRuntimeErr(ctx, "download", "invalid_proxy", err)
			return nil
		}
	}
	d, err := client.Download(url, fileName, dlPath, &warpcli.DownloadOpts{
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
	})
	if err != nil {
		cmdcommon.PrintRuntimeErr(ctx, "info", "download", err)
		return nil
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

	if ctx.Bool("background") {
		fmt.Printf("Started download %s in background.\n", d.DownloadId)
		fmt.Printf("Use 'warpdl attach %s' to view progress.\n", d.DownloadId)
		fmt.Println("Use 'warpdl list' to check status.")
		return nil
	}

	RegisterHandlers(client, int64(d.ContentLength))
	return client.Listen()
}

// downloadBatchFromFile handles batch download from an input file.
// It reads URLs from the file, combines with any direct URL arguments,
// and downloads them all using the batch download logic.
func downloadBatchFromFile(ctx *cli.Context, client *warpcli.Client, inputFile string) error {
	fmt.Println(">> Initiating WARP batch download << ")

	// Resolve download path
	resolvedPath, err := resolveDownloadPath(dlPath)
	if err != nil {
		cmdcommon.PrintRuntimeErr(ctx, "download", "resolve_path", err)
		return nil
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
		cmdcommon.PrintRuntimeErr(ctx, "download", "parse_cookies", err)
		return nil
	}

	// Validate proxy if provided
	if proxyURL != "" {
		if _, err := warplib.ParseProxyURL(proxyURL); err != nil {
			cmdcommon.PrintRuntimeErr(ctx, "download", "invalid_proxy", err)
			return nil
		}
	}

	// Build download options
	opts := &BatchDownloadOpts{
		DownloadDir: resolvedPath,
		DownloadOpts: &warpcli.DownloadOpts{
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
		},
	}

	// Collect direct URLs from arguments (skip first if it's "download" command name)
	directURLs := ctx.Args()

	fmt.Printf("Input file: %s\n", inputFile)
	if len(directURLs) > 0 {
		fmt.Printf("Additional URLs: %d\n", len(directURLs))
	}

	// Perform batch download
	result, err := DownloadBatch(client, inputFile, directURLs, opts)
	if err != nil {
		cmdcommon.PrintRuntimeErr(ctx, "download", "batch_download", err)
		return nil
	}

	// Print summary using BatchResult's String() method
	fmt.Println()
	fmt.Print(result.String())

	return nil
}
