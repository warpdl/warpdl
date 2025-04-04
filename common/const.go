package common

type UpdateType string

const (
	UPDATE_DOWNLOAD       UpdateType = "download"
	UPDATE_DOWNLOADING    UpdateType = "downloading"
	UPDATE_ATTACH         UpdateType = "attach"
	UPDATE_RESUME         UpdateType = "resume"
	UPDATE_FLUSH          UpdateType = "flush"
	UPDATE_STOP           UpdateType = "stop"
	UPDATE_LIST           UpdateType = "list"
	UPDATE_ADD_EXT        UpdateType = "add_extension"
	UPDATE_ACTIVATE_EXT   UpdateType = "activate_extension"
	UPDATE_DEACTIVATE_EXT UpdateType = "deactivate_extension"
	UPDATE_UNLOAD_EXT     UpdateType = "unload_extension"
	UPDATE_GET_EXT        UpdateType = "get_extension"
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
