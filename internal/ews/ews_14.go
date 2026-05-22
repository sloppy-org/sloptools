package ews

import (
	"context"
	"fmt"
	"strings"
)

// CreateMailFolder creates a mail subfolder under the given parent and returns
// the new folder ID and change key. The parent accepts a raw EWS folder ID or
// one of the canonical distinguished folder names (e.g. "msgfolderroot"). When
// parent is empty the new folder is created at the top of the mailbox under
// msgfolderroot.
func (c *Client) CreateMailFolder(ctx context.Context, parentFolderID, displayName string) (folderID, changeKey string, err error) {
	clean := strings.TrimSpace(displayName)
	if clean == "" {
		return "", "", fmt.Errorf("ews CreateMailFolder: display name is required")
	}
	parent := strings.TrimSpace(parentFolderID)
	if parent == "" {
		parent = "msgfolderroot"
	}
	body := `<m:CreateFolder>` +
		`<m:ParentFolderId>` + folderIDXML(parent) + `</m:ParentFolderId>` +
		`<m:Folders><t:Folder><t:DisplayName>` + xmlEscapeText(clean) +
		`</t:DisplayName></t:Folder></m:Folders></m:CreateFolder>`
	var resp createMailFolderEnvelope
	if err := c.call(ctx, "CreateFolder", body, &resp); err != nil {
		return "", "", err
	}
	folders := resp.Body.CreateFolderResponse.ResponseMessages.Message.Folders.Folders
	if len(folders) == 0 {
		return "", "", fmt.Errorf("ews CreateMailFolder returned no folders")
	}
	return strings.TrimSpace(folders[0].FolderID.ID), strings.TrimSpace(folders[0].FolderID.ChangeKey), nil
}

// FindMailSubfolder looks up a direct child of the given parent by display
// name. Returns (nil, nil) when no such child exists. The parent accepts a raw
// EWS folder ID or a distinguished folder name.
func (c *Client) FindMailSubfolder(ctx context.Context, parentFolderID, displayName string) (*Folder, error) {
	target := strings.ToLower(strings.TrimSpace(displayName))
	if target == "" {
		return nil, nil
	}
	parent := strings.TrimSpace(parentFolderID)
	if parent == "" {
		parent = "msgfolderroot"
	}
	body := `<m:FindFolder Traversal="Shallow">` +
		`<m:FolderShape><t:BaseShape>Default</t:BaseShape></m:FolderShape>` +
		`<m:ParentFolderIds>` + folderIDXML(parent) + `</m:ParentFolderIds>` +
		`</m:FindFolder>`
	var resp findFolderEnvelope
	if err := c.call(ctx, "FindFolder", body, &resp); err != nil {
		return nil, err
	}
	for _, f := range resp.Body.FindFolderResponse.ResponseMessages.Message.Root.Folders.Items {
		if strings.EqualFold(strings.TrimSpace(f.DisplayName), target) {
			folder := f.toFolder()
			return &folder, nil
		}
	}
	return nil, nil
}

type createMailFolderEnvelope struct {
	Body struct {
		CreateFolderResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Folders      struct {
						Folders []folderXML `xml:"Folder"`
					} `xml:"Folders"`
				} `xml:"CreateFolderResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"CreateFolderResponse"`
	} `xml:"Body"`
}
