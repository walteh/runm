package env

import (
	"slices"
)

func GuessCurrentShimMode(args []string) string {

	if slices.Contains(args, "-info") {
		return "info"
	}

	if slices.Contains(args, "delete") {
		return "delete"
	}

	if slices.Contains(args, "start") {
		return "start"
	}

	return "primary"
}
