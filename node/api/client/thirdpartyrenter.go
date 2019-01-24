package client

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/HyperspaceApp/Hyperspace/node/api"
)

// ThirdpartyRenterSetRepairPathPost uses the /thirdpartyrenter/tracking endpoint to set the repair
// path of a file to a new location. The file at newPath must exists.
func (c *Client) ThirdpartyRenterSetRepairPathPost(siaPath, newPath string) (err error) {
	values := url.Values{}
	values.Set("trackingpath", url.QueryEscape(newPath))
	err = c.post("/thirdpartyrenter/file/"+siaPath, values.Encode(), nil)
	return
}

// ThirdpartyRenterUploadPost uses the /thirdpartyrenter/upload endpoint to upload a file
func (c *Client) ThirdpartyRenterUploadPost(path, siaPath string, dataPieces, parityPieces uint64) (err error) {
	return c.RenterUploadForcePost(path, siaPath, dataPieces, parityPieces, false)
}

// ThirdpartyRenterUploadForcePost uses the /thirdpartyrenter/upload endpoint to upload a file
// and to overwrite if the file already exists
func (c *Client) ThirdpartyRenterUploadForcePost(path, siaPath string, dataPieces, parityPieces uint64, force bool) (err error) {
	siaPath = escapeHyperspacePath(trimHyperspacePath(siaPath))
	values := url.Values{}
	values.Set("source", path)
	values.Set("datapieces", strconv.FormatUint(dataPieces, 10))
	values.Set("paritypieces", strconv.FormatUint(parityPieces, 10))
	values.Set("force", strconv.FormatBool(force))
	err = c.post(fmt.Sprintf("/thirdpartyrenter/upload/%s", siaPath), values.Encode(), nil)
	return
}

// ThirdpartyRenterUploadDefaultPost uses the /thirdpartyrenter/upload endpoint with default
// redundancy settings to upload a file.
func (c *Client) ThirdpartyRenterUploadDefaultPost(path, siaPath string) (err error) {
	siaPath = escapeHyperspacePath(trimHyperspacePath(siaPath))
	values := url.Values{}
	values.Set("source", path)
	err = c.post(fmt.Sprintf("/thirdpartyrenter/upload/%s", siaPath), values.Encode(), nil)
	return
}

// ThirdpartyRenterDirCreatePost uses the /thirdpartyrenter/dir/ endpoint to create a directory for the
// renter
func (c *Client) ThirdpartyRenterDirCreatePost(siaPath string) (err error) {
	siaPath = strings.TrimPrefix(siaPath, "/")
	err = c.post(fmt.Sprintf("/thirdpartyrenter/dir/%s", siaPath), "action=create", nil)
	return
}

// ThirdpartyRenterDirDeletePost uses the /thirdpartyrenter/dir/ endpoint to delete a directory for the
// renter
func (c *Client) ThirdpartyRenterDirDeletePost(siaPath string) (err error) {
	siaPath = strings.TrimPrefix(siaPath, "/")
	err = c.post(fmt.Sprintf("/thirdpartyrenter/dir/%s", siaPath), "action=delete", nil)
	return
}

// ThirdpartyRenterDirRenamePost uses the /thirdpartyrenter/dir/ endpoint to rename a directory for the
// renter
func (c *Client) ThirdpartyRenterDirRenamePost(siaPath, newHyperspacePath string) (err error) {
	siaPath = strings.TrimPrefix(siaPath, "/")
	newHyperspacePath = strings.TrimPrefix(newHyperspacePath, "/")
	err = c.post(fmt.Sprintf("/thirdpartyrenter/dir/%s?newhyperspacepath=%s", siaPath, newHyperspacePath), "action=rename", nil)
	return
}

// ThirdpartyRenterGetDir uses the /thirdpartyrenter/dir/ endpoint to query a directory
func (c *Client) ThirdpartyRenterGetDir(siaPath string) (rd api.RenterDirectory, err error) {
	siaPath = strings.TrimPrefix(siaPath, "/")
	err = c.get(fmt.Sprintf("/thirdpartyrenter/dir/%s", siaPath), &rd)
	return
}
