package main

const HELP_TEMPL = `Usage: {{if .UsageText}}{{.UsageText}}{{else}}{{.HelpName}} {{if .VisibleFlags}}[global options]{{end}}{{if .Commands}} command [command options]{{end}} {{if .ArgsUsage}}{{.ArgsUsage}}{{else}}[arguments...]{{end}}{{end}}
{{.Description}}{{if .VisibleCommands}}
Commands:{{range .VisibleCategories}}{{if .Name}}

{{.Name}}:{{range .VisibleCommands}}
  {{join .Names ", "}}{{"\t"}}{{.Usage}}{{end}}{{else}}{{range .VisibleCommands}}
{{"\t"}}{{index .Names 0}}{{"\t:\t"}}{{.Usage}}{{end}}{{end}}{{end}}{{end}}{{if .VisibleFlags}}{{end}}

Use "{{.HelpName}} help <command>" for more information about any command.

`

const CMD_HELP_TEMPL = `{{if .Description}}{{.Description}}{{else}}{{.HelpName}} - {{.Usage}}

{{end}}Usage:
        {{.HelpName}} {{if .UsageText}}{{.UsageText}}{{else}}[arguments...]{{end}}{{if .VisibleFlags}}

Supported Flags:{{range .VisibleFlags}}
  {{.}}{{end}}{{end}}

`
