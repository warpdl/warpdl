package cmd

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
)

var queueCmd = cli.Command{
	Name:  "queue",
	Usage: "manage download queue",
	Subcommands: []cli.Command{
		{
			Name:   "status",
			Usage:  "show queue status",
			Action: queueStatusAction,
			Flags:  globalFlags,
		},
		{
			Name:   "pause",
			Usage:  "pause the queue (no new downloads start)",
			Action: queuePauseAction,
			Flags:  globalFlags,
		},
		{
			Name:   "resume",
			Usage:  "resume the queue",
			Action: queueResumeAction,
			Flags:  globalFlags,
		},
		{
			Name:      "move",
			Usage:     "move a queued download to a new position",
			ArgsUsage: "<hash> <position>",
			Action:    queueMoveAction,
			Flags:     globalFlags,
		},
	},
	Action: queueStatusAction,
	Flags:  globalFlags,
}

func queueStatusAction(ctx *cli.Context) error {
	if ctx.Args().First() == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	client, err := getClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "queue", "new_client", err)
		return nil
	}
	defer client.Close()

	status, err := client.QueueStatus()
	if err != nil {
		common.PrintRuntimeErr(ctx, "queue", "get_status", err)
		return nil
	}

	// Print queue status
	pausedStr := "running"
	if status.Paused {
		pausedStr = "paused"
	}
	fmt.Printf("Queue Status: %s\n", pausedStr)
	fmt.Printf("Max Concurrent: %d\n", status.MaxConcurrent)
	fmt.Printf("Active: %d, Waiting: %d\n", status.ActiveCount, status.WaitingCount)

	if len(status.Active) > 0 {
		fmt.Println("\nActive downloads:")
		for i, hash := range status.Active {
			fmt.Printf("  %d. %s\n", i+1, hash)
		}
	}

	if len(status.Waiting) > 0 {
		fmt.Println("\nWaiting queue:")
		for _, item := range status.Waiting {
			priority := priorityName(item.Priority)
			fmt.Printf("  %d. %s [%s]\n", item.Position+1, item.Hash, priority)
		}
	}

	return nil
}

func queuePauseAction(ctx *cli.Context) error {
	if ctx.Args().First() == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	client, err := getClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "queue pause", "new_client", err)
		return nil
	}
	defer client.Close()

	if err := client.QueuePause(); err != nil {
		common.PrintRuntimeErr(ctx, "queue pause", "pause", err)
		return nil
	}
	fmt.Println("Queue paused. No new downloads will start automatically.")
	return nil
}

func queueResumeAction(ctx *cli.Context) error {
	if ctx.Args().First() == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	client, err := getClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "queue resume", "new_client", err)
		return nil
	}
	defer client.Close()

	if err := client.QueueResume(); err != nil {
		common.PrintRuntimeErr(ctx, "queue resume", "resume", err)
		return nil
	}
	fmt.Println("Queue resumed. Waiting downloads will start automatically.")
	return nil
}

func queueMoveAction(ctx *cli.Context) error {
	args := ctx.Args()
	if args.First() == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	if len(args) < 2 {
		return common.PrintErrWithCmdHelp(
			ctx,
			errors.New("usage: warpdl queue move <hash> <position>"),
		)
	}

	hash := args.Get(0)
	posStr := args.Get(1)
	position, err := strconv.Atoi(posStr)
	if err != nil {
		return common.PrintErrWithCmdHelp(
			ctx,
			fmt.Errorf("invalid position '%s': must be a number", posStr),
		)
	}

	client, err := getClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "queue move", "new_client", err)
		return nil
	}
	defer client.Close()

	if err := client.QueueMove(hash, position); err != nil {
		common.PrintRuntimeErr(ctx, "queue move", "move", err)
		return nil
	}
	fmt.Printf("Moved %s to position %d.\n", hash, position)
	return nil
}

// priorityName converts priority int to human-readable string.
func priorityName(p int) string {
	switch p {
	case 0:
		return "low"
	case 1:
		return "normal"
	case 2:
		return "high"
	default:
		return "unknown"
	}
}
