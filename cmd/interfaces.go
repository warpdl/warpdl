package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// resolveInterfacePolicy resolves the command's interface policy before the
// daemon is contacted. An explicit flag wins, then WARPDL_INTERFACES, then
// "off" for a new download. Resume omits the field when neither is set so the
// saved policy is kept. The daemon does not read WARPDL_INTERFACES.
func resolveInterfacePolicy(ctx *cli.Context, newDownload bool) string {
	if ctx != nil && ctx.IsSet("interfaces") {
		return strings.TrimSpace(ctx.String("interfaces"))
	}
	if value, ok := os.LookupEnv(common.InterfacesEnv); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if newDownload {
		return "off"
	}
	return ""
}

// segmentLimitChosen reports whether the caller set a segment cap, including
// when the number is 200. The filled-in flag default is not a choice.
func segmentLimitChosen(ctx *cli.Context) bool {
	if ctx != nil && ctx.IsSet("max-parts") {
		return true
	}
	_, ok := os.LookupEnv("WARP_MAX_PARTS")
	return ok
}

// interfaces lists device names, IPv4 addresses, hardware port names, and
// whether automatic selection would use each device. It does not talk to the
// daemon.
func interfaces(ctx *cli.Context) error {
	list, err := warplib.ListInterfaces()
	if err != nil {
		return fmt.Errorf("list network interfaces: %w", err)
	}
	if _, err = fmt.Fprintf(ctx.App.Writer, "%-16s %-16s %-24s %s\n", "NAME", "IPV4", "PORT", "AUTO"); err != nil {
		return err
	}
	for _, iface := range list {
		ip := "-"
		if iface.IPv4 != nil {
			ip = iface.IPv4.String()
		}
		port := iface.PortName
		if port == "" {
			port = "-"
		}
		auto := "no"
		if iface.Auto {
			auto = "yes"
		}
		if _, err = fmt.Fprintf(ctx.App.Writer, "%-16s %-16s %-24s %s\n", iface.Name, ip, port, auto); err != nil {
			return err
		}
	}
	return nil
}
