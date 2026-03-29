package web

import "embed"

// PlexDashboard is the dashboard static tree (used when no web/plex-dashboard exists on disk).
//
//go:embed all:plex-dashboard
var PlexDashboard embed.FS
