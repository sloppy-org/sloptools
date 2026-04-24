package email

import (
	"context"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"golang.org/x/oauth2"
	gmail "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"strings"
	"sync"
)

func (c *GmailClient) getTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return c.auth.TokenSource(ctx)
}

func (c *GmailClient) getService(ctx context.Context) (*gmail.Service, error) {
	if c.serviceBuilder != nil {
		return c.serviceBuilder(ctx)
	}
	tokenSource, err := c.getTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	return gmail.NewService(ctx, option.WithTokenSource(tokenSource))
}

func (c *GmailClient) ListLabels(ctx context.Context) ([]providerdata.Label, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	c.rateLimiter.Acquire("labels.list")
	result, err := service.Users.Labels.List("me").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list labels: %w", err)
	}
	labels := make([]providerdata.Label, 0, len(result.Labels))
	for _, lbl := range result.Labels {
		labels = append(labels, providerdata.Label{ID: lbl.Id, Name: lbl.Name, Type: lbl.Type, MessagesTotal: int(lbl.MessagesTotal), MessagesUnread: int(lbl.MessagesUnread)})
	}
	return labels, nil
}

func (c *GmailClient) ListMessages(ctx context.Context, opts SearchOptions) ([]string, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	maxResults := opts.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}
	query := buildGmailQuery(opts)
	var messageIDs []string
	pageToken := ""
	for int64(len(messageIDs)) < maxResults {
		c.rateLimiter.Acquire("messages.list")
		call := service.Users.Messages.List("me").Context(ctx).MaxResults(minInt64(500, maxResults-int64(len(messageIDs)))).IncludeSpamTrash(opts.IncludeSpamTrash)
		if query != "" {
			call = call.Q(query)
		}
		if len(opts.LabelIDs) > 0 {
			call = call.LabelIds(opts.LabelIDs...)
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		result, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to list messages: %w", err)
		}
		for _, msg := range result.Messages {
			if int64(len(messageIDs)) >= maxResults {
				break
			}
			messageIDs = append(messageIDs, msg.Id)
		}
		pageToken = result.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return messageIDs, nil
}

func (c *GmailClient) ListMessagesPage(ctx context.Context, opts SearchOptions, pageToken string) (MessagePage, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return MessagePage{}, err
	}
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 100
	}
	query := buildGmailQuery(opts)
	c.rateLimiter.Acquire("messages.list")
	call := service.Users.Messages.List("me").Context(ctx).MaxResults(minInt64(500, maxResults)).IncludeSpamTrash(opts.IncludeSpamTrash)
	if query != "" {
		call = call.Q(query)
	}
	if len(opts.LabelIDs) > 0 {
		call = call.LabelIds(opts.LabelIDs...)
	}
	if strings.TrimSpace(pageToken) != "" {
		call = call.PageToken(strings.TrimSpace(pageToken))
	}
	result, err := call.Do()
	if err != nil {
		return MessagePage{}, fmt.Errorf("failed to list messages: %w", err)
	}
	page := MessagePage{IDs: make([]string, 0, len(result.Messages)), NextPageToken: strings.TrimSpace(result.NextPageToken)}
	for _, msg := range result.Messages {
		if id := strings.TrimSpace(msg.Id); id != "" {
			page.IDs = append(page.IDs, id)
		}
	}
	return page, nil
}

func buildGmailQuery(opts SearchOptions) string {
	var parts []string
	if opts.Folder != "" {
		parts = append(parts, fmt.Sprintf("label:%s", opts.Folder))
	}
	if opts.Text != "" {
		parts = append(parts, opts.Text)
	}
	if opts.Subject != "" {
		parts = append(parts, fmt.Sprintf("subject:%s", opts.Subject))
	}
	if opts.From != "" {
		parts = append(parts, fmt.Sprintf("from:%s", opts.From))
	}
	if opts.To != "" {
		parts = append(parts, fmt.Sprintf("to:%s", opts.To))
	}
	if !opts.After.IsZero() {
		parts = append(parts, fmt.Sprintf("after:%s", opts.After.Format("2006/01/02")))
	}
	if !opts.Before.IsZero() {
		parts = append(parts, fmt.Sprintf("before:%s", opts.Before.Format("2006/01/02")))
	}
	if !opts.Since.IsZero() {
		parts = append(parts, fmt.Sprintf("after:%s", opts.Since.AddDate(0, 0, -1).Format("2006/01/02")))
	}
	if !opts.Until.IsZero() {
		parts = append(parts, fmt.Sprintf("before:%s", opts.Until.AddDate(0, 0, 1).Format("2006/01/02")))
	}
	if opts.HasAttachment != nil && *opts.HasAttachment {
		parts = append(parts, "has:attachment")
	}
	if opts.IsRead != nil {
		if *opts.IsRead {
			parts = append(parts, "-is:unread")
		} else {
			parts = append(parts, "is:unread")
		}
	}
	if opts.IsFlagged != nil {
		if *opts.IsFlagged {
			parts = append(parts, "is:starred")
		} else {
			parts = append(parts, "-is:starred")
		}
	}
	if opts.SizeGreater > 0 {
		parts = append(parts, fmt.Sprintf("larger:%d", opts.SizeGreater))
	}
	if opts.SizeLess > 0 {
		parts = append(parts, fmt.Sprintf("smaller:%d", opts.SizeLess))
	}
	return strings.Join(parts, " ")
} // buildGmailQuery converts SearchOptions to a Gmail query string.

func (c *GmailClient) GetMessage(ctx context.Context, messageID, format string) (*providerdata.EmailMessage, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	if format == "" {
		format = "full"
	}
	c.rateLimiter.Acquire("messages.get")
	msg, err := service.Users.Messages.Get("me", messageID).Context(ctx).Format(format).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	return parseGmailMessage(msg), nil
}

func (c *GmailClient) GetMessages(ctx context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	if format == "" {
		format = "metadata"
	}
	results := make([]*providerdata.EmailMessage, len(messageIDs))
	errors := make([]error, len(messageIDs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 50)
	for i, id := range messageIDs {
		wg.Add(1)
		go func(idx int, msgID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() {
				<-sem
			}()
			msg, err := c.GetMessage(ctx, msgID, format)
			results[idx] = msg
			errors[idx] = err
		}(i, id)
	}
	wg.Wait()
	var validResults []*providerdata.EmailMessage
	for i, msg := range results {
		if errors[i] == nil && msg != nil {
			validResults = append(validResults, msg)
		}
	}
	return validResults, nil
}

func (c *GmailClient) ModifyLabels(ctx context.Context, messageIDs, addLabels, removeLabels []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}
	service, err := c.getService(ctx)
	if err != nil {
		return 0, err
	}
	succeeded := 0
	batchSize := 50
	for i := 0; i < len(messageIDs); i += batchSize {
		end := i + batchSize
		if end > len(messageIDs) {
			end = len(messageIDs)
		}
		batch := messageIDs[i:end]
		c.rateLimiter.Acquire("messages.batchModify")
		req := &gmail.BatchModifyMessagesRequest{Ids: batch, AddLabelIds: addLabels, RemoveLabelIds: removeLabels}
		err := service.Users.Messages.BatchModify("me", req).Context(ctx).Do()
		if err == nil {
			succeeded += len(batch)
		}
	}
	return succeeded, nil
}

func (c *GmailClient) MarkRead(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, nil, []string{"UNREAD"})
}

func (c *GmailClient) MarkUnread(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, []string{"UNREAD"}, nil)
}

func (c *GmailClient) Archive(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, nil, []string{"INBOX"})
}

func (c *GmailClient) MoveToInbox(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, []string{"INBOX"}, nil)
}

func (c *GmailClient) Trash(ctx context.Context, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}
	service, err := c.getService(ctx)
	if err != nil {
		return 0, err
	}
	succeeded := 0
	for _, id := range messageIDs {
		c.rateLimiter.Acquire("messages.trash")
		_, err := service.Users.Messages.Trash("me", id).Context(ctx).Do()
		if err == nil {
			succeeded++
		}
	}
	return succeeded, nil
}

func (c *GmailClient) ApplyNamedLabel(ctx context.Context, messageIDs []string, label string, archive bool) (int, error) {
	labelID, err := c.ensureUserLabel(ctx, label)
	if err != nil {
		return 0, err
	}
	remove := []string(nil)
	if archive {
		remove = []string{"INBOX"}
	}
	return c.ModifyLabels(ctx, messageIDs, []string{labelID}, remove)
}

func (c *GmailClient) SetFlag(ctx context.Context, messageIDs []string, flag Flag) (int, error) {
	switch strings.ToLower(strings.TrimSpace(flag.Status)) {
	case strings.ToLower(FlagStatusFlagged):
		return c.ModifyLabels(ctx, messageIDs, []string{"STARRED"}, nil)
	case strings.ToLower(FlagStatusNotFlagged):
		return c.ModifyLabels(ctx, messageIDs, nil, []string{"STARRED"})
	case strings.ToLower(FlagStatusComplete):
		return 0, fmt.Errorf("gmail flag status %q: %w", FlagStatusComplete, ErrCapabilityUnsupported)
	default:
		return 0, fmt.Errorf("gmail flag status %q is not recognised", flag.Status)
	}
}

func (c *GmailClient) ClearFlag(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, nil, []string{"STARRED"})
}

func (c *GmailClient) SetCategories(ctx context.Context, messageIDs []string, categories []string) (int, error) {
	ids := compactStrings(messageIDs)
	if len(ids) == 0 {
		return 0, nil
	}
	labels, err := c.ListLabels(ctx)
	if err != nil {
		return 0, err
	}
	labelsByNameLower := make(map[string]string, len(labels))
	userLabelIDs := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		name := strings.TrimSpace(label.Name)
		id := strings.TrimSpace(label.ID)
		if name == "" || id == "" {
			continue
		}
		labelsByNameLower[strings.ToLower(name)] = id
		if strings.EqualFold(strings.TrimSpace(label.Type), "user") {
			userLabelIDs[id] = struct{}{}
		}
	}
	desiredIDs := make([]string, 0, len(categories))
	desiredSet := make(map[string]struct{}, len(categories))
	for _, category := range compactStrings(categories) {
		labelID, err := c.ensureUserLabel(ctx, category)
		if err != nil {
			return 0, err
		}
		if _, seen := desiredSet[labelID]; seen {
			continue
		}
		desiredSet[labelID] = struct{}{}
		desiredIDs = append(desiredIDs, labelID)
		userLabelIDs[labelID] = struct{}{}
	}
	currentByMsg, err := c.collectUserLabelIDs(ctx, ids, userLabelIDs)
	if err != nil {
		return 0, err
	}
	succeeded := 0
	for _, id := range ids {
		current := currentByMsg[id]
		remove := make([]string, 0)
		for labelID := range current {
			if _, keep := desiredSet[labelID]; !keep {
				remove = append(remove, labelID)
			}
		}
		add := make([]string, 0, len(desiredIDs))
		for _, labelID := range desiredIDs {
			if _, already := current[labelID]; !already {
				add = append(add, labelID)
			}
		}
		if len(add) == 0 && len(remove) == 0 {
			succeeded++
			continue
		}
		count, err := c.ModifyLabels(ctx, []string{id}, add, remove)
		if err != nil {
			return succeeded, err
		}
		succeeded += count
	}
	return succeeded, nil
}

func (c *GmailClient) collectUserLabelIDs(ctx context.Context, messageIDs []string, userLabelIDs map[string]struct{}) (map[string]map[string]struct{}, error) {
	out := make(map[string]map[string]struct{}, len(messageIDs))
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range messageIDs {
		c.rateLimiter.Acquire("messages.get")
		msg, err := service.Users.Messages.Get("me", id).Context(ctx).Format("minimal").Do()
		if err != nil {
			return nil, fmt.Errorf("failed to read gmail labels for %s: %w", id, err)
		}
		assigned := make(map[string]struct{}, len(msg.LabelIds))
		for _, labelID := range msg.LabelIds {
			clean := strings.TrimSpace(labelID)
			if clean == "" {
				continue
			}
			if _, ok := userLabelIDs[clean]; ok {
				assigned[clean] = struct{}{}
			}
		}
		out[id] = assigned
	}
	return out, nil
}

func (c *GmailClient) ServerFilterCapabilities() ServerFilterCapabilities {
	return ServerFilterCapabilities{Provider: c.ProviderName(), SupportsList: true, SupportsUpsert: true, SupportsDelete: true, SupportsArchive: true, SupportsTrash: false, SupportsMoveTo: true, SupportsMarkRead: true, SupportsForward: true, SupportsAddLabels: true, SupportsQuery: true}
}

func (c *GmailClient) ListServerFilters(ctx context.Context) ([]ServerFilter, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	c.rateLimiter.Acquire("settings.filters.list")
	result, err := service.Users.Settings.Filters.List("me").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list gmail filters: %w", err)
	}
	labels, err := c.ListLabels(ctx)
	if err != nil {
		return nil, err
	}
	labelByID := make(map[string]string, len(labels))
	for _, label := range labels {
		labelByID[strings.TrimSpace(label.ID)] = strings.TrimSpace(label.Name)
	}
	out := make([]ServerFilter, 0, len(result.Filter))
	for _, filter := range result.Filter {
		out = append(out, gmailFilterToServerFilter(filter, labelByID))
	}
	return out, nil
}

func (c *GmailClient) UpsertServerFilter(ctx context.Context, filter ServerFilter) (ServerFilter, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return ServerFilter{}, err
	}
	payload, err := c.serverFilterToGmailFilter(ctx, filter)
	if err != nil {
		return ServerFilter{}, err
	}
	if strings.TrimSpace(filter.ID) != "" {
		c.rateLimiter.Acquire("settings.filters.delete")
		if err := service.Users.Settings.Filters.Delete("me", strings.TrimSpace(filter.ID)).Context(ctx).Do(); err != nil {
			return ServerFilter{}, fmt.Errorf("failed to delete gmail filter %s: %w", filter.ID, err)
		}
	}
	c.rateLimiter.Acquire("settings.filters.create")
	created, err := service.Users.Settings.Filters.Create("me", payload).Context(ctx).Do()
	if err != nil {
		return ServerFilter{}, fmt.Errorf("failed to create gmail filter: %w", err)
	}
	labels, err := c.ListLabels(ctx)
	if err != nil {
		return ServerFilter{}, err
	}
	labelByID := make(map[string]string, len(labels))
	for _, label := range labels {
		labelByID[strings.TrimSpace(label.ID)] = strings.TrimSpace(label.Name)
	}
	return gmailFilterToServerFilter(created, labelByID), nil
}
