package main

import "github.com/sonicfromnewyoke/mithril/pkg/version"

// Re-export version variables for CLI usage
// These are set via ldflags at build time in pkg/version
var (
	Version   = version.Version
	GitCommit = version.GitCommit
	BuildDate = version.BuildDate
)
