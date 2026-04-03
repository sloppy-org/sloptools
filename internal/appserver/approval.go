package appserver

import (
	"fmt"
	"strings"
)

const (
	ApprovalPolicyNever          = "never"
	ApprovalPolicyOnRequest      = "on-request"
	ApprovalPolicyUnlessTrusted  = "unlessTrusted"
	ApprovalMethodCommandRequest = "item/commandExecution/requestApproval"
	ApprovalMethodFileRequest    = "item/fileChange/requestApproval"
)

type ApprovalRequest struct {
	ID        interface{}
	Method    string
	Kind      string
	ItemID    string
	Reason    string
	GrantRoot string
	Risk      map[string]interface{}
}

func applyDefaultApprovalPolicy(params map[string]interface{}) map[string]interface{} {
	if params == nil {
		params = map[string]interface{}{}
	}
	value, ok := params["approvalPolicy"]
	if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" || strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "<nil>") {
		params["approvalPolicy"] = ApprovalPolicyOnRequest
	}
	return params
}

func normalizeApprovalPolicy(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "default", "<nil>":
		return ApprovalPolicyOnRequest
	case ApprovalPolicyNever:
		return ApprovalPolicyNever
	case ApprovalPolicyOnRequest:
		return ApprovalPolicyOnRequest
	case "untrusted", "unless-trusted", "unlesstrusted", "unless_trusted", ApprovalPolicyUnlessTrusted:
		return ApprovalPolicyUnlessTrusted
	default:
		return strings.TrimSpace(raw)
	}
}

func NormalizeApprovalPolicy(raw string) string {
	return normalizeApprovalPolicy(raw)
}

func parseApprovalRequest(msg map[string]interface{}) (*ApprovalRequest, bool) {
	method := strings.TrimSpace(stringifyItemValue(msg["method"]))
	switch method {
	case ApprovalMethodCommandRequest, "execCommandApproval":
		params, _ := msg["params"].(map[string]interface{})
		req := &ApprovalRequest{
			ID:     msg["id"],
			Method: method,
			Kind:   "command_execution",
			ItemID: strings.TrimSpace(stringifyItemValue(params["item_id"])),
			Reason: strings.TrimSpace(stringifyItemValue(params["reason"])),
		}
		if risk, _ := params["risk"].(map[string]interface{}); risk != nil {
			req.Risk = risk
		}
		return req, true
	case ApprovalMethodFileRequest, "fileChangeApproval":
		params, _ := msg["params"].(map[string]interface{})
		return &ApprovalRequest{
			ID:        msg["id"],
			Method:    method,
			Kind:      "file_change",
			ItemID:    strings.TrimSpace(stringifyItemValue(params["item_id"])),
			Reason:    strings.TrimSpace(stringifyItemValue(params["reason"])),
			GrantRoot: strings.TrimSpace(stringifyItemValue(params["grant_root"])),
		}, true
	default:
		return nil, false
	}
}

func normalizeApprovalDecision(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "cancel", "canceled", "cancelled":
		return "cancel"
	case "approve", "approved", "accept", "acceptforsession":
		return "accept"
	case "reject", "rejected", "decline", "denied", "deny":
		return "decline"
	default:
		return "cancel"
	}
}
