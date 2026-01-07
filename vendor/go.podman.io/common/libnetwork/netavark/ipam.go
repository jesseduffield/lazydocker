//go:build linux || freebsd

package netavark

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"go.etcd.io/bbolt"
	boltErrors "go.etcd.io/bbolt/errors"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/libnetwork/util"
)

// IPAM boltdb structure
// Each network has their own bucket with the network name as bucket key.
// Inside the network bucket there is an ID bucket which maps the container ID (key)
// to a json array of ip addresses (value).
// The network bucket also has a bucket for each subnet, the subnet is used as key.
// Inside the subnet bucket an ip is used as key and the container ID as value.

const (
	idBucket = "ids"
	// lastIP this is used as key to store the last allocated ip
	// note that this string should not be 4 or 16 byte long.
	lastIP = "lastIP"
)

var (
	idBucketKey = []byte(idBucket)
	lastIPKey   = []byte(lastIP)
)

type ipamError struct {
	msg   string
	cause error
}

func (e *ipamError) Error() string {
	msg := "IPAM error"
	if e.msg != "" {
		msg += ": " + e.msg
	}
	if e.cause != nil {
		msg += ": " + e.cause.Error()
	}
	return msg
}

func newIPAMError(cause error, msg string, args ...any) *ipamError {
	return &ipamError{
		msg:   fmt.Sprintf(msg, args...),
		cause: cause,
	}
}

// openDB will open the ipam database
// Note that the caller has to Close it.
func (n *netavarkNetwork) openDB() (*bbolt.DB, error) {
	db, err := bbolt.Open(n.ipamDBPath, 0o600, nil)
	if err != nil {
		return nil, newIPAMError(err, "failed to open database %s", n.ipamDBPath)
	}
	return db, nil
}

// allocIPs will allocate ips for the container. It will change the
// NetworkOptions in place. When static ips are given it will validate
// that these are free to use and will allocate them to the container.
func (n *netavarkNetwork) allocIPs(opts *types.NetworkOptions) error {
	db, err := n.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	err = db.Update(func(tx *bbolt.Tx) error {
		for netName, netOpts := range opts.Networks {
			network := n.networks[netName]
			if network == nil {
				return newIPAMError(nil, "could not find network %q", netName)
			}

			// check if we have to alloc ips
			if !requiresIPAMAlloc(network) {
				continue
			}

			// create/get network bucket
			netBkt, err := tx.CreateBucketIfNotExists([]byte(netName))
			if err != nil {
				return newIPAMError(err, "failed to create/get network bucket for network %s", netName)
			}

			// requestIPs is the list of ips which should be used for this container
			requestIPs := make([]net.IP, 0, len(network.Subnets))

			for i := range network.Subnets {
				subnetBkt, err := netBkt.CreateBucketIfNotExists([]byte(network.Subnets[i].Subnet.String()))
				if err != nil {
					return newIPAMError(err, "failed to create/get subnet bucket for network %s", netName)
				}

				// search for a static ip which matches the current subnet
				// in this case the user wants this one and we should not assign a free one
				var ip net.IP
				for _, staticIP := range netOpts.StaticIPs {
					if network.Subnets[i].Subnet.Contains(staticIP) {
						ip = staticIP
						break
					}
				}

				// when static ip is requested for this network
				if ip != nil {
					// convert to 4 byte ipv4 if needed
					util.NormalizeIP(&ip)
					id := subnetBkt.Get(ip)
					if id != nil {
						return newIPAMError(nil, "requested ip address %s is already allocated to container ID %s", ip.String(), string(id))
					}
				} else {
					ip, err = getFreeIPFromBucket(subnetBkt, &network.Subnets[i])
					if err != nil {
						return err
					}
					err = subnetBkt.Put(lastIPKey, ip)
					if err != nil {
						return newIPAMError(err, "failed to store last ip in database")
					}
				}

				err = subnetBkt.Put(ip, []byte(opts.ContainerID))
				if err != nil {
					return newIPAMError(err, "failed to store ip in database")
				}

				requestIPs = append(requestIPs, ip)
			}

			idsBucket, err := netBkt.CreateBucketIfNotExists(idBucketKey)
			if err != nil {
				return newIPAMError(err, "failed to create/get id bucket for network %s", netName)
			}

			ipsBytes, err := json.Marshal(requestIPs)
			if err != nil {
				return newIPAMError(err, "failed to marshal ips")
			}

			err = idsBucket.Put([]byte(opts.ContainerID), ipsBytes)
			if err != nil {
				return newIPAMError(err, "failed to store ips in database")
			}

			netOpts.StaticIPs = requestIPs
			opts.Networks[netName] = netOpts
		}
		return nil
	})
	return err
}

func getFreeIPFromBucket(bucket *bbolt.Bucket, subnet *types.Subnet) (net.IP, error) {
	var rangeStart net.IP
	var rangeEnd net.IP
	if subnet.LeaseRange != nil {
		rangeStart = subnet.LeaseRange.StartIP
		rangeEnd = subnet.LeaseRange.EndIP
	}

	if rangeStart == nil {
		// let start with the first ip in subnet
		rangeStart = util.NextIP(subnet.Subnet.IP)
	} else if util.Cmp(rangeStart, subnet.Subnet.IP) == 0 {
		// when we start on the subnet ip we need to inc by one as the subnet ip cannot be assigned
		rangeStart = util.NextIP(rangeStart)
	}

	lastIP, err := util.LastIPInSubnet(&subnet.Subnet.IPNet)
	// this error should never happen but lets check anyways to prevent panics
	if err != nil {
		return nil, fmt.Errorf("failed to get lastIP: %w", err)
	}
	if rangeEnd == nil {
		rangeEnd = lastIP
	}
	// ipv4 uses the last ip in a subnet for broadcast so we cannot use it
	if util.IsIPv4(rangeEnd) && util.Cmp(rangeEnd, lastIP) == 0 {
		rangeEnd = util.PrevIP(rangeEnd)
	}

	lastIPByte := bucket.Get(lastIPKey)
	curIP := net.IP(lastIPByte)
	if curIP == nil {
		curIP = rangeStart
	} else {
		curIP = util.NextIP(curIP)
	}

	// store the start ip to make sure we know when we looped over all available ips
	startIP := curIP

	for {
		// skip the gateway
		if subnet.Gateway != nil {
			if util.Cmp(curIP, subnet.Gateway) == 0 {
				curIP = util.NextIP(curIP)
				continue
			}
		}

		// if we are at the end we need to jump back to the start ip
		if util.Cmp(curIP, rangeEnd) > 0 {
			if util.Cmp(rangeStart, startIP) == 0 {
				return nil, newIPAMError(nil, "failed to find free IP in range: %s - %s", rangeStart.String(), rangeEnd.String())
			}
			curIP = rangeStart
			continue
		}

		// check if ip is already used by another container
		// if not return it
		if bucket.Get(curIP) == nil {
			return curIP, nil
		}

		curIP = util.NextIP(curIP)

		if util.Cmp(curIP, startIP) == 0 {
			return nil, newIPAMError(nil, "failed to find free IP in range: %s - %s", rangeStart.String(), rangeEnd.String())
		}
	}
}

// getAssignedIPs will read the ipam database and will fill in the used ips for this container.
// It will change the NetworkOptions in place.
func (n *netavarkNetwork) getAssignedIPs(opts *types.NetworkOptions) error {
	db, err := n.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	err = db.View(func(tx *bbolt.Tx) error {
		for netName, netOpts := range opts.Networks {
			network := n.networks[netName]
			if network == nil {
				return newIPAMError(nil, "could not find network %q", netName)
			}

			// check if we have to alloc ips
			if !requiresIPAMAlloc(network) {
				continue
			}
			// get network bucket
			netBkt := tx.Bucket([]byte(netName))
			if netBkt == nil {
				return newIPAMError(nil, "failed to get network bucket for network %s", netName)
			}

			idBkt := netBkt.Bucket(idBucketKey)
			if idBkt == nil {
				return newIPAMError(nil, "failed to get id bucket for network %s", netName)
			}

			ipJSON := idBkt.Get([]byte(opts.ContainerID))
			if ipJSON == nil {
				return newIPAMError(nil, "failed to get ips for container ID %s on network %s", opts.ContainerID, netName)
			}

			// assignedIPs is the list of ips which should be used for this container
			assignedIPs := make([]net.IP, 0, len(network.Subnets))

			err = json.Unmarshal(ipJSON, &assignedIPs)
			if err != nil {
				return newIPAMError(err, "failed to unmarshal ips from database")
			}

			for i := range assignedIPs {
				util.NormalizeIP(&assignedIPs[i])
			}

			netOpts.StaticIPs = assignedIPs
			opts.Networks[netName] = netOpts
		}
		return nil
	})
	return err
}

// deallocIPs will release the ips in the network options from the DB so that
// they can be reused by other containers. It expects that the network options
// are already filled with the correct ips. Use getAssignedIPs() for this.
func (n *netavarkNetwork) deallocIPs(opts *types.NetworkOptions) error {
	db, err := n.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	err = db.Update(func(tx *bbolt.Tx) error {
		for netName, netOpts := range opts.Networks {
			network := n.networks[netName]
			if network == nil {
				return newIPAMError(nil, "could not find network %q", netName)
			}

			// check if we have to alloc ips
			if !requiresIPAMAlloc(network) {
				continue
			}
			// get network bucket
			netBkt := tx.Bucket([]byte(netName))
			if netBkt == nil {
				return newIPAMError(nil, "failed to get network bucket for network %s", netName)
			}

			for _, subnet := range network.Subnets {
				subnetBkt := netBkt.Bucket([]byte(subnet.Subnet.String()))
				if subnetBkt == nil {
					return newIPAMError(nil, "failed to get subnet bucket for network %s", netName)
				}

				// search for a static ip which matches the current subnet
				// in this case the user wants this one and we should not assign a free one
				var ip net.IP
				for _, staticIP := range netOpts.StaticIPs {
					if subnet.Subnet.Contains(staticIP) {
						ip = staticIP
						break
					}
				}
				if ip == nil {
					return newIPAMError(nil, "failed to find ip for subnet %s on network %s", subnet.Subnet.String(), netName)
				}
				util.NormalizeIP(&ip)

				err = subnetBkt.Delete(ip)
				if err != nil {
					return newIPAMError(err, "failed to remove ip %s from subnet bucket for network %s", ip.String(), netName)
				}
			}

			idBkt := netBkt.Bucket(idBucketKey)
			if idBkt == nil {
				return newIPAMError(nil, "failed to get id bucket for network %s", netName)
			}

			err = idBkt.Delete([]byte(opts.ContainerID))
			if err != nil {
				return newIPAMError(err, "failed to remove allocated ips for container ID %s on network %s", opts.ContainerID, netName)
			}
		}
		return nil
	})
	return err
}

func (n *netavarkNetwork) removeNetworkIPAMBucket(network *types.Network) error {
	if !requiresIPAMAlloc(network) {
		return nil
	}
	db, err := n.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Update(func(tx *bbolt.Tx) error {
		// Ignore ErrBucketNotFound, can happen if the network never allocated any ips,
		// i.e. because no container was started.
		if err := tx.DeleteBucket([]byte(network.Name)); err != nil && !errors.Is(err, boltErrors.ErrBucketNotFound) {
			return err
		}
		return nil
	})
}

// requiresIPAMAlloc return true when we have to allocate ips for this network
// it checks the ipam driver and if subnets are set.
func requiresIPAMAlloc(network *types.Network) bool {
	// only do host allocation when driver is set to HostLocalIPAMDriver or unset
	switch network.IPAMOptions[types.Driver] {
	case "", types.HostLocalIPAMDriver:
	default:
		return false
	}

	// no subnets == no ips to assign
	return len(network.Subnets) > 0
}
