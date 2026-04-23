package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	_ "github.com/emersion/go-message/charset"
	gomessage "github.com/emersion/go-message/mail"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

// searchResult holds a message reference with its date for sorting.
type searchResult struct {
	folder string
	uid    imap.UID
	date   time.Time
}

// IMAPClient provides access to an IMAP server.
type IMAPClient struct {
	name     string
	host     string
	port     int
	username string
	password string
	useTLS   bool
	startTLS bool

	client *imapclient.Client
	mu     sync.Mutex

	// Connection pool for parallel operations
	pool     chan *imapclient.Client
	poolSize int

	// Mailbox cache to avoid re-listing folders
	mailboxCache     []string
	mailboxCacheTime time.Time
	mailboxCacheMu   sync.RWMutex

	smtpConfig SMTPConfig
	smtpSend   SMTPSender
}

// Compile-time check that IMAPClient implements EmailProvider.
var _ EmailProvider = (*IMAPClient)(nil)
var _ MessageActionProvider = (*IMAPClient)(nil)
var _ MessagePageProvider = (*IMAPClient)(nil)

// DefaultPoolSize is the default number of pooled connections for parallel operations.
const DefaultPoolSize = 10

// NewIMAPClient creates a new IMAP client.
func NewIMAPClient(name, host string, port int, username, password string, useTLS, startTLS bool) *IMAPClient {
	if port == 0 {
		if useTLS {
			port = 993
		} else {
			port = 143
		}
	}
	return &IMAPClient{
		name:     name,
		host:     host,
		port:     port,
		username: username,
		password: password,
		useTLS:   useTLS,
		startTLS: startTLS,
		poolSize: DefaultPoolSize,
		pool:     make(chan *imapclient.Client, DefaultPoolSize),
		smtpSend: defaultSMTPSender,
	}
}

func (c *IMAPClient) ConfigureDraftTransport(cfg SMTPConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.smtpConfig = cfg
	if c.smtpSend == nil {
		c.smtpSend = defaultSMTPSender
	}
}

// NewIMAPFromConfig creates an IMAP client from a provider configuration.
// Password is read from environment variable SLOPPY_IMAP_PASSWORD_<NAME>.
func NewIMAPFromConfig(name string, config ProviderConfig) (*IMAPClient, error) {
	envName := "SLOPPY_IMAP_PASSWORD_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	password := os.Getenv(envName)
	if password == "" {
		return nil, fmt.Errorf("password not set - export %s environment variable", envName)
	}

	useTLS := config.TLS
	if config.Port == 993 {
		useTLS = true
	}

	return NewIMAPClient(name, config.Host, config.Port, config.Username, password, useTLS, config.StartTLS), nil
}

func (c *IMAPClient) connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		return nil
	}

	addr := fmt.Sprintf("%s:%d", c.host, c.port)
	tlsConfig := &tls.Config{ServerName: c.host}

	var client *imapclient.Client
	var err error

	switch {
	case c.useTLS:
		client, err = imapclient.DialTLS(addr, &imapclient.Options{TLSConfig: tlsConfig})
	case c.startTLS:
		client, err = imapclient.DialStartTLS(addr, &imapclient.Options{TLSConfig: tlsConfig})
	default:
		client, err = imapclient.DialInsecure(addr, nil)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to IMAP server: %w", err)
	}

	if err := client.Login(c.username, c.password).Wait(); err != nil {
		client.Close()
		return fmt.Errorf("failed to login: %w", err)
	}

	c.client = client
	return nil
}

// WarmUp pre-establishes the main connection and fills the connection pool.
// Call this at startup to avoid login latency on the first request.
func (c *IMAPClient) WarmUp(ctx context.Context) error {
	// Connect main client
	if err := c.connect(ctx); err != nil {
		return err
	}

	// Fill the connection pool
	var wg sync.WaitGroup
	errors := make(chan error, c.poolSize)

	for i := 0; i < c.poolSize; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := c.newConnection()
			if err != nil {
				errors <- err
				return
			}
			c.putPoolConn(conn)
		}()
	}

	wg.Wait()
	close(errors)

	// Return first error if any
	for err := range errors {
		return fmt.Errorf("failed to warm up connection pool: %w", err)
	}

	return nil
}

// getPoolConn gets a connection from the pool or creates a new one.
func (c *IMAPClient) getPoolConn() (*imapclient.Client, error) {
	select {
	case conn := <-c.pool:
		// Test if connection is still alive with NOOP
		if err := conn.Noop().Wait(); err != nil {
			conn.Close()
			return c.newConnection()
		}
		return conn, nil
	default:
		return c.newConnection()
	}
}

// putPoolConn returns a connection to the pool or closes it if pool is full.
func (c *IMAPClient) putPoolConn(conn *imapclient.Client) {
	select {
	case c.pool <- conn:
		// Returned to pool
	default:
		// Pool full, close connection
		conn.Close()
	}
}

// ListLabels returns all IMAP folders as labels.
func (c *IMAPClient) ListLabels(ctx context.Context) ([]providerdata.Label, error) {
	if err := c.connect(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	mailboxes, err := c.client.List("", "*", nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to list mailboxes: %w", err)
	}

	labels := make([]providerdata.Label, 0, len(mailboxes))
	for _, mbox := range mailboxes {
		label := providerdata.Label{
			ID:   mbox.Mailbox,
			Name: mbox.Mailbox,
			Type: "imap",
		}

		selectData, err := c.client.Select(mbox.Mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err == nil {
			label.MessagesTotal = int(selectData.NumMessages)
		}

		labels = append(labels, label)
	}

	return labels, nil
}

// ListMessages returns message IDs matching the search options.
// For IMAP, message IDs are formatted as "folder:uid".
// When no folder is specified, searches all folders (except Junk/Trash).
func (c *IMAPClient) ListMessages(ctx context.Context, opts SearchOptions) ([]string, error) {
	if err := c.connect(ctx); err != nil {
		return nil, err
	}

	// If a specific folder is requested, search only that folder
	if opts.Folder != "" {
		return c.searchFolder(ctx, opts.Folder, opts)
	}

	// Otherwise search all folders
	return c.searchAllFolders(ctx, opts)
}

func (c *IMAPClient) ListMessagesPage(ctx context.Context, opts SearchOptions, pageToken string) (MessagePage, error) {
	if err := c.connect(ctx); err != nil {
		return MessagePage{}, err
	}
	if strings.TrimSpace(opts.Folder) == "" {
		return MessagePage{}, fmt.Errorf("imap paged message listing requires a folder")
	}
	offset := 0
	if strings.TrimSpace(pageToken) != "" {
		value, err := strconv.Atoi(strings.TrimSpace(pageToken))
		if err != nil || value < 0 {
			return MessagePage{}, fmt.Errorf("imap invalid page token %q", pageToken)
		}
		offset = value
	}
	return c.searchFolderPage(ctx, opts.Folder, opts, offset)
}

// searchFolder searches a single folder for messages matching the criteria.
func (c *IMAPClient) searchFolder(ctx context.Context, folder string, opts SearchOptions) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to select folder %q: %w", folder, err)
	}

	criteria := buildIMAPSearchCriteria(opts)

	searchData, err := c.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to search messages: %w", err)
	}

	uids := searchData.AllUIDs()

	maxResults := opts.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}
	if int64(len(uids)) > maxResults {
		uids = uids[len(uids)-int(maxResults):]
	}

	messageIDs := make([]string, len(uids))
	for i, uid := range uids {
		messageIDs[len(uids)-1-i] = fmt.Sprintf("%s:%d", folder, uid)
	}

	return messageIDs, nil
}

func (c *IMAPClient) searchFolderPage(ctx context.Context, folder string, opts SearchOptions, offset int) (MessagePage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return MessagePage{}, fmt.Errorf("failed to select folder %q: %w", folder, err)
	}

	criteria := buildIMAPSearchCriteria(opts)
	searchData, err := c.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return MessagePage{}, fmt.Errorf("failed to search messages: %w", err)
	}

	uids := searchData.AllUIDs()
	if offset >= len(uids) {
		return MessagePage{}, nil
	}

	maxResults := int(opts.MaxResults)
	if maxResults <= 0 {
		maxResults = 100
	}
	end := offset + maxResults
	if end > len(uids) {
		end = len(uids)
	}

	page := MessagePage{
		IDs: make([]string, 0, end-offset),
	}
	for i := end - 1; i >= offset; i-- {
		page.IDs = append(page.IDs, fmt.Sprintf("%s:%d", folder, uids[i]))
	}
	if end < len(uids) {
		page.NextPageToken = strconv.Itoa(end)
	}
	return page, nil
}

// mailboxCacheTTL is how long the mailbox list cache is valid.
const mailboxCacheTTL = 5 * time.Minute

// getCachedMailboxes returns the cached mailbox list or fetches a fresh one.
func (c *IMAPClient) getCachedMailboxes(ctx context.Context) ([]string, error) {
	c.mailboxCacheMu.RLock()
	if len(c.mailboxCache) > 0 && time.Since(c.mailboxCacheTime) < mailboxCacheTTL {
		result := make([]string, len(c.mailboxCache))
		copy(result, c.mailboxCache)
		c.mailboxCacheMu.RUnlock()
		return result, nil
	}
	c.mailboxCacheMu.RUnlock()

	// Fetch fresh list
	if err := c.connect(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	mailboxes, err := c.client.List("", "*", nil).Collect()
	c.mu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("failed to list mailboxes: %w", err)
	}

	folders := make([]string, 0, len(mailboxes))
	for _, mbox := range mailboxes {
		folders = append(folders, mbox.Mailbox)
	}

	// Update cache
	c.mailboxCacheMu.Lock()
	c.mailboxCache = folders
	c.mailboxCacheTime = time.Now()
	c.mailboxCacheMu.Unlock()

	return folders, nil
}

// searchAllFolders searches all folders in parallel and combines results.
// Optimizations: cached mailbox list, early termination, smart folder filtering.
func (c *IMAPClient) searchAllFolders(ctx context.Context, opts SearchOptions) ([]string, error) {
	folders, err := c.getCachedMailboxes(ctx)
	if err != nil {
		return nil, err
	}

	// Folders to always skip
	skipFolders := map[string]bool{
		"Junk-E-Mail":        true,
		"Gelöschte Elemente": true,
		"Entwürfe":           true,
		"Postausgang":        true,
		"Kalender":           true,
		"Kontakte":           true,
		"Aufgaben":           true,
		"Journal":            true,
		"Notizen":            true,
		"RSS-Feeds":          true,
	}
	if !opts.IncludeSpamTrash {
		skipFolders["SPAM"] = true
		skipFolders["TRASH"] = true
		skipFolders["Spam"] = true
		skipFolders["Trash"] = true
	}

	// Smart folder filtering: for recent searches, skip old archive subfolders
	isRecentSearch := !opts.Since.IsZero() && time.Since(opts.Since) < 365*24*time.Hour
	oldArchiveYears := make(map[string]bool)
	if isRecentSearch {
		// Skip archive folders older than the search window
		searchYear := opts.Since.Year()
		for year := 2010; year < searchYear; year++ {
			oldArchiveYears[fmt.Sprintf("Archive/%d", year)] = true
		}
	}

	// Categorize and prioritize folders
	var priorityFolders, archiveFolders, otherFolders []string
	for _, folder := range folders {
		if skipFolders[folder] ||
			strings.HasPrefix(folder, "Synchronisierungsprobleme") ||
			strings.HasPrefix(folder, "Kontakte/") {
			continue
		}

		// Skip old archive years for recent searches
		if isRecentSearch {
			skip := false
			for prefix := range oldArchiveYears {
				if strings.HasPrefix(folder, prefix) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}

		switch {
		case folder == "INBOX":
			priorityFolders = append([]string{folder}, priorityFolders...)
		case folder == "Archive" || folder == "Gesendete Elemente":
			priorityFolders = append(priorityFolders, folder)
		case strings.HasPrefix(folder, "Archive/"):
			archiveFolders = append(archiveFolders, folder)
		default:
			otherFolders = append(otherFolders, folder)
		}
	}

	// Order: INBOX, Archive, Sent, Archive/*, then others
	allFolders := append(priorityFolders, archiveFolders...)
	allFolders = append(allFolders, otherFolders...)

	maxResults := opts.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}

	// Context for early termination
	searchCtx, cancelSearch := context.WithCancel(ctx)
	defer cancelSearch()

	// Channel for results and semaphore for parallel connections
	resultsChan := make(chan []searchResult, len(allFolders))
	sem := make(chan struct{}, c.poolSize)
	var wg sync.WaitGroup

	// Track total results for early termination
	var resultCount int64
	var resultCountMu sync.Mutex

	criteria := buildIMAPSearchCriteria(opts)

	for _, folder := range allFolders {
		wg.Add(1)
		go func(folder string) {
			defer wg.Done()

			// Check if we should stop early
			select {
			case <-searchCtx.Done():
				return
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			results := c.searchFolderWithDates(searchCtx, folder, criteria)
			if len(results) > 0 {
				resultsChan <- results

				// Check for early termination
				resultCountMu.Lock()
				resultCount += int64(len(results))
				shouldStop := resultCount >= maxResults*2 // Get 2x for better sorting
				resultCountMu.Unlock()

				if shouldStop {
					cancelSearch()
				}
			}
		}(folder)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect all results
	var allResults []searchResult
	for results := range resultsChan {
		allResults = append(allResults, results...)
	}

	// Sort by date descending (newest first)
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].date.After(allResults[j].date)
	})

	if int64(len(allResults)) > maxResults {
		allResults = allResults[:maxResults]
	}

	messageIDs := make([]string, len(allResults))
	for i, r := range allResults {
		messageIDs[i] = fmt.Sprintf("%s:%d", r.folder, r.uid)
	}

	return messageIDs, nil
}

// searchFolderWithDates searches a folder and returns results with dates for sorting.
func (c *IMAPClient) searchFolderWithDates(ctx context.Context, folder string, criteria *imap.SearchCriteria) []searchResult {
	// Get a connection from the pool
	conn, err := c.getPoolConn()
	if err != nil {
		return nil
	}
	defer c.putPoolConn(conn)

	_, err = conn.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil
	}

	searchData, err := conn.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return nil
	}

	// Fetch envelope to get dates
	uidSet := imap.UIDSet{}
	for _, uid := range uids {
		uidSet.AddNum(uid)
	}

	fetchCmd := conn.Fetch(uidSet, &imap.FetchOptions{
		UID:      true,
		Envelope: true,
	})

	var results []searchResult
	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}
		msgBuf, err := msg.Collect()
		if err != nil {
			continue
		}

		date := time.Now()
		if msgBuf.Envelope != nil && !msgBuf.Envelope.Date.IsZero() {
			date = msgBuf.Envelope.Date
		}

		results = append(results, searchResult{
			folder: folder,
			uid:    msgBuf.UID,
			date:   date,
		})
	}

	return results
}

// newConnection creates a new IMAP connection for parallel operations.
func (c *IMAPClient) newConnection() (*imapclient.Client, error) {
	addr := fmt.Sprintf("%s:%d", c.host, c.port)
	tlsConfig := &tls.Config{ServerName: c.host}

	var client *imapclient.Client
	var err error

	switch {
	case c.useTLS:
		client, err = imapclient.DialTLS(addr, &imapclient.Options{TLSConfig: tlsConfig})
	case c.startTLS:
		client, err = imapclient.DialStartTLS(addr, &imapclient.Options{TLSConfig: tlsConfig})
	default:
		client, err = imapclient.DialInsecure(addr, nil)
	}

	if err != nil {
		return nil, err
	}

	if err := client.Login(c.username, c.password).Wait(); err != nil {
		client.Close()
		return nil, err
	}

	return client, nil
}

// buildIMAPSearchCriteria converts SearchOptions to IMAP search criteria.
func buildIMAPSearchCriteria(opts SearchOptions) *imap.SearchCriteria {
	criteria := &imap.SearchCriteria{}

	if opts.Text != "" {
		criteria.Text = append(criteria.Text, opts.Text)
	}

	if opts.Subject != "" {
		criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{
			Key:   "Subject",
			Value: opts.Subject,
		})
	}

	if opts.From != "" {
		criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{
			Key:   "From",
			Value: opts.From,
		})
	}

	if opts.To != "" {
		criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{
			Key:   "To",
			Value: opts.To,
		})
	}

	if !opts.After.IsZero() {
		criteria.Since = opts.After
	}

	if !opts.Before.IsZero() {
		criteria.Before = opts.Before
	}

	if !opts.Since.IsZero() {
		criteria.Since = opts.Since
	}

	if !opts.Until.IsZero() {
		criteria.Before = opts.Until.AddDate(0, 0, 1)
	}

	if opts.IsRead != nil {
		if *opts.IsRead {
			criteria.Flag = append(criteria.Flag, imap.FlagSeen)
		} else {
			criteria.NotFlag = append(criteria.NotFlag, imap.FlagSeen)
		}
	}

	if opts.IsFlagged != nil {
		if *opts.IsFlagged {
			criteria.Flag = append(criteria.Flag, imap.FlagFlagged)
		} else {
			criteria.NotFlag = append(criteria.NotFlag, imap.FlagFlagged)
		}
	}

	if opts.SizeGreater > 0 {
		criteria.Larger = opts.SizeGreater
	}

	if opts.SizeLess > 0 {
		criteria.Smaller = opts.SizeLess
	}

	return criteria
}

// GetMessage retrieves a single message by ID.
func (c *IMAPClient) GetMessage(ctx context.Context, messageID, format string) (*providerdata.EmailMessage, error) {
	folder, uid, err := parseMessageID(messageID)
	if err != nil {
		return nil, err
	}

	if err := c.connect(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	_, err = c.client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to select folder %q: %w", folder, err)
	}

	fetchOpts := &imap.FetchOptions{
		UID:      true,
		Flags:    true,
		Envelope: true,
	}

	if format == "full" {
		fetchOpts.BodySection = []*imap.FetchItemBodySection{
			{Peek: true},
		}
	}

	uidSet := imap.UIDSet{}
	uidSet.AddNum(uid)
	fetchCmd := c.client.Fetch(uidSet, fetchOpts)

	msg := fetchCmd.Next()
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}

	msgBuf, err := msg.Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to collect message: %w", err)
	}

	// Drain remaining messages
	for fetchCmd.Next() != nil {
	}

	return parseIMAPMessage(folder, msgBuf, format == "full")
}

// GetMessages retrieves multiple messages concurrently.
func (c *IMAPClient) GetMessages(ctx context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}

	byFolder := make(map[string][]imap.UID)
	for _, id := range messageIDs {
		folder, uid, err := parseMessageID(id)
		if err != nil {
			continue
		}
		byFolder[folder] = append(byFolder[folder], uid)
	}

	var results []*providerdata.EmailMessage
	var resultsMu sync.Mutex

	for folder, uids := range byFolder {
		msgs, err := c.fetchMessages(ctx, folder, uids, format)
		if err != nil {
			continue
		}
		resultsMu.Lock()
		results = append(results, msgs...)
		resultsMu.Unlock()
	}

	return results, nil
}

func (c *IMAPClient) fetchMessages(ctx context.Context, folder string, uids []imap.UID, format string) ([]*providerdata.EmailMessage, error) {
	if err := c.connect(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to select folder %q: %w", folder, err)
	}

	fetchOpts := &imap.FetchOptions{
		UID:      true,
		Flags:    true,
		Envelope: true,
	}

	if format == "full" {
		fetchOpts.BodySection = []*imap.FetchItemBodySection{
			{Peek: true},
		}
	}

	uidSet := imap.UIDSet{}
	for _, uid := range uids {
		uidSet.AddNum(uid)
	}

	fetchCmd := c.client.Fetch(uidSet, fetchOpts)

	var results []*providerdata.EmailMessage
	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		msgBuf, err := msg.Collect()
		if err != nil {
			continue
		}

		email, err := parseIMAPMessage(folder, msgBuf, format == "full")
		if err != nil {
			continue
		}
		results = append(results, email)
	}

	return results, nil
}

// MarkRead marks messages as read by adding the \Seen flag.
func (c *IMAPClient) MarkRead(ctx context.Context, messageIDs []string) (int, error) {
	return c.storeFlags(ctx, messageIDs, imap.StoreFlagsAdd, []imap.Flag{imap.FlagSeen})
}

// MarkUnread marks messages as unread by removing the \Seen flag.
func (c *IMAPClient) MarkUnread(ctx context.Context, messageIDs []string) (int, error) {
	return c.storeFlags(ctx, messageIDs, imap.StoreFlagsDel, []imap.Flag{imap.FlagSeen})
}

// Archive moves messages to an archive mailbox.
func (c *IMAPClient) Archive(ctx context.Context, messageIDs []string) (int, error) {
	return c.moveMessages(ctx, messageIDs, []string{"Archive", "Archiv"}, "Archive", "archive")
}

// MoveToInbox restores messages to INBOX.
func (c *IMAPClient) MoveToInbox(ctx context.Context, messageIDs []string) (int, error) {
	return c.moveMessages(ctx, messageIDs, []string{"INBOX"}, "INBOX", "move to inbox")
}

// Trash moves messages to the trash mailbox.
func (c *IMAPClient) Trash(ctx context.Context, messageIDs []string) (int, error) {
	return c.moveMessages(ctx, messageIDs, []string{"Gelöschte Elemente", "Trash", "TRASH", "Deleted Items", "Deleted Messages"}, "Trash", "trash")
}

// Delete permanently deletes messages from their current folders.
func (c *IMAPClient) Delete(ctx context.Context, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	byFolder, invalidIDs := groupMessageIDsByFolder(messageIDs)

	if err := c.connect(ctx); err != nil {
		return 0, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	caps := c.client.Caps()
	supportsUIDExpunge := caps.Has(imap.CapUIDPlus) || caps.Has(imap.CapIMAP4rev2)

	succeeded := 0
	var errMsgs []string

	for folder, uids := range byFolder {
		if len(uids) == 0 {
			continue
		}

		if _, err := c.client.Select(folder, nil).Wait(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: select failed: %v", folder, err))
			continue
		}

		uidSet := uidSetFromSlice(uids)
		storeCmd := c.client.Store(uidSet, &imap.StoreFlags{
			Op:     imap.StoreFlagsAdd,
			Silent: true,
			Flags:  []imap.Flag{imap.FlagDeleted},
		}, nil)
		if err := storeCmd.Close(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: mark deleted failed: %v", folder, err))
			continue
		}

		var expungeErr error
		if supportsUIDExpunge {
			expungeErr = c.client.UIDExpunge(uidSet).Close()
		} else {
			// Fallback when UID EXPUNGE is unavailable.
			expungeErr = c.client.Expunge().Close()
		}
		if expungeErr != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: expunge failed: %v", folder, expungeErr))
			continue
		}

		succeeded += len(uids)
	}

	for _, id := range invalidIDs {
		errMsgs = append(errMsgs, fmt.Sprintf("%s: invalid message ID", id))
	}

	if len(errMsgs) > 0 {
		return succeeded, fmt.Errorf("delete completed with errors: %s", strings.Join(errMsgs, "; "))
	}

	return succeeded, nil
}

// Defer returns an explicit v1 stub response for IMAP providers.
func (c *IMAPClient) Defer(_ context.Context, messageID string, _ time.Time) (MessageActionResult, error) {
	return MessageActionResult{
		Provider:              c.ProviderName(),
		Action:                "defer",
		MessageID:             messageID,
		Status:                "stub_not_supported",
		EffectiveProviderMode: "stub",
		StubReason:            "DEFER_NOT_SUPPORTED_FOR_PROVIDER",
	}, nil
}

func (c *IMAPClient) SupportsNativeDefer() bool {
	return false
}

func (c *IMAPClient) moveMessages(ctx context.Context, messageIDs []string, destinationCandidates []string, fallbackDestination, opName string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	byFolder, invalidIDs := groupMessageIDsByFolder(messageIDs)

	if err := c.connect(ctx); err != nil {
		return 0, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	destination, err := c.resolveOrCreateMailboxLocked(destinationCandidates, fallbackDestination)
	if err != nil {
		return 0, err
	}

	succeeded := 0
	var errMsgs []string

	for folder, uids := range byFolder {
		if len(uids) == 0 {
			continue
		}

		// Message is already in destination mailbox.
		if folder == destination {
			succeeded += len(uids)
			continue
		}

		if _, err := c.client.Select(folder, nil).Wait(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: select failed: %v", folder, err))
			continue
		}

		uidSet := uidSetFromSlice(uids)
		if _, err := c.client.Move(uidSet, destination).Wait(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: move failed: %v", folder, err))
			continue
		}

		succeeded += len(uids)
	}

	for _, id := range invalidIDs {
		errMsgs = append(errMsgs, fmt.Sprintf("%s: invalid message ID", id))
	}

	if len(errMsgs) > 0 {
		return succeeded, fmt.Errorf("%s completed with errors: %s", opName, strings.Join(errMsgs, "; "))
	}

	return succeeded, nil
}

func (c *IMAPClient) storeFlags(ctx context.Context, messageIDs []string, op imap.StoreFlagsOp, flags []imap.Flag) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	byFolder, invalidIDs := groupMessageIDsByFolder(messageIDs)

	if err := c.connect(ctx); err != nil {
		return 0, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	succeeded := 0
	var errMsgs []string

	for folder, uids := range byFolder {
		if len(uids) == 0 {
			continue
		}

		if _, err := c.client.Select(folder, nil).Wait(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: select failed: %v", folder, err))
			continue
		}

		uidSet := uidSetFromSlice(uids)
		storeCmd := c.client.Store(uidSet, &imap.StoreFlags{
			Op:     op,
			Silent: true,
			Flags:  flags,
		}, nil)
		if err := storeCmd.Close(); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: store failed: %v", folder, err))
			continue
		}

		succeeded += len(uids)
	}

	for _, id := range invalidIDs {
		errMsgs = append(errMsgs, fmt.Sprintf("%s: invalid message ID", id))
	}

	if len(errMsgs) > 0 {
		return succeeded, fmt.Errorf("flag update completed with errors: %s", strings.Join(errMsgs, "; "))
	}

	return succeeded, nil
}

func (c *IMAPClient) resolveOrCreateMailboxLocked(candidates []string, fallback string) (string, error) {
	mailboxes, err := c.client.List("", "*", nil).Collect()
	if err != nil {
		return "", fmt.Errorf("failed to list mailboxes: %w", err)
	}

	existing := make(map[string]struct{}, len(mailboxes))
	for _, mbox := range mailboxes {
		existing[mbox.Mailbox] = struct{}{}
	}

	for _, name := range candidates {
		if _, ok := existing[name]; ok {
			return name, nil
		}
	}

	if fallback == "" {
		return "", fmt.Errorf("no destination mailbox available")
	}

	if err := c.client.Create(fallback, nil).Wait(); err != nil {
		// If creation fails due to existence/race, continue and use fallback.
		if _, ok := existing[fallback]; !ok {
			return "", fmt.Errorf("failed to create mailbox %q: %w", fallback, err)
		}
	}

	// Invalidate mailbox cache after create.
	c.mailboxCacheMu.Lock()
	c.mailboxCache = nil
	c.mailboxCacheTime = time.Time{}
	c.mailboxCacheMu.Unlock()

	return fallback, nil
}

func groupMessageIDsByFolder(messageIDs []string) (map[string][]imap.UID, []string) {
	byFolder := make(map[string][]imap.UID)
	seen := make(map[string]map[imap.UID]bool)
	var invalid []string

	for _, id := range messageIDs {
		folder, uid, err := parseMessageID(id)
		if err != nil {
			invalid = append(invalid, id)
			continue
		}
		if seen[folder] == nil {
			seen[folder] = make(map[imap.UID]bool)
		}
		if seen[folder][uid] {
			continue
		}
		seen[folder][uid] = true
		byFolder[folder] = append(byFolder[folder], uid)
	}

	return byFolder, invalid
}

func uidSetFromSlice(uids []imap.UID) imap.UIDSet {
	set := imap.UIDSet{}
	for _, uid := range uids {
		set.AddNum(uid)
	}
	return set
}

// ProviderName returns the name of the provider.
func (c *IMAPClient) ProviderName() string {
	return c.name
}

// Close releases resources held by the client.
func (c *IMAPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Drain connection pool
	for {
		select {
		case conn := <-c.pool:
			conn.Close()
		default:
			goto poolDrained
		}
	}
poolDrained:

	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
	}
	return nil
}

func parseMessageID(messageID string) (folder string, uid imap.UID, err error) {
	parts := strings.SplitN(messageID, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid message ID format: %s", messageID)
	}

	uidNum, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("invalid UID in message ID: %s", messageID)
	}

	return parts[0], imap.UID(uidNum), nil
}

func parseIMAPMessage(folder string, msg *imapclient.FetchMessageBuffer, fetchBody bool) (*providerdata.EmailMessage, error) {
	email := &providerdata.EmailMessage{
		ID:     fmt.Sprintf("%s:%d", folder, msg.UID),
		Labels: []string{folder},
	}

	seenFound := false
	flaggedFound := false
	for _, flag := range msg.Flags {
		if flag == imap.FlagSeen {
			seenFound = true
		}
		if flag == imap.FlagFlagged {
			flaggedFound = true
		}
	}
	email.IsRead = seenFound
	email.IsFlagged = flaggedFound

	if msg.Envelope != nil {
		env := msg.Envelope
		email.Subject = env.Subject
		if email.Subject == "" {
			email.Subject = "(No subject)"
		}

		if len(env.From) > 0 {
			email.Sender = formatAddress(env.From[0])
		}

		for _, addr := range env.To {
			email.Recipients = append(email.Recipients, formatAddress(addr))
		}

		if !env.Date.IsZero() {
			email.Date = env.Date
		} else {
			email.Date = time.Now()
		}

		email.ThreadID = env.MessageID
	}

	if fetchBody && len(msg.BodySection) > 0 {
		for _, section := range msg.BodySection {
			bodyText, bodyHTML := extractIMAPBody(section.Bytes)
			if bodyText != "" {
				email.BodyText = &bodyText
			}
			if bodyHTML != "" {
				email.BodyHTML = &bodyHTML
			}
		}
	}

	return email, nil
}

func formatAddress(addr imap.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s@%s>", addr.Name, addr.Mailbox, addr.Host)
	}
	return fmt.Sprintf("%s@%s", addr.Mailbox, addr.Host)
}

func extractIMAPBody(literal []byte) (text, html string) {
	mr, err := gomessage.CreateReader(bytes.NewReader(literal))
	if err != nil {
		return string(literal), ""
	}

	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}

		switch h := part.Header.(type) {
		case *gomessage.InlineHeader:
			ct, _, _ := h.ContentType()
			body, err := io.ReadAll(part.Body)
			if err != nil {
				continue
			}

			switch ct {
			case "text/plain":
				if text == "" {
					text = string(body)
				}
			case "text/html":
				if html == "" {
					html = string(body)
				}
			}
		}
	}

	return text, html
}
