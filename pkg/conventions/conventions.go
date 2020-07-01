package conventions

import (
	"fmt"
	"regexp"
)

var (
	branchRegexp = regexp.MustCompile("^release-([0-9].[0-9])$")
)

func ReleaseNameFromBranchName(branchName string) (string, error) {
	if branchName == "master" {
		return "latest", nil
	}

	matches := branchRegexp.FindStringSubmatch(branchName)
	if len(matches) > 0 {
		if len(matches) != 2 {
			panic("faulty regex")
		}

		return matches[1], nil
	}

	return "", fmt.Errorf("can't parse release name from branch %q", branchName)
}
