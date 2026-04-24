package email

import (
	"context"
	"crypto/tls"
	"fmt"
	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type IMAPClient struct {
	name             string
	host             string
	port             int
	username         string
	password         string
	useTLS           bool
	startTLS         bool
	client           *imapclient.Client
	mu               sync.Mutex
	pool             chan *imapclient.Client
	poolSize         int
	mailboxCache     []string
	mailboxCacheTime time.Time
	mailboxCacheMu   sync.RWMutex
	smtpConfig       SMTPConfig
	smtpSend         SMTPSender
} // IMAPClient provides access to an IMAP server.

var _ EmailProvider = (*IMAPClient)(nil) // Compile-time check that IMAPClient implements EmailProvider.

var _ MessageActionProvider = (*IMAPClient)(nil)

var _ MessagePageProvider = (*IMAPClient)(nil)

const DefaultPoolSize = 10 // DefaultPoolSize is the default number of pooled connections for parallel operations.

func NewIMAPClient(name, host string, port int, username, password string, useTLS, startTLS bool) *IMAPClient {
	if port == 0 {
		if useTLS {
			port = 993
		} else {
			port = 143
		}
	}
	return &IMAPClient{name: name, host: host, port: port, username: username, password: password, useTLS: useTLS, startTLS: startTLS, poolSize: DefaultPoolSize, pool: make(chan *imapclient.Client, DefaultPoolSize), smtpSend: defaultSMTPSender}
}

func (c *IMAPClient) ConfigureDraftTransport(cfg SMTPConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.smtpConfig = cfg
	if c.smtpSend == nil {
		c.smtpSend = defaultSMTPSender
	}
}

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

func (c *IMAPClient) WarmUp(ctx context.Context) error {
	if err := c.connect(ctx); err != nil {
		return err
	}
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
	for err := range errors {
		return fmt.Errorf("failed to warm up connection pool: %w", err)
	}
	return nil
}

func (c *IMAPClient) getPoolConn() (*imapclient.Client, error) {
	select {
	case conn := <-c.pool:
		if err := conn.Noop().Wait(); err != nil {
			conn.Close()
			return c.newConnection()
		}
		return conn, nil
	default:
		return c.newConnection()
	}
}

func (c *IMAPClient) putPoolConn(conn *imapclient.Client) {
	select {
	case c.pool <- conn:
	default:
		conn.Close()
	}
}

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
		label := providerdata.Label{ID: mbox.Mailbox, Name: mbox.Mailbox, Type: "imap"}
		selectData, err := c.client.Select(mbox.Mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err == nil {
			label.MessagesTotal = int(selectData.NumMessages)
		}
		labels = append(labels, label)
	}
	return labels, nil
}

func (c *IMAPClient) ListMessages(ctx context.Context, opts SearchOptions) ([]string, error) {
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	if opts.Folder != "" {
		return c.searchFolder(ctx, opts.Folder, opts)
	}
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
	page := MessagePage{IDs: make([]string, 0, end-offset)}
	for i := end - 1; i >= offset; i-- {
		page.IDs = append(page.IDs, fmt.Sprintf("%s:%d", folder, uids[i]))
	}
	if end < len(uids) {
		page.NextPageToken = strconv.Itoa(end)
	}
	return page, nil
}

const mailboxCacheTTL = 5 * time.Minute // mailboxCacheTTL is how long the mailbox list cache is valid.

func (c *IMAPClient) getCachedMailboxes(ctx context.Context) ([]string, error) {
	c.mailboxCacheMu.RLock()
	if len(c.mailboxCache) > 0 && time.Since(c.mailboxCacheTime) < mailboxCacheTTL {
		result := make([]string, len(c.mailboxCache))
		copy(result, c.mailboxCache)
		c.mailboxCacheMu.RUnlock()
		return result, nil
	}
	c.mailboxCacheMu.RUnlock()
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
	c.mailboxCacheMu.Lock()
	c.mailboxCache = folders
	c.mailboxCacheTime = time.Now()
	c.mailboxCacheMu.Unlock()
	return folders, nil
}

func (c *IMAPClient) searchAllFolders(ctx context.Context, opts SearchOptions) ([]string, error) {
	folders, err := c.getCachedMailboxes(ctx)
	if err != nil {
		return nil, err
	}
	skipFolders := map[string]bool{"Junk-E-Mail": true, "Gelöschte Elemente": true, "Entwürfe": true, "Postausgang": true, "Kalender": true, "Kontakte": true, "Aufgaben": true, "Journal": true, "Notizen": true, "RSS-Feeds": true}
	if !opts.IncludeSpamTrash {
		skipFolders["SPAM"] = true
		skipFolders["TRASH"] = true
		skipFolders["Spam"] = true
		skipFolders["Trash"] = true
	}
	isRecentSearch := !opts.Since.IsZero() && time.Since(opts.Since) < 365*24*time.Hour
	oldArchiveYears := make(map[string]bool)
	if isRecentSearch {
		searchYear := opts.Since.Year()
		for year := 2010; year < searchYear; year++ {
			oldArchiveYears[fmt.Sprintf("Archive/%d", year)] = true
		}
	}
	var priorityFolders, archiveFolders, otherFolders []string
	for _, folder := range // Categorize and prioritize folders
	folders {
		if skipFolders[folder] || strings.HasPrefix(folder, "Synchronisierungsprobleme") || strings.HasPrefix(folder, "Kontakte/") {
			continue
		}
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
	allFolders := append(priorityFolders, archiveFolders...)
	allFolders = append(allFolders, otherFolders...)
	maxResults := opts.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}
	searchCtx, cancelSearch := context.WithCancel(ctx)
	defer cancelSearch()
	resultsChan := make(chan []searchResult, len(allFolders))
	sem := make(chan struct{}, c.poolSize)
	var wg sync.WaitGroup
	var resultCount int64
	var resultCountMu sync.Mutex
	criteria := buildIMAPSearchCriteria(opts)
	for _, folder := range allFolders {
		wg.Add(1)
		go func(folder string) {
			defer wg.Done()
			select {
			case <-searchCtx.Done():
				return
			case sem <- struct{}{}:
				defer func() {
					<-sem
				}()
			}
			results := c.searchFolderWithDates(searchCtx, folder, criteria)
			if len(results) > 0 {
				resultsChan <- results
				resultCountMu.Lock()
				resultCount += int64(len(results))
				shouldStop := resultCount >= maxResults*2
				resultCountMu.Unlock()
				if shouldStop {
					cancelSearch()
				}
			}
		}(folder)
	}
	go func() {
		wg.Wait()
		close(resultsChan)
	}()
	var allResults []searchResult
	for results := range // Collect all results
	resultsChan {
		allResults = append(allResults, results...)
	}
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

func (c *IMAPClient) searchFolderWithDates(ctx context.Context, folder string, criteria *imap.SearchCriteria) []searchResult {
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
	uidSet := imap.UIDSet{}
	for _, uid := range uids {
		uidSet.AddNum(uid)
	}
	fetchCmd := conn.Fetch(uidSet, &imap.FetchOptions{UID: true, Envelope: true})
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
		results = append(results, searchResult{folder: folder, uid: msgBuf.UID, date: date})
	}
	return results
}

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
