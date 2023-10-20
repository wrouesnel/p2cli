package envutil

import (
	"os"
	"strings"

	"github.com/wrouesnel/p2cli/pkg/errdefs"
)

// FromEnvironment consumes the environment and outputs a valid input data field into the
// supplied map.
func FromEnvironment(env []string) (map[string]string, error) {
	results := map[string]string{}

	if env == nil {
		env = os.Environ()
	}

	const expectedArgs = 2

	for _, keyval := range env {
		splitKeyVal := strings.SplitN(keyval, "=", expectedArgs)
		if len(splitKeyVal) != expectedArgs {
			return results, error(errdefs.EnvironmentVariablesError{
				Reason:    "Could not find an equals value to split on",
				RawEnvVar: keyval,
			})
		}
		results[splitKeyVal[0]] = splitKeyVal[1]
	}

	return results, nil
}
