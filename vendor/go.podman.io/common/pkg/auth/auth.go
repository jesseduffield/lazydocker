package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	passwd "go.podman.io/common/pkg/password"
	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/pkg/docker/config"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/homedir"
)

// ErrNewCredentialsInvalid means that the new user-provided credentials are
// not accepted by the registry.
type ErrNewCredentialsInvalid struct {
	underlyingError error
	message         string
}

// Error returns the error message as a string.
func (e ErrNewCredentialsInvalid) Error() string {
	return e.message
}

// Unwrap returns the underlying error.
func (e ErrNewCredentialsInvalid) Unwrap() error {
	return e.underlyingError
}

// GetDefaultAuthFile returns env value REGISTRY_AUTH_FILE as default
// --authfile path used in multiple --authfile flag definitions
// Will fail over to DOCKER_CONFIG if REGISTRY_AUTH_FILE environment is not set
//
// WARNINGS:
//   - In almost all invocations, expect this function to return ""; so it can not be used
//     for directly accessing the file.
//   - Use this only for commands that _read_ credentials, not write them.
//     The path may refer to github.com/containers auth.json, or to Docker config.json,
//     and the distinction is lost; writing auth.json data to config.json may not be consumable by Docker,
//     or it may overwrite and discard unrelated Docker configuration set by the user.
func GetDefaultAuthFile() string {
	// Keep this in sync with the default logic in systemContextWithOptions!

	if authfile := os.Getenv("REGISTRY_AUTH_FILE"); authfile != "" {
		return authfile
	}
	// This pre-existing behavior is not conceptually consistent:
	// If users have a ~/.docker/config.json in the default path, and no environment variable
	// set, we read auth.json first, falling back to config.json;
	// but if DOCKER_CONFIG is set, we read only config.json in that path, and we don’t read auth.json at all.
	if authEnv := os.Getenv("DOCKER_CONFIG"); authEnv != "" {
		return filepath.Join(authEnv, "config.json")
	}
	return ""
}

// CheckAuthFile validates a path option, failing if the option is set but the referenced file is not accessible.
func CheckAuthFile(pathOption string) error {
	if pathOption == "" {
		return nil
	}
	if err := fileutils.Exists(pathOption); err != nil {
		return fmt.Errorf("credential file is not accessible: %w", err)
	}
	return nil
}

// systemContextWithOptions returns a version of sys
// updated with authFile, dockerCompatAuthFile and certDir values (if they are not "").
// NOTE: this is a shallow copy that can be used and updated, but may share
// data with the original parameter.
func systemContextWithOptions(sys *types.SystemContext, authFile, dockerCompatAuthFile, certDir string) (*types.SystemContext, error) {
	if sys != nil {
		sysCopy := *sys
		sys = &sysCopy
	} else {
		sys = &types.SystemContext{}
	}

	defaultDockerConfigPath := filepath.Join(homedir.Get(), ".docker", "config.json")
	switch {
	case authFile != "" && dockerCompatAuthFile != "":
		return nil, errors.New("options for paths to the credential file and to the Docker-compatible credential file can not be set simultaneously")
	case authFile != "":
		if authFile == defaultDockerConfigPath {
			logrus.Warn("saving credentials to ~/.docker/config.json, but not using Docker-compatible file format")
		}
		sys.AuthFilePath = authFile
	case dockerCompatAuthFile != "":
		sys.DockerCompatAuthFilePath = dockerCompatAuthFile
	default:
		// Keep this in sync with GetDefaultAuthFile()!
		//
		// Note that c/image does not natively implement the REGISTRY_AUTH_FILE
		// variable, so not all callers look for credentials in this location.
		if authFileVar := os.Getenv("REGISTRY_AUTH_FILE"); authFileVar != "" {
			if authFileVar == defaultDockerConfigPath {
				logrus.Warn("$REGISTRY_AUTH_FILE points to ~/.docker/config.json, but the file format is not fully compatible; use the Docker-compatible file path option instead")
			}
			sys.AuthFilePath = authFileVar
		} else if dockerConfig := os.Getenv("DOCKER_CONFIG"); dockerConfig != "" {
			// This preserves pre-existing _inconsistent_ behavior:
			// If the Docker configuration exists in the default ~/.docker/config.json location,
			// we DO NOT write to it; instead, we update auth.json in the default path.
			// Only if the user explicitly sets DOCKER_CONFIG, we write to that config.json.
			sys.DockerCompatAuthFilePath = filepath.Join(dockerConfig, "config.json")
		}
	}
	if certDir != "" {
		sys.DockerCertPath = certDir
	}
	return sys, nil
}

// Login implements a “log in” command with the provided opts and args
// reading the password from opts.Stdin or the options in opts.
func Login(ctx context.Context, systemContext *types.SystemContext, opts *LoginOptions, args []string) error {
	systemContext, err := systemContextWithOptions(systemContext, opts.AuthFile, opts.DockerCompatAuthFile, opts.CertDir)
	if err != nil {
		return err
	}

	var key, registry string
	switch len(args) {
	case 0:
		if !opts.AcceptUnspecifiedRegistry {
			return errors.New("please provide a registry to log in to")
		}
		if key, err = defaultRegistryWhenUnspecified(systemContext); err != nil {
			return err
		}
		registry = key
		logrus.Debugf("registry not specified, default to the first registry %q from registries.conf", key)

	case 1:
		key, registry, err = parseCredentialsKey(args[0], opts.AcceptRepositories)
		if err != nil {
			return err
		}

	default:
		return errors.New("login accepts only one registry to log in to")
	}

	authConfig, err := config.GetCredentials(systemContext, key)
	if err != nil {
		return fmt.Errorf("get credentials: %w", err)
	}

	if opts.GetLoginSet {
		if authConfig.Username == "" {
			return fmt.Errorf("not logged into %s", key)
		}
		fmt.Fprintf(opts.Stdout, "%s\n", authConfig.Username)
		return nil
	}
	if authConfig.IdentityToken != "" {
		return errors.New("currently logged in, auth file contains an Identity token")
	}

	password := opts.Password
	if opts.StdinPassword {
		var stdinPasswordStrBuilder strings.Builder
		if opts.Password != "" {
			return errors.New("can't specify both --password-stdin and --password")
		}
		if opts.Username == "" {
			return errors.New("must provide --username with --password-stdin")
		}
		scanner := bufio.NewScanner(opts.Stdin)
		for scanner.Scan() {
			fmt.Fprint(&stdinPasswordStrBuilder, scanner.Text())
		}
		password = stdinPasswordStrBuilder.String()
	}

	// If no username and no password is specified, try to use existing ones.
	if opts.Username == "" && password == "" && authConfig.Username != "" && authConfig.Password != "" {
		fmt.Fprintf(opts.Stdout, "Authenticating with existing credentials for %s\n", key)
		if err := docker.CheckAuth(ctx, systemContext, authConfig.Username, authConfig.Password, registry); err == nil {
			fmt.Fprintf(opts.Stdout, "Existing credentials are valid. Already logged in to %s\n", registry)
			return nil
		}
		fmt.Fprintln(opts.Stdout, "Existing credentials are invalid, please enter valid username and password")
	}

	username, password, err := getUserAndPass(opts, password, authConfig.Username)
	if err != nil {
		return fmt.Errorf("getting username and password: %w", err)
	}

	if err = docker.CheckAuth(ctx, systemContext, username, password, registry); err == nil {
		if !opts.NoWriteBack {
			// Write the new credentials to the authfile
			desc, err := config.SetCredentials(systemContext, key, username, password)
			if err != nil {
				return err
			}
			if opts.Verbose {
				fmt.Fprintln(opts.Stdout, "Used: ", desc)
			}
		}
		fmt.Fprintln(opts.Stdout, "Login Succeeded!")
		return nil
	}
	if unauthorized, ok := err.(docker.ErrUnauthorizedForCredentials); ok {
		logrus.Debugf("error logging into %q: %v", key, unauthorized)
		return ErrNewCredentialsInvalid{
			underlyingError: err,
			message:         fmt.Sprintf("logging into %q: invalid username/password", key),
		}
	}
	return fmt.Errorf("authenticating creds for %q: %w", key, err)
}

// parseCredentialsKey turns the provided argument into a valid credential key
// and computes the registry part.
func parseCredentialsKey(arg string, acceptRepositories bool) (key, registry string, err error) {
	// URL arguments are replaced with their host[:port] parts.
	key, err = replaceURLByHostPort(arg)
	if err != nil {
		return "", "", err
	}

	registry, _, _ = strings.Cut(key, "/")

	if !acceptRepositories {
		return registry, registry, nil
	}

	// Return early if the key isn't namespaced or uses an http{s} prefix.
	if registry == key {
		return key, registry, nil
	}

	// Sanity-check that the key looks reasonable (e.g. doesn't use invalid characters),
	// and does not contain a tag or digest.
	// WARNING: ref.Named() MUST NOT be used to compute key, because
	// reference.ParseNormalizedNamed() turns docker.io/vendor to docker.io/library/vendor
	// Ideally c/image should provide dedicated validation functionality.
	ref, err := reference.ParseNormalizedNamed(key)
	if err != nil {
		return "", "", fmt.Errorf("parse reference from %q: %w", key, err)
	}
	if !reference.IsNameOnly(ref) {
		return "", "", fmt.Errorf("reference %q contains tag or digest", ref.String())
	}
	refRegistry := reference.Domain(ref)
	if refRegistry != registry { // This should never happen, check just to make sure
		return "", "", fmt.Errorf("internal error: key %q registry mismatch, %q vs. %q", key, ref, refRegistry)
	}

	return key, registry, nil
}

// If the specified string starts with http{s} it is replaced with it's
// host[:port] parts; everything else is stripped. Otherwise, the string is
// returned as is.
func replaceURLByHostPort(repository string) (string, error) {
	if !strings.HasPrefix(repository, "https://") && !strings.HasPrefix(repository, "http://") {
		return repository, nil
	}
	u, err := url.Parse(repository)
	if err != nil {
		return "", fmt.Errorf("trimming http{s} prefix: %v", err)
	}
	return u.Host, nil
}

// getUserAndPass gets the username and password from STDIN if not given
// using the -u and -p flags.  If the username prompt is left empty, the
// displayed userFromAuthFile will be used instead.
func getUserAndPass(opts *LoginOptions, password, userFromAuthFile string) (user, pass string, err error) {
	username := opts.Username
	if username == "" {
		if opts.Stdin == nil {
			return "", "", errors.New("cannot prompt for username without stdin")
		}

		if userFromAuthFile != "" {
			fmt.Fprintf(opts.Stdout, "Username (%s): ", userFromAuthFile)
		} else {
			fmt.Fprint(opts.Stdout, "Username: ")
		}

		reader := bufio.NewReader(opts.Stdin)
		username, err = reader.ReadString('\n')
		if err != nil {
			return "", "", fmt.Errorf("reading username: %w", err)
		}
		// If the user just hit enter, use the displayed user from
		// the authentication file.  This allows to do a lazy
		// `$ buildah login -p $NEW_PASSWORD` without specifying the
		// user.
		if strings.TrimSpace(username) == "" {
			username = userFromAuthFile
		}
	}
	if password == "" {
		fmt.Fprint(opts.Stdout, "Password: ")
		pass, err := passwd.Read(int(os.Stdin.Fd()))
		if err != nil {
			return "", "", fmt.Errorf("reading password: %w", err)
		}
		password = string(pass)
		fmt.Fprintln(opts.Stdout)
	}
	return strings.TrimSpace(username), password, err
}

// Logout implements a “log out” command with the provided opts and args.
func Logout(systemContext *types.SystemContext, opts *LogoutOptions, args []string) error {
	if err := CheckAuthFile(opts.AuthFile); err != nil {
		return err
	}
	if err := CheckAuthFile(opts.DockerCompatAuthFile); err != nil {
		return err
	}
	systemContext, err := systemContextWithOptions(systemContext, opts.AuthFile, opts.DockerCompatAuthFile, "")
	if err != nil {
		return err
	}

	if opts.All {
		if len(args) != 0 {
			return errors.New("--all takes no arguments")
		}
		if err := config.RemoveAllAuthentication(systemContext); err != nil {
			return err
		}
		fmt.Fprintln(opts.Stdout, "Removed login credentials for all registries")
		return nil
	}

	var key, registry string
	switch len(args) {
	case 0:
		if !opts.AcceptUnspecifiedRegistry {
			return errors.New("please provide a registry to log out from")
		}
		if key, err = defaultRegistryWhenUnspecified(systemContext); err != nil {
			return err
		}
		registry = key
		logrus.Debugf("registry not specified, default to the first registry %q from registries.conf", key)

	case 1:
		key, registry, err = parseCredentialsKey(args[0], opts.AcceptRepositories)
		if err != nil {
			return err
		}

	default:
		return errors.New("logout accepts only one registry to log out from")
	}

	err = config.RemoveAuthentication(systemContext, key)
	if err == nil {
		fmt.Fprintf(opts.Stdout, "Removed login credentials for %s\n", key)
		return nil
	}

	if errors.Is(err, config.ErrNotLoggedIn) {
		authConfig, err := config.GetCredentials(systemContext, key)
		if err != nil {
			return fmt.Errorf("get credentials: %w", err)
		}

		authInvalid := docker.CheckAuth(context.Background(), systemContext, authConfig.Username, authConfig.Password, registry)
		if authConfig.Username != "" && authConfig.Password != "" && authInvalid == nil {
			fmt.Printf("Not logged into %s with current tool. Existing credentials were established via docker login. Please use docker logout instead.\n", key) //nolint:forbidigo
			return nil
		}
		return fmt.Errorf("not logged into %s", key)
	}

	return fmt.Errorf("logging out of %q: %w", key, err)
}

// defaultRegistryWhenUnspecified returns first registry from search list of registry.conf
// used by login/logout when registry argument is not specified.
func defaultRegistryWhenUnspecified(systemContext *types.SystemContext) (string, error) {
	registriesFromFile, err := sysregistriesv2.UnqualifiedSearchRegistries(systemContext)
	if err != nil {
		return "", fmt.Errorf("getting registry from registry.conf, please specify a registry: %w", err)
	}
	if len(registriesFromFile) == 0 {
		return "", errors.New("no registries found in registries.conf, a registry must be provided")
	}
	return registriesFromFile[0], nil
}
