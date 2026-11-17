package mainproxy

import "whoisthat-core/lib/xraymgr"

// requireTunCaps is the pre-TUN-start precondition: xray's TUN inbound needs
// cap_net_admin on the runtime file, and a missing capability otherwise
// surfaces only as xray exiting 255. Test seam.
var requireTunCaps = func() error {
	bin, err := xraymgr.GetManager().GetXrayBin()
	if err != nil {
		return nil // runtime not ready: Start reports that itself
	}
	return xraymgr.EnsureTunCaps(bin)
}
