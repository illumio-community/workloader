package vsexport

import (
	"fmt"
	"strconv"

	ia "github.com/brian1917/illumioapi/v2"
)

// ProtoToString converts a protocol number to the string used in the CSV.
func ProtoToString(proto int) string {
	switch proto {
	case 6:
		return "tcp"
	case 17:
		return "udp"
	case 1:
		return "icmp"
	case 58:
		return "icmpv6"
	}
	return strconv.Itoa(proto)
}

// ServicePortToString converts a service port to "port proto", "port-toport proto", or "proto" (no port).
func ServicePortToString(sp ia.ServicePort) string {
	proto := ProtoToString(sp.Protocol)
	if sp.Port == nil || (*sp.Port == 0 && sp.ToPort == 0) {
		return proto
	}
	if sp.ToPort != 0 {
		return fmt.Sprintf("%d-%d %s", *sp.Port, sp.ToPort, proto)
	}
	return fmt.Sprintf("%d %s", *sp.Port, proto)
}

// ServicePortsToString converts the service ports of a virtual service into CSV entries.
func ServicePortsToString(sps *[]ia.ServicePort) []string {
	entries := []string{}
	for _, sp := range ia.PtrToVal(sps) {
		entries = append(entries, ServicePortToString(sp))
	}
	return entries
}

// ServiceAddressToString converts a service address to "ip", "fqdn", with an optional "#description" suffix.
func ServiceAddressToString(sa ia.ServiceAddresses) string {
	s := sa.IP
	if s == "" {
		s = sa.Fqdn
	}
	if sa.Description != "" {
		s = fmt.Sprintf("%s#%s", s, sa.Description)
	}
	return s
}

// ServiceAddressesToString converts the service addresses of a virtual service into CSV entries.
func ServiceAddressesToString(sas *[]ia.ServiceAddresses) []string {
	entries := []string{}
	for _, sa := range ia.PtrToVal(sas) {
		entries = append(entries, ServiceAddressToString(sa))
	}
	return entries
}
