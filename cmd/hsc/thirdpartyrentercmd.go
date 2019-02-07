package main

// TODO: If you run siac from a non-existent directory, the abs() function does
// not handle this very gracefully.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/HyperspaceApp/Hyperspace/node/api"
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

	thirdpartyRenterFilesListCmd = &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the status of all files",
		Long:    "List the status of all files known to the renter on the Sia network.",
		Run:     wrap(thirdpartyrenterfileslistcmd),
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
	upload, list, download.`)
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

// thirdpartyrenterfileslistcmd is the handler for the command `hsc thirdpartyrenter list`.
func thirdpartyrenterfileslistcmd() {
	var rf api.RenterFiles
	rf, err := httpClient.ThirdpartyRenterFilesGet()
	if err != nil {
		die("Could not get file list:", err)
	}
	if len(rf.Files) == 0 {
		fmt.Println("No files have been uploaded.")
		return
	}
	fmt.Print("\nTracking ", len(rf.Files), " files:")
	var totalStored uint64
	for _, file := range rf.Files {
		totalStored += file.Filesize
	}
	fmt.Printf(" %9s\n", filesizeUnits(int64(totalStored)))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if renterListVerbose {
		fmt.Fprintln(w, "  File size\tAvailable\tUploaded\tProgress\tRedundancy\tRenewing\tOn Disk\tRecoverable\tSia path")
	}
	sort.Sort(byHyperspacePath(rf.Files))
	for _, file := range rf.Files {
		fmt.Fprintf(w, "  %9s", filesizeUnits(int64(file.Filesize)))
		if renterListVerbose {
			availableStr := yesNo(file.Available)
			renewingStr := yesNo(file.Renewing)
			redundancyStr := fmt.Sprintf("%.2f", file.Redundancy)
			if file.Redundancy == -1 {
				redundancyStr = "-"
			}
			uploadProgressStr := fmt.Sprintf("%.2f%%", file.UploadProgress)
			if file.UploadProgress == -1 {
				uploadProgressStr = "-"
			}
			onDiskStr := yesNo(file.OnDisk)
			recoverableStr := yesNo(file.Recoverable)
			fmt.Fprintf(w, "\t%s\t%9s\t%8s\t%10s\t%s\t%s\t%s", availableStr, filesizeUnits(int64(file.UploadedBytes)), uploadProgressStr, redundancyStr, renewingStr, onDiskStr, recoverableStr)
		}
		fmt.Fprintf(w, "\t%s", file.HyperspacePath)
		if !renterListVerbose && !file.Available {
			fmt.Fprintf(w, " (uploading, %0.2f%%)", file.UploadProgress)
		}
		fmt.Fprintln(w, "")
	}
	w.Flush()
}
