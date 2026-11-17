package xrayjson

import (
	"encoding/json"
	"testing"
)

const forwardingFixture = `{"remarks":"fw","outbounds":[
 {"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"203.0.113.9","port":443}]}},
 {"tag":"hop","protocol":"vless","settings":{"vnext":[{"address":"203.0.113.10","port":443}]},"streamSettings":{"sockopt":{"dialerProxy":"proxy"}}},
 {"tag":"bh","protocol":"blackhole"}]}`

func prepared(t *testing.T, tun string) map[string]interface{} {
	t.Helper()
	p, err := Prepare(json.RawMessage(forwardingFixture), Options{TunName: tun, SocksPort: 1080})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(p.Config, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func outboundMark(ob map[string]interface{}) interface{} {
	stream, _ := ob["streamSettings"].(map[string]interface{})
	sockopt, _ := stream["sockopt"].(map[string]interface{})
	return sockopt["mark"]
}

// The forwarding path, not just the device: with a TUN inbound every socket
// xray opens must carry fwmark 1 (so it takes table 100, never the TUN default
// route) and traffic entering tun-in must be routed to a real outbound.
func TestTunForwardingPathExists(t *testing.T) {
	cfg := prepared(t, "whoisthattun")

	var tunIn map[string]interface{}
	for _, raw := range cfg["inbounds"].([]interface{}) {
		if ib := raw.(map[string]interface{}); ib["tag"] == "tun-in" {
			tunIn = ib
		}
	}
	if tunIn == nil || tunIn["protocol"] != "tun" {
		t.Fatalf("no tun inbound: %+v", cfg["inbounds"])
	}
	if tunIn["settings"].(map[string]interface{})["name"] != "whoisthattun" {
		t.Errorf("tun inbound name mismatch: %+v", tunIn)
	}

	byTag := map[string]map[string]interface{}{}
	for _, raw := range cfg["outbounds"].([]interface{}) {
		ob := raw.(map[string]interface{})
		byTag[ob["tag"].(string)] = ob
		switch ob["protocol"] {
		case "blackhole", "dns", "loopback":
			continue
		}
		if outboundMark(ob) != float64(1) {
			t.Errorf("outbound %v dials without fwmark 1: %+v", ob["tag"], ob)
		}
	}
	if byTag["hop"]["streamSettings"].(map[string]interface{})["sockopt"].(map[string]interface{})["dialerProxy"] != "proxy" {
		t.Error("marking must keep dialerProxy")
	}

	reachable := false
	routing := cfg["routing"].(map[string]interface{})
	for _, raw := range routing["rules"].([]interface{}) {
		rule := raw.(map[string]interface{})
		tags, _ := rule["inboundTag"].([]interface{})
		for _, tg := range tags {
			if tg != "tun-in" {
				continue
			}
			if ot, _ := rule["outboundTag"].(string); ot != "" && byTag[ot] != nil && byTag[ot]["protocol"] != "blackhole" {
				reachable = true
			}
			if _, ok := rule["balancerTag"]; ok {
				reachable = true
			}
		}
	}
	if !reachable {
		t.Errorf("no rule forwards tun-in to a live outbound: %+v", routing["rules"])
	}
}

func TestMarkOnProxyOutboundsWithoutTun(t *testing.T) {
	cfg := prepared(t, "")
	for _, raw := range cfg["outbounds"].([]interface{}) {
		ob := raw.(map[string]interface{})
		if ob["protocol"] == "vless" && outboundMark(ob) == nil {
			t.Errorf("dialing outbound must be marked even without a tun name: %+v", ob)
		}
	}
}
