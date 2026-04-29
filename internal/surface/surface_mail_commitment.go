package surface

func init() {
	MCPTools = append(MCPTools, Tool{
		Name:        "mail_commitment_list",
		Description: "Derive GTD commitments from mail messages and linked email artifacts. Use body_limit to bound confirmation fetches.",
		Required:    []string{"account_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context filter used when account_id is omitted.",
				Enum:        []string{"work", "private"},
			},
			"limit": {
				Type:        "integer",
				Description: "Optional maximum number of messages to inspect.",
			},
			"body_limit": {
				Type:        "integer",
				Description: "Optional maximum number of matching messages whose full bodies may be fetched. Defaults to 5.",
			},
		},
	})
}
