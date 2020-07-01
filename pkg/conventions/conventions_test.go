package conventions

import (
	"errors"
	"reflect"
	"testing"
)

func TestReleaseNameFromBranchName(t *testing.T) {
	tt := []struct {
		name                string
		branchName          string
		expectedReleaseName string
		expectedError       error
	}{
		{
			name:                "master -> latest",
			branchName:          "master",
			expectedReleaseName: "latest",
			expectedError:       nil,
		},
		{
			name:                " -> 4.5",
			branchName:          "release-4.5",
			expectedReleaseName: "4.5",
			expectedError:       nil,
		},
		{
			name:                "invalid name - prefix",
			branchName:          "xrelease-4.5",
			expectedReleaseName: "",
			expectedError:       errors.New(`can't parse release name from branch "xrelease-4.5"`),
		},
		{
			name:                "invalid name - suffix",
			branchName:          "release-4.5x",
			expectedReleaseName: "",
			expectedError:       errors.New(`can't parse release name from branch "release-4.5x"`),
		},
		{
			name:                "invalid name - z stream",
			branchName:          "release-4.5.1",
			expectedReleaseName: "",
			expectedError:       errors.New(`can't parse release name from branch "release-4.5.1"`),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReleaseNameFromBranchName(tc.branchName)
			if !reflect.DeepEqual(err, tc.expectedError) {
				t.Errorf("expected err %v, got %v", tc.expectedError, err)
			}

			if got != tc.expectedReleaseName {
				t.Errorf("expected release name %q, got %q", tc.expectedReleaseName, got)
			}
		})
	}
}
