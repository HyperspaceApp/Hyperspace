Renter API
==========

This document contains detailed descriptions of the renter's API routes. For an
overview of the renter's API routes, see [API.md#renter](/doc/API.md#renter).  For
an overview of all API routes, see [API.md](/doc/API.md)

There may be functional API calls which are not documented. These are not
guaranteed to be supported beyond the current release, and should not be used
in production.

Overview
--------

The renter manages the user's files on the network. The renter's API endpoints
expose methods for managing files on the network and managing the renter's
allocated funds.

Index
-----

| Route                                                                                         | HTTP verb |
| --------------------------------------------------------------------------------------------- | --------- |
| [/renter](#renter-get)                                                                        | GET       |
| [/renter](#renter-post)                                                                       | POST      |
| [/renter/contract/cancel](#rentercontractcancel-post)                                         | POST      |
| [/renter/contracts](#rentercontracts-get)                                                     | GET       |
| [/renter/downloads](#renterdownloads-get)                                                     | GET       |
| [/renter/downloads/clear](#renterdownloadsclear-post)                                         | POST      |
| [/renter/files](#renterfiles-get)                                                             | GET       |
| [/renter/file/*___hyperspacepath___](#renterfilehyperspacepath-get)                           | GET       |
| [/renter/file/*__hyperspacepath__](#rentertrackinghyperspacepath-post)                        | POST      |
| [/renter/prices](#renter-prices-get)                                                          | GET       |
| [/renter/delete/___*hyperspacepath___](#renterdelete___hyperspacepath___-post)                | POST      |
| [/renter/download/___*hyperspacepath___](#renterdownload__hyperspacepath___-get)              | GET       |
| [/renter/downloadasync/___*hyperspacepath___](#renterdownloadasync__hyperspacepath___-get)    | GET       |
| [/renter/rename/___*hyperspacepath___](#renterrename___hyperspacepath___-post)                | POST      |
| [/renter/stream/___*hyperspacepath___](#renterstreamhyperspacepath-get)                       | GET       |
| [/renter/upload/___*hyperspacepath___](#renteruploadhyperspacepath-post)                      | POST      |

#### /renter [GET]

returns the current settings along with metrics on the renter's spending.

###### JSON Response
```javascript
{
  // Settings that control the behavior of the renter.
  "settings": {
    // Allowance dictates how much the renter is allowed to spend in a given
    // period. Note that funds are spent on both storage and bandwidth.
    "allowance": {
      // Amount of money allocated for contracts. Funds are spent on both
      // storage and bandwidth.
      "funds": "1234", // hastings

      // Number of hosts that contracts will be formed with.
      "hosts":24,

      // Duration of contracts formed, in number of blocks.
      "period": 6048, // blocks

      // If the current blockheight + the renew window >= the height the
      // contract is scheduled to end, the contract is renewed automatically.
      // Is always nonzero.
      "renewwindow": 3024 // blocks
    },
    // MaxUploadSpeed by default is unlimited but can be set by the user to
    // manage bandwidth
    "maxuploadspeed":     1234, // bytes per second

    // MaxDownloadSpeed by default is unlimited but can be set by the user to
    // manage bandwidth
    "maxdownloadspeed":   1234, // bytes per second

    // The StreamCacheSize is the number of data chunks that will be cached during
    // streaming
    "streamcachesize":  4
  },

  // Metrics about how much the Renter has spent on storage, uploads, and
  // downloads.
  "financialmetrics": {
    // Amount of money spent on contract fees and transaction fees.
    "contractfees": "1234", // hastings

    // How much money, in hastings, the Renter has spent on file contracts,
    // including fees.
    "contractspending": "1234", // hastings, (deprecated, now totalallocated)

    // Amount of money spent on downloads.
    "downloadspending": "5678", // hastings

    // Amount of money spend on storage.
    "storagespending": "1234", // hastings

    // Total amount of money that the renter has put into contracts. Includes
    // spent money and also money that will be returned to the renter.
    "totalallocated": "1234", // hastings

    // Amount of money spent on uploads.
    "uploadspending": "5678", // hastings

    // Amount of money in the allowance that has not been spent.
    "unspent": "1234" // hastings
  },
  // Height at which the current allowance period began.
  "currentperiod": 200
}
```

#### /renter [POST]

modify settings that control the renter's behavior.

###### Query String Parameters
```
// Enables or disables the check for hosts using the same ip subnets within the
hostdb. It's turned on by default and causes Sia to not form contracts with
hosts from the same subnet and if such contracts already exist, it will
deactivate the contract which has occupied that subnet for the shorter time.
checkforipviolation // true or false

// Number of hastings allocated for file contracts in the given period.
funds // hastings

// Number of hosts that contracts should be formed with. Files cannot be
// uploaded to more hosts than you have contracts with, and it's generally good
// to form a few more contracts than you need.
hosts

// Duration of contracts formed. Must be nonzero.
period // block height

// Renew window specifies how many blocks before the expiration of the current
// contracts the renter will wait before renewing the contracts. A smaller
// renew window means that Hyperspace must be run more frequently, but also means
// fewer total transaction fees. Storage spending is not affected by the renew
// window size.
renewwindow // block height

// Max download speed permitted, speed provide in bytes per second
maxdownloadspeed

// Max upload speed permitted, speed provide in bytes per second
maxuploadspeed

// Stream cache size specifies how many data chunks will be cached while
// streaming.
streamcachesize

// The next 4 values all relate to tuning the allowance. The usage pattern for
// the storage (e.g. download heavy, storage heavy, etc.) impacts which hosts are
// optimal. These values provide hints indicating what sort of usage pattern the
// user will be employing, allowing the software to pick more optimal selections
// of hosts.
//
// The expected storage in bytes the user expects to upload to the network.
// Shouldn't include redundancy.
expectedstorage

// The expected amount of data which will be uploaded through the API before
// redundancy in bytes per block.
expectedupload

// The expected amount of data which will be downloaded through the API in
// bytes per block.
expecteddownload

// Expected redundancy is the expected redundancy of the majority of the files
// the user is going to upload. If most files are going to be uploaded at a 3.0x
// redundancy, this should be set to 3.0.
expectedredundancy
```

###### Response
standard success or error response. See
[API.md#standard-responses](/doc/API.md#standard-responses).

#### /renter/contract/cancel [POST]

cancels a specific contract of the Renter.

###### Query String Parameter
```
// ID of the file contract
id
```

###### Response
standard success or error response. See
[API.md#standard-responses](/doc/API.md#standard-responses).

#### /renter/contracts [GET]

returns the renter's contracts.  Active contracts are contracts that the Renter
is currently using to store, upload, and download data, and are returned by
default. Inactive contracts are contracts that are in the current period but are
marked as not good for renew, these contracts have the potential to become
active again but currently are not storing data.  Expired contracts are
contracts not in the current period, where not more data is being stored and
excess funds have been released to the renter.

###### Contract Parameters
```
inactive   // true or false - Optional
expired    // true or false - Optional
```

###### JSON Response
```javascript
{
  "activecontracts": [
    {
      // Amount of contract funds that have been spent on downloads.
      "downloadspending": "1234", // hastings

      // Block height that the file contract ends on.
      "endheight": 50000, // block height

      // Fees paid in order to form the file contract.
      "fees": "1234", // hastings

      // Public key of the host the contract was formed with.
      "hostpublickey": {
        "algorithm": "ed25519",
        "key": "RW50cm9weSBpc24ndCB3aGF0IGl0IHVzZWQgdG8gYmU="
      },

      // ID of the file contract.
      "id": "1234567890abcdef0123456789abcdef0123456789abcdef0123456789abcdef",

      // A signed transaction containing the most recent contract revision.
      "lasttransaction": {},

      // Address of the host the file contract was formed with.
      "netaddress": "12.34.56.78:9",

      // Remaining funds left for the renter to spend on uploads & downloads.
      "renterfunds": "1234", // hastings

      // Size of the file contract, which is typically equal to the number of
      // bytes that have been uploaded to the host.
      "size": 8192, // bytes

      // Block height that the file contract began on.
      "startheight": 50000, // block height

      // DEPRECATED: This is the exact same value as StorageSpending, but it has
      // incorrect capitalization. This was fixed in 1.3.2, but this field is kept
      // to preserve backwards compatibility on clients who depend on the
      // incorrect capitalization. This field will be removed in the future, so
      // clients should switch to the StorageSpending field (above) with the
      // correct lowercase name.
      "StorageSpending": 0,

      // Amount of contract funds that have been spent on storage.
      "storagespending": "1234", // hastings

      // Total cost to the wallet of forming the file contract.
      // This includes both the fees and the funds allocated in the contract.
      "totalcost": "1234", // hastings

      // Amount of contract funds that have been spent on uploads.
      "uploadspending": "1234" // hastings

      // Signals if contract is good for uploading data
      "goodforupload": true,

      // Signals if contract is good for a renewal
      "goodforrenew": false,
    }
  ],
  "inactivecontracts": [],
  "expiredcontracts": [],
}
```

#### /renter/downloads [GET]

lists all files in the download queue.

###### JSON Response
```javascript
{
  "downloads": [
    {
      // Local path that the file will be downloaded to.
      "destination": "/home/users/alice",

      // What type of destination was used. Can be "file", indicating a download
      // to disk, can be "buffer", indicating a download to memory, and can be
      // "http stream", indicating that the download was streamed through the
      // http API.
      "destinationtype": "file",

      // Length of the download. If the download was a partial download, this
      // will indicate the length of the partial download, and not the length of
      // the full file.
      "length": 8192, // bytes

      // Offset within the file of the download. For full file downloads, the //
      offset will be '0'. For partial downloads, the offset may be anywhere //
      within the file. offset+length will never exceed the full file size.
      "offset": 0,

      // Hyperspacepath given to the file when it was uploaded.
      "hyperspacepath": "foo/bar.txt",

      // Whether or not the download has completed. Will be false initially, and
      // set to true immediately as the download has been fully written out to
      // the file, to the http stream, or to the in-memory buffer. Completed
      // will also be set to true if there is an error that causes the download to
      // fail.
      "completed": true,

      // Time at which the download completed. Will be zero if the download has
      // not yet completed.
      "endtime": "2009-11-10T23:00:00Z", // RFC 3339 time

      // Error encountered while downloading. If there was no error (yet), it
      // will be the empty string.
      "error": ""

      // Number of bytes downloaded thus far. Will only be updated as segments
      // of the file complete fully. This typically has a resolution of tens of
      // megabytes.
      "received": 4096, // bytes

      // Time at which the download was initiated.
      "starttime": "2009-11-10T23:00:00Z", // RFC 3339 time

      // The total amount of data transfered when downloading the file. This
      // will eventually include data transferred during contract + payment
      // negotiation, as well as data from failed piece downloads.
      "totaldatatransfered": 10321,
    }
  ]
}
```
#### /renter/downloads/clear [POST]

Clears the download history of the renter for a range of unix time stamps.  Both
parameters are optional, if no parameters are provided, the entire download
history will be cleared.  To clear a single download, provide the timestamp for
the download as both parameters.  Providing only the before parameter will clear
all downloads older than the timestamp.  Conversely, providing only the after
parameter will clear all downloads newer than the timestamp.

###### Timestamp Parameters [(with comments)]
```
before  // Optional - unix timestamp found in the download history
after   // Optional - unix timestamp found in the download history
```

###### Response
standard success or error response. See
[API.md#standard-responses](/doc/API.md#standard-responses).

#### /renter/files [GET]

lists the status of all files.

###### Query String Parameters
```
// Optional regular expression applied to 'hyperspacepath' that can be used to produce a filtered file list
filter      // Regex string syntax
```

###### JSON Response
```javascript
{
  "files": [
    {
      // Path to the file in the renter on the network.
      "hyperspacepath": "foo/bar.txt",

      // Path to the local file on disk.
      "localpath": "/home/foo/bar.txt",

      // Size of the file in bytes.
      "filesize": 8192, // bytes

      // true if the file is available for download. Files may be available
      // before they are completely uploaded.
      "available": true,

      // true if the file's contracts will be automatically renewed by the
      // renter.
      "renewing": true,

      // Average redundancy of the file on the network. Redundancy is
      // calculated by dividing the amount of data uploaded in the file's open
      // contracts by the size of the file. Redundancy does not necessarily
      // correspond to availability. Specifically, a redundancy >= 1 does not
      // indicate the file is available as there could be a chunk of the file
      // with 0 redundancy.
      "redundancy": 5,

      // Total number of bytes successfully uploaded via current file contracts.
      // This number includes padding and rendundancy, so a file with a size of
      // 8192 bytes might be padded to 40 MiB and, with a redundancy of 5,
      // encoded to 200 MiB for upload.
      "uploadedbytes": 209715200, // bytes

      // Percentage of the file uploaded, including redundancy. Uploading has
      // completed when uploadprogress is 100. Files may be available for
      // download before upload progress is 100.
      "uploadprogress": 100, // percent

      // Block height at which the file ceases availability.
      "expiration": 60000
    }
  ]
}
```

#### /renter/file/*___hyperspacepath___ [GET]

lists the status of specified file.

###### JSON Response
```javascript
{
  "file": {
    // Path to the file in the renter on the network.
    "hyperspacepath": "foo/bar.txt",

    // Path to the local file on disk.
    "localpath": "/home/foo/bar.txt",

    // Size of the file in bytes.
    "filesize": 8192, // bytes

    // true if the file is available for download. Files may be available
    // before they are completely uploaded.
    "available": true,

    // true if the file's contracts will be automatically renewed by the
    // renter.
    "renewing": true,

    // Average redundancy of the file on the network. Redundancy is
    // calculated by dividing the amount of data uploaded in the file's open
    // contracts by the size of the file. Redundancy does not necessarily
    // correspond to availability. Specifically, a redundancy >= 1 does not
    // indicate the file is available as there could be a chunk of the file
    // with 0 redundancy.
    "redundancy": 5,

    // Total number of bytes successfully uploaded via current file contracts.
    // This number includes padding and rendundancy, so a file with a size of
    // 8192 bytes might be padded to 40 MiB and, with a redundancy of 5,
    // encoded to 200 MiB for upload.
    "uploadedbytes": 209715200, // bytes

    // Percentage of the file uploaded, including redundancy. Uploading has
    // completed when uploadprogress is 100. Files may be available for
    // download before upload progress is 100.
    "uploadprogress": 100, // percent

    // Block height at which the file ceases availability.
    "expiration": 60000
  }
}
```

#### /renter/file/*___hyperspacepath___ [POST]

endpoint for changing file metadata.

###### Path Parameters [(with comments)](/doc/api/Renter.md#path-parameters-3)
```
// HyperspacePath of the file on the network. The path must be non-empty, may not
// include any path traversal strings ("./", "../"), and may not begin with a
// forward-slash character.
*hyperspacepath
```

###### Query String Parameters [(with comments)](/doc/api/Renter.md#query-string-parameters-3)
```
// If provided, this parameter changes the tracking path of a file to the
// specified path. Useful if moving the file to a different location on disk.
trackingpath
```

###### Response
standard success or error response. See
[#standard-responses](#standard-responses).

#### /renter/prices [GET]

lists the estimated prices of performing various storage and data operations. An
allowance can be submitted to provide a more personalized estimate. If no
allowance is submitted then the current set allowance will be used, if there is
no allowance set then sane defaults will be used. Submitting an allowance is
optional, but when submitting an allowance all the components of the allowance
are required. The allowance used to create the estimate is returned with the
estimate.

###### Query String Parameters 5
```
all optional or all required

// Number of hastings allocated for file contracts in the given period.
funds // hastings

// Number of hosts that contracts should be formed with. Files cannot be
// uploaded to more hosts than you have contracts with, and it's generally good
// to form a few more contracts than you need.
hosts

// Duration of contracts formed. Must be nonzero.
period // block height

// Renew window specifies how many blocks before the expiration of the current
// contracts the renter will wait before renewing the contracts. A smaller
// renew window means that Sia must be run more frequently, but also means
// fewer total transaction fees. Storage spending is not affected by the renew
// window size.
renewwindow // block height
```

###### JSON Response 5
```javascript
{
    // The estimated cost of downloading one terabyte of data from the
    // network.
    "downloadterabyte": "1234", // hastings

    // The estimated cost of forming a set of contracts on the network. This
    // cost also applies to the estimated cost of renewing the renter's set of
    // contracts.
    "formcontracts": "1234", // hastings

    // The estimated cost of storing one terabyte of data on the network for
    // a month, including accounting for redundancy.
    "storageterabytemonth": "1234", // hastings

    // The estimated cost of uploading one terabyte of data to the network,
    // including accounting for redundancy.
    "uploadterabyte": "1234", // hastings

    // Amount of money allocated for contracts. Funds are spent on both
    // storage and bandwidth.
    "funds": "1234", // hastings

    // Number of hosts that contracts will be formed with.
    "hosts":24,

    // Duration of contracts formed, in number of blocks.
    "period": 6048, // blocks

    // If the current blockheight + the renew window >= the height the
    // contract is scheduled to end, the contract is renewed automatically.
    // Is always nonzero.
    "renewwindow": 3024 // blocks
}
```

#### /renter/delete/___*hyperspacepath___ [POST]

deletes a renter file entry. Does not delete any downloads or original files,
only the entry in the renter.

###### Path Parameters
```
// Location of the file in the renter on the network.
*hyperspacepath
```

###### Response
standard success or error response. See
[API.md#standard-responses](/doc/API.md#standard-responses).

#### /renter/download/___*hyperspacepath___ [GET]

downloads a file to the local filesystem. The call will block until the file
has been downloaded.

###### Path Parameters
```
// Location of the file in the renter on the network.
*hyperspacepath
```

###### Query String Parameters
```
// If async is true, the http request will be non blocking. Can't be used with
async
// Location on disk that the file will be downloaded to.
destination
// If httresp is true, the data will be written to the http response.
httpresp
// Length of the requested data. Has to be <= filesize-offset.
length
// Offset relative to the file start from where the download starts.
offset
```

###### Response
standard success or error response. See
[API.md#standard-responses](/doc/API.md#standard-responses).

#### /renter/downloadasync/___*hyperspacepath___ [GET]

downloads a file to the local filesystem. The call will return immediately.

###### Path Parameters
```
*hyperspacepath
```

###### Query String Parameters
```
destination
```

###### Response
standard success or error response. See
[API.md#standard-responses](/doc/API.md#standard-responses).

#### /renter/rename/___*hyperspacepath___ [POST]

renames a file. Does not rename any downloads or source files, only renames the
entry in the renter. An error is returned if `hyperspacepath` does not exist or
`newhyperspacepath` already exists.

###### Path Parameters
```
// Current location of the file in the renter on the network.
*hyperspacepath
```

###### Query String Parameters
```
// New location of the file in the renter on the network.
newhyperspacepath
```

###### Response
standard success or error response. See
[API.md#standard-responses](/doc/API.md#standard-responses).

#### /renter/stream/*___hyperspacepath___ [GET]

downloads a file using http streaming. This call blocks until the data is
received.
The streaming endpoint also uses caching internally to prevent hsd from
re-downloading the same chunk multiple times when only parts of a file are
requested at once. This might lead to a substantial increase in ram usage and
therefore it is not recommended to stream multiple files in parallel at the
moment. This restriction will be removed together with the caching once partial
downloads are supported in the future. If you want to stream multiple files you
should increase the size of the Renter's `streamcachesize` to at least 2x the
number of files you are steaming.

###### Path Parameters [(with comments)](/doc/api/Renter.md#path-parameters-1)
```
*hyperspacepath
```

###### Response
standard success with the requested data in the body or error response. See
[#standard-responses](#standard-responses).

#### /renter/upload/___*hyperspacepath___ [POST]

starts a file upload to the Hyperspace network from the local filesystem.

###### Path Parameters

```
// Location where the file will reside in the renter on the network. The path
// must be non-empty, may not include any path traversal strings ("./", "../"),
// and may not begin with a forward-slash character.
*hyperspacepath

// Optional paramater used to overwrite an existing file
// Default is 'false' if unspecified
force // bool
```

###### Query String Parameters
```
// The number of data pieces to use when erasure coding the file.
datapieces // int

// The number of parity pieces to use when erasure coding the file. Total
// redundancy of the file is (datapieces+paritypieces)/datapieces.
paritypieces // int

// Location on disk of the file being uploaded.
source // string - a filepath

// Optional paramater used to overwrite an existing file
// Default is 'false' if unspecified
overwrite // bool
```

###### Response
standard success or error response. See
[API.md#standard-responses](/doc/API.md#standard-responses). A successful
response indicates that the upload started successfully. To confirm the upload
completed successfully, the caller must call [/renter/files](#renterfiles-get)
until that API returns success with an `uploadprogress` >= 100.0 for the file
at the given `hyperspacepath`.
