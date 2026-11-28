package clientgo

type AttachmentResponse struct {
	ID          int64     `json:"id"`
	FileName    string    `json:"file_name"`
	SizeBytes   uint64    `json:"size_bytes"`
	UploadedBy  UserBrief `json:"uploaded_by"`
	UploadedAt  string    `json:"uploaded_at"`
	DownloadURL string    `json:"download_url"`
}

type AttachmentUploadRequest struct {
	FileName  string `json:"file_name"  validate:"required"`
	SizeBytes uint64 `json:"size_bytes" validate:"required,min=1"`
}

type AttachmentUploadResponse struct {
	ID        int64  `json:"id"`
	FileName  string `json:"file_name"`
	SizeBytes uint64 `json:"size_bytes"`
	UploadURL string `json:"upload_url"`
}

type AttachmentDownloadResponse struct {
	DownloadURL string `json:"download_url"`
}
