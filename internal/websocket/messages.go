package websocket

// ChatMessagePayload структура для передачи сообщений чата.
type ChatMessagePayload struct {
	OrderID    uint   `json:"order_id"`
	Sender     string `json:"sender"`
	Message    string `json:"message"`
	CreatedAt  int64  `json:"created_at"`
	UploadedBy string `json:"uploaded_by,omitempty"`
}

// FileUpdatePayload структура для уведомлений о новых файлах.
type FileUpdatePayload struct {
	OrderID      uint   `json:"order_id"`
	Filename     string `json:"filename"`
	OriginalName string `json:"original_name"`
	UploadedBy   string `json:"uploaded_by"`
	URL          string `json:"url"`
}

// OrderStatusUpdatePayload структура для уведомлений о статусе заказа.
type OrderStatusUpdatePayload struct {
	OrderID uint   `json:"order_id"`
	Status  string `json:"status"`
	At      int64  `json:"at"`
}
