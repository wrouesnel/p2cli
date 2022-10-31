package errdefs

import "fmt"

// ErrorEnvironmentVariables is raised when an environment variable is improperly formatted
type ErrorEnvironmentVariables struct {
	Reason    string
	RawEnvVar string
}

// Error implements error
func (eev ErrorEnvironmentVariables) Error() string {
	return fmt.Sprintf("%s: %s", eev.Reason, eev.RawEnvVar)
}
