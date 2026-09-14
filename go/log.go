package caution

import (
	"os"
	"strings"

	"github.com/rs/zerolog"
)

func init() {
	ev := os.Getenv("CLOG_VERBOSE")

	if strings.EqualFold(ev, "v") {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else if strings.EqualFold(ev, "vv") {
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
