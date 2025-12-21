package cmd

import (
	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/pkg/credman"
)

const cookieKeyEnv = "WARPDL_COOKIE_KEY"

func getCookieManager(*cli.Context) (*credman.CookieManager, error) {
	return &credman.CookieManager{}, nil
}
