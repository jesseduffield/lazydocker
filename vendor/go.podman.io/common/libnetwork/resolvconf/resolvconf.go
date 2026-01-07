// Package resolvconf provides utility code to query and update DNS configuration in /etc/resolv.conf.
// Originally from github.com/docker/libnetwork/resolvconf but heavily modified to better work with podman.
package resolvconf

import (
	"bytes"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/regexp"
)

const (
	// DefaultResolvConf points to the default file used for dns configuration on a linux machine.
	DefaultResolvConf = "/etc/resolv.conf"
)

var (
	// Note: the default IPv4 & IPv6 resolvers are set to Google's Public DNS.
	defaultIPv4Dns = []string{"nameserver 8.8.8.8", "nameserver 8.8.4.4"}
	defaultIPv6Dns = []string{"nameserver 2001:4860:4860::8888", "nameserver 2001:4860:4860::8844"}
	ipv4NumBlock   = `(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)`
	ipv4Address    = `(` + ipv4NumBlock + `\.){3}` + ipv4NumBlock
	// This is not an IPv6 address verifier as it will accept a super-set of IPv6, and also
	// will *not match* IPv4-Embedded IPv6 Addresses (RFC6052), but that and other variants
	// -- e.g. other link-local types -- either won't work in containers or are unnecessary.
	// For readability and sufficiency for Docker purposes this seemed more reasonable than a
	// 1000+ character regexp with exact and complete IPv6 validation.
	ipv6Address = `([0-9A-Fa-f]{0,4}:){2,7}([0-9A-Fa-f]{0,4})(%\w+)?`

	// ipLocalhost is a regex pattern for IPv4 or IPv6 loopback range.
	ipLocalhost = `((127\.([0-9]{1,3}\.){2}[0-9]{1,3})|(::1)$)`

	localhostNSRegexp     = regexp.Delayed(`(?m)^nameserver\s+` + ipLocalhost + `\s*\n*`)
	nsIPv6Regexp          = regexp.Delayed(`(?m)^nameserver\s+` + ipv6Address + `\s*\n*`)
	nsIPv6LinkLocalRegexp = regexp.Delayed(`(?m)^nameserver\s+` + ipv6Address + `%.*\s*\n*`)
	nsRegexp              = regexp.Delayed(`^\s*nameserver\s*((` + ipv4Address + `)|(` + ipv6Address + `))\s*$`)
	searchRegexp          = regexp.Delayed(`^\s*search\s*(([^\s]+\s*)*)$`)
	optionsRegexp         = regexp.Delayed(`^\s*options\s*(([^\s]+\s*)*)$`)
)

// filterResolvDNS cleans up the config in resolvConf.  It has two main jobs:
//  1. If a netns is enabled, it looks for localhost (127.*|::1) entries in the provided
//     resolv.conf, removing local nameserver entries, and, if the resulting
//     cleaned config has no defined nameservers left, adds default DNS entries
//  2. Given the caller provides the enable/disable state of IPv6, the filter
//     code will remove all IPv6 nameservers if it is not enabled for containers
func filterResolvDNS(resolvConf []byte, ipv6Enabled bool, netnsEnabled bool) []byte {
	// If we're using the host netns, we have nothing to do besides hash the file.
	if !netnsEnabled {
		return resolvConf
	}
	cleanedResolvConf := localhostNSRegexp.ReplaceAll(resolvConf, []byte{})
	// if IPv6 is not enabled, also clean out any IPv6 address nameserver
	if !ipv6Enabled {
		cleanedResolvConf = nsIPv6Regexp.ReplaceAll(cleanedResolvConf, []byte{})
	} else {
		// If ipv6 is we still must remove any ipv6 link-local addresses as
		// the zone will never match the interface name or index in the container.
		cleanedResolvConf = nsIPv6LinkLocalRegexp.ReplaceAll(cleanedResolvConf, []byte{})
	}
	// if the resulting resolvConf has no more nameservers defined, add appropriate
	// default DNS servers for IPv4 and (optionally) IPv6
	if len(getNameservers(cleanedResolvConf)) == 0 {
		logrus.Infof("No non-localhost DNS nameservers are left in resolv.conf. Using default external servers: %v", defaultIPv4Dns)
		dns := defaultIPv4Dns
		if ipv6Enabled {
			logrus.Infof("IPv6 enabled; Adding default IPv6 external servers: %v", defaultIPv6Dns)
			dns = append(dns, defaultIPv6Dns...)
		}
		cleanedResolvConf = append(cleanedResolvConf, []byte("\n"+strings.Join(dns, "\n"))...)
	}
	return cleanedResolvConf
}

// getLines parses input into lines and strips away comments.
func getLines(input []byte) [][]byte {
	var output [][]byte
	for currentLine := range bytes.SplitSeq(input, []byte("\n")) {
		commentIndex := bytes.Index(currentLine, []byte("#"))
		if commentIndex == -1 {
			output = append(output, currentLine)
		} else {
			output = append(output, currentLine[:commentIndex])
		}
	}
	return output
}

// getNameservers returns nameservers (if any) listed in /etc/resolv.conf.
func getNameservers(resolvConf []byte) []string {
	nameservers := []string{}
	for _, line := range getLines(resolvConf) {
		ns := nsRegexp.FindSubmatch(line)
		if len(ns) > 0 {
			nameservers = append(nameservers, string(ns[1]))
		}
	}
	return nameservers
}

// getSearchDomains returns search domains (if any) listed in /etc/resolv.conf
// If more than one search line is encountered, only the contents of the last
// one is returned.
func getSearchDomains(resolvConf []byte) []string {
	domains := []string{}
	for _, line := range getLines(resolvConf) {
		match := searchRegexp.FindSubmatch(line)
		if match == nil {
			continue
		}
		domains = strings.Fields(string(match[1]))
	}
	return domains
}

// getOptions returns options (if any) listed in /etc/resolv.conf
// If more than one options line is encountered, only the contents of the last
// one is returned.
func getOptions(resolvConf []byte) []string {
	options := []string{}
	for _, line := range getLines(resolvConf) {
		match := optionsRegexp.FindSubmatch(line)
		if match == nil {
			continue
		}
		options = strings.Fields(string(match[1]))
	}
	return options
}

// build writes a configuration file to path containing a "nameserver" entry
// for every element in dns, a "search" entry for every element in
// dnsSearch, and an "options" entry for every element in dnsOptions.
func build(path string, dns, dnsSearch, dnsOptions []string) error {
	content := new(bytes.Buffer)
	if len(dnsSearch) > 0 {
		if searchString := strings.Join(dnsSearch, " "); strings.Trim(searchString, " ") != "." {
			if _, err := content.WriteString("search " + searchString + "\n"); err != nil {
				return err
			}
		}
	}
	for _, dns := range dns {
		if _, err := content.WriteString("nameserver " + dns + "\n"); err != nil {
			return err
		}
	}
	if len(dnsOptions) > 0 {
		if optsString := strings.Join(dnsOptions, " "); strings.Trim(optsString, " ") != "" {
			if _, err := content.WriteString("options " + optsString + "\n"); err != nil {
				return err
			}
		}
	}

	return os.WriteFile(path, content.Bytes(), 0o644)
}
