package cmd

import (
	"testing"

	appconfig "whoisthat-core/lib/AppConfig"
	tunmode "whoisthat-core/lib/proxy/tun"
	"whoisthat-core/structs"
)

func TestDnsHijackForFallsBackToLegacy(t *testing.T) {
	cmd, _, _ := newTestCmd(t)

	plan := cmd.dnsHijackFor(structs.Profile{GroupId: 0})
	if !plan.Enabled || !plan.Legacy || plan.Happ {
		t.Fatalf("expected legacy hijack, got %+v", plan)
	}
	if plan.Sink4 == "" {
		t.Error("legacy plan must carry a v4 resolver")
	}
}

func TestDnsHijackForManagedJSONProfileUsesSink(t *testing.T) {
	cmd, _, _ := newTestCmd(t)

	plan := cmd.dnsHijackFor(structs.Profile{GroupId: 99, Managed: true})
	if !plan.Enabled || !plan.Happ || plan.Legacy {
		t.Fatalf("expected Happ-style hijack for managed profile, got %+v", plan)
	}
	if plan.Sink4 != tunmode.DNSSink4 || plan.Sink6 != tunmode.DNSSink6 {
		t.Errorf("unexpected sinks: %+v", plan)
	}
}

func TestDnsHijackForUsesSinkForHappDNS(t *testing.T) {
	cmd, _, _ := newTestCmd(t)
	group, err := cmd.DB.AddGroup("DNSGroup", "")
	if err != nil {
		t.Fatalf("AddGroup failed: %v", err)
	}

	profile := structs.RoutingProfile{
		Id:      "dns",
		GroupId: group.Id,
		Name:    "DNS",
		Source:  "happ",
		State:   "ready",
		Rules: []structs.RoutingRule{
			{Type: "field", Domain: "example.com", OutboundTag: "direct", Enabled: true},
		},
		DNS: &structs.HappDNSConfig{
			RemoteDNSType: "DoU",
			RemoteDNSIP:   "1.1.1.1",
		},
	}
	if err := cmd.DB.SaveRoutingProfile(profile); err != nil {
		t.Fatalf("SaveRoutingProfile failed: %v", err)
	}
	if err := cmd.DB.SetActiveRoutingProfile(group.Id, profile.Id); err != nil {
		t.Fatalf("SetActiveRoutingProfile failed: %v", err)
	}

	plan := cmd.dnsHijackFor(structs.Profile{GroupId: group.Id})
	if !plan.Enabled || !plan.Happ || plan.Legacy {
		t.Fatalf("expected Happ hijack, got %+v", plan)
	}
	if plan.Sink4 != tunmode.DNSSink4 || plan.Sink6 != tunmode.DNSSink6 {
		t.Errorf("unexpected sinks: %+v", plan)
	}
	if plan.ScopeMark != 1 {
		t.Errorf("expected mark-1 scope, got %d", plan.ScopeMark)
	}

	// include mode must keep the Happ plan but disable the hijack.
	appconfig.SetSplitTunnelMode("include")
	t.Cleanup(func() { appconfig.SetSplitTunnelMode("off") })
	plan = cmd.dnsHijackFor(structs.Profile{GroupId: group.Id})
	if plan.Enabled {
		t.Errorf("include mode must disable the hijack: %+v", plan)
	}
	if !plan.Happ || plan.Sink4 != tunmode.DNSSink4 {
		t.Errorf("include mode must preserve the Happ plan: %+v", plan)
	}
}

func TestDnsHijackForManualModeUsesLegacy(t *testing.T) {
	cmd, _, _ := newTestCmd(t)
	group, err := cmd.DB.AddGroup("ManualGroup", "")
	if err != nil {
		t.Fatalf("AddGroup failed: %v", err)
	}

	profile := structs.RoutingProfile{
		Id:      "dns",
		GroupId: group.Id,
		Name:    "DNS",
		Source:  "happ",
		State:   "ready",
		Rules: []structs.RoutingRule{
			{Type: "field", Domain: "example.com", OutboundTag: "direct", Enabled: true},
		},
		DNS: &structs.HappDNSConfig{
			RemoteDNSType: "DoU",
			RemoteDNSIP:   "1.1.1.1",
		},
	}
	if err := cmd.DB.SaveRoutingProfile(profile); err != nil {
		t.Fatalf("SaveRoutingProfile failed: %v", err)
	}
	if err := cmd.DB.SetGroupRoutingMode(group.Id, "manual"); err != nil {
		t.Fatalf("SetGroupRoutingMode failed: %v", err)
	}

	plan := cmd.dnsHijackFor(structs.Profile{GroupId: group.Id})
	if !plan.Legacy || plan.Happ {
		t.Errorf("manual routing must use the legacy hijack: %+v", plan)
	}
}

// K-4: allowlisted resolvers must stay out of the port-53 DNAT in every
// strategy, otherwise they are redirected to the sink and blackhole when the
// core dies.
func TestDnsHijackForExcludesAllowlistedResolvers(t *testing.T) {
	cmd, _, _ := newTestCmd(t)
	want := appconfig.GetConfig().DnsServers
	if len(want) == 0 {
		t.Fatal("test needs default dns servers")
	}

	group, err := cmd.DB.AddGroup("K4Group", "")
	if err != nil {
		t.Fatalf("AddGroup failed: %v", err)
	}
	rp := structs.RoutingProfile{
		Id: "k4", GroupId: group.Id, Name: "K4", Source: "happ", State: "ready",
		Rules: []structs.RoutingRule{{Type: "field", Domain: "example.com", OutboundTag: "direct", Enabled: true}},
		DNS:   &structs.HappDNSConfig{RemoteDNSType: "DoU", RemoteDNSIP: "1.1.1.1"},
	}
	if err := cmd.DB.SaveRoutingProfile(rp); err != nil {
		t.Fatalf("SaveRoutingProfile failed: %v", err)
	}
	if err := cmd.DB.SetActiveRoutingProfile(group.Id, rp.Id); err != nil {
		t.Fatalf("SetActiveRoutingProfile failed: %v", err)
	}

	cases := map[string]tunmode.DnsHijack{
		"managed": cmd.dnsHijackFor(structs.Profile{GroupId: 99, Managed: true}),
		"happ":    cmd.dnsHijackFor(structs.Profile{GroupId: group.Id}),
		"legacy":  cmd.dnsHijackFor(structs.Profile{GroupId: 0}),
	}
	for name, plan := range cases {
		got := map[string]bool{}
		for _, ip := range plan.Exclude {
			got[ip] = true
		}
		for _, w := range want {
			if !got[w] {
				t.Errorf("%s plan: resolver %s missing from Exclude %v", name, w, plan.Exclude)
			}
		}
	}
}
