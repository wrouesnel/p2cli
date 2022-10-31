package envutil

import (
	"errors"
	"os"
	"strings"

	"github.com/wrouesnel/p2cli/pkg/errdefs"
)

// FromEnvironment consumes the environment and outputs a valid input data field into the
// supplied map.
func FromEnvironment(env []string) (map[string]string, error) {
	r := map[string]string{}

	if env == nil {
		return r, errors.New("nil inputData map supplied")
	}

	if env == nil {
		env = os.Environ()
	}

	for _, keyval := range env {
		splitKeyVal := strings.SplitN(keyval, "=", 2)
		if len(splitKeyVal) != 2 {
			return r, error(errdefs.ErrorEnvironmentVariables{
				Reason:    "Could not find an equals value to split on",
				RawEnvVar: keyval,
			})
		}
	}

	return r, nil
}
