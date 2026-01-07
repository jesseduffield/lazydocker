`storage` is a Go library which aims to provide methods for storing filesystem
layers, container images, and containers.  A `containers-storage` CLI wrapper
is also included for manual and scripting use.

To build the CLI wrapper, use 'make binary'.

Operations which use VMs expect to launch them using 'vagrant', defaulting to
using its 'libvirt' provider.  The boxes used are also available for the
'virtualbox' provider, and can be selected by setting $VAGRANT_PROVIDER to
'virtualbox' before kicking off the build.

The library manages three types of items: layers, images, and containers.

A *layer* is a copy-on-write filesystem which is notionally stored as a set of
changes relative to its *parent* layer, if it has one.  A given layer can only
have one parent, but any layer can be the parent of multiple layers.  Layers
which are parents of other layers should be treated as read-only.

An *image* is a reference to a particular layer (its _top_ layer), along with
other information which the library can manage for the convenience of its
caller.  This information typically includes configuration templates for
running a binary contained within the image's layers, and may include
cryptographic signatures.  Multiple images can reference the same layer, as the
differences between two images may not be in their layer contents.

A *container* is a read-write layer which is a child of an image's top layer,
along with information which the library can manage for the convenience of its
caller.  This information typically includes configuration information for
running the specific container.  Multiple containers can be derived from a
single image.

Layers, images, and containers are represented primarily by 32 character
hexadecimal IDs, but items of each kind can also have one or more arbitrary
names attached to them, which the library will automatically resolve to IDs
when they are passed in to API calls which expect IDs.

The library can store what it calls *metadata* for each of these types of
items.  This is expected to be a small piece of data, since it is cached in
memory and stored along with the library's own bookkeeping information.

Additionally, the library can store one or more of what it calls *big data* for
images and containers.  This is a named chunk of larger data, which is only in
memory when it is being read from or being written to its own disk file.

**[Contributing](../CONTRIBUTING.md)**
Information about contributing to this project.
