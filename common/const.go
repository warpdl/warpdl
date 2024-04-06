package common

const (
	UPDATE_DOWNLOAD    = "download"
	UPDATE_DOWNLOADING = "downloading"
	UPDATE_ATTACH      = "attach"
	UPDATE_RESUME      = "resume"
	UPDATE_FLUSH       = "flush"
	UPDATE_STOP        = "stop"
	UPDATE_LIST        = "list"
	UPDATE_LOAD_EXT    = "load_extension"
	UPDATE_UNLOAD_EXT  = "unload_extension"
	UPDATE_GET_EXT     = "get_extension"
)

type DownloadingAction string

const (
	ResumeProgress   DownloadingAction = "resume_progress"
	DownloadProgress DownloadingAction = "download_progress"
	DownloadComplete DownloadingAction = "download_complete"
	DownloadStopped  DownloadingAction = "download_stopped"
	CompileStart     DownloadingAction = "compile_start"
	CompileProgress  DownloadingAction = "compile_progress"
	CompileComplete  DownloadingAction = "compile_complete"
)
