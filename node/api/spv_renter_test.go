package api

import (
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
)

func waitTillTesterSync(st1, st2 *serverTester, t *testing.T) {
	// blockchains should now match
	for i := 0; i < 50; i++ {
		if st1.cs.Height() != st2.cs.Height() {
			time.Sleep(250 * time.Millisecond)
		}
	}
	log.Printf("st1 %d, st2 %d", st1.cs.Height(), st2.cs.Height())

	if st1.cs.Height() != st2.cs.Height() {
		t.Fatal("Synchronize failed")
	}
}

// setupTestDownload creates a server tester with an uploaded file of size
// `size` and name `name`.
func setupSPVTestDownload(t *testing.T, size int, name string, waitOnAvailability bool, hostTester *serverTester) (*serverTester, string) {
	st, err := createSPVServerTester(t.Name() + "SPV")
	if err != nil {
		t.Fatal(err)
	}

	// Announce the host and start accepting contracts.
	err = hostTester.announceHost()
	if err != nil {
		t.Fatal(err)
	}
	err = hostTester.acceptContracts()
	if err != nil {
		t.Fatal(err)
	}
	err = hostTester.setHostStorage()
	if err != nil {
		t.Fatal(err)
	}

	st.fundRenter(t, hostTester)

	// connect and wait till sync
	err = st.gateway.Connect(hostTester.gateway.Address())
	if err != nil {
		t.Fatal(err)
	}

	waitTillTesterSync(hostTester, st, t)

	// Set an allowance for the renter, allowing a contract to be formed.
	allowanceValues := url.Values{}
	testFunds := testFunds
	testPeriod := "10"
	renewWindow := "5"
	allowanceValues.Set("funds", testFunds)
	allowanceValues.Set("period", testPeriod)
	allowanceValues.Set("renewwindow", renewWindow)
	allowanceValues.Set("hosts", fmt.Sprint(recommendedHosts))
	err = st.stdPostAPI("/renter", allowanceValues)
	if err != nil {
		t.Fatal(err)
	}

	// Create a file.
	path := filepath.Join(build.SiaTestingDir, "api", t.Name(), name)
	err = createRandFile(path, size)
	if err != nil {
		t.Fatal(err)
	}

	// Upload to host.
	uploadValues := url.Values{}
	uploadValues.Set("source", path)
	uploadValues.Set("renew", "true")
	uploadValues.Set("datapieces", "1")
	uploadValues.Set("paritypieces", "1")
	err = st.stdPostAPI("/renter/upload/"+name, uploadValues)
	if err != nil {
		t.Fatal(err)
	}

	if waitOnAvailability {
		// wait for the file to become available
		err = build.Retry(200, time.Second, func() error {
			var rf RenterFiles
			st.getAPI("/renter/files", &rf)
			if len(rf.Files) != 1 || !rf.Files[0].Available {
				return fmt.Errorf("the uploading is not succeeding for some reason: %v\n", rf.Files[0])
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	return st, path
}

// TestRenterAsyncDownload tests that the /renter/downloadasync route works
// correctly.
func TestSPVRenterAsyncDownload(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	hostTester, err := createServerTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	st, _ := setupSPVTestDownload(t, 1e4, "test.dat", true, hostTester)
	defer st.server.panicClose()
	defer hostTester.server.panicClose()

	// Download the file asynchronously.
	downpath := filepath.Join(st.dir, "asyncdown.dat")
	err = st.getAPI("/renter/downloadasync/test.dat?destination="+downpath, nil)
	if err != nil {
		t.Fatal(err)
	}

	// download should eventually complete
	var rdq RenterDownloadQueue
	success := false
	for start := time.Now(); time.Since(start) < 30*time.Second; time.Sleep(time.Millisecond * 10) {
		err = st.getAPI("/renter/downloads", &rdq)
		if err != nil {
			t.Fatal(err)
		}
		for _, download := range rdq.Downloads {
			if download.Received == download.Filesize && download.SiaPath == "test.dat" {
				success = true
			}
		}
		if success {
			break
		}
	}
	if !success {
		t.Fatal("/renter/downloadasync did not download our test file")
	}
}
