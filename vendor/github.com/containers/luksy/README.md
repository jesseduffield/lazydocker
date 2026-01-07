luksy: offline encryption/decryption using LUKS formats [![Cirrus CI Status](https://img.shields.io/cirrus/github/containers/luksy/main)](https://cirrus-ci.com/github/containers/luksy/main)
-
luksy implements encryption and decryption using LUKSv1 and LUKSv2 formats.
Think of it as a clunkier cousin of gzip/bzip2/xz that doesn't actually produce
smaller output than input, but it encrypts, and that's nice.

* The main goal is to be able to encrypt/decrypt when we don't have access to
  the Linux device mapper.  Duplicating functions of cryptsetup that it can
  perform without accessing the Linux device mapper is not a priority.
* If you can use cryptsetup instead, use cryptsetup instead.
