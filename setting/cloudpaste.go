package setting

var (
	// CloudPasteEnabled 是否启用 CloudPaste 转存
	CloudPasteEnabled bool = false
	// CloudPasteBaseURL CloudPaste 服务地址（如 https://cp.example.com）
	CloudPasteBaseURL string = ""
	// CloudPasteAPIKey CloudPaste API Key
	CloudPasteAPIKey string = ""
	// CloudPasteStorageConfigID 存储配置 ID（可选）
	CloudPasteStorageConfigID string = ""
	// CloudPasteAutoTransfer 任务完成后是否自动转存
	CloudPasteAutoTransfer bool = true
)
