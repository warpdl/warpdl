package main

import "github.com/urfave/cli"

func init() {
	rsFlags = append(rsFlags, infoFlags...)
	dlFlags = append(dlFlags, rsFlags...)
}

var (
	maxParts   int
	maxConns   int
	dlPath     string
	fileName   string
	forceParts bool
	timeTaken  bool
	userAgent  string
)

var dlFlags = []cli.Flag{
	cli.StringFlag{
		Name:        "file-name, o",
		Usage:       "explicitly set the name of file (determined automatically if not specified)",
		Destination: &fileName,
	},
	cli.StringFlag{
		Name:        "download-path, l",
		Usage:       "set the path where downloaded file should be saved",
		Value:       ".",
		Destination: &dlPath,
	},
}

var rsFlags = []cli.Flag{
	cli.IntFlag{
		Name:        "max-parts, s",
		Usage:       "to specify the number of maximum file segments",
		EnvVar:      "WARP_MAX_PARTS",
		Destination: &maxParts,
		Value:       DEF_MAX_PARTS,
	},
	cli.IntFlag{
		Name:        "max-connection, x",
		Usage:       "specify the number of maximum parallel connection",
		EnvVar:      "WARP_MAX_CONN",
		Destination: &maxConns,
		Value:       DEF_MAX_CONNS,
	},
	cli.BoolTFlag{
		Name:        "force-parts, f",
		Usage:       "forceful file segmentation (default: true)",
		EnvVar:      "WARP_FORCE_SEGMENTS",
		Destination: &forceParts,
	},
	cli.BoolFlag{
		Name:        "time-taken, e",
		Destination: &timeTaken,
		Hidden:      true,
	},
}

var infoFlags = []cli.Flag{
	cli.StringFlag{
		Name:        "user-agent",
		Usage:       "HTTP user agent to use for downloding (default: warp)",
		Destination: &userAgent,
	},
}

var (
	showHidden    bool
	showCompleted bool
	showPending   bool
	showAll       bool
)

var lsFlags = []cli.Flag{
	cli.BoolFlag{
		Name:        "show-completed, c",
		Usage:       "use this flag to list completed downloads (default: false)",
		Destination: &showCompleted,
	},
	cli.BoolTFlag{
		Name:        "show-pending, p",
		Usage:       "use this flag to include pending downloads (default: true)",
		Destination: &showPending,
	},
	cli.BoolFlag{
		Name:        "show-all, a",
		Usage:       "use this flag to list all downloads (default: false)",
		Destination: &showAll,
	},
	cli.BoolFlag{
		Name:        "show-hidden, g",
		Usage:       "use this flag to list hidden downloads (default: false)",
		Destination: &showHidden,
	},
}

var (
	forceFlush bool
)

var flsFlags = []cli.Flag{
	cli.BoolFlag{
		Name:        "force, f",
		Usage:       "use this flag to force flush (default: false)",
		Destination: &forceFlush,
	},
}
