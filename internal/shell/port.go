package shell

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ipv4Loopback     = "127.0.0.1"
	ipv6Loopback     = "::1"
	bunDefaultPort   = 3000
	nextDefaultPort  = 3000
	viteDefaultPort  = 5173
	astroDefaultPort = 4321
	highestPort      = 65535
	procListenState  = "0A"
)

func PortOpen(host string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

type Address struct {
	Host    string
	Open    bool
	PID     int
	Command string
}

type listener struct {
	port int
	ipv6 bool
	pid  int
}

func dialLoopbacks(port int, dial time.Duration) []Address {
	addresses := []Address{{Host: ipv4Loopback}, {Host: ipv6Loopback}}
	var dialing sync.WaitGroup
	for i := range addresses {
		dialing.Go(func() { addresses[i].Open = PortOpen(addresses[i].Host, port, dial) })
	}
	dialing.Wait()
	return addresses
}

func Probe(ctx context.Context, port int, dial time.Duration) []Address {
	addresses := dialLoopbacks(port, dial)
	if !slices.ContainsFunc(addresses, func(one Address) bool { return one.Open }) {
		return addresses
	}
	held := listening(ctx)
	for i := range addresses {
		at := slices.IndexFunc(held, func(one listener) bool {
			return one.port == port && one.ipv6 == (addresses[i].Host == ipv6Loopback)
		})
		if addresses[i].Open && at >= 0 {
			addresses[i].PID = held[at].pid
			addresses[i].Command = commandLine(ctx, held[at].pid)
		}
	}
	return addresses
}

func listening(ctx context.Context) []listener {
	if runtime.GOOS == "windows" {
		return netstatListeners(ctx)
	}
	if _, err := exec.LookPath("lsof"); err != nil && runtime.GOOS == "linux" {
		return procListeners()
	}
	return lsofListeners(ctx)
}

func procListeners() []listener {
	sockets := map[string]listener{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, _ := os.ReadFile(table)
		for line := range strings.Lines(string(raw)) {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != procListenState {
				continue
			}
			cut := strings.LastIndex(fields[1], ":")
			if port, err := strconv.ParseInt(fields[1][cut+1:], 16, 32); err == nil {
				sockets["socket:["+fields[9]+"]"] = listener{port: int(port), ipv6: strings.HasSuffix(table, "6")}
			}
		}
	}
	links, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	var found []listener
	for _, link := range links {
		target, _ := os.Readlink(link)
		held, isListener := sockets[target]
		if !isListener {
			continue
		}
		held.pid, _ = strconv.Atoi(strings.Split(link, "/")[2])
		found = append(found, held)
	}
	return found
}

func netstatListeners(ctx context.Context) []listener {
	out, _ := exec.CommandContext(ctx, "netstat", "-ano").Output()
	var found []listener
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) != 5 || fields[2] != "0.0.0.0:0" && fields[2] != "[::]:0" {
			continue
		}
		cut := strings.LastIndex(fields[1], ":")
		port, portErr := strconv.Atoi(fields[1][cut+1:])
		pid, pidErr := strconv.Atoi(fields[4])
		if portErr == nil && pidErr == nil {
			found = append(found, listener{port: port, ipv6: strings.HasPrefix(fields[1], "["), pid: pid})
		}
	}
	return found
}

func lsofListeners(ctx context.Context) []listener {
	out, _ := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fptn").Output()
	var found []listener
	var pid int
	var ipv6 bool
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		value := line[1:]
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(value)
		case 't':
			ipv6 = value == "IPv6"
		case 'n':
			if port, err := strconv.Atoi(value[strings.LastIndex(value, ":")+1:]); err == nil {
				found = append(found, listener{port: port, ipv6: ipv6, pid: pid})
			}
		}
	}
	return found
}

func commandLine(ctx context.Context, pid int) string {
	if runtime.GOOS != "windows" {
		out, _ := exec.CommandContext(ctx, "ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
		return strings.TrimSpace(string(out))
	}
	out, _ := exec.CommandContext(ctx, "wmic", "process", "where", "processid="+strconv.Itoa(pid), "get", "CommandLine", "/value").Output()
	for line := range strings.Lines(string(out)) {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "CommandLine="); found {
			return html.UnescapeString(value)
		}
	}
	return ""
}

var ErrPortHeld = errors.New("shell: the port is already held")

func (r *Registry) refuseHeld(ctx context.Context, port int, dial time.Duration) error {
	addresses := Probe(ctx, port, dial)
	at := slices.IndexFunc(addresses, func(one Address) bool { return one.Open && one.PID > 0 })
	if at < 0 {
		at = slices.IndexFunc(addresses, func(one Address) bool { return one.Open })
	}
	if at < 0 {
		return nil
	}
	holder := addresses[at]
	held, free := listening(ctx), port+1
	for slices.ContainsFunc(held, func(one listener) bool { return one.port == free }) {
		free++
	}
	listed, _ := r.List()
	if own := slices.IndexFunc(listed, func(one Shell) bool { return one.State == Running && treeHas(one.PID, holder.PID) }); holder.PID > 0 && own >= 0 {
		return fmt.Errorf("%w: port %d on %s is served by %s, which tofu started as pid %d: restart it with the shell tool rather than starting a second server, or use port %d, which is free",
			ErrPortHeld, port, holder.Host, listed[own].Name, listed[own].PID, free)
	}
	return fmt.Errorf("%w: port %d on %s is held by %s, which tofu did not start. this start was refused, because two servers on one port answer the browser at random. port %d is free",
		ErrPortHeld, port, holder.Host, holder.Holder(), free)
}

func (a Address) Holder() string {
	if a.PID == 0 {
		return "a process this OS would not name"
	}
	return fmt.Sprintf("pid %d: %s", a.PID, cmp.Or(a.Command, "command line not readable"))
}

var (
	controlSequence = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	localAddress    = regexp.MustCompile(`(?:^|[^\w.-])(?:localhost|127\.0\.0\.1|\[::1\]|0\.0\.0\.0):(\d{1,5})\b`)
)

func PortInOutput(text string) int {
	for _, match := range localAddress.FindAllStringSubmatch(controlSequence.ReplaceAllString(text, ""), -1) {
		if port, _ := strconv.Atoi(match[1]); port > 0 && port <= highestPort {
			return port
		}
	}
	return 0
}

func (r *Registry) Listening(ctx context.Context, shells []Shell) map[string]int {
	found := map[string]int{}
	for _, held := range listening(ctx) {
		for _, one := range shells {
			if held.pid > 0 && treeHas(one.PID, held.pid) && (found[one.Name] == 0 || held.port < found[one.Name]) {
				found[one.Name] = held.port
			}
		}
	}
	return found
}

func (r *Registry) SetPort(name string, port int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, err := r.readLocked(name)
	if err != nil || entry.Port != 0 {
		return err
	}
	entry.Port = port
	return r.writeLocked(entry)
}

func NamedPort(dir, command string, env []string) int {
	script := packageScript(dir, command)
	explicit := regexp.MustCompile(`(?:\b(?:BUN_)?PORT=|--port[= ])(\d+)`)
	for _, text := range []string{command, script} {
		if match := explicit.FindStringSubmatch(text); match != nil {
			port, _ := strconv.Atoi(match[1])
			return port
		}
	}
	for _, pair := range slices.Backward(env) {
		name, value, _ := strings.Cut(pair, "=")
		if port, err := strconv.Atoi(value); err == nil && (name == "PORT" || name == "BUN_PORT") {
			return port
		}
	}
	return devServerDefault(cmp.Or(script, command))
}

func wordsWithoutAssignments(text string) []string {
	return slices.DeleteFunc(strings.Fields(text), func(field string) bool { return strings.Contains(field, "=") })
}

func devServerDefault(text string) int {
	fields := wordsWithoutAssignments(text)
	if len(fields) > 0 && (fields[0] == "npx" || fields[0] == "bunx") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return 0
	}
	subcommand := ""
	if len(fields) > 1 {
		subcommand = fields[1]
	}
	switch fields[0] {
	case "vite":
		if subcommand == "" || subcommand == "dev" || subcommand == "serve" || strings.HasPrefix(subcommand, "-") {
			return viteDefaultPort
		}
	case "next":
		if subcommand == "dev" {
			return nextDefaultPort
		}
	case "astro":
		if subcommand == "dev" {
			return astroDefaultPort
		}
	case "bun":
		if subcommand != "test" && slices.ContainsFunc(fields[1:], func(field string) bool {
			return field == "--hot" || slices.Contains([]string{".ts", ".tsx", ".js", ".jsx", ".mjs"}, filepath.Ext(field))
		}) {
			return bunDefaultPort
		}
	}
	return 0
}

func packageScript(dir, command string) string {
	fields := wordsWithoutAssignments(command)
	if len(fields) < 2 || !slices.Contains([]string{"bun", "npm", "pnpm", "yarn"}, fields[0]) {
		return ""
	}
	name := fields[1]
	if name == "run" && len(fields) > 2 {
		name = fields[2]
	}
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err != nil || json.Unmarshal(raw, &manifest) != nil {
		return ""
	}
	return manifest.Scripts[name]
}
