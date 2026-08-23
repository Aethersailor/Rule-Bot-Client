package client

import (
	"fmt"
	"runtime"
)

var (
	BuildVersion = "dev"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
	BuildTarget  = "unknown"
)

func VersionString() string {
	return fmt.Sprintf("rule-bot-client %s commit=%s built=%s go=%s target=%s", BuildVersion, BuildCommit, BuildDate, runtime.Version(), BuildTarget)
}
