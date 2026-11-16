package vsimport

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	ia "github.com/brian1917/illumioapi/v2"
	"github.com/brian1917/workloader/cmd/vsexport"
)

// splitEntries splits a semicolon-separated cell and drops blank entries.
func splitEntries(cell string) []string {
	entries := []string{}
	for _, e := range strings.Split(cell, ";") {
		if e = strings.TrimSpace(e); e != "" {
			entries = append(entries, e)
		}
	}
	return entries
}

// normalize sorts entries and joins them so lists can be compared regardless of order.
func normalize(entries []string) string {
	s := append([]string{}, entries...)
	sort.Strings(s)
	return strings.Join(s, ";")
}

func parseProto(s string) (int, error) {
	switch strings.ToLower(s) {
	case "tcp":
		return 6, nil
	case "udp":
		return 17, nil
	case "icmp":
		return 1, nil
	case "icmpv6":
		return 58, nil
	}
	proto, err := strconv.Atoi(s)
	if err != nil || proto < 0 || proto > 255 {
		return 0, fmt.Errorf("invalid protocol %q", s)
	}
	return proto, nil
}

func parsePort(s string) (int, error) {
	port, err := strconv.Atoi(s)
	if err != nil || port < 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	return port, nil
}

// parseServicePort parses "port proto", "port-toport proto", or "proto".
func parseServicePort(entry string) (ia.ServicePort, error) {
	sp := ia.ServicePort{}
	fields := strings.Fields(strings.Replace(entry, " - ", "-", -1))
	var err error
	switch len(fields) {
	case 1:
		sp.Protocol, err = parseProto(fields[0])
		return sp, err
	case 2:
		if sp.Protocol, err = parseProto(fields[1]); err != nil {
			return sp, err
		}
		ports := strings.Split(fields[0], "-")
		if len(ports) > 2 {
			return sp, fmt.Errorf("invalid port range %q", fields[0])
		}
		port, err := parsePort(ports[0])
		if err != nil {
			return sp, err
		}
		sp.Port = &port
		if len(ports) == 2 {
			if sp.ToPort, err = parsePort(ports[1]); err != nil {
				return sp, err
			}
			if sp.ToPort <= port {
				return sp, fmt.Errorf("invalid port range %q - end port must be greater than start port", fields[0])
			}
		}
		return sp, nil
	}
	return sp, fmt.Errorf("invalid service port %q - format must be \"port proto\", \"port-toport proto\", or \"proto\"", entry)
}

// parseServicePorts parses a semicolon-separated service ports cell.
func parseServicePorts(cell string) ([]ia.ServicePort, error) {
	sps := []ia.ServicePort{}
	for _, e := range splitEntries(cell) {
		sp, err := parseServicePort(e)
		if err != nil {
			return nil, err
		}
		sps = append(sps, sp)
	}
	return sps, nil
}

// parseServiceAddresses parses a semicolon-separated service addresses cell.
// Entries that are valid IP addresses are IP service addresses. All other entries are FQDNs.
// Existing addresses are used to carry over the network of an IP address.
func parseServiceAddresses(cell string, existing *[]ia.ServiceAddresses) ([]ia.ServiceAddresses, error) {
	existingNetworks := make(map[string]*ia.Network)
	for _, sa := range ia.PtrToVal(existing) {
		if sa.IP != "" && sa.Network != nil {
			existingNetworks[sa.IP] = &ia.Network{Href: sa.Network.Href}
		}
	}

	sas := []ia.ServiceAddresses{}
	for _, e := range splitEntries(cell) {
		addr, desc, _ := strings.Cut(e, "#")
		addr = strings.TrimSpace(addr)
		desc = strings.TrimSpace(desc)
		if addr == "" {
			return nil, fmt.Errorf("invalid service address %q", e)
		}
		if strings.Contains(addr, "/") {
			return nil, fmt.Errorf("invalid service address %q - cidr ranges are not supported", e)
		}
		sa := ia.ServiceAddresses{Description: desc}
		if net.ParseIP(addr) != nil {
			sa.IP = addr
			sa.Network = existingNetworks[addr]
		} else {
			sa.Fqdn = addr
		}
		sas = append(sas, sa)
	}
	return sas, nil
}

// parseIPOverrides parses a semicolon-separated ip overrides cell.
func parseIPOverrides(cell string) ([]string, error) {
	ips := splitEntries(cell)
	for _, ip := range ips {
		if net.ParseIP(ip) == nil {
			return nil, fmt.Errorf("invalid ip override %q", ip)
		}
	}
	return ips, nil
}

func servicePortsKey(sps []ia.ServicePort) string {
	return normalize(vsexport.ServicePortsToString(&sps))
}

func serviceAddressesKey(sas []ia.ServiceAddresses) string {
	return normalize(vsexport.ServiceAddressesToString(&sas))
}
