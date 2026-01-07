package auth

import (
	"io"

	"github.com/spf13/pflag"
	"go.podman.io/common/pkg/completion"
)

// LoginOptions represents common flags in login
// In addition, the caller should probably provide a --tls-verify flag (that affects the provided
// *types.SystemContest).
type LoginOptions struct {
	// CLI flags managed by the FlagSet returned by GetLoginFlags
	// Callers that use GetLoginFlags should not need to touch these values at all; callers that use
	// other CLI frameworks should set them based on user input.
	AuthFile             string
	DockerCompatAuthFile string
	CertDir              string
	Password             string
	Username             string
	StdinPassword        bool
	GetLoginSet          bool
	Verbose              bool // set to true for verbose output
	AcceptRepositories   bool // set to true to allow namespaces or repositories rather than just registries
	// Options caller can set
	Stdin                     io.Reader // set to os.Stdin
	Stdout                    io.Writer // set to os.Stdout
	AcceptUnspecifiedRegistry bool      // set to true if allows login with unspecified registry
	NoWriteBack               bool      // set to true to not write the credentials to the authfile/cred helpers
}

// LogoutOptions represents the results for flags in logout.
type LogoutOptions struct {
	// CLI flags managed by the FlagSet returned by GetLogoutFlags
	// Callers that use GetLogoutFlags should not need to touch these values at all; callers that use
	// other CLI frameworks should set them based on user input.
	AuthFile             string
	DockerCompatAuthFile string
	All                  bool
	AcceptRepositories   bool // set to true to allow namespaces or repositories rather than just registries
	// Options caller can set
	Stdout                    io.Writer // set to os.Stdout
	AcceptUnspecifiedRegistry bool      // set to true if allows logout with unspecified registry
}

// GetLoginFlags defines and returns login flags for containers tools.
func GetLoginFlags(flags *LoginOptions) *pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.StringVar(&flags.AuthFile, "authfile", "", "path of the authentication file. Use REGISTRY_AUTH_FILE environment variable to override")
	fs.StringVar(&flags.DockerCompatAuthFile, "compat-auth-file", "", "path of a Docker-compatible config file to update instead")
	fs.StringVar(&flags.CertDir, "cert-dir", "", "use certificates at the specified path to access the registry")
	fs.StringVarP(&flags.Password, "password", "p", "", "Password for registry")
	fs.StringVarP(&flags.Username, "username", "u", "", "Username for registry")
	fs.BoolVar(&flags.StdinPassword, "password-stdin", false, "Take the password from stdin")
	fs.BoolVar(&flags.GetLoginSet, "get-login", false, "Return the current login user for the registry")
	fs.BoolVarP(&flags.Verbose, "verbose", "v", false, "Write more detailed information to stdout")
	return &fs
}

// GetLoginFlagsCompletions returns the FlagCompletions for the login flags.
func GetLoginFlagsCompletions() completion.FlagCompletions {
	flagCompletion := completion.FlagCompletions{}
	flagCompletion["authfile"] = completion.AutocompleteDefault
	flagCompletion["compat-auth-file"] = completion.AutocompleteDefault
	flagCompletion["cert-dir"] = completion.AutocompleteDefault
	flagCompletion["password"] = completion.AutocompleteNone
	flagCompletion["username"] = completion.AutocompleteNone
	return flagCompletion
}

// GetLogoutFlags defines and returns logout flags for containers tools.
func GetLogoutFlags(flags *LogoutOptions) *pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.StringVar(&flags.AuthFile, "authfile", "", "path of the authentication file. Use REGISTRY_AUTH_FILE environment variable to override")
	fs.StringVar(&flags.DockerCompatAuthFile, "compat-auth-file", "", "path of a Docker-compatible config file to update instead")
	fs.BoolVarP(&flags.All, "all", "a", false, "Remove the cached credentials for all registries in the auth file")
	return &fs
}

// GetLogoutFlagsCompletions returns the FlagCompletions for the logout flags.
func GetLogoutFlagsCompletions() completion.FlagCompletions {
	flagCompletion := completion.FlagCompletions{}
	flagCompletion["authfile"] = completion.AutocompleteDefault
	flagCompletion["compat-auth-file"] = completion.AutocompleteDefault
	return flagCompletion
}
