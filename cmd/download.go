package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

var (
	dlPath   string
	fileName string

	dlFlags = []cli.Flag{
		cli.StringFlag{
			Name:        "file-name, o",
			Usage:       "explicitly set the name of file (determined automatically if not specified)",
			Destination: &fileName,
		},
		cli.StringFlag{
			Name:        "download-path, l",
			Usage:       "set the path where downloaded file should be saved",
			Value:       "",
			Destination: &dlPath,
		},
	}
)

func download(ctx *cli.Context) (err error) {
	url := ctx.Args().First()
	if url == "" {
		if ctx.Command.Name == "" {
			return common.Help(ctx)
		}
		return common.PrintErrWithCmdHelp(
			ctx,
			errors.New("no url provided"),
		)
	} else if url == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	client, err := warpcli.NewClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "download", "new_client", err)
		return
	}
	client.CheckVersionMismatch(currentBuildArgs.Version)
	fmt.Println(">> Initiating a WARP download << ")
	url = strings.TrimSpace(url)

	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}
	cwd, err := os.Getwd()
	if err != nil {
		common.PrintRuntimeErr(ctx, "download", "getwd", err)
		return nil
	}
	if dlPath == "" {
		dlPath = cwd
	}

	// Check if file exists and handle accordingly
	finalFileName := fileName
	
	// Only check for existing files if we have a filename or can fetch it
	if finalFileName == "" {
		// Try to fetch filename from server
		d, err := warplib.NewDownloader(
			&http.Client{},
			url,
			&warplib.DownloaderOpts{
				Headers:   headers,
				SkipSetup: true,
			},
		)
		if err == nil {
			finalFileName = d.GetFileName()
		}
		// If we can't get filename, proceed without checking - the daemon will handle it
	}

	// If we have a filename, check if file exists at target location
	if finalFileName != "" {
		targetPath := filepath.Join(dlPath, finalFileName)
		if fileExists(targetPath) {
			fmt.Printf("File '%s' already exists in '%s'.\n", finalFileName, dlPath)
			fmt.Println("What would you like to do?")
			fmt.Println("1. Replace the existing file")
			fmt.Println("2. Save with a different name")
			fmt.Println("3. Cancel download")
			fmt.Print("Enter your choice (1/2/3): ")
			
			var choice string
			_, _ = fmt.Scanf("%s", &choice)
			choice = strings.TrimSpace(choice)
			
			switch choice {
			case "1":
				// User chose to replace - proceed with download
				fmt.Println("Replacing existing file...")
			case "2":
				// Generate a unique filename
				finalFileName = generateUniqueFileName(dlPath, finalFileName)
				fmt.Printf("Saving as: %s\n", finalFileName)
			case "3":
				fmt.Println("Download cancelled.")
				return nil
			default:
				fmt.Println("Invalid choice. Download cancelled.")
				return nil
			}
		}
	}

	d, err := client.Download(url, finalFileName, dlPath, &warpcli.DownloadOpts{
		ForceParts:     forceParts,
		MaxConnections: int32(maxConns),
		MaxSegments:    int32(maxParts),
		Headers:        headers,
	})
	if err != nil {
		common.PrintRuntimeErr(ctx, "info", "download", err)
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
	RegisterHandlers(client, int64(d.ContentLength))
	return client.Listen()
}

// fileExists checks if a regular file exists at the given path.
func fileExists(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !stat.IsDir()
}

// generateUniqueFileName generates a unique filename by appending a counter.
// For example: "file.txt" -> "file (1).txt", "file (2).txt", etc.
func generateUniqueFileName(dir, filename string) string {
	ext := filepath.Ext(filename)
	nameWithoutExt := strings.TrimSuffix(filename, ext)
	
	counter := 1
	newName := filename
	for {
		testPath := filepath.Join(dir, newName)
		if !fileExists(testPath) {
			return newName
		}
		newName = fmt.Sprintf("%s (%d)%s", nameWithoutExt, counter, ext)
		counter++
	}
}
