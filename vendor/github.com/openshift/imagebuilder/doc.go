// Package builder uses code from github.com/docker/docker/builder/* to implement
// a Docker builder that does not create individual layers, but instead creates a
// single layer.
//
// TODO: full windows support
package imagebuilder
