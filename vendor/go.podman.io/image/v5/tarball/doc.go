// Package tarball provides a way to generate images using one or more layer
// tarballs and an optional template configuration.
//
// An example:
//
//	package main
//
//	import (
//		"context"
//
//		cp "go.podman.io/image/v5/copy"
//		"go.podman.io/image/v5/signature"
//		"go.podman.io/image/v5/tarball"
//		"go.podman.io/image/v5/transports/alltransports"
//		"go.podman.io/image/v5/types"
//		imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
//	)
//
//	func imageFromTarball() {
//		src, err := alltransports.ParseImageName("tarball:/var/cache/mock/fedora-26-x86_64/root_cache/cache.tar.gz")
//		// - or -
//		// src, err := tarball.Transport.ParseReference("/var/cache/mock/fedora-26-x86_64/root_cache/cache.tar.gz")
//		if err != nil {
//			panic(err)
//		}
//		updater, ok := src.(tarball.ConfigUpdater)
//		if !ok {
//			panic("unexpected: a tarball reference should implement tarball.ConfigUpdater")
//		}
//		config := imgspecv1.Image{
//			Config: imgspecv1.ImageConfig{
//				Cmd: []string{"/bin/bash"},
//			},
//		}
//		annotations := make(map[string]string)
//		annotations[imgspecv1.AnnotationDescription] = "test image built from a mock root cache"
//		err = updater.ConfigUpdate(config, annotations)
//		if err != nil {
//			panic(err)
//		}
//		dest, err := alltransports.ParseImageName("docker-daemon:mock:latest")
//		if err != nil {
//			panic(err)
//		}
//
//		policy, err := signature.DefaultPolicy(nil)
//		if err != nil {
//			panic(err)
//		}
//
//		pc, err := signature.NewPolicyContext(policy)
//		if err != nil {
//			panic(err)
//		}
//		defer pc.Destroy()
//		_, err = cp.Image(context.TODO(), pc, dest, src, nil)
//		if err != nil {
//			panic(err)
//		}
//	}
package tarball
