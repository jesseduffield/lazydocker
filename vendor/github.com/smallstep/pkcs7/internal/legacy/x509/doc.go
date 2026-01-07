/*
Package legacyx509 is a copy of certain parts of Go's crypto/x509 package.
It is based on Go 1.23, and has just the parts copied over required for
parsing X509 certificates.

The primary reason this copy exists is to keep support for parsing PKCS7
messages containing Simple Certificate Enrolment Protocol (SCEP) requests
from Windows devices. Go 1.23 made a change marking certificates with a
critical authority key identifier as invalid, which is mandated by RFC 5280,
but apparently Windows marks those specific certificates as such, resulting
in those SCEP requests failing from being parsed correctly.
*/

package legacyx509
