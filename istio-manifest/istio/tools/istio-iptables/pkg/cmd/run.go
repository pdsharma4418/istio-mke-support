// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package cmd

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"istio.io/istio/tools/istio-iptables/pkg/builder"
	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
	"istio.io/pkg/log"
)

type Ops int

const (
	// AppendOps performs append operations of rules
	AppendOps Ops = iota
	// DeleteOps performs delete operations of rules
	DeleteOps

	// In TPROXY mode, mark the packet from envoy outbound to app by podIP,
	// this is to prevent it being intercepted to envoy inbound listener.
	outboundMark = "1338"
)

var opsToString = map[Ops]string{
	AppendOps: "-A",
	DeleteOps: "-D",
}

type IptablesConfigurator struct {
	iptables *builder.IptablesBuilder
	// TODO(abhide): Fix dep.Dependencies with better interface
	ext dep.Dependencies
	cfg *config.Config
}

func NewIptablesConfigurator(cfg *config.Config, ext dep.Dependencies) *IptablesConfigurator {
	return &IptablesConfigurator{
		iptables: builder.NewIptablesBuilder(cfg),
		ext:      ext,
		cfg:      cfg,
	}
}

type NetworkRange struct {
	IsWildcard    bool
	IPNets        []*net.IPNet
	HasLoopBackIP bool
}

func filterEmpty(strs []string) []string {
	filtered := make([]string, 0, len(strs))
	for _, s := range strs {
		if s == "" {
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	return filterEmpty(strings.Split(s, ","))
}

func (iptConfigurator *IptablesConfigurator) separateV4V6(cidrList string) (NetworkRange, NetworkRange, error) {
	if cidrList == "*" {
		return NetworkRange{IsWildcard: true}, NetworkRange{IsWildcard: true}, nil
	}
	ipv6Ranges := NetworkRange{}
	ipv4Ranges := NetworkRange{}
	for _, ipRange := range split(cidrList) {
		ip, ipNet, err := net.ParseCIDR(ipRange)
		if err != nil {
			_, err = fmt.Fprintf(os.Stderr, "Ignoring error for bug compatibility with istio-iptables: %s\n", err.Error())
			if err != nil {
				return ipv4Ranges, ipv6Ranges, err
			}
			continue
		}
		if ip.To4() != nil {
			ipv4Ranges.IPNets = append(ipv4Ranges.IPNets, ipNet)
			if ip.IsLoopback() {
				ipv4Ranges.HasLoopBackIP = true
			}
		} else {
			ipv6Ranges.IPNets = append(ipv6Ranges.IPNets, ipNet)
			if ip.IsLoopback() {
				ipv6Ranges.HasLoopBackIP = true
			}
		}
	}
	return ipv4Ranges, ipv6Ranges, nil
}

func (iptConfigurator *IptablesConfigurator) logConfig() {
	// Dump out our environment for debugging purposes.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("ENVOY_PORT=%s\n", os.Getenv("ENVOY_PORT")))
	b.WriteString(fmt.Sprintf("INBOUND_CAPTURE_PORT=%s\n", os.Getenv("INBOUND_CAPTURE_PORT")))
	b.WriteString(fmt.Sprintf("ISTIO_INBOUND_INTERCEPTION_MODE=%s\n", os.Getenv("ISTIO_INBOUND_INTERCEPTION_MODE")))
	b.WriteString(fmt.Sprintf("ISTIO_INBOUND_TPROXY_ROUTE_TABLE=%s\n", os.Getenv("ISTIO_INBOUND_TPROXY_ROUTE_TABLE")))
	b.WriteString(fmt.Sprintf("ISTIO_INBOUND_PORTS=%s\n", os.Getenv("ISTIO_INBOUND_PORTS")))
	b.WriteString(fmt.Sprintf("ISTIO_OUTBOUND_PORTS=%s\n", os.Getenv("ISTIO_OUTBOUND_PORTS")))
	b.WriteString(fmt.Sprintf("ISTIO_LOCAL_EXCLUDE_PORTS=%s\n", os.Getenv("ISTIO_LOCAL_EXCLUDE_PORTS")))
	b.WriteString(fmt.Sprintf("ISTIO_EXCLUDE_INTERFACES=%s\n", os.Getenv("ISTIO_EXCLUDE_INTERFACES")))
	b.WriteString(fmt.Sprintf("ISTIO_SERVICE_CIDR=%s\n", os.Getenv("ISTIO_SERVICE_CIDR")))
	b.WriteString(fmt.Sprintf("ISTIO_SERVICE_EXCLUDE_CIDR=%s\n", os.Getenv("ISTIO_SERVICE_EXCLUDE_CIDR")))
	b.WriteString(fmt.Sprintf("ISTIO_META_DNS_CAPTURE=%s", os.Getenv("ISTIO_META_DNS_CAPTURE")))
	log.Infof("Istio iptables environment:\n%s", b.String())
	iptConfigurator.cfg.Print()
}

func (iptConfigurator *IptablesConfigurator) handleInboundPortsInclude() {
	// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
	// to the local service. If not set, no inbound port will be intercepted by istio iptablesOrFail.
	var table string
	if iptConfigurator.cfg.InboundPortsInclude != "" {
		if iptConfigurator.cfg.InboundInterceptionMode == constants.TPROXY {
			// When using TPROXY, create a new chain for routing all inbound traffic to
			// Envoy. Any packet entering this chain gets marked with the ${INBOUND_TPROXY_MARK} mark,
			// so that they get routed to the loopback interface in order to get redirected to Envoy.
			// In the ISTIOINBOUND chain, '-j ISTIODIVERT' reroutes to the loopback
			// interface.
			// Mark all inbound packets.
			iptConfigurator.iptables.AppendRule(constants.ISTIODIVERT, constants.MANGLE, "-j", constants.MARK, "--set-mark",
				iptConfigurator.cfg.InboundTProxyMark)
			iptConfigurator.iptables.AppendRule(constants.ISTIODIVERT, constants.MANGLE, "-j", constants.ACCEPT)
			// Route all packets marked in chain ISTIODIVERT using routing table ${INBOUND_TPROXY_ROUTE_TABLE}.
			// TODO: (abhide): Move this out of this method
			iptConfigurator.ext.RunOrFail(
				constants.IP, "-f", "inet", "rule", "add", "fwmark", iptConfigurator.cfg.InboundTProxyMark, "lookup",
				iptConfigurator.cfg.InboundTProxyRouteTable)
			// In routing table ${INBOUND_TPROXY_ROUTE_TABLE}, create a single default rule to route all traffic to
			// the loopback interface.
			// TODO: (abhide): Move this out of this method
			err := iptConfigurator.ext.Run(constants.IP, "-f", "inet", "route", "add", "local", "default", "dev", "lo", "table",
				iptConfigurator.cfg.InboundTProxyRouteTable)
			if err != nil {
				// TODO: (abhide): Move this out of this method
				iptConfigurator.ext.RunOrFail(constants.IP, "route", "show", "table", "all")
			}

			// Create a new chain for redirecting inbound traffic to the common Envoy
			// port.
			// In the ISTIOINBOUND chain, '-j RETURN' bypasses Envoy and
			// '-j ISTIOTPROXY' redirects to Envoy.
			iptConfigurator.iptables.AppendVersionedRule("127.0.0.1/32", "::1/128", constants.ISTIOTPROXY, constants.MANGLE, "!", "-d", constants.IPVersionSpecific,
				"-p", constants.TCP, "-j", constants.TPROXY,
				"--tproxy-mark", iptConfigurator.cfg.InboundTProxyMark+"/0xffffffff", "--on-port", iptConfigurator.cfg.InboundCapturePort)
			table = constants.MANGLE
		} else {
			table = constants.NAT
		}
		iptConfigurator.iptables.AppendRule(constants.PREROUTING, table, "-p", constants.TCP, "-j", constants.ISTIOINBOUND)

		if iptConfigurator.cfg.InboundPortsInclude == "*" {
			// Makes sure SSH is not redirected
			iptConfigurator.iptables.AppendRule(constants.ISTIOINBOUND, table, "-p", constants.TCP, "--dport", "22", "-j", constants.RETURN)
			// Apply any user-specified port exclusions.
			if iptConfigurator.cfg.InboundPortsExclude != "" {
				for _, port := range split(iptConfigurator.cfg.InboundPortsExclude) {
					iptConfigurator.iptables.AppendRule(constants.ISTIOINBOUND, table, "-p", constants.TCP, "--dport", port, "-j", constants.RETURN)
				}
			}
			// Redirect remaining inbound traffic to Envoy.
			if iptConfigurator.cfg.InboundInterceptionMode == constants.TPROXY {
				// If an inbound packet belongs to an established socket, route it to the
				// loopback interface.
				iptConfigurator.iptables.AppendRule(constants.ISTIOINBOUND, constants.MANGLE, "-p", constants.TCP,
					"-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", constants.ISTIODIVERT)
				// Otherwise, it's a new connection. Redirect it using TPROXY.
				iptConfigurator.iptables.AppendRule(constants.ISTIOINBOUND, constants.MANGLE, "-p", constants.TCP, "-j", constants.ISTIOTPROXY)
			} else {
				iptConfigurator.iptables.AppendRule(constants.ISTIOINBOUND, constants.NAT, "-p", constants.TCP, "-j", constants.ISTIOINREDIRECT)
			}
		} else {
			// User has specified a non-empty list of ports to be redirected to Envoy.
			for _, port := range split(iptConfigurator.cfg.InboundPortsInclude) {
				if iptConfigurator.cfg.InboundInterceptionMode == constants.TPROXY {
					iptConfigurator.iptables.AppendRule(constants.ISTIOINBOUND, constants.MANGLE, "-p", constants.TCP,
						"--dport", port, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", constants.ISTIODIVERT)
					iptConfigurator.iptables.AppendRule(
						constants.ISTIOINBOUND, constants.MANGLE, "-p", constants.TCP, "--dport", port, "-j", constants.ISTIOTPROXY)
				} else {
					iptConfigurator.iptables.AppendRule(
						constants.ISTIOINBOUND, constants.NAT, "-p", constants.TCP, "--dport", port, "-j", constants.ISTIOINREDIRECT)
				}
			}
		}
	}
}

func (iptConfigurator *IptablesConfigurator) handleOutboundIncludeRules(
	rangeInclude NetworkRange,
	appendRule func(chain string, table string, params ...string) *builder.IptablesBuilder,
	insert func(chain string, table string, position int, params ...string) *builder.IptablesBuilder) {
	// Apply outbound IP inclusions.
	if rangeInclude.IsWildcard {
		// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
		appendRule(constants.ISTIOOUTPUT, constants.NAT, "-j", constants.ISTIOREDIRECT)
		for _, internalInterface := range split(iptConfigurator.cfg.KubevirtInterfaces) {
			insert(
				constants.PREROUTING, constants.NAT, 1, "-i", internalInterface, "-j", constants.ISTIOREDIRECT)
		}
	} else if len(rangeInclude.IPNets) > 0 {
		// User has specified a non-empty list of cidrs to be redirected to Envoy.
		for _, cidr := range rangeInclude.IPNets {
			for _, internalInterface := range split(iptConfigurator.cfg.KubevirtInterfaces) {
				insert(constants.PREROUTING, constants.NAT, 1, "-i", internalInterface,
					"-d", cidr.String(), "-j", constants.ISTIOREDIRECT)
			}
			appendRule(
				constants.ISTIOOUTPUT, constants.NAT, "-d", cidr.String(), "-j", constants.ISTIOREDIRECT)
		}
		// All other traffic is not redirected.
		appendRule(constants.ISTIOOUTPUT, constants.NAT, "-j", constants.RETURN)
	}
}

func (iptConfigurator *IptablesConfigurator) shortCircuitKubeInternalInterface() {
	for _, internalInterface := range split(iptConfigurator.cfg.KubevirtInterfaces) {
		iptConfigurator.iptables.InsertRule(constants.PREROUTING, constants.NAT, 1, "-i", internalInterface, "-j", constants.RETURN)
	}
}

func (iptConfigurator *IptablesConfigurator) shortCircuitExcludeInterfaces() {
	for _, excludeInterface := range split(iptConfigurator.cfg.ExcludeInterfaces) {
		iptConfigurator.iptables.AppendRule(
			constants.PREROUTING, constants.NAT, "-i", excludeInterface, "-j", constants.RETURN)
		iptConfigurator.iptables.AppendRule(constants.OUTPUT, constants.NAT, "-o", excludeInterface, "-j", constants.RETURN)
	}
	if iptConfigurator.cfg.InboundInterceptionMode == constants.TPROXY {
		for _, excludeInterface := range split(iptConfigurator.cfg.ExcludeInterfaces) {

			iptConfigurator.iptables.AppendRule(
				constants.PREROUTING, constants.MANGLE, "-i", excludeInterface, "-j", constants.RETURN)
			iptConfigurator.iptables.AppendRule(constants.OUTPUT, constants.MANGLE, "-o", excludeInterface, "-j", constants.RETURN)
		}
	}
}

func SplitV4V6(ips []string) (ipv4 []string, ipv6 []string) {
	for _, i := range ips {
		parsed := net.ParseIP(i)
		if parsed.To4() != nil {
			ipv4 = append(ipv4, i)
		} else {
			ipv6 = append(ipv6, i)
		}
	}
	return
}

func (iptConfigurator *IptablesConfigurator) run() {
	defer func() {
		// Best effort since we don't know if the commands exist
		_ = iptConfigurator.ext.Run(constants.IPTABLESSAVE)
		if iptConfigurator.cfg.EnableInboundIPv6 {
			_ = iptConfigurator.ext.Run(constants.IP6TABLESSAVE)
		}
	}()

	// Since OUTBOUND_IP_RANGES_EXCLUDE could carry ipv4 and ipv6 ranges
	// need to split them in different arrays one for ipv4 and one for ipv6
	// in order to not to fail
	ipv4RangesExclude, ipv6RangesExclude, err := iptConfigurator.separateV4V6(iptConfigurator.cfg.OutboundIPRangesExclude)
	if err != nil {
		panic(err)
	}
	if ipv4RangesExclude.IsWildcard {
		panic("Invalid value for OUTBOUND_IP_RANGES_EXCLUDE")
	}
	// FixMe: Do we need similar check for ipv6RangesExclude as well ??

	ipv4RangesInclude, ipv6RangesInclude, err := iptConfigurator.separateV4V6(iptConfigurator.cfg.OutboundIPRangesInclude)
	if err != nil {
		panic(err)
	}

	redirectDNS := iptConfigurator.cfg.RedirectDNS
	iptConfigurator.logConfig()

	if iptConfigurator.cfg.EnableInboundIPv6 {
		// TODO: (abhide): Move this out of this method
		iptConfigurator.ext.RunOrFail(constants.IP, "-6", "addr", "add", "::6/128", "dev", "lo")
	}

	iptConfigurator.shortCircuitExcludeInterfaces()

	// Do not capture internal interface.
	iptConfigurator.shortCircuitKubeInternalInterface()

	// Create a new chain for to hit tunnel port directly. Envoy will be listening on port acting as VPN tunnel.
	iptConfigurator.iptables.AppendRule(constants.ISTIOINBOUND, constants.NAT, "-p", constants.TCP, "--dport",
		iptConfigurator.cfg.InboundTunnelPort, "-j", constants.RETURN)

	// Create a new chain for redirecting outbound traffic to the common Envoy port.
	// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIOREDIRECT'
	// redirects to Envoy.
	iptConfigurator.iptables.AppendRule(
		constants.ISTIOREDIRECT, constants.NAT, "-p", constants.TCP, "-j", constants.REDIRECT, "--to-ports", iptConfigurator.cfg.ProxyPort)

	// Use this chain also for redirecting inbound traffic to the common Envoy port
	// when not using TPROXY.

	iptConfigurator.iptables.AppendRule(constants.ISTIOINREDIRECT, constants.NAT, "-p", constants.TCP, "-j", constants.REDIRECT,
		"--to-ports", iptConfigurator.cfg.InboundCapturePort)

	iptConfigurator.handleInboundPortsInclude()

	// TODO: change the default behavior to not intercept any output - user may use http_proxy or another
	// iptablesOrFail wrapper (like ufw). Current default is similar with 0.1
	// Jump to the ISTIOOUTPUT chain from OUTPUT chain for all tcp traffic, and UDP dns (if enabled)
	iptConfigurator.iptables.AppendRule(constants.OUTPUT, constants.NAT, "-p", constants.TCP, "-j", constants.ISTIOOUTPUT)
	// Apply port based exclusions. Must be applied before connections back to self are redirected.
	if iptConfigurator.cfg.OutboundPortsExclude != "" {
		for _, port := range split(iptConfigurator.cfg.OutboundPortsExclude) {
			iptConfigurator.iptables.AppendRule(constants.ISTIOOUTPUT, constants.NAT, "-p", constants.TCP, "--dport", port, "-j", constants.RETURN)
		}
	}

	// 127.0.0.6/::7 is bind connect from inbound passthrough cluster
	iptConfigurator.iptables.AppendVersionedRule("127.0.0.6/32", "::6/128", constants.ISTIOOUTPUT, constants.NAT,
		"-o", "lo", "-s", constants.IPVersionSpecific, "-j", constants.RETURN)

	for _, uid := range split(iptConfigurator.cfg.ProxyUID) {
		// Redirect app calls back to itself via Envoy when using the service VIP
		// e.g. appN => Envoy (client) => Envoy (server) => appN.
		// nolint: lll
		if redirectDNS {
			// When DNS is enabled, we skip this for port 53. This ensures we do not have:
			// app => istio-agent => Envoy inbound => dns server
			// Instead, we just have:
			// app => istio-agent => dns server
			iptConfigurator.iptables.AppendVersionedRule("127.0.0.1/32", "::1/128", constants.ISTIOOUTPUT, constants.NAT,
				"-o", "lo", "!", "-d", constants.IPVersionSpecific,
				"-p", "tcp", "!", "--dport", "53",
				"-m", "owner", "--uid-owner", uid, "-j", constants.ISTIOINREDIRECT)
		} else {
			iptConfigurator.iptables.AppendVersionedRule("127.0.0.1/32", "::1/128", constants.ISTIOOUTPUT, constants.NAT,
				"-o", "lo", "!", "-d", constants.IPVersionSpecific,
				"-m", "owner", "--uid-owner", uid, "-j", constants.ISTIOINREDIRECT)
		}

		// Do not redirect app calls to back itself via Envoy when using the endpoint address
		// e.g. appN => appN by lo
		// If loopback explicitly set via OutboundIPRangesInclude, then don't return.
		if !ipv4RangesInclude.HasLoopBackIP && !ipv6RangesInclude.HasLoopBackIP {
			if redirectDNS {
				// Users may have a DNS server that is on localhost. In these cases, applications may
				// send TCP traffic to the DNS server that we actually *do* want to intercept. To
				// handle this case, we exclude port 53 from this rule. Note: We cannot just move the
				// port 53 redirection rule further up the list, as we will want to avoid capturing
				// DNS requests from the proxy UID/GID
				iptConfigurator.iptables.AppendRule(constants.ISTIOOUTPUT, constants.NAT, "-o", "lo", "-p", "tcp",
					"!", "--dport", "53",
					"-m", "owner", "!", "--uid-owner", uid, "-j", constants.RETURN)
			} else {
				iptConfigurator.iptables.AppendRule(constants.ISTIOOUTPUT, constants.NAT, "-o", "lo", "-m", "owner", "!", "--uid-owner", uid, "-j", constants.RETURN)
			}
		}

		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		iptConfigurator.iptables.AppendRule(constants.ISTIOOUTPUT, constants.NAT, "-m", "owner", "--uid-owner", uid, "-j", constants.RETURN)
	}

	for _, gid := range split(iptConfigurator.cfg.ProxyGID) {
		// Redirect app calls back to itself via Envoy when using the service VIP
		// e.g. appN => Envoy (client) => Envoy (server) => appN.
		iptConfigurator.iptables.AppendVersionedRule("127.0.0.1/32", "::1/128", constants.ISTIOOUTPUT, constants.NAT,
			"-o", "lo", "!", "-d", constants.IPVersionSpecific,
			"-m", "owner", "--gid-owner", gid, "-j", constants.ISTIOINREDIRECT)

		// Do not redirect app calls to back itself via Envoy when using the endpoint address
		// e.g. appN => appN by lo
		// If loopback explicitly set via OutboundIPRangesInclude, then don't return.
		if !ipv4RangesInclude.HasLoopBackIP && !ipv6RangesInclude.HasLoopBackIP {
			if redirectDNS {
				// Users may have a DNS server that is on localhost. In these cases, applications may
				// send TCP traffic to the DNS server that we actually *do* want to intercept. To
				// handle this case, we exclude port 53 from this rule. Note: We cannot just move the
				// port 53 redirection rule further up the list, as we will want to avoid capturing
				// DNS requests from the proxy UID/GID
				iptConfigurator.iptables.AppendRule(constants.ISTIOOUTPUT, constants.NAT, "-o", "lo", "-p", "tcp",
					"!", "--dport", "53",
					"-m", "owner", "!", "--gid-owner", gid, "-j", constants.RETURN)
			} else {
				iptConfigurator.iptables.AppendRule(constants.ISTIOOUTPUT, constants.NAT, "-o", "lo", "-m", "owner", "!", "--gid-owner", gid, "-j", constants.RETURN)
			}
		}

		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		iptConfigurator.iptables.AppendRule(constants.ISTIOOUTPUT, constants.NAT, "-m", "owner", "--gid-owner", gid, "-j", constants.RETURN)
	}

	if redirectDNS {
		if iptConfigurator.cfg.CaptureAllDNS {
			// Redirect all TCP dns traffic on port 53 to the agent on port 15053
			// This will be useful for the CNI case where pod DNS server address cannot be decided.
			iptConfigurator.iptables.AppendRule(
				constants.ISTIOOUTPUT, constants.NAT,
				"-p", constants.TCP,
				"--dport", "53",
				"-j", constants.REDIRECT,
				"--to-ports", constants.IstioAgentDNSListenerPort)
		} else {
			for _, s := range iptConfigurator.cfg.DNSServersV4 {
				// redirect all TCP dns traffic on port 53 to the agent on port 15053 for all servers
				// in etc/resolv.conf
				// We avoid redirecting all IP ranges to avoid infinite loops when there are local DNS proxies
				// such as: app -> istio dns server -> dnsmasq -> upstream
				// This ensures that we do not get requests from dnsmasq sent back to the agent dns server in a loop.
				// Note: If a user somehow configured etc/resolv.conf to point to dnsmasq and server X, and dnsmasq also
				// pointed to server X, this would not work. However, the assumption is that is not a common case.
				iptConfigurator.iptables.AppendRuleV4(
					constants.ISTIOOUTPUT, constants.NAT,
					"-p", constants.TCP,
					"--dport", "53",
					"-d", s+"/32",
					"-j", constants.REDIRECT,
					"--to-ports", constants.IstioAgentDNSListenerPort)
			}
			for _, s := range iptConfigurator.cfg.DNSServersV6 {
				iptConfigurator.iptables.AppendRuleV6(
					constants.ISTIOOUTPUT, constants.NAT,
					"-p", constants.TCP,
					"--dport", "53",
					"-d", s+"/128",
					"-j", constants.REDIRECT,
					"--to-ports", constants.IstioAgentDNSListenerPort)
			}
		}
	}

	// Skip redirection for Envoy-aware applications and
	// container-to-container traffic both of which explicitly use
	// localhost.
	iptConfigurator.iptables.AppendVersionedRule("127.0.0.1/32", "::1/128", constants.ISTIOOUTPUT, constants.NAT,
		"-d", constants.IPVersionSpecific, "-j", constants.RETURN)
	// Apply outbound IPv4 exclusions. Must be applied before inclusions.
	for _, cidr := range ipv4RangesExclude.IPNets {
		iptConfigurator.iptables.AppendRuleV4(constants.ISTIOOUTPUT, constants.NAT, "-d", cidr.String(), "-j", constants.RETURN)
	}
	for _, cidr := range ipv6RangesExclude.IPNets {
		iptConfigurator.iptables.AppendRuleV6(constants.ISTIOOUTPUT, constants.NAT, "-d", cidr.String(), "-j", constants.RETURN)
	}

	iptConfigurator.handleOutboundPortsInclude()

	iptConfigurator.handleOutboundIncludeRules(ipv4RangesInclude, iptConfigurator.iptables.AppendRuleV4, iptConfigurator.iptables.InsertRuleV4)
	iptConfigurator.handleOutboundIncludeRules(ipv6RangesInclude, iptConfigurator.iptables.AppendRuleV6, iptConfigurator.iptables.InsertRuleV6)

	if redirectDNS {
		HandleDNSUDP(
			AppendOps, iptConfigurator.iptables, iptConfigurator.ext, "",
			iptConfigurator.cfg.ProxyUID, iptConfigurator.cfg.ProxyGID,
			iptConfigurator.cfg.DNSServersV4, iptConfigurator.cfg.DNSServersV6, iptConfigurator.cfg.CaptureAllDNS)
	}

	if iptConfigurator.cfg.InboundInterceptionMode == constants.TPROXY {
		// save packet mark set by envoy.filters.listener.original_src as connection mark
		iptConfigurator.iptables.AppendRule(constants.PREROUTING, constants.MANGLE,
			"-p", constants.TCP, "-m", "mark", "--mark", iptConfigurator.cfg.InboundTProxyMark, "-j", "CONNMARK", "--save-mark")
		// If the packet is already marked with 1337, then return. This is to prevent mark envoy --> app traffic again.
		iptConfigurator.iptables.AppendRule(constants.OUTPUT, constants.MANGLE,
			"-p", constants.TCP, "-o", "lo", "-m", "mark", "--mark", iptConfigurator.cfg.InboundTProxyMark, "-j", constants.RETURN)
		for _, uid := range split(iptConfigurator.cfg.ProxyUID) {
			// mark outgoing packets from envoy to workload by pod ip
			// app call VIP --> envoy outbound -(mark 1338)-> envoy inbound --> app
			iptConfigurator.iptables.AppendRule(constants.OUTPUT, constants.MANGLE,
				"!", "-d", "127.0.0.1/32", "-p", constants.TCP, "-o", "lo", "-m", "owner", "--uid-owner", uid, "-j", constants.MARK, "--set-mark", outboundMark)
		}
		for _, gid := range split(iptConfigurator.cfg.ProxyGID) {
			// mark outgoing packets from envoy to workload by pod ip
			// app call VIP --> envoy outbound -(mark 1338)-> envoy inbound --> app
			iptConfigurator.iptables.AppendRule(constants.OUTPUT, constants.MANGLE,
				"!", "-d", "127.0.0.1/32", "-p", constants.TCP, "-o", "lo", "-m", "owner", "--gid-owner", gid, "-j", constants.MARK, "--set-mark", outboundMark)
		}
		// mark outgoing packets from workload, match it to policy routing entry setup for TPROXY mode
		iptConfigurator.iptables.AppendRule(constants.OUTPUT, constants.MANGLE,
			"-p", constants.TCP, "-m", "connmark", "--mark", iptConfigurator.cfg.InboundTProxyMark, "-j", "CONNMARK", "--restore-mark")
		// prevent infinite redirect
		iptConfigurator.iptables.InsertRule(constants.ISTIOINBOUND, constants.MANGLE, 1,
			"-p", constants.TCP, "-m", "mark", "--mark", iptConfigurator.cfg.InboundTProxyMark, "-j", constants.RETURN)
		// prevent intercept traffic from envoy/pilot-agent ==> app by 127.0.0.6 --> podip
		iptConfigurator.iptables.InsertRule(constants.ISTIOINBOUND, constants.MANGLE, 2,
			"-p", constants.TCP, "-s", "127.0.0.6/32", "-i", "lo", "-j", constants.RETURN)
		// prevent intercept traffic from app ==> app by pod ip
		iptConfigurator.iptables.InsertRule(constants.ISTIOINBOUND, constants.MANGLE, 3,
			"-p", constants.TCP, "-i", "lo", "-m", "mark", "!", "--mark", outboundMark, "-j", constants.RETURN)
	}
	iptConfigurator.executeCommands()
}

type UDPRuleApplier struct {
	iptables *builder.IptablesBuilder
	ext      dep.Dependencies
	ops      Ops
	table    string
	chain    string
	cmd      string
}

func (f UDPRuleApplier) RunV4(args ...string) {
	switch f.ops {
	case AppendOps:
		f.iptables.AppendRuleV4(f.chain, f.table, args...)
	case DeleteOps:
		deleteArgs := []string{"-t", f.table, opsToString[f.ops], f.chain}
		deleteArgs = append(deleteArgs, args...)
		f.ext.RunQuietlyAndIgnore(f.cmd, deleteArgs...)
	}
}

func (f UDPRuleApplier) RunV6(args ...string) {
	switch f.ops {
	case AppendOps:
		f.iptables.AppendRuleV6(f.chain, f.table, args...)
	case DeleteOps:
		deleteArgs := []string{"-t", f.table, opsToString[f.ops], f.chain}
		deleteArgs = append(deleteArgs, args...)
		f.ext.RunQuietlyAndIgnore(f.cmd, deleteArgs...)
	}
}

func (f UDPRuleApplier) Run(args ...string) {
	f.RunV4(args...)
	f.RunV6(args...)
}

func (f UDPRuleApplier) WithChain(chain string) UDPRuleApplier {
	f.chain = chain
	return f
}

func (f UDPRuleApplier) WithTable(table string) UDPRuleApplier {
	f.table = table
	return f
}

// HandleDNSUDP is a helper function to tackle with DNS UDP specific operations.
// This helps the creation logic of DNS UDP rules in sync with the deletion.
func HandleDNSUDP(
	ops Ops, iptables *builder.IptablesBuilder, ext dep.Dependencies,
	cmd, proxyUID, proxyGID string, dnsServersV4 []string, dnsServersV6 []string, captureAllDNS bool) {
	f := UDPRuleApplier{
		iptables: iptables,
		ext:      ext,
		ops:      ops,
		table:    constants.NAT,
		chain:    constants.OUTPUT,
		cmd:      cmd,
	}
	// Make sure that upstream DNS requests from agent/envoy dont get captured.
	// TODO: add ip6 as well
	for _, uid := range split(proxyUID) {
		f.Run("-p", "udp", "--dport", "53", "-m", "owner", "--uid-owner", uid, "-j", constants.RETURN)
	}
	for _, gid := range split(proxyGID) {
		f.Run("-p", "udp", "--dport", "53", "-m", "owner", "--gid-owner", gid, "-j", constants.RETURN)
	}

	if captureAllDNS {
		// Redirect all TCP dns traffic on port 53 to the agent on port 15053
		// This will be useful for the CNI case where pod DNS server address cannot be decided.
		f.Run("-p", "udp", "--dport", "53", "-j", constants.REDIRECT, "--to-port", constants.IstioAgentDNSListenerPort)
	} else {
		// redirect all TCP dns traffic on port 53 to the agent on port 15053 for all servers
		// in etc/resolv.conf
		// We avoid redirecting all IP ranges to avoid infinite loops when there are local DNS proxies
		// such as: app -> istio dns server -> dnsmasq -> upstream
		// This ensures that we do not get requests from dnsmasq sent back to the agent dns server in a loop.
		// Note: If a user somehow configured etc/resolv.conf to point to dnsmasq and server X, and dnsmasq also
		// pointed to server X, this would not work. However, the assumption is that is not a common case.
		for _, s := range dnsServersV4 {
			f.RunV4("-p", "udp", "--dport", "53", "-d", s+"/32",
				"-j", constants.REDIRECT, "--to-port", constants.IstioAgentDNSListenerPort)
		}
		for _, s := range dnsServersV6 {
			f.RunV6("-p", "udp", "--dport", "53", "-d", s+"/128",
				"-j", constants.REDIRECT, "--to-port", constants.IstioAgentDNSListenerPort)
		}
	}
	// Split UDP DNS traffic to separate conntrack zones
	addConntrackZoneDNSUDP(f.WithTable(constants.RAW), proxyUID, proxyGID, dnsServersV4, dnsServersV6, captureAllDNS)
}

// addConntrackZoneDNSUDP is a helper function to add iptables rules to split DNS traffic
// in two separate conntrack zones to avoid issues with UDP conntrack race conditions.
// Traffic that goes from istio to DNS servers and vice versa are zone 1 and traffic from
// DNS client to istio and vice versa goes to zone 2
func addConntrackZoneDNSUDP(
	f UDPRuleApplier, proxyUID, proxyGID string, dnsServersV4 []string, dnsServersV6 []string, captureAllDNS bool) {
	// TODO: add ip6 as well
	for _, uid := range split(proxyUID) {
		// Packets with dst port 53 from istio to zone 1. These are Istio calls to upstream resolvers
		f.Run("-p", "udp", "--dport", "53", "-m", "owner", "--uid-owner", uid, "-j", constants.CT, "--zone", "1")
		// Packets with src port 15053 from istio to zone 2. These are Istio response packets to application clients
		f.Run("-p", "udp", "--sport", "15053", "-m", "owner", "--uid-owner", uid, "-j", constants.CT, "--zone", "2")
	}
	for _, gid := range split(proxyGID) {
		// Packets with dst port 53 from istio to zone 1. These are Istio calls to upstream resolvers
		f.Run("-p", "udp", "--dport", "53", "-m", "owner", "--gid-owner", gid, "-j", constants.CT, "--zone", "1")
		// Packets with src port 15053 from istio to zone 2. These are Istio response packets to application clients
		f.Run("-p", "udp", "--sport", "15053", "-m", "owner", "--gid-owner", gid, "-j", constants.CT, "--zone", "2")

	}

	if captureAllDNS {
		// Not specifying destination address is useful for the CNI case where pod DNS server address cannot be decided.

		// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
		f.Run("-p", "udp", "--dport", "53",
			"-j", constants.CT, "--zone", "2")
		// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
		f.WithChain(constants.PREROUTING).Run("-p", "udp", "--sport", "53",
			"-j", constants.CT, "--zone", "1")
	} else {
		// Go through all DNS servers in etc/resolv.conf and mark the packets based on these destination addresses.
		for _, s := range dnsServersV4 {
			// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
			f.RunV4("-p", "udp", "--dport", "53", "-d", s+"/32",
				"-j", constants.CT, "--zone", "2")
			// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
			f.WithChain(constants.PREROUTING).RunV4("-p", "udp", "--sport", "53", "-d", s+"/32",
				"-j", constants.CT, "--zone", "1")
		}
		for _, s := range dnsServersV6 {
			// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
			f.RunV6("-p", "udp", "--dport", "53", "-d", s+"/128",
				"-j", constants.CT, "--zone", "2")
			// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
			f.WithChain(constants.PREROUTING).RunV6("-p", "udp", "--sport", "53", "-d", s+"/128",
				"-j", constants.CT, "--zone", "1")
		}
	}
}

func (iptConfigurator *IptablesConfigurator) handleOutboundPortsInclude() {
	if iptConfigurator.cfg.OutboundPortsInclude != "" {
		for _, port := range split(iptConfigurator.cfg.OutboundPortsInclude) {
			iptConfigurator.iptables.AppendRule(
				constants.ISTIOOUTPUT, constants.NAT, "-p", constants.TCP, "--dport", port, "-j", constants.ISTIOREDIRECT)
		}
	}
}

func (iptConfigurator *IptablesConfigurator) createRulesFile(f *os.File, contents string) error {
	defer f.Close()
	log.Infof("Writing following contents to rules file: %v\n%v", f.Name(), strings.TrimSpace(contents))
	writer := bufio.NewWriter(f)
	_, err := writer.WriteString(contents)
	if err != nil {
		return fmt.Errorf("unable to write iptables-restore file: %v", err)
	}
	err = writer.Flush()
	return err
}

func (iptConfigurator *IptablesConfigurator) executeIptablesCommands(commands [][]string) {
	for _, cmd := range commands {
		if len(cmd) > 1 {
			iptConfigurator.ext.RunOrFail(cmd[0], cmd[1:]...)
		} else {
			iptConfigurator.ext.RunOrFail(cmd[0])
		}
	}
}

func (iptConfigurator *IptablesConfigurator) executeIptablesRestoreCommand(isIpv4 bool) error {
	var data, filename, cmd string
	if isIpv4 {
		data = iptConfigurator.iptables.BuildV4Restore()
		filename = fmt.Sprintf("iptables-rules-%d.txt", time.Now().UnixNano())
		cmd = constants.IPTABLESRESTORE
	} else {
		data = iptConfigurator.iptables.BuildV6Restore()
		filename = fmt.Sprintf("ip6tables-rules-%d.txt", time.Now().UnixNano())
		cmd = constants.IP6TABLESRESTORE
	}
	var rulesFile *os.File
	var err error
	if iptConfigurator.cfg.OutputPath != "" {
		// Print the iptables rules into the given output file.
		rulesFile, err = os.OpenFile(iptConfigurator.cfg.OutputPath, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("unable to open iptables rules output file %v: %v", iptConfigurator.cfg.OutputPath, err)
		}
	} else {
		// Otherwise create a temporary file to write iptables rules to, which will be cleaned up at the end.
		rulesFile, err = os.CreateTemp("", filename)
		if err != nil {
			return fmt.Errorf("unable to create iptables-restore file: %v", err)
		}
		defer os.Remove(rulesFile.Name())
	}
	if err := iptConfigurator.createRulesFile(rulesFile, data); err != nil {
		return err
	}
	// --noflush to prevent flushing/deleting previous contents from table
	iptConfigurator.ext.RunOrFail(cmd, "--noflush", rulesFile.Name())
	return nil
}

func (iptConfigurator *IptablesConfigurator) executeCommands() {
	if iptConfigurator.cfg.RestoreFormat {
		// Execute iptables-restore
		err := iptConfigurator.executeIptablesRestoreCommand(true)
		if err != nil {
			log.Errorf("Failed to execute iptables-restore command: %v", err)
			os.Exit(1)
		}
		// Execute ip6tables-restore
		err = iptConfigurator.executeIptablesRestoreCommand(false)
		if err != nil {
			log.Errorf("Failed to execute iptables-restore command: %v", err)
			os.Exit(1)
		}
	} else {
		// Execute iptables commands
		iptConfigurator.executeIptablesCommands(iptConfigurator.iptables.BuildV4())
		// Execute ip6tables commands
		iptConfigurator.executeIptablesCommands(iptConfigurator.iptables.BuildV6())

	}
}
