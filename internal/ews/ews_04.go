package ews

import (
	"encoding/xml"
	"strconv"
	"strings"
)

func parseInt64(value string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return n
}

func mailboxSlice(values []mailboxXML) []Mailbox {
	out := make([]Mailbox, 0, len(values))
	for _, value := range values {
		out = append(out, value.toMailbox())
	}
	return out
}

func responseCode(target any) string {
	if coder, ok := target.(interface{ responseCode() string }); ok {
		return coder.responseCode()
	}
	switch typed := target.(type) {
	case *findFolderEnvelope:
		return typed.Body.FindFolderResponse.ResponseMessages.Message.ResponseCode
	case *findItemEnvelope:
		return typed.Body.FindItemResponse.ResponseMessages.Message.ResponseCode
	case *getItemEnvelope:
		return typed.Body.GetItemResponse.ResponseMessages.Message.ResponseCode
	case *getInboxRulesEnvelope:
		return typed.Body.GetInboxRulesResponse.ResponseCode
	case *simpleResponseEnvelope:
		return typed.Body.Response.Messages.ResponseCode
	case *updateItemEnvelope:
		return typed.Body.Response.Messages.FirstCode()
	case *subscribeEnvelope:
		return typed.Body.SubscribeResponse.ResponseMessages.Message.ResponseCode
	case *getStreamingEventsEnvelope:
		return typed.Body.GetStreamingEventsResponse.ResponseMessages.Message.ResponseCode
	case *createItemEnvelope:
		return typed.Body.CreateItemResponse.ResponseMessages.Message.ResponseCode
	case *createAttachmentEnvelope:
		return typed.Body.CreateAttachmentResponse.ResponseMessages.Message.ResponseCode
	case *moveItemEnvelope:
		return typed.Body.MoveItemResponse.ResponseMessages.FirstCode()
	case *updateInboxRulesEnvelope:
		return typed.Body.UpdateInboxRulesResponse.ResponseCode
	default:
		return contactEnvelopeResponseCode(target)
	}
}

type simpleResponseEnvelope struct {
	Body struct {
		Response struct {
			Messages struct {
				ResponseCode string `xml:"ResponseCode"`
			} `xml:",any"`
		} `xml:"Body"`
	} `xml:"Body"`
}

type updateItemEnvelope struct {
	Body struct {
		Response struct {
			Messages updateResponseMessages `xml:"UpdateItemResponse>ResponseMessages"`
		} `xml:"Body"`
	} `xml:"Body"`
}

type createItemEnvelope struct {
	Body struct {
		CreateItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Items        struct {
						Values []itemXML `xml:",any"`
					} `xml:"Items"`
				} `xml:"CreateItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"CreateItemResponse"`
	} `xml:"Body"`
}

type createAttachmentEnvelope struct {
	Body struct {
		CreateAttachmentResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Attachments  struct {
						Files []createAttachmentFile `xml:"FileAttachment"`
					} `xml:"Attachments"`
				} `xml:"CreateAttachmentResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"CreateAttachmentResponse"`
	} `xml:"Body"`
}

type createAttachmentFile struct {
	AttachmentID struct {
		ID                string `xml:"Id,attr"`
		RootItemID        string `xml:"RootItemId,attr"`
		RootItemChangeKey string `xml:"RootItemChangeKey,attr"`
	} `xml:"AttachmentId"`
	Name string `xml:"Name"`
}

type moveItemEnvelope struct {
	Body struct {
		MoveItemResponse struct {
			ResponseMessages moveItemResponseMessages `xml:"ResponseMessages"`
		} `xml:"MoveItemResponse"`
	} `xml:"Body"`
}

func (e *moveItemEnvelope) ResolvedItemIDs() []string {
	if e == nil {
		return nil
	}
	return e.Body.MoveItemResponse.ResponseMessages.ResolvedItemIDs()
}

type moveItemResponseMessages struct {
	Items []struct {
		ResponseCode string `xml:"ResponseCode"`
		Items        struct {
			Values []itemXML `xml:",any"`
		} `xml:"Items"`
	} `xml:",any"`
}

func (m moveItemResponseMessages) FirstCode() string {
	for _, item := range m.Items {
		if clean := strings.TrimSpace(item.ResponseCode); clean != "" {
			return clean
		}
	}
	return ""
}

func (m moveItemResponseMessages) ResolvedItemIDs() []string {
	var ids []string
	for _, item := range m.Items {
		for _, value := range item.Items.Values {
			if id := strings.TrimSpace(value.ItemID.ID); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

type updateResponseMessages struct {
	Items []struct {
		ResponseCode string `xml:"ResponseCode"`
	} `xml:",any"`
}

func (m updateResponseMessages) FirstCode() string {
	for _, item := range m.Items {
		if clean := strings.TrimSpace(item.ResponseCode); clean != "" {
			return clean
		}
	}
	return ""
}

type folderIDXMLNode struct {
	ID        string `xml:"Id,attr"`
	ChangeKey string `xml:"ChangeKey,attr"`
}

type mailboxXML struct {
	Name        string `xml:"Name"`
	Email       string `xml:"EmailAddress"`
	RoutingType string `xml:"RoutingType"`
	MailboxType string `xml:"MailboxType"`
}

func (m mailboxXML) toMailbox() Mailbox {
	return Mailbox{Name: strings.TrimSpace(m.Name), Email: strings.TrimSpace(m.Email), RoutingType: strings.TrimSpace(m.RoutingType), MailboxType: strings.TrimSpace(m.MailboxType)}
}

type findFolderEnvelope struct {
	Body struct {
		FindFolderResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Root         struct {
						Folders struct {
							Items []folderXML `xml:",any"`
						} `xml:"Folders"`
					} `xml:"RootFolder"`
				} `xml:"FindFolderResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"FindFolderResponse"`
	} `xml:"Body"`
}

type folderXML struct {
	XMLName          xml.Name        `xml:""`
	FolderID         folderIDXMLNode `xml:"FolderId"`
	DisplayName      string          `xml:"DisplayName"`
	TotalCount       string          `xml:"TotalCount"`
	ChildFolderCount string          `xml:"ChildFolderCount"`
	UnreadCount      string          `xml:"UnreadCount"`
}

func (f folderXML) toFolder() Folder {
	kind := FolderKindGeneric
	switch strings.ToLower(strings.TrimSpace(f.XMLName.Local)) {
	case "calendarfolder":
		kind = FolderKindCalendar
	case "contactsfolder":
		kind = FolderKindContacts
	case "tasksfolder":
		kind = FolderKindTasks
	}
	return Folder{ID: strings.TrimSpace(f.FolderID.ID), ChangeKey: strings.TrimSpace(f.FolderID.ChangeKey), Name: strings.TrimSpace(f.DisplayName), Kind: kind, TotalCount: parseInt(f.TotalCount), ChildFolderCount: parseInt(f.ChildFolderCount), UnreadCount: parseInt(f.UnreadCount)}
}

type findItemEnvelope struct {
	Body struct {
		FindItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Root         struct {
						IndexedPagingOffset     int  `xml:"IndexedPagingOffset,attr"`
						TotalItemsInView        int  `xml:"TotalItemsInView,attr"`
						IncludesLastItemInRange bool `xml:"IncludesLastItemInRange,attr"`
						Items                   struct {
							Items []itemIDOnlyXML `xml:",any"`
						} `xml:"Items"`
					} `xml:"RootFolder"`
				} `xml:"FindItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"FindItemResponse"`
	} `xml:"Body"`
}

type itemIDOnlyXML struct {
	XMLName xml.Name `xml:""`
	ItemID  struct {
		ID string `xml:"Id,attr"`
	} `xml:"ItemId"`
}

type getItemEnvelope struct {
	Body struct {
		GetItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Items        struct {
						Values []itemXML `xml:",any"`
					} `xml:"Items"`
				} `xml:"GetItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"GetItemResponse"`
	} `xml:"Body"`
}

type getAttachmentEnvelope struct {
	Body struct {
		GetAttachmentResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Attachments  struct {
						Files []attachmentContentXML `xml:"FileAttachment"`
					} `xml:"Attachments"`
				} `xml:"GetAttachmentResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"GetAttachmentResponse"`
	} `xml:"Body"`
}

type syncFolderItemsEnvelope struct {
	Body struct {
		SyncFolderItemsResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode            string `xml:"ResponseCode"`
					SyncState               string `xml:"SyncState"`
					IncludesLastItemInRange bool   `xml:"IncludesLastItemInRange"`
					Changes                 struct {
						Values []syncChangeXML `xml:",any"`
					} `xml:"Changes"`
				} `xml:"SyncFolderItemsResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"SyncFolderItemsResponse"`
	} `xml:"Body"`
}

type syncChangeXML struct {
	XMLName xml.Name `xml:""`
	Message struct {
		ItemID folderIDXMLNode `xml:"ItemId"`
	} `xml:"Message"`
	ItemID folderIDXMLNode `xml:"ItemId"`
}

func (c syncChangeXML) ResolveItemID() string {
	if clean := strings.TrimSpace(c.Message.ItemID.ID); clean != "" {
		return clean
	}
	return strings.TrimSpace(c.ItemID.ID)
}

type getInboxRulesEnvelope struct {
	Body struct {
		GetInboxRulesResponse struct {
			ResponseCode string    `xml:"ResponseCode"`
			Rules        []ruleXML `xml:"InboxRules>Rule"`
		} `xml:"GetInboxRulesResponse"`
	} `xml:"Body"`
}

type updateInboxRulesEnvelope struct {
	Body struct {
		UpdateInboxRulesResponse struct {
			ResponseCode string `xml:"ResponseCode"`
		} `xml:"UpdateInboxRulesResponse"`
	} `xml:"Body"`
}

type subscribeEnvelope struct {
	Body struct {
		SubscribeResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode   string `xml:"ResponseCode"`
					SubscriptionID string `xml:"SubscriptionId"`
				} `xml:"SubscribeResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"SubscribeResponse"`
	} `xml:"Body"`
}

type getStreamingEventsEnvelope struct {
	Body struct {
		GetStreamingEventsResponse struct {
			ResponseMessages struct {
				Message streamingResponseMessageXML `xml:"GetStreamingEventsResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"GetStreamingEventsResponse"`
	} `xml:"Body"`
}

type streamingResponseMessageXML struct {
	ResponseCode  string                     `xml:"ResponseCode"`
	Notifications []streamingNotificationXML `xml:"Notifications>Notification"`
}
