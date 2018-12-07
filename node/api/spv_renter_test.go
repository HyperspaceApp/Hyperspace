package api

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter"
	"github.com/HyperspaceApp/errors"
)

func waitTillTesterSync(st1, st2 *serverTester, t *testing.T) {
	// blockchains should now match
	for i := 0; i < 50; i++ {
		if st1.cs.Height() != st2.cs.Height() {
			time.Sleep(250 * time.Millisecond)
		}
	}
	// log.Printf("st1 %d, st2 %d", st1.cs.Height(), st2.cs.Height())

	if st1.cs.Height() != st2.cs.Height() {
		t.Fatal("Synchronize failed")
	}
}

// setupTestDosetupSPVTestDownloadwnload creates 2 server tester a host and a spv renter
func setupSPVTestDownload(t *testing.T, size int, name string, waitOnAvailability bool) (*serverTester, *serverTester, string) {
	hostTester, err := createServerTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

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
	allowanceValues.Set("hosts", fmt.Sprint(modules.DefaultAllowance.Hosts))
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

	return st, hostTester, path
}

// runDownloadTest uploads a file and downloads it using the specified
// parameters, verifying that the parameters are applied correctly and the file
// is downloaded successfully.
func runSPVDownloadTest(t *testing.T, filesize, offset, length int64, useHttpResp bool, testName string) error {
	ulHyperspacePath := testName + ".dat"
	st, _, path := setupSPVTestDownload(t, int(filesize), ulHyperspacePath, true)
	defer func() {
		st.server.panicClose()
		os.Remove(path)
	}()

	// Read the section to be downloaded from the original file.
	uf, err := os.Open(path) // Uploaded file.
	if err != nil {
		return err
	}
	var originalBytes bytes.Buffer
	_, err = uf.Seek(offset, 0)
	if err != nil {
		return err
	}
	_, err = io.CopyN(&originalBytes, uf, length)
	if err != nil {
		return err
	}

	// Download the original file from the passed offsets.
	fname := testName + "-download.dat"
	downpath := filepath.Join(st.dir, fname)
	defer os.Remove(downpath)

	dlURL := fmt.Sprintf("/renter/download/%s?offset=%d&length=%d", ulHyperspacePath, offset, length)

	var downbytes bytes.Buffer

	if useHttpResp {
		dlURL += "&httpresp=true"
		// Make request.
		resp, err := HttpGET("http://" + st.server.listener.Addr().String() + dlURL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		_, err = io.Copy(&downbytes, resp.Body)
		if err != nil {
			return err
		}
	} else {
		dlURL += "&destination=" + downpath
		err := st.getAPI(dlURL, nil)
		if err != nil {
			return err
		}
		// wait for the download to complete
		err = build.Retry(30, time.Second, func() error {
			var rdq RenterDownloadQueue
			err = st.getAPI("/renter/downloads", &rdq)
			if err != nil {
				return err
			}
			for _, download := range rdq.Downloads {
				if download.Received == download.Filesize && download.HyperspacePath == ulHyperspacePath {
					return nil
				}
			}
			return errors.New("file not downloaded")
		})
		if err != nil {
			t.Fatal(err)
		}

		// open the downloaded file
		df, err := os.Open(downpath)
		if err != nil {
			return err
		}
		defer df.Close()

		_, err = io.Copy(&downbytes, df)
		if err != nil {
			return err
		}
	}

	// should have correct length
	if int64(downbytes.Len()) != length {
		return fmt.Errorf("downloaded file has incorrect size: %d, %d expected", downbytes.Len(), length)
	}

	// should be byte-for-byte equal to the original uploaded file
	if !bytes.Equal(originalBytes.Bytes(), downbytes.Bytes()) {
		return fmt.Errorf("downloaded content differs from original content")
	}

	return nil
}

func TestSPVRenterDownloadError(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	st, hostTester, _ := setupSPVTestDownload(t, 1e4, "test.dat", false)
	defer st.server.Close()
	defer hostTester.server.Close()

	// don't wait for the upload to complete, try to download immediately to
	// intentionally cause a download error
	downpath := filepath.Join(st.dir, "down.dat")
	expectedErr := st.getAPI("/renter/download/test.dat?destination="+downpath, nil)
	if expectedErr == nil {
		t.Fatal("download unexpectedly succeeded")
	}

	// verify the file has the expected error
	var rdq RenterDownloadQueue
	err := st.getAPI("/renter/downloads", &rdq)
	if err != nil {
		t.Fatal(err)
	}
	for _, download := range rdq.Downloads {
		if download.HyperspacePath == "test.dat" && download.Received == download.Filesize && download.Error == expectedErr.Error() {
			t.Fatal("download had unexpected error: ", download.Error)
		}
	}
}

// TestValidDownloads tests valid and boundary parameter combinations.
func TestSPVValidDownloads(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sectorSize := int64(modules.SectorSize)

	testParams := []struct {
		filesize,
		offset,
		length int64
		useHttpResp bool
		testName    string
	}{
		// file-backed tests.
		{sectorSize, 40, sectorSize - 40, false, "OffsetSingleChunk"},
		{sectorSize * 2, 20, sectorSize*2 - 20, false, "OffsetTwoChunk"},
		{int64(float64(sectorSize) * 2.4), 20, int64(float64(sectorSize)*2.4) - 20, false, "OffsetThreeChunk"},
		{sectorSize, 0, sectorSize / 2, false, "ShortLengthSingleChunk"},
		{sectorSize, sectorSize / 4, sectorSize / 2, false, "ShortLengthAndOffsetSingleChunk"},
		{sectorSize * 2, 0, int64(float64(sectorSize) * 2 * 0.75), false, "ShortLengthTwoChunk"},
		{int64(float64(sectorSize) * 2.7), 0, int64(2.2 * float64(sectorSize)), false, "ShortLengthThreeChunkInThirdChunk"},
		{int64(float64(sectorSize) * 2.7), 0, int64(1.6 * float64(sectorSize)), false, "ShortLengthThreeChunkInSecondChunk"},
		{sectorSize * 5, 0, int64(float64(sectorSize*5) * 0.75), false, "ShortLengthMultiChunk"},
		{sectorSize * 2, 50, int64(float64(sectorSize*2) * 0.75), false, "ShortLengthAndOffsetTwoChunk"},
		{sectorSize * 3, 50, int64(float64(sectorSize*3) * 0.5), false, "ShortLengthAndOffsetThreeChunkInSecondChunk"},
		{sectorSize * 3, 50, int64(float64(sectorSize*3) * 0.75), false, "ShortLengthAndOffsetThreeChunkInThirdChunk"},

		// http response tests.
		{sectorSize, 40, sectorSize - 40, true, "HttpRespOffsetSingleChunk"},
		{sectorSize * 2, 40, sectorSize*2 - 40, true, "HttpRespOffsetTwoChunk"},
		{sectorSize * 5, 40, sectorSize*5 - 40, true, "HttpRespOffsetManyChunks"},
		{sectorSize, 40, 4 * sectorSize / 5, true, "RespOffsetAndLengthSingleChunk"},
		{sectorSize * 2, 80, 3 * (sectorSize * 2) / 4, true, "RespOffsetAndLengthTwoChunk"},
		{sectorSize * 5, 150, 3 * (sectorSize * 5) / 4, true, "HttpRespOffsetAndLengthManyChunks"},
		{sectorSize * 5, 150, sectorSize * 5 / 4, true, "HttpRespOffsetAndLengthManyChunksSubsetOfChunks"},
	}
	for _, params := range testParams {
		t.Run(params.testName, func(st *testing.T) {
			st.Parallel()
			err := runSPVDownloadTest(st, params.filesize, params.offset, params.length, params.useHttpResp, params.testName)
			if err != nil {
				st.Fatal(err)
			}
		})
	}
}

func runSPVDownloadParamTest(t *testing.T, length, offset, filesize int) error {
	ulHyperspacePath := "test.dat"

	st, hostTester, _ := setupSPVTestDownload(t, int(filesize), ulHyperspacePath, true)
	defer st.server.Close()
	defer hostTester.server.Close()

	// Download the original file from offset 40 and length 10.
	fname := "offsetsinglechunk.dat"
	downpath := filepath.Join(st.dir, fname)
	dlURL := fmt.Sprintf("/renter/download/%s?destination=%s", ulHyperspacePath, downpath)
	dlURL += fmt.Sprintf("&length=%d", length)
	dlURL += fmt.Sprintf("&offset=%d", offset)
	return st.getAPI(dlURL, nil)
}

func TestSPVInvalidDownloadParameters(t *testing.T) {
	if testing.Short() || !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	testParams := []struct {
		length   int
		offset   int
		filesize int
		errorMsg string
	}{
		{0, -10, 1e4, "/download not prompting error when passing negative offset."},
		{0, 1e4, 1e4, "/download not prompting error when passing offset equal to filesize."},
		{1e4 + 1, 0, 1e4, "/download not prompting error when passing length exceeding filesize."},
		{1e4 + 11, 10, 1e4, "/download not prompting error when passing length exceeding filesize with non-zero offset."},
		{-1, 0, 1e4, "/download not prompting error when passing negative length."},
	}

	for _, params := range testParams {
		err := runDownloadParamTest(t, params.length, params.offset, params.filesize)
		if err == nil {
			t.Fatal(params.errorMsg)
		}
	}
}

func TestSPVRenterDownloadAsyncAndHttpRespError(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	filesize := 1e4
	ulHyperspacePath := "test.dat"

	st, hostTester, _ := setupSPVTestDownload(t, int(filesize), ulHyperspacePath, true)
	defer st.server.Close()
	defer hostTester.server.Close()

	// Download the original file from offset 40 and length 10.
	fname := "offsetsinglechunk.dat"
	dlURL := fmt.Sprintf("/renter/download/%s?destination=%s&async=true&httpresp=true", ulHyperspacePath, fname)
	err := st.getAPI(dlURL, nil)
	if err == nil {
		t.Fatalf("/download not prompting error when only passing both async and httpresp fields.")
	}
}

func TestSPVRenterDownloadAsyncNonexistentFile(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	st, err := createSPVServerTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer st.server.Close()

	downpath := filepath.Join(st.dir, "testfile")
	err = st.getAPI(fmt.Sprintf("/renter/downloadasync/doesntexist?destination=%v", downpath), nil)
	if err == nil || err.Error() != fmt.Sprintf("download failed: no file with that path: doesntexist") {
		t.Fatal("downloadasync did not return error on nonexistent file")
	}
}

func TestSPVRenterDownloadAsyncAndNotDestinationError(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	filesize := 1e4
	ulHyperspacePath := "test.dat"

	st, hostTester, _ := setupSPVTestDownload(t, int(filesize), ulHyperspacePath, true)
	defer st.server.Close()
	defer hostTester.server.Close()

	// Download the original file from offset 40 and length 10.
	dlURL := fmt.Sprintf("/renter/download/%s?async=true", ulHyperspacePath)
	err := st.getAPI(dlURL, nil)
	if err == nil {
		t.Fatal("/download not prompting error when async is specified but destination is empty.")
	}
}

func TestSPVRenterDownloadHttpRespAndDestinationError(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	filesize := 1e4
	ulHyperspacePath := "test.dat"

	st, hostTester, _ := setupSPVTestDownload(t, int(filesize), ulHyperspacePath, true)
	defer st.server.Close()
	defer hostTester.server.Close()

	// Download the original file from offset 40 and length 10.
	fname := "test.dat"
	dlURL := fmt.Sprintf("/renter/download/%s?destination=%shttpresp=true", ulHyperspacePath, fname)
	err := st.getAPI(dlURL, nil)
	if err == nil {
		t.Fatal("/download not prompting error when httpresp is specified and destination is non-empty.")
	}
}

func TestSPVRenterAsyncDownloadError(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	st, hostTester, _ := setupSPVTestDownload(t, 1e4, "test.dat", false)
	defer st.server.panicClose()
	defer hostTester.server.panicClose()

	// don't wait for the upload to complete, try to download immediately to
	// intentionally cause a download error
	downpath := filepath.Join(st.dir, "asyncdown.dat")
	st.getAPI("/renter/downloadasync/test.dat?destination="+downpath, nil)

	// verify the file has an error
	var rdq RenterDownloadQueue
	err := st.getAPI("/renter/downloads", &rdq)
	if err != nil {
		t.Fatal(err)
	}
	for _, download := range rdq.Downloads {
		if download.HyperspacePath == "test.dat" && download.Received == download.Filesize && download.Error == "" {
			t.Fatal("download had nil error")
		}
	}
}

// TestRenterAsyncDownload tests that the /renter/downloadasync route works
// correctly.
func TestSPVRenterAsyncDownload(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	var err error

	st, hostTester, _ := setupSPVTestDownload(t, 1e4, "test.dat", true)
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
			if download.Received == download.Filesize && download.HyperspacePath == "test.dat" {
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

// skip some spv irrelevant test

func TestSPVRenterHandlerDelete(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	var err error
	st, hostTester, _ := setupSPVTestDownload(t, 1e4, "test.dat", true)
	defer st.server.panicClose()
	defer hostTester.server.panicClose()

	// Delete the file.
	if err = st.stdPostAPI("/renter/delete/test.dat", url.Values{}); err != nil {
		t.Fatal(err)
	}

	// The renter's list of files should now be empty.
	var files RenterFiles
	if err = st.getAPI("/renter/files", &files); err != nil {
		t.Fatal(err)
	}
	if len(files.Files) != 0 {
		t.Fatalf("renter's list of files should be empty; got %v instead", files)
	}

	// Try deleting a nonexistent file.
	err = st.stdPostAPI("/renter/delete/dne", url.Values{})
	if err == nil || err.Error() != renter.ErrUnknownPath.Error() {
		t.Errorf("expected error to be %v, got %v", renter.ErrUnknownPath, err)
	}
}

func TestSPVRenterPricesHandler(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hostTester, err := createServerTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	st, err := createSPVServerTester(t.Name() + "SPV")
	if err != nil {
		t.Fatal(err)
	}
	defer st.server.panicClose()
	defer hostTester.server.panicClose()

	// connect and wait till sync
	err = st.gateway.Connect(hostTester.gateway.Address())
	if err != nil {
		t.Fatal(err)
	}

	waitTillTesterSync(hostTester, st, t)

	// Announce the host and then get the calculated prices for when there is a
	// single host.
	var rpeSingle modules.RenterPriceEstimation
	if err = hostTester.announceHost(); err != nil {
		t.Fatal(err)
	}
	if err = st.getAPI("/renter/prices", &rpeSingle); err != nil {
		t.Fatal(err)
	}

	// Create several more hosts all using the default settings.
	stHost1, err := blankServerTester(t.Name() + " - Host 1")
	if err != nil {
		t.Fatal(err)
	}
	defer stHost1.panicClose()
	stHost2, err := blankServerTester(t.Name() + " - Host 2")
	if err != nil {
		t.Fatal(err)
	}
	defer stHost2.panicClose()

	// Connect all the nodes and announce all of the hosts.
	sts := []*serverTester{hostTester, stHost1, stHost2}
	err = fullyConnectNodes(sts)
	if err != nil {
		t.Fatal(err)
	}
	err = fundAllNodes(sts)
	if err != nil {
		t.Fatal(err)
	}
	err = announceAllHosts(sts)
	if err != nil {
		t.Fatal(err)
	}

	// Grab the price estimates for when there are a bunch of hosts with the
	// same stats.
	var rpeMulti modules.RenterPriceEstimation
	if err = st.getAPI("/renter/prices", &rpeMulti); err != nil {
		t.Fatal(err)
	}

	// Verify that the aggregate is the same.
	if !rpeMulti.DownloadTerabyte.Equals(rpeSingle.DownloadTerabyte) {
		t.Log(rpeMulti.DownloadTerabyte)
		t.Log(rpeSingle.DownloadTerabyte)
		t.Error("price changed from single to multi")
	}
	if !rpeMulti.FormContracts.Equals(rpeSingle.FormContracts) {
		t.Error("price changed from single to multi")
	}
	if !rpeMulti.StorageTerabyteMonth.Equals(rpeSingle.StorageTerabyteMonth) {
		t.Error("price changed from single to multi")
	}
	if !rpeMulti.UploadTerabyte.Equals(rpeSingle.UploadTerabyte) {
		t.Error("price changed from single to multi")
	}
}
