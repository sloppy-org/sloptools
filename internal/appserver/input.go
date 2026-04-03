package appserver

import "strings"

type TurnInputItem struct {
	Type         string
	Text         string
	ImageURL     string
	TextElements []interface{}
}

func DefaultTurnInput(prompt string) []map[string]interface{} {
	return BuildTurnInput([]TurnInputItem{{
		Type:         "text",
		Text:         prompt,
		TextElements: []interface{}{},
	}})
}

func BuildTurnInput(items []TurnInputItem) []map[string]interface{} {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "", "text":
			text := strings.TrimSpace(item.Text)
			if text == "" {
				continue
			}
			textElements := item.TextElements
			if textElements == nil {
				textElements = []interface{}{}
			}
			out = append(out, map[string]interface{}{
				"type":          "text",
				"text":          text,
				"text_elements": textElements,
			})
		case "image_url":
			imageURL := strings.TrimSpace(item.ImageURL)
			if imageURL == "" {
				continue
			}
			out = append(out, map[string]interface{}{
				"type":      "image_url",
				"image_url": imageURL,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
