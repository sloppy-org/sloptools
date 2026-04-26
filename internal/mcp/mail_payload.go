package mcp

import (
	"time"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

func mailMessageListPayloads(messages []*providerdata.EmailMessage, includeBody bool) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		payload := map[string]interface{}{
			"id":                  message.ID,
			"thread_id":           message.ThreadID,
			"internet_message_id": message.InternetMessageID,
			"subject":             message.Subject,
			"sender":              message.Sender,
			"recipients":          message.Recipients,
			"date":                message.Date.Format(time.RFC3339),
			"snippet":             message.Snippet,
			"labels":              message.Labels,
			"is_read":             message.IsRead,
			"is_flagged":          message.IsFlagged,
			"attachments":         message.Attachments,
		}
		if includeBody {
			if message.BodyText != nil {
				payload["body_text"] = *message.BodyText
			}
			if message.BodyHTML != nil {
				payload["body_html"] = *message.BodyHTML
			}
		}
		out = append(out, payload)
	}
	return out
}
