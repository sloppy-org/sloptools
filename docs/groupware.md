# Groupware MCP reference

MCP tool reference and per-backend capability matrix for the groupware surface (mail, contacts, calendar, tasks, mailbox settings).

Out of scope: Canvas, Handoff, Temp files, Workspace, Items, Artifacts, and Actors tools.

## Capability interfaces

Authoritative interface declarations (Go file:line):

**Calendar** — `internal/calendar/provider.go`
- `Provider` (line 29), `EventMutator` (39), `InviteResponder` (47), `FreeBusyLooker` (53), `RecurrenceExpander` (59), `ICSExporter` (65), `EventSearcher` (73).

**Contacts** — `internal/contacts/contacts_02.go`
- `Provider` (line 27), `Searcher` (37), `Mutator` (44), `Grouper` (57), `PhotoFetcher` (65).

**Tasks** — `internal/tasks/provider.go`
- `Provider` (line 24), `Mutator` (34), `Completer` (43), `ListManager` (50).

**Mailbox settings** — `internal/mailboxsettings/provider.go`
- `OOFProvider` (line 21), `DelegationProvider` (line 31).

**Mail** — `internal/email/email_12.go`, `internal/email/email_13.go`, `internal/email/email_01.go`
- `FlagMutator` (email_12:244), `CategoryMutator` (email_12:249), `EmailProvider` (email_12:253), `AttachmentProvider` (email_12:268), `ResolvedArchiveProvider` (email_12:277), `ResolvedMoveToInboxProvider` (email_12:281), `ResolvedTrashProvider` (email_12:285), `ResolvedNamedFolderProvider` (email_12:289), `MessagePageProvider` (email_12:298), `FolderIncrementalSyncProvider` (email_12:309), `MessageActionProvider` (email_12:333), `RawMessageProvider` (email_12:338), `ServerFilterProvider` (email_13:232), `NamedFolderProvider` (email_13:239), `NamedLabelProvider` (email_13:243), `DraftProvider` (email_01:265), `ExistingDraftSender` (email_01:272).

## Capability matrix

| Capability | Gmail | IMAP | Exchange (EWS) | Google Calendar | ICS | Google Tasks |
|---|---|---|---|---|---|---|
| **Calendar: Provider** | ✓ | ✗ | ✓ | ✓ | ✗ | — |
| **Calendar: EventMutator** | ✓ | ✗ | ✓ | ✓ | ✗ | — |
| **Calendar: InviteResponder** | ✓ | ✗ | ✓ | ✓ | ✗ | — |
| **Calendar: FreeBusyLooker** | ✓ | ✗ | ✓ | ✓ | ✗ | — |
| **Calendar: ICSExporter** | ✓ | partial | ✓ | ✓ | ✗ | — |
| **Calendar: EventSearcher** | ✓ | ✗ | ✓ | ✓ | ✗ | — |
| **Contacts: Provider** | ✓ | ✗ | ✓ | — | — | — |
| **Contacts: Searcher** | ✓ | ✗ | partial | — | — | — |
| **Contacts: Mutator** | ✓ | ✗ | ✓ | — | — | — |
| **Contacts: Grouper** | ✓ | ✗ | partial | — | — | — |
| **Contacts: PhotoFetcher** | ✓ | ✗ | ✗ | — | — | — |
| **Tasks: Provider** | ✓ | ✗ | ✓ | — | — | ✓ |
| **Tasks: Mutator** | ✓ | ✗ | ✓ | — | — | ✓ |
| **Tasks: Completer** | ✓ | ✗ | ✓ | — | — | ✓ |
| **Tasks: ListManager** | partial | ✗ | ✓ | — | — | partial |
| **Mail: EmailProvider** | ✓ | ✓ | ✓ | — | — | — |
| **Mail: FlagMutator** | ✓ | ✓ | ✓ | — | — | — |
| **Mail: CategoryMutator** | ✓ | ✗ | ✓ | — | — | — |
| **Mail: AttachmentProvider** | ✓ | ✓ | ✓ | — | — | — |
| **Mail: DraftProvider** | ✓ | ✓ | ✓ | — | — | — |
| **Mail: ExistingDraftSender** | ✓ | partial | ✓ | — | — | — |
| **Mail: ServerFilterProvider** | ✓ | ✗ | ✓ | — | — | — |
| **Mail: NamedFolderProvider** | ✓ | ✓ | ✓ | — | — | — |
| **Mail: NamedLabelProvider** | ✓ | ✗ | partial | — | — | — |
| **Mail: MessageActionProvider** | ✓ | ✗ | ✓ | — | — | — |
| **Mail: ResolvedArchiveProvider** | ✓ | ✓ | ✓ | — | — | — |
| **Mail: ResolvedMoveToInboxProvider** | ✓ | ✓ | ✓ | — | — | — |
| **Mail: ResolvedTrashProvider** | ✓ | ✓ | ✓ | — | — | — |
| **Mailbox: OOFProvider** | ✓ | ✗ | ✓ | — | — | — |
| **Mailbox: DelegationProvider** | ✓ | ✗ | ✓ | — | — | — |

Key: `✓` = fully supported, `partial` = supported with limitations, `✗` = not supported, `—` = not applicable.

## Mail tools

### `mail_account_list`

Lists enabled email accounts available through Sloppy. No inputs. Returns provider name, sphere, and account id for each account.

### `mail_label_list`

Lists labels or folders for a mail account. Required: `account_id`. Optional: `folder` (scope). Gmail returns user labels; IMAP returns mailboxes; EWS returns folders.

### `mail_message_list`

Lists messages from a mail account, newest first, with mailbox filters and paging. Returns up to 50 messages per page (default 20). Required: `account_id`. Optional filters: `folder`, `subject`, `from`, `to`, `after`, `before`, `is_read`, `is_flagged`, `has_attachment`, `query`, `limit`, `next_page_token`. Gmail and EWS support server-side filtering; IMAP uses client-side filtering with server-side FETCH.

### `mail_message_get`

Gets one full message from a mail account. Required: `account_id`, `message_id`. Optional: `format` (`full` or `metadata`). All backends support metadata format; `full` returns body content when available.

### `mail_attachment_get`

Downloads one mail attachment to disk. Required: `account_id`, `message_id`, `attachment_id`. Optional: `dest_dir` (defaults to `~/Downloads/sloppy-attachments`). All backends support this.

### `mail_action`

Applies one mailbox action to one or more messages, optionally resolving targets from a search query. Required: `account_id`, `action`, `message_id` or `message_ids`. Actions: `archive`, `trash`, `move_to_inbox`, `move_to_folder`, `defer`, `delegate`, `mark_read`, `mark_unread`, `apply_label`, `archive_label`. Gmail maps actions to label operations; IMAP uses STORE flags and COPY/UID MOVE; EWS uses native SOAP operations. Returns `capability_unsupported` for unknown actions.

### `mail_message_copy`

Copies one or more messages from one mail account to another, preserving full message content including attachments. Required: `source_account_id`, `target_account_id`, `message_id` or `message_ids`, `target_folder`. All backends support this by fetching and re-sending.

### `mail_send`

Composes and sends a plain-text email. Required: `account_id`, `to`, `subject`, `body`. Optional: `cc`, `bcc`, `attachments`, `draft_only`, `in_reply_to`, `references`. Gmail uses the Gmail API send method; IMAP uses SMTP; EWS uses CreateItem. When `draft_only=true`, saves as draft instead of sending.

### `mail_draft_send`

Sends an existing draft by id without rewriting its content. Required: `account_id`, `draft_id`. Supported for accounts whose backend can dispatch a saved draft directly (Exchange EWS, Gmail). Use this after `mail_send` with `draft_only=true`, or to send a draft edited in a mail client.

### `mail_reply`

Repplies to an existing message with correct threading. Required: `account_id`, `message_id`, `body`. Optional: `reply_all`, `to`, `cc`, `quote_style` (`bottom_post` or `top_post`). Gmail and EWS use native reply; IMAP composes a new message with In-Reply-To/References headers.

### `mail_server_filter_list`

Lists provider-native server filters or rules. Required: `account_id`. Gmail returns filters from users.settings.filters; IMAP returns `capability_unsupported` (no server-side filter protocol); EWS returns rules from GetRules.

### `mail_server_filter_upsert`

Creates or updates a provider-native server filter. Required: `account_id`, `filter` object with `name`, `criteria`, `action`. Gmail uses CreateFilter; EWS uses SetRule; IMAP returns `capability_unsupported`.

### `mail_server_filter_delete`

Deletes a provider-native server filter. Required: `account_id`, `filter_id`. Gmail uses DeleteFilter; EWS uses SetRule with delete action; IMAP returns `capability_unsupported`.

### `mail_flag_set`

Sets the follow-up flag on one or more messages. Required: `account_id`, `message_id` or `message_ids`, `status` (`flagged` or `complete`). Optional: `due_at` (RFC3339). Gmail maps flagged onto STARRED label; `complete` status returns `capability_unsupported` for Gmail. EWS sets FlagStatus natively. IMAP uses \Flagged flag.

### `mail_flag_clear`

Clears the follow-up flag on one or more messages. Required: `account_id`, `message_id` or `message_ids`. All backends support this.

### `mail_categories_set`

Replaces the set of categories on one or more messages. Required: `account_id`, `message_id` or `message_ids`, `categories`. Gmail creates or reuses user labels whose display name matches each category; IMAP returns `capability_unsupported` (no category concept); EWS writes ItemCategories natively.

### `mail_oof_get`

Reads out-of-office / vacation-responder settings for the mailbox. Required: `account_id`. Gmail returns settings from users.settings; EWS returns from GetOofSettings; IMAP returns `capability_unsupported`.

### `mail_oof_set`

Writes out-of-office / vacation-responder settings. Required: `account_id`, `settings` object with `enabled`, `scope`, `internal_reply`, `external_reply`, `start_at`, `end_at`. Gmail uses UpdateSettings; EWS uses SetOofSettings; IMAP returns `capability_unsupported`.

### `mail_delegate_list`

Lists mailbox delegates and shared mailboxes. Required: `account_id`. Gmail reports delegates from users.settings.delegates and forwarding addresses as shared mailboxes; EWS reports delegates via GetDelegate and returns an empty shared_mailboxes list. Returns `capability_unsupported` for IMAP.

## Contacts tools

### `contact_list`

Lists contacts in an address book. Required: `account_id`. Optional: `group_id` (filter by contact group). Gmail returns all contacts from Google People; EWS returns contacts from the Contacts folder.

### `contact_get`

Gets one contact by provider id. Required: `account_id`, `contact_id`. Returns full contact payload including provider_ref.

### `contact_search`

Searches contacts by free-text query. Required: `account_id`, `query`. Gmail uses Google People searchContacts; EWS uses FindPeople; IMAP returns `capability_unsupported` (backends fall back to ListContacts + local filtering).

### `contact_create`

Creates a new contact. Required: `account_id`, `contact` object. Returns error_code=`capability_unsupported` when the backend is read-only. Gmail and EWS support this.

### `contact_update`

Updates an existing contact. Required: `account_id`, `contact` object with `provider_ref`. Returns error_code=`capability_unsupported` when the backend is read-only.

### `contact_delete`

Deletes a contact by provider id. Required: `account_id`, `contact_id`. Returns error_code=`capability_unsupported` when the backend is read-only.

### `contact_group_list`

Lists contact groups. Required: `account_id`. Gmail returns contact groups from Google People; EWS returns distribution list categories. Returns error_code=`capability_unsupported` when the backend does not expose groups.

### `contact_photo_get`

Fetches a contact's primary photo as base64-encoded bytes. Required: `account_id`, `contact_id`. Returns error_code=`capability_unsupported` when the backend does not expose photos or the contact has none. Only Gmail supports this.

## Calendar tools

### `calendar_list`

Lists available calendars. Required: `account_id`. Optional: `sphere` (work or private). Gmail/Google Calendar returns calendars from the CalendarList API; EWS returns calendars from the CalendarFolder.

### `calendar_events`

Lists upcoming calendar events. Required: `account_id`. Optional: `calendar_id`, `before`, `after`, `limit`, `query` (free-text search). Returns events whose start falls inside the time window. EWS supports server-side free-text search; Google Calendar supports it via query parameter; ICS is read-only.

### `calendar_event_create`

Creates a calendar event. Required: `account_id`, `summary`, `start`. Optional: `end`, `location`, `description`, `attendees`, `all_day`, `duration_minutes`. Google Calendar uses events.insert; EWS uses CreateItem; ICS returns `capability_unsupported`.

### `calendar_event_get`

Gets a single calendar event by provider id. Required: `account_id`, `event_id`. Optional: `calendar_id`. Google Calendar uses events.get; EWS uses Item retrieval; ICS returns `capability_unsupported`.

### `calendar_event_update`

Updates an existing calendar event. Required: `account_id`, `event_id`, `event` object. Missing optional fields clear their values (full-replace semantics). Google Calendar uses events.patch; EWS uses UpdateItem; ICS returns `capability_unsupported`.

### `calendar_event_delete`

Deletes a calendar event by provider id. Required: `account_id`, `event_id`. Optional: `calendar_id`. Google Calendar uses events.delete; EWS uses DeleteItem; ICS returns `capability_unsupported`.

### `calendar_event_respond`

Responds to a meeting invitation. Required: `account_id`, `event_id`, `response` (`accepted`, `declined`, `tentative`). Optional: `comment`. Google Calendar uses events.update with attendee response; EWS uses Accept/Decline/Tentate responses.

### `calendar_event_ics_export`

Exports a calendar event as an RFC5545 iCalendar payload. Required: `account_id`, `event_id`. Optional: `calendar_id`. Falls back to a synthetic payload when the backend does not natively support ICS export.

### `calendar_freebusy`

Queries free/busy windows for participants. Required: `account_id`. Optional: `participants` (list of email addresses), `start`, `until`. Google Calendar uses freeBusy.query; EWS uses FreeBusyRequest; ICS returns `capability_unsupported`.

## Tasks tools

### `task_list_list`

Lists task lists (Google Tasks lists or EWS Tasks folders). Required: `account_id`. Returns the containers available for task operations.

### `task_list_create`

Creates a new task list. Required: `account_id`, `name` (display name). Returns error_code=`capability_unsupported` when the backend does not support list management. Google Tasks supports this; EWS supports this.

### `task_list_delete`

Deletes a task list. Required: `account_id`, `list_id`. Returns error_code=`capability_unsupported` when the backend does not support list management.

### `task_list`

Lists tasks inside a task list. Required: `account_id`. Optional: `list_id`, `state` (filter by state), `limit`. Returns tasks from the specified list. When `account_id` is omitted, the first enabled tasks-capable account for the sphere is used.

### `task_get`

Gets one task by provider id. Required: `account_id`. Optional: `list_id`. Returns the full task item.

### `task_create`

Creates a new task. Required: `account_id`, `title`. Optional: `list_id`, `due_at`, `notes`, `priority`, `state`. Returns error_code=`capability_unsupported` when the backend is read-only.

### `task_update`

Updates an existing task (full-replace semantics). Required: `account_id`, `task` object with `provider_ref`. Returns error_code=`capability_unsupported` when the backend is read-only.

### `task_complete`

Toggles the completed state on a task. Required: `account_id`, `task_id`. Optional: `completed` (defaults to `true`). Returns error_code=`capability_unsupported` when the backend does not support completion. Google Tasks and EWS support this.

### `task_delete`

Deletes a task. Required: `account_id`, `task_id`. Returns error_code=`capability_unsupported` when the backend is read-only.

## Error codes

- `account_not_found` — The specified external account id does not exist or is disabled.
- `capability_unsupported` — The backend for this account does not implement the required capability interface (e.g. IMAP has no category support, ICS is read-only).
- `invalid_argument` — A required parameter is missing or malformed.
