// Package sysupdate holds files shared by the sysupdate command, such as the
// embedded user guide. The command itself lives in cmd/sysupdate.
package sysupdate

import _ "embed"

// Help is the user guide (HELP.md), shown by the TUI help screen and `sysupdate help --full`.
//
//go:embed HELP.md
var Help string
