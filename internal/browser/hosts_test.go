package browser

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestHostsSaysForEachBrowserWhetherItsNativeHostReachesALiveTofu(t *testing.T) {
	home, hostsKey := t.TempDir(), scratchHostsKey(t)
	t.Cleanup(func() { _, _ = Uninstall(home, hostsKey) })
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
	if _, _, err := Install(home, filepath.Join(home, "gone", "tofu"), hostsKey); err != nil {
		t.Fatal(err)
	}
	every("installed for a tofu that is gone", HostStale)
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
