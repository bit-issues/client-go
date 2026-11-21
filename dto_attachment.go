package clientgo

type AttachmentResponse struct {
	ID          int64     `json:"id"`
	FileName    string    `json:"file_name"`
	SizeBytes   uint64    `json:"size_bytes"`
	UploadedBy  UserBrief `json:"uploaded_by"`
	UploadedAt  string    `json:"uploaded_at"`
	DownloadURL string    `json:"download_url"`
}
