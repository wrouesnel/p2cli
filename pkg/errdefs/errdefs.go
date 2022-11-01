package errdefs

import "fmt"

// EnvironmentVariablesError is raised when an environment variable is improperly formatted.
type EnvironmentVariablesError struct {
	Reason    string
	RawEnvVar string
}

// Error implements error.
func (eev EnvironmentVariablesError) Error() string {
	return fmt.Sprintf("%s: %s", eev.Reason, eev.RawEnvVar)
}
