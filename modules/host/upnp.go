package host

import (
	"net"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
)

// managedLearnHostname discovers the external IP of the Host. If the host's
// net address is blank and the host's auto address appears to have changed,
// the host will make an announcement on the blockchain.
func (h *Host) managedLearnHostname() {
	if build.Release == "testing" {
		return
	}

	// Fetch a group of host vars that will be used to dictate the logic of the
	// function.
	h.mu.RLock()
	netAddr := h.settings.NetAddress
	hostPort := h.port
	hostAutoAddress := h.autoAddress
	hostAnnounced := h.announced
	hostAcceptingContracts := h.settings.AcceptingContracts
	hostContractCount := h.financialMetrics.ContractCount
	h.mu.RUnlock()

	// If the settings indicate that an address has been manually set, there is
	// no reason to learn the hostname.
	if netAddr != "" {
		return
	}
	h.log.Println("No manually set net address. Scanning to automatically determine address.")

	// Use the gateway to get the external ip.
	hostname, err := h.g.DiscoverAddress(h.tg.StopChan())
	if err != nil {
		h.log.Println("WARN: failed to discover external IP")
		return
	}

	autoAddress := modules.NetAddress(net.JoinHostPort(hostname.String(), hostPort))
	if err := autoAddress.IsValid(); err != nil {
		h.log.Printf("WARN: discovered hostname %q is invalid: %v", autoAddress, err)
		return
	}
	if autoAddress == hostAutoAddress && hostAnnounced {
		// Nothing to do - the auto address has not changed and the previous
		// annoucement was successful.
		return
	}

	h.mu.Lock()
	h.autoAddress = autoAddress
	err = h.saveSync()
	h.mu.Unlock()
	if err != nil {
		h.log.Println(err)
	}

	// Announce the host, but only if the host is either accepting contracts or
	// has a storage obligation. If the host is not accepting contracts and has
	// no open contracts, there is no reason to notify anyone that the host's
	// address has changed.
	if hostAcceptingContracts || hostContractCount > 0 {
		h.log.Println("Host external IP address changed from", hostAutoAddress, "to", autoAddress, "- performing host announcement.")
		err = h.managedAnnounce(autoAddress)
		if err != nil {
			// Set h.announced to false, as the address has changed yet the
			// renewed annoucement has failed.
			h.mu.Lock()
			h.announced = false
			h.mu.Unlock()
			h.log.Println("unable to announce address after upnp-detected address change:", err)
		}
	}
}
