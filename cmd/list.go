package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// formatScheduleColumn returns the display string for the Scheduled column.
// For scheduled items: shows countdown (e.g. "in 1h30m") if within 24h, else date/time.
// For missed items: shows "was YYYY-MM-DD HH:MM (starting now)".
// For recurring items: shows cron expression and next scheduled time.
// For unscheduled items: shows "—".
func formatScheduleColumn(item *warplib.Item) string {
	switch item.ScheduleState {
	case warplib.ScheduleStateMissed:
		if !item.ScheduledAt.IsZero() {
			return "was " + item.ScheduledAt.Format("2006-01-02 15:04") + " (starting now)"
		}
		return "missed"
	case warplib.ScheduleStateScheduled:
		// T071: For recurring items, show cron expression and next scheduled time.
		if item.CronExpr != "" {
			if !item.ScheduledAt.IsZero() {
				nextStr := item.ScheduledAt.Format("2006-01-02 15:04")
				return fmt.Sprintf("(recurring: %s, next: %s)", item.CronExpr, nextStr)
			}
			return fmt.Sprintf("(recurring: %s)", item.CronExpr)
		}
		if !item.ScheduledAt.IsZero() {
			remaining := time.Until(item.ScheduledAt)
			if remaining > 0 && remaining <= 24*time.Hour {
				return formatCountdown(remaining)
			}
			return item.ScheduledAt.Format("01-02 15:04")
		}
		return "\u2014"
	default:
		return "\u2014"
	}
}

// formatCountdown formats a duration as a human-readable countdown string (e.g., "in 1h30m").
func formatCountdown(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("in %dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("in %dh", h)
	case m > 0:
		return fmt.Sprintf("in %dm", m)
	default:
		s := int(d.Seconds())
		if s > 0 {
			return fmt.Sprintf("in %ds", s)
		}
		return "now"
	}
}

var (
	showHidden    bool
	showCompleted bool
	showPending   bool
	showAll       bool

	lsFlags = []cli.Flag{
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
)

func list(ctx *cli.Context) error {
	if ctx.Args().First() == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	client, err := getClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "list", "new_client", err)
		return nil
	}
	defer client.Close()
	l, err := client.List(&warpcli.ListOpts{
		ShowCompleted: showCompleted || showAll,
		ShowPending:   showPending || showAll,
	})
	if err != nil {
		common.PrintRuntimeErr(ctx, "list", "get_list", err)
		return nil
	}
	fback := func() error {
		fmt.Println("warp: no downloads found")
		return nil
	}
	if len(l.Items) == 0 {
		return fback()
	}
	txt := "Here are your downloads:"
	txt += "\n\n------------------------------------------------------------------------"
	txt += "\n|Num|\t         Name         | Unique Hash | Status |   Scheduled    |"
	txt += "\n|---|-------------------------|-------------|--------|----------------|"
	var i int
	sort.Sort(warplib.ItemSlice(l.Items))
	for _, item := range l.Items {
		if !showHidden && (item.Hidden || item.Children) {
			continue
		}
		i++
		name := item.Name
		n := len(name)
		switch {
		case n > 23:
			name = name[:20] + "..."
		case n < 23:
			name = common.Beaut(name, 23)
		}
		perc := fmt.Sprintf(`%d%%`, item.GetPercentage())
		sched := formatScheduleColumn(item)
		txt += fmt.Sprintf("\n| %d | %s |   %s  |  %s  | %s |", i, name, item.Hash, common.Beaut(perc, 4), common.Beaut(sched, 14))
	}
	if i == 0 {
		return fback()
	}
	txt += "\n------------------------------------------------------------------------"
	fmt.Println(txt)
	return nil
}
