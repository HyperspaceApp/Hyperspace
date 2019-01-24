package main

// TODO: If you run siac from a non-existent directory, the abs() function does
// not handle this very gracefully.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	thirdpartyRenterCmd = &cobra.Command{
		Use:   "thirdpartyrenter",
		Short: "Perform thirdpartyrenter actions",
		Long:  "upload, download",
		Run:   wrap(thirdpartyrentercmd),
	}

	thirdpartyRenterFilesUploadCmd = &cobra.Command{
		Use:   "upload [source] [path]",
		Short: "Upload a file or folder",
		Long:  "Upload a file or folder to [path] on the Sia network.",
		Run:   wrap(thirdpartyrenterfilesuploadcmd),
	}

	// renterUploadsCmd = &cobra.Command{
	// 	Use:   "uploads",
	// 	Short: "View the upload queue",
	// 	Long:  "View the list of files currently uploading.",
	// 	Run:   wrap(renteruploadscmd),
	// }
)

// rentercmd displays the renter's financial metrics and lists the files it is
// tracking.
func thirdpartyrentercmd() {
	fmt.Printf(`ThirdpartyRenter commands:
	upload, download, uploads, downloads.`)
}

// thirdpartyrenterfilesuploadcmd is the handler for the command `hsc renter upload
// [source] [path]`. Uploads the [source] file to [path] on the Sia network.
// If [source] is a directory, all files inside it will be uploaded and named
// relative to [path].
func thirdpartyrenterfilesuploadcmd(source, path string) {
	stat, err := os.Stat(source)
	if err != nil {
		die("Could not stat file or folder:", err)
	}

	if stat.IsDir() {
		// folder
		var files []string
		err := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				fmt.Println("Warning: skipping file:", err)
				return nil
			}
			if info.IsDir() {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			die("Could not read folder:", err)
		} else if len(files) == 0 {
			die("Nothing to upload.")
		}
		failed := 0
		for _, file := range files {
			fpath, _ := filepath.Rel(source, file)
			fpath = filepath.Join(path, fpath)
			fpath = filepath.ToSlash(fpath)
			err = httpClient.ThirdpartyRenterUploadDefaultPost(abs(file), fpath)
			if err != nil {
				failed++
				fmt.Printf("Could not upload file %s :%v\n", file, err)
			}
		}
		fmt.Printf("\nUploaded %d of %d files into '%s'.\n", len(files)-failed, len(files), path)
	} else {
		// single file
		err = httpClient.ThirdpartyRenterUploadDefaultPost(abs(source), path)
		if err != nil {
			die("Could not upload file:", err)
		}
		fmt.Printf("Uploaded '%s' as '%s'.\n", abs(source), path)
	}
}
