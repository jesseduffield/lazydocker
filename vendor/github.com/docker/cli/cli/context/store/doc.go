// Package store provides a generic way to store credentials to connect to virtually any kind of remote system.
// The term `context` comes from the similar feature in Kubernetes kubectl config files.
//
// Conceptually, a context is a set of metadata and TLS data, that can be used to connect to various endpoints
// of a remote system. TLS data and metadata are stored separately, so that in the future, we will be able to store sensitive
// information in a more secure way, depending on the os we are running on (e.g.: on Windows we could use the user Certificate Store, on Mac OS the user Keychain...).
//
// Current implementation is purely file based with the following structure:
// ${CONTEXT_ROOT}
//   - meta/
//     - <context id>/meta.json: contains context medata (key/value pairs) as well as a list of endpoints (themselves containing key/value pair metadata)
//   - tls/
//     - <context id>/endpoint1/: directory containing TLS data for the endpoint1 in the corresponding context
//
// The context store itself has absolutely no knowledge about what a docker or a kubernetes endpoint should contain in term of metadata or TLS config.
// Client code is responsible for generating and parsing endpoint metadata and TLS files.
// The multi-endpoints approach of this package allows to combine many different endpoints in the same "context" (e.g., the Docker CLI
// is able for a single context to define both a docker endpoint and a Kubernetes endpoint for the same cluster, and also specify which
// orchestrator to use by default when deploying a compose stack on this cluster).
//
// Context IDs are actually SHA256 hashes of the context name, and are there only to avoid dealing with special characters in context names.
package store
