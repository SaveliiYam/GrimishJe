package websocket

// ChatMessagePayload представляет сообщение чата.
type ChatMessagePayload struct {
	OrderID    uint   `json:"order_id"`
	Sender     string `json:"sender"`
	Message    string `json:"message"`
	CreatedAt  int64  `json:"created_at"`
	UploadedBy string `json:"uploaded_by,omitempty"`
}

// FileUpdatePayload представляет уведомление о файле.
type FileUpdatePayload struct {
	OrderID      uint   `json:"order_id"`
	Filename     string `json:"filename"`
	OriginalName string `json:"original_name"`
	UploadedBy   string `json:"uploaded_by"`
	URL          string `json:"url"`
}

// OrderStatusUpdatePayload представляет уведомление о статусе заказа или статусе исполнителя.
type OrderStatusUpdatePayload struct {
	Type    string `json:"type"` // Например, "executor_status" или "order_status_change"
	OrderID uint   `json:"order_id"`
	Status  string `json:"status"`
	At      int64  `json:"at"`
}

// FileDeletePayload представляет уведомление об удалении файла.
type FileDeletePayload struct {
	OrderID uint   `json:"order_id"`
	FileID  uint   `json:"file_id"`
	Type    string `json:"type"`
}
