package warplib

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
)

const (
	// interfacePartMax is the bonded part size target. The final part may be
	// shorter, or longer when the 4096 ceiling or a chosen segment cap forces it.
	interfacePartMax int64 = 8 * MB
	// interfacePartMin is the smallest non-final bonded part.
	interfacePartMin int64 = 1 * MB
	// interfacePartCeiling caps a bonded part map so a huge file cannot
	// create an unbounded set of parts.
	interfacePartCeiling int32 = 4096
	// interfaceMinPartsPerIface keeps each selected link busy.
	interfaceMinPartsPerIface = 4
	// interfaceMaxWorkersPerIface is the per-link worker cap.
	interfaceMaxWorkersPerIface = 8
	// interfaceMaxWorkers is the absolute worker cap. The connection limit
	// can only make this smaller.
	interfaceMaxWorkers = 32

	logChosenInterfaces = "chosen interfaces:"
	logPolicyNotApplied = "interface policy not applied:"
	logSingleRoute      = "falling back to a single route:"
	logPartsTooLarge    = "parts are too large to balance links tightly"
	logMovedPart        = "moved part from"
	logSkippedInterface = "skipped interface"
)

// InterfaceBinding is one selected network device and the IPv4 address parts
// bind to. Addresses are resolved when a transfer starts and again on resume;
// they are not stored with the download.
type InterfaceBinding struct {
	Name string
	IP   net.IP
}

// InterfaceDialFunc dials address for one selected device.
//
// An empty address is the pre-body pin probe. Return ErrInterfacePinRefused
// when the device cannot be pinned, or (nil, nil) when the pin is acceptable.
// Production uses this probe once per device before any part body is saved.
// A test replaces the function to record the local address, slow one device
// down, or fail dials.
type InterfaceDialFunc func(ctx context.Context, network, address string, local net.IP, device string) (net.Conn, error)

// InterfaceInfo is one row of the interfaces listing.
type InterfaceInfo struct {
	Name     string
	IPv4     net.IP
	PortName string
	Auto     bool
}

type policyKind int

const (
	policyOff policyKind = iota
	policyAuto
	policyExplicit
)

type hostInterface struct {
	Name  string
	Flags net.Flags
	IPv4  []net.IP
}

// discoverInterfaces lists host interfaces. Tests replace it so a download
// does not depend on the machine's real devices.
var discoverInterfaces = discoverSystemInterfaces

// lookupHardwarePorts maps a device name to a human-readable port name.
// macOS fills it from networksetup; other systems leave it empty.
var lookupHardwarePorts = readHardwarePorts

var virtualInterfacePrefixes = []string{
	"utun",
	"tun",
	"tap",
	"wg",
	"tailscale",
	"docker",
	"veth",
	"virbr",
	"vmnet",
	"awdl",
	"bridge",
}

func discoverSystemInterfaces() ([]hostInterface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]hostInterface, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, addrErr := iface.Addrs()
		if addrErr != nil {
			continue
		}
		item := hostInterface{Name: iface.Name, Flags: iface.Flags}
		for _, addr := range addrs {
			if ip := ipv4FromAddr(addr); ip != nil {
				item.IPv4 = append(item.IPv4, ip)
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func ipv4FromAddr(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP.To4()
	case *net.IPAddr:
		return value.IP.To4()
	default:
		return nil
	}
}

// classifyInterfacePolicy parses a policy string. Empty and "off" are off.
// "auto" selects eligible devices. Anything else is a comma-separated device
// list. A mixed token such as "off,eth0" is invalid.
func classifyInterfacePolicy(raw string) (kind policyKind, names []string, normalized string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "off") {
		return policyOff, nil, "off", nil
	}
	if strings.EqualFold(trimmed, "auto") {
		return policyAuto, nil, "auto", nil
	}
	parts := strings.Split(trimmed, ",")
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" || strings.EqualFold(name, "off") || strings.EqualFold(name, "auto") {
			return policyOff, nil, "", fmt.Errorf("%w: %q", ErrInvalidInterfacePolicy, raw)
		}
		if strings.ContainsAny(name, " \t/\\") {
			return policyOff, nil, "", fmt.Errorf("%w: %q", ErrInvalidInterfacePolicy, raw)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return policyOff, nil, "", fmt.Errorf("%w: %q", ErrInvalidInterfacePolicy, raw)
	}
	return policyExplicit, names, strings.Join(names, ","), nil
}

// MultiInterfaceHTTPOnly reports an explicit device list on a protocol that
// cannot use it. auto and off are ignored so a machine-wide HTTP default does
// not fail FTP or SFTP.
func MultiInterfaceHTTPOnly(policy string) error {
	kind, _, _, err := classifyInterfacePolicy(policy)
	if err != nil {
		return err
	}
	if kind == policyExplicit {
		return fmt.Errorf("%w", ErrMultiInterfaceHTTPOnly)
	}
	return nil
}

func interfacePolicyUsesBondedBudget(policy string) bool {
	kind, _, _, err := classifyInterfacePolicy(policy)
	if err != nil {
		return false
	}
	return kind == policyAuto || kind == policyExplicit
}

func skippedVirtualName(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range virtualInterfacePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func firstIPv4(ips []net.IP, globalOnly bool) net.IP {
	for _, ip := range ips {
		ip = ip.To4()
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			continue
		}
		if globalOnly && (ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
			continue
		}
		return append(net.IP(nil), ip...)
	}
	return nil
}

func autoBindings(ifaces []hostInterface) []InterfaceBinding {
	out := make([]InterfaceBinding, 0, len(ifaces))
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if skippedVirtualName(iface.Name) {
			continue
		}
		ip := firstIPv4(iface.IPv4, true)
		if ip == nil {
			continue
		}
		out = append(out, InterfaceBinding{Name: iface.Name, IP: ip})
	}
	return out
}

func explicitBindings(names []string, ifaces []hostInterface) ([]InterfaceBinding, error) {
	byName := make(map[string]hostInterface, len(ifaces))
	for _, iface := range ifaces {
		byName[iface.Name] = iface
	}
	out := make([]InterfaceBinding, 0, len(names))
	for _, name := range names {
		iface, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrInterfaceNotFound, name)
		}
		if iface.Flags&net.FlagUp == 0 {
			return nil, fmt.Errorf("%w: %s", ErrInterfaceDown, name)
		}
		// An explicit name bypasses the virtual-adapter filter, but it still
		// needs an IPv4 address. Link-local is accepted; auto would skip it.
		ip := firstIPv4(iface.IPv4, false)
		if ip == nil {
			return nil, fmt.Errorf("%w: %s", ErrInterfaceNoIPv4, name)
		}
		out = append(out, InterfaceBinding{Name: name, IP: ip})
	}
	return out, nil
}

// planBondedPartCount chooses the bonded part count. It returns ok false when
// the file cannot meet the 1 MB floor and four parts per interface; the caller
// then keeps today's single-route schedule.
func planBondedPartCount(size int64, nIfaces int, chosen bool, chosenCap int32) (count int32, oversized bool, ok bool) {
	if size <= 0 || nIfaces < 2 {
		return 0, false, false
	}
	minCount := int64(interfaceMinPartsPerIface * nIfaces)
	if size < minCount*interfacePartMin {
		return 0, false, false
	}
	count64 := (size + interfacePartMax - 1) / interfacePartMax
	if count64 < minCount {
		count64 = minCount
	}
	maxByMin := size / interfacePartMin
	if count64 > maxByMin {
		count64 = maxByMin
	}
	if count64 > int64(interfacePartCeiling) {
		count64 = int64(interfacePartCeiling)
		oversized = true
	}
	if chosen && chosenCap > 0 && count64 > int64(chosenCap) {
		count64 = int64(chosenCap)
	}
	if count64 < 2 {
		return 0, false, false
	}
	nominal := size / count64
	if nominal < interfacePartMin {
		return 0, false, false
	}
	if nominal > interfacePartMax {
		oversized = true
	}
	return int32(count64), oversized, true
}

// ListInterfaces returns every discovered device, its IPv4 address when it
// has one, a hardware port name when the OS provides one, and whether auto
// would select it.
func ListInterfaces() ([]InterfaceInfo, error) {
	ifaces, err := discoverInterfaces()
	if err != nil {
		return nil, err
	}
	ports := lookupHardwarePorts()
	selected := make(map[string]struct{})
	for _, binding := range autoBindings(ifaces) {
		selected[binding.Name] = struct{}{}
	}
	out := make([]InterfaceInfo, 0, len(ifaces))
	for _, iface := range ifaces {
		_, auto := selected[iface.Name]
		out = append(out, InterfaceInfo{
			Name:     iface.Name,
			IPv4:     firstIPv4(iface.IPv4, false),
			PortName: ports[iface.Name],
			Auto:     auto,
		})
	}
	return out, nil
}

func readHardwarePorts() map[string]string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	out, err := exec.Command("networksetup", "-listallhardwareports").Output()
	if err != nil {
		return nil
	}
	return parseHardwarePorts(string(out))
}

// parseHardwarePorts reads the macOS networksetup device listing.
// A "Hardware Port" line is paired with the following "Device" line.
func parseHardwarePorts(text string) map[string]string {
	ports := make(map[string]string)
	var pending string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if name, ok := strings.CutPrefix(line, "Hardware Port:"); ok {
			pending = strings.TrimSpace(name)
			continue
		}
		if name, ok := strings.CutPrefix(line, "Device:"); ok && pending != "" {
			ports[strings.TrimSpace(name)] = pending
			pending = ""
		}
	}
	return ports
}

func (d *Downloader) applyInterfacePlan(resume bool) error {
	kind, names, normalized, err := classifyInterfacePolicy(d.interfacePolicy)
	if err != nil {
		return err
	}
	d.interfacePolicy = normalized
	if kind == policyOff {
		return nil
	}
	if !d.resumable || d.GetContentLength().v() <= 1 {
		d.Log("%s response is not splittable", logPolicyNotApplied)
		return nil
	}
	bindings, err := d.resolveInterfaceBindings(kind, names)
	if err != nil {
		d.Log("%s %v", logPolicyNotApplied, err)
		return err
	}
	if len(bindings) < 2 && kind == policyAuto {
		d.Log("%s fewer than 2 usable interfaces", logPolicyNotApplied)
		return nil
	}
	pinned, err := d.selectPinnedInterfaces(bindings, kind == policyExplicit)
	if err != nil {
		d.Log("%s %v", logPolicyNotApplied, err)
		return err
	}
	if len(pinned) < 2 {
		d.Log("%s fewer than 2 usable interfaces", logSingleRoute)
		return nil
	}
	chosenCap := int32(0)
	if d.segmentLimitChosen {
		chosenCap = d.maxParts
	}
	count, oversized, ok := planBondedPartCount(
		d.GetContentLength().v(),
		len(pinned),
		d.segmentLimitChosen,
		chosenCap,
	)
	if !ok {
		// A file that cannot meet the bonded minimums stays on one connection.
		if !resume {
			d.numBaseParts = 1
		}
		d.Log("%s file is too small to split across interfaces", logPolicyNotApplied)
		return nil
	}
	clients, err := d.buildInterfaceClients(pinned)
	if err != nil {
		return err
	}
	if !resume {
		d.numBaseParts = count
		// An unchosen limit, including the command's filled-in 200, is not
		// the bonded cap. The stored cap is the one this transfer applied.
		if !(d.segmentLimitChosen && d.maxParts > 0 && count <= d.maxParts) {
			d.maxParts = count
		}
		if oversized {
			nominal := d.GetContentLength().v() / int64(count)
			d.Log("%s: %d bytes", logPartsTooLarge, nominal)
		}
	}
	d.ifaceClients = clients
	d.multiActive = true
	if d.partIface == nil {
		d.partIface = make(map[string]*partIfaceState)
	}
	d.Log("%s %s", logChosenInterfaces, formatInterfaceBindings(pinned))
	return nil
}

func (d *Downloader) resolveInterfaceBindings(kind policyKind, names []string) ([]InterfaceBinding, error) {
	if d.interfaceBindingsHint != nil {
		out := make([]InterfaceBinding, len(d.interfaceBindingsHint))
		for i, binding := range d.interfaceBindingsHint {
			out[i] = InterfaceBinding{
				Name: binding.Name,
				IP:   append(net.IP(nil), binding.IP...),
			}
		}
		return out, nil
	}
	found, err := discoverInterfaces()
	if err != nil {
		if kind == policyExplicit {
			return nil, fmt.Errorf("%w: %s", ErrInterfaceNotFound, strings.Join(names, ","))
		}
		d.Log("%s interface lookup failed: %v", logPolicyNotApplied, err)
		return nil, nil
	}
	if kind == policyAuto {
		return autoBindings(found), nil
	}
	return explicitBindings(names, found)
}

func formatInterfaceBindings(bindings []InterfaceBinding) string {
	parts := make([]string, len(bindings))
	for i, binding := range bindings {
		ip := ""
		if binding.IP != nil {
			ip = binding.IP.String()
		}
		parts[i] = fmt.Sprintf("%s (%s)", binding.Name, ip)
	}
	return strings.Join(parts, ", ")
}
