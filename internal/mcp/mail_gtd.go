package mcp

import (
	"context"
	"regexp"
	"sort"
	"strings"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type mailCommitmentRecord struct {
	Commitment  braingtd.Commitment       `json:"commitment"`
	Message     providerdata.EmailMessage `json:"message"`
	Artifact    store.Artifact            `json:"artifact"`
	SourceID    string                    `json:"source_id"`
	SourceURL   string                    `json:"source_url,omitempty"`
	BodyFetched bool                      `json:"body_fetched,omitempty"`
	Diagnostics []string                  `json:"diagnostics,omitempty"`
}

type mailCommitmentCandidate struct {
	message     *providerdata.EmailMessage
	reason      string
	needsBody   bool
	peer        string
	status      string
	followUp    string
	actor       string
	waitingFor  string
	nextAction  string
	context     string
	diagnostics []string
}

var (
	mailDeadlinePattern = regexp.MustCompile(`(?i)\b(?:by|until|due)\s+((?:\d{4}-\d{2}-\d{2})(?:[ tT]\d{2}:\d{2}(?::\d{2})?(?:z|[+-]\d{2}:?\d{2})?)?)`)
	mailAskPattern      = regexp.MustCompile(`(?i)\b(could you|can you|would you|please|please let me know|need you to|could you please|can you please|would you please)\b`)
	mailMachinePattern  = regexp.MustCompile(`(?i)\b(no-?reply|noreply|mailer-daemon|do-?not-?reply|notification|automated|system|support|robot|bot|daemon)\b`)
)

func (s *Server) mailCommitmentList(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()

	opts, pageToken, err := mailSearchOptionsFromArgs(args)
	if err != nil {
		return nil, err
	}
	if _, ok := args["limit"]; !ok || opts.MaxResults <= 0 || opts.MaxResults > 50 {
		opts.MaxResults = compactListLimit
	}
	ids, nextPageToken, err := listMailMessageIDs(context.Background(), provider, opts, pageToken)
	if err != nil {
		return nil, err
	}
	metadata, err := provider.GetMessages(context.Background(), ids, "metadata")
	if err != nil {
		return nil, err
	}
	orderedMetadata, err := orderedMailMessages(ids, metadata)
	if err != nil {
		return nil, err
	}
	selfAddresses := mailAccountAddresses(account)
	bodyLimit := intArg(args, "body_limit", 5)
	if bodyLimit < 0 {
		bodyLimit = 0
	}

	analysis := make([]mailCommitmentCandidate, 0, len(orderedMetadata))
	bodyIDs := make([]string, 0, minInt(bodyLimit, len(orderedMetadata)))
	for _, message := range orderedMetadata {
		candidate := analyzeMailCommitmentCandidate(account, selfAddresses, message)
		if candidate.status == "" {
			continue
		}
		if candidate.needsBody && len(bodyIDs) < bodyLimit {
			bodyIDs = append(bodyIDs, candidate.message.ID)
		}
		analysis = append(analysis, candidate)
	}

	bodyByID := map[string]*providerdata.EmailMessage{}
	if len(bodyIDs) > 0 {
		bodyMessages, err := provider.GetMessages(context.Background(), bodyIDs, "full")
		if err != nil {
			return nil, err
		}
		orderedBodies, err := orderedMailMessages(bodyIDs, bodyMessages)
		if err != nil {
			return nil, err
		}
		for _, bodyMessage := range orderedBodies {
			if bodyMessage == nil {
				continue
			}
			bodyByID[strings.TrimSpace(bodyMessage.ID)] = bodyMessage
		}
	}

	records := make([]mailCommitmentRecord, 0, len(analysis))
	for _, candidate := range analysis {
		body := bodyByID[strings.TrimSpace(candidate.message.ID)]
		record, ok := buildMailCommitmentRecord(account, candidate.message, body, candidate, selfAddresses)
		if !ok {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		left := records[i].Message.Date
		right := records[j].Message.Date
		switch {
		case left.Equal(right):
			return records[i].Message.ID < records[j].Message.ID
		case left.IsZero():
			return false
		case right.IsZero():
			return true
		default:
			return left.After(right)
		}
	})

	return map[string]interface{}{
		"account":          account,
		"commitments":      records,
		"count":            len(records),
		"page_token":       pageToken,
		"next_page_token":  nextPageToken,
		"body_limit":       bodyLimit,
		"body_fetch_count": len(bodyIDs),
	}, nil
}

func analyzeMailCommitmentCandidate(account store.ExternalAccount, selfAddresses []string, message *providerdata.EmailMessage) mailCommitmentCandidate {
	candidate := mailCommitmentCandidate{message: message, context: "mail"}
	if message == nil {
		return candidate
	}
	candidate.context = mailContext(message)
	text := mailCommitmentText(message)
	sent := mailLooksSent(message, selfAddresses)
	human := mailSenderLooksHuman(message.Sender)
	hasAsk := mailAskPattern.MatchString(text)
	hasDeadline := mailDeadlinePattern.MatchString(text)
	candidate.peer = mailPeerForMessage(message, selfAddresses, sent)
	if candidate.peer == "" {
		candidate.peer = mailPersonLabel(message.Sender)
	}
	if sent {
		if candidate.peer == "" {
			candidate.peer = mailPersonLabel(message.Sender)
		}
		candidate.status = "waiting"
		candidate.reason = "sent mail with a request"
		candidate.actor = candidate.peer
		candidate.waitingFor = candidate.peer
		candidate.nextAction = "Follow up with " + candidate.peer
		if !hasAsk && !hasDeadline {
			candidate.needsBody = true
		}
		return candidate
	}
	if !human {
		return candidate
	}
	if !hasAsk && !hasDeadline {
		candidate.needsBody = true
		candidate.status = "next"
		candidate.reason = "human sender, body review needed"
		candidate.actor = candidate.peer
		candidate.nextAction = "Reply to " + candidate.peer
		return candidate
	}
	candidate.status = "next"
	candidate.reason = "human sender with request language"
	candidate.actor = candidate.peer
	candidate.nextAction = "Reply to " + candidate.peer
	return candidate
}

func buildMailCommitmentRecord(account store.ExternalAccount, metadata, body *providerdata.EmailMessage, candidate mailCommitmentCandidate, selfAddresses []string) (mailCommitmentRecord, bool) {
	message := cloneMailMessage(metadata)
	bodyFetched := false
	if body != nil {
		bodyFetched = true
		if body.BodyText != nil {
			message.BodyText = body.BodyText
		}
		if body.BodyHTML != nil {
			message.BodyHTML = body.BodyHTML
		}
		if len(body.Attachments) > 0 {
			message.Attachments = append([]providerdata.Attachment(nil), body.Attachments...)
		}
	}
	text := mailCommitmentText(message)
	hasAsk := mailAskPattern.MatchString(text)
	hasDeadline := mailDeadlinePattern.MatchString(text)
	if candidate.status == "" {
		return mailCommitmentRecord{}, false
	}
	peer := candidate.peer
	if peer == "" {
		peer = mailPersonLabel(message.Sender)
	}
	if peer == "" {
		return mailCommitmentRecord{}, false
	}
	followUp := candidate.followUp
	if followUp == "" && hasDeadline {
		if parsed, ok := mailDeadlineFromText(text, message.Date); ok {
			followUp = parsed
		}
	}
	if followUp == "" && candidate.status == "waiting" {
		followUp = message.Date.UTC().AddDate(0, 0, 7).Format("2006-01-02")
	}
	if candidate.status == "next" && !hasAsk && !hasDeadline && !bodyFetched {
		return mailCommitmentRecord{}, false
	}
	if candidate.status == "waiting" && !hasAsk && !hasDeadline && !bodyFetched {
		return mailCommitmentRecord{}, false
	}
	commitment := braingtd.Commitment{
		Kind:           "commitment",
		Title:          mailCommitmentTitle(candidate.status, peer, message.Subject),
		Sphere:         account.Sphere,
		Status:         candidate.status,
		Context:        candidate.context,
		NextAction:     candidate.nextAction,
		FollowUp:       followUp,
		Actor:          candidate.actor,
		WaitingFor:     candidate.waitingFor,
		Project:        mailProjectForMessage(message),
		Labels:         mailCommitmentLabels(message),
		People:         mailCommitmentPeople(candidate.status, peer),
		LastEvidenceAt: evidenceTimestamp(message.Date),
		SourceBindings: []braingtd.SourceBinding{{
			Provider:         account.Provider,
			Ref:              strings.TrimSpace(message.ID),
			URL:              mailSourceURL(account, message.ID),
			Writeable:        false,
			AuthoritativeFor: []string{"status", "next_action", "waiting_for", "follow_up"},
			Summary:          strings.TrimSpace(message.Subject),
			CreatedAt:        evidenceTimestamp(message.Date),
			UpdatedAt:        evidenceTimestamp(message.Date),
		}},
		LocalOverlay: braingtd.LocalOverlay{
			Status:   candidate.status,
			FollowUp: followUp,
			Actor:    candidate.actor,
		},
	}
	if followUp != "" && candidate.status == "waiting" && commitment.NextAction == "" {
		commitment.NextAction = "Follow up with " + peer
	}
	if hasDeadline && commitment.FollowUp == "" {
		if parsed, ok := mailDeadlineFromText(text, message.Date); ok {
			commitment.FollowUp = parsed
			commitment.LocalOverlay.FollowUp = parsed
		}
	}
	diagnostics := append([]string(nil), candidate.diagnostics...)
	if mailPersonLabel(message.Sender) == mailPersonEmail(message.Sender) {
		diagnostics = append(diagnostics, "person stub: "+mailPersonEmail(message.Sender))
	}
	if len(diagnostics) == 0 && candidate.status == "next" && !hasAsk {
		diagnostics = append(diagnostics, "body review bounded before confirming request language")
	}
	sourceURL := mailSourceURL(account, message.ID)
	artifactTitle := strings.TrimSpace(message.Subject)
	if artifactTitle == "" {
		artifactTitle = commitment.Title
	}
	meta := mailArtifactMetaJSON(account, message, sourceURL, bodyFetched)
	return mailCommitmentRecord{
		Commitment:  commitment,
		Message:     *message,
		Artifact:    store.Artifact{Kind: store.ArtifactKindEmail, RefURL: stringPtr(sourceURL), Title: stringPtr(artifactTitle), MetaJSON: stringPtr(meta)},
		SourceID:    strings.TrimSpace(message.ID),
		SourceURL:   sourceURL,
		BodyFetched: bodyFetched,
		Diagnostics: diagnostics,
	}, true
}
