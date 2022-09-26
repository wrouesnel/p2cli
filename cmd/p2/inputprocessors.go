package main

import (
	"errors"
	"os"
	"strings"
)

// fromEnvironment consumes the environment and outputs a valid input data field into the
// supplied map.
func fromEnvironment(inputData map[string]interface{}) error {
	if inputData == nil {
		return errors.New("nil inputData map supplied")
	}

	for _, keyval := range os.Environ() {
		splitKeyVal := strings.SplitN(keyval, "=", 2)
		if len(splitKeyVal) != 2 {
			return error(ErrorEnvironmentVariables{
				Reason:    "Could not find an equals value to split on",
				RawEnvVar: keyval,
			})
		}

		// os.Environ consumption has special-case logic. Since there's all sorts of things
		// which can end up in the environment, we want to filter here only for keys which we
		// like.
		if reIdentifiers.MatchString(splitKeyVal[0]) {
			inputData[splitKeyVal[0]] = splitKeyVal[1]
		}
	}

	return nil
}
