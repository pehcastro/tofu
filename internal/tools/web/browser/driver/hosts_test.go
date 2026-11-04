package driver

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestHostsSaysForEachBrowserWhetherItsNativeHostReachesALiveTofu(t *testing.T) {
	home, hostsKey := t.TempDir(), scratchHostsKey(t)
	t.Cleanup(func() { _, _, _ = Uninstall(home, hostsKey) })
	live, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	every := func(step string, want HostState) {
		t.Helper()
		hosts := Hosts(home, hostsKey)
		t.Logf("%s: %+v", step, hosts)
		if len(hosts) == 0 || slices.ContainsFunc(hosts, func(host NativeHost) bool { return host.State != want || host.Browser == "" }) {
			t.Fatalf("%s: the hosts are %+v, want every one named and %s", step, hosts, want)
		}
	}
	every("nothing installed", HostMissing)
	if _, _, err := Install(home, live, hostsKey); err != nil {
		t.Fatal(err)
	}
	every("installed for a tofu that exists", HostInstalled)
	if err := os.Remove(CopyPath(home)); err != nil {
		t.Fatal(err)
	}
	every("the copy Chrome starts is gone", HostStale)
	if _, _, err := Install(home, live, hostsKey); err != nil {
		t.Fatal(err)
	}
	pointedAtTofu, err := json.Marshal(hostManifest{Name: HostName, Type: "stdio", Path: live})
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range Hosts(home, hostsKey) {
		if err := os.WriteFile(host.Manifest, pointedAtTofu, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	every("an install from before the copy, naming tofu itself", HostStale)
	for _, host := range Hosts(home, hostsKey) {
		if err := os.WriteFile(host.Manifest, []byte("not a manifest"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	every("a manifest that is not one", HostMissing)
	for _, host := range Hosts(home, hostsKey) {
		if err := os.Remove(host.Manifest); err != nil {
			t.Fatal(err)
		}
	}
	every("the manifest deleted and the registration left", HostMissing)
}
