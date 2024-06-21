package cmd

import "time"

const (
	DEF_MAX_PARTS = 200
	DEF_MAX_CONNS = 24
	DEF_TIMEOUT   = time.Second * 30
)

const DESCRIPTION = `
WarpDL is a powerful and versatile cross-platform download manager. 
With its advanced technology, it has the ability to accelerate
your download speeds by up to 10 times, revolutionizing the way
you obtain files on any operating system.
`

const (
	ListDescription = `The list command displays a list of incomplete 
downloads along with their unique download hashes
which can be used to resume pending downloads.

Example:
        warpdl list

`
	InfoDescription = `The info command makes a GET request to the entered 
url and and tries to fetch the basic file info like 
name, size etc.

Example:
        warpdl info https://domain.com/file.zip

`
	DownloadDescription = `The download command lets you quickly fetch and save 
files from the internet. You can initiate the download
process and securely store the desired file on your 
local system.

Warp uses dynamic file segmentation technique by default
to download files fastly by utilizing the full alloted 
bandwidth 

Example:
        warpdl https://domain.com/file.zip
					OR
        warpdl download https://domain.com/file.zip

`
	ResumeDescription = `The resume command lets you resume an incomplete download
using its unique download hash which you can retrieve by 
using "warpdl list" command.

Example:
        warpdl resume <unique download hash>

`
	FlushDescription = `The flush command deletes download history for the current
user, it will also delete incomplete downloads and their date.

Example:
        warpdl flush

`
)
